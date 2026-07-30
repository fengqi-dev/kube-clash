//go:build !windows

package helper

import (
	"path/filepath"
	"runtime"
)

func platformSystemStateDir() string {
	return "/var/lib/kubeloop"
}

func platformBinaryInstallPath() string {
	if runtime.GOOS == "darwin" {
		return "/Library/PrivilegedHelperTools/" + ServiceLabel
	}
	return "/usr/local/libexec/kubeloop-helper"
}

func platformLegacyBinaryInstallPath() string {
	return ""
}

func platformBundledSingBoxPath() string {
	switch runtime.GOOS {
	case "darwin":
		return ""
	case "linux":
		return "/usr/lib/kubeloop/sing-box"
	default:
		return ""
	}
}

// platformInstallRoot is unused on non-Windows; kept for API symmetry.
func platformInstallRoot() string {
	return filepath.Dir(platformBinaryInstallPath())
}
