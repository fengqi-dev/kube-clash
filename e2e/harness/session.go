//go:build e2e

package harness

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/helper"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/store"
)

// Helper enforces a single privileged TUN session; serialize Connect across tests.
var tunMu sync.Mutex

type LiveSession struct {
	Manager  *session.Manager
	Store    *store.Store
	Provider *cluster.Provider
	Client   kubernetes.Interface
	State    session.State
}

func ConnectSession(
	t *testing.T,
	ctx context.Context,
	req session.Request,
	setup func(*session.Manager),
) *LiveSession {
	t.Helper()
	tunMu.Lock()
	locked := true
	unlock := func() {
		if locked {
			locked = false
			tunMu.Unlock()
		}
	}

	// Clear any leftover privileged session before Connect.
	StopAllHelperSessions()
	// Give Linux nftables/auto_redirect a moment to drop the previous TUN path.
	if runtime.GOOS == "linux" {
		time.Sleep(500 * time.Millisecond)
	}

	provider := NewProvider(t)
	stateStore, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		unlock()
		t.Fatalf("open store: %v", err)
	}
	manager := session.NewManager(
		provider,
		session.WithStore(stateStore),
		session.WithGatewayImage(GatewayImage()),
	)
	if setup != nil {
		setup(manager)
	}

	connected := make(chan session.State, 1)
	failed := make(chan session.State, 1)
	manager.Subscribe(func(state session.State) {
		switch state.Phase {
		case session.PhaseConnected:
			select {
			case connected <- state:
			default:
			}
		case session.PhaseError:
			select {
			case failed <- state:
			default:
			}
		}
	})

	if req.Context == "" {
		req.Context = KubeContext()
	}
	if req.Namespace == "" {
		req.Namespace = EchoNamespace
	}
	if err := manager.Connect(ctx, req); err != nil {
		unlock()
		t.Fatalf("connect: %v", err)
	}

	var state session.State
	select {
	case state = <-connected:
	case state = <-failed:
		_ = manager.Disconnect()
		StopAllHelperSessions()
		unlock()
		t.Fatalf("session failed: %s (%s)", state.Phase, state.Message)
	case <-ctx.Done():
		_ = manager.Disconnect()
		StopAllHelperSessions()
		unlock()
		t.Fatal(ctx.Err())
	}

	t.Cleanup(func() {
		_ = manager.Disconnect()
		StopAllHelperSessions()
		unlock()
	})

	return &LiveSession{
		Manager:  manager,
		Store:    stateStore,
		Provider: provider,
		Client:   KubeClient(t, provider),
		State:    state,
	}
}

func StopAllHelperSessions() {
	client, err := helper.NewClient()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _ = client.StopAll(ctx)
}

// RunMain runs package tests then clears leftover helper sessions.
func RunMain(m *testing.M) {
	code := m.Run()
	StopAllHelperSessions()
	os.Exit(code)
}

