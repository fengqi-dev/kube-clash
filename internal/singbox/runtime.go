package singbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/miekg/dns"
)

// PrivilegedStartFunc starts sing-box via an external privileged helper.
// It returns a stop function used during Close.
type PrivilegedStartFunc func(
	ctx context.Context, spec SessionSpec,
) (stop func(context.Context) error, err error)

// PrivilegedUpdateDNSFunc re-applies split DNS without restarting sing-box.
type PrivilegedUpdateDNSFunc func(
	ctx context.Context, sessionID string, dns DNSMeta,
) error

type RunningCore interface {
	io.Closer
	Done() <-chan struct{}
	Err() error
	Snapshot(ctx context.Context) (Metrics, error)
	TrafficEndpoints() TrafficEndpoints
	// Config returns the generated sing-box config JSON for the active core.
	Config() []byte
	// UpdateDNSNamespace refreshes search domains / split-DNS matcher files.
	UpdateDNSNamespace(ctx context.Context, namespace string) error
	// ProbeClusterDNS resolves kubernetes.default.svc.<domain> via the local split DNS.
	ProbeClusterDNS(ctx context.Context) error
	DNSPort() int
}

type TrafficEndpoint struct {
	Address  string
	Username string
	Password string
}

type TrafficEndpoints struct {
	PortForward   TrafficEndpoint
	Exchange      TrafficEndpoint
	Preview       TrafficEndpoint
	MirrorPrimary TrafficEndpoint
	MirrorShadow  TrafficEndpoint
}

func (e TrafficEndpoints) Validate() error {
	items := []struct {
		name     string
		endpoint TrafficEndpoint
	}{
		{PortForwardInbound, e.PortForward},
		{ExchangeInbound, e.Exchange},
		{PreviewInbound, e.Preview},
		{MirrorPrimaryInbound, e.MirrorPrimary},
		{MirrorShadowInbound, e.MirrorShadow},
	}
	for _, item := range items {
		host, rawPort, err := net.SplitHostPort(item.endpoint.Address)
		if err != nil {
			return fmt.Errorf("%s address: %w", item.name, err)
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("%s must listen on loopback", item.name)
		}
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("%s has invalid port", item.name)
		}
		if item.endpoint.Username == "" || item.endpoint.Password == "" {
			return fmt.Errorf("%s credentials are required", item.name)
		}
	}
	return nil
}

const DefaultMetricsInterval = time.Second

type Runtime struct {
	HTTPClient          *http.Client
	PrivilegedStart     PrivilegedStartFunc
	PrivilegedUpdateDNS PrivilegedUpdateDNSFunc
}

