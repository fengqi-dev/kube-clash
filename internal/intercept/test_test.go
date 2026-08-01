package intercept

import (
	"context"
	"net"
	"strconv"
	"testing"
)

func TestConnectivityDialsEveryLocalTCPMapping(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	port := func(listener net.Listener) int {
		_, raw, splitErr := net.SplitHostPort(listener.Addr().String())
		if splitErr != nil {
			t.Fatal(splitErr)
		}
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		return value
	}
	manager := NewManager(nil)
	manager.registry.add(&runtimeIntercept{info: Info{
		ID: "intercept-1",
		Locals: []PortMapping{
			{Protocol: "tcp", LocalHost: "127.0.0.1", LocalPort: port(first)},
			{Protocol: "tcp", LocalHost: "0.0.0.0", LocalPort: port(second)},
		},
	}})

	if err := manager.Test(context.Background(), "intercept-1"); err != nil {
		t.Fatal(err)
	}
}

func TestConnectivityRejectsMissingIntercept(t *testing.T) {
	manager := NewManager(nil)
	if err := manager.Test(context.Background(), "missing"); err == nil {
		t.Fatal("expected missing session error")
	}
}

func TestControlConnectivityReportsUnavailableGateway(t *testing.T) {
	manager := NewManager(nil)
	manager.registry.add(&runtimeIntercept{
		info:     Info{ID: "intercept-1"},
		portKeys: map[string]PortMapping{"intercept-1/tcp/80": {}},
	})
	if err := manager.TestControl("intercept-1"); err == nil {
		t.Fatal("expected unavailable Gateway control error")
	}
}

func TestConnectivityRejectsUDPIntercept(t *testing.T) {
	manager := NewManager(nil)
	manager.registry.add(&runtimeIntercept{info: Info{
		ID: "intercept-1",
		Locals: []PortMapping{
			{Protocol: "udp", LocalHost: "127.0.0.1", LocalPort: 12345},
		},
	}})
	if err := manager.Test(context.Background(), "intercept-1"); err == nil {
		t.Fatal("expected unsupported UDP test error")
	}
}
