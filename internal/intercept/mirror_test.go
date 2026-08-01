package intercept

import (
	"net"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

func TestPrimaryAddressFromEndpointSlice(t *testing.T) {
	name := "http"
	protocol := corev1.ProtocolTCP
	port := int32(8080)
	ready := true
	address, err := primaryAddressFromSlices(
		[]discoveryv1.EndpointSlice{{
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{{
				Name: &name, Protocol: &protocol, Port: &port,
			}},
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"10.244.0.8"},
				Conditions: discoveryv1.EndpointConditions{
					Ready: &ready,
				},
			}},
		}},
		cluster.InterceptPort{
			Name: "http", Protocol: corev1.ProtocolTCP, ServicePort: 80,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if address != "10.244.0.8:8080" {
		t.Fatalf("address = %q", address)
	}
}

func TestPrimaryAddressMatchesPortName(t *testing.T) {
	addr, err := primaryAddress(
		[]corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{IP: "10.244.0.5"}},
			Ports: []corev1.EndpointPort{{
				Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP,
			}},
		}},
		cluster.InterceptPort{Name: "http", ServicePort: 80, Protocol: corev1.ProtocolTCP},
	)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "10.244.0.5:8080" {
		t.Fatalf("addr=%q", addr)
	}
}

func TestShadowWriterDoesNotBlockWhenLocalTargetStalls(t *testing.T) {
	local, stalledPeer := net.Pipe()
	defer stalledPeer.Close()
	writer := newShadowWriter(local)
	payload := make([]byte, 32<<10)
	started := time.Now()
	for range mirrorShadowQueueSize * 4 {
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("shadow writes blocked primary path for %s", elapsed)
	}
}

func TestPrimaryAddressMatchesUDP(t *testing.T) {
	addr, err := primaryAddress(
		[]corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{IP: "10.244.0.5"}},
			Ports: []corev1.EndpointPort{{
				Name: "dns", Port: 5353, Protocol: corev1.ProtocolUDP,
			}},
		}},
		cluster.InterceptPort{Name: "dns", ServicePort: 53, Protocol: corev1.ProtocolUDP},
	)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "10.244.0.5:5353" {
		t.Fatalf("addr=%q", addr)
	}
}
