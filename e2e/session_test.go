//go:build e2e

package e2e

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/net/proxy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

func TestSessionConnectSOCKSDataPath(t *testing.T) {
	requireE2E(t)
	ctx, cancel := testContext(t, 3*time.Minute)
	defer cancel()

	provider := newProvider(t)
	core := &recordingCore{process: &recordingProcess{done: make(chan struct{})}}
	manager := session.NewManager(
		provider,
		session.WithCore(core),
		session.WithGatewayImage(gatewayImage()),
	)
	connected := make(chan session.State, 1)
	failed := make(chan session.State, 1)
	manager.Subscribe(func(state session.State) {
		switch state.Phase {
		case session.PhaseConnected:
			connected <- state
		case session.PhaseError:
			failed <- state
		}
	})

	if err := manager.Connect(ctx, session.Request{
		Context: kubeContext(), Namespace: "default",
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Disconnect() })

	var state session.State
	select {
	case state = <-connected:
	case state = <-failed:
		t.Fatalf("session failed: %s", state)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if state.Discovery == nil {
		t.Fatal("connected session has no cluster discovery")
	}

	client := kubeClient(t, provider)
	service, err := client.CoreV1().Services("default").Get(ctx, "kubernetes", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dialer, err := proxy.SOCKS5("tcp", core.bridgeAddress, nil, proxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := dialer.Dial("tcp", net.JoinHostPort(service.Spec.ClusterIP, "443"))
	if err != nil {
		t.Fatalf("connect ClusterIP through session SOCKS bridge: %v", err)
	}
	_ = connection.Close()
}

type recordingCore struct {
	process       *recordingProcess
	bridgeAddress string
}

func (r *recordingCore) Start(
	_ context.Context, _ cluster.Discovery, bridgeAddress string, _ string, _ []singbox.HostAlias,
) (singbox.RunningCore, error) {
	r.bridgeAddress = bridgeAddress
	return r.process, nil
}

type recordingProcess struct {
	done chan struct{}
}

func (r *recordingProcess) Done() <-chan struct{} { return r.done }
func (r *recordingProcess) Err() error            { return nil }
func (r *recordingProcess) Snapshot(context.Context) (singbox.Metrics, error) {
	return singbox.Metrics{Connections: []singbox.Connection{}}, nil
}
func (r *recordingProcess) TrafficEndpoints() singbox.TrafficEndpoints {
	endpoint := singbox.TrafficEndpoint{
		Address: "127.0.0.1:18080", Username: "test-user", Password: "test-password",
	}
	return singbox.TrafficEndpoints{
		PortForward: endpoint, Exchange: endpoint, Preview: endpoint,
		MirrorPrimary: endpoint, MirrorShadow: endpoint,
	}
}
func (r *recordingProcess) Close() error {
	select {
	case <-r.done:
	default:
		close(r.done)
	}
	return nil
}
