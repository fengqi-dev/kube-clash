package helper

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func UserHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return home, nil
}

func UserDir() (string, error) {
	home, err := UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kubeloop"), nil
}

func TokenPath() (string, error) {
	dir, err := UserDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "helper.token"), nil
}

func SessionsRoot() (string, error) {
	dir, err := UserDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions"), nil
}

func SystemStateDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), "KubeLoop")
	default:
		return "/var/lib/kubeloop"
	}
}

func SystemTokenPath() string {
	return filepath.Join(SystemStateDir(), "helper.token")
}

func SystemAuthPath() string {
	return filepath.Join(SystemStateDir(), "helper.auth.json")
}

func BinaryInstallPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/PrivilegedHelperTools/" + ServiceLabel
	case "windows":
		return filepath.Join(os.Getenv("ProgramFiles"), "KubeLoop", "kubeloop-helper.exe")
	default:
		return "/usr/local/libexec/kubeloop-helper"
	}
}

func SocketPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(SystemStateDir(), "helper.sock")
	default:
		return "/var/run/kubeloop/helper.sock"
	}
}

func Disabled() bool {
	return os.Getenv("KUBELOOP_HELPER") == "0"
}
