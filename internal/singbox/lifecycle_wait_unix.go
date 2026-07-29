//go:build darwin || linux

package singbox

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// waitLifecycleCleanup waits until the privileged lifecycle has finished tearing
// down sing-box and (on Darwin) cleared KubeLoop /etc/resolver files.
// Needed because older helpers returned from Stop before restore completed.
func waitLifecycleCleanup(workDir string, timeout time.Duration) {
	if workDir == "" || timeout <= 0 {
		return
	}
	deadline := time.Now().Add(timeout)
	pidPath := filepath.Join(workDir, privilegedPIDFilename)
	for time.Now().Before(deadline) {
		if !pidFileProcessAlive(pidPath) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Lifecycle runs split-DNS restore after the child exits; give it time, and
	// on Darwin prefer waiting until resolver leftovers are gone.
	for time.Now().Before(deadline) {
		if !kubeLoopResolversPresent() {
			time.Sleep(150 * time.Millisecond)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func pidFileProcessAlive(pidPath string) bool {
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
