//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fengqi-dev/kube-loop/internal/intercept"
)

func TestServiceInterceptTCPAndUDP(t *testing.T) {
	requireE2E(t)
	ctx, cancel := testContext(t, 5*time.Minute)
	defer cancel()

	provider := newProvider(t)
	gateway, forwarder := ensureGateway(t, ctx, provider)
	client := kubeClient(t, provider)

	if err := ensureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	service, err := client.CoreV1().Services(echoNamespace).Get(ctx, "echo", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	localTCP, localTCPAddr := startLocalTCPEcho(t, "local-tcp")
	defer localTCP.Close()
	localUDP, localUDPAddr := startLocalUDPEcho(t, "local-udp")
	defer localUDP.Close()

	manager := intercept.NewManager(provider)
	if err := manager.Start(ctx, kubeContext(), gateway.IP, forwarder.Address()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.StopAll(context.Background()) }()

	_ = waitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 8080, "tcp", "ping", "cluster-tcp:")
	_ = waitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 9090, "udp", "ping", "cluster-udp:")

	info, err := manager.StartIntercept(ctx, intercept.Mapping{
		Namespace: echoNamespace,
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

	_ = waitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 8080, "tcp", "ping", "local-tcp:")
	_ = waitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 9090, "udp", "ping", "local-udp:")

	if err := manager.Stop(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	_ = waitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 8080, "tcp", "ping", "cluster-tcp:")
	_ = waitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 9090, "udp", "ping", "cluster-udp:")
}
