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

	corev1 "k8s.io/api/core/v1"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
	"github.com/fengqi-dev/kube-loop/internal/socksbridge"
	"github.com/fengqi-dev/kube-loop/internal/store"
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
	Message         string                `json:"message"`
	Error           string                `json:"error,omitempty"`
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

type ClusterProvider interface {
	Contexts() ([]cluster.ContextInfo, error)
	Namespaces(context.Context, string) ([]string, error)
	ServerVersion(context.Context, string) (string, error)
	ListServices(context.Context, string, string) ([]cluster.ServiceInfo, error)
	ListPods(context.Context, string, string) ([]cluster.PodInfo, error)
	Discover(context.Context, string, []string) (cluster.Discovery, error)
	WatchInventory(
		context.Context,
		string,
		[]string,
		func(cluster.InventorySnapshot),
	) (io.Closer, error)
	ProbeCapabilities(context.Context, string) (cluster.Capabilities, error)
	GetGateway(context.Context, string) (cluster.GatewayInfo, error)
	EnsureGateway(context.Context, string, string) (cluster.GatewayInfo, error)
	StartPortForward(context.Context, string, string, uint16) (cluster.PortForward, error)
	StartPodPortForward(context.Context, string, string, string, uint16, uint16) (cluster.PortForward, error)
	ResolveServiceBackend(context.Context, string, string, string, int32) (string, uint16, error)
	ApplyServiceIntercept(context.Context, string, *cluster.ServiceInterceptSnapshot, string) error
	RestoreServiceIntercept(context.Context, string, cluster.ServiceInterceptSnapshot) error
	CreatePreviewService(context.Context, string, cluster.PreviewServiceSnapshot, string) (*corev1.Service, error)
	DeletePreviewService(context.Context, string, cluster.PreviewServiceSnapshot) error
	GetService(context.Context, string, string, string) (*corev1.Service, error)
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
	provider      ClusterProvider
	core          Core
	bridgeFactory BridgeFactory
	gatewayImage  string
	store         *store.Store

	mu        sync.RWMutex
	state     State
	cancel    context.CancelFunc
	done      chan struct{}
	listeners []func(State)
	intercept *intercept.Manager
	portfwd   *portfwd.Manager

	recentConnections map[string]recentConnection
	lastTraffic       map[string]connectionTraffic
	restoring         bool
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
		provider:      provider,
		core:          newSingboxRuntime(),
		bridgeFactory: socksbridge.Listen,
		gatewayImage:  ResolveGatewayImage(""),
		intercept:     intercept.NewManager(provider),
		portfwd:       portfwd.NewManager(provider),
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

func (m *Manager) ListServices(
	ctx context.Context, contextName, namespace string,
) ([]cluster.ServiceInfo, error) {
	return m.provider.ListServices(ctx, contextName, namespace)
}

func (m *Manager) ListPods(
	ctx context.Context, contextName, namespace string,
) ([]cluster.PodInfo, error) {
	return m.provider.ListPods(ctx, contextName, namespace)
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

	prev := m.State()
	state := State{
		Phase: PhaseChecking, Context: request.Context, Namespace: request.Namespace,
		Message: "正在检查 Kubernetes 访问权限", CoreVersion: singbox.Version,
		// Keep the last probed version so the Overview subtitle does not flash
		// back to the cluster name while ServerVersion is re-fetched.
		KubernetesVersion: prev.KubernetesVersion,
	}
	m.publish(state)
	m.AppendLog("INFO", fmt.Sprintf("connecting to context %s", request.Context))

	caps, err := m.provider.ProbeCapabilities(ctx, request.Context)
	if err != nil {
		m.fail(ctx, state, "无法检查集群权限", err)
		return
	}
	state.Capabilities = &caps
	state.ScopeNamespaces = append([]string{}, caps.ScopeNamespaces...)
	if version, versionErr := m.provider.ServerVersion(ctx, request.Context); versionErr == nil {
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
		m.fail(ctx, state, "当前账号无法对 Gateway 建立 port-forward", errors.New("missing pods/portforward in kubeloop-system"))
		return
	}

	state.Phase = PhaseInstalling
	state.Message = "正在安装或检查集群 Gateway"
	m.publish(state)
	var gateway cluster.GatewayInfo
	if caps.GatewayInstall {
		gateway, err = m.provider.EnsureGateway(ctx, request.Context, m.gatewayImage)
		if err != nil {
			m.fail(ctx, state, "无法安装集群 Gateway", err)
			return
		}
	} else {
		gateway, err = m.provider.GetGateway(ctx, request.Context)
		if err != nil {
			state.GatewayManifest = cluster.GatewayInstallManifest(m.gatewayImage)
			m.fail(ctx, state, "未找到预装 Gateway，请管理员安装或授予安装权限", err)
			return
		}
	}

	state.Phase = PhaseDiscovering
	state.Message = "正在发现 Pod、Service 和集群 DNS"
	m.publish(state)
	scopeNS := caps.ScopeNamespaces
	if caps.InventoryCluster {
		scopeNS = nil
	}
	discovery, err := m.provider.Discover(ctx, request.Context, scopeNS)
	if err != nil {
		m.fail(ctx, state, "无法读取集群网络信息", err)
		return
	}
	if m.store != nil {
		manual := m.store.ManualNetwork(request.Context)
		discovery = cluster.MergeManualNetwork(discovery, cluster.ManualNetwork{
			PodCIDRs:     manual.PodCIDRs,
			ServiceCIDRs: manual.ServiceCIDRs,
			DNSServer:    manual.DNSServer,
		})
	}
	state.Discovery = &discovery

	forwarder, err := m.provider.StartPortForward(
		ctx, request.Context, gateway.Name, cluster.GatewayPort,
	)
	if err != nil {
		m.fail(ctx, state, "无法建立 Gateway 安全通道", err)
		return
	}
	resources = append(resources, forwarder)

	if err := m.intercept.Start(ctx, request.Context, gateway.IP, forwarder.Address()); err != nil {
		m.fail(ctx, state, "无法启动 Service Intercept 控制通道", err)
		return
	}
	resources = append(resources, closerFunc(func() {
		_ = m.intercept.StopAll(context.Background())
	}))

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
	hosts := m.hostAliasesFor(request.Context)
	core, err := m.core.Start(ctx, discovery, bridge.Addr().String(), request.Namespace, hosts)
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
	state.Capabilities = &caps
	state.ScopeNamespaces = append([]string{}, caps.ScopeNamespaces...)
	m.publish(state)
	m.AppendLog("INFO", fmt.Sprintf("connected to context %s", request.Context))
	if m.store != nil {
		if err := m.store.SetConnected(request.Context, request.Namespace, true); err != nil {
			log.Printf("persist connected state: %v", err)
		}
	}
	m.restoreBindings(ctx, request.Context)

	inventory, err := m.provider.WatchInventory(ctx, request.Context, scopeNS, func(snap cluster.InventorySnapshot) {
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
				Phase:             PhaseIdle,
				Message:           "未连接",
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
			Message:           "未连接",
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
			"started port-forward %s/%s/%s:%d → :%d",
			request.Kind, request.Namespace, request.Name, request.RemotePort, info.LocalPort,
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
		PodCIDRs: item.PodCIDRs, ServiceCIDRs: item.ServiceCIDRs, DNSServer: item.DNSServer,
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
		PodCIDRs: normalized.PodCIDRs, ServiceCIDRs: normalized.ServiceCIDRs, DNSServer: normalized.DNSServer,
	})
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
