package socksbridge

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

func TestSOCKSUDPPacketRoundTrip(t *testing.T) {
	want := []byte("dns payload")
	packet, err := encodeUDPPacket("10.96.0.10", 53, want)
	if err != nil {
		t.Fatal(err)
	}
	host, port, got, err := parseUDPPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if host != "10.96.0.10" || port != 53 || !bytes.Equal(got, want) {
		t.Fatalf("got %s:%d %q", host, port, got)
	}
}

func TestSOCKSUDPDomainRoundTrip(t *testing.T) {
	packet, err := encodeUDPPacket("kube-dns.kube-system.svc.cluster.local", 53, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	host, port, _, err := parseUDPPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if host != "kube-dns.kube-system.svc.cluster.local" || port != 53 {
		t.Fatalf("got %s:%d", host, port)
	}
}

func TestOpenGatewayUsesKCG2SessionToken(t *testing.T) {
	gateway, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()

	var token tunnel.SessionToken
	for index := range token {
		token[index] = byte(index + 1)
	}
	requestCh := make(chan tunnel.OpenRequest, 1)
	go func() {
		connection, err := gateway.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		header, err := tunnel.ReadSessionHeaderInfo(connection)
		if err != nil ||
			header.Version != tunnel.ProtocolV2 ||
			header.Token != token {
			return
		}
		request, err := tunnel.ReadOpenBody(connection, header.Command)
		if err != nil {
			return
		}
		requestCh <- request
		_ = tunnel.WriteStatus(connection, nil)
	}()

	server := &Server{GatewayAddress: gateway.Addr().String(), SessionToken: token}
	connection, err := server.openGateway(tunnel.CommandTCP, "10.0.0.8", 8080)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	select {
	case request := <-requestCh:
		if request.Host != "10.0.0.8" || request.Port != 8080 {
			t.Fatalf("unexpected request %#v", request)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for KCG2 request")
	}
}

func TestHostTCPHandlerBypassesGateway(t *testing.T) {
	local, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	go func() {
		conn, err := local.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64)
		n, _ := conn.Read(buf)
		_, _ = conn.Write(append([]byte("local:"), buf[:n]...))
	}()

	server := &Server{
		GatewayAddress: "127.0.0.1:1", // must not be used
		HostTCP: func(host string, port uint16) (func(net.Conn), bool) {
			if host != "10.105.153.132" || port != 80 {
				return nil, false
			}
			return func(client net.Conn) {
				defer client.Close()
				upstream, err := net.Dial("tcp", local.Addr().String())
				if err != nil {
					return
				}
				defer upstream.Close()
				relay(client, upstream)
			}, true
		},
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { _ = server.Serve(listener) }()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	// SOCKS greeting + connect to intercepted ClusterIP.
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	req := []byte{5, 1, 0, 1, 10, 105, 153, 132, 0, 80}
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatal(err)
	}
	if head[1] != 0 {
		t.Fatalf("socks status=%d", head[1])
	}
	// drain bind addr
	rest := make([]byte, 6)
	if _, err := io.ReadFull(conn, rest); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "local:ping" {
		t.Fatalf("got %q", got)
	}
}

func TestHostUDPHandlerBypassesGateway(t *testing.T) {
	local, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	go func() {
		buf := make([]byte, 64)
		for {
			n, addr, err := local.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = local.WriteTo(append([]byte("local-udp:"), buf[:n]...), addr)
		}
	}()
	localPort := local.LocalAddr().(*net.UDPAddr).Port

	server := &Server{
		GatewayAddress: "127.0.0.1:1", // must not be used
		HostUDP: func(host string, port uint16) (func(context.Context) (net.Conn, error), bool) {
			if host != "10.105.153.132" || port != 9090 {
				return nil, false
			}
			return func(ctx context.Context) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(
					ctx, "udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)),
				)
			}, true
		},
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { _ = server.Serve(listener) }()

	control, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	_ = control.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := control.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(control, reply); err != nil {
		t.Fatal(err)
	}
	// UDP ASSOCIATE
	if _, err := control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(control, head); err != nil {
		t.Fatal(err)
	}
	if head[1] != 0 {
		t.Fatalf("socks status=%d", head[1])
	}
	bindIP := make([]byte, 4)
	if _, err := io.ReadFull(control, bindIP); err != nil {
		t.Fatal(err)
	}
	var bindPort [2]byte
	if _, err := io.ReadFull(control, bindPort[:]); err != nil {
		t.Fatal(err)
	}
	relayPort := int(bindPort[0])<<8 | int(bindPort[1])
	relayAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: relayPort}

	client, err := net.DialUDP("udp", nil, relayAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))

	packet, err := encodeUDPPacket("10.105.153.132", 9090, []byte("ping"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(packet); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 512)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	_, _, payload, err := parseUDPPacket(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if got := string(payload); got != "local-udp:ping" {
		t.Fatalf("got %q", got)
	}
}
