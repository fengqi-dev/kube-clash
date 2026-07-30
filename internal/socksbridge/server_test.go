package socksbridge

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
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
