//go:build integration

package session

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
	"golang.org/x/net/proxy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func TestMinikubeSessionConnectsSOCKSDataPath(t *testing.T) {
	if os.Getenv("KUBELOOP_MINIKUBE_TEST") != "1" {
		t.Skip("set KUBELOOP_MINIKUBE_TEST=1 to run against the local minikube context")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	provider := cluster.NewProvider()
	core := &recordingCore{process: &recordingProcess{done: make(chan struct{})}}
	manager := NewManager(
		provider,
		WithCore(core),
		WithGatewayImage("kube-loop-gateway:dev"),
	)
	connected := make(chan State, 1)
	failed := make(chan State, 1)
	manager.Subscribe(func(state State) {
		switch state.Phase {
		case PhaseConnected:
			connected <- state
		case PhaseError:
			failed <- state
		}
	})
	if err := manager.Connect(ctx, Request{Context: "minikube", Namespace: "default"}); err != nil {
		t.Fatal(err)
	}

	var state State
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

	config, err := provider.RESTConfig("minikube")
	if err != nil {
		t.Fatal(err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	service, err := client.CoreV1().Services("default").Get(
		ctx, "kubernetes", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	dialer, err := proxy.SOCKS5("tcp", core.bridgeAddress, nil, proxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := dialer.Dial(
		"tcp", net.JoinHostPort(service.Spec.ClusterIP, "443"),
	)
	if err != nil {
		t.Fatalf("connect to Kubernetes ClusterIP through session SOCKS bridge: %v", err)
	}
	connection.Close()

	if err := manager.Disconnect(); err != nil {
		t.Fatal(err)
	}
}

type recordingCore struct {
	process       *recordingProcess
	bridgeAddress string
}

func (r *recordingCore) Start(
	_ context.Context,
	_ cluster.Discovery,
	bridgeAddress string,
	_ string,
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
func (r *recordingProcess) Close() error {
	select {
	case <-r.done:
	default:
		close(r.done)
	}
	return nil
}
