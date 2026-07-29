package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
	"github.com/fengqi-dev/kube-loop/internal/socksbridge"
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

type Request struct {
	Context   string
	Namespace string
}

type State struct {
	Phase       Phase              `json:"phase"`
	Context     string             `json:"context"`
	Namespace   string             `json:"namespace"`
	Message     string             `json:"message"`
	Error       string             `json:"error,omitempty"`
	Discovery   *cluster.Discovery `json:"discovery,omitempty"`
	CoreVersion string             `json:"coreVersion,omitempty"`
	ConnectedAt *time.Time         `json:"connectedAt,omitempty"`
	Metrics     *singbox.Metrics   `json:"metrics,omitempty"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

type ClusterProvider interface {
	Contexts() ([]cluster.ContextInfo, error)
	Namespaces(context.Context, string) ([]string, error)
	Discover(context.Context, string) (cluster.Discovery, error)
	WatchInventory(
		context.Context,
		string,
		func(cluster.InventorySnapshot),
	) (io.Closer, error)
	EnsureGateway(context.Context, string, string) (string, error)
	StartPortForward(context.Context, string, string, uint16) (cluster.PortForward, error)
}

type Core interface {
	Start(
		context.Context,
		cluster.Discovery,
		string,
		string,
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
	provider      ClusterProvider
	core          Core
	bridgeFactory BridgeFactory
	gatewayImage  string

	mu        sync.RWMutex
	state     State
	cancel    context.CancelFunc
	done      chan struct{}
	listeners []func(State)

	recentConnections map[string]recentConnection
	lastTraffic       map[string]connectionTraffic
}

// Keep short-lived TUN connections visible between core snapshot polls.
const connectionRetainFor = 30 * time.Second

// Bound retained/published rows so Wails/React are not flooded by bursty TUN flows.
const (
	maxRetainedConnections  = 500
	maxPublishedConnections = 100
)

func NewManager(provider ClusterProvider, options ...Option) *Manager {
	image := os.Getenv("KUBELOOP_GATEWAY_IMAGE")
	if image == "" {
		image = DefaultGatewayImage
	}
	manager := &Manager{
		provider:      provider,
		core:          &singbox.Runtime{},
		bridgeFactory: socksbridge.Listen,
		gatewayImage:  image,
		state: State{
			Phase: PhaseIdle, Message: "未连接", CoreVersion: singbox.Version, UpdatedAt: time.Now(),
		},
	}
	for _, option := range options {
		option(manager)
	}
	return manager
}

func (m *Manager) Contexts() ([]cluster.ContextInfo, error) {
	return m.provider.Contexts()
}

func (m *Manager) Namespaces(ctx context.Context, contextName string) ([]string, error) {
	return m.provider.Namespaces(ctx, contextName)
}

func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Manager) Subscribe(listener func(State)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, listener)
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
		}
		m.mu.Unlock()
		close(done)
	}()

	state := State{
		Phase: PhaseChecking, Context: request.Context, Namespace: request.Namespace,
		Message: "正在检查 Kubernetes 访问权限", CoreVersion: singbox.Version,
	}
	m.publish(state)

	state.Phase = PhaseInstalling
	state.Message = "正在安装或检查集群 Gateway"
	m.publish(state)
	podName, err := m.provider.EnsureGateway(ctx, request.Context, m.gatewayImage)
	if err != nil {
		m.fail(ctx, state, "无法安装集群 Gateway", err)
		return
	}

	state.Phase = PhaseDiscovering
	state.Message = "正在发现 Pod、Service 和集群 DNS"
	m.publish(state)
	discovery, err := m.provider.Discover(ctx, request.Context)
	if err != nil {
		m.fail(ctx, state, "无法读取集群网络信息", err)
		return
	}
	state.Discovery = &discovery

	forwarder, err := m.provider.StartPortForward(
		ctx, request.Context, podName, cluster.GatewayPort,
	)
	if err != nil {
		m.fail(ctx, state, "无法建立 Gateway 安全通道", err)
		return
	}
	resources = append(resources, forwarder)

	bridgeContext, stopBridge := context.WithCancel(ctx)
	resources = append(resources, closerFunc(stopBridge))
	bridge, err := m.bridgeFactory(bridgeContext, forwarder.Address())
	if err != nil {
		m.fail(ctx, state, "无法启动本地 SOCKS Bridge", err)
		return
	}
	resources = append(resources, bridge)

	state.Phase = PhaseStarting
	state.Message = "正在安装并启动 sing-box TUN"
	m.publish(state)
	core, err := m.core.Start(ctx, discovery, bridge.Addr().String(), request.Namespace)
	if err != nil {
		m.fail(ctx, state, "无法启动 sing-box TUN", err)
		return
	}
	resources = append(resources, core)

	connectedAt := time.Now()
	state.Phase = PhaseConnected
	state.Message = "已连接，可访问 Pod、Service 和集群 DNS"
	state.ConnectedAt = &connectedAt
	state.Metrics = &singbox.Metrics{}
	m.publish(state)

	inventory, err := m.provider.WatchInventory(ctx, request.Context, func(snap cluster.InventorySnapshot) {
		m.applyInventory(snap)
	})
	if err != nil {
		m.fail(ctx, state, "无法监听集群资源变化", err)
		return
	}
	resources = append(resources, inventory)

	ticker := time.NewTicker(singbox.DefaultMetricsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.clearRecentConnections()
			m.publish(State{
				Phase: PhaseIdle, Message: "未连接", CoreVersion: singbox.Version,
			})
			return
		case <-core.Done():
			m.clearRecentConnections()
			if ctx.Err() == nil {
				err := core.Err()
				if err == nil {
					err = errors.New("sing-box stopped unexpectedly")
				}
				m.fail(ctx, state, "sing-box TUN 意外退出", err)
			}
			return
		case <-ticker.C:
			metrics, err := core.Snapshot(ctx)
			if err != nil {
				continue
			}
			retained := m.retainMetrics(metrics)
			m.mu.RLock()
			next := m.state
			m.mu.RUnlock()
			if next.Phase != PhaseConnected {
				continue
			}
			next.Metrics = retained
			m.publish(next)
		}
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
	m.mu.Unlock()
	m.publish(next)
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
	m.clearRecentConnections()
	m.mu.RLock()
	cancel, done := m.cancel, m.done
	m.mu.RUnlock()
	if cancel == nil {
		m.publish(State{Phase: PhaseIdle, Message: "未连接", CoreVersion: singbox.Version})
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-time.After(8 * time.Second):
		return errors.New("timed out cleaning up the active connection")
	}
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
}

func (m *Manager) publish(state State) {
	state.UpdatedAt = time.Now()
	m.mu.Lock()
	m.state = state
	listeners := append([]func(State){}, m.listeners...)
	m.mu.Unlock()
	for _, listener := range listeners {
		listener(state)
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