func (r *Runtime) Start(
	ctx context.Context,
	discovery cluster.Discovery,
	bridgeAddress string,
	namespace string,
	hosts []HostAlias,
) (RunningCore, error) {
	host, rawPort, err := net.SplitHostPort(bridgeAddress)
	if err != nil {
		return nil, fmt.Errorf("parse SOCKS bridge address: %w", err)
	}
	bridgePort, err := strconv.Atoi(rawPort)
	if err != nil {
		return nil, fmt.Errorf("parse SOCKS bridge port: %w", err)
	}
	controllerPort, err := availablePort()
	if err != nil {
		return nil, err
	}
	// Public port is advertised to the OS (/etc/resolver). sing-box dns-in uses
	// an internal port; dnsSearchProxy expands short names then forwards.
	publicDNSPort, err := selectDNSPort()
	if err != nil {
		return nil, err
	}
	internalDNSPort, err := availablePort()
	if err != nil {
		return nil, err
	}
	secret, err := randomSecret()
	if err != nil {
		return nil, err
	}
	trafficPorts, err := availableTrafficPorts(
		bridgePort, controllerPort, publicDNSPort, internalDNSPort,
	)
	if err != nil {
		return nil, err
	}
	trafficUsername, err := randomSecret()
	if err != nil {
		return nil, err
	}
	trafficPassword, err := randomSecret()
	if err != nil {
		return nil, err
	}
	tunAddress, err := selectTUNAddress()
	if err != nil {
		return nil, err
	}
	normalizedHosts, err := NormalizeHostAliases(hosts)
	if err != nil {
		return nil, err
	}
	clusterDomains, _ := cluster.NormalizeClusterDomains(discovery.ClusterDomains)
	dnsNamespace := namespace
	if dnsNamespace == "" {
		dnsNamespace = "default"
	}
	spec := SessionSpec{
		ID:               "session-" + secret[:16],
		PodCIDRs:         discovery.PodCIDRs,
		ServiceCIDRs:     discovery.ServiceCIDRs,
		ServiceIPs:       discovery.ServiceIPs,
		ClusterDNSServer: discovery.DNSServer,
		ClusterDomains:   clusterDomains,
		BridgeHost:       host,
		BridgePort:       bridgePort,
		ControllerPort:   controllerPort,
		ControllerSecret: secret,
		DNSHost:          DefaultDNSListen,
		DNSPort:          internalDNSPort,
		PublicDNSPort:    publicDNSPort,
		TUNAddress:       tunAddress,
		Namespace:        namespace,
		DNSNamespace:     dnsNamespace,
		Hosts:            normalizedHosts,
		TrafficPorts:     trafficPorts,
		TrafficUsername:  trafficUsername,
		TrafficPassword:  trafficPassword,
	}
	if r.PrivilegedStart == nil {
		return nil, errors.New("privileged helper is required")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	config, err := spec.GenerateConfig()
	if err != nil {
		return nil, fmt.Errorf("generate sing-box config: %w", err)
	}
	meta, err := spec.DNS()
	if err != nil {
		return nil, err
	}
	searchDomains := meta.Search
	resolverDomains := meta.Domains
	dnsProxy, err := startDNSSearchProxy(
		DefaultDNSListen, publicDNSPort, DefaultDNSListen, internalDNSPort,
		searchDomains, clusterDomains...,
	)
	if err != nil {
		return nil, err
	}
	process := &Process{
		done: make(chan struct{}), stopCh: make(chan struct{}),
		controllerAddress: net.JoinHostPort("127.0.0.1", strconv.Itoa(controllerPort)),
		controllerSecret:  secret,
		dnsPort:           publicDNSPort,
		resolverDomains:   resolverDomains,
		dnsProxy:          dnsProxy,
		httpClient:        r.HTTPClient,
		trafficEndpoints:  trafficEndpoints(trafficPorts, trafficUsername, trafficPassword),
		config:            config,
		spec:              spec,
		updateDNS:         r.PrivilegedUpdateDNS,
	}
	stop, startErr := r.PrivilegedStart(ctx, spec)
	if startErr != nil {
		_ = dnsProxy.Close()
		return nil, startErr
	}
	process.helperStop = stop
	go process.wait()
	go func() {
		select {
		case <-ctx.Done():
			_ = process.Close()
		case <-process.Done():
		}
	}()
	if err := r.waitReady(ctx, process); err != nil {
		_ = process.Close()
		return nil, err
	}
	return process, nil
}

func (r *Runtime) waitReady(ctx context.Context, process *Process) error {
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		response, requestErr := process.request(ctx, "/")
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusNotFound {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-process.Done():
			if err := process.Err(); err != nil {
				return err
			}
			return errors.New("sing-box exited before becoming ready")
		case <-deadline.C:
			return errors.New("timed out waiting for sing-box controller")
		case <-ticker.C:
		}
	}
}

type Process struct {
	done              chan struct{}
	stopCh            chan struct{}
	helperStop        func(context.Context) error
	controllerAddress string
	controllerSecret  string
	dnsPort           int
	resolverDomains   []string
	dnsProxy          *dnsSearchProxy
	httpClient        *http.Client
	closeOnce         sync.Once
	errMu             sync.RWMutex
	waitErr           error
	trafficEndpoints  TrafficEndpoints
	config            []byte
	spec              SessionSpec
	updateDNS         PrivilegedUpdateDNSFunc
	specMu            sync.Mutex
}

func (p *Process) Done() <-chan struct{} { return p.done }

func (p *Process) Err() error {
	p.errMu.RLock()
	defer p.errMu.RUnlock()
	return p.waitErr
}

func (p *Process) TrafficEndpoints() TrafficEndpoints { return p.trafficEndpoints }

func (p *Process) DNSPort() int { return p.dnsPort }

