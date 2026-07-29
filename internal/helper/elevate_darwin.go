//go:build darwin

package helper

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func ElevateInstall(ctx context.Context, source, token string, uid int, homeDir string) error {
	args := installArgs(source, token, uid, homeDir)
	quoted := make([]string, 0, len(args)+1)
	quoted = append(quoted, shellQuote(source))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	script := "do shell script " + strconv.Quote(strings.Join(quoted, " ")) +
		" with administrator privileges"
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install helper: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func ElevateUninstall(ctx context.Context, source string) error {
	script := "do shell script " + strconv.Quote(shellQuote(source)+" uninstall") +
		" with administrator privileges"
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("uninstall helper: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
