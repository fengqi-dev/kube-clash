package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

type fakeProvider struct {
	discovery cluster.Discovery
	err       error
	forwarder *fakeForwarder
}

func (f *fakeProvider) Contexts() ([]cluster.ContextInfo, error) { return nil, nil }
func (f *fakeProvider) Namespaces(context.Context, string) ([]string, error) {
	return []string{"default"}, nil
}
func (f *fakeProvider) Discover(context.Context, string) (cluster.Discovery, error) {
	return f.discovery, f.err
}
func (f *fakeProvider) WatchInventory(
	context.Context, string, func(cluster.InventorySnapshot),
) (io.Closer, error) {
	return closerFunc(func() {}), f.err
}
func (f *fakeProvider) EnsureGateway(context.Context, string, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "gateway-pod", nil
}
func (f *fakeProvider) StartPortForward(
	context.Context, string, string, uint16,
) (cluster.PortForward, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.forwarder == nil {
		f.forwarder = &fakeForwarder{}
	}
	return f.forwarder, nil
}

type fakeForwarder struct {
	mu     sync.Mutex
	closed bool
}

func (f *fakeForwarder) Address() string { return "127.0.0.1:12345" }
func (f *fakeForwarder) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

type fakeCore struct {
	process *fakeProcess
	started chan struct{}
}

func newFakeCore() *fakeCore {
	return &fakeCore{process: &fakeProcess{done: make(chan struct{})}, started: make(chan struct{})}
}

func (f *fakeCore) Start(
	context.Context, cluster.Discovery, string, string,
) (singbox.RunningCore, error) {
	close(f.started)
	return f.process, nil
}

type fakeProcess struct {
	once sync.Once
	done chan struct{}
	err  error
}

func (f *fakeProcess) Done() <-chan struct{} { return f.done }
func (f *fakeProcess) Err() error            { return f.err }
func (f *fakeProcess) Snapshot(context.Context) (singbox.Metrics, error) {
	return singbox.Metrics{Connections: []singbox.Connection{}}, nil
}
func (f *fakeProcess) Close() error {
	f.once.Do(func() { close(f.done) })
	return nil
}

func testBridge(context.Context, string) (net.Listener, error) {
	return &fakeListener{}, nil
}

type fakeListener struct{}

