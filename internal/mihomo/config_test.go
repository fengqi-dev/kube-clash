package mihomo

import (
	"strings"
	"testing"

	"github.com/kube-clash/kube-clash/internal/cluster"
)

func TestGenerateRoutesOnlyClusterTraffic(t *testing.T) {
	content, err := Generate(cluster.Discovery{
		PodCIDRs:   []string{"10.244.0.0/16"},
		ServiceIPs: []string{"10.96.0.10", "10.96.0.1"},
		DNSServer:  "10.96.0.10",
	}, Options{
		BridgePort: 17890, ControllerPort: 19090, ControllerSecret: "test-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	required := []string{
		"stack: mixed",
		"auto-route: true",
		"route-address:",
		"- 10.244.0.0/16",
		"- 10.96.0.1/32",
		"name: KUBERNETES",
		"type: socks5",
		"udp: true",
		"+.cluster.local: udp://10.96.0.10#KUBERNETES",
		"DOMAIN-SUFFIX,cluster.local,KUBERNETES",
		"MATCH,DIRECT",
	}
	for _, item := range required {
		if !strings.Contains(text, item) {
			t.Errorf("generated config does not contain %q:\n%s", item, text)
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
