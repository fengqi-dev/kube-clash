package session

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-clash/internal/cluster"
	"github.com/fengqi-dev/kube-clash/internal/mihomo"
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
) (mihomo.RunningCore, error) {
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
func (f *fakeProcess) Snapshot(context.Context) (mihomo.Metrics, error) {
	return mihomo.Metrics{}, nil
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
