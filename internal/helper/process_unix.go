//go:build darwin || linux

package helper

import "os"

func stopManagedProcess(process *os.Process) error {
	return process.Signal(os.Interrupt)
}
