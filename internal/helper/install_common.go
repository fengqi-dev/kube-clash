package helper

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(dst, mode)
}

func InstallFromCLI(source, token string, uid int, version, homeDir string) error {
	if source == "" {
		return fmt.Errorf("--source is required")
	}
	if token == "" {
		return fmt.Errorf("--token is required")
	}
	if version == "" {
		version = Version
	}
	if homeDir == "" {
		return fmt.Errorf("--home is required")
	}
	if err := copyFile(source, BinaryInstallPath(), 0o755); err != nil {
		return fmt.Errorf("install helper binary: %w", err)
	}
	if err := WriteSystemAuth(AuthFile{
		Token: token, UID: uid, Version: version, HomeDir: homeDir,
	}); err != nil {
		return err
	}
	return enableService(BinaryInstallPath())
}

func UninstallFromCLI() error {
	if err := disableService(); err != nil {
		return err
	}
	_ = os.Remove(BinaryInstallPath())
	_ = os.Remove(SystemAuthPath())
	_ = os.Remove(SystemTokenPath())
	_ = os.Remove(SocketPath())
	return nil
}
