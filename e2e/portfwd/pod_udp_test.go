//go:build e2e

package portfwd

import (
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/e2e/harness"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/session"
)

func TestTUNPortForwardPodUDP(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 6*time.Minute)
	defer cancel()

	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	podName, _ := harness.EchoPodIP(t, ctx, client)

	live := harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, nil)
	info, err := live.Manager.StartPortForwardSession(ctx, portfwd.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
		Kind: portfwd.KindPod, Name: podName,
		Protocol: "udp", RemotePort: 9090,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := live.Manager.StopPortForward(info.ID); err != nil {
			t.Error(err)
		}
	}()

	deadline := time.Now().Add(30 * time.Second)
	for {
		got, probeErr := harness.DialLocalUDPEcho(info.Address, "ping")
		if probeErr == nil && got == "cluster-udp:ping" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("response=%q err=%v, want cluster-udp:ping", got, probeErr)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
