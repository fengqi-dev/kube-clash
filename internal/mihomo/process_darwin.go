package mihomo

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	privilegedPIDFilename  = "mihomo.pid"
	privilegedStopFilename = "mihomo.stop"
)

func defaultStartCommand(binaryPath, workDir string, output io.Writer) (*exec.Cmd, error) {
	if os.Geteuid() == 0 {
		cmd := exec.Command(binaryPath, "-d", workDir)
		cmd.Stdout = output
		cmd.Stderr = output
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start Mihomo: %w", err)
		}
		return cmd, nil
	}
	logFile, ok := output.(*os.File)
	if !ok {
		return nil, errors.New("start privileged mihomo: log output is not a file")
	}
	pidPath := filepath.Join(workDir, privilegedPIDFilename)
	stopPath := filepath.Join(workDir, privilegedStopFilename)
	stalePIDs := staleManagedPIDs(binaryPath, workDir)
	stopStale := ""
	for _, pid := range stalePIDs {
		stopStale += fmt.Sprintf(
			"kill -INT %s 2>/dev/null || true; "+
				"for i in $(seq 1 50); do kill -0 %s 2>/dev/null || break; sleep 0.1; done; "+
				"kill -TERM %s 2>/dev/null || true; ",
			pid,
			pid,
			pid,
		)
	}
	cleanupRoutes, err := managedRouteCleanupCommands(workDir)
	if err != nil {
		return nil, err
	}
	shellCommand := fmt.Sprintf(
		"%s%srm -f %s; %s -d %s >> %s 2>&1 & child=$!; "+
			"echo $child > %s; "+
			"while kill -0 $child 2>/dev/null; do "+
			"if [ -f %s ]; then kill -INT $child 2>/dev/null || true; break; fi; "+
			"sleep 0.2; done; "+
			"status=0; wait $child || status=$?; exit $status",
		stopStale,
		cleanupRoutes,
		shellQuote(stopPath),
		shellQuote(binaryPath),
		shellQuote(workDir),
		shellQuote(logFile.Name()),
		shellQuote(pidPath),
		shellQuote(stopPath),
	)
	script := "do shell script " + strconv.Quote(shellCommand) + " with administrator privileges"
	cmd := exec.Command("osascript", "-e", script)
	cmd.Stdout = io.Discard
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("request administrator access for Mihomo TUN: %w", err)
	}
	return cmd, nil
}

func managedRouteCleanupCommands(workDir string) (string, error) {
	content, err := os.ReadFile(filepath.Join(workDir, "config.yaml"))
	if err != nil {
		return "", fmt.Errorf("read Mihomo config for route cleanup: %w", err)
	}
	var current config
	if err := yaml.Unmarshal(content, &current); err != nil {
		return "", fmt.Errorf("parse Mihomo config for route cleanup: %w", err)
	}
	var commands strings.Builder
	var routes []string
	if current.TUN != nil {
		routes = append(routes, current.TUN.RouteAddress...)
	}
	for _, listener := range current.Listeners {
		if listener.Type == "tun" {
			routes = append(routes, listener.RouteAddress...)
		}
	}
	for _, raw := range routes {
		prefix, parseErr := netip.ParsePrefix(raw)
		if parseErr != nil {
			return "", fmt.Errorf("parse managed route %q: %w", raw, parseErr)
		}
		prefix = prefix.Masked()
		if prefix.Bits() == prefix.Addr().BitLen() {
			fmt.Fprintf(
				&commands,
				"/sbin/route -n delete -host %s >/dev/null 2>&1 || true; ",
				prefix.Addr(),
			)
		} else {
			fmt.Fprintf(
				&commands,
				"/sbin/route -n delete -net %s >/dev/null 2>&1 || true; ",
				prefix,
			)
		}
	}
	return commands.String(), nil
}

func staleManagedPIDs(binaryPath, workDir string) []string {
	return staleManagedPIDsWith(binaryPath, workDir, func(pid string) (string, error) {
		command, err := exec.Command("ps", "-p", pid, "-o", "command=").Output()
		return string(command), err
	})
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
		if strings.Contains(command, binaryPath) && strings.Contains(command, "-d "+sessionDir) {
			result = append(result, pid)
		}
	}
	return result
}

func privilegedPIDPath(workDir string, enabled bool) string {
	if !enabled || os.Geteuid() == 0 {
		return ""
	}
	return filepath.Join(workDir, privilegedPIDFilename)
}

func stopPrivilegedProcess(pidPath string) error {
	if _, err := os.Stat(pidPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("find privileged Mihomo pid: %w", err)
	}
	stopPath := filepath.Join(filepath.Dir(pidPath), privilegedStopFilename)
	if err := os.WriteFile(stopPath, []byte("stop\n"), 0o600); err != nil {
		return fmt.Errorf("signal privileged Mihomo to stop: %w", err)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
