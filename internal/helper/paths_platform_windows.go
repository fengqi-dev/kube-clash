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
	return filepath.Join(root, InstallProductDir())
}

func platformBinaryInstallPath() string {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DEFAULT)
	if err != nil {
		root = `C:\Program Files`
	}
	// Clash Verge-style layout: Program Files\KubeLoop\resources\kubeloop-helper.exe
	return filepath.Join(root, InstallProductDir(), "resources", HelperBinaryBaseName()+".exe")
}

// platformLegacyBinaryInstallPath is the pre-resources helper location.
func platformLegacyBinaryInstallPath() string {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DEFAULT)
	if err != nil {
		root = `C:\Program Files`
	}
	return filepath.Join(root, InstallProductDir(), HelperBinaryBaseName()+".exe")
}

func platformBundledSingBoxPath() string {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DEFAULT)
	if err != nil {
		root = `C:\Program Files`
	}
	return filepath.Join(root, InstallProductDir(), "sing-box.exe")
}

func platformInstallRoot() string {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DEFAULT)
	if err != nil {
		root = `C:\Program Files`
	}
	return filepath.Join(root, InstallProductDir())
}
