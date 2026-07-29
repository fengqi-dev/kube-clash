package cluster

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestApplyAndRestoreServiceIntercept(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
			Spec: corev1.ServiceSpec{
				ClusterIP: "10.96.10.20",
				Selector:  map[string]string{"app": "api"},
				Ports: []corev1.ServicePort{{
					Name: "http", Port: 80, Protocol: corev1.ProtocolTCP,
				}},
			},
		},
		&corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{IP: "10.244.0.5"}},
				Ports:     []corev1.EndpointPort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP}},
			}},
		},
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "api-xyz",
				Namespace: "default",
				Labels:    map[string]string{interceptServiceNameLabel: "api"},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.244.0.5"}}},
		},
	)

	snapshot := &ServiceInterceptSnapshot{
		Namespace: "default",
		Service:   "api",
		Selector:  map[string]string{"app": "api"},
		GatewayIP: "10.244.0.9",
		Ports: []InterceptPort{{
			Name: "http", Protocol: corev1.ProtocolTCP, ServicePort: 80, ListenPort: 20080,
		}},
	}
	if err := applyServiceIntercept(context.Background(), client, snapshot, "id-1"); err != nil {
		t.Fatal(err)
	}

	service, err := client.CoreV1().Services("default").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(service.Spec.Selector) != 0 {
		t.Fatalf("selector not cleared: %#v", service.Spec.Selector)
	}
	if service.Annotations[annotationInterceptID] != "id-1" {
		t.Fatalf("missing intercept annotation")
	}
	if !snapshot.HasEndpoints || len(snapshot.EndpointsSubsets) != 1 {
		t.Fatalf("endpoints not snapshotted: %#v", snapshot)
	}
	if snapshot.EndpointsSubsets[0].Addresses[0].IP != "10.244.0.5" {
		t.Fatalf("unexpected snapshotted address: %#v", snapshot.EndpointsSubsets)
	}

	_, err = client.CoreV1().Endpoints("default").Get(context.Background(), "api", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("classic endpoints should be deleted, got %v", err)
	}

	slices, err := client.DiscoveryV1().EndpointSlices("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(slices.Items) != 1 {
		t.Fatalf("slice count=%d", len(slices.Items))
	}
	if slices.Items[0].Endpoints[0].Addresses[0] != "10.244.0.9" {
		t.Fatalf("gateway IP not applied")
	}
	if *slices.Items[0].Ports[0].Port != 20080 {
		t.Fatalf("listen port=%d", *slices.Items[0].Ports[0].Port)
	}

	if err := restoreServiceIntercept(context.Background(), client, *snapshot); err != nil {
		t.Fatal(err)
	}
	service, err = client.CoreV1().Services("default").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if service.Spec.Selector["app"] != "api" {
		t.Fatalf("selector not restored: %#v", service.Spec.Selector)
	}
	if service.Annotations[annotationInterceptID] != "" {
		t.Fatalf("intercept annotation still present")
	}

	endpoints, err := client.CoreV1().Endpoints("default").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints.Subsets) != 1 || endpoints.Subsets[0].Addresses[0].IP != "10.244.0.5" {
		t.Fatalf("endpoints not restored: %#v", endpoints.Subsets)
	}

	_, err = client.DiscoveryV1().EndpointSlices("default").Get(
		context.Background(), managedEndpointSliceName("api"), metav1.GetOptions{},
	)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("managed slice should be deleted, got %v", err)
	}
}

func TestBuildInterceptPorts(t *testing.T) {
	service := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
				{Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP},
			},
		},
	}
	next := int32(20000)
	ports, err := BuildInterceptPorts(service, func(corev1.Protocol) (int32, error) {
		next++
		return next, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 2 || ports[0].ListenPort != 20001 || ports[1].ListenPort != 20002 {
		t.Fatalf("unexpected ports %#v", ports)
	}
}
