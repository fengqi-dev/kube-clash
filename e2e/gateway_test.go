//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fengqi-dev/kube-loop/internal/socksbridge"
	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

func TestGatewayTCPAndDNS(t *testing.T) {
	requireE2E(t)
	ctx, cancel := testContext(t, 3*time.Minute)
	defer cancel()

	provider := newProvider(t)
	_, forwarder := ensureGateway(t, ctx, provider)
	client := kubeClient(t, provider)

	apiService, err := client.CoreV1().Services("default").Get(ctx, "kubernetes", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dnsService, err := client.CoreV1().Services("kube-system").Get(ctx, "kube-dns", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	echoService, err := client.CoreV1().Services(echoNamespace).Get(ctx, "echo", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	gatewayAddress := forwarder.Address()
	assertGatewayTCP(t, gatewayAddress, apiService.Spec.ClusterIP, 443)
	assertGatewayDNS(t, gatewayAddress, dnsService.Spec.ClusterIP)
	assertGatewayUDPEcho(t, gatewayAddress, echoService.Spec.ClusterIP, 9090)

	bridgeContext, stopBridge := context.WithCancel(ctx)
	defer stopBridge()
	bridge, err := socksbridge.Listen(bridgeContext, gatewayAddress)
	if err != nil {
		t.Fatal(err)
	}
	assertSOCKSTCP(t, bridge.Addr().String(), apiService.Spec.ClusterIP, 443)
	assertSOCKSDNS(t, bridge.Addr().String(), dnsService.Spec.ClusterIP)
	assertSOCKSUDPEcho(t, bridge.Addr().String(), echoService.Spec.ClusterIP, 9090)
}

func assertGatewayTCP(t *testing.T, gatewayAddress, targetIP string, port uint16) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", gatewayAddress, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := tunnel.WriteOpen(connection, tunnel.OpenRequest{
		Command: tunnel.CommandTCP, Host: targetIP, Port: port,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(connection); err != nil {
		t.Fatalf("gateway TCP dial failed: %v", err)
	}
}

func assertGatewayDNS(t *testing.T, gatewayAddress, dnsIP string) {
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

func assertGatewayUDPEcho(t *testing.T, gatewayAddress, targetIP string, port uint16) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", gatewayAddress, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := tunnel.WriteOpen(connection, tunnel.OpenRequest{
		Command: tunnel.CommandUDP, Host: targetIP, Port: port,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(connection); err != nil {
		t.Fatalf("gateway UDP dial failed: %v", err)
	}
	if err := tunnel.WriteDatagram(connection, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	response, err := tunnel.ReadDatagram(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(response); got != "cluster-udp:ping" {
		t.Fatalf("gateway UDP echo got %q, want cluster-udp:ping", got)
	}
}

func assertSOCKSUDPEcho(t *testing.T, bridgeAddress, targetIP string, port uint16) {
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

	packet := append([]byte{0, 0, 0}, socksAddress(t, targetIP)...)
	var encodedPort [2]byte
	binary.BigEndian.PutUint16(encodedPort[:], port)
	packet = append(packet, encodedPort[:]...)
	packet = append(packet, []byte("ping")...)
	if _, err := udp.Write(packet); err != nil {
		t.Fatal(err)
	}
	_ = udp.SetReadDeadline(time.Now().Add(5 * time.Second))
	response := make([]byte, 65535)
	read, err := udp.Read(response)
	if err != nil {
		t.Fatal(err)
	}
	// SOCKS UDP reply: RSV(2) FRAG(1) ATYP+ADDR+PORT then payload.
	if read <= 10 {
		t.Fatalf("short SOCKS UDP response: %d", read)
	}
	if got := string(response[10:read]); got != "cluster-udp:ping" {
		t.Fatalf("SOCKS UDP echo got %q, want cluster-udp:ping", got)
	}
}

func assertSOCKSTCP(t *testing.T, bridgeAddress, targetIP string, port uint16) {
	t.Helper()
	control := openSOCKSControl(t, bridgeAddress)
	defer control.Close()
	if _, err := control.Write(socksRequest(t, 1, targetIP, port)); err != nil {
		t.Fatal(err)
	}
	if status := readSOCKSReply(t, control); status != 0 {
		t.Fatalf("SOCKS TCP connect failed with status %d", status)
	}
}

func assertSOCKSDNS(t *testing.T, bridgeAddress, dnsIP string) {
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
