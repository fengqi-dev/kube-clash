//go:build darwin || linux

package singbox

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

func startLifecycleScript(binaryPath, workDir string, logFile *os.File) (*exec.Cmd, error) {
	routeCleanup, err := managedRouteCleanupCommands(workDir)
	if err != nil {
		return nil, err
	}
	scriptPath, err := writeUnixLifecycleScript(binaryPath, workDir, logFile.Name(), routeCleanup)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start sing-box lifecycle: %w", err)
	}
	return cmd, nil
}
