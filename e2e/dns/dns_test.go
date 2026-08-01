//go:build e2e

package dns

import (
	"testing"
	"time"

	"github.com/miekg/dns"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fengqi-dev/kube-loop/e2e/harness"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/store"
)

func TestMain(m *testing.M) { harness.RunMain(m) }

func TestTUNDNSResolution(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 1*time.Minute)
	defer cancel()

	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	clusterIP := harness.EchoServiceIP(t, ctx, client)
	aliasDomain := "echo.kubeloop-e2e.test"

	live := harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, func(manager *session.Manager) {
		if err := manager.SetHostAliases(harness.KubeContext(), []store.HostAliasSpec{
			{Domain: aliasDomain, IP: clusterIP},
		}); err != nil {
			t.Fatal(err)
		}
	})

	port, err := live.Manager.DNSPort()
	if err != nil {
		t.Fatal(err)
	}

	fqdn := "echo." + harness.EchoNamespace + ".svc.cluster.local"
	svcForm := "echo." + harness.EchoNamespace + ".svc"
	nsForm := "echo." + harness.EchoNamespace

	t.Run("fqdn-a-udp", func(t *testing.T) {
		harness.WaitDNSA(t, port, fqdn, clusterIP)
	})
	t.Run("svc-relative-a", func(t *testing.T) {
		harness.WaitDNSA(t, port, svcForm, clusterIP)
	})
	t.Run("ns-relative-a", func(t *testing.T) {
		harness.WaitDNSA(t, port, nsForm, clusterIP)
	})
	t.Run("short-name-a", func(t *testing.T) {
		harness.WaitDNSA(t, port, "echo", clusterIP)
	})
	t.Run("host-alias-a", func(t *testing.T) {
		harness.WaitDNSA(t, port, aliasDomain, clusterIP)
	})
	t.Run("fqdn-a-tcp", func(t *testing.T) {
		harness.WaitDNSTCPA(t, port, fqdn, clusterIP)
	})
	t.Run("short-name-a-tcp", func(t *testing.T) {
		harness.WaitDNSTCPA(t, port, "echo", clusterIP)
	})
	t.Run("kubernetes-api-a", func(t *testing.T) {
		apiService, err := client.CoreV1().Services("default").Get(ctx, "kubernetes", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		harness.WaitDNSA(t, port, "kubernetes.default.svc.cluster.local", apiService.Spec.ClusterIP)
		harness.WaitDNSA(t, port, "kubernetes.default.svc", apiService.Spec.ClusterIP)
	})
	t.Run("nxdomain-missing-service", func(t *testing.T) {
		harness.WaitDNSNXDOMAIN(t, port, "no-such-service."+harness.EchoNamespace+".svc.cluster.local")
	})
	t.Run("os-resolver-fqdn", func(t *testing.T) {
		harness.WaitLookupIP(t, fqdn, clusterIP)
	})
	t.Run("os-resolver-short-name", func(t *testing.T) {
		harness.WaitLookupIP(t, "echo", clusterIP)
	})
	t.Run("dial-by-fqdn", func(t *testing.T) {
		harness.WaitHostTCP(t, fqdn, 8080, "dns", "cluster-tcp:")
	})

	t.Run("set-dns-namespace", func(t *testing.T) {
		if err := live.Manager.SetDNSNamespace(harness.KubeContext(), "default"); err != nil {
			t.Fatal(err)
		}
		harness.AssertDNSNoA(t, port, "echo", clusterIP)
		harness.WaitDNSA(t, port, fqdn, clusterIP)
		harness.WaitDNSA(t, port, aliasDomain, clusterIP)

		if err := live.Manager.SetDNSNamespace(harness.KubeContext(), harness.EchoNamespace); err != nil {
			t.Fatal(err)
		}
		harness.WaitDNSA(t, port, "echo", clusterIP)
	})
}

func TestTUNDNSGoneAfterDisconnect(t *testing.T) {
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
	port, err := live.Manager.DNSPort()
	if err != nil {
		t.Fatal(err)
	}
	fqdn := "echo." + harness.EchoNamespace + ".svc.cluster.local"
	harness.WaitDNSA(t, port, fqdn, clusterIP)
	harness.WaitLookupIP(t, fqdn, clusterIP)

	if err := live.Manager.Disconnect(); err != nil {
		t.Fatal(err)
	}
	harness.AssertHelperIdle(t)
	harness.AssertClusterDNSGone(t, fqdn, clusterIP)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		_, err := harness.ExchangeDNS(port, "udp", fqdn, dns.TypeA)
		if err != nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("DNS proxy on :%d still answering after disconnect", port)
}
