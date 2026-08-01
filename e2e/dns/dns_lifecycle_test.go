//go:build e2e

package dns

import (
	"sync"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/e2e/harness"
	"github.com/fengqi-dev/kube-loop/internal/session"
)

func TestTUNDNSUpdateDuringDisconnectCleansUp(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 1*time.Minute)
	defer cancel()

	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	clusterIP := harness.EchoServiceIP(t, ctx, client)
	fqdn := "echo." + harness.EchoNamespace + ".svc.cluster.local"

	live := harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, nil)
	harness.WaitLookupIP(t, fqdn, clusterIP)

	stopUpdates := make(chan struct{})
	var updates sync.WaitGroup
	updates.Add(1)
	go func() {
		defer updates.Done()
		namespaces := []string{"default", harness.EchoNamespace}
		for index := 0; ; index++ {
			select {
			case <-stopUpdates:
				return
			default:
			}
			// An update losing the race with Disconnect is expected to fail.
			_ = live.Manager.SetDNSNamespace(
				harness.KubeContext(),
				namespaces[index%len(namespaces)],
			)
		}
	}()

	time.Sleep(100 * time.Millisecond)
	if err := live.Manager.Disconnect(); err != nil {
		close(stopUpdates)
		updates.Wait()
		t.Fatal(err)
	}
	close(stopUpdates)
	updates.Wait()

	harness.AssertHelperIdle(t)
	harness.AssertClusterDNSGone(t, fqdn, clusterIP)
}
