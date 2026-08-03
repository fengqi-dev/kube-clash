//go:build windows

package helper

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func installInspectorRootCertificate(certificate inspectorRootCertificate) error {
	return withInspectorCertificateFile(certificate.pem, func(path string) error {
		return runTrustCommand("certutil.exe", "-addstore", "-f", "Root", path)
	})
}

func removeInspectorRootCertificate(certificate inspectorRootCertificate) error {
	trusted, err := inspectorRootCertificateTrusted(certificate)
	if err != nil || !trusted {
		return err
	}
	return runTrustCommand("certutil.exe", "-delstore", "Root", certificate.sha1)
}

func inspectorRootCertificateTrusted(certificate inspectorRootCertificate) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(
		ctx, "certutil.exe", "-store", "Root", certificate.sha1,
	).CombinedOutput()
	if err != nil {
		lower := bytes.ToLower(output)
		if bytes.Contains(lower, []byte("cannot find")) ||
			bytes.Contains(lower, []byte("not found")) {
			return false, nil
		}
		return false, commandError("inspect Windows certificate trust", err, output)
	}
	return true, nil
}

func withInspectorCertificateFile(value []byte, action func(string) error) error {
	file, err := os.CreateTemp("", "kubeloop-inspector-root-*.crt")
	if err != nil {
		return fmt.Errorf("create temporary certificate: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
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
