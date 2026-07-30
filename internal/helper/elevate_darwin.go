//go:build darwin

package helper

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func ElevateInstall(ctx context.Context, source, expectedSHA256, token string, uid int, homeDir string) error {
	command := `set -eu
workdir="$(mktemp -d "${TMPDIR:-/private/tmp}/kubeloop-helper.XXXXXX")"
trap 'rm -rf "$workdir"' EXIT HUP INT TERM
staged="$workdir/kubeloop-helper"
/bin/cp ` + shellQuote(source) + ` "$staged"
actual="$(/usr/bin/shasum -a 256 "$staged")"
actual="${actual%% *}"
if [ "$actual" != ` + shellQuote(expectedSHA256) + ` ]; then
	echo "bundled helper checksum mismatch" >&2
	exit 1
fi
/bin/chmod 700 "$staged"
"$staged" install --source "$staged" --token ` + shellQuote(token) +
		` --uid ` + shellQuote(strconv.Itoa(uid)) +
		` --version ` + shellQuote(Version) +
		` --home ` + shellQuote(homeDir)
	script := "do shell script " + strconv.Quote(command) +
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
