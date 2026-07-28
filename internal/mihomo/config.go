package mihomo

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"

	"go.yaml.in/yaml/v3"

	"github.com/kube-clash/kube-clash/internal/cluster"
)

const KubernetesProxy = "KUBERNETES"

type Options struct {
	BridgeHost       string
	BridgePort       int
	ControllerHost   string
	ControllerPort   int
	ControllerSecret string
	IPv6             bool
}

type config struct {
	Mode               string    `yaml:"mode"`
	LogLevel           string    `yaml:"log-level"`
	IPv6               bool      `yaml:"ipv6"`
	ExternalController string    `yaml:"external-controller"`
	Secret             string    `yaml:"secret"`
	TUN                tunConfig `yaml:"tun"`
	DNS                dnsConfig `yaml:"dns"`
	Proxies            []proxy   `yaml:"proxies"`
	Rules              []string  `yaml:"rules"`
}

type tunConfig struct {
	Enable              bool     `yaml:"enable"`
	Stack               string   `yaml:"stack"`
	Device              string   `yaml:"device"`
	AutoRoute           bool     `yaml:"auto-route"`
	AutoDetectInterface bool     `yaml:"auto-detect-interface"`
	StrictRoute         bool     `yaml:"strict-route"`
	DNSHijack           []string `yaml:"dns-hijack"`
	RouteAddress        []string `yaml:"route-address"`
}

type dnsConfig struct {
	Enable           bool              `yaml:"enable"`
	IPv6             bool              `yaml:"ipv6"`
	EnhancedMode     string            `yaml:"enhanced-mode"`
	UseSystemHosts   bool              `yaml:"use-system-hosts"`
	Nameserver       []string          `yaml:"nameserver"`
	NameserverPolicy map[string]string `yaml:"nameserver-policy,omitempty"`
}

type proxy struct {
	Name      string `yaml:"name"`
	Type      string `yaml:"type"`
	Server    string `yaml:"server"`
	Port      int    `yaml:"port"`
	UDP       bool   `yaml:"udp"`
	IPVersion string `yaml:"ip-version"`
}

func Generate(discovery cluster.Discovery, options Options) ([]byte, error) {
	if options.BridgeHost == "" {
		options.BridgeHost = "127.0.0.1"
	}
	if options.ControllerHost == "" {
		options.ControllerHost = "127.0.0.1"
	}
	if err := validatePort(options.BridgePort, "bridge"); err != nil {
		return nil, err
	}
	if err := validatePort(options.ControllerPort, "controller"); err != nil {
		return nil, err
	}
	if options.ControllerSecret == "" {
		return nil, errors.New("controller secret is required")
	}

	routes, rules, err := networkRules(discovery)
	if err != nil {
		return nil, err
	}
	policy := map[string]string{}
	if discovery.DNSServer != "" {
		dnsIP, parseErr := netip.ParseAddr(discovery.DNSServer)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid cluster DNS address %q: %w", discovery.DNSServer, parseErr)
		}
		policy["+.cluster.local"] = "udp://" + dnsIP.String() + "#" + KubernetesProxy
	}

	value := config{
		Mode:               "rule",
		LogLevel:           "info",
		IPv6:               options.IPv6,
		ExternalController: net.JoinHostPort(options.ControllerHost, strconv.Itoa(options.ControllerPort)),
		Secret:             options.ControllerSecret,
		TUN: tunConfig{
			Enable:              true,
			Stack:               "mixed",
			Device:              "KubeClash",
			AutoRoute:           true,
			AutoDetectInterface: true,
			StrictRoute:         true,
			DNSHijack:           []string{"any:53", "tcp://any:53"},
			RouteAddress:        routes,
		},
		DNS: dnsConfig{
			Enable:           true,
			IPv6:             options.IPv6,
			EnhancedMode:     "redir-host",
			UseSystemHosts:   true,
			Nameserver:       []string{"system://"},
			NameserverPolicy: policy,
		},
		Proxies: []proxy{{
			Name: KubernetesProxy, Type: "socks5",
			Server: options.BridgeHost, Port: options.BridgePort,
			UDP: true, IPVersion: ipVersion(options.IPv6),
		}},
		Rules: append(rules, "MATCH,DIRECT"),
	}
	return yaml.Marshal(value)
}

func networkRules(discovery cluster.Discovery) ([]string, []string, error) {
	routeSet := make(map[string]struct{})
	for _, raw := range discovery.PodCIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid pod CIDR %q: %w", raw, err)
		}
		routeSet[prefix.Masked().String()] = struct{}{}
	}
	for _, raw := range discovery.ServiceIPs {
		ip, err := netip.ParseAddr(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid service IP %q: %w", raw, err)
		}
		routeSet[netip.PrefixFrom(ip, ip.BitLen()).String()] = struct{}{}
	}
	if len(routeSet) == 0 {
		return nil, nil, errors.New("cluster discovery returned no routable addresses")
	}

	routes := make([]string, 0, len(routeSet))
	for route := range routeSet {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	rules := []string{"DOMAIN-SUFFIX,cluster.local," + KubernetesProxy}
	for _, route := range routes {
		rules = append(rules, "IP-CIDR,"+route+","+KubernetesProxy+",no-resolve")
	}
	return routes, rules, nil
}

func validatePort(port int, label string) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s port must be between 1 and 65535", label)
	}
	return nil
}

func ipVersion(ipv6 bool) string {
	if ipv6 {
		return "dual"
	}
	return "ipv4"
}
