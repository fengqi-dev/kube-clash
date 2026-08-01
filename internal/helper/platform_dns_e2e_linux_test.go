//go:build e2e && linux

package helper

import (
	"os"
	"strings"
	"testing"
)

func assertPlatformE2EDNS(t *testing.T, want bool) {
	t.Helper()
	content, err := os.ReadFile(resolvedDropIn)
	got := err == nil &&
		strings.Contains(string(content), linuxDNSMarker) &&
		strings.Contains(string(content), platformE2EDomain)
	if got != want {
		t.Fatalf("Linux DNS configured=%v want %v (read error=%v)", got, want, err)
	}
}
