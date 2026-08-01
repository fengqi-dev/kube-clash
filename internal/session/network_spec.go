package session

import (
	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

func networkSpec(discovery cluster.Discovery) singbox.NetworkSpec {
	return singbox.NetworkSpec{
		PodCIDRs:       append([]string(nil), discovery.PodCIDRs...),
		ServiceCIDRs:   append([]string(nil), discovery.ServiceCIDRs...),
		ServiceIPs:     append([]string(nil), discovery.ServiceIPs...),
		DNSServer:      discovery.DNSServer,
		ClusterDomains: append([]string(nil), discovery.ClusterDomains...),
	}
}
