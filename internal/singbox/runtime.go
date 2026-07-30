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
)

// PrivilegedStartFunc starts sing-box via an external privileged helper.
// It returns a stop function used during Close.
type PrivilegedStartFunc func(
	ctx context.Context, spec SessionSpec,
) (stop func(context.Context) error, err error)

type RunningCore interface {
	io.Closer
	Done() <-chan struct{}
	Err() error
	Snapshot(ctx context.Context) (Metrics, error)
}

const DefaultMetricsInterval = time.Second

type Runtime struct {
	HTTPClient      *http.Client
	PrivilegedStart PrivilegedStartFunc
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
	tunAddress, err := selectTUNAddress()
	if err != nil {
		return nil, err
	}
	normalizedHosts, err := NormalizeHostAliases(hosts)
	if err != nil {
		return nil, err
	}
	spec := SessionSpec{
		ID:               "session-" + secret[:16],
		PodCIDRs:         discovery.PodCIDRs,
		ServiceCIDRs:     discovery.ServiceCIDRs,
		ServiceIPs:       discovery.ServiceIPs,
		ClusterDNSServer: discovery.DNSServer,
		BridgeHost:       host,
		BridgePort:       bridgePort,
		ControllerPort:   controllerPort,
		ControllerSecret: secret,
		DNSHost:          DefaultDNSListen,
		DNSPort:          internalDNSPort,
		PublicDNSPort:    publicDNSPort,
		TUNAddress:       tunAddress,
		Namespace:        namespace,
		Hosts:            normalizedHosts,
	}
	if r.PrivilegedStart == nil {
		return nil, errors.New("privileged helper is required")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	meta, err := spec.DNS()
	if err != nil {
		return nil, err
	}
	searchDomains := meta.Search
	resolverDomains := meta.Domains
	dnsProxy, err := startDNSSearchProxy(
		DefaultDNSListen, publicDNSPort, DefaultDNSListen, internalDNSPort, searchDomains,
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
}

func (p *Process) Done() <-chan struct{} { return p.done }

func (p *Process) Err() error {
	p.errMu.RLock()
	defer p.errMu.RUnlock()
	return p.waitErr
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
			process = filepath.Base(item.Metadata.ProcessPath)
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

func availablePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func randomSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
