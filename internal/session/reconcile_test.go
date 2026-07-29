package session

import (
	"testing"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
)

func TestStalePortForwardReason(t *testing.T) {
	pods := map[string]cluster.PodInfo{
		"default/web": {
			Name: "web", Namespace: "default",
			Ports: []cluster.PodPortInfo{{Port: 8080, Protocol: "TCP"}},
		},
	}
	services := map[string]cluster.ServiceInfo{
		"default/api": {
			Name: "api", Namespace: "default",
			Ports: []cluster.ServicePortInfo{{Port: 80, Protocol: "TCP"}},
		},
	}

	if got := stalePortForwardReason(portfwd.Info{
		Kind: portfwd.KindPod, Namespace: "default", Name: "missing", RemotePort: 8080,
	}, pods, services); got != "pod deleted" {
		t.Fatalf("pod deleted: %q", got)
	}
	if got := stalePortForwardReason(portfwd.Info{
		Kind: portfwd.KindPod, Namespace: "default", Name: "web", RemotePort: 9090,
	}, pods, services); got != "pod port removed" {
		t.Fatalf("pod port removed: %q", got)
	}
	if got := stalePortForwardReason(portfwd.Info{
		Kind: portfwd.KindService, Namespace: "default", Name: "api", RemotePort: 80,
	}, pods, services); got != "" {
		t.Fatalf("healthy service: %q", got)
	}
	if got := stalePortForwardReason(portfwd.Info{
		Kind: portfwd.KindService, Namespace: "default", Name: "gone", RemotePort: 80,
	}, pods, services); got != "service deleted" {
		t.Fatalf("service deleted: %q", got)
	}
}

func TestStaleServiceBindingReason(t *testing.T) {
	services := map[string]cluster.ServiceInfo{
		"default/api": {
			Name: "api", Namespace: "default",
			Ports: []cluster.ServicePortInfo{{Port: 80, Protocol: "TCP"}},
		},
	}
	locals := []intercept.PortMapping{{ServicePort: 80, LocalPort: 18080}}
	if got := staleServiceBindingReason("default", "api", locals, services); got != "" {
		t.Fatalf("healthy: %q", got)
	}
	if got := staleServiceBindingReason("default", "missing", locals, services); got != "service deleted" {
		t.Fatalf("deleted: %q", got)
	}
	if got := staleServiceBindingReason("default", "api", []intercept.PortMapping{{ServicePort: 443}}, services); got != "service port removed" {
		t.Fatalf("port removed: %q", got)
	}
}
