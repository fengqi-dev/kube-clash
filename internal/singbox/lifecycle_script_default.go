//go:build !darwin && !linux && !windows

package singbox

import (
	"fmt"
	"os"
	"os/exec"
)

func startLifecycleScript(binaryPath, workDir string, logFile *os.File) (*exec.Cmd, error) {
	return nil, fmt.Errorf("privileged lifecycle is unsupported on this platform")
}