func (p *Process) Config() []byte {
	if len(p.config) == 0 {
		return nil
	}
	out := make([]byte, len(p.config))
	copy(out, p.config)
	return out
}

func (p *Process) UpdateDNSNamespace(ctx context.Context, namespace string) error {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "default"
	}
	if !safeDNSName(strings.ToLower(namespace)) {
		return errors.New("invalid DNS namespace")
	}
	p.specMu.Lock()
	p.spec.DNSNamespace = namespace
	p.spec.Namespace = namespace
	dns, err := p.spec.DNS()
	domains := append([]string{}, p.spec.ClusterDomains...)
	sessionID := p.spec.ID
	p.specMu.Unlock()
	if err != nil {
		return err
	}
	if p.dnsProxy != nil {
		p.dnsProxy.SetSearch(dns.Search)
		p.dnsProxy.SetClusterDomains(domains)
	}
	p.resolverDomains = dns.Domains
	if p.updateDNS == nil {
		return errors.New("privileged DNS update is unavailable; reconnect to apply")
	}
	return p.updateDNS(ctx, sessionID, dns)
}

func (p *Process) ProbeClusterDNS(ctx context.Context) error {
	p.specMu.Lock()
	domains := append([]string{}, p.spec.ClusterDomains...)
	port := p.dnsPort
	p.specMu.Unlock()
	if len(domains) == 0 {
		domains = []string{cluster.DefaultClusterDomain}
	}
	name := "kubernetes.default.svc." + domains[0] + "."
	return probeLocalDNS(ctx, DefaultDNSListen, port, name)
}

func (p *Process) Snapshot(ctx context.Context) (Metrics, error) {
	response, err := p.request(ctx, "/connections")
	if err != nil {
		return Metrics{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Metrics{}, fmt.Errorf("sing-box connections API returned %s", response.Status)
	}
	var raw clashConnections
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(&raw); err != nil {
		return Metrics{}, fmt.Errorf("decode sing-box connections: %w", err)
	}
	return mapClashMetrics(raw), nil
}

type clashConnections struct {
	DownloadTotal int64             `json:"downloadTotal"`
	UploadTotal   int64             `json:"uploadTotal"`
	Memory        uint64            `json:"memory"`
	Connections   []clashConnection `json:"connections"`
}

type clashConnection struct {
	ID       string `json:"id"`
	Metadata struct {
		Network         string `json:"network"`
		SourceIP        string `json:"sourceIP"`
		SourcePort      string `json:"sourcePort"`
		DestinationIP   string `json:"destinationIP"`
		DestinationPort string `json:"destinationPort"`
		Host            string `json:"host"`
		Process         string `json:"process"`
		ProcessPath     string `json:"processPath"`
		Type            string `json:"type"`
	} `json:"metadata"`
	Upload   int64    `json:"upload"`
	Download int64    `json:"download"`
	Start    string   `json:"start"`
	Chains   []string `json:"chains"`
	Rule     string   `json:"rule"`
}

func mapClashMetrics(raw clashConnections) Metrics {
	connections := make([]Connection, 0, len(raw.Connections))
	for _, item := range raw.Connections {
		destination := item.Metadata.Host
		if destination == "" {
			destination = joinHostPort(item.Metadata.DestinationIP, item.Metadata.DestinationPort)
		} else if item.Metadata.DestinationPort != "" {
			destination = net.JoinHostPort(destination, item.Metadata.DestinationPort)
		}
		process := item.Metadata.Process
		if process == "" {
			process = processName(item.Metadata.ProcessPath)
		}
		outbound := DirectOutbound
		if len(item.Chains) > 0 {
			outbound = item.Chains[0]
		}
		connections = append(connections, Connection{
			ID:          item.ID,
			Network:     item.Metadata.Network,
			Source:      joinHostPort(item.Metadata.SourceIP, item.Metadata.SourcePort),
			Destination: destination,
			Process:     process,
			Upload:      item.Upload,
			Download:    item.Download,
			StartedAt:   item.Start,
			Inbound:     inboundTag(item.Metadata.Type),
			Outbound:    outbound,
			Rule:        item.Rule,
		})
	}
	if connections == nil {
		connections = []Connection{}
	}
	return Metrics{
		DownloadTotal:     raw.DownloadTotal,
		UploadTotal:       raw.UploadTotal,
		Memory:            raw.Memory,
		ActiveConnections: len(connections),
		Connections:       connections,
	}
}

func inboundTag(value string) string {
	if _, tag, found := strings.Cut(value, "/"); found {
		return tag
	}
	return value
}

// processName turns Clash API processPath into a short executable label.
// Paths may look like "/usr/bin/curl (alice)".
func processName(processPath string) string {
	processPath = strings.TrimSpace(processPath)
	if processPath == "" {
		return ""
	}
	if before, _, ok := strings.Cut(processPath, " ("); ok {
		processPath = before
	}
	return filepath.Base(processPath)
}

func joinHostPort(host, port string) string {
	if host == "" && port == "" {
		return ""
	}
	if port == "" {
		return host
	}
	return net.JoinHostPort(host, port)
}

func (p *Process) request(ctx context.Context, path string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "http://"+p.controllerAddress+path, nil,
	)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+p.controllerSecret)
	client := p.httpClient
	if client == nil {
		client = &http.Client{Timeout: time.Second}
	}
	return client.Do(request)
}

