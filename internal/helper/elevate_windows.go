//go:build windows

package helper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const elevatedResultPollInterval = 100 * time.Millisecond

type elevatedRequest struct {
	ExpectedSHA256 string `json:"expectedSha256,omitempty"`
	Token          string `json:"token,omitempty"`
	UID            int    `json:"uid,omitempty"`
	Version        string `json:"version,omitempty"`
	HomeDir        string `json:"homeDir,omitempty"`
	OwnerSID       string `json:"ownerSid,omitempty"`
}

type elevatedResult struct {
	Error string `json:"error,omitempty"`
}

func ElevateInstall(
	ctx context.Context,
	source, expectedSHA256, token string,
	uid int,
	homeDir string,
) error {
	ownerSID, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("find current Windows user SID: %w", err)
	}
	return runElevatedHelper(ctx, source, "install", elevatedRequest{
		ExpectedSHA256: expectedSHA256,
		Token:          token,
		UID:            uid,
		Version:        Version,
		HomeDir:        homeDir,
		OwnerSID:       ownerSID,
	})
}

func ElevateUninstall(ctx context.Context, source string) error {
	expectedSHA256, err := bundledHelperSHA256(source)
	if err != nil {
		return err
	}
	return runElevatedHelper(ctx, source, "uninstall", elevatedRequest{
		ExpectedSHA256: expectedSHA256,
	})
}

func runElevatedHelper(
	ctx context.Context,
	source, operation string,
	request elevatedRequest,
) error {
	lockedSource, err := lockAndVerifyElevatedSource(source, request.ExpectedSHA256)
	if err != nil {
		return err
	}
	defer lockedSource.Close()
	ownerSID, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("find current Windows user SID: %w", err)
	}
	requestPath, err := createElevatedExchangeFile("kubeloop-elevated-request-*.json", ownerSID)
	if err != nil {
		return fmt.Errorf("create elevated request: %w", err)
	}
	defer os.Remove(requestPath)
	resultPath, err := createElevatedExchangeFile("kubeloop-elevated-result-*.json", ownerSID)
	if err != nil {
		return fmt.Errorf("create elevated result: %w", err)
	}
	defer os.Remove(resultPath)

	raw, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode elevated request: %w", err)
	}
	if err := os.WriteFile(requestPath, raw, 0o600); err != nil {
		return fmt.Errorf("write elevated request: %w", err)
	}
	if err := os.WriteFile(resultPath, nil, 0o600); err != nil {
		return fmt.Errorf("clear elevated result: %w", err)
	}

	args := strings.Join([]string{
		syscall.EscapeArg("elevated"),
		syscall.EscapeArg("--operation"),
		syscall.EscapeArg(operation),
		syscall.EscapeArg("--request"),
		syscall.EscapeArg(requestPath),
		syscall.EscapeArg("--result"),
		syscall.EscapeArg(resultPath),
	}, " ")
	verbPtr, _ := windows.UTF16PtrFromString("runas")
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return fmt.Errorf("encode elevated helper path: %w", err)
	}
	argsPtr, err := windows.UTF16PtrFromString(args)
	if err != nil {
		return fmt.Errorf("encode elevated helper arguments: %w", err)
	}
	cwdPtr, err := windows.UTF16PtrFromString(filepath.Dir(source))
	if err != nil {
		return fmt.Errorf("encode elevated helper directory: %w", err)
	}
	if err := windows.ShellExecute(
		0, verbPtr, sourcePtr, argsPtr, cwdPtr, windows.SW_HIDE,
	); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return fmt.Errorf("Windows elevation was cancelled")
		}
		return fmt.Errorf("launch elevated helper: %w", err)
	}
	return waitElevatedResult(ctx, resultPath)
}

func lockAndVerifyElevatedSource(source, expectedSHA256 string) (*os.File, error) {
	if expectedSHA256 == "" {
		return nil, fmt.Errorf("expected helper SHA-256 is required")
	}
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return nil, fmt.Errorf("encode elevated helper path: %w", err)
	}
	handle, err := windows.CreateFile(
		sourcePtr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("lock elevated helper source: %w", err)
	}
	file := os.NewFile(uintptr(handle), source)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("open locked elevated helper source")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("hash locked elevated helper source: %w", err)
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedSHA256) {
		_ = file.Close()
		return nil, fmt.Errorf("bundled helper checksum mismatch")
	}
	return file, nil
}

func createElevatedExchangeFile(pattern, ownerSID string) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := configureElevatedExchangeAccess(path, ownerSID); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func waitElevatedResult(ctx context.Context, path string) error {
	ticker := time.NewTicker(elevatedResultPollInterval)
	defer ticker.Stop()
	for {
		raw, err := os.ReadFile(path)
		if err == nil && len(raw) != 0 {
			var result elevatedResult
			if decodeErr := json.Unmarshal(raw, &result); decodeErr != nil {
				return fmt.Errorf("decode elevated helper result: %w", decodeErr)
			}
			if result.Error != "" {
				return fmt.Errorf("elevated helper command: %s", result.Error)
			}
			return nil
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read elevated helper result: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
