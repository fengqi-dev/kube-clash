package singbox

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

func TestGenerateRoutesOnlyClusterTraffic(t *testing.T) {
	content, err := Generate(cluster.Discovery{
		PodCIDRs:     []string{"10.244.0.0/16"},
		ServiceCIDRs: []string{"10.96.0.0/12"},
		ServiceIPs:   []string{"10.96.0.10", "10.96.0.1", "10.105.153.132"},
		DNSServer:    "10.96.0.10",
	}, Options{
		BridgePort: 17890, ControllerPort: 19090, ControllerSecret: "test-secret",
		DNSPort: 1053, Namespace: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	required := []string{
		`"type": "tun"`,
		`"auto_route": true`,
		`"strict_route": true`,
		`"10.244.0.0/16"`,
		`"10.96.0.0/12"`,
		`"cluster.local"`,
		`"tag": "kubernetes"`,
		`"type": "socks"`,
		`"final": "direct"`,
		`"external_controller"`,
	}
	for _, item := range []string{`"10.96.0.1/32"`, `"10.105.153.132/32"`} {
		if strings.Contains(text, item) {
			t.Errorf("per-IP route %s should be omitted when Service CIDR is present:\n%s", item, text)
		}
	}
	for _, item := range required {
		if !strings.Contains(text, item) {
			t.Errorf("generated config does not contain %q:\n%s", item, text)
		}
	}
	forbidden := []string{
		`"0.0.0.0/1"`,
		`"128.0.0.0/1"`,
		`114.114.114.114`,
		`fake-ip`,
		`fake_ip`,
	}
	for _, item := range forbidden {
		if strings.Contains(text, item) {
			t.Errorf("generated config unexpectedly contains %q:\n%s", item, text)
		}
	}

	var parsed map[string]any
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatal(err)
	}
	inbounds, _ := parsed["inbounds"].([]any)
	var routeAddress []any
	for _, inbound := range inbounds {
		item, _ := inbound.(map[string]any)
		if item["type"] == "tun" {
			routeAddress, _ = item["route_address"].([]any)
		}
	}
	if len(routeAddress) == 0 {
		t.Fatal("tun route_address missing")
	}
	for _, route := range routeAddress {
		value, _ := route.(string)
		if value == "0.0.0.0/1" || value == "128.0.0.0/1" {
			t.Fatalf("global route leaked into tun route_address: %v", routeAddress)
		}
	}
}

func TestGenerateRejectsInvalidDiscovery(t *testing.T) {
	_, err := Generate(cluster.Discovery{PodCIDRs: []string{"not-a-cidr"}}, Options{
		BridgePort: 17890, ControllerPort: 19090, ControllerSecret: "secret",
	})
	if err == nil {
		t.Fatal("expected invalid CIDR error")
	}
}

func TestResolverDomains(t *testing.T) {
	got := ResolverDomains("demo")
	want := []string{"cluster.local", "svc.cluster.local", "demo.svc.cluster.local"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ResolverDomains = %v, want %v", got, want)
	}
	withHosts := ResolverDomains("demo", HostAlias{Domain: "app.dev", IP: "10.96.0.50"})
	if !strings.Contains(strings.Join(withHosts, ","), "app.dev") {
		t.Fatalf("ResolverDomains missing host alias: %v", withHosts)
	}
}

func TestGenerateHostAliases(t *testing.T) {
	content, err := Generate(cluster.Discovery{
		PodCIDRs:     []string{"10.244.0.0/16"},
		ServiceCIDRs: []string{"10.96.0.0/12"},
		DNSServer:    "10.96.0.10",
	}, Options{
		BridgePort: 17890, ControllerPort: 19090, ControllerSecret: "test-secret",
		DNSPort: 1053, Namespace: "default",
		Hosts: []HostAlias{{Domain: "app.dev", IP: "10.96.0.50"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, item := range []string{`"type": "hosts"`, `"app.dev"`, `"10.96.0.50"`} {
		if !strings.Contains(text, item) {
			t.Fatalf("generated config missing %q:\n%s", item, text)
		}
	}
}

func TestNormalizeHostAliasesClearsEmpty(t *testing.T) {
	got, err := NormalizeHostAliases(nil)
	if err != nil || got != nil {
		t.Fatalf("empty aliases = %v, %v", got, err)
	}
}

func TestSearchDomains(t *testing.T) {
	got := SearchDomains("demo")
	want := []string{"demo.svc.cluster.local", "svc.cluster.local", "cluster.local"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("SearchDomains = %v, want %v", got, want)
	}
	if strings.Join(SearchDomains(""), ",") != strings.Join(SearchDomains("default"), ",") {
		t.Fatal("empty namespace should default to default")
	}
}
