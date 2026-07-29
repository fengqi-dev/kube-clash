package singbox

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// StartLifecycleDirect runs the privileged lifecycle without elevation.
// The caller must already have sufficient privileges (helper service / root).
func StartLifecycleDirect(binaryPath, workDir string, output io.Writer) (*exec.Cmd, error) {
	return startLifecycleCommand(binaryPath, workDir, output, false)
}

// StartLifecycleElevated runs the privileged lifecycle with a platform elevation prompt.
func StartLifecycleElevated(binaryPath, workDir string, output io.Writer) (*exec.Cmd, error) {
	return startLifecycleCommand(binaryPath, workDir, output, true)
}

// SignalLifecycleStop asks a lifecycle wrapper to stop sing-box for workDir.
func SignalLifecycleStop(workDir string) error {
	return stopPrivilegedProcess(filepath.Join(workDir, privilegedPIDFilename))
}

func startLifecycleCommand(
	binaryPath, workDir string, output io.Writer, elevate bool,
) (*exec.Cmd, error) {
	if !usesLifecycleWrapper() {
		cmd := exec.Command(binaryPath, "run", "-c", "config.json", "-D", workDir)
		cmd.Stdout = output
		cmd.Stderr = output
		cmd.Dir = workDir
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start sing-box: %w", err)
		}
		return cmd, nil
	}
	if elevate {
		return defaultStartCommand(binaryPath, workDir, output)
	}
	return startLifecycleUnprivileged(binaryPath, workDir, output)
}

func startLifecycleUnprivileged(binaryPath, workDir string, output io.Writer) (*exec.Cmd, error) {
	logFile, ok := output.(*os.File)
	if !ok {
		return nil, fmt.Errorf("start privileged sing-box: log output is not a file")
	}
	return startLifecycleScript(binaryPath, workDir, logFile)
}
