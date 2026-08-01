package intercept

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

func TestHostRouteRegistryInstallsLooksUpAndRemovesRoutes(t *testing.T) {
	registry := newHostRouteRegistry()
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "API", Namespace: "Team"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.8",
		},
	}
	ports := []cluster.InterceptPort{
		{Protocol: corev1.ProtocolTCP, ServicePort: 80},
		{Protocol: corev1.ProtocolUDP, ServicePort: 53},
	}
	portKeys := map[string]PortMapping{
		"team/api:tcp:80": {
			ServicePort: 80, Protocol: "tcp", LocalHost: "127.0.0.2", LocalPort: 8080,
		},
		"team/api:udp:53": {
			ServicePort: 53, Protocol: "udp", LocalHost: "127.0.0.3", LocalPort: 5353,
		},
	}
	primaryAddrs := map[string]string{
		"team/api:tcp:80": "10.244.0.8:8080",
	}

	keys := registry.install(
		service, ports, portKeys, primaryAddrs, ModeMirror, false, "team/api",
	)
	if len(keys) != 8 {
		t.Fatalf("installed %d keys, want 8", len(keys))
	}

	route, ok := registry.lookup(" API.Team.SVC.Cluster.Local ", 80)
	if !ok {
		t.Fatal("DNS route was not normalized during lookup")
	}
	if route.mode != ModeMirror || route.preview ||
		route.local.LocalHost != "127.0.0.2" || route.local.LocalPort != 8080 ||
		route.primaryAddr != "10.244.0.8:8080" {
		t.Fatalf("unexpected TCP route: %#v", route)
	}

	udp, ok := registry.lookup("10.96.0.8", 53)
	if !ok || udp.local.LocalPort != 5353 || udp.primaryAddr != "" {
		t.Fatalf("unexpected UDP route: %#v, found=%v", udp, ok)
	}

	registry.remove(keys)
	if _, ok := registry.lookup("api.team", 80); ok {
		t.Fatal("route remains after removal")
	}
}

func TestHostRouteRegistrySkipsServicesWithoutRewriteHosts(t *testing.T) {
	registry := newHostRouteRegistry()
	keys := registry.install(
		&corev1.Service{Spec: corev1.ServiceSpec{ClusterIP: corev1.ClusterIPNone}},
		[]cluster.InterceptPort{{Protocol: corev1.ProtocolTCP, ServicePort: 80}},
		nil,
		nil,
		"",
		false,
		"default/headless",
	)
	if len(keys) != 0 || len(registry.byTarget) != 0 {
		t.Fatalf("headless unnamed service installed routes: %#v", registry.byTarget)
	}
}
