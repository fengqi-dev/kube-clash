//go:build e2e

package portfwd

import (
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/e2e/harness"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/session"
)

func TestMain(m *testing.M) { harness.RunMain(m) }

func TestTUNPortForwardServiceAndPod(t *testing.T) {
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

	tests := []struct {
		name     string
		kind     string
		target   string
		protocol string
		port     uint16
		want     string
	}{
		{
			name: "service-tcp", kind: portfwd.KindService, target: "echo",
			protocol: "tcp", port: 8080, want: "cluster-tcp:ping",
		},
		{
			name: "service-udp", kind: portfwd.KindService, target: "echo",
			protocol: "udp", port: 9090, want: "cluster-udp:ping",
		},
		{
			name: "pod-tcp", kind: portfwd.KindPod, target: podName,
			protocol: "tcp", port: 8080, want: "cluster-tcp:ping",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info, err := live.Manager.StartPortForwardSession(ctx, portfwd.Request{
				Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
				Kind: test.kind, Name: test.target,
				Protocol: test.protocol, RemotePort: test.port,
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
			var got string
			for {
				if test.protocol == "udp" {
					got, err = harness.DialLocalUDPEcho(info.Address, "ping")
				} else {
					got, err = harness.DialLocalEcho(info.Address, "ping")
				}
				if err == nil && got == test.want {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("response=%q err=%v, want %q", got, err, test.want)
				}
				time.Sleep(250 * time.Millisecond)
			}
			if info.LocalPort == 0 || info.Address == "" {
				t.Fatalf("unexpected forward info %#v", info)
			}
		})
	}

	if n := len(live.Manager.ListPortForwards()); n != 0 {
		t.Fatalf("expected no active forwards after stop, got %d", n)
	}
}
