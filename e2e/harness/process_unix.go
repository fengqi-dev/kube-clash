//go:build e2e && !windows

package harness

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// KillPrivilegedProcess terminates the core directly when possible and falls
// back to passwordless sudo (CI) or a macOS admin dialog for local runs.
func KillPrivilegedProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err == nil {
		err = process.Kill()
	}
	if err == nil {
		return nil
	}

	output, sudoErr := exec.Command("sudo", "-n", "kill", "-KILL", strconv.Itoa(pid)).CombinedOutput()
	if sudoErr == nil {
		return nil
	}

	if runtime.GOOS == "darwin" {
		script := "do shell script " + strconv.Quote("kill -KILL "+strconv.Itoa(pid)) +
			" with administrator privileges"
		osaOut, osaErr := exec.Command("osascript", "-e", script).CombinedOutput()
		if osaErr == nil {
			return nil
		}
		return fmt.Errorf("kill process: %v; sudo kill: %w (%s); osascript: %v (%s)",
			err, sudoErr, strings.TrimSpace(string(output)), osaErr, strings.TrimSpace(string(osaOut)))
	}

	return fmt.Errorf("kill process: %v; sudo kill: %w (%s)", err, sudoErr, strings.TrimSpace(string(output)))
}
