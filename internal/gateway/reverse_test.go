package gateway

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

func TestReverseTCPRegisterAccept(t *testing.T) {
	gatewayListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer gatewayListener.Close()

	server := NewServer(log.New(io.Discard, "", 0), time.Second)
	go func() { _ = server.Serve(gatewayListener) }()

	control, err := net.Dial("tcp", gatewayListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	if err := tunnel.WriteControlSession(control); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(control); err != nil {
		t.Fatal(err)
	}

	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		conn, err := echo.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64)
		n, _ := conn.Read(buf)
		_, _ = conn.Write(append([]byte("echo:"), buf[:n]...))
	}()

	listenPort := freeTCPPort(t)
	if err := tunnel.WriteControlMessage(control, tunnel.ControlMessage{
		Type:        tunnel.CtrlRegister,
		InterceptID: "test/tcp",
		Network:     tunnel.NetworkTCP,
		ListenPort:  listenPort,
	}); err != nil {
		t.Fatal(err)
	}
	ack, err := tunnel.ReadControlMessage(control)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Type != tunnel.CtrlAck {
		t.Fatalf("ack type=%d err=%s", ack.Type, ack.Error)
	}

	readyCh := make(chan tunnel.ControlMessage, 1)
	go func() {
		msg, err := tunnel.ReadControlMessage(control)
		if err != nil {
			return
		}
		readyCh <- msg
	}()

	client, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", listenPort))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	var ready tunnel.ControlMessage
	select {
	case ready = <-readyCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for inbound-ready")
	}
	if ready.Type != tunnel.CtrlInboundReady || ready.StreamID == 0 {
		t.Fatalf("unexpected ready %#v", ready)
	}

	accept, err := net.Dial("tcp", gatewayListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer accept.Close()
	if err := tunnel.WriteAccept(accept, ready.StreamID); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(accept); err != nil {
		t.Fatal(err)
	}

	local, err := net.Dial("tcp", echo.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()

	go func() { _, _ = io.Copy(local, accept) }()
	go func() { _, _ = io.Copy(accept, local) }()

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := client.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "echo:ping" {
		t.Fatalf("got %q", got)
	}
}

func TestKCG2ControlNegotiatesCapabilities(t *testing.T) {
	gatewayListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer gatewayListener.Close()

	server := NewServer(log.New(io.Discard, "", 0), time.Second)
	server.Capabilities = tunnel.Capabilities{
		ProtocolVersion: 2,
		Inspector:       true,
		Protocols:       []string{"http", "https", "grpc"},
		MaxBodySize:     1 << 20,
		MaxTargets:      8,
		Engine:          "mitmproxy",
	}
	go func() { _ = server.Serve(gatewayListener) }()

	control, err := net.Dial("tcp", gatewayListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	token := gatewaySessionToken(1)
	if err := tunnel.WriteControlSessionV2(control, token); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(control); err != nil {
		t.Fatal(err)
	}
	capabilities, err := tunnel.ReadCapabilities(control)
	if err != nil {
		t.Fatal(err)
	}
	if !capabilities.Inspector ||
		capabilities.ProtocolVersion != 2 ||
		capabilities.Engine != "mitmproxy" ||
		len(capabilities.Protocols) != 3 {
		t.Fatalf("unexpected capabilities %#v", capabilities)
	}

	events, err := net.Dial("tcp", gatewayListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	if err := tunnel.WriteInspectorEventsSession(events, token); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(events); err != nil {
		t.Fatalf("event channel handshake: %v", err)
	}
}

func TestKCG2RejectsUnknownOutboundSession(t *testing.T) {
	gatewayListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer gatewayListener.Close()

	server := NewServer(log.New(io.Discard, "", 0), time.Second)
	go func() { _ = server.Serve(gatewayListener) }()

	connection, err := net.Dial("tcp", gatewayListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := tunnel.WriteOpenV2(connection, gatewaySessionToken(9), tunnel.OpenRequest{
		Command: tunnel.CommandTCP,
		Host:    "10.0.0.1",
		Port:    80,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(connection); err == nil ||
		err.Error() != "unknown or expired KCG2 session" {
		t.Fatalf("unexpected status: %v", err)
	}
}

func TestKCG2ControlReconnectReplacesPreviousConnection(t *testing.T) {
	gatewayListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer gatewayListener.Close()

	server := NewServer(log.New(io.Discard, "", 0), time.Second)
	go func() { _ = server.Serve(gatewayListener) }()
	token := gatewaySessionToken(3)

	dialControl := func() net.Conn {
		t.Helper()
		connection, dialErr := net.Dial("tcp", gatewayListener.Addr().String())
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		if writeErr := tunnel.WriteControlSessionV2(connection, token); writeErr != nil {
			connection.Close()
			t.Fatal(writeErr)
		}
		if statusErr := tunnel.ReadStatus(connection); statusErr != nil {
			connection.Close()
			t.Fatal(statusErr)
		}
		if _, capabilitiesErr := tunnel.ReadCapabilities(connection); capabilitiesErr != nil {
			connection.Close()
			t.Fatal(capabilitiesErr)
		}
		return connection
	}

	first := dialControl()
	defer first.Close()
	second := dialControl()
	defer second.Close()

	_ = first.SetReadDeadline(time.Now().Add(3 * time.Second))
	var value [1]byte
	if _, err := first.Read(value[:]); err == nil {
		t.Fatal("previous control connection remained active")
	}
}

func TestKCG2AcceptCannotTakeAnotherSessionsStream(t *testing.T) {
	server := NewServer(log.New(io.Discard, "", 0), time.Second)
	first := &controlSession{
		version: tunnel.ProtocolV2,
		token:   gatewaySessionToken(1),
	}
	second := &controlSession{
		version: tunnel.ProtocolV2,
		token:   gatewaySessionToken(2),
	}
	want := &pendingStream{id: 42, control: first}
	server.pending[want.id] = want

	if got := server.takePendingForSession(want.id, tunnel.SessionHeader{
		Version: tunnel.ProtocolV2,
		Command: tunnel.CommandAccept,
		Token:   second.token,
	}); got != nil {
		t.Fatal("another session took the pending stream")
	}
	if server.pending[want.id] == nil {
		t.Fatal("cross-session attempt consumed the pending stream")
	}
	if got := server.takePendingForSession(want.id, tunnel.SessionHeader{
		Version: tunnel.ProtocolV2,
		Command: tunnel.CommandAccept,
		Token:   first.token,
	}); got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func gatewaySessionToken(seed byte) tunnel.SessionToken {
	var token tunnel.SessionToken
	for index := range token {
		token[index] = seed + byte(index)
	}
	return token
}

func TestReverseUDPRegisterAccept(t *testing.T) {
	gatewayListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer gatewayListener.Close()

	server := NewServer(log.New(io.Discard, "", 0), time.Second)
	go func() { _ = server.Serve(gatewayListener) }()

	control, err := net.Dial("tcp", gatewayListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	if err := tunnel.WriteControlSession(control); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(control); err != nil {
		t.Fatal(err)
	}

	listenPort := freeUDPPort(t)
	if err := tunnel.WriteControlMessage(control, tunnel.ControlMessage{
		Type:        tunnel.CtrlRegister,
		InterceptID: "test/udp",
		Network:     tunnel.NetworkUDP,
		ListenPort:  listenPort,
	}); err != nil {
		t.Fatal(err)
	}
	ack, err := tunnel.ReadControlMessage(control)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Type != tunnel.CtrlAck {
		t.Fatalf("ack type=%d err=%s", ack.Type, ack.Error)
	}

	readyCh := make(chan tunnel.ControlMessage, 1)
	go func() {
		msg, err := tunnel.ReadControlMessage(control)
		if err == nil {
			readyCh <- msg
		}
	}()

	client, err := net.DialUDP("udp4", nil, &net.UDPAddr{
		IP: net.IPv4(127, 0, 0, 1), Port: int(listenPort),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}

	var ready tunnel.ControlMessage
	select {
	case ready = <-readyCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for UDP inbound-ready")
	}
	if ready.Type != tunnel.CtrlInboundReady ||
		ready.Network != tunnel.NetworkUDP ||
		ready.StreamID == 0 {
		t.Fatalf("unexpected ready %#v", ready)
	}

	accept, err := net.Dial("tcp", gatewayListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer accept.Close()
	if err := tunnel.WriteAccept(accept, ready.StreamID); err != nil {
		t.Fatal(err)
	}
	if err := tunnel.ReadStatus(accept); err != nil {
		t.Fatal(err)
	}
	payload, err := tunnel.ReadDatagram(bufio.NewReader(accept), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(payload); got != "ping" {
		t.Fatalf("got request %q", got)
	}
	if err := tunnel.WriteDatagram(accept, []byte("pong")); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	buffer := make([]byte, 32)
	n, err := client.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buffer[:n]); got != "pong" {
		t.Fatalf("got response %q", got)
	}
}

func freeTCPPort(t *testing.T) uint16 {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return uint16(l.Addr().(*net.TCPAddr).Port)
}

func freeUDPPort(t *testing.T) uint16 {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	return uint16(conn.LocalAddr().(*net.UDPAddr).Port)
}
