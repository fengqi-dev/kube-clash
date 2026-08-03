package tunnel

import (
	"strings"
	"testing"
)

func TestInspectorConfigHTTPSRequiresTLSMaterial(t *testing.T) {
	config := InspectorConfig{
		Targets: []InspectorTarget{{
			ID: "https-target", Host: "api.default.svc", Port: 443, Protocol: "https",
		}},
	}
	if err := config.Validate(); err == nil {
		t.Fatal("expected HTTPS target without TLS material to fail")
	}
	config.TLS = &InspectorTLSConfig{
		CertificatePEM: []byte("certificate"),
		PrivateKeyPEM:  []byte("private key"),
	}
	if err := config.Validate(); err == nil {
		t.Fatal("expected HTTPS target without CA chain to fail")
	}
	config.TLS.ChainPEM = []byte("chain")
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestInspectorTLSConfigRejectsOversizedPEM(t *testing.T) {
	config := InspectorTLSConfig{
		CertificatePEM: []byte("certificate"),
		PrivateKeyPEM:  []byte("private key"),
		ChainPEM:       []byte("chain"),
		UpstreamCAPEM:  []byte(strings.Repeat("x", maxInspectorPEMSize+1)),
	}
	if err := config.Validate(); err == nil {
		t.Fatal("expected oversized upstream CA PEM to fail")
	}
}

func TestInspectorServicePolicyValidationAndNormalization(t *testing.T) {
	targets := []InspectorTarget{{
		ID: "api", Host: "api.default.svc", Port: 443, Protocol: "https",
		Namespace: "DEFAULT", Service: "API", ServiceUID: "uid-1",
		Addresses: []string{"10.96.0.1", "::ffff:10.96.0.1", "fd00::1"},
	}}
	if err := ValidateInspectorTargets(targets); err != nil {
		t.Fatal(err)
	}
	if targets[0].Host != "api.default.svc" ||
		len(targets[0].Addresses) != 2 ||
		targets[0].Addresses[0] != "10.96.0.1" ||
		targets[0].Addresses[1] != "fd00::1" {
		t.Fatalf("normalized target = %#v", targets[0])
	}
	targets[0].ServiceUID = ""
	if err := ValidateInspectorTargets(targets); err == nil {
		t.Fatal("expected incomplete Service policy to fail")
	}
}
