//go:build e2e && !windows

package harness

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// KillPrivilegedProcess terminates the core directly when possible and falls
// back to the passwordless sudo configuration supplied by hosted CI runners.
func KillPrivilegedProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err == nil {
		err = process.Kill()
	}
	if err == nil {
		return nil
	}
	output, sudoErr := exec.Command("sudo", "-n", "kill", "-KILL", strconv.Itoa(pid)).CombinedOutput()
	if sudoErr != nil {
		return fmt.Errorf("kill process: %v; sudo kill: %w (%s)", err, sudoErr, output)
	}
	return nil
}
