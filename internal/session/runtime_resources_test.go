package session

import (
	"errors"
	"net"
	"strings"
	"testing"
)

type recordingCloser struct {
	name  string
	order *[]string
	err   error
}

func (c recordingCloser) Close() error {
	*c.order = append(*c.order, c.name)
	return c.err
}

func TestSessionRuntimeClosesInReverseOrderOnlyOnce(t *testing.T) {
	var order []string
	runtime := newSessionRuntime()
	runtime.Add("gateway", recordingCloser{name: "gateway", order: &order})
	runtime.Add("bridge", recordingCloser{name: "bridge", order: &order})
	runtime.AddFunc("bindings", func() {
		order = append(order, "bindings")
	})

	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}

	const want = "bindings,bridge,gateway"
	if got := strings.Join(order, ","); got != want {
		t.Fatalf("close order = %q, want %q", got, want)
	}
}

func TestSessionRuntimeReportsAllNamedCloseErrors(t *testing.T) {
	firstErr := errors.New("first failure")
	secondErr := errors.New("second failure")
	var order []string
	runtime := newSessionRuntime()
	runtime.Add("first", recordingCloser{name: "first", order: &order, err: firstErr})
	runtime.Add("second", recordingCloser{name: "second", order: &order, err: secondErr})

	err := runtime.Close()
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("joined error = %v", err)
	}
	if !strings.Contains(err.Error(), "close first") ||
		!strings.Contains(err.Error(), "close second") {
		t.Fatalf("resource names missing from error: %v", err)
	}
	if got := strings.Join(order, ","); got != "second,first" {
		t.Fatalf("close order = %q, want %q", got, "second,first")
	}
}

func TestSessionRuntimeIgnoresAlreadyClosedNetworkResources(t *testing.T) {
	var order []string
	runtime := newSessionRuntime()
	runtime.Add("bridge", recordingCloser{
		name: "bridge", order: &order, err: net.ErrClosed,
	})

	if err := runtime.Close(); err != nil {
		t.Fatalf("already closed network resource returned an error: %v", err)
	}
}
