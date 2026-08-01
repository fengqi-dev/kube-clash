//go:build e2e && windows

package harness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/windows"
)

// KillPrivilegedProcess terminates a process owned by the elevated helper.
// The Windows E2E test process is normally unelevated, so taskkill must be
// launched through the native UAC API.
func KillPrivilegedProcess(pid int) error {
	taskkill := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
	verb, _ := windows.UTF16PtrFromString("runas")
	executable, err := windows.UTF16PtrFromString(taskkill)
	if err != nil {
		return err
	}
	arguments, err := windows.UTF16PtrFromString("/PID " + strconv.Itoa(pid) + " /F")
	if err != nil {
		return err
	}
	cwd, err := windows.UTF16PtrFromString(filepath.Dir(taskkill))
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(
		0, verb, executable, arguments, cwd, windows.SW_HIDE,
	); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return fmt.Errorf("Windows elevation was cancelled")
		}
		return fmt.Errorf("launch elevated taskkill: %w", err)
	}
	return nil
}
