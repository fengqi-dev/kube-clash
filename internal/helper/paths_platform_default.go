//go:build !windows

package helper

import "runtime"

func platformSystemStateDir() string {
	return "/var/lib/kubeloop"
}

func platformBinaryInstallPath() string {
	if runtime.GOOS == "darwin" {
		return "/Library/PrivilegedHelperTools/" + ServiceLabel
	}
	return "/usr/local/libexec/kubeloop-helper"
}
