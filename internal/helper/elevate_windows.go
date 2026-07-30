//go:build windows

package helper

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

func ElevateInstall(ctx context.Context, source, expectedSHA256, token string, uid int, homeDir string) error {
	installScript := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$workdir = Join-Path ([IO.Path]::GetTempPath()) ('kubeloop-helper-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $workdir | Out-Null
try {
    $acl = New-Object System.Security.AccessControl.DirectorySecurity
    $acl.SetAccessRuleProtection($true, $false)
    $inherit = [System.Security.AccessControl.InheritanceFlags]'ContainerInherit, ObjectInherit'
    $propagate = [System.Security.AccessControl.PropagationFlags]::None
    $allow = [System.Security.AccessControl.AccessControlType]::Allow
    foreach ($sidValue in @('S-1-5-32-544', 'S-1-5-18')) {
        $sid = New-Object System.Security.Principal.SecurityIdentifier($sidValue)
        $rule = New-Object System.Security.AccessControl.FileSystemAccessRule(
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
	encoded := encodePowerShellCommand(script)
	command := fmt.Sprintf(
		"$p = Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-EncodedCommand','%s') -Verb RunAs -Wait -PassThru; exit $p.ExitCode",
		encoded,
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

func encodePowerShellCommand(command string) string {
	codeUnits := utf16.Encode([]rune(command))
	bytes := make([]byte, len(codeUnits)*2)
	for i, unit := range codeUnits {
		bytes[i*2] = byte(unit)
		bytes[i*2+1] = byte(unit >> 8)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}