func EchoServiceIP(t *testing.T, ctx context.Context, client kubernetes.Interface) string {
	t.Helper()
	service, err := client.CoreV1().Services(EchoNamespace).Get(ctx, "echo", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if service.Spec.ClusterIP == "" {
		t.Fatal("echo service has no ClusterIP")
	}
	return service.Spec.ClusterIP
}

func EchoPodIP(t *testing.T, ctx context.Context, client kubernetes.Interface) (podName, podIP string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		pods, err := client.CoreV1().Pods(EchoNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=kubeloop-e2e-echo",
		})
		if err == nil {
			for _, pod := range pods.Items {
				if pod.Status.Phase == "Running" && pod.Status.PodIP != "" {
					return pod.Name, pod.Status.PodIP
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("echo pod IP not ready")
	return "", ""
}

func WaitHostTCP(t *testing.T, host string, port int, payload, wantPrefix string) string {
	t.Helper()
	address := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	deadline := time.Now().Add(45 * time.Second)
	var last string
	var lastErr error
	for time.Now().Before(deadline) {
		got, err := DialLocalEcho(address, payload)
		if err == nil && strings.HasPrefix(got, wantPrefix) {
			return got
		}
		last, lastErr = got, err
		time.Sleep(400 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("host TCP %s: %v (last=%q)", address, lastErr, last)
	}
	t.Fatalf("host TCP %s: got %q want prefix %q", address, last, wantPrefix)
	return ""
}

func WaitHostUDP(t *testing.T, host string, port int, payload, wantPrefix string) string {
	t.Helper()
	address := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	deadline := time.Now().Add(45 * time.Second)
	var last string
	var lastErr error
	for time.Now().Before(deadline) {
		got, err := DialLocalUDPEcho(address, payload)
		if err == nil && strings.HasPrefix(got, wantPrefix) {
			return got
		}
		last, lastErr = got, err
		time.Sleep(400 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("host UDP %s: %v (last=%q)", address, lastErr, last)
	}
	t.Fatalf("host UDP %s: got %q want prefix %q", address, last, wantPrefix)
	return ""
}

func WaitLookupIP(t *testing.T, name, wantIP string) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	var last []string
	var lastErr error
	for time.Now().Before(deadline) {
		ips, err := net.LookupHost(name)
		if err == nil {
			for _, ip := range ips {
				if ip == wantIP {
					return
				}
			}
			last, lastErr = ips, fmt.Errorf("got %v want %s", ips, wantIP)
		} else {
			lastErr = err
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("lookup %s: %v (last=%v)", name, lastErr, last)
}

func AssertHelperIdle(t *testing.T) {
	t.Helper()
	client, err := helper.NewClient()
	if err != nil {
		t.Fatalf("helper client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := client.Ping(ctx)
	if err != nil {
		t.Fatalf("helper ping: %v", err)
	}
	if len(response.ActiveSessions) != 0 {
		t.Fatalf("helper still has sessions after disconnect: %v", response.ActiveSessions)
	}
}

// assertClusterDNSGone checks split-DNS no longer answers the cluster FQDN.
func AssertClusterDNSGone(t *testing.T, fqdn, clusterIP string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		ips, err := net.LookupHost(fqdn)
		if err != nil || !ContainsIP(ips, clusterIP) {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("cluster DNS %s still resolves to %s after disconnect", fqdn, clusterIP)
}

type hostRoute struct {
	Gateway   string
	Interface string
}

func lookupHostRoute(host string) (hostRoute, error) {
	switch runtime.GOOS {
	case "linux":
		return lookupHostRouteLinux(host)
	default:
		return lookupHostRouteDarwin(host)
	}
}

func lookupHostRouteDarwin(host string) (hostRoute, error) {
	cmd := exec.Command("route", "-n", "get", host)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return hostRoute{}, fmt.Errorf("route get %s: %w (%s)", host, err, strings.TrimSpace(string(output)))
	}
	var route hostRoute
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "gateway:"):
			route.Gateway = strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
		case strings.HasPrefix(line, "interface:"):
			route.Interface = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}
	if route.Interface == "" {
		return hostRoute{}, fmt.Errorf("no interface in route get %s:\n%s", host, output)
	}
	return route, nil
}

func lookupHostRouteLinux(host string) (hostRoute, error) {
	cmd := exec.Command("ip", "-o", "route", "get", host)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return hostRoute{}, fmt.Errorf("ip route get %s: %w (%s)", host, err, strings.TrimSpace(string(output)))
	}
	// Example: "10.96.0.1 via 198.19.0.1 dev tun0 src 198.19.0.2 uid 1001"
	fields := strings.Fields(string(output))
	var route hostRoute
	for i := 0; i < len(fields)-1; i++ {
		switch fields[i] {
		case "via":
			route.Gateway = fields[i+1]
		case "dev":
			route.Interface = fields[i+1]
		}
	}
	if route.Interface == "" {
		return hostRoute{}, fmt.Errorf("no device in ip route get %s:\n%s", host, output)
	}
	return route, nil
}

func isKubeLoopTUN(iface string) bool {
	return strings.HasPrefix(iface, "utun") || strings.HasPrefix(iface, "tun")
}

// RequireRoutedViaKubeLoop requires host traffic to use the same TUN gateway as a
// known-good ClusterIP path. Skips when another TUN client (e.g. Clash 198.18/16)
// owns the destination CIDR — direct Pod IP cannot be validated in that environment.
func RequireRoutedViaKubeLoop(t *testing.T, host, referenceHost string) {
	t.Helper()
	if runtime.GOOS == "linux" {
		// Linux uses sing-box auto_redirect (nftables). ip route get often still
		// shows the Docker/Minikube path instead of tun*, so the route table is
		// not a reliable TUN ownership signal. WaitHostTCP/UDP cover reachability.
		t.Log("skipping TUN route table check on linux (auto_redirect)")
		return
	}
	deadline := time.Now().Add(15 * time.Second)
	var last hostRoute
	var lastErr error
	var want hostRoute
	for time.Now().Before(deadline) {
		ref, err := lookupHostRoute(referenceHost)
		if err != nil || !isKubeLoopTUN(ref.Interface) {
			lastErr = err
			time.Sleep(300 * time.Millisecond)
			continue
		}
		want = ref
		got, err := lookupHostRoute(host)
		if err == nil && got.Gateway == want.Gateway && got.Interface == want.Interface {
			return
		}
		last, lastErr = got, err
		time.Sleep(300 * time.Millisecond)
	}
	if lastErr == nil && want.Interface != "" && (last.Gateway != want.Gateway || last.Interface != want.Interface) {
		t.Skipf(
			"route to %s is %+v, KubeLoop TUN is %+v; disable conflicting TUN (e.g. Clash) to cover host Pod IP",
			host, last, want,
		)
	}
	t.Fatalf("route to %s not via KubeLoop TUN (last=%+v err=%v want=%+v)", host, last, lastErr, want)
}

func ContainsIP(ips []string, want string) bool {
	for _, ip := range ips {
		if ip == want {
			return true
		}
	}
	return false
}

func DialLocalEcho(address, payload string) (string, error) {
	conn, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(conn, payload); err != nil {
		return "", err
	}
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

func DialLocalUDPEcho(address, payload string) (string, error) {
	remote, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return "", err
	}
	connection, err := net.DialUDP("udp", nil, remote)
	if err != nil {
		return "", err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := connection.Write([]byte(payload)); err != nil {
		return "", err
	}
	buffer := make([]byte, 128)
	n, err := connection.Read(buffer)
	if err != nil {
		return "", err
	}
	return string(buffer[:n]), nil
}
