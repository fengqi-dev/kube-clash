//go:build windows

package helper

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func platformSystemStateDir() string {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		root = `C:\ProgramData`
	}
	return filepath.Join(root, "KubeLoop")
}

func platformBinaryInstallPath() string {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DEFAULT)
	if err != nil {
		root = `C:\Program Files`
	}
	return filepath.Join(root, "KubeLoop", "kubeloop-helper.exe")
}
