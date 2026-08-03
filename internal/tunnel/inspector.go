package tunnel

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

const (
	CtrlInspectorStart         byte = 6
	CtrlInspectorUpdateTargets byte = 7
	CtrlInspectorStop          byte = 8

	MaxInspectorTargets        = 128
	maxInspectorPEMSize        = 256 << 10
	maxInspectorDescriptorSize = 32 << 10
)

type InspectorTarget struct {
	ID            string   `json:"id"`
	Host          string   `json:"host"`
	Port          uint16   `json:"port"`
	Protocol      string   `json:"protocol"`
	CaptureBody   bool     `json:"captureBody,omitempty"`
	DescriptorSet []byte   `json:"descriptorSet,omitempty"`
	Namespace     string   `json:"namespace,omitempty"`
	Service       string   `json:"service,omitempty"`
	ServiceUID    string   `json:"serviceUID,omitempty"`
	Addresses     []string `json:"addresses,omitempty"`
	FlowSource    string   `json:"-"`
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
		switch strings.ToLower(strings.TrimSpace(target.Protocol)) {
		case "https", "http2", "grpc":
			needsTLS = true
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
	descriptorBytes := 0
	for index := range targets {
		target := &targets[index]
		target.ID = strings.TrimSpace(target.ID)
		target.Host = strings.ToLower(strings.TrimSpace(target.Host))
		target.Protocol = strings.ToLower(strings.TrimSpace(target.Protocol))
		target.Namespace = strings.ToLower(strings.TrimSpace(target.Namespace))
		target.Service = strings.ToLower(strings.TrimSpace(target.Service))
		target.ServiceUID = strings.TrimSpace(target.ServiceUID)
		if target.ID == "" || len(target.ID) > maxIDSize {
			return fmt.Errorf("Inspector target %d ID length is invalid", index)
		}
		if target.Host == "" || len(target.Host) > maxHostSize {
			return fmt.Errorf("Inspector target %q host length is invalid", target.ID)
		}
		if target.Port == 0 {
			return fmt.Errorf("Inspector target %q port is required", target.ID)
		}
		if target.Protocol != "http" && target.Protocol != "https" &&
			target.Protocol != "http2" && target.Protocol != "grpc" {
			return fmt.Errorf(
				"Inspector target %q protocol %q is unsupported",
				target.ID, target.Protocol,
			)
		}
		hasServicePolicy := target.Namespace != "" || target.Service != "" ||
			target.ServiceUID != "" || len(target.Addresses) > 0
		if hasServicePolicy {
			if target.Namespace == "" || target.Service == "" ||
				target.ServiceUID == "" || len(target.Addresses) == 0 {
				return fmt.Errorf(
					"Inspector target %q Service policy is incomplete", target.ID,
				)
			}
			expectedHost := target.Service + "." + target.Namespace + ".svc"
			if target.Host != expectedHost {
				return fmt.Errorf(
					"Inspector target %q host must be %s", target.ID, expectedHost,
				)
			}
			seenAddresses := make(map[string]struct{}, len(target.Addresses))
			normalized := make([]string, 0, len(target.Addresses))
			for _, rawAddress := range target.Addresses {
				address, err := netip.ParseAddr(strings.TrimSpace(rawAddress))
				if err != nil {
					return fmt.Errorf(
						"Inspector target %q Service address %q is invalid",
						target.ID, rawAddress,
					)
				}
				value := address.Unmap().String()
				if _, exists := seenAddresses[value]; exists {
					continue
				}
				seenAddresses[value] = struct{}{}
				normalized = append(normalized, value)
			}
			target.Addresses = normalized
		}
		descriptorBytes += len(target.DescriptorSet)
		if descriptorBytes > maxInspectorDescriptorSize {
			return fmt.Errorf(
				"Inspector descriptor sets exceed %d KiB",
				maxInspectorDescriptorSize>>10,
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
