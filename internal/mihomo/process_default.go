//go:build !darwin

package mihomo

import (
	"fmt"
	"io"
	"os/exec"
)

func defaultStartCommand(binaryPath, workDir string, output io.Writer) (*exec.Cmd, error) {
	cmd := exec.Command(binaryPath, "-d", workDir)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mihomo: %w", err)
	}
	return cmd, nil
}

func privilegedPIDPath(_ string, _ bool) string {
	return ""
}

func stopPrivilegedProcess(_ string) error {
	return nil
}
