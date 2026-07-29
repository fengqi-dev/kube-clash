//go:build darwin

package singbox

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
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
	script := "do shell script " + strconv.Quote("/bin/sh "+shellQuote(scriptPath)) +
		" with administrator privileges"
	cmd := exec.Command("osascript", "-e", script)
	cmd.Stdout = io.Discard
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("request administrator access for sing-box TUN: %w", err)
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
