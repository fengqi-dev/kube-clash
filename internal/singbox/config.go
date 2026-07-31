package singbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

const (
	KubernetesOutbound = "kubernetes"
	LocalOutbound      = "local"
	DirectOutbound     = "direct"
	DefaultDNSListen   = "127.0.0.1"
	DefaultDNSPort     = 1053

	PortForwardInbound   = "portfwd-in"
	ExchangeInbound      = "exchange-in"
	PreviewInbound       = "preview-in"
	MirrorPrimaryInbound = "mirror-primary-in"
	MirrorShadowInbound  = "mirror-shadow-in"
)

// TrafficInboundPorts contains the fixed loopback SOCKS inbounds used by
// KubeLoop's feature adapters. Targets remain dynamic and are carried in the
// SOCKS request, so feature sessions do not require a sing-box reload.
type TrafficInboundPorts struct {
	PortForward   int `json:"portForward"`
	Exchange      int `json:"exchange"`
	Preview       int `json:"preview"`
	MirrorPrimary int `json:"mirrorPrimary"`
	MirrorShadow  int `json:"mirrorShadow"`
}

func (p TrafficInboundPorts) items() []struct {
	tag  string
	port int
} {
	return []struct {
		tag  string
		port int
	}{
		{PortForwardInbound, p.PortForward},
		{ExchangeInbound, p.Exchange},
		{PreviewInbound, p.Preview},
		{MirrorPrimaryInbound, p.MirrorPrimary},
		{MirrorShadowInbound, p.MirrorShadow},
	}
}

