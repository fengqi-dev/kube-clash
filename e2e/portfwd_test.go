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

func TestPortForwardSessionTCPAndUDP(t *testing.T) {
	requireE2E(t)
	ctx, cancel := testContext(t, 5*time.Minute)
	defer cancel()

	provider := newProvider(t)
	client := kubeClient(t, provider)
	if err := ensureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	_, gatewayForward := ensureGateway(t, ctx, provider)
	dataPlane := startTrafficDataPlane(t, ctx, provider, gatewayForward.Address())

	manager := portfwd.NewManager(provider)
	manager.SetTrafficDialer(
		kubeContext(), dataPlane.dialer(dataPlane.endpoints.PortForward),
	)
	t.Cleanup(manager.StopAll)

	tests := []struct {
		protocol string
		port     uint16
		want     string
	}{
		{protocol: "tcp", port: 8080, want: "cluster-tcp:ping"},
		{protocol: "udp", port: 9090, want: "cluster-udp:ping"},
	}
	for _, test := range tests {
		t.Run(test.protocol, func(t *testing.T) {
			info, err := manager.Start(ctx, portfwd.Request{
				Context: kubeContext(), Namespace: echoNamespace,
				Kind: portfwd.KindService, Name: "echo",
				Protocol: test.protocol, RemotePort: test.port,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := manager.Stop(info.ID); err != nil {
					t.Error(err)
				}
			}()

			deadline := time.Now().Add(30 * time.Second)
			var got string
			for {
				if test.protocol == "udp" {
					got, err = dialLocalUDPEcho(info.Address, "ping")
				} else {
					got, err = dialLocalEcho(info.Address, "ping")
				}
				if err == nil && got == test.want {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("response=%q err=%v, want %q", got, err, test.want)
				}
				time.Sleep(250 * time.Millisecond)
			}
		})
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

func dialLocalUDPEcho(address, payload string) (string, error) {
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
