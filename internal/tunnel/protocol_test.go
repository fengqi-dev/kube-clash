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

func TestOpenV2RoundTrip(t *testing.T) {
	token := testSessionToken()
	var stream bytes.Buffer
	want := OpenRequest{Command: CommandUDP, Host: "10.96.0.10", Port: 53}
	if err := WriteOpenV2(&stream, token, want); err != nil {
		t.Fatal(err)
	}
	header, err := ReadSessionHeaderInfo(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if header.Version != ProtocolV2 || header.Command != CommandUDP || header.Token != token {
		t.Fatalf("unexpected header %#v", header)
	}
	got, err := ReadOpenBody(&stream, header.Command)
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

func TestAcceptV2RoundTrip(t *testing.T) {
	token := testSessionToken()
	var stream bytes.Buffer
	if err := WriteAcceptV2(&stream, token, 101); err != nil {
		t.Fatal(err)
	}
	header, err := ReadSessionHeaderInfo(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if header.Version != ProtocolV2 || header.Command != CommandAccept || header.Token != token {
		t.Fatalf("unexpected header %#v", header)
	}
	streamID, err := ReadAcceptStreamID(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if streamID != 101 {
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

func TestControlSessionV2Capabilities(t *testing.T) {
	token := testSessionToken()
	var stream bytes.Buffer
	if err := WriteControlSessionV2(&stream, token); err != nil {
		t.Fatal(err)
	}
	want := Capabilities{
		ProtocolVersion: 2,
		Inspector:       true,
		Protocols:       []string{"http", "https", "grpc"},
		MaxBodySize:     1 << 20,
		MaxTargets:      8,
		Engine:          "mitmproxy",
	}
	if err := WriteCapabilities(&stream, want); err != nil {
		t.Fatal(err)
	}
	header, err := ReadSessionHeaderInfo(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if header.Version != ProtocolV2 || header.Command != CommandControl || header.Token != token {
		t.Fatalf("unexpected header %#v", header)
	}
	got, err := ReadCapabilities(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProtocolVersion != want.ProtocolVersion ||
		got.Inspector != want.Inspector ||
		got.Engine != want.Engine ||
		len(got.Protocols) != len(want.Protocols) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestV2RejectsZeroToken(t *testing.T) {
	var stream bytes.Buffer
	stream.Write([]byte{'K', 'C', 'G', ProtocolV2, CommandControl})
	stream.Write(make([]byte, SessionTokenSize))
	if _, err := ReadSessionHeaderInfo(&stream); err == nil {
		t.Fatal("expected zero token to be rejected")
	}
}

func TestInspectorEventsV2Header(t *testing.T) {
	token := testSessionToken()
	var stream bytes.Buffer
	if err := WriteInspectorEventsSession(&stream, token); err != nil {
		t.Fatal(err)
	}
	header, err := ReadSessionHeaderInfo(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if header.Version != ProtocolV2 ||
		header.Command != CommandInspectorEvents ||
		header.Token != token {
		t.Fatalf("unexpected header %#v", header)
	}
}

func TestInspectorEventRoundTrip(t *testing.T) {
	want := InspectorEvent{
		Version:  InspectorEventVersion1,
		Type:     InspectorEventHeaders,
		FlowID:   "flow-42",
		Sequence: 7,
		Payload:  []byte(`{"method":"GET","status":200}`),
	}
	var stream bytes.Buffer
	if err := WriteInspectorEvent(&stream, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadInspectorEvent(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != want.Version ||
		got.Type != want.Type ||
		got.FlowID != want.FlowID ||
		got.Sequence != want.Sequence ||
		!bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func testSessionToken() SessionToken {
	var token SessionToken
	for index := range token {
		token[index] = byte(index + 1)
	}
	return token
}
