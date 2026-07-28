package mihomo

import (
	"net/netip"
	"testing"
)

func TestSelectTUNAddressUsesPreferredSubnetWhenFree(t *testing.T) {
	address, err := selectTUNAddressFrom([]netip.Prefix{
		netip.MustParsePrefix("192.168.1.10/24"),
		netip.MustParsePrefix("198.18.0.1/30"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != defaultTUNAddress {
		t.Fatalf("address = %q, want %q", address, defaultTUNAddress)
	}
}

func TestSelectTUNAddressSkipsOccupiedSubnets(t *testing.T) {
	address, err := selectTUNAddressFrom([]netip.Prefix{
		netip.MustParsePrefix("198.19.0.1/30"),
		netip.MustParsePrefix("198.19.0.4/30"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != "198.19.0.9/30" {
		t.Fatalf("address = %q, want %q", address, "198.19.0.9/30")
	}
}

func TestSelectTUNAddressSkipsOverlappingLargerNetwork(t *testing.T) {
	address, err := selectTUNAddressFrom([]netip.Prefix{
		netip.MustParsePrefix("198.19.0.0/24"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != "198.19.1.1/30" {
		t.Fatalf("address = %q, want %q", address, "198.19.1.1/30")
	}
}
