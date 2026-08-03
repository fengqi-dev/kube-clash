package cluster

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestCollectNodePodCIDRsUsesOnlyAdvertisedCIDRs(t *testing.T) {
	nodes := []corev1.Node{
		{
			Spec: corev1.NodeSpec{
				PodCIDR:  "10.244.0.7/24",
				PodCIDRs: []string{"10.244.0.0/24", "fd00:10:244::1/64"},
			},
		},
	}

	got := sortedKeys(collectNodePodCIDRs(nodes))
	want := []string{"10.244.0.0/24", "fd00:10:244::/64"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectNodePodCIDRs() = %v, want %v", got, want)
	}
}
