//go:build windows

package singbox

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func defaultStartCommand(binaryPath, workDir string, output io.Writer) (*exec.Cmd, error) {
	logFile, ok := output.(*os.File)
	if !ok {
		return nil, errors.New("start privileged sing-box: log output is not a file")
	}
	_ = logFile // sing-box writes sing-box.log under workDir via config output
	scriptPath, err := writeWindowsLifecycleScript(binaryPath, workDir)
	if err != nil {
		return nil, err
	}
	// Elevated PowerShell runs the lifecycle script and blocks until stop/exit.
	ps := fmt.Sprintf(
		"Start-Process -FilePath %s -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-File',%s) -Verb RunAs -Wait",
		powershellQuote("powershell.exe"),
		powershellQuote(scriptPath),
	)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps)
	cmd.Stdout = io.Discard
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("request administrator access for sing-box TUN: %w", err)
	}
	return cmd, nil
}

func writeWindowsLifecycleScript(binaryPath, workDir string) (string, error) {
	pidPath := filepath.Join(workDir, privilegedPIDFilename)
	stopPath := filepath.Join(workDir, privilegedStopFilename)
	routes, err := loadTUNRouteAddresses(workDir)
	if err != nil {
		return "", err
	}
	var routeCleanup strings.Builder
	for _, raw := range routes {
		prefix, parseErr := netip.ParsePrefix(raw)
		if parseErr != nil {
			return "", fmt.Errorf("parse managed route %q: %w", raw, parseErr)
		}
		prefix = prefix.Masked()
		fmt.Fprintf(
			&routeCleanup,
			"try { route delete %s >$null 2>&1 } catch { }\n",
			prefix.String(),
		)
	}

	var stopStale strings.Builder
	for _, pid := range staleManagedPIDs(binaryPath, workDir) {
		fmt.Fprintf(
			&stopStale,
			"try { Stop-Process -Id %s -Force -ErrorAction SilentlyContinue } catch { }\n",
			pid,
		)
	}

	script := fmt.Sprintf(
		"$ErrorActionPreference = 'Continue'\n"+
			"%s"+
			"%s"+
			"%s"+
			"if (Test-Path -LiteralPath %s) { Remove-Item -LiteralPath %s -Force }\n"+
			"$proc = Start-Process -FilePath %s -ArgumentList @('run','-c',%s,'-D',%s) "+
			"-PassThru -WindowStyle Hidden\n"+
			"Set-Content -LiteralPath %s -Value $proc.Id\n"+
			"while (-not $proc.HasExited) {\n"+
			"  if (Test-Path -LiteralPath %s) {\n"+
			"    %s\n"+
			"    try { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue } catch { }\n"+
			"    break\n"+
			"  }\n"+
			"  Start-Sleep -Milliseconds 200\n"+
			"  $proc.Refresh()\n"+
			"}\n"+
			"try { Wait-Process -Id $proc.Id -ErrorAction SilentlyContinue } catch { }\n"+
			"%s\n",
		stopStale.String(),
		routeCleanup.String(),
		splitDNSSetupCommands(workDir),
		powershellQuote(stopPath),
		powershellQuote(stopPath),
		powershellQuote(binaryPath),
		powershellQuote(filepath.Join(workDir, "config.json")),
		powershellQuote(workDir),
		powershellQuote(pidPath),
		powershellQuote(stopPath),
		splitDNSRestoreCommands(workDir),
		splitDNSRestoreCommands(workDir),
	)
	path := filepath.Join(workDir, lifecycleScriptName+".ps1")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return "", fmt.Errorf("write sing-box lifecycle script: %w", err)
	}
	return path, nil
}

func staleManagedPIDs(binaryPath, workDir string) []string {
	return staleManagedPIDsWith(binaryPath, workDir, func(pid string) (string, error) {
		out, err := exec.Command(
			"powershell.exe", "-NoProfile", "-Command",
			fmt.Sprintf(
				"(Get-CimInstance Win32_Process -Filter \"ProcessId = %s\").CommandLine",
				pid,
			),
		).Output()
		return string(out), err
	})
}
