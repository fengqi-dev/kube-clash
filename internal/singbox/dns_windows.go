//go:build windows

package singbox

import (
	"fmt"
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
	return builder.String()
}

func splitDNSRestoreCommands(string) string {
	return fmt.Sprintf(
		`Get-DnsClientNrptRule -ErrorAction SilentlyContinue | `+
			`Where-Object { $_.Comment -eq %s } | `+
			`ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force -ErrorAction SilentlyContinue }; `,
		powershellQuote(nrptComment),
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
