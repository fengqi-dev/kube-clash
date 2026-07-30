package singbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

func TestGeneratedConfigAcceptedBySingBox(t *testing.T) {
	binary := os.Getenv("KUBELOOP_SINGBOX_PATH")
	if binary == "" {
		t.Skip("KUBELOOP_SINGBOX_PATH is not set")
	}
	content, err := Generate(cluster.Discovery{
		PodCIDRs:     []string{"10.244.0.0/16"},
		ServiceCIDRs: []string{"10.96.0.0/12"},
		DNSServer:    "10.96.0.10",
	}, Options{
		BridgePort: 17890, ControllerPort: 19090,
		ControllerSecret: "controller-secret-1234567890123456",
		DNSPort:          1053, TUNAddress: "198.19.0.1/30",
		TrafficPorts: TrafficInboundPorts{
			PortForward: 18081, Exchange: 18082, Preview: 18083,
			MirrorPrimary: 18084, MirrorShadow: 18085,
		},
		TrafficUsername: "traffic-user-1234",
		TrafficPassword: "traffic-password-1234567890123456",
	})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "check", "-c", configPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sing-box config check failed: %v\n%s", err, output)
	}
}
