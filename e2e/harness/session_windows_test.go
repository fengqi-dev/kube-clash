//go:build e2e

package harness

import "testing"

func TestParseWindowsRouteChoosesLongestPrefix(t *testing.T) {
	output := `
IPv4 Route Table
===========================================================================
Active Routes:
Network Destination        Netmask          Gateway       Interface  Metric
          0.0.0.0          0.0.0.0     192.168.1.1    192.168.1.10     25
        10.96.0.0      255.240.0.0         On-link       198.19.0.1      5
      10.96.154.0    255.255.255.0       198.19.0.2      198.19.0.1      3
`
	got, err := parseWindowsRoute(output, "10.96.154.157")
	if err != nil {
		t.Fatal(err)
	}
	if got.Gateway != "198.19.0.2" || got.Interface != "198.19.0.1" {
		t.Fatalf("route = %+v", got)
	}
}

func TestParseWindowsRouteRejectsInvalidDestination(t *testing.T) {
	if _, err := parseWindowsRoute("", "not-an-ip"); err == nil {
		t.Fatal("expected invalid destination error")
	}
}
