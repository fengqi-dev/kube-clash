//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/portfwd"
)

func TestPortForwardServiceTCP(t *testing.T) {
	requireE2E(t)
	ctx, cancel := testContext(t, 4*time.Minute)
	defer cancel()

	provider := newProvider(t)
	client := kubeClient(t, provider)
	if err := ensureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}

	manager := portfwd.NewManager(provider)
	t.Cleanup(manager.StopAll)

	info, err := manager.Start(ctx, portfwd.Request{
		Context:    kubeContext(),
		Namespace:  echoNamespace,
		Kind:       portfwd.KindService,
		Name:       "echo",
		RemotePort: 8080,
		LocalPort:  0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.LocalPort == 0 || info.Address == "" {
		t.Fatalf("unexpected forward info %#v", info)
	}
	if info.PodName == "" {
		t.Fatal("service port-forward did not resolve a pod")
	}

	deadline := time.Now().Add(30 * time.Second)
	var last error
	for {
		got, err := dialLocalEcho(info.Address, "ping")
		if err == nil && got == "cluster-tcp:ping" {
			break
		}
		last = err
		if err == nil {
			last = fmt.Errorf("unexpected response %q", got)
		}
		if time.Now().After(deadline) {
			t.Fatalf("port-forward echo failed: %v", last)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := manager.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	if len(manager.List()) != 0 {
		t.Fatalf("expected no active forwards, got %d", len(manager.List()))
	}
}

func dialLocalEcho(address, payload string) (string, error) {
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
