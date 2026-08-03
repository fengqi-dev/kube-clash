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
)

type InspectorTarget struct {
	ID          string `json:"id"`
	Host        string `json:"host"`
	Port        uint16 `json:"port"`
	Protocol    string `json:"protocol"`
	CaptureBody bool   `json:"captureBody,omitempty"`
}

type InspectorConfig struct {
	MaxBodySize int64             `json:"maxBodySize"`
	Targets     []InspectorTarget `json:"targets"`
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
	return ValidateInspectorTargets(c.Targets)
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
		if target.Protocol != "http" {
			return fmt.Errorf(
				"Inspector target %q protocol %q is unsupported in Phase 1",
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
