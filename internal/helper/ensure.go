package helper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

var (
	bundledBinaryMu      sync.RWMutex
	bundledBinary        []byte
	materializeBundledMu sync.Mutex
	ensureInstallMu      sync.Mutex
)

// SetBundledBinary supplies the platform helper embedded by the desktop
// application. The standalone helper binary never calls it.
func SetBundledBinary(content []byte) {
	bundledBinaryMu.Lock()
	defer bundledBinaryMu.Unlock()
	bundledBinary = bytes.Clone(content)
}

// GetStatus probes whether the helper is installed and reachable.
func GetStatus(ctx context.Context) Status {
	status := Status{
		Expected: Version,
		Socket:   SocketPath(),
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
	status.Protocol = response.Protocol
	return status
}

// EnsureInstall installs or upgrades the helper when missing/outdated, then waits for ping.
func EnsureInstall(ctx context.Context) error {
	ensureInstallMu.Lock()
	defer ensureInstallMu.Unlock()

	status := GetStatus(ctx)
	if status.Running && status.Version == Version &&
		status.Protocol == ProtocolVersion {
		return nil
	}
	source, err := LocateBundledHelper()
	if err != nil {
		return err
	}
	sourceSHA256, err := bundledHelperSHA256(source)
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
	if err := ElevateInstall(ctx, source, sourceSHA256, token, currentUID(), home); err != nil {
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
		response, err := client.Ping(pingCtx)
		cancel()
		if err == nil && response.Protocol == ProtocolVersion {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("helper did not become ready after install")
}

// Uninstall removes the helper service (requires elevation).
func Uninstall(ctx context.Context) error {
	// The installed binary lives in an administrator-owned location. Never run
	// the materialized, user-writable copy with elevated privileges.
	return ElevateUninstall(ctx, BinaryInstallPath())
}

func LocateBundledHelper() (string, error) {
	if path, ok, err := materializeBundledHelper(); ok || err != nil {
		return path, err
	}

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
	// Prefer Resources (macOS Contents/Resources, or a Resources/ next to the binary).
	// Keep Helpers / same-dir fallbacks for older packages and local builds.
	candidates := []string{
		filepath.Join(dir, "..", "Resources", name), // macOS: Contents/MacOS/../Resources
		filepath.Join(dir, "Resources", name),
		filepath.Join(dir, name),
		filepath.Join(dir, "Helpers", name),
		filepath.Join(dir, "..", "Helpers", name),
		filepath.Join("build", "bin", "Resources", name),
		filepath.Join("build", "bin", name),
	}
	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "build", "bin", "Resources", name),
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

func materializeBundledHelper() (string, bool, error) {
	materializeBundledMu.Lock()
	defer materializeBundledMu.Unlock()

	bundledBinaryMu.RLock()
	content := bytes.Clone(bundledBinary)
	bundledBinaryMu.RUnlock()
	if len(content) == 0 {
		return "", false, nil
	}

	dir, err := UserDir()
	if err != nil {
		return "", true, err
	}
	dir = filepath.Join(dir, "helper")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", true, fmt.Errorf("create bundled helper directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil && runtime.GOOS != "windows" {
		return "", true, fmt.Errorf("secure bundled helper directory: %w", err)
	}

	name := "kubeloop-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if current, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(current, content) {
		if err := os.Chmod(path, 0o700); err != nil && runtime.GOOS != "windows" {
			return "", true, fmt.Errorf("make bundled helper executable: %w", err)
		}
		return path, true, nil
	}

	temp, err := os.CreateTemp(dir, ".kubeloop-helper-*")
	if err != nil {
		return "", true, fmt.Errorf("create bundled helper: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o700); err != nil && runtime.GOOS != "windows" {
		_ = temp.Close()
		return "", true, fmt.Errorf("make temporary helper executable: %w", err)
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return "", true, fmt.Errorf("write bundled helper: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", true, fmt.Errorf("close bundled helper: %w", err)
	}
	// Removing first also makes replacement work on Windows, where Rename does
	// not replace an existing destination.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", true, fmt.Errorf("replace bundled helper: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return "", true, fmt.Errorf("install bundled helper: %w", err)
	}
	return path, true, nil
}

func bundledHelperSHA256(source string) (string, error) {
	bundledBinaryMu.RLock()
	if len(bundledBinary) > 0 {
		sum := sha256.Sum256(bundledBinary)
		bundledBinaryMu.RUnlock()
		return fmt.Sprintf("%x", sum), nil
	}
	bundledBinaryMu.RUnlock()

	file, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open bundled helper for hashing: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash bundled helper: %w", err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
