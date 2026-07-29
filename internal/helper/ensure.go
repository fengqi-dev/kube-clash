package helper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// GetStatus probes whether the helper is installed and reachable.
func GetStatus(ctx context.Context) Status {
	status := Status{
		Expected: Version,
		Socket:   SocketPath(),
	}
	if Disabled() {
		status.Error = "disabled by KUBELOOP_HELPER=0"
		return status
	}
	if _, err := os.Stat(BinaryInstallPath()); err == nil {
		status.Installed = true
	}
	token, err := ReadUserToken()
	if err != nil {
		status.Error = err.Error()
		return status
	}
	client := &Client{Token: token, Dial: dialHelper}
	pingCtx, cancel := withDialTimeout(ctx)
	defer cancel()
	response, err := client.Ping(pingCtx)
	if err != nil {
		if status.Error == "" {
			status.Error = err.Error()
		}
		return status
	}
	status.Running = true
	status.Version = response.Version
	return status
}

// EnsureInstall installs or upgrades the helper when missing/outdated, then waits for ping.
func EnsureInstall(ctx context.Context) error {
	if Disabled() {
		return fmt.Errorf("helper disabled by KUBELOOP_HELPER=0")
	}
	status := GetStatus(ctx)
	if status.Running && status.Version == Version {
		return nil
	}
	source, err := LocateBundledHelper()
	if err != nil {
		return err
	}
	token, err := EnsureUserToken()
	if err != nil {
		return err
	}
	home, err := UserHomeDir()
	if err != nil {
		return err
	}
	if err := ElevateInstall(ctx, source, token, currentUID(), home); err != nil {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		client := &Client{Token: token, Dial: dialHelper}
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := client.Ping(pingCtx)
		cancel()
		if err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("helper did not become ready after install")
}

// Uninstall removes the helper service (requires elevation).
func Uninstall(ctx context.Context) error {
	source, err := LocateBundledHelper()
	if err != nil {
		// Fall back to installed binary for uninstall.
		source = BinaryInstallPath()
	}
	return ElevateUninstall(ctx, source)
}

func LocateBundledHelper() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	name := "kubeloop-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidates := []string{
		filepath.Join(dir, name),
		filepath.Join(dir, "Helpers", name),
		filepath.Join(dir, "..", "Helpers", name),
		filepath.Join(dir, "..", "Resources", name),
		filepath.Join("build", "bin", name),
	}
	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "build", "bin", name),
			filepath.Join(cwd, name),
		)
	}
	for _, candidate := range candidates {
		info, statErr := os.Stat(candidate)
		if statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if path, lookErr := exec.LookPath(name); lookErr == nil {
		return path, nil
	}
	return "", fmt.Errorf("kubeloop-helper binary not found near %s", exe)
}

func installArgs(source, token string, uid int, homeDir string) []string {
	return []string{
		"install",
		"--source", source,
		"--token", token,
		"--uid", strconv.Itoa(uid),
		"--version", Version,
		"--home", homeDir,
	}
}
