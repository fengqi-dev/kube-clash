//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fengqi-dev/kube-loop/internal/intercept"
)

func TestPreviewExposesLocalTCPAndUDP(t *testing.T) {
	requireE2E(t)
	ctx, cancel := testContext(t, 5*time.Minute)
	defer cancel()

	provider := newProvider(t)
	gateway, forwarder := ensureGateway(t, ctx, provider)
	client := kubeClient(t, provider)
	if err := ensureEchoNamespace(ctx, client); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.CoreV1().Services(echoNamespace).Delete(
			context.Background(), "preview-local", metav1.DeleteOptions{},
		)
	})

	localTCP, localTCPAddr := startLocalTCPEcho(t, "preview-tcp")
	defer localTCP.Close()
	localUDP, localUDPAddr := startLocalUDPEcho(t, "preview-udp")
	defer localUDP.Close()

	manager := intercept.NewManager(provider)
	if err := manager.Start(ctx, kubeContext(), gateway.IP, forwarder.Address()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.StopAll(context.Background()) }()

	info, err := manager.StartPreview(ctx, intercept.PreviewRequest{
		Namespace: echoNamespace,
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

	_ = waitClusterProbe(t, ctx, client, info.ClusterIP, 8080, "tcp", "ping", "preview-tcp:")
	_ = waitClusterProbe(t, ctx, client, info.ClusterIP, 9090, "udp", "ping", "preview-udp:")

	if err := manager.Stop(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	_, err = client.CoreV1().Services(echoNamespace).Get(ctx, "preview-local", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected service deleted, got %v", err)
	}
}
