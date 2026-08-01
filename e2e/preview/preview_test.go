//go:build e2e

package preview

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fengqi-dev/kube-loop/e2e/harness"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/session"
)

func TestMain(m *testing.M) { harness.RunMain(m) }

func TestTUNPreviewExposesLocalTCPAndUDP(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 6*time.Minute)
	defer cancel()

	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoNamespace(ctx, client); err != nil {
		t.Fatal(err)
	}
	// Prior canceled CI runs can leave this Service behind and break restore.
	_ = client.CoreV1().Services(harness.EchoNamespace).Delete(
		ctx, "preview-local", metav1.DeleteOptions{},
	)
	t.Cleanup(func() {
		_ = client.CoreV1().Services(harness.EchoNamespace).Delete(
			context.Background(), "preview-local", metav1.DeleteOptions{},
		)
	})

	localTCP, localTCPAddr := harness.StartLocalTCPEcho(t, "preview-tcp")
	defer localTCP.Close()
	localUDP, localUDPAddr := harness.StartLocalUDPEcho(t, "preview-udp")
	defer localUDP.Close()

	live := harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, nil)

	info, err := live.Manager.StartPreview(ctx, intercept.PreviewRequest{
		Namespace: harness.EchoNamespace,
		Name:      "preview-local",
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
	if info.ClusterIP == "" {
		t.Fatal("expected cluster IP")
	}
	gateway, err := live.Provider.GetGateway(ctx, harness.KubeContext())
	if err != nil {
		t.Fatal(err)
	}
	udpListenPort := harness.InterceptListenPort(t, info.Ports, 9090, corev1.ProtocolUDP)

	_ = harness.WaitClusterProbe(t, ctx, client, info.ClusterIP, 8080, "tcp", "ping", "preview-tcp:")
	_ = harness.WaitClusterProbe(
		t, ctx, client, gateway.IP, udpListenPort, "udp", "ping", "preview-udp:",
	)
	harness.WaitHostTCP(t, info.ClusterIP, 8080, "ping", "preview-tcp:")
	harness.WaitHostUDP(t, info.ClusterIP, 9090, "ping", "preview-udp:")
	if _, err := harness.WaitClusterProbeOptional(
		ctx, client, info.ClusterIP, 9090, "udp", "ping", "preview-udp:", 45*time.Second,
	); err != nil {
		t.Logf("cluster Service UDP probe after preview: %v", err)
	}

	if err := live.Manager.StopPreview(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	_, err = client.CoreV1().Services(harness.EchoNamespace).Get(ctx, "preview-local", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected service deleted, got %v", err)
	}
}
