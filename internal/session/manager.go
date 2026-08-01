package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
	"github.com/fengqi-dev/kube-loop/internal/socksbridge"
	"github.com/fengqi-dev/kube-loop/internal/store"
	"github.com/fengqi-dev/kube-loop/internal/traffic"
)

type Phase string

const (
	PhaseIdle        Phase = "idle"
	PhaseChecking    Phase = "checking"
	PhaseInstalling  Phase = "installing-gateway"
	PhaseDiscovering Phase = "discovering-network"
	PhaseStarting    Phase = "starting-tunnel"
	PhaseConnected   Phase = "connected"
	PhaseError       Phase = "error"
)

const DefaultGatewayImage = "ghcr.io/fengqi-dev/kube-loop/gateway:latest"

// ResolveGatewayImage picks the Gateway image for this desktop build.
// KUBELOOP_GATEWAY_IMAGE wins; release builds pin the matching image tag.
func ResolveGatewayImage(appVersion string) string {
	if image := strings.TrimSpace(os.Getenv("KUBELOOP_GATEWAY_IMAGE")); image != "" {
		return image
	}
	if appVersion != "" && appVersion != "dev" {
		return "ghcr.io/fengqi-dev/kube-loop/gateway:" + appVersion
	}
	return DefaultGatewayImage
}

type Request struct {
	Context   string
	Namespace string
}

