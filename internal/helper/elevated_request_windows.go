//go:build windows

package helper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func RunElevatedRequest(operation, requestPath, resultPath string) error {
	err := executeElevatedRequest(operation, requestPath)
	result := elevatedResult{}
	if err != nil {
		result.Error = err.Error()
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return marshalErr
	}
	if writeErr := os.WriteFile(resultPath, raw, 0o600); writeErr != nil {
		return fmt.Errorf("write elevated result: %w", writeErr)
	}
	return err
}

func executeElevatedRequest(operation, requestPath string) error {
	raw, err := os.ReadFile(requestPath)
	if err != nil {
		return fmt.Errorf("read elevated request: %w", err)
	}
	var request elevatedRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return fmt.Errorf("decode elevated request: %w", err)
	}
	if request.ExpectedSHA256 == "" {
		return fmt.Errorf("expected helper SHA-256 is required")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find elevated helper executable: %w", err)
	}
	actual, err := fileSHA256(executable)
	if err != nil {
		return fmt.Errorf("hash elevated helper executable: %w", err)
	}
	if !strings.EqualFold(actual, request.ExpectedSHA256) {
		return fmt.Errorf("elevated helper checksum mismatch")
	}
	switch operation {
	case "install":
		return elevatedInstall(request)
	case "uninstall":
		return UninstallFromCLI()
	default:
		return fmt.Errorf("unsupported elevated operation %q", operation)
	}
}

func elevatedInstall(request elevatedRequest) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find elevated helper executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve elevated helper executable: %w", err)
	}
	stagingRoot := filepath.Dir(BinaryInstallPath())
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return fmt.Errorf("create helper staging root: %w", err)
	}
	workDir, err := os.MkdirTemp(stagingRoot, ".install-")
	if err != nil {
		return fmt.Errorf("create helper staging directory: %w", err)
	}
	defer os.RemoveAll(workDir)
	if err := configureElevatedStagingAccess(workDir); err != nil {
		return err
	}
	staged := filepath.Join(workDir, "kubeloop-helper.exe")
	if err := copyFile(executable, staged, 0o700); err != nil {
		return fmt.Errorf("stage elevated helper: %w", err)
	}
	actual, err := fileSHA256(staged)
	if err != nil {
		return fmt.Errorf("hash staged helper: %w", err)
	}
	if !strings.EqualFold(actual, request.ExpectedSHA256) {
		return fmt.Errorf("bundled helper checksum mismatch")
	}
	return InstallFromCLI(
		staged,
		request.Token,
		request.UID,
		request.Version,
		request.HomeDir,
		request.OwnerSID,
	)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
