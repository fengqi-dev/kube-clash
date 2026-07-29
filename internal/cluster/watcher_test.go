package cluster

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSnapshotFromListsCountsInventory(t *testing.T) {
	snap := snapshotFromLists(
		[]*corev1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Name: "a"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "b"}},
		},
		[]*corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"},
				Spec:       corev1.ServiceSpec{ClusterIPs: []string{"10.96.0.1"}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "kube-dns", Namespace: "kube-system"},
				Spec:       corev1.ServiceSpec{ClusterIPs: []string{"10.96.0.10"}},
			},
		},
		[]*appsv1.Deployment{
			{ObjectMeta: metav1.ObjectMeta{Name: "gateway"}},
		},
	)
	if snap.Pods != 2 || snap.Services != 2 || snap.Deployments != 1 {
		t.Fatalf("inventory = %+v", snap)
	}
	if snap.DNSServer != "10.96.0.10" {
		t.Fatalf("dns = %q", snap.DNSServer)
	}
	if len(snap.ServiceIPs) != 2 {
		t.Fatalf("service IPs = %v", snap.ServiceIPs)
	}
}