type State struct {
	Phase           Phase                 `json:"phase"`
	Context         string                `json:"context"`
	Namespace       string                `json:"namespace"`
	DNSNamespace    string                `json:"dnsNamespace,omitempty"`
	Message         string                `json:"message"`
	Error           string                `json:"error,omitempty"`
	DNSWarning      string                `json:"dnsWarning,omitempty"`
	Network         *NetworkDiagnostics   `json:"network,omitempty"`
	Discovery       *cluster.Discovery    `json:"discovery,omitempty"`
	Capabilities    *cluster.Capabilities `json:"capabilities,omitempty"`
	ScopeNamespaces []string              `json:"scopeNamespaces,omitempty"`
	GatewayManifest string                `json:"gatewayManifest,omitempty"`
	Pods            []cluster.PodInfo     `json:"pods,omitempty"`
	Services        []cluster.ServiceInfo `json:"services,omitempty"`
	Events          []LogEvent            `json:"events,omitempty"`
	CoreVersion     string                `json:"coreVersion,omitempty"`
	ConnectedAt     *time.Time            `json:"connectedAt,omitempty"`
	Metrics         *singbox.Metrics      `json:"metrics,omitempty"`
	// InventoryRevision increments only on Informer-driven inventory snapshots
	// (pod/service/deployment add/update/delete). UI lists should key off this
	// instead of UpdatedAt, which also advances on the metrics ticker.
	InventoryRevision int64     `json:"inventoryRevision"`
	KubernetesVersion string    `json:"kubernetesVersion,omitempty"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// ClusterCatalog exposes read-only cluster inventory used by the desktop
// facade. Keeping it separate from connection lifecycle operations lets
// callers test and replace those concerns independently.
type ClusterCatalog interface {
	Contexts() ([]cluster.ContextInfo, error)
	Namespaces(context.Context, string) ([]string, error)
	ListServices(context.Context, string, string) ([]cluster.ServiceInfo, error)
	ListPods(context.Context, string, string) ([]cluster.PodInfo, error)
}

// ClusterConnection exposes the Kubernetes operations needed to establish and
// monitor a connected session.
type ClusterConnection interface {
	ServerVersion(context.Context, string) (string, error)
	Discover(context.Context, string, []string) (cluster.Discovery, error)
	WatchInventory(
		context.Context,
		string,
		[]string,
		func(cluster.InventorySnapshot),
	) (io.Closer, error)
	ProbeCapabilities(context.Context, string) (cluster.Capabilities, error)
}

// GatewayManager owns the in-cluster Gateway and its API-server channel.
type GatewayManager interface {
	GetGateway(context.Context, string) (cluster.GatewayInfo, error)
	EnsureGateway(context.Context, string, string) (cluster.GatewayInfo, error)
	StartPortForward(context.Context, string, string, uint16) (cluster.PortForward, error)
}

// ClusterProvider is the composition-root contract implemented by
// cluster.Provider. Manager stores each facet behind its narrow interface,
// while feature managers receive only their own consumer-defined contracts.
type ClusterProvider interface {
	ClusterCatalog
	ClusterConnection
	GatewayManager
	intercept.ClusterAPI
	portfwd.ClusterAPI
}

type Core interface {
	Start(
		context.Context,
		cluster.Discovery,
		string,
		string,
		[]singbox.HostAlias,
	) (singbox.RunningCore, error)
}

type BridgeFactory func(context.Context, string) (net.Listener, error)

type Option func(*Manager)

func WithCore(core Core) Option {
	return func(manager *Manager) { manager.core = core }
}

func WithBridgeFactory(factory BridgeFactory) Option {
	return func(manager *Manager) { manager.bridgeFactory = factory }
}

func WithGatewayImage(image string) Option {
	return func(manager *Manager) { manager.gatewayImage = image }
}

type recentConnection struct {
	connection singbox.Connection
	lastSeen   time.Time
}

type connectionTraffic struct {
	upload   int64
	download int64
	at       time.Time
}

type Manager struct {
	catalog       ClusterCatalog
	connection    ClusterConnection
	gateway       GatewayManager
	core          Core
	bridgeFactory BridgeFactory
	gatewayImage  string
	store         *store.Store

	mu               sync.RWMutex
	state            State
	cancel           context.CancelFunc
	done             chan struct{}
	listeners        []func(State)
	metricsListeners []func(*singbox.Metrics)
	intercept        *intercept.Manager
	portfwd          *portfwd.Manager

	recentConnections map[string]recentConnection
	lastTraffic       map[string]connectionTraffic
	restoring         bool
	runningCore       singbox.RunningCore
	trafficTracker    *traffic.Tracker
}

// Keep short-lived TUN connections visible between core snapshot polls.
const connectionRetainFor = 30 * time.Second

// Bound retained/published rows so Wails/React are not flooded by bursty TUN flows.
const (
	maxRetainedConnections  = 500
	maxPublishedConnections = 100
)

func NewManager(provider ClusterProvider, options ...Option) *Manager {
	manager := &Manager{
		catalog:    provider,
		connection: provider,
		gateway:    provider,
		core:       newSingboxRuntime(),
		bridgeFactory: func(ctx context.Context, gatewayAddress string) (net.Listener, error) {
			return socksbridge.Listen(ctx, gatewayAddress)
		},
		gatewayImage: ResolveGatewayImage(""),
		intercept:    intercept.NewManager(provider),
		portfwd:      portfwd.NewManager(provider),
		state: State{
			Phase: PhaseIdle, Message: "Disconnected", CoreVersion: singbox.Version, UpdatedAt: time.Now(),
		},
	}
	for _, option := range options {
		option(manager)
	}
	return manager
}

func (m *Manager) Contexts() ([]cluster.ContextInfo, error) {
	return m.catalog.Contexts()
}

func (m *Manager) Namespaces(ctx context.Context, contextName string) ([]string, error) {
	return m.catalog.Namespaces(ctx, contextName)
}

func (m *Manager) ListServices(
	ctx context.Context, contextName, namespace string,
) ([]cluster.ServiceInfo, error) {
	return m.catalog.ListServices(ctx, contextName, namespace)
}

func (m *Manager) ListPods(
	ctx context.Context, contextName, namespace string,
) ([]cluster.PodInfo, error) {
	return m.catalog.ListPods(ctx, contextName, namespace)
}

// SingBoxConfig returns the active session's generated sing-box config JSON.
func (m *Manager) SingBoxConfig() ([]byte, error) {
	m.mu.RLock()
	core := m.runningCore
	phase := m.state.Phase
	m.mu.RUnlock()
	if core == nil || phase != PhaseConnected {
		return nil, errors.New("not connected")
	}
	config := core.Config()
	if len(config) == 0 {
		return nil, errors.New("sing-box config unavailable")
	}
	return config, nil
}

// DNSPort returns the local split-DNS / search-proxy listen port for the active session.
func (m *Manager) DNSPort() (int, error) {
	m.mu.RLock()
	core := m.runningCore
	phase := m.state.Phase
	m.mu.RUnlock()
	if core == nil || phase != PhaseConnected {
		return 0, errors.New("not connected")
	}
	port := core.DNSPort()
	if port < 1 {
		return 0, errors.New("DNS port is unavailable")
	}
	return port, nil
}

// InternalDNSPort returns sing-box's loopback DNS listener. It bypasses the
// OS-facing split-DNS port, which may be redirected by another local TUN.
func (m *Manager) InternalDNSPort() (int, error) {
	m.mu.RLock()
	core := m.runningCore
	phase := m.state.Phase
	m.mu.RUnlock()
	if core == nil || phase != PhaseConnected {
		return 0, errors.New("not connected")
	}
	port := core.InternalDNSPort()
	if port < 1 {
		return 0, errors.New("internal DNS port is unavailable")
	}
	return port, nil
}

func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// SetKubernetesVersion records the API server version for the sidebar/overview.
func (m *Manager) SetKubernetesVersion(version string) {
	if version == "" {
		return
	}
	m.mu.Lock()
	if m.state.KubernetesVersion == version {
		m.mu.Unlock()
		return
	}
	next := m.state
	next.KubernetesVersion = version
	m.mu.Unlock()
	m.publish(next)
}

func (m *Manager) Subscribe(listener func(State)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, listener)
}

// SubscribeMetrics receives high-frequency connection/traffic snapshots without
// re-emitting the full session inventory on every poll.
func (m *Manager) SubscribeMetrics(listener func(*singbox.Metrics)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metricsListeners = append(m.metricsListeners, listener)
}

func (m *Manager) Connect(parent context.Context, request Request) error {
	if request.Context == "" {
		return errors.New("context is required")
	}
	if request.Namespace == "" {
		request.Namespace = "default"
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return errors.New("a connection is already active")
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	m.cancel = cancel
	m.done = done
	m.mu.Unlock()

	go m.run(ctx, request, done)
	return nil
}

func (m *Manager) run(ctx context.Context, request Request, done chan struct{}) {
	var resources []io.Closer
	defer func() {
		for index := len(resources) - 1; index >= 0; index-- {
			_ = resources[index].Close()
		}
		m.mu.Lock()
		if m.done == done {
			m.cancel = nil
			m.done = nil
			m.runningCore = nil
		}
		m.mu.Unlock()
		close(done)
	}()

	prev := m.State()
	state := State{
		Phase: PhaseChecking, Context: request.Context, Namespace: request.Namespace,
		Message: "Checking Kubernetes access", CoreVersion: singbox.Version,
		// Keep the last probed version so the Overview subtitle does not flash
		// back to the cluster name while ServerVersion is re-fetched.
		KubernetesVersion: prev.KubernetesVersion,
	}
	m.publish(state)
	m.AppendLog("INFO", fmt.Sprintf("connecting to context %s", request.Context))

	caps, err := m.connection.ProbeCapabilities(ctx, request.Context)
	if err != nil {
		m.fail(ctx, state, "Could not check cluster permissions", err)
		return
	}
	state.Capabilities = &caps
	state.ScopeNamespaces = append([]string{}, caps.ScopeNamespaces...)
	if version, versionErr := m.connection.ServerVersion(ctx, request.Context); versionErr == nil {
		state.KubernetesVersion = version
	}
	m.publish(state)
	if state.KubernetesVersion != "" {
		m.AppendLog("INFO", "kubernetes "+state.KubernetesVersion)
	} else {
		m.AppendLog("INFO", "kubernetes version unavailable")
	}
	for _, issue := range caps.Issues {
		m.AppendLog("INFO", issue)
	}
	if !caps.GatewayPortForward {
		state.GatewayManifest = cluster.GatewayInstallManifest(m.gatewayImage)
		m.fail(ctx, state, "Current account cannot port-forward to the Gateway", errors.New("missing pods/portforward in kubeloop-system"))
		return
	}

	state.Phase = PhaseInstalling
	state.Message = "Installing or checking the cluster Gateway"
	m.publish(state)
	var gateway cluster.GatewayInfo
	if caps.GatewayInstall {
		gateway, err = m.gateway.EnsureGateway(ctx, request.Context, m.gatewayImage)
		if err != nil {
			m.fail(ctx, state, "Could not install the cluster Gateway", err)
			return
		}
	} else {
		gateway, err = m.gateway.GetGateway(ctx, request.Context)
		if err != nil {
			state.GatewayManifest = cluster.GatewayInstallManifest(m.gatewayImage)
			m.fail(ctx, state, "No preinstalled Gateway found; ask an admin to install it or grant install permission", err)
			return
		}
	}

	state.Phase = PhaseDiscovering
	state.Message = "Discovering Pods, Services, and cluster DNS"
	m.publish(state)
	scopeNS := caps.ScopeNamespaces
	if caps.InventoryCluster {
		scopeNS = nil
	}
	discovery, err := m.connection.Discover(ctx, request.Context, scopeNS)
	if err != nil {
		m.fail(ctx, state, "Could not read cluster network information", err)
		return
	}
	dnsNamespace := request.Namespace
	if m.store != nil {
		manual := m.store.ManualNetwork(request.Context)
		discovery = cluster.MergeManualNetwork(discovery, cluster.ManualNetwork{
			PodCIDRs:       manual.PodCIDRs,
			ServiceCIDRs:   manual.ServiceCIDRs,
			DNSServer:      manual.DNSServer,
			ClusterDomains: manual.ClusterDomains,
		})
		if manual.DNSNamespace != "" {
			dnsNamespace = manual.DNSNamespace
		}
	}
	state.Discovery = &discovery
	state.DNSNamespace = dnsNamespace
	state.Network = inspectNetwork(discovery)
	m.publish(state)
	for _, issue := range state.Network.Issues {
		m.AppendLog("WARN", issue.Message)
	}

	forwarder, err := m.gateway.StartPortForward(
		ctx, request.Context, gateway.Name, cluster.GatewayPort,
	)
	if err != nil {
		m.fail(ctx, state, "Could not establish a secure Gateway channel", err)
		return
	}
	resources = append(resources, forwarder)

	if err := m.intercept.Start(ctx, request.Context, gateway.IP, forwarder.Address()); err != nil {
		m.fail(ctx, state, "Could not start the Service Intercept control channel", err)
		return
	}
	var interceptCloseOnce sync.Once
	closeIntercept := closerFunc(func() {
		interceptCloseOnce.Do(func() {
			_ = m.intercept.StopAll(context.Background())
		})
	})
	// Keep an early guard for failures before sing-box starts. The same
	// idempotent closer is appended again after the core so normal teardown
	// restores Kubernetes resources before closing the data plane.
	resources = append(resources, closeIntercept)

	bridgeContext, stopBridge := context.WithCancel(ctx)
	resources = append(resources, closerFunc(stopBridge))
	bridge, err := m.bridgeFactory(bridgeContext, forwarder.Address())
	if err != nil {
		m.fail(ctx, state, "Could not start the local SOCKS Bridge", err)
		return
	}
	resources = append(resources, bridge)
	if hostBridge, ok := bridge.(*socksbridge.Bridge); ok {
		hostBridge.SetHostTCPHandler(m.intercept.HostTCP)
		hostBridge.SetHostUDPHandler(m.intercept.HostUDP)
		resources = append(resources, closerFunc(func() {
			hostBridge.SetHostTCPHandler(nil)
			hostBridge.SetHostUDPHandler(nil)
		}))
	}

	state.Phase = PhaseStarting
	state.Message = "Installing and starting sing-box TUN"
	m.publish(state)
	hosts := m.hostAliasesFor(request.Context)
	core, err := m.core.Start(ctx, discovery, bridge.Addr().String(), dnsNamespace, hosts)
	if err != nil {
		m.fail(ctx, state, "Could not start sing-box TUN", err)
		return
	}
	resources = append(resources, core)
	m.mu.Lock()
	m.runningCore = core
	m.mu.Unlock()
	trafficEndpoints := core.TrafficEndpoints()
	if err := trafficEndpoints.Validate(); err != nil {
		m.fail(ctx, state, "sing-box feature inbounds are unavailable", err)
		return
	}
	tracker := traffic.NewTracker()
	m.mu.Lock()
	m.trafficTracker = tracker
	m.mu.Unlock()
	m.intercept.SetTrafficDialers(intercept.TrafficDialers{
		Exchange:     trackedTrafficDialer(trafficEndpoints.Exchange, singbox.TrafficUserExchange, tracker),
		Preview:      trackedTrafficDialer(trafficEndpoints.Preview, singbox.TrafficUserPreview, tracker),
		MirrorShadow: trackedTrafficDialer(trafficEndpoints.MirrorShadow, singbox.TrafficUserMirrorShadow, tracker),
	})
	m.portfwd.SetTrafficDialer(
		request.Context,
		trackedPortForwardDialer(trafficEndpoints.PortForward, tracker),
	)
	resources = append(resources, closerFunc(func() {
		m.intercept.SetTrafficDialers(intercept.TrafficDialers{})
		m.portfwd.StopRouted()
		m.portfwd.SetTrafficDialer("", nil)
		m.persistPortForwards()
		m.mu.Lock()
		m.trafficTracker = nil
		m.mu.Unlock()
	}))
	resources = append(resources, closeIntercept)

	connectedAt := time.Now()
	state.Phase = PhaseConnected
	state.Message = "Connected; Pods, Services, and cluster DNS are reachable"
	state.ConnectedAt = &connectedAt
	state.Metrics = &singbox.Metrics{}
	state.Capabilities = &caps
	state.ScopeNamespaces = append([]string{}, caps.ScopeNamespaces...)
	state.DNSNamespace = dnsNamespace
	state.DNSWarning = ""
	m.publish(state)
	m.AppendLog("INFO", fmt.Sprintf("connected to context %s", request.Context))
	if m.store != nil {
		if err := m.store.SetConnected(request.Context, request.Namespace, true); err != nil {
			log.Printf("persist connected state: %v", err)
		}
	}
	m.probeClusterDNS(ctx, state, core)
	m.restoreBindings(ctx, request.Context)

	inventory, err := m.connection.WatchInventory(ctx, request.Context, scopeNS, func(snap cluster.InventorySnapshot) {
		m.applyInventory(snap)
	})
	if err != nil {
		m.fail(ctx, state, "Could not watch cluster resource changes", err)
		return
	}
	resources = append(resources, inventory)

	ticker := time.NewTicker(singbox.DefaultMetricsInterval)
	defer ticker.Stop()
	controlLost := m.intercept.ControlLost()
	for {
		select {
		case <-ctx.Done():
			m.clearRecentConnections()
			m.publish(State{
				Phase:             PhaseIdle,
				Message:           "Disconnected",
				CoreVersion:       singbox.Version,
				KubernetesVersion: state.KubernetesVersion,
				Context:           state.Context,
				Namespace:         state.Namespace,
			})
			m.AppendLog("INFO", "disconnected")
			return
		case <-core.Done():
			m.clearRecentConnections()
			if ctx.Err() == nil {
				err := core.Err()
				if err == nil {
					err = errors.New("sing-box stopped unexpectedly")
				}
				m.fail(ctx, state, "sing-box TUN exited unexpectedly", err)
			}
			return
		case <-controlLost:
			if ctx.Err() != nil {
				m.clearRecentConnections()
				return
			}
			m.AppendLog("WARN", "gateway control channel closed; reconnecting")
			var lastErr error
			recovered := false
			for attempt := range 5 {
				if attempt > 0 {
					delay := 500 * time.Millisecond
					switch {
					case attempt >= 3:
						delay = 2 * time.Second
					case attempt >= 1:
						delay = time.Second
					}
					select {
					case <-ctx.Done():
						m.clearRecentConnections()
						return
					case <-time.After(delay):
					}
				}
				lastErr = m.intercept.RecoverControl(ctx)
				if lastErr == nil {
					recovered = true
					break
				}
				m.AppendLog("WARN", fmt.Sprintf(
					"gateway control reconnect attempt %d/5 failed: %v", attempt+1, lastErr,
				))
			}
			if !recovered {
				m.clearRecentConnections()
				if lastErr == nil {
					lastErr = errors.New("gateway control channel closed")
				}
				m.fail(ctx, state, "Gateway control channel closed; reconnect required", lastErr)
				return
			}
			m.AppendLog("INFO", "gateway control channel restored")
			controlLost = m.intercept.ControlLost()
		case <-ticker.C:
			metrics, err := core.Snapshot(ctx)
			if err != nil {
				continue
			}
			m.mu.RLock()
			tracker := m.trafficTracker
			phase := m.state.Phase
			m.mu.RUnlock()
			if phase != PhaseConnected {
				continue
			}
			metrics = mergeTrafficTracker(metrics, tracker)
			retained := m.retainMetrics(metrics)
			m.mu.Lock()
			if m.state.Phase != PhaseConnected {
				m.mu.Unlock()
				continue
			}
			m.state.Metrics = retained
			m.state.UpdatedAt = time.Now()
			m.mu.Unlock()
			m.publishMetrics(retained)
		}
	}
}

func trafficDialer(endpoint singbox.TrafficEndpoint) traffic.Dialer {
	return traffic.Dialer{Endpoint: traffic.Endpoint{
		Address: endpoint.Address, Username: endpoint.Username, Password: endpoint.Password,
	}}
}

func trackedTrafficDialer(
	endpoint singbox.TrafficEndpoint, feature string, tracker *traffic.Tracker,
) intercept.TrafficDialer {
	if endpoint.Address == "" {
		return nil
	}
	return traffic.TrackedDialer{
		Inner:   trafficDialer(endpoint),
		Feature: feature,
		Tracker: tracker,
	}
}

func trackedPortForwardDialer(
	endpoint singbox.TrafficEndpoint, tracker *traffic.Tracker,
) portfwd.TrafficDialer {
	if endpoint.Address == "" {
		return nil
	}
	return traffic.TrackedDialer{
		Inner:   trafficDialer(endpoint),
		Feature: singbox.TrafficUserPortForward,
		Tracker: tracker,
	}
}

// mergeTrafficTracker dyes clash traffic-in rows and injects Adapter-tracked
// connections that clash_api missed (short-lived or no metadata.user).
func mergeTrafficTracker(metrics singbox.Metrics, tracker *traffic.Tracker) singbox.Metrics {
	if tracker == nil {
		return metrics
	}
	live := tracker.Snapshot()
	if len(live) == 0 && len(metrics.Connections) == 0 {
		return metrics
	}
	seenPorts := make(map[string]struct{}, len(metrics.Connections))
	for i := range metrics.Connections {
		conn := &metrics.Connections[i]
		if conn.Inbound != singbox.TrafficInbound {
			continue
		}
		_, port, err := net.SplitHostPort(conn.Source)
		if err != nil {
			continue
		}
		seenPorts[port] = struct{}{}
		if feature := tracker.FeatureBySourcePort(port); feature != "" {
			conn.Feature = feature
		}
	}
	for _, item := range live {
		_, port, _ := net.SplitHostPort(item.Source)
		if port != "" {
			if _, ok := seenPorts[port]; ok {
				continue
			}
		}
		network := item.Network
		if network == "tcp4" || network == "tcp6" {
			network = "tcp"
		}
		if network == "udp4" || network == "udp6" {
			network = "udp"
		}
		metrics.Connections = append(metrics.Connections, singbox.Connection{
			ID:          "adapter-" + item.ID,
			Network:     network,
			Source:      item.Source,
			Destination: item.Destination,
			Process:     "KubeLoop",
			Upload:      item.Upload,
			Download:    item.Download,
			StartedAt:   item.StartedAt.Format(time.RFC3339Nano),
			Inbound:     singbox.TrafficInbound,
			Feature:     item.Feature,
			Outbound:    trafficOutboundForFeature(item.Feature),
		})
	}
	metrics.ActiveConnections = len(metrics.Connections)
	return metrics
}

func trafficOutboundForFeature(feature string) string {
	switch feature {
	case singbox.TrafficUserPortForward:
		return singbox.KubernetesOutbound
	case singbox.TrafficUserExchange, singbox.TrafficUserPreview, singbox.TrafficUserMirrorShadow:
		return singbox.LocalOutbound
	default:
		return singbox.DirectOutbound
	}
}

func (m *Manager) applyInventory(snap cluster.InventorySnapshot) {
	m.mu.Lock()
	if m.state.Phase != PhaseConnected || m.state.Discovery == nil {
		m.mu.Unlock()
		return
	}
	next := m.state
	discovery := *next.Discovery
	discovery.Pods = snap.Pods
	discovery.Services = snap.Services
	discovery.Deployments = snap.Deployments
	discovery.ServiceIPs = append([]string{}, snap.ServiceIPs...)
	if snap.DNSServer != "" {
		discovery.DNSServer = snap.DNSServer
	}
	next.Discovery = &discovery
	next.Pods = append([]cluster.PodInfo{}, snap.PodItems...)
	next.Services = append([]cluster.ServiceInfo{}, snap.ServiceItems...)
	next.InventoryRevision++
	m.state = next
	m.mu.Unlock()

	ctx := context.Background()
	m.reconcileBindings(ctx, snap)
	m.publish(m.State())
}

func (m *Manager) retainMetrics(metrics singbox.Metrics) *singbox.Metrics {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recentConnections == nil {
		m.recentConnections = make(map[string]recentConnection)
	}
	for _, connection := range metrics.Connections {
		m.recentConnections[connection.ID] = recentConnection{
			connection: connection,
			lastSeen:   now,
		}
	}
	for id, item := range m.recentConnections {
		if now.Sub(item.lastSeen) > connectionRetainFor {
			delete(m.recentConnections, id)
		}
	}
	m.pruneRecentConnections(maxRetainedConnections)

	if len(m.recentConnections) == 0 {
		metrics.Connections = []singbox.Connection{}
		m.lastTraffic = nil
		return &metrics
	}
	live := make(map[string]singbox.Connection, len(metrics.Connections))
	for _, connection := range metrics.Connections {
		live[connection.ID] = connection
	}
	merged := make([]singbox.Connection, 0, len(m.recentConnections))
	for id, item := range m.recentConnections {
		if connection, ok := live[id]; ok {
			merged = append(merged, connection)
			continue
		}
		merged = append(merged, item.connection)
	}
	metrics.Connections = limitConnections(
		m.annotateSpeeds(merged, now),
		maxPublishedConnections,
	)
	return &metrics
}

func (m *Manager) pruneRecentConnections(limit int) {
	if limit <= 0 || len(m.recentConnections) <= limit {
		return
	}
	items := make([]recentConnection, 0, len(m.recentConnections))
	for _, item := range m.recentConnections {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return connectionRank(items[i].connection) > connectionRank(items[j].connection)
	})
	m.recentConnections = make(map[string]recentConnection, limit)
	for _, item := range items[:limit] {
		m.recentConnections[item.connection.ID] = item
	}
}

func limitConnections(connections []singbox.Connection, limit int) []singbox.Connection {
	if limit <= 0 || len(connections) <= limit {
		return connections
	}
	sort.SliceStable(connections, func(i, j int) bool {
		return connectionRank(connections[i]) > connectionRank(connections[j])
	})
	return connections[:limit]
}

func connectionRank(connection singbox.Connection) int64 {
	return connection.DownloadSpeed + connection.UploadSpeed + connection.Download + connection.Upload
}

func (m *Manager) annotateSpeeds(connections []singbox.Connection, now time.Time) []singbox.Connection {
	if m.lastTraffic == nil {
		m.lastTraffic = make(map[string]connectionTraffic)
	}
	next := make(map[string]connectionTraffic, len(connections))
	for i := range connections {
		connection := &connections[i]
		next[connection.ID] = connectionTraffic{
			upload:   connection.Upload,
			download: connection.Download,
			at:       now,
		}
		previous, ok := m.lastTraffic[connection.ID]
		if !ok {
			continue
		}
		elapsed := now.Sub(previous.at).Seconds()
		if elapsed <= 0 {
			continue
		}
		if connection.DownloadSpeed == 0 {
			speed := int64(float64(connection.Download-previous.download) / elapsed)
			if speed > 0 {
				connection.DownloadSpeed = speed
			}
		}
		if connection.UploadSpeed == 0 {
			speed := int64(float64(connection.Upload-previous.upload) / elapsed)
			if speed > 0 {
				connection.UploadSpeed = speed
			}
		}
	}
	m.lastTraffic = next
	return connections
}

func (m *Manager) clearRecentConnections() {
	m.mu.Lock()
	m.recentConnections = nil
	m.lastTraffic = nil
	m.mu.Unlock()
}

func (m *Manager) Disconnect() error {
	return m.disconnect(true)
}

// Shutdown persists restore intents, then tears down runtime without clearing
// the "was connected" flag used for next-launch recovery.
func (m *Manager) Shutdown() error {
	m.PersistShutdown()
	m.StopAllPortForwards()
	return m.disconnect(false)
}

func (m *Manager) disconnect(clearConnected bool) error {
	state := m.State()
	m.clearRecentConnections()
	m.mu.RLock()
	cancel, done := m.cancel, m.done
	m.mu.RUnlock()
	if cancel == nil {
		m.publish(State{
			Phase:             PhaseIdle,
			Message:           "Disconnected",
			CoreVersion:       singbox.Version,
			KubernetesVersion: state.KubernetesVersion,
			Context:           state.Context,
			Namespace:         state.Namespace,
		})
		if clearConnected {
			m.markDisconnected(state.Context, state.Namespace)
		}
		return nil
	}
	cancel()
	select {
	case <-done:
		if clearConnected {
			m.markDisconnected(state.Context, state.Namespace)
		}
		return nil
	case <-time.After(25 * time.Second):
		return errors.New("timed out cleaning up the active connection")
	}
}

func (m *Manager) markDisconnected(contextName, namespace string) {
	if m.store == nil || contextName == "" {
		return
	}
	if err := m.store.SetConnected(contextName, namespace, false); err != nil {
		log.Printf("persist disconnected state: %v", err)
	}
}

func (m *Manager) StartIntercept(ctx context.Context, mapping intercept.Mapping) (intercept.Info, error) {
	info, err := m.intercept.StartIntercept(ctx, mapping)
	if err == nil && !m.isRestoring() {
		m.persistExchanges(m.State().Context)
		m.AppendLog("INFO", fmt.Sprintf("started exchange %s/%s", mapping.Namespace, mapping.Service))
	}
	return info, err
}

func (m *Manager) StartMirror(ctx context.Context, mapping intercept.Mapping) (intercept.Info, error) {
	info, err := m.intercept.StartMirror(ctx, mapping)
	if err == nil && !m.isRestoring() {
		m.persistMirrors(m.State().Context)
		m.AppendLog("INFO", fmt.Sprintf("started mirror %s/%s", mapping.Namespace, mapping.Service))
	}
	return info, err
}

func (m *Manager) StopIntercept(ctx context.Context, id string) error {
	err := m.intercept.Stop(ctx, id)
	if err == nil && !m.isRestoring() {
		m.persistExchanges(m.State().Context)
		m.persistMirrors(m.State().Context)
		m.AppendLog("INFO", fmt.Sprintf("stopped intercept %s", id))
	}
	return err
}

func (m *Manager) ListIntercepts() []intercept.Info {
	return m.intercept.List()
}

func (m *Manager) ListMirrors() []intercept.Info {
	return m.intercept.ListMirrors()
}

func (m *Manager) StartPreview(ctx context.Context, request intercept.PreviewRequest) (intercept.Info, error) {
	info, err := m.intercept.StartPreview(ctx, request)
	if err == nil && !m.isRestoring() {
		m.persistPreviews(m.State().Context)
		m.AppendLog("INFO", fmt.Sprintf("started preview %s/%s", request.Namespace, request.Name))
	}
	return info, err
}

func (m *Manager) StopPreview(ctx context.Context, id string) error {
	err := m.intercept.Stop(ctx, id)
	if err == nil && !m.isRestoring() {
		m.persistPreviews(m.State().Context)
		m.AppendLog("INFO", fmt.Sprintf("stopped preview %s", id))
	}
	return err
}

func (m *Manager) ListPreviews() []intercept.Info {
	return m.intercept.ListPreviews()
}

func (m *Manager) StartPortForwardSession(
	ctx context.Context, request portfwd.Request,
) (portfwd.Info, error) {
	info, err := m.portfwd.Start(ctx, request)
	if err == nil {
		m.persistPortForwards()
		m.AppendLog("INFO", fmt.Sprintf(
			"started port-forward %s/%s/%s:%d/%s → :%d",
			request.Kind, request.Namespace, request.Name, request.RemotePort,
			info.Protocol, info.LocalPort,
		))
	}
	return info, err
}

func (m *Manager) StopPortForward(id string) error {
	err := m.portfwd.Stop(id)
	if err == nil {
		m.persistPortForwards()
		m.AppendLog("INFO", fmt.Sprintf("stopped port-forward %s", id))
	}
	return err
}

func (m *Manager) ListPortForwards() []portfwd.Info {
	return m.portfwd.List()
}

func (m *Manager) StopAllPortForwards() {
	m.portfwd.StopAll()
}

// ResetSessions stops every port-forward, exchange, and mirror, then clears their
// persisted intents from state.json. This still clears disk state when the
// cluster is unavailable and live stop fails. Previews are left alone.
func (m *Manager) ResetSessions(ctx context.Context) error {
	for _, item := range m.portfwd.List() {
		if err := m.StopPortForward(item.ID); err != nil {
			m.AppendLog("WARN", fmt.Sprintf("reset stop port-forward %s: %v", item.ID, err))
		}
	}
	for _, item := range m.intercept.List() {
		if err := m.StopIntercept(ctx, item.ID); err != nil {
			m.AppendLog("WARN", fmt.Sprintf("reset stop exchange %s: %v", item.ID, err))
		}
	}
	for _, item := range m.intercept.ListMirrors() {
		if err := m.StopIntercept(ctx, item.ID); err != nil {
			m.AppendLog("WARN", fmt.Sprintf("reset stop mirror %s: %v", item.ID, err))
		}
	}
	if err := m.clearPersistedSessions(); err != nil {
		return err
	}
	m.AppendLog("INFO", "reset sessions: cleared port-forwards, exchanges, and mirrors")
	return nil
}

// SessionIntentCounts returns persisted restore intents from state.json.
func (m *Manager) SessionIntentCounts() store.SessionIntentCounts {
	if m.store == nil {
		return store.SessionIntentCounts{}
	}
	return m.store.SessionIntentCounts()
}

func (m *Manager) fail(ctx context.Context, state State, message string, err error) {
	if ctx.Err() != nil {
		return
	}
	state.Phase = PhaseError
	state.Message = message
	state.Error = err.Error()
	state.ConnectedAt = nil
	m.publish(state)
	m.AppendLog("ERROR", message+": "+err.Error())
}

func (m *Manager) ManualNetwork(contextName string) cluster.ManualNetwork {
	if m.store == nil || contextName == "" {
		return cluster.ManualNetwork{}
	}
	item := m.store.ManualNetwork(contextName)
	return cluster.ManualNetwork{
		PodCIDRs:       item.PodCIDRs,
		ServiceCIDRs:   item.ServiceCIDRs,
		DNSServer:      item.DNSServer,
		ClusterDomains: item.ClusterDomains,
		DNSNamespace:   item.DNSNamespace,
	}
}

func (m *Manager) SetManualNetwork(contextName string, network cluster.ManualNetwork) error {
	if m.store == nil {
		return errors.New("state store is unavailable")
	}
	if contextName == "" {
		return errors.New("context is required")
	}
	normalized, err := cluster.NormalizeManualNetwork(network)
	if err != nil {
		return err
	}
	return m.store.SetManualNetwork(contextName, store.ManualNetwork{
		PodCIDRs:       normalized.PodCIDRs,
		ServiceCIDRs:   normalized.ServiceCIDRs,
		DNSServer:      normalized.DNSServer,
		ClusterDomains: normalized.ClusterDomains,
		DNSNamespace:   normalized.DNSNamespace,
	})
}

// SetDNSNamespace updates short-name search namespace for the active tunnel.
func (m *Manager) SetDNSNamespace(contextName, namespace string) error {
	namespace = strings.TrimSpace(namespace)
	if contextName == "" {
		return errors.New("context is required")
	}
	if namespace == "" {
		namespace = "default"
	}
	normalized, err := cluster.NormalizeManualNetwork(cluster.ManualNetwork{DNSNamespace: namespace})
	if err != nil {
		return err
	}
	namespace = normalized.DNSNamespace
	if namespace == "" {
		namespace = "default"
	}
	if m.store != nil {
		current := m.ManualNetwork(contextName)
		current.DNSNamespace = namespace
		if err := m.SetManualNetwork(contextName, current); err != nil {
			return err
		}
	}
	m.mu.Lock()
	core := m.runningCore
	state := m.state
	m.mu.Unlock()
	if core == nil || state.Phase != PhaseConnected || state.Context != contextName {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := core.UpdateDNSNamespace(ctx, namespace); err != nil {
		return err
	}
	m.mu.Lock()
	next := m.state
	next.DNSNamespace = namespace
	next.DNSWarning = ""
	m.state = next
	m.mu.Unlock()
	m.publish(next)
	m.AppendLog("INFO", "DNS search namespace set to "+namespace)
	m.probeClusterDNS(ctx, next, core)
	return nil
}

func (m *Manager) probeClusterDNS(parent context.Context, state State, core singbox.RunningCore) {
	if core == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	if err := core.ProbeClusterDNS(ctx); err != nil {
		warning := "Cluster DNS probe failed; split DNS may be overridden by another proxy (for example Clash Verge TUN/system DNS). Try disabling the other client's TUN DNS or reconnect KubeLoop last."
		m.mu.Lock()
		next := m.state
		if next.Phase == PhaseConnected && next.Context == state.Context {
			next.DNSWarning = warning
			m.state = next
			m.mu.Unlock()
			m.publish(next)
			m.AppendLog("WARN", warning+": "+err.Error())
			return
		}
		m.mu.Unlock()
	}
}

func (m *Manager) HostAliases(contextName string) []store.HostAliasSpec {
	if m.store == nil || contextName == "" {
		return nil
	}
	return m.store.HostAliases(contextName)
}

// SetHostAliases replaces host aliases for a context. An empty list clears stored config.
func (m *Manager) SetHostAliases(contextName string, items []store.HostAliasSpec) error {
	if m.store == nil {
		return errors.New("state store is unavailable")
	}
	if contextName == "" {
		return errors.New("context is required")
	}
	normalized, err := normalizeHostAliasSpecs(items)
	if err != nil {
		return err
	}
	return m.store.SetHostAliases(contextName, normalized)
}

func (m *Manager) hostAliasesFor(contextName string) []singbox.HostAlias {
	items := m.HostAliases(contextName)
	if len(items) == 0 {
		return nil
	}
	out := make([]singbox.HostAlias, 0, len(items))
	for _, item := range items {
		out = append(out, singbox.HostAlias{Domain: item.Domain, IP: item.IP})
	}
	return out
}

func normalizeHostAliasSpecs(items []store.HostAliasSpec) ([]store.HostAliasSpec, error) {
	if len(items) == 0 {
		return nil, nil
	}
	converted := make([]singbox.HostAlias, 0, len(items))
	for _, item := range items {
		converted = append(converted, singbox.HostAlias{Domain: item.Domain, IP: item.IP})
	}
	normalized, err := singbox.NormalizeHostAliases(converted)
	if err != nil {
		return nil, err
	}
	out := make([]store.HostAliasSpec, 0, len(normalized))
	for _, item := range normalized {
		out = append(out, store.HostAliasSpec{Domain: item.Domain, IP: item.IP})
	}
	return out, nil
}

func (m *Manager) GatewayInstallManifest() string {
	return cluster.GatewayInstallManifest(m.gatewayImage)
}

func (m *Manager) publish(state State) {
	state.UpdatedAt = time.Now()
	m.mu.Lock()
	if state.Events == nil {
		state.Events = m.state.Events
	}
	// Keep the latest high-frequency metrics when a slower state publish
	// (inventory, phase change) does not carry a fresher snapshot.
	if state.Metrics == nil && m.state.Metrics != nil && state.Phase == PhaseConnected {
		state.Metrics = m.state.Metrics
	}
	m.state = state
	listeners := append([]func(State){}, m.listeners...)
	m.mu.Unlock()
	for _, listener := range listeners {
		listener(state)
	}
}

func (m *Manager) publishMetrics(metrics *singbox.Metrics) {
	if metrics == nil {
		return
	}
	m.mu.RLock()
	listeners := append([]func(*singbox.Metrics){}, m.metricsListeners...)
	m.mu.RUnlock()
	for _, listener := range listeners {
		listener(metrics)
	}
}

type closerFunc func()

func (function closerFunc) Close() error {
	function()
	return nil
}

func (state State) String() string {
	if state.Error != "" {
		return fmt.Sprintf("%s: %s", state.Message, state.Error)
	}
	return state.Message
}
