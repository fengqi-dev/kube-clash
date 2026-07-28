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
