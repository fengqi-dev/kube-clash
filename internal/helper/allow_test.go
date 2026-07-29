package helper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateWorkDir(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".kubeloop", "sessions", "session-1")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWorkDir(root, home); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "other")
	_ = os.MkdirAll(outside, 0o755)
	_ = os.WriteFile(filepath.Join(outside, "config.json"), []byte(`{}`), 0o600)
	if err := ValidateWorkDir(outside, home); err == nil {
		t.Fatal("expected outside workDir to be rejected")
	}
}
