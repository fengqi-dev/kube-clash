package singbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	privilegedPIDFilename  = "sing-box.pid"
	privilegedStopFilename = "sing-box.stop"
	lifecycleScriptName    = "kubeloop-lifecycle"
)

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func privilegedPIDPath(workDir string, enabled bool) string {
	if !enabled {
		return ""
	}
	return filepath.Join(workDir, privilegedPIDFilename)
}

func stopPrivilegedProcess(pidPath string) error {
	if _, err := os.Stat(pidPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("find privileged sing-box pid: %w", err)
	}
	stopPath := filepath.Join(filepath.Dir(pidPath), privilegedStopFilename)
	if err := os.WriteFile(stopPath, []byte("stop\n"), 0o600); err != nil {
		return fmt.Errorf("signal privileged sing-box to stop: %w", err)
	}
	return nil
}

func loadTUNRouteAddresses(workDir string) ([]string, error) {
	content, err := os.ReadFile(filepath.Join(workDir, "config.json"))
	if err != nil {
		return nil, fmt.Errorf("read sing-box config for route cleanup: %w", err)
	}
	var current struct {
		Inbounds []struct {
			Type         string   `json:"type"`
			RouteAddress []string `json:"route_address"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(content, &current); err != nil {
		return nil, fmt.Errorf("parse sing-box config for route cleanup: %w", err)
	}
	var routes []string
	for _, inbound := range current.Inbounds {
		if inbound.Type != "tun" {
			continue
		}
		routes = append(routes, inbound.RouteAddress...)
	}
	return routes, nil
}

func staleManagedPIDsWith(
	binaryPath string,
	workDir string,
	processCommand func(string) (string, error),
) []string {
	sessionRoot := filepath.Dir(workDir)
	entries, err := os.ReadDir(sessionRoot)
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionDir := filepath.Join(sessionRoot, entry.Name())
		if sessionDir == workDir {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(sessionDir, privilegedPIDFilename))
		if readErr != nil {
			continue
		}
		pid := strings.TrimSpace(string(content))
		if _, parseErr := strconv.Atoi(pid); parseErr != nil {
			continue
		}
		command, commandErr := processCommand(pid)
		if commandErr != nil {
			continue
		}
		if strings.Contains(command, binaryPath) && strings.Contains(command, sessionDir) {
			result = append(result, pid)
		}
	}
	return result
}
