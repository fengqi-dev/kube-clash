package cluster

import (
	"net/netip"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestAddObservedPodIPsSkipsHostNetworkAndNodeIPs(t *testing.T) {
	nodeIP := netip.MustParseAddr("192.168.64.66")
	podCIDRs := map[string]struct{}{
		"10.244.0.0/24": {},
	}
	nodeIPs := map[netip.Addr]struct{}{nodeIP: {}}
	pods := []corev1.Pod{
		{
			Spec:   corev1.PodSpec{HostNetwork: true},
			Status: corev1.PodStatus{PodIP: "192.168.64.66"},
		},
		{
			Status: corev1.PodStatus{PodIP: "10.244.0.9"},
		},
		{
			Status: corev1.PodStatus{
				PodIP:  "10.245.1.2",
				PodIPs: []corev1.PodIP{{IP: "10.245.1.2"}},
			},
		},
		{
			// Same IP as a node, even without HostNetwork set.
			Status: corev1.PodStatus{PodIP: "192.168.64.66"},
		},
	}

	addObservedPodIPs(podCIDRs, pods, nodeIPs)

	if _, ok := podCIDRs["192.168.64.66/32"]; ok {
		t.Fatalf("node/hostNetwork IP must not become a TUN route: %#v", podCIDRs)
	}
	if _, ok := podCIDRs["10.244.0.9/32"]; ok {
		t.Fatalf("IP inside node PodCIDR should not be re-added: %#v", podCIDRs)
	}
	if _, ok := podCIDRs["10.245.1.2/32"]; !ok {
		t.Fatalf("out-of-prefix Pod IP should be added: %#v", podCIDRs)
	}
	if len(podCIDRs) != 2 {
		t.Fatalf("unexpected pod CIDRs: %#v", podCIDRs)
	}
}
