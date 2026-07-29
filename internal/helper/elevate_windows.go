//go:build windows

package helper

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func ElevateInstall(ctx context.Context, source, token string, uid int, homeDir string) error {
	args := installArgs(source, token, uid, homeDir)
	return runElevated(ctx, source, args)
}

func ElevateUninstall(ctx context.Context, source string) error {
	return runElevated(ctx, source, []string{"uninstall"})
}

func runElevated(ctx context.Context, source string, args []string) error {
	psArgs := make([]string, 0, len(args))
	for _, arg := range args {
		psArgs = append(psArgs, powershellQuote(arg))
	}
	command := fmt.Sprintf(
		"$p = Start-Process -FilePath %s -ArgumentList @(%s) -Verb RunAs -Wait -PassThru; exit $p.ExitCode",
		powershellQuote(source),
		strings.Join(psArgs, ","),
	)
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("elevated helper command: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
