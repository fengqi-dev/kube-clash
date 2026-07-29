//go:build e2e

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

const (
	defaultContext = "minikube"
	defaultImage   = "kube-loop-gateway:dev"
	echoNamespace  = "kubeloop-e2e"
)

func requireE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("KUBELOOP_E2E") != "1" {
		t.Skip("set KUBELOOP_E2E=1 to run Minikube end-to-end tests")
	}
}

func kubeContext() string {
	if value := os.Getenv("KUBELOOP_E2E_CONTEXT"); value != "" {
		return value
	}
	return defaultContext
}

func gatewayImage() string {
	if value := os.Getenv("KUBELOOP_GATEWAY_IMAGE"); value != "" {
		return value
	}
	return defaultImage
}

func newProvider(t *testing.T) *cluster.Provider {
	t.Helper()
	return cluster.NewProvider()
}

func kubeClient(t *testing.T, provider *cluster.Provider) kubernetes.Interface {
	t.Helper()
	cfg, err := provider.RESTConfig(kubeContext())
	if err != nil {
		t.Fatal(err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func testContext(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), timeout)
}

func ensureGateway(
	t *testing.T, ctx context.Context, provider *cluster.Provider,
) (cluster.GatewayInfo, cluster.PortForward) {
	t.Helper()
	gateway, err := provider.EnsureGateway(ctx, kubeContext(), gatewayImage())
	if err != nil {
		t.Fatalf("ensure gateway: %v", err)
	}
	forwarder, err := provider.StartPortForward(ctx, kubeContext(), gateway.Name, cluster.GatewayPort)
	if err != nil {
		t.Fatalf("gateway port-forward: %v", err)
	}
	t.Cleanup(func() {
		if err := forwarder.Close(); err != nil {
			t.Logf("close gateway port-forward: %v", err)
		}
	})
	return gateway, forwarder
}
