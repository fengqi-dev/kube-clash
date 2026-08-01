package portfwd

import (
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

type fakeCluster struct {
	podName    string
	targetPort uint16
	forwarder  *fakeForwarder
	pods       []cluster.PodInfo
	services   []cluster.ServiceInfo
}

func (f *fakeCluster) ListPods(
	context.Context, string, string,
) ([]cluster.PodInfo, error) {
	return f.pods, nil
}

func (f *fakeCluster) ListServices(
	context.Context, string, string,
) ([]cluster.ServiceInfo, error) {
	return f.services, nil
}

func (f *fakeCluster) ResolveServiceBackend(
	context.Context, string, string, string, int32,
) (string, uint16, error) {
	return f.podName, f.targetPort, nil
}

func (f *fakeCluster) ResolveRoutedTarget(_ context.Context, request Request) (string, error) {
	switch request.Kind {
	case KindPod:
		return net.JoinHostPort(
			f.pods[0].IP, strconv.Itoa(int(request.RemotePort)),
		), nil
	case KindService:
		return net.JoinHostPort(
			f.services[0].ClusterIP, strconv.Itoa(int(request.RemotePort)),
		), nil
	default:
		return "", nil
	}
}

func (f *fakeCluster) StartPodPortForward(
	context.Context, string, string, string, uint16, uint16,
) (Forwarder, error) {
	if f.forwarder == nil {
		f.forwarder = &fakeForwarder{address: "127.0.0.1:18080"}
	}
	return f.forwarder, nil
}

type fakeForwarder struct {
	address string
	closed  bool
}

func (f *fakeForwarder) Address() string { return f.address }
func (f *fakeForwarder) Close() error {
	f.closed = true
	return nil
}

func TestStartStopPodPortForward(t *testing.T) {
	api := &fakeCluster{}
	manager := NewManager(api)
	info, err := manager.Start(context.Background(), Request{
		Context: "minikube", Namespace: "default", Kind: KindPod,
		Name: "api-0", RemotePort: 8080,
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.LocalPort != 18080 || info.Address != "127.0.0.1:18080" {
		t.Fatalf("unexpected info %#v", info)
	}
	if len(manager.List()) != 1 {
		t.Fatalf("list=%d", len(manager.List()))
	}
	if err := manager.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	if !api.forwarder.closed {
		t.Fatal("forwarder not closed")
	}
}

func TestStartServicePortForwardResolvesPod(t *testing.T) {
	api := &fakeCluster{podName: "api-abc", targetPort: 8080}
	manager := NewManager(api)
	info, err := manager.Start(context.Background(), Request{
		Context: "minikube", Namespace: "default", Kind: KindService,
		Name: "api", RemotePort: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.PodName != "api-abc" || info.RemotePort != 80 {
		t.Fatalf("unexpected info %#v", info)
	}
	manager.StopAll()
}

type echoTrafficDialer struct {
	targets chan string
}

func (d *echoTrafficDialer) DialContext(
	_ context.Context, network, address string,
) (net.Conn, error) {
	d.targets <- network + ":" + address
	client, server := net.Pipe()
	go func() {
		_, _ = io.Copy(server, server)
		_ = server.Close()
	}()
	return client, nil
}

func TestStartRoutedPodPortForward(t *testing.T) {
	api := &fakeCluster{pods: []cluster.PodInfo{{
		Name: "api-0", Namespace: "default", IP: "10.244.1.9",
	}}}
	dialer := &echoTrafficDialer{targets: make(chan string, 1)}
	manager := NewManager(api)
	manager.SetTrafficDialer("minikube", dialer)
	info, err := manager.Start(context.Background(), Request{
		Context: "minikube", Namespace: "default", Kind: KindPod,
		Name: "api-0", RemotePort: 8080,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.StopAll()

	connection, err := net.DialTimeout("tcp", info.Address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(connection, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "ping" {
		t.Fatalf("echo = %q", buffer)
	}
	select {
	case target := <-dialer.targets:
		if target != "tcp:10.244.1.9:8080" {
			t.Fatalf("target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("traffic dialer was not used")
	}
}

func TestStartRoutedUDPPortForward(t *testing.T) {
	api := &fakeCluster{services: []cluster.ServiceInfo{{
		Name: "dns", Namespace: "default", ClusterIP: "10.96.0.10",
	}}}
	dialer := &echoTrafficDialer{targets: make(chan string, 1)}
	manager := NewManager(api)
	manager.SetTrafficDialer("minikube", dialer)
	info, err := manager.Start(context.Background(), Request{
		Context: "minikube", Namespace: "default", Kind: KindService,
		Name: "dns", Protocol: "UDP", RemotePort: 53,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.StopAll()
	if info.Protocol != "udp" {
		t.Fatalf("protocol = %q", info.Protocol)
	}

	remote, err := net.ResolveUDPAddr("udp", info.Address)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialUDP("udp", nil, remote)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := connection.Write([]byte("dns")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 3)
	if _, err := io.ReadFull(connection, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "dns" {
		t.Fatalf("echo = %q", buffer)
	}
	select {
	case target := <-dialer.targets:
		if target != "udp:10.96.0.10:53" {
			t.Fatalf("target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("traffic dialer was not used")
	}
}

func TestUDPPortForwardWithoutSessionReturnsExplicitError(t *testing.T) {
	manager := NewManager(&fakeCluster{})
	_, err := manager.Start(context.Background(), Request{
		Context: "minikube", Namespace: "default", Kind: KindPod,
		Name: "dns-0", Protocol: "udp", RemotePort: 53,
	})
	if err == nil {
		t.Fatal("expected UDP session requirement error")
	}
}