func (f *fakeListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (f *fakeListener) Close() error              { return nil }
func (f *fakeListener) Addr() net.Addr            { return fakeAddress("127.0.0.1:23456") }

type fakeAddress string

func (f fakeAddress) Network() string { return "tcp" }
func (f fakeAddress) String() string  { return string(f) }

func TestManagerPublishesConnectedStateAndCleansUp(t *testing.T) {
	want := cluster.Discovery{
		PodCIDRs: []string{"10.244.0.0/16"}, ServiceIPs: []string{"10.96.0.1"},
	}
	provider := &fakeProvider{discovery: want}
	core := newFakeCore()
	manager := NewManager(
		provider, WithCore(core), WithBridgeFactory(testBridge), WithGatewayImage("gateway:test"),
	)
	connected := make(chan State, 1)
	manager.Subscribe(func(state State) {
		if state.Phase == PhaseConnected {
			connected <- state
		}
	})
	if err := manager.Connect(context.Background(), Request{Context: "dev"}); err != nil {
		t.Fatal(err)
	}
	var state State
	select {
	case state = <-connected:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for connected state; current state: %#v", manager.State())
	}
	if state.Discovery == nil || len(state.Discovery.PodCIDRs) != 1 {
		t.Fatalf("unexpected discovery: %#v", state.Discovery)
	}
	if state.ConnectedAt == nil {
		t.Fatal("connected state does not include connection time")
	}
	if err := manager.Disconnect(); err != nil {
		t.Fatal(err)
	}
	provider.forwarder.mu.Lock()
	closed := provider.forwarder.closed
	provider.forwarder.mu.Unlock()
	if !closed {
		t.Fatal("port-forward was not closed")
	}
	if manager.State().Phase != PhaseIdle {
		t.Fatalf("unexpected state after disconnect: %s", manager.State().Phase)
	}
}

func TestManagerPublishesGatewayError(t *testing.T) {
	manager := NewManager(
		&fakeProvider{err: errors.New("forbidden")},
		WithCore(newFakeCore()),
		WithBridgeFactory(testBridge),
	)
	failed := make(chan State, 1)
	manager.Subscribe(func(state State) {
		if state.Phase == PhaseError {
			failed <- state
		}
	})
	if err := manager.Connect(context.Background(), Request{Context: "dev"}); err != nil {
		t.Fatal(err)
	}
	state := receiveState(t, failed)
	if state.Error != "forbidden" || state.Message != "无法安装集群 Gateway" {
		t.Fatalf("unexpected error state: %#v", state)
	}
}

func TestManagerRejectsSecondConnection(t *testing.T) {
	manager := NewManager(
		&fakeProvider{discovery: cluster.Discovery{
			PodCIDRs: []string{"10.244.0.0/16"}, ServiceIPs: []string{"10.96.0.1"},
		}},
		WithCore(newFakeCore()),
		WithBridgeFactory(testBridge),
	)
	if err := manager.Connect(context.Background(), Request{Context: "dev"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Connect(context.Background(), Request{Context: "dev"}); err == nil {
		t.Fatal("expected second connection to be rejected")
	}
	if err := manager.Disconnect(); err != nil {
		t.Fatal(err)
	}
}

func receiveState(t *testing.T, states <-chan State) State {
	t.Helper()
	select {
	case state := <-states:
		return state
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for session state")
		return State{}
	}
}

func TestRetainMetricsKeepsRecentConnections(t *testing.T) {
	manager := NewManager(&fakeProvider{})
	first := manager.retainMetrics(singbox.Metrics{
		DownloadTotal: 100,
		Connections: []singbox.Connection{{
			ID:          "conn-1",
			Network:     "tcp",
			Destination: "10.96.0.1:443",
			Download:    50,
		}},
	})
	if len(first.Connections) != 1 {
		t.Fatalf("expected 1 live connection, got %d", len(first.Connections))
	}

	retained := manager.retainMetrics(singbox.Metrics{
		DownloadTotal: 200,
		Connections:   nil,
	})
	if retained.DownloadTotal != 200 {
		t.Fatalf("download total = %d, want 200", retained.DownloadTotal)
	}
	if len(retained.Connections) != 1 || retained.Connections[0].ID != "conn-1" {
		t.Fatalf("expected retained connection, got %#v", retained.Connections)
	}

	manager.clearRecentConnections()
	cleared := manager.retainMetrics(singbox.Metrics{Connections: nil})
	if len(cleared.Connections) != 0 {
		t.Fatalf("expected no connections after clear, got %#v", cleared.Connections)
	}
}

func TestRetainMetricsCapsPublishedConnections(t *testing.T) {
	manager := NewManager(&fakeProvider{})
	connections := make([]singbox.Connection, 250)
	for i := range connections {
		connections[i] = singbox.Connection{
			ID:          fmt.Sprintf("conn-%d", i),
			Network:     "tcp",
			Destination: "10.96.0.1:80",
			Download:    int64(i),
		}
	}
	metrics := manager.retainMetrics(singbox.Metrics{Connections: connections})
	if len(metrics.Connections) != maxPublishedConnections {
		t.Fatalf("published = %d, want %d", len(metrics.Connections), maxPublishedConnections)
	}
	if metrics.Connections[0].Download < metrics.Connections[len(metrics.Connections)-1].Download {
		t.Fatalf("expected higher-traffic connections first, got first=%d last=%d",
			metrics.Connections[0].Download, metrics.Connections[len(metrics.Connections)-1].Download)
	}
}
