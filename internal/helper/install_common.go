package helper

import (
	"bytes"
	"crypto/sha256"
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
	copiedHash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, copiedHash), in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	staged, err := os.Open(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	stagedHash := sha256.New()
	_, hashErr := io.Copy(stagedHash, staged)
	closeErr := staged.Close()
	if hashErr != nil {
		_ = os.Remove(tmp)
		return hashErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if !bytes.Equal(copiedHash.Sum(nil), stagedHash.Sum(nil)) {
		_ = os.Remove(tmp)
		return fmt.Errorf("staged helper hash does not match source")
	}
	if err := replaceFile(tmp, dst); err != nil {
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
	if err := prepareBinaryInstall(); err != nil {
		return fmt.Errorf("prepare helper binary install: %w", err)
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
