//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/fengqi-dev/kube-loop/internal/intercept"
)

func TestServiceMirrorTCPAndUDP(t *testing.T) {
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

	tcpMirrored := make(chan string, 8)
	localTCP, localTCPAddr := startLocalTCPCapture(t, tcpMirrored)
	defer localTCP.Close()
	udpMirrored := make(chan string, 8)
	localUDP, localUDPAddr := startLocalUDPCapture(t, udpMirrored)
	defer localUDP.Close()

	manager := intercept.NewManager(provider)
	if err := manager.Start(ctx, kubeContext(), gateway.IP, forwarder.Address()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.StopAll(context.Background()) }()

	_ = waitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 8080, "tcp", "ping", "cluster-tcp:")
	_ = waitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 9090, "udp", "ping", "cluster-udp:")

	info, err := manager.StartMirror(ctx, intercept.Mapping{
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
	if info.Mode != intercept.ModeMirror {
		t.Fatalf("mode=%q, want mirror", info.Mode)
	}
	if len(manager.List()) != 0 || len(manager.ListMirrors()) != 1 {
		t.Fatalf("list exchange=%d mirror=%d", len(manager.List()), len(manager.ListMirrors()))
	}

	// ClusterIP may briefly still hit the Pod before kube-proxy picks up the
	// Gateway EndpointSlice. Retry until a probe both returns the primary
	// response and delivers a request copy to the local mirror listener.
	if err := waitMirrorActive(ctx, client, service.Spec.ClusterIP, 8080, "tcp", "cluster-tcp:", tcpMirrored); err != nil {
		t.Fatal(err)
	}
	if err := waitMirrorActive(ctx, client, service.Spec.ClusterIP, 9090, "udp", "cluster-udp:", udpMirrored); err != nil {
		t.Fatal(err)
	}

	if err := manager.Stop(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	if len(manager.ListMirrors()) != 0 {
		t.Fatalf("expected no mirrors after stop, got %d", len(manager.ListMirrors()))
	}
	_ = waitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 8080, "tcp", "ping", "cluster-tcp:")
	_ = waitClusterProbe(t, ctx, client, service.Spec.ClusterIP, 9090, "udp", "ping", "cluster-udp:")
}

func waitMirrorActive(
	ctx context.Context,
	client kubernetes.Interface,
	clusterIP string,
	port int,
	protocol, prefix string,
	mirrored <-chan string,
) error {
	payload := "mirror-ping"
	wantProbe := prefix + payload
	deadline := time.Now().Add(90 * time.Second)
	var lastProbe string
	var lastProbeErr error
	for time.Now().Before(deadline) {
		drainMirror(mirrored)
		got, err := probeFromCluster(ctx, client, clusterIP, port, protocol, payload)
		lastProbe, lastProbeErr = got, err
		if err == nil && got == wantProbe {
			select {
			case copy := <-mirrored:
				if copy != payload {
					return fmt.Errorf("%s local mirror got %q, want %s", protocol, copy, payload)
				}
				return nil
			case <-time.After(2 * time.Second):
				// Stale ClusterIP path still talking to the Pod.
			}
		}
		time.Sleep(2 * time.Second)
	}
	if lastProbeErr != nil {
		return fmt.Errorf("%s mirror probe failed: %v (last=%q)", protocol, lastProbeErr, lastProbe)
	}
	return fmt.Errorf("%s local mirror did not receive a request copy (last probe=%q)", protocol, lastProbe)
}

func drainMirror(ch <-chan string) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// startLocalTCPCapture accepts TCP payloads and sends them on received.
func startLocalTCPCapture(t *testing.T, received chan<- string) (net.Listener, *net.TCPAddr) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64)
				n, err := c.Read(buf)
				if err != nil || n == 0 {
					return
				}
				select {
				case received <- string(buf[:n]):
				default:
				}
				_, _ = fmt.Fprintf(c, "local-ignored:%s", buf[:n])
			}(conn)
		}
	}()
	return listener, listener.Addr().(*net.TCPAddr)
}

// startLocalUDPCapture receives UDP payloads and sends them on received.
func startLocalUDPCapture(t *testing.T, received chan<- string) (net.PacketConn, *net.UDPAddr) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 64)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			payload := string(buf[:n])
			select {
			case received <- payload:
			default:
			}
			_, _ = conn.WriteTo([]byte("local-ignored:"+payload), addr)
		}
	}()
	return conn, conn.LocalAddr().(*net.UDPAddr)
}
