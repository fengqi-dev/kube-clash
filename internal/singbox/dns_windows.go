//go:build windows

package singbox

import (
	"fmt"
	"path/filepath"
	"strings"
)

const nrptComment = "KubeLoop"

func splitDNSSetupCommands(workDir string) string {
	meta, err := loadDNSMeta(workDir)
	if err != nil {
		return ""
	}
	var builder strings.Builder
	for _, domain := range meta.Domains {
		namespace := dnsNamespace(domain)
		fmt.Fprintf(
			&builder,
			`try { Add-DnsClientNrptRule -Namespace %s -NameServers %s -Comment %s -ErrorAction Stop } catch { }; `,
			powershellQuote(namespace),
			powershellQuote(meta.Listen),
			powershellQuote(nrptComment),
		)
	}
	builder.WriteString(windowsSearchDomainSetupCommands(workDir, meta.Search))
	return builder.String()
}

func splitDNSRestoreCommands(workDir string) string {
	return fmt.Sprintf(
		`Get-DnsClientNrptRule -ErrorAction SilentlyContinue | `+
			`Where-Object { $_.Comment -eq %s } | `+
			`ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force -ErrorAction SilentlyContinue }; %s`,
		powershellQuote(nrptComment),
		windowsSearchDomainRestoreCommands(workDir),
	)
}

func windowsSearchDomainSetupCommands(workDir string, search []string) string {
	if len(search) == 0 {
		return ""
	}
	backup := powershellQuote(filepath.Join(workDir, "search-domains.bak.json"))
	items := make([]string, 0, len(search))
	for _, domain := range search {
		items = append(items, powershellQuote(domain))
	}
	return fmt.Sprintf(
		`$backup = %s; `+
			`$search = @(%s); `+
			`try { $old = @((Get-DnsClientGlobalSetting).SuffixSearchList); } catch { $old = @() }; `+
			`if ($null -eq $old) { $old = @() }; `+
			`$old | ConvertTo-Json -Compress | Set-Content -LiteralPath $backup -Encoding utf8; `+
			`$merged = @($search + ($old | Where-Object { $_ -and ($search -notcontains $_) })); `+
			`try { Set-DnsClientGlobalSetting -SuffixSearchList $merged -ErrorAction Stop } catch { }; `,
		backup,
		strings.Join(items, ","),
	)
}

func windowsSearchDomainRestoreCommands(workDir string) string {
	backup := powershellQuote(filepath.Join(workDir, "search-domains.bak.json"))
	return fmt.Sprintf(
		`$backup = %s; `+
			`if (Test-Path -LiteralPath $backup) { `+
			`$raw = Get-Content -LiteralPath $backup -Raw -ErrorAction SilentlyContinue; `+
			`if ([string]::IsNullOrWhiteSpace($raw) -or $raw -eq 'null') { `+
			`try { Set-DnsClientGlobalSetting -SuffixSearchList @() -ErrorAction Stop } catch { } `+
			`} else { `+
			`try { $old = $raw | ConvertFrom-Json; if ($null -eq $old) { $old = @() }; `+
			`if ($old -isnot [array]) { $old = @($old) }; `+
			`Set-DnsClientGlobalSetting -SuffixSearchList $old -ErrorAction Stop } catch { } `+
			`}; Remove-Item -LiteralPath $backup -Force -ErrorAction SilentlyContinue }; `,
		backup,
	)
}

func dnsNamespace(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "."
	}
	if strings.HasPrefix(domain, ".") {
		return domain
	}
	return "." + domain
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
