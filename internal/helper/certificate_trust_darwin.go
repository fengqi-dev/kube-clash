//go:build darwin

package helper

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const systemKeychainPath = "/Library/Keychains/System.keychain"

func installInspectorRootCertificate(certificate inspectorRootCertificate) error {
	return withInspectorCertificateFile(certificate.pem, func(path string) error {
		return runTrustCommand(
			"security", "add-trusted-cert", "-d", "-r", "trustRoot",
			"-k", systemKeychainPath, path,
		)
	})
}

func removeInspectorRootCertificate(certificate inspectorRootCertificate) error {
	trusted, err := inspectorRootCertificateTrusted(certificate)
	if err != nil || !trusted {
		return err
	}
	return runTrustCommand(
		"security", "delete-certificate", "-Z", certificate.sha1, systemKeychainPath,
	)
}

func inspectorRootCertificateTrusted(certificate inspectorRootCertificate) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(
		ctx, "security", "find-certificate", "-a", "-p",
		"-c", inspectorRootCommonName, systemKeychainPath,
	).CombinedOutput()
	if err != nil {
		if bytes.Contains(output, []byte("could not be found")) {
			return false, nil
		}
		return false, commandError("inspect system certificate trust", err, output)
	}
	for len(output) > 0 {
		block, rest := pem.Decode(output)
		if block == nil {
			break
		}
		output = rest
		parsed, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr == nil && bytes.Equal(parsed.Raw, certificate.raw) {
			return true, nil
		}
	}
	return false, nil
}

func withInspectorCertificateFile(value []byte, action func(string) error) error {
	file, err := os.CreateTemp("", "kubeloop-inspector-root-*.crt")
	if err != nil {
		return fmt.Errorf("create temporary certificate: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(value); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return action(path)
}

func runTrustCommand(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return commandError(strings.Join(append([]string{name}, args...), " "), err, output)
	}
	return nil
}

func commandError(action string, err error, output []byte) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, detail)
}
