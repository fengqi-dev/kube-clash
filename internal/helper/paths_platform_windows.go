//go:build windows

package helper

import (
	"os"
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
	return filepath.Join(platformInstallRoot(), "resources", HelperBinaryBaseName()+".exe")
}

// platformLegacyBinaryInstallPath is the pre-resources helper location under Program Files.
func platformLegacyBinaryInstallPath() string {
	return filepath.Join(windowsProgramFilesProductRoot(), HelperBinaryBaseName()+".exe")
}

func platformBundledSingBoxPath() string {
	return filepath.Join(platformInstallRoot(), "sing-box.exe")
}

// platformInstallRoot is the desktop app install directory (may be on D: etc).
// Derived from the running executable so NSIS custom InstallDir is respected:
//
//	{root}\KubeLoop.exe
//	{root}\resources\kubeloop-helper.exe
func platformInstallRoot() string {
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		if root := installRootFromWindowsExe(exe); root != "" {
			return root
		}
	}
	return windowsProgramFilesProductRoot()
}

func windowsProgramFilesProductRoot() string {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DEFAULT)
	if err != nil {
		root = `C:\Program Files`
	}
	return filepath.Join(root, InstallProductDir())
}

// windowsDisplacedHelperPaths are older fixed Program Files locations to remove
// after the helper moves with a custom install directory (e.g. D:\KubeLoop).
func windowsDisplacedHelperPaths(current string) []string {
	root := windowsProgramFilesProductRoot()
	candidates := []string{
		filepath.Join(root, HelperBinaryBaseName()+".exe"),
		filepath.Join(root, "resources", HelperBinaryBaseName()+".exe"),
	}
	out := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if current != "" && sameInstallPath(path, current) {
			continue
		}
		out = append(out, path)
	}
	return out
}
