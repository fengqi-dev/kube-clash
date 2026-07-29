//go:build linux

package singbox

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"strings"
)

func defaultStartCommand(binaryPath, workDir string, output io.Writer) (*exec.Cmd, error) {
	logFile, ok := output.(*os.File)
	if !ok {
		return nil, errors.New("start privileged sing-box: log output is not a file")
	}
	routeCleanup, err := managedRouteCleanupCommands(workDir)
	if err != nil {
		return nil, err
	}
	scriptPath, err := writeUnixLifecycleScript(binaryPath, workDir, logFile.Name(), routeCleanup)
	if err != nil {
		return nil, err
	}
	if os.Geteuid() == 0 {
		cmd := exec.Command("/bin/sh", scriptPath)
		cmd.Stdout = io.Discard
		cmd.Stderr = output
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start sing-box: %w", err)
		}
		return cmd, nil
	}
	elevate := "pkexec"
	if _, lookErr := exec.LookPath("pkexec"); lookErr != nil {
		elevate = "sudo"
	}
	cmd := exec.Command(elevate, "/bin/sh", scriptPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("request administrator access for sing-box TUN (%s): %w", elevate, err)
	}
	return cmd, nil
}

func managedRouteCleanupCommands(workDir string) (string, error) {
	routes, err := loadTUNRouteAddresses(workDir)
	if err != nil {
		return "", err
	}
	var commands strings.Builder
	for _, raw := range routes {
		prefix, parseErr := netip.ParsePrefix(raw)
		if parseErr != nil {
			return "", fmt.Errorf("parse managed route %q: %w", raw, parseErr)
		}
		fmt.Fprintf(
			&commands,
			"/sbin/ip route del %s >/dev/null 2>&1 || /usr/sbin/ip route del %s >/dev/null 2>&1 || true; ",
			prefix.Masked(), prefix.Masked(),
		)
	}
	return commands.String(), nil
}
