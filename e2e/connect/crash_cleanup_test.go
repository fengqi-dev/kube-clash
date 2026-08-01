//go:build e2e

package connect

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/e2e/harness"
	"github.com/fengqi-dev/kube-loop/internal/helper"
	"github.com/fengqi-dev/kube-loop/internal/session"
)

func TestTUNCoreCrashCleansUpSessionAndDNS(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 6*time.Minute)
	defer cancel()

	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	clusterIP := harness.EchoServiceIP(t, ctx, client)
	fqdn := "echo." + harness.EchoNamespace + ".svc.cluster.local"

	_ = harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, nil)
	harness.WaitLookupIP(t, fqdn, clusterIP)

	helperClient, err := helper.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	response, err := helperClient.Ping(pingCtx)
	pingCancel()
	if err != nil {
		t.Fatal(err)
	}
	if response.PID < 1 {
		t.Fatalf("helper did not report an active sing-box PID: %+v", response)
	}
	process, err := os.FindProcess(response.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Kill(); err != nil {
		t.Fatalf("kill sing-box PID %d: %v", response.PID, err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		checkCtx, checkCancel := context.WithTimeout(context.Background(), 2*time.Second)
		status, pingErr := helperClient.Ping(checkCtx)
		checkCancel()
		if pingErr == nil && len(status.ActiveSessions) == 0 && status.PID == 0 {
			harness.AssertClusterDNSGone(t, fqdn, clusterIP)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("helper did not clean up the crashed sing-box session")
}
