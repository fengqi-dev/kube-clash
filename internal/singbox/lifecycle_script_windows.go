//go:build windows

package singbox

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

func startLifecycleScript(binaryPath, workDir string, logFile *os.File) (*exec.Cmd, error) {
	scriptPath, err := writeWindowsLifecycleScript(binaryPath, workDir)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(
		"powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath,
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start sing-box lifecycle: %w", err)
	}
	return cmd, nil
}
