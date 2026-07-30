//go:build windows

package helper

import "os"

func stopManagedProcess(process *os.Process) error {
	return process.Kill()
}
