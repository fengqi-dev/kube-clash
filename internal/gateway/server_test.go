package gateway

import (
	"context"
	"net/netip"
	"testing"
)

func TestClusterAddressPolicy(t *testing.T) {
	allowed := []string{"10.0.0.1", "172.16.1.2", "192.168.10.2", "fd00::1"}
	for _, raw := range allowed {
		if !isClusterAddress(netip.MustParseAddr(raw)) {
			t.Errorf("expected %s to be allowed", raw)
		}
	}
	denied := []string{"127.0.0.1", "8.8.8.8", "169.254.1.1", "::1"}
	for _, raw := range denied {
		if isClusterAddress(netip.MustParseAddr(raw)) {
			t.Errorf("expected %s to be denied", raw)
		}
	}
}

func TestResolvePrivateRejectsPublicTarget(t *testing.T) {
	if _, err := resolvePrivate(context.Background(), "8.8.8.8", 53); err == nil {
		t.Fatal("expected public target to be rejected")
	}
}
