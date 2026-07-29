//go:build darwin || linux

package singbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func writeUnixLifecycleScript(
	binaryPath, workDir, logPath string,
	routeCleanup string,
) (string, error) {
	pidPath := filepath.Join(workDir, privilegedPIDFilename)
	stopPath := filepath.Join(workDir, privilegedStopFilename)
	setupDNS := splitDNSSetupCommands(workDir)
	restoreDNS := splitDNSRestoreCommands(workDir)

	stalePIDs := staleManagedPIDs(binaryPath, workDir)
	var stopStale strings.Builder
	for _, pid := range stalePIDs {
		fmt.Fprintf(
			&stopStale,
			"kill -INT %s 2>/dev/null || true; "+
				"for i in $(seq 1 50); do kill -0 %s 2>/dev/null || break; sleep 0.1; done; "+
				"kill -TERM %s 2>/dev/null || true; ",
			pid, pid, pid,
		)
	}

	// Restore must not abort the script on a single failing rm/grep (set -e).
	restore := "set +e; " + restoreDNS + "set -e; "
	script := fmt.Sprintf(
		"#!/bin/sh\nset -e\n"+
			"%s%s%s"+
			"rm -f %s\n"+
			"%s run -c %s -D %s >> %s 2>&1 &\n"+
			"child=$!\n"+
			"echo \"$child\" > %s\n"+
			"while kill -0 \"$child\" 2>/dev/null; do\n"+
			"  if [ -f %s ]; then\n"+
			"    %s\n"+
			"    kill -INT \"$child\" 2>/dev/null || true\n"+
			"    break\n"+
			"  fi\n"+
			"  sleep 0.2\n"+
			"done\n"+
			"status=0\n"+
			"wait \"$child\" || status=$?\n"+
			"%s\n"+
			"exit \"$status\"\n",
		stopStale.String(),
		routeCleanup,
		setupDNS,
		shellQuote(stopPath),
		shellQuote(binaryPath),
		shellQuote(filepath.Join(workDir, "config.json")),
		shellQuote(workDir),
		shellQuote(logPath),
		shellQuote(pidPath),
		shellQuote(stopPath),
		restore,
		restore,
	)
	path := filepath.Join(workDir, lifecycleScriptName+".sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return "", fmt.Errorf("write sing-box lifecycle script: %w", err)
	}
	return path, nil
}

func staleManagedPIDs(binaryPath, workDir string) []string {
	return staleManagedPIDsWith(binaryPath, workDir, func(pid string) (string, error) {
		command, err := exec.Command("ps", "-p", pid, "-o", "command=").Output()
		return string(command), err
	})
}
