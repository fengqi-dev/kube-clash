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
