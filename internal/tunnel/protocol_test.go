package tunnel

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
)

func TestOpenRoundTrip(t *testing.T) {
	var stream bytes.Buffer
	want := OpenRequest{Command: CommandTCP, Host: "api.default.svc.cluster.local", Port: 8080}
	if err := WriteOpen(&stream, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadOpen(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestStatusRoundTrip(t *testing.T) {
	var stream bytes.Buffer
	if err := WriteStatus(&stream, errors.New("target denied")); err != nil {
		t.Fatal(err)
	}
	if err := ReadStatus(&stream); err == nil || err.Error() != "target denied" {
		t.Fatalf("unexpected status error: %v", err)
	}
}

func TestDatagramRoundTrip(t *testing.T) {
	var stream bytes.Buffer
	want := []byte("dns query")
	if err := WriteDatagram(&stream, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDatagram(bufio.NewReader(&stream), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestControlMessageRoundTrip(t *testing.T) {
	cases := []ControlMessage{
		{Type: CtrlRegister, InterceptID: "default/http:tcp:80", Network: NetworkTCP, ListenPort: 20080},
		{Type: CtrlUnregister, InterceptID: "default/http:tcp:80"},
		{Type: CtrlInboundReady, InterceptID: "default/http:udp:53", Network: NetworkUDP, StreamID: 42},
		{Type: CtrlAck},
		{Type: CtrlError, Error: "listen failed"},
	}
	for _, want := range cases {
		var stream bytes.Buffer
		if err := WriteControlMessage(&stream, want); err != nil {
			t.Fatalf("write %#v: %v", want, err)
		}
		got, err := ReadControlMessage(&stream)
		if err != nil {
			t.Fatalf("read %#v: %v", want, err)
		}
		if got != want {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func TestAcceptRoundTrip(t *testing.T) {
	var stream bytes.Buffer
	if err := WriteAccept(&stream, 99); err != nil {
		t.Fatal(err)
	}
	command, err := ReadSessionHeader(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if command != CommandAccept {
		t.Fatalf("command=%d", command)
	}
	streamID, err := ReadAcceptStreamID(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if streamID != 99 {
		t.Fatalf("streamID=%d", streamID)
	}
}

func TestControlSessionHeader(t *testing.T) {
	var stream bytes.Buffer
	if err := WriteControlSession(&stream); err != nil {
		t.Fatal(err)
	}
	command, err := ReadSessionHeader(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if command != CommandControl {
		t.Fatalf("command=%d", command)
	}
}
