package intercept

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/gateway"
)

type fakeCluster struct {
	service   *corev1.Service
	applied   *cluster.ServiceInterceptSnapshot
	restored  bool
	preview   *cluster.PreviewServiceSnapshot
	deleted   bool
	previewIP string
}

func (f *fakeCluster) GetService(
	context.Context, string, string, string,
) (*corev1.Service, error) {
	return f.service.DeepCopy(), nil
}

func (f *fakeCluster) ApplyServiceIntercept(
	_ context.Context, _ string, snapshot cluster.ServiceInterceptSnapshot, _ string,
) error {
	copySnapshot := snapshot
	f.applied = &copySnapshot
	return nil
}

func (f *fakeCluster) RestoreServiceIntercept(
	context.Context, string, cluster.ServiceInterceptSnapshot,
) error {
	f.restored = true
	return nil
}

func (f *fakeCluster) CreatePreviewService(
	_ context.Context, _ string, snapshot cluster.PreviewServiceSnapshot, _ string,
) (*corev1.Service, error) {
	copySnapshot := snapshot
	f.preview = &copySnapshot
	ip := f.previewIP
	if ip == "" {
		ip = "10.96.9.9"
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: snapshot.Service, Namespace: snapshot.Namespace},
		Spec:       corev1.ServiceSpec{ClusterIP: ip, Ports: []corev1.ServicePort{{Port: snapshot.Ports[0].ServicePort}}},
	}, nil
}

func (f *fakeCluster) DeletePreviewService(
	context.Context, string, cluster.PreviewServiceSnapshot,
) error {
	f.deleted = true
	return nil
}

func TestStartStopInterceptRegistersAndRestores(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := gateway.NewServer(log.New(io.Discard, "", 0), time.Second)
	go func() { _ = server.Serve(listener) }()

	local, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	go func() {
		conn, err := local.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 32)
		n, _ := conn.Read(buf)
		_, _ = fmt.Fprintf(conn, "local:%s", buf[:n])
	}()

	api := &fakeCluster{
		service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
			Spec: corev1.ServiceSpec{
				ClusterIP: "10.96.1.10",
				Selector:  map[string]string{"app": "api"},
				Ports: []corev1.ServicePort{{
					Name: "http", Port: 80, Protocol: corev1.ProtocolTCP,
				}},
			},
		},
	}
	manager := NewManager(api)
	ctx := context.Background()
	if err := manager.Start(ctx, "minikube", "10.244.0.8", listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.StopAll(context.Background()) }()

	info, err := manager.StartIntercept(ctx, Mapping{
		Namespace: "default",
		Service:   "api",
		Ports: []PortMapping{{
			ServicePort: 80, Protocol: "TCP",
			LocalHost: "127.0.0.1", LocalPort: local.Addr().(*net.TCPAddr).Port,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if api.applied == nil || api.applied.GatewayIP != "10.244.0.8" {
		t.Fatalf("apply not called: %#v", api.applied)
	}
	if len(manager.List()) != 1 {
		t.Fatalf("list=%d", len(manager.List()))
	}

	listenPort := info.Ports[0].ListenPort
	client, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", listenPort))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "local:ping" {
		t.Fatalf("got %q", got)
	}

	if err := manager.Stop(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	if !api.restored {
		t.Fatal("restore not called")
	}
}

func TestStartStopPreviewCreatesAndDeletes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := gateway.NewServer(log.New(io.Discard, "", 0), time.Second)
	go func() { _ = server.Serve(listener) }()

	local, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()

	api := &fakeCluster{}
	manager := NewManager(api)
	ctx := context.Background()
	if err := manager.Start(ctx, "minikube", "10.244.0.8", listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.StopAll(context.Background()) }()

	info, err := manager.StartPreview(ctx, PreviewRequest{
		Namespace: "demo",
		Name:      "local-api",
		Ports: []PortMapping{{
			ServicePort: 8080, Protocol: "TCP",
			LocalHost: "127.0.0.1", LocalPort: local.Addr().(*net.TCPAddr).Port,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !info.Preview || info.ClusterIP == "" {
		t.Fatalf("preview info = %#v", info)
	}
	if api.preview == nil || api.preview.Service != "local-api" {
		t.Fatalf("create not called: %#v", api.preview)
	}
	if len(manager.List()) != 0 || len(manager.ListPreviews()) != 1 {
		t.Fatalf("list intercept=%d preview=%d", len(manager.List()), len(manager.ListPreviews()))
	}
	if err := manager.Stop(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	if !api.deleted {
		t.Fatal("delete not called")
	}
}
