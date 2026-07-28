//go:build integration

package mihomo

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-clash/internal/cluster"
)

func TestPinnedMihomoAcceptsGeneratedConfig(t *testing.T) {
	binaryPath := os.Getenv("KUBE_CLASH_MIHOMO_TEST_PATH")
	if binaryPath == "" {
		t.Skip("set KUBE_CLASH_MIHOMO_TEST_PATH to validate config with a real Mihomo binary")
	}
	content, err := Generate(cluster.Discovery{
		PodCIDRs:   []string{"10.244.0.0/16"},
		ServiceIPs: []string{"10.96.0.1", "10.96.0.10"},
		DNSServer:  "10.96.0.10",
	}, Options{
		BridgePort:       17890,
		ControllerPort:   19090,
		ControllerSecret: "integration-test-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "config.yaml"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(binaryPath, "-t", "-d", workDir).CombinedOutput()
	if err != nil {
		t.Fatalf("mihomo rejected generated config: %v\n%s", err, output)
	}
}

func TestMihomoTUNListenerStarts(t *testing.T) {
	if os.Getenv("KUBE_CLASH_MIHOMO_TUN_TEST") != "1" {
		t.Skip("set KUBE_CLASH_MIHOMO_TUN_TEST=1 to test the privileged TUN listener")
	}
	bridge, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	core, err := (&Runtime{}).Start(ctx, cluster.Discovery{
		PodCIDRs:   []string{"10.244.0.0/24"},
		ServiceIPs: []string{"10.96.0.1", "10.96.0.10"},
		DNSServer:  "10.96.0.10",
	}, bridge.Addr().String(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Close(); err != nil {
		t.Logf("close TUN test core: %v", err)
	}
}
