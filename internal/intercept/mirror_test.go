package intercept

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

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
