package socksbridge

import (
	"bytes"
	"testing"
)

func TestSOCKSUDPPacketRoundTrip(t *testing.T) {
	want := []byte("dns payload")
	packet, err := encodeUDPPacket("10.96.0.10", 53, want)
	if err != nil {
		t.Fatal(err)
	}
	host, port, got, err := parseUDPPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if host != "10.96.0.10" || port != 53 || !bytes.Equal(got, want) {
		t.Fatalf("got %s:%d %q", host, port, got)
	}
}

func TestSOCKSUDPDomainRoundTrip(t *testing.T) {
	packet, err := encodeUDPPacket("kube-dns.kube-system.svc.cluster.local", 53, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	host, port, _, err := parseUDPPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if host != "kube-dns.kube-system.svc.cluster.local" || port != 53 {
		t.Fatalf("got %s:%d", host, port)
	}
}
