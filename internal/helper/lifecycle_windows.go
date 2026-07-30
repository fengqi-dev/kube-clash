//go:build windows

package helper

import (
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

const windowsSearchBackup = "search-domains.bak.json"

func applyPlatformDNS(workDir string, dns singbox.DNSMeta) error {
	backup, err := runPowerShell(
		"@((Get-DnsClientGlobalSetting).SuffixSearchList) | ConvertTo-Json -Compress",
	)
	if err != nil {
		return fmt.Errorf("read DNS search domains: %w", err)
	}
	if err := os.WriteFile(
		filepath.Join(workDir, windowsSearchBackup), backup, 0o600,
	); err != nil {
		return err
	}
	if _, err := runPowerShell(
		`Get-DnsClientNrptRule -ErrorAction SilentlyContinue | ` +
			`Where-Object { $_.Comment -eq 'KubeLoop' } | ` +
			`ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force -ErrorAction SilentlyContinue }`,
	); err != nil {
		return err
	}
	for _, domain := range dns.Domains {
		command := fmt.Sprintf(
			"Add-DnsClientNrptRule -Namespace %s -NameServers %s -Comment 'KubeLoop' -ErrorAction Stop",
			powershellLiteral("."+strings.TrimPrefix(domain, ".")),
			powershellLiteral(dns.Listen),
		)
		if _, err := runPowerShell(command); err != nil {
			return fmt.Errorf("add NRPT rule for %s: %w", domain, err)
		}
	}
	if len(dns.Search) > 0 {
		items := make([]string, 0, len(dns.Search))
		for _, domain := range dns.Search {
			items = append(items, powershellLiteral(domain))
		}
		command := fmt.Sprintf(
			"$want=@(%s); $old=@((Get-DnsClientGlobalSetting).SuffixSearchList); "+
				"$merged=@($want+($old|Where-Object { $_ -and ($want -notcontains $_) })); "+
				"Set-DnsClientGlobalSetting -SuffixSearchList $merged -ErrorAction Stop",
			strings.Join(items, ","),
		)
		if _, err := runPowerShell(command); err != nil {
			return fmt.Errorf("set DNS search domains: %w", err)
		}
	}
	_, _ = runPowerShell("Clear-DnsClientCache")
	return nil
}

func restorePlatformDNS(workDir string, _ singbox.DNSMeta) error {
	_, removeErr := runPowerShell(
		`Get-DnsClientNrptRule -ErrorAction SilentlyContinue | ` +
			`Where-Object { $_.Comment -eq 'KubeLoop' } | ` +
			`ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force -ErrorAction SilentlyContinue }`,
	)
	backupPath := filepath.Join(workDir, windowsSearchBackup)
	backup, readErr := os.ReadFile(backupPath)
	if readErr == nil {
		raw := strings.TrimSpace(string(backup))
		command := "Set-DnsClientGlobalSetting -SuffixSearchList @() -ErrorAction Stop"
		if raw != "" && raw != "null" && raw != "[]" {
			command = fmt.Sprintf(
				"$old=%s | ConvertFrom-Json; if ($old -isnot [array]) { $old=@($old) }; "+
					"Set-DnsClientGlobalSetting -SuffixSearchList $old -ErrorAction Stop",
				powershellLiteral(raw),
			)
		}
		if _, err := runPowerShell(command); err != nil && removeErr == nil {
			removeErr = err
		}
		_ = os.Remove(backupPath)
	}
	_, _ = runPowerShell("Clear-DnsClientCache")
	return removeErr
}

func runPowerShell(command string) ([]byte, error) {
	return exec.Command(
		"powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Restricted", "-Command", command,
	).CombinedOutput()
}

func powershellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func cleanupPlatformRoutes(routes []string) {
	for _, raw := range routes {
		prefix, err := netip.ParsePrefix(raw)
		if err == nil {
			_ = exec.Command("route.exe", "delete", prefix.Masked().String()).Run()
		}
	}
}
