package cluster

import (
	"fmt"
	"strings"
)

const DefaultClusterDomain = "cluster.local"

// NormalizeClusterDomains validates and canonicalizes cluster DNS domains.
// An empty input yields the Kubernetes default (cluster.local). The default
// domain is always retained alongside any custom domains.
func NormalizeClusterDomains(domains []string) ([]string, error) {
	seen := make(map[string]struct{}, len(domains)+1)
	out := make([]string, 0, len(domains)+1)
	add := func(raw string) error {
		domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
		if domain == "" {
			return nil
		}
		if !safeClusterDomain(domain) {
			return fmt.Errorf("invalid cluster domain %q", raw)
		}
		if _, exists := seen[domain]; exists {
			return nil
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
		return nil
	}
	for _, domain := range domains {
		if err := add(domain); err != nil {
			return nil, err
		}
	}
	if err := add(DefaultClusterDomain); err != nil {
		return nil, err
	}
	// Keep default first for stable UX, then the rest in input order.
	if len(out) > 1 && out[0] != DefaultClusterDomain {
		rest := make([]string, 0, len(out)-1)
		for _, domain := range out {
			if domain == DefaultClusterDomain {
				continue
			}
			rest = append(rest, domain)
		}
		out = append([]string{DefaultClusterDomain}, rest...)
	}
	return out, nil
}

func safeClusterDomain(value string) bool {
	if value == "" || len(value) > 253 || strings.HasPrefix(value, ".") ||
		strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}
