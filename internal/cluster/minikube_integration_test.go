//go:build integration

package cluster

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fengqi-dev/kube-loop/internal/socksbridge"
	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

func TestMinikubeGatewayTCPAndDNS(t *testing.T) {
	if os.Getenv("KUBELOOP_MINIKUBE_TEST") != "1" {
		t.Skip("set KUBELOOP_MINIKUBE_TEST=1 to run against the local minikube context")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	provider := NewProvider()
	podName, err := provider.EnsureGateway(ctx, "minikube", "kube-loop-gateway:dev")
	if err != nil {
		t.Fatal(err)
	}
	forwarder, err := provider.StartPortForward(ctx, "minikube", podName, GatewayPort)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := forwarder.Close(); closeErr != nil {
			t.Logf("close port-forward: %v", closeErr)
		}
	}()

	client, err := provider.client("minikube")
	if err != nil {
		t.Fatal(err)
	}
	apiService, err := client.CoreV1().Services("default").Get(
		ctx, "kubernetes", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	dnsService, err := client.CoreV1().Services("kube-system").Get(
		ctx, "kube-dns", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	gatewayAddress := forwarder.Address()
	testGatewayTCP(t, gatewayAddress, apiService.Spec.ClusterIP)
	testGatewayDNS(t, gatewayAddress, dnsService.Spec.ClusterIP)
	if target := os.Getenv("KUBELOOP_MINIKUBE_HTTP_TARGET"); target != "" {
		testGatewayHTTP(t, gatewayAddress, target)
	}

	bridgeContext, stopBridge := context.WithCancel(ctx)
	defer stopBridge()
	bridge, err := socksbridge.Listen(bridgeContext, gatewayAddress)
	if err != nil {
		t.Fatal(err)
	}
	testSOCKSTCP(t, bridge.Addr().String(), apiService.Spec.ClusterIP)
	testSOCKSDNS(t, bridge.Addr().String(), dnsService.Spec.ClusterIP)
}

func testGatewayHTTP(t *testing.T, gatewayAddress, target string) {
	t.Helper()
	host, rawPort, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatalf("parse HTTP target %q: %v", target, err)
	}
	var port uint16
	if _, err := fmt.Sscanf(rawPort, "%d", &port); err != nil {
		t.Fatalf("parse HTTP target port %q: %v", rawPort, err)
	}
	connection, err := net.DialTimeout("tcp", gatewayAddress, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := tunnel.WriteOpen(connection, tunnel.OpenRequest{
		Command: tunnel.CommandTCP, Host: host, Port: port,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(connection); err != nil {
		t.Fatalf("gateway HTTP dial failed: %v", err)
	}
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(connection, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", host); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(io.LimitReader(connection, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response), "HTTP/1.1 200 OK") {
		t.Fatalf("unexpected HTTP response through gateway: %q", response)
	}
}

func testGatewayTCP(t *testing.T, gatewayAddress, targetIP string) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", gatewayAddress, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := tunnel.WriteOpen(connection, tunnel.OpenRequest{
		Command: tunnel.CommandTCP, Host: targetIP, Port: 443,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(connection); err != nil {
		t.Fatalf("gateway TCP dial failed: %v", err)
	}
}

func testGatewayDNS(t *testing.T, gatewayAddress, dnsIP string) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", gatewayAddress, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := tunnel.WriteOpen(connection, tunnel.OpenRequest{
		Command: tunnel.CommandUDP, Host: dnsIP, Port: 53,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(connection); err != nil {
		t.Fatalf("gateway UDP dial failed: %v", err)
	}
	name := dnsmessage.MustNewName("kubernetes.default.svc.cluster.local.")
	query, err := (&dnsmessage.Message{
		Header:    dnsmessage.Header{ID: 42, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
	}).Pack()
	if err != nil {
		t.Fatal(err)
	}
	if err := tunnel.WriteDatagram(connection, query); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	response, err := tunnel.ReadDatagram(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal(err)
	}
	var message dnsmessage.Message
	if err := message.Unpack(response); err != nil {
		t.Fatal(err)
	}
	if message.Header.RCode != dnsmessage.RCodeSuccess || len(message.Answers) == 0 {
		t.Fatalf("unexpected DNS response: rcode=%v answers=%d", message.Header.RCode, len(message.Answers))
	}
}

func testSOCKSTCP(t *testing.T, bridgeAddress, targetIP string) {
	t.Helper()
	control := openSOCKSControl(t, bridgeAddress)
	defer control.Close()
	request := socksRequest(t, 1, targetIP, 443)
	if _, err := control.Write(request); err != nil {
		t.Fatal(err)
	}
	if status := readSOCKSReply(t, control); status != 0 {
		t.Fatalf("SOCKS TCP connect failed with status %d", status)
	}
}

func testSOCKSDNS(t *testing.T, bridgeAddress, dnsIP string) {
	t.Helper()
	control := openSOCKSControl(t, bridgeAddress)
	defer control.Close()
	if _, err := control.Write(socksRequest(t, 3, "0.0.0.0", 0)); err != nil {
		t.Fatal(err)
	}
	status, bindAddress := readSOCKSReplyAddress(t, control)
	if status != 0 {
		t.Fatalf("SOCKS UDP associate failed with status %d", status)
	}
	udp, err := net.DialUDP("udp", nil, bindAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()

	name := dnsmessage.MustNewName("kubernetes.default.svc.cluster.local.")
	query, err := (&dnsmessage.Message{
		Header:    dnsmessage.Header{ID: 43, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
	}).Pack()
	if err != nil {
		t.Fatal(err)
	}
	packet := append([]byte{0, 0, 0}, socksAddress(t, dnsIP)...)
	var port [2]byte
	binary.BigEndian.PutUint16(port[:], 53)
	packet = append(packet, port[:]...)
	packet = append(packet, query...)
	if _, err := udp.Write(packet); err != nil {
		t.Fatal(err)
	}
	_ = udp.SetReadDeadline(time.Now().Add(5 * time.Second))
	response := make([]byte, 65535)
	read, err := udp.Read(response)
	if err != nil {
		t.Fatal(err)
	}
	if read <= 10 {
		t.Fatalf("short SOCKS UDP response: %d", read)
	}
	var message dnsmessage.Message
	if err := message.Unpack(response[10:read]); err != nil {
		t.Fatal(err)
	}
	if message.Header.RCode != dnsmessage.RCodeSuccess || len(message.Answers) == 0 {
		t.Fatalf("unexpected SOCKS DNS response: rcode=%v answers=%d", message.Header.RCode, len(message.Answers))
	}
}

func openSOCKSControl(t *testing.T, address string) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte{5, 1, 0}); err != nil {
		connection.Close()
		t.Fatal(err)
	}
	response := make([]byte, 2)
	if _, err := io.ReadFull(connection, response); err != nil {
		connection.Close()
		t.Fatal(err)
	}
	if response[0] != 5 || response[1] != 0 {
		connection.Close()
		t.Fatalf("SOCKS negotiation failed: %v", response)
	}
	return connection
}

func socksRequest(t *testing.T, command byte, host string, port uint16) []byte {
	t.Helper()
	value := append([]byte{5, command, 0}, socksAddress(t, host)...)
	var encodedPort [2]byte
	binary.BigEndian.PutUint16(encodedPort[:], port)
	return append(value, encodedPort[:]...)
}

func socksAddress(t *testing.T, host string) []byte {
	t.Helper()
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		t.Fatalf("test requires IPv4 target, got %q", host)
	}
	return append([]byte{1}, ip.To4()...)
}

func readSOCKSReply(t *testing.T, connection net.Conn) byte {
	t.Helper()
	status, _ := readSOCKSReplyAddress(t, connection)
	return status
}

func readSOCKSReplyAddress(t *testing.T, connection net.Conn) (byte, *net.UDPAddr) {
	t.Helper()
	header := make([]byte, 4)
	if _, err := io.ReadFull(connection, header); err != nil {
		t.Fatal(err)
	}
	if header[0] != 5 || header[3] != 1 {
		t.Fatalf("unexpected SOCKS reply: %v", header)
	}
	address := make([]byte, 6)
	if _, err := io.ReadFull(connection, address); err != nil {
		t.Fatal(err)
	}
	return header[1], &net.UDPAddr{
		IP: net.IP(address[:4]), Port: int(binary.BigEndian.Uint16(address[4:])),
	}
}
