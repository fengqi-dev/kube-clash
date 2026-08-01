//go:build e2e && darwin

package helper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertPlatformE2EDNS(t *testing.T, want bool) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(resolverDir, platformE2EDomain))
	got := err == nil && strings.Contains(string(content), dnsMarker)
	if got != want {
		t.Fatalf("macOS resolver configured=%v want %v (read error=%v)", got, want, err)
	}
}
