//go:build !darwin && !linux && !windows

package singbox

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
)

func defaultStartCommand(binaryPath, workDir string, output io.Writer) (*exec.Cmd, error) {
	cmd := exec.Command(
		binaryPath, "run",
		"-c", filepath.Join(workDir, "config.json"),
		"-D", workDir,
	)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start sing-box: %w", err)
	}
	return cmd, nil
}
