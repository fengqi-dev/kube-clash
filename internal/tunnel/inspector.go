package tunnel

import (
	"errors"
	"fmt"
	"strings"
)

const (
	CtrlInspectorStart         byte = 6
	CtrlInspectorUpdateTargets byte = 7
	CtrlInspectorStop          byte = 8

	MaxInspectorTargets = 128
	maxInspectorPEMSize = 256 << 10
)

type InspectorTarget struct {
	ID          string `json:"id"`
	Host        string `json:"host"`
	Port        uint16 `json:"port"`
	Protocol    string `json:"protocol"`
	CaptureBody bool   `json:"captureBody,omitempty"`
}

type InspectorConfig struct {
	MaxBodySize int64               `json:"maxBodySize"`
	Targets     []InspectorTarget   `json:"targets"`
	TLS         *InspectorTLSConfig `json:"tls,omitempty"`
}

// InspectorTLSConfig contains a session-scoped Intermediate CA. The Root CA
// private key never leaves the desktop OS keyring.
type InspectorTLSConfig struct {
	CertificatePEM []byte `json:"certificatePEM"`
	PrivateKeyPEM  []byte `json:"privateKeyPEM"`
	ChainPEM       []byte `json:"chainPEM,omitempty"`
	UpstreamCAPEM  []byte `json:"upstreamCAPEM,omitempty"`
}

type InspectorState struct {
	Active      bool              `json:"active"`
	MaxBodySize int64             `json:"maxBodySize"`
	Targets     []InspectorTarget `json:"targets"`
}

func (c InspectorConfig) Validate() error {
	if c.MaxBodySize < 0 || c.MaxBodySize > 16<<20 {
		return errors.New("Inspector max body size must be between 0 and 16 MiB")
	}
	if err := ValidateInspectorTargets(c.Targets); err != nil {
		return err
	}
	needsTLS := false
	for _, target := range c.Targets {
		if strings.EqualFold(strings.TrimSpace(target.Protocol), "https") {
			needsTLS = true
			break
		}
	}
	if !needsTLS {
		return nil
	}
	if c.TLS == nil {
		return errors.New("HTTPS Inspector targets require session TLS material")
	}
	return c.TLS.Validate()
}

func (c InspectorTLSConfig) Validate() error {
	if len(c.CertificatePEM) == 0 || len(c.PrivateKeyPEM) == 0 || len(c.ChainPEM) == 0 {
		return errors.New("Inspector TLS certificate, private key, and CA chain are required")
	}
	for name, value := range map[string][]byte{
		"certificate": c.CertificatePEM,
		"private key": c.PrivateKeyPEM,
		"chain":       c.ChainPEM,
		"upstream CA": c.UpstreamCAPEM,
	} {
		if len(value) > maxInspectorPEMSize {
			return fmt.Errorf("Inspector TLS %s exceeds 256 KiB", name)
		}
	}
	return nil
}

func ValidateInspectorTargets(targets []InspectorTarget) error {
	if len(targets) > MaxInspectorTargets {
		return fmt.Errorf("Inspector target count exceeds %d", MaxInspectorTargets)
	}
	seen := make(map[string]struct{}, len(targets))
	for index := range targets {
		target := &targets[index]
		target.ID = strings.TrimSpace(target.ID)
		target.Host = strings.ToLower(strings.TrimSpace(target.Host))
		target.Protocol = strings.ToLower(strings.TrimSpace(target.Protocol))
		if target.ID == "" || len(target.ID) > maxIDSize {
			return fmt.Errorf("Inspector target %d ID length is invalid", index)
		}
		if target.Host == "" || len(target.Host) > maxHostSize {
			return fmt.Errorf("Inspector target %q host length is invalid", target.ID)
		}
		if target.Port == 0 {
			return fmt.Errorf("Inspector target %q port is required", target.ID)
		}
		if target.Protocol != "http" && target.Protocol != "https" {
			return fmt.Errorf(
				"Inspector target %q protocol %q is unsupported",
				target.ID, target.Protocol,
			)
		}
		key := InspectorTargetKey(target.Host, target.Port)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate Inspector target %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func InspectorTargetKey(host string, port uint16) string {
	return fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(host)), port)
}
