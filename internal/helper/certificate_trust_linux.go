//go:build linux

package helper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const inspectorRootCertificatePath = "/usr/local/share/ca-certificates/kubeloop-inspector-root.crt"

func installInspectorRootCertificate(certificate inspectorRootCertificate) error {
	if err := os.WriteFile(inspectorRootCertificatePath, certificate.pem, 0o644); err != nil {
		return fmt.Errorf("write Inspector Root CA: %w", err)
	}
	if err := runUpdateCACertificates(); err != nil {
		_ = os.Remove(inspectorRootCertificatePath)
		return err
	}
	return nil
}

func removeInspectorRootCertificate(_ inspectorRootCertificate) error {
	if err := os.Remove(inspectorRootCertificatePath); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Inspector Root CA: %w", err)
	}
	return runUpdateCACertificates()
}

func inspectorRootCertificateTrusted(certificate inspectorRootCertificate) (bool, error) {
	value, err := os.ReadFile(inspectorRootCertificatePath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read Inspector Root CA: %w", err)
	}
	parsed, err := validateInspectorRootCertificate(value)
	if err != nil {
		return false, nil
	}
	return bytes.Equal(parsed.raw, certificate.raw), nil
}

func runUpdateCACertificates() error {
	path, err := exec.LookPath("update-ca-certificates")
	if err != nil {
		return errors.New("update-ca-certificates is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("update CA certificates: %w: %s", err, bytes.TrimSpace(output))
	}
	return nil
}
