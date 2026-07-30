//go:build windows

package helper

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

func ElevateInstall(ctx context.Context, source, expectedSHA256, token string, uid int, homeDir string) error {
	installScript := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$programFiles = [Environment]::GetFolderPath([Environment+SpecialFolder]::ProgramFiles)
$stagingRoot = Join-Path $programFiles 'KubeLoop'
New-Item -ItemType Directory -Path $stagingRoot -Force | Out-Null
$workdir = Join-Path $stagingRoot ('.install-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $workdir | Out-Null
try {
    $acl = [System.Security.AccessControl.DirectorySecurity]::new()
    $acl.SetAccessRuleProtection($true, $false)
    $inherit = [System.Security.AccessControl.InheritanceFlags]'ContainerInherit, ObjectInherit'
    $propagate = [System.Security.AccessControl.PropagationFlags]::None
    $allow = [System.Security.AccessControl.AccessControlType]::Allow
    foreach ($sidValue in @('S-1-5-32-544', 'S-1-5-18')) {
        $sid = [System.Security.Principal.SecurityIdentifier]::new($sidValue)
        $rule = [System.Security.AccessControl.FileSystemAccessRule]::new(
            $sid, 'FullControl', $inherit, $propagate, $allow)
        $acl.AddAccessRule($rule)
    }
    Set-Acl -LiteralPath $workdir -AclObject $acl
    $staged = Join-Path $workdir 'kubeloop-helper.exe'
    Copy-Item -LiteralPath %s -Destination $staged
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $staged).Hash
    if ($actual -ine %s) { throw 'bundled helper checksum mismatch' }
    & $staged install --source $staged --token %s --uid %d --version %s --home %s
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Remove-Item -LiteralPath $workdir -Recurse -Force -ErrorAction SilentlyContinue
}
`, powershellQuote(source), powershellQuote(expectedSHA256), powershellQuote(token),
		uid, powershellQuote(Version), powershellQuote(homeDir))
	return runElevatedPowerShell(ctx, installScript)
}

func ElevateUninstall(ctx context.Context, source string) error {
	script := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
& %s uninstall
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Remove-Item -LiteralPath %s -Force -ErrorAction SilentlyContinue
`, powershellQuote(source), powershellQuote(source))
	return runElevatedPowerShell(ctx, script)
}

func runElevatedPowerShell(ctx context.Context, script string) error {
	outputFile, err := os.CreateTemp("", "kubeloop-elevated-*.log")
	if err != nil {
		return fmt.Errorf("create elevated helper log: %w", err)
	}
	outputPath := outputFile.Name()
	if err := outputFile.Close(); err != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("close elevated helper log: %w", err)
	}
	defer os.Remove(outputPath)

	payload := elevatedPowerShellPayload(script, outputPath)
	command := elevatedPowerShellLauncher(encodePowerShellCommand(payload))
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	launcherOutput, err := cmd.CombinedOutput()
	if err != nil {
		elevatedOutput, _ := os.ReadFile(outputPath)
		detail := strings.TrimSpace(strings.Join([]string{
			string(launcherOutput), string(elevatedOutput),
		}, "\n"))
		return fmt.Errorf("elevated helper command: %w: %s", err, detail)
	}
	return nil
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func encodePowerShellCommand(command string) string {
	codeUnits := utf16.Encode([]rune(command))
	bytes := make([]byte, len(codeUnits)*2)
	for i, unit := range codeUnits {
		bytes[i*2] = byte(unit)
		bytes[i*2+1] = byte(unit >> 8)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}
