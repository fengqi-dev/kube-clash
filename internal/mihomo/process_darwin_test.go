package mihomo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaleManagedPIDsOnlyReturnsMatchingKubeClashCore(t *testing.T) {
	root := t.TempDir()
	oldSession := filepath.Join(root, "session-old")
	newSession := filepath.Join(root, "session-new")
	if err := os.MkdirAll(oldSession, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newSession, 0o700); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(root, "mihomo-test")
	pid := "1234"
	if err := os.WriteFile(
		filepath.Join(oldSession, privilegedPIDFilename),
		[]byte(pid),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	found := staleManagedPIDsWith(binaryPath, newSession, func(foundPID string) (string, error) {
		return binaryPath + " -d " + oldSession, nil
	})
	if len(found) != 1 || found[0] != pid {
		t.Fatalf("stale PIDs = %v, want [%s]", found, pid)
	}
}

func TestManagedRouteCleanupCommands(t *testing.T) {
	workDir := t.TempDir()
	content := []byte(`tun:
  route-address:
    - 10.244.0.0/24
    - 10.96.0.10/32
`)
	if err := os.WriteFile(filepath.Join(workDir, "config.yaml"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	commands, err := managedRouteCleanupCommands(workDir)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"/sbin/route -n delete -net 10.244.0.0/24",
		"/sbin/route -n delete -host 10.96.0.10",
	}
	for _, item := range expected {
		if !strings.Contains(commands, item) {
			t.Fatalf("commands %q do not contain %q", commands, item)
		}
	}
}

func TestStopPrivilegedProcessUsesStopFile(t *testing.T) {
	workDir := t.TempDir()
	pidPath := filepath.Join(workDir, privilegedPIDFilename)
	if err := os.WriteFile(pidPath, []byte("1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stopPrivilegedProcess(pidPath); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(workDir, privilegedStopFilename))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "stop\n" {
		t.Fatalf("stop file = %q", content)
	}
}
