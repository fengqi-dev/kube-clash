//go:build e2e

package connect

import (
	"runtime"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/e2e/harness"
	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/session"
)

func TestMain(m *testing.M) { harness.RunMain(m) }

func TestTUNConnectClusterIP(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 1*time.Minute)
	defer cancel()

	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	clusterIP := harness.EchoServiceIP(t, ctx, client)

	live := harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, nil)
	if live.State.Network == nil {
		t.Fatal("connected session is missing network diagnostics")
	}
	if live.State.Network.RoutingMode != "native" {
		t.Fatalf("routing mode = %q, want native", live.State.Network.RoutingMode)
	}
	wantStrictRoute := runtime.GOOS != "windows"
	if live.State.Network.StrictRoute != wantStrictRoute {
		t.Fatalf(
			"strict route = %v, want %v on %s",
			live.State.Network.StrictRoute, wantStrictRoute, runtime.GOOS,
		)
	}

	harness.WaitHostTCP(t, clusterIP, 8080, "ping", "cluster-tcp:")
	harness.WaitHostUDP(t, clusterIP, 9090, "ping", "cluster-udp:")
}

func TestTUNConnectPodIP(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 1*time.Minute)
	defer cancel()

	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	clusterIP := harness.EchoServiceIP(t, ctx, client)
	_, podIP := harness.EchoPodIP(t, ctx, client)

	_ = harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, nil)

	harness.WaitHostTCP(t, clusterIP, 8080, "ping", "cluster-tcp:")
	harness.RequireRoutedViaKubeLoop(t, podIP, clusterIP)
	harness.WaitHostTCP(t, podIP, 8080, "ping", "cluster-tcp:")
	harness.WaitHostUDP(t, podIP, 9090, "ping", "cluster-udp:")
}

func TestTUNConnectManualNetwork(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 1*time.Minute)
	defer cancel()

	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	clusterIP := harness.EchoServiceIP(t, ctx, client)

	discovery, err := provider.Discover(ctx, harness.KubeContext(), []string{harness.EchoNamespace})
	if err != nil {
		t.Fatal(err)
	}
	if discovery.DNSServer == "" || len(discovery.ServiceCIDRs) == 0 {
		t.Fatalf("discovery incomplete: %#v", discovery)
	}

	live := harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, func(manager *session.Manager) {
		if err := manager.SetManualNetwork(harness.KubeContext(), cluster.ManualNetwork{
			PodCIDRs:       discovery.PodCIDRs,
			ServiceCIDRs:   discovery.ServiceCIDRs,
			DNSServer:      discovery.DNSServer,
			ClusterDomains: discovery.ClusterDomains,
			DNSNamespace:   harness.EchoNamespace,
		}); err != nil {
			t.Fatal(err)
		}
	})

	harness.WaitHostTCP(t, clusterIP, 8080, "manual", "cluster-tcp:")
	dnsPort, err := live.Manager.InternalDNSPort()
	if err != nil {
		t.Fatal(err)
	}
	harness.WaitDNSA(
		t,
		dnsPort,
		"echo."+harness.EchoNamespace+".svc.cluster.local",
		clusterIP,
	)
}

func TestTUNDisconnectTearsDown(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 1*time.Minute)
	defer cancel()

	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	clusterIP := harness.EchoServiceIP(t, ctx, client)

	live := harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, nil)
	fqdn := "echo." + harness.EchoNamespace + ".svc.cluster.local"
	harness.WaitHostTCP(t, clusterIP, 8080, "ping", "cluster-tcp:")
	harness.WaitLookupIP(t, fqdn, clusterIP)

	if err := live.Manager.Disconnect(); err != nil {
		t.Fatal(err)
	}
	harness.AssertHelperIdle(t)
	harness.AssertClusterDNSGone(t, fqdn, clusterIP)
}
