package cluster

import "github.com/fengqi-dev/kube-loop/internal/dnsname"

const DefaultClusterDomain = dnsname.DefaultClusterDomain

// NormalizeClusterDomains validates and canonicalizes cluster DNS domains.
// An empty input yields the Kubernetes default (cluster.local). The default
// domain is always retained alongside any custom domains.
func NormalizeClusterDomains(domains []string) ([]string, error) {
	return dnsname.NormalizeClusterDomains(domains)
}

func safeClusterDomain(value string) bool {
	return dnsname.ValidClusterDomain(value)
}
