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
