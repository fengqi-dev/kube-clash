package singbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

const (
	KubernetesOutbound = "kubernetes"
	DirectOutbound     = "direct"
	DefaultDNSListen   = "127.0.0.1"
	DefaultDNSPort     = 1053
)

type Options struct {
	BridgeHost       string
	BridgePort       int
	ControllerHost   string
	ControllerPort   int
	ControllerSecret string
	DNSHost          string
	DNSPort          int
	TUNAddress       string
	Namespace        string
}

func Generate(discovery cluster.Discovery, options Options) ([]byte, error) {
	if options.BridgeHost == "" {
		options.BridgeHost = "127.0.0.1"
	}
	if options.ControllerHost == "" {
		options.ControllerHost = "127.0.0.1"
	}
	if options.DNSHost == "" {
		options.DNSHost = DefaultDNSListen
	}
	if options.DNSPort == 0 {
		options.DNSPort = DefaultDNSPort
	}
	if err := validatePort(options.BridgePort, "bridge"); err != nil {
		return nil, err
	}
	if err := validatePort(options.ControllerPort, "controller"); err != nil {
		return nil, err
	}
	if err := validatePort(options.DNSPort, "dns"); err != nil {
		return nil, err
	}
	if options.ControllerSecret == "" {
		return nil, errors.New("controller secret is required")
	}
	if options.TUNAddress == "" {
		options.TUNAddress = defaultTUNAddress
	}
	if _, err := netip.ParsePrefix(options.TUNAddress); err != nil {
		return nil, fmt.Errorf("invalid TUN address %q: %w", options.TUNAddress, err)
	}

	routes, err := clusterRoutes(discovery)
	if err != nil {
		return nil, err
	}

	dnsServers := []map[string]any{
		{"type": "local", "tag": "local"},
	}
	dnsRules := []map[string]any{}
	if discovery.DNSServer != "" {
		dnsIP, parseErr := netip.ParseAddr(discovery.DNSServer)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid cluster DNS address %q: %w", discovery.DNSServer, parseErr)
		}
		dnsServers = append([]map[string]any{{
			"type":   "udp",
			"tag":    "cluster",
			"server": dnsIP.String(),
			"detour": KubernetesOutbound,
		}}, dnsServers...)
		dnsRules = append(dnsRules, map[string]any{
			"domain_suffix": []string{"cluster.local"},
			"server":        "cluster",
		})
	}

	routeRules := []map[string]any{
		{"inbound": []string{"dns-in"}, "action": "hijack-dns"},
		{"action": "sniff"},
		{"protocol": "dns", "action": "hijack-dns"},
		{"domain_suffix": []string{"cluster.local"}, "outbound": KubernetesOutbound},
	}
	for _, route := range routes {
		routeRules = append(routeRules, map[string]any{
			"ip_cidr":  []string{route},
			"outbound": KubernetesOutbound,
		})
	}

	config := map[string]any{
		"log": map[string]any{"level": "info", "output": "sing-box.log"},
		"dns": map[string]any{
			"servers":  dnsServers,
			"rules":    dnsRules,
			"final":    "local",
			"strategy": "ipv4_only",
		},
		"inbounds": []map[string]any{
			{
				// dns_mode is sing-box 1.14+; we pin 1.13 and use /etc/resolver
				// (or platform split DNS) + dns-in instead of TUN DNS hijack.
				"type":          "tun",
				"tag":           "tun-in",
				"address":       []string{options.TUNAddress},
				"mtu":           9000,
				"auto_route":    true,
				"strict_route":  true,
				"stack":         "mixed",
				"route_address": routes,
			},
			{
				"type":        "direct",
				"tag":         "dns-in",
				"listen":      options.DNSHost,
				"listen_port": options.DNSPort,
			},
		},
		"outbounds": []map[string]any{
			{
				"type":        "socks",
				"tag":         KubernetesOutbound,
				"server":      options.BridgeHost,
				"server_port": options.BridgePort,
				"version":     "5",
			},
			{"type": "direct", "tag": DirectOutbound},
		},
		"route": map[string]any{
			"rules":                   routeRules,
			"final":                   DirectOutbound,
			"auto_detect_interface":   true,
			"default_domain_resolver": "local",
		},
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": net.JoinHostPort(
					options.ControllerHost, strconv.Itoa(options.ControllerPort),
				),
				"secret": options.ControllerSecret,
			},
		},
	}

	return json.MarshalIndent(config, "", "  ")
}

func clusterRoutes(discovery cluster.Discovery) ([]string, error) {
	routeSet := make(map[string]struct{})
	for _, raw := range discovery.PodCIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid pod CIDR %q: %w", raw, err)
		}
		routeSet[prefix.Masked().String()] = struct{}{}
	}
	for _, raw := range discovery.ServiceCIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid service CIDR %q: %w", raw, err)
		}
		routeSet[prefix.Masked().String()] = struct{}{}
	}
	// Fall back to per-Service /32s when the cluster Service CIDR is unknown.
	if len(discovery.ServiceCIDRs) == 0 {
		for _, raw := range discovery.ServiceIPs {
			ip, err := netip.ParseAddr(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid service IP %q: %w", raw, err)
			}
			routeSet[netip.PrefixFrom(ip, ip.BitLen()).String()] = struct{}{}
		}
	}
	if len(routeSet) == 0 {
		return nil, errors.New("cluster discovery returned no routable addresses")
	}
	routes := make([]string, 0, len(routeSet))
	for route := range routeSet {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	return routes, nil
}

func validatePort(port int, label string) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s port must be between 1 and 65535", errLabel(label))
	}
	return nil
}

func errLabel(label string) string { return label }

// ResolverDomains returns split-DNS match domains routed to the local dns-in.
func ResolverDomains(namespace string) []string {
	domains := []string{"cluster.local", "svc.cluster.local"}
	if namespace != "" {
		domains = append(domains, namespace+".svc.cluster.local")
	}
	return domains
}

// SearchDomains returns Kubernetes-style DNS search suffixes for short names
// such as mysql, mysql.default, and mysql.default.svc.
func SearchDomains(namespace string) []string {
	if namespace == "" {
		namespace = "default"
	}
	return []string{
		namespace + ".svc.cluster.local",
		"svc.cluster.local",
		"cluster.local",
	}
}