func (p *Process) wait() {
	<-p.stopCh
	p.errMu.Lock()
	p.waitErr = nil
	p.errMu.Unlock()
	close(p.done)
}

func (p *Process) Close() error {
	p.closeOnce.Do(func() {
		select {
		case <-p.done:
		default:
			// helperStop blocks until the helper has restored DNS and routes.
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			if p.helperStop != nil {
				_ = p.helperStop(ctx)
			}
			cancel()
			close(p.stopCh)
			select {
			case <-p.done:
			case <-time.After(20 * time.Second):
				select {
				case <-p.done:
				case <-time.After(2 * time.Second):
				}
			}
		}
		if p.dnsProxy != nil {
			_ = p.dnsProxy.Close()
			p.dnsProxy = nil
		}
	})
	err := p.Err()
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "signal") {
		return err
	}
	return nil
}

func probeLocalDNS(ctx context.Context, host string, port int, qname string) error {
	if host == "" {
		host = DefaultDNSListen
	}
	if port < 1 {
		return errors.New("DNS port is unavailable")
	}
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(qname), dns.TypeA)
	client := &dns.Client{Net: "udp", Timeout: 3 * time.Second}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	type result struct {
		resp *dns.Msg
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, _, err := client.Exchange(msg, addr)
		ch <- result{resp: resp, err: err}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out := <-ch:
		if out.err != nil {
			return out.err
		}
		if out.resp == nil {
			return errors.New("empty DNS response")
		}
		if out.resp.Rcode != dns.RcodeSuccess || len(out.resp.Answer) == 0 {
			return fmt.Errorf("DNS lookup %s failed (rcode=%d)", qname, out.resp.Rcode)
		}
		return nil
	}
}

func availablePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func availableTrafficPorts(excluded ...int) (TrafficInboundPorts, error) {
	ports := TrafficInboundPorts{}
	targets := []*int{
		&ports.PortForward,
		&ports.Exchange,
		&ports.Preview,
		&ports.MirrorPrimary,
		&ports.MirrorShadow,
	}
	seen := make(map[int]struct{}, len(targets)+len(excluded))
	for _, port := range excluded {
		seen[port] = struct{}{}
	}
	for _, target := range targets {
		for {
			port, err := availablePort()
			if err != nil {
				return TrafficInboundPorts{}, err
			}
			if _, exists := seen[port]; exists {
				continue
			}
			seen[port] = struct{}{}
			*target = port
			break
		}
	}
	return ports, nil
}

func trafficEndpoints(ports TrafficInboundPorts, username, password string) TrafficEndpoints {
	endpoint := func(port int) TrafficEndpoint {
		return TrafficEndpoint{
			Address:  net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
			Username: username,
			Password: password,
		}
	}
	return TrafficEndpoints{
		PortForward:   endpoint(ports.PortForward),
		Exchange:      endpoint(ports.Exchange),
		Preview:       endpoint(ports.Preview),
		MirrorPrimary: endpoint(ports.MirrorPrimary),
		MirrorShadow:  endpoint(ports.MirrorShadow),
	}
}

func randomSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
