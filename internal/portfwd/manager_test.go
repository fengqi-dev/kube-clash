package portfwd

import (
	"context"
	"testing"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

type fakeCluster struct {
	podName    string
	targetPort uint16
	forwarder  *fakeForwarder
}

func (f *fakeCluster) ResolveServiceBackend(
	context.Context, string, string, string, int32,
) (string, uint16, error) {
	return f.podName, f.targetPort, nil
}

func (f *fakeCluster) StartPodPortForward(
	context.Context, string, string, string, uint16, uint16,
) (cluster.PortForward, error) {
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