// HostAlias maps a DNS name to an IPv4 address for the local dns-in resolver.
type HostAlias struct {
	Domain string `json:"domain"`
	IP     string `json:"ip"`
}

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
	ClusterDomains   []string
	Hosts            []HostAlias
	TrafficPorts     TrafficInboundPorts
	TrafficUsername  string
	TrafficPassword  string
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

	hosts, err := NormalizeHostAliases(options.Hosts)
	if err != nil {
		return nil, err
	}

	clusterDomains, err := cluster.NormalizeClusterDomains(options.ClusterDomains)
	if err != nil {
		return nil, err
	}
	if len(discovery.ClusterDomains) > 0 {
		merged, mergeErr := cluster.NormalizeClusterDomains(append(append([]string{}, clusterDomains...), discovery.ClusterDomains...))
		if mergeErr != nil {
			return nil, mergeErr
		}
		clusterDomains = merged
	}
	reverseZones := ReverseZones(discovery.PodCIDRs, discovery.ServiceCIDRs, discovery.ServiceIPs)

	dnsServers := make([]map[string]any, 0, 3)
	dnsRules := make([]map[string]any, 0, 4)
	if len(hosts) > 0 {
		predefined := make(map[string]any, len(hosts))
		domains := make([]string, 0, len(hosts))
		for _, item := range hosts {
			predefined[item.Domain] = item.IP
			domains = append(domains, item.Domain)
		}
		dnsServers = append(dnsServers, map[string]any{
			"type":       "hosts",
			"tag":        "hosts",
			"predefined": predefined,
		})
		dnsRules = append(dnsRules, map[string]any{
			"domain": domains,
			"server": "hosts",
		})
	}
	if discovery.DNSServer != "" {
		dnsIP, parseErr := netip.ParseAddr(discovery.DNSServer)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid cluster DNS address %q: %w", discovery.DNSServer, parseErr)
		}
		dnsServers = append(dnsServers, map[string]any{
			"type":   "udp",
			"tag":    "cluster",
			"server": dnsIP.String(),
			"detour": KubernetesOutbound,
		})
		dnsSuffixes := append([]string{}, clusterDomains...)
		dnsSuffixes = append(dnsSuffixes, reverseZones...)
		dnsRules = append(dnsRules, map[string]any{
			"domain_suffix": dnsSuffixes,
			"server":        "cluster",
		})
	}
	dnsServers = append(dnsServers, map[string]any{"type": "local", "tag": "local"})

	localInbounds := []string{ExchangeInbound, PreviewInbound, MirrorShadowInbound}
	routeRules := []map[string]any{
		{"inbound": []string{"dns-in"}, "action": "hijack-dns"},
		{"inbound": []string{PortForwardInbound}, "outbound": KubernetesOutbound},
		{"inbound": []string{MirrorPrimaryInbound}, "outbound": KubernetesOutbound},
	}
	for _, route := range routes {
		routeRules = append(routeRules, map[string]any{
			"inbound": localInbounds,
			"ip_cidr": []string{route},
			"action":  "reject",
		})
	}
	routeRules = append(routeRules,
		map[string]any{
			"inbound":  localInbounds,
			"ip_cidr":  []string{"127.0.0.0/8", "::1/128"},
			"outbound": LocalOutbound,
		},
		map[string]any{
			"inbound":  localInbounds,
			"domain":   []string{"localhost"},
			"outbound": LocalOutbound,
		},
		map[string]any{
			"inbound":       localInbounds,
			"ip_is_private": true,
			"outbound":      LocalOutbound,
		},
		map[string]any{"inbound": localInbounds, "action": "reject"},
		map[string]any{"action": "sniff"},
		map[string]any{"protocol": "dns", "action": "hijack-dns"},
		map[string]any{
			"domain_suffix": clusterDomains, "outbound": KubernetesOutbound,
		},
	)
	for _, route := range routes {
		routeRules = append(routeRules, map[string]any{
			"ip_cidr":  []string{route},
			"outbound": KubernetesOutbound,
		})
	}

	inbounds := []map[string]any{
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
	}
	if options.TrafficPorts != (TrafficInboundPorts{}) {
		if options.TrafficUsername == "" || options.TrafficPassword == "" {
			return nil, errors.New("traffic inbound credentials are required")
		}
		for _, item := range options.TrafficPorts.items() {
			if err := validatePort(item.port, item.tag); err != nil {
				return nil, err
			}
			inbounds = append(inbounds, map[string]any{
				"type":        "socks",
				"tag":         item.tag,
				"listen":      "127.0.0.1",
				"listen_port": item.port,
				"users": []map[string]any{{
					"username": options.TrafficUsername,
					"password": options.TrafficPassword,
				}},
			})
		}
	}

	config := map[string]any{
		"log": map[string]any{"level": "info", "output": "sing-box.log"},
		"dns": map[string]any{
			"servers":  dnsServers,
			"rules":    dnsRules,
			"final":    "local",
			"strategy": "prefer_ipv4",
		},
		"inbounds": inbounds,
		"outbounds": []map[string]any{
			{
				"type":        "socks",
				"tag":         KubernetesOutbound,
				"server":      options.BridgeHost,
				"server_port": options.BridgePort,
				"version":     "5",
			},
			{"type": "direct", "tag": LocalOutbound},
			{"type": "direct", "tag": DirectOutbound},
		},
		"route": map[string]any{
			"rules":                   routeRules,
			"final":                   DirectOutbound,
			"auto_detect_interface":   true,
			"find_process":            true,
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
// "svc" is included so macOS /etc/resolver/svc catches short names like
// static-web.default.svc (search domains alone query the primary resolver).
func ResolverDomains(namespace string, clusterDomains []string, hosts []HostAlias, extra ...string) []string {
	domains, err := cluster.NormalizeClusterDomains(clusterDomains)
	if err != nil || len(domains) == 0 {
		domains = []string{cluster.DefaultClusterDomain}
	}
	out := make([]string, 0, len(domains)*3+len(hosts)+len(extra)+1)
	seen := make(map[string]struct{}, len(domains)*3+len(hosts)+len(extra)+1)
	add := func(domain string) {
		domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
		if domain == "" {
			return
		}
		if _, exists := seen[domain]; exists {
			return
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
	}
	for _, domain := range domains {
		add(domain)
		add("svc." + domain)
		if namespace != "" {
			add(namespace + ".svc." + domain)
		}
	}
	add("svc")
	for _, item := range hosts {
		add(item.Domain)
	}
	for _, domain := range extra {
		add(domain)
	}
	return out
}

// NormalizeHostAliases validates and canonicalizes host aliases.
// An empty input returns nil (clears config).
func NormalizeHostAliases(items []HostAlias) ([]HostAlias, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]HostAlias, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(item.Domain)), ".")
		if domain == "" {
			return nil, errors.New("host alias domain is required")
		}
		if strings.ContainsAny(domain, " \t/") {
			return nil, fmt.Errorf("invalid host alias domain %q", item.Domain)
		}
		if !safeDNSName(domain) {
			return nil, fmt.Errorf("invalid host alias domain %q", item.Domain)
		}
		ip, err := netip.ParseAddr(strings.TrimSpace(item.IP))
		if err != nil || !ip.Is4() {
			return nil, fmt.Errorf("invalid host alias IPv4 %q", item.IP)
		}
		if _, exists := seen[domain]; exists {
			return nil, fmt.Errorf("duplicate host alias domain %q", domain)
		}
		seen[domain] = struct{}{}
		out = append(out, HostAlias{Domain: domain, IP: ip.String()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

func safeDNSName(value string) bool {
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

// SearchDomains returns Kubernetes-style DNS search suffixes for short names
// such as mysql, mysql.default, and mysql.default.svc.
func SearchDomains(namespace string, clusterDomains ...string) []string {
	if namespace == "" {
		namespace = "default"
	}
	domains, err := cluster.NormalizeClusterDomains(clusterDomains)
	if err != nil || len(domains) == 0 {
		domains = []string{cluster.DefaultClusterDomain}
	}
	out := make([]string, 0, len(domains)*3)
	seen := make(map[string]struct{}, len(domains)*3)
	add := func(domain string) {
		if _, ok := seen[domain]; ok {
			return
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
	}
	for _, domain := range domains {
		add(namespace + ".svc." + domain)
		add("svc." + domain)
		add(domain)
	}
	return out
}
