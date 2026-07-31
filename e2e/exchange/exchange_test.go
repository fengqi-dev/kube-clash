//go:build e2e

package exchange

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fengqi-dev/kube-loop/e2e/harness"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/session"
)

func TestMain(m *testing.M) { harness.RunMain(m) }

func TestTUNServiceExchangeTCPAndUDP(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 6*time.Minute)
	defer cancel()

	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	service, err := client.CoreV1().Services(harness.EchoNamespace).Get(ctx, "echo", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	localTCP, localTCPAddr := harness.StartLocalTCPEcho(t, "local-tcp")
	defer localTCP.Close()
	localUDP, localUDPAddr := harness.StartLocalUDPEcho(t, "local-udp")
	defer localUDP.Close()

	live := harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, nil)

	_ = harness.WaitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 8080, "tcp", "ping", "cluster-tcp:")
	_ = harness.WaitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 9090, "udp", "ping", "cluster-udp:")
	harness.WaitHostTCP(t, service.Spec.ClusterIP, 8080, "ping", "cluster-tcp:")

	info, err := live.Manager.StartIntercept(ctx, intercept.Mapping{
		Namespace: harness.EchoNamespace,
		Service:   "echo",
		Ports: []intercept.PortMapping{
			{
				ServicePort: 8080, Protocol: "TCP",
				LocalHost: "127.0.0.1", LocalPort: localTCPAddr.Port,
			},
			{
				ServicePort: 9090, Protocol: "UDP",
				LocalHost: "127.0.0.1", LocalPort: localUDPAddr.Port,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = harness.WaitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 8080, "tcp", "ping", "local-tcp:")
	_ = harness.WaitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 9090, "udp", "ping", "local-udp:")
	harness.WaitHostTCP(t, service.Spec.ClusterIP, 8080, "ping", "local-tcp:")
	harness.WaitHostUDP(t, service.Spec.ClusterIP, 9090, "ping", "local-udp:")

	if err := live.Manager.StopIntercept(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	_ = harness.WaitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 8080, "tcp", "ping", "cluster-tcp:")
	_ = harness.WaitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 9090, "udp", "ping", "cluster-udp:")
	harness.WaitHostTCP(t, service.Spec.ClusterIP, 8080, "ping", "cluster-tcp:")
}

func TestTUNDisconnectRestoresExchange(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 6*time.Minute)
	defer cancel()

	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	service, err := client.CoreV1().Services(harness.EchoNamespace).Get(ctx, "echo", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	localTCP, localTCPAddr := harness.StartLocalTCPEcho(t, "local-tcp")
	defer localTCP.Close()

	live := harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, nil)

	_, err = live.Manager.StartIntercept(ctx, intercept.Mapping{
		Namespace: harness.EchoNamespace,
		Service:   "echo",
		Ports: []intercept.PortMapping{{
			ServicePort: 8080, Protocol: "TCP",
			LocalHost: "127.0.0.1", LocalPort: localTCPAddr.Port,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = harness.WaitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 8080, "tcp", "ping", "local-tcp:")

	if err := live.Manager.Disconnect(); err != nil {
		t.Fatal(err)
	}
	_ = harness.WaitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 8080, "tcp", "ping", "cluster-tcp:")
	harness.AssertHelperIdle(t)
}
