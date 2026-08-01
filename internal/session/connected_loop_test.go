package session

import (
	"context"
	"io"
	"log"
	"net"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/gateway"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

type replacementGatewayProvider struct {
	*fakeProvider
	gateway   cluster.GatewayInfo
	forwarder cluster.PortForward
}

func (p *replacementGatewayProvider) GetGateway(
	context.Context, string,
) (cluster.GatewayInfo, error) {
	return p.gateway, nil
}

func (p *replacementGatewayProvider) StartPortForward(
	context.Context, string, string, uint16,
) (cluster.PortForward, error) {
	return p.forwarder, nil
}

type gatewayAddressRecorder struct {
	address string
}

func (r *gatewayAddressRecorder) SetGatewayAddress(address string) {
	r.address = address
}

func TestControlRecoveryDelay(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 0},
		{attempt: 1, want: time.Second},
		{attempt: 2, want: time.Second},
		{attempt: 3, want: 2 * time.Second},
		{attempt: 4, want: 2 * time.Second},
	}
	for _, test := range tests {
		if got := controlRecoveryDelay(test.attempt); got != test.want {
			t.Fatalf("attempt %d delay = %s, want %s", test.attempt, got, test.want)
		}
	}
}

func TestUpdateCoreMetricsPublishesOnlyWhileConnected(t *testing.T) {
	manager := NewManager(&fakeProvider{})
	process := &fakeProcess{done: make(chan struct{})}
	published := make(chan *singbox.Metrics, 1)
	manager.SubscribeMetrics(func(metrics *singbox.Metrics) {
		published <- metrics
	})

	manager.publish(State{Phase: PhaseConnected, Message: "connected"})
	manager.updateCoreMetrics(context.Background(), process)
	select {
	case metrics := <-published:
		if metrics == nil {
			t.Fatal("published nil metrics")
		}
	case <-time.After(time.Second):
		t.Fatal("connected metrics were not published")
	}

	manager.publish(State{Phase: PhaseIdle, Message: "disconnected"})
	manager.updateCoreMetrics(context.Background(), process)
	select {
	case metrics := <-published:
		t.Fatalf("published metrics while disconnected: %#v", metrics)
	default:
	}
}

func TestReplaceGatewayPortForwardUpdatesControlAndBridge(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	firstServer := gateway.NewServer(log.New(io.Discard, "", 0), time.Second)
	go func() { _ = firstServer.Serve(first) }()

	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	secondServer := gateway.NewServer(log.New(io.Discard, "", 0), time.Second)
	go func() { _ = secondServer.Serve(second) }()

	provider := &replacementGatewayProvider{
		fakeProvider: &fakeProvider{},
		gateway:      cluster.GatewayInfo{Name: "gateway-new", IP: "10.244.0.9"},
		forwarder: &fakeForwarder{
			address: second.Addr().String(),
		},
	}
	manager := NewManager(provider)
	if err := manager.intercept.Start(
		context.Background(), "minikube", "10.244.0.8", first.Addr().String(),
	); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.intercept.StopAll(context.Background()) }()

	runtime := newSessionRuntime()
	defer func() { _ = runtime.Close() }()
	bridge := &gatewayAddressRecorder{}
	if err := manager.replaceGatewayPortForward(
		context.Background(), "minikube", bridge, runtime,
	); err != nil {
		t.Fatal(err)
	}
	if bridge.address != second.Addr().String() {
		t.Fatalf("bridge address = %q, want %q", bridge.address, second.Addr())
	}
}
