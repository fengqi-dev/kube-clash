//go:build linux

package helper

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func ElevateInstall(ctx context.Context, source, token string, uid int, homeDir string) error {
	args := installArgs(source, token, uid, homeDir)
	elevate := "pkexec"
	if _, err := exec.LookPath("pkexec"); err != nil {
		elevate = "sudo"
	}
	cmdArgs := append([]string{source}, args...)
	cmd := exec.CommandContext(ctx, elevate, cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install helper (%s): %w: %s", elevate, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func ElevateUninstall(ctx context.Context, source string) error {
	elevate := "pkexec"
	if _, err := exec.LookPath("pkexec"); err != nil {
		elevate = "sudo"
	}
	cmd := exec.CommandContext(ctx, elevate, source, "uninstall")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("uninstall helper (%s): %w: %s", elevate, err, strings.TrimSpace(string(output)))
	}
	return nil
}
