package cluster

import "testing"

func TestNormalizeManualNetwork(t *testing.T) {
	got, err := NormalizeManualNetwork(ManualNetwork{
		PodCIDRs:       []string{"10.244.0.0/16, 10.245.0.0/16"},
		ServiceCIDRs:   []string{"10.96.0.0/12"},
		DNSServer:      " 10.96.0.10 ",
		ClusterDomains: []string{"corp.local"},
		DNSNamespace:   "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PodCIDRs) != 2 || got.PodCIDRs[0] != "10.244.0.0/16" {
		t.Fatalf("pod CIDRs: %#v", got.PodCIDRs)
	}
	if got.DNSServer != "10.96.0.10" {
		t.Fatalf("dns: %q", got.DNSServer)
	}
	if len(got.ClusterDomains) != 2 || got.ClusterDomains[0] != DefaultClusterDomain || got.ClusterDomains[1] != "corp.local" {
		t.Fatalf("cluster domains: %#v", got.ClusterDomains)
	}
	if got.DNSNamespace != "demo" {
		t.Fatalf("dns namespace: %q", got.DNSNamespace)
	}
	if _, err := NormalizeManualNetwork(ManualNetwork{DNSServer: "not-an-ip"}); err == nil {
		t.Fatal("expected DNS validation error")
	}
	if _, err := NormalizeManualNetwork(ManualNetwork{PodCIDRs: []string{"10.0.0.1"}}); err == nil {
		t.Fatal("expected CIDR validation error")
	}
}

func TestMergeManualNetworkFillsEmptyOnly(t *testing.T) {
	auto := Discovery{PodCIDRs: []string{"10.1.0.0/16"}, DNSServer: ""}
	merged := MergeManualNetwork(auto, ManualNetwork{
		PodCIDRs:       []string{"10.2.0.0/16"},
		DNSServer:      "10.96.0.10",
		ClusterDomains: []string{"corp.local"},
	})
	if merged.PodCIDRs[0] != "10.1.0.0/16" {
		t.Fatalf("auto pod CIDR should win: %#v", merged.PodCIDRs)
	}
	if merged.DNSServer != "10.96.0.10" {
		t.Fatalf("manual DNS should fill empty: %q", merged.DNSServer)
	}
	if len(merged.ClusterDomains) < 2 || merged.ClusterDomains[1] != "corp.local" {
		t.Fatalf("cluster domains: %#v", merged.ClusterDomains)
	}
}
