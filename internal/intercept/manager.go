package intercept

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

// PortMapping maps one Service port onto a local listener.
type PortMapping struct {
	ServicePort int32  `json:"servicePort"`
	Protocol    string `json:"protocol"` // tcp or udp
	LocalHost   string `json:"localHost"`
	LocalPort   int    `json:"localPort"`
}

// Mapping replaces a cluster Service with local targets.
type Mapping struct {
	Namespace string        `json:"namespace"`
	Service   string        `json:"service"`
	Ports     []PortMapping `json:"ports"`
	Mode      string        `json:"mode,omitempty"` // exchange (default) or mirror
}

// PreviewRequest creates a new ClusterIP Service that exposes a local process.
type PreviewRequest struct {
	Namespace string        `json:"namespace"`
	Name      string        `json:"name"`
	Ports     []PortMapping `json:"ports"`
}

// Info is a running intercept, mirror, or preview visible to the UI.
type Info struct {
	ID        string                  `json:"id"`
	Namespace string                  `json:"namespace"`
	Service   string                  `json:"service"`
	ClusterIP string                  `json:"clusterIP,omitempty"`
	Preview   bool                    `json:"preview,omitempty"`
	Mode      string                  `json:"mode,omitempty"` // exchange | mirror
	Ports     []cluster.InterceptPort `json:"ports"`
	Locals    []PortMapping           `json:"locals"`
}

type ClusterAPI interface {
	GetService(context.Context, string, string, string) (*corev1.Service, error)
	ApplyServiceIntercept(context.Context, string, *cluster.ServiceInterceptSnapshot, string) error
	RestoreServiceIntercept(context.Context, string, cluster.ServiceInterceptSnapshot) error
	CreatePreviewService(context.Context, string, cluster.PreviewServiceSnapshot, string) (*corev1.Service, error)
	DeletePreviewService(context.Context, string, cluster.PreviewServiceSnapshot) error
}

type TrafficDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type TrafficDialers struct {
	Exchange     TrafficDialer
	Preview      TrafficDialer
	MirrorShadow TrafficDialer
}

type Manager struct {
	cluster ClusterAPI

	mu             sync.Mutex
	active         bool
	stopping       bool
	recovering     bool
	ctx            context.Context
	contextName    string
	gatewayIP      string
	gatewayAddress string
	control        *controlClient
	controlLost    chan struct{}
	nextPort       uint32
	byID           map[string]*runtimeIntercept
	byKey          map[string]string // namespace/service -> id
	hostRoutes     map[hostRouteKey]*hostRoute
	traffic        TrafficDialers
}

type controlRegistration struct {
	id         string
	network    byte
	listenPort uint16
}

// SetTrafficDialers installs the fixed sing-box feature inbounds. It is
// intentionally separate from Start because the Gateway control channel is
// established before sing-box is launched during Session startup.
func (m *Manager) SetTrafficDialers(dialers TrafficDialers) {
	m.mu.Lock()
	m.traffic = dialers
	m.mu.Unlock()
}

type hostRouteKey struct {
	host string
	port uint16
}

// hostRoute serves host TUN traffic to an intercepted Service locally, so the
// desktop does not depend on kube-proxy hairpin through the Gateway.
type hostRoute struct {
	mode        string
	preview     bool
	local       PortMapping
	primaryAddr string
}

type runtimeIntercept struct {
	info         Info
	snapshot     cluster.ServiceInterceptSnapshot
	preview      *cluster.PreviewServiceSnapshot
	portKeys     map[string]PortMapping // subID -> local mapping
	primaryAddrs map[string]string      // subID -> pod host:port (mirror)
	hostKeys     []hostRouteKey
}

func NewManager(api ClusterAPI) *Manager {
	return &Manager{
		cluster:    api,
		nextPort:   20000,
		byID:       make(map[string]*runtimeIntercept),
		byKey:      make(map[string]string),
		hostRoutes: make(map[hostRouteKey]*hostRoute),
	}
}

func (m *Manager) Start(
	ctx context.Context, contextName, gatewayIP, gatewayAddress string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active {
		return fmt.Errorf("intercept manager already started")
	}
	control, err := dialControl(ctx, gatewayAddress)
	if err != nil {
		return err
	}
	lost := make(chan struct{})
	m.attachControlLocked(control, lost)
	m.active = true
	m.stopping = false
	m.recovering = false
	m.ctx = ctx
	m.contextName = contextName
	m.gatewayIP = gatewayIP
	m.gatewayAddress = gatewayAddress
	return nil
}

// ControlLost is closed when the Gateway control channel drops unexpectedly
// or after StopAll closes it. Session uses this to leave the connected state
// (or to attempt RecoverControl first).
func (m *Manager) ControlLost() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.controlLost
}

// RecoverControl redials the Gateway control channel on the existing
// port-forward address and re-registers active Exchange/Mirror/Preview ports.
func (m *Manager) RecoverControl(ctx context.Context) error {
	m.mu.Lock()
	if !m.active || m.stopping {
		m.mu.Unlock()
		return fmt.Errorf("session is not connected")
	}
	if m.recovering {
		m.mu.Unlock()
		return fmt.Errorf("control recovery already in progress")
	}
	if m.gatewayAddress == "" {
		m.mu.Unlock()
		return fmt.Errorf("gateway address is unavailable")
	}
	m.recovering = true
	address := m.gatewayAddress
	regs := m.controlRegistrationsLocked()
	old := m.control
	m.control = nil
	m.mu.Unlock()

	if old != nil {
		_ = old.close()
	}

	control, err := dialControl(ctx, address)
	if err != nil {
		m.mu.Lock()
		m.recovering = false
		m.mu.Unlock()
		return err
	}
	for _, reg := range regs {
		if err := control.register(reg.id, reg.network, reg.listenPort); err != nil {
			_ = control.close()
			m.mu.Lock()
			m.recovering = false
			m.mu.Unlock()
			return fmt.Errorf("re-register %s: %w", reg.id, err)
		}
	}

	lost := make(chan struct{})
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recovering = false
	if !m.active || m.stopping {
		_ = control.close()
		return fmt.Errorf("session is not connected")
	}
	m.attachControlLocked(control, lost)
	return nil
}

func (m *Manager) attachControlLocked(control *controlClient, lost chan struct{}) {
	control.onReady = m.handleReady
	control.onClose = sync.OnceFunc(func() { close(lost) })
	m.control = control
	m.controlLost = lost
}

func (m *Manager) controlRegistrationsLocked() []controlRegistration {
	regs := make([]controlRegistration, 0)
	for _, runtime := range m.byID {
		for subID := range runtime.portKeys {
			network, listenPort, ok := registrationFromRuntime(runtime, subID)
			if !ok {
				continue
			}
			regs = append(regs, controlRegistration{
				id: subID, network: network, listenPort: listenPort,
			})
		}
	}
	return regs
}

func registrationFromRuntime(runtime *runtimeIntercept, subID string) (byte, uint16, bool) {
	ports := runtime.info.Ports
	if len(ports) == 0 && runtime.preview != nil {
		ports = runtime.preview.Ports
	}
	if len(ports) == 0 {
		ports = runtime.snapshot.Ports
	}
	for _, port := range ports {
		network := tunnel.NetworkTCP
		if port.Protocol == corev1.ProtocolUDP {
			network = tunnel.NetworkUDP
		}
		want := fmt.Sprintf("%s:%s:%d", runtime.info.ID, networkName(network), port.ServicePort)
		if want != subID {
			continue
		}
		if port.ListenPort <= 0 || port.ListenPort > 65535 {
			return 0, 0, false
		}
		return network, uint16(port.ListenPort), true
	}
	return 0, 0, false
}

func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	m.stopping = true
	ids := make([]string, 0, len(m.byID))
	for id := range m.byID {
		ids = append(ids, id)
	}
	control := m.control
	m.control = nil
	m.active = false
	m.recovering = false
	m.mu.Unlock()

	var firstErr error
	for _, id := range ids {
		if err := m.Stop(ctx, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if control != nil {
		_ = control.close()
	}
	return firstErr
}

func (m *Manager) List() []Info {
	return m.listByMode(ModeExchange)
}

func (m *Manager) ListMirrors() []Info {
	return m.listByMode(ModeMirror)
}

func (m *Manager) ListPreviews() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Info, 0, len(m.byID))
	for _, item := range m.byID {
		if item.info.Preview {
			items = append(items, item.info)
		}
	}
	return items
}

func (m *Manager) listByMode(mode string) []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Info, 0, len(m.byID))
	for _, item := range m.byID {
		if item.info.Preview {
			continue
		}
		itemMode := item.info.Mode
		if itemMode == "" {
			itemMode = ModeExchange
		}
		if itemMode != mode {
			continue
		}
		items = append(items, item.info)
	}
	return items
}

func (m *Manager) StartIntercept(ctx context.Context, mapping Mapping) (Info, error) {
	mapping.Mode = ModeExchange
	return m.startServiceIntercept(ctx, mapping)
}

func (m *Manager) StartMirror(ctx context.Context, mapping Mapping) (Info, error) {
	mapping.Mode = ModeMirror
	return m.startServiceIntercept(ctx, mapping)
}

func (m *Manager) startServiceIntercept(ctx context.Context, mapping Mapping) (Info, error) {
	if mapping.Namespace == "" {
		mapping.Namespace = "default"
	}
	if mapping.Service == "" {
		return Info{}, fmt.Errorf("service is required")
	}
	mode := mapping.Mode
	if mode == "" {
		mode = ModeExchange
	}
	if mode != ModeExchange && mode != ModeMirror {
		return Info{}, fmt.Errorf("unsupported intercept mode %q", mode)
	}

	m.mu.Lock()
	if !m.active || m.control == nil || m.recovering {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("session is not connected")
	}
	key := mapping.Namespace + "/" + mapping.Service
	if existingID, exists := m.byKey[key]; exists {
		existing := m.byID[existingID]
		m.mu.Unlock()
		return Info{}, conflictError(key, mode, existing)
	}
	contextName := m.contextName
	gatewayIP := m.gatewayIP
	control := m.control
	m.mu.Unlock()

	service, err := m.cluster.GetService(ctx, contextName, mapping.Namespace, mapping.Service)
	if err != nil {
		return Info{}, err
	}
	locals, err := mapping.resolveLocals(service)
	if err != nil {
		return Info{}, err
	}
	ports, err := buildPortsForLocals(service, locals, m.allocateListenPort)
	if err != nil {
		return Info{}, err
	}

	interceptID := fmt.Sprintf("%s/%s", mapping.Namespace, mapping.Service)
	portKeys := make(map[string]PortMapping)
	for i, port := range ports {
		network := tunnel.NetworkTCP
		if port.Protocol == corev1.ProtocolUDP {
			network = tunnel.NetworkUDP
		}
		subID := fmt.Sprintf("%s:%s:%d", interceptID, networkName(network), port.ServicePort)
		local := localFor(ports[i], locals)
		if err := control.register(subID, network, uint16(port.ListenPort)); err != nil {
			m.rollbackRegisters(control, portKeys)
			return Info{}, fmt.Errorf("register %s: %w", subID, err)
		}
		portKeys[subID] = local
	}

	selector := map[string]string{}
	for k, v := range service.Spec.Selector {
		selector[k] = v
	}
	snapshot := &cluster.ServiceInterceptSnapshot{
		Namespace: mapping.Namespace,
		Service:   mapping.Service,
		Selector:  selector,
		Ports:     ports,
		GatewayIP: gatewayIP,
	}
	if err := m.cluster.ApplyServiceIntercept(ctx, contextName, snapshot, interceptID); err != nil {
		m.rollbackRegisters(control, portKeys)
		return Info{}, err
	}

	var primaryAddrs map[string]string
	if mode == ModeMirror {
		primaryAddrs, err = buildPrimaryAddrs(*snapshot, ports, portKeys, interceptID)
		if err != nil {
			_ = m.cluster.RestoreServiceIntercept(ctx, contextName, *snapshot)
			m.rollbackRegisters(control, portKeys)
			return Info{}, err
		}
	}

	info := Info{
		ID:        interceptID,
		Namespace: mapping.Namespace,
		Service:   mapping.Service,
		ClusterIP: service.Spec.ClusterIP,
		Mode:      mode,
		Ports:     ports,
		Locals:    locals,
	}
	hostKeys := m.installHostRoutes(service, ports, portKeys, primaryAddrs, mode, false, interceptID)
	m.mu.Lock()
	m.byID[interceptID] = &runtimeIntercept{
		info: info, snapshot: *snapshot, portKeys: portKeys, primaryAddrs: primaryAddrs, hostKeys: hostKeys,
	}
	m.byKey[key] = interceptID
	m.mu.Unlock()
	return info, nil
}

func (m *Manager) StartPreview(ctx context.Context, request PreviewRequest) (Info, error) {
	if request.Namespace == "" {
		request.Namespace = "default"
	}
	if request.Name == "" {
		return Info{}, fmt.Errorf("service name is required")
	}
	locals, err := normalizePreviewPorts(request.Ports)
	if err != nil {
		return Info{}, err
	}

	m.mu.Lock()
	if !m.active || m.control == nil || m.recovering {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("session is not connected")
	}
	key := request.Namespace + "/" + request.Name
	if _, exists := m.byKey[key]; exists {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("service %s is already in use", key)
	}
	contextName := m.contextName
	gatewayIP := m.gatewayIP
	control := m.control
	m.mu.Unlock()

	ports, err := buildPreviewPorts(locals, m.allocateListenPort)
	if err != nil {
		return Info{}, err
	}

	previewID := fmt.Sprintf("%s/%s", request.Namespace, request.Name)
	portKeys := make(map[string]PortMapping)
	for i, port := range ports {
		network := tunnel.NetworkTCP
		if port.Protocol == corev1.ProtocolUDP {
			network = tunnel.NetworkUDP
		}
		subID := fmt.Sprintf("%s:%s:%d", previewID, networkName(network), port.ServicePort)
		local := localFor(ports[i], locals)
		if err := control.register(subID, network, uint16(port.ListenPort)); err != nil {
			m.rollbackRegisters(control, portKeys)
			return Info{}, fmt.Errorf("register %s: %w", subID, err)
		}
		portKeys[subID] = local
	}

	snapshot := cluster.PreviewServiceSnapshot{
		Namespace: request.Namespace,
		Service:   request.Name,
		Ports:     ports,
		GatewayIP: gatewayIP,
	}
	service, err := m.cluster.CreatePreviewService(ctx, contextName, snapshot, previewID)
	if err != nil {
		m.rollbackRegisters(control, portKeys)
		return Info{}, err
	}

	info := Info{
		ID:        previewID,
		Namespace: request.Namespace,
		Service:   request.Name,
		ClusterIP: service.Spec.ClusterIP,
		Preview:   true,
		Ports:     ports,
		Locals:    locals,
	}
	hostKeys := m.installHostRoutes(service, ports, portKeys, nil, ModeExchange, true, previewID)
	m.mu.Lock()
	m.byID[previewID] = &runtimeIntercept{
		info: info, preview: &snapshot, portKeys: portKeys, hostKeys: hostKeys,
	}
	m.byKey[key] = previewID
	m.mu.Unlock()
	return info, nil
}

func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.Lock()
	runtime := m.byID[id]
	if runtime == nil {
		m.mu.Unlock()
		return fmt.Errorf("intercept %q not found", id)
	}
	delete(m.byID, id)
	delete(m.byKey, runtime.info.Namespace+"/"+runtime.info.Service)
	for _, key := range runtime.hostKeys {
		delete(m.hostRoutes, key)
	}
	control := m.control
	contextName := m.contextName
	portKeys := runtime.portKeys
	snapshot := runtime.snapshot
	preview := runtime.preview
	m.mu.Unlock()

	if control != nil {
		for subID := range portKeys {
			_ = control.unregister(subID)
		}
	}
	if preview != nil {
		return m.cluster.DeletePreviewService(ctx, contextName, *preview)
	}
	return m.cluster.RestoreServiceIntercept(ctx, contextName, snapshot)
}

// HostTCP returns a serve callback when host:port is an active intercept /
// preview target. Used by the local SOCKS bridge for TUN traffic.
func (m *Manager) HostTCP(host string, port uint16) (func(net.Conn), bool) {
	m.mu.Lock()
	route := m.hostRoutes[hostRouteKey{host: strings.ToLower(strings.TrimSpace(host)), port: port}]
	if route == nil {
		m.mu.Unlock()
		return nil, false
	}
	copyRoute := *route
	ctx := m.ctx
	gatewayAddress := m.gatewayAddress
	dialers := m.traffic
	m.mu.Unlock()

	return func(client net.Conn) {
		m.serveHostTCP(ctx, gatewayAddress, client, copyRoute, dialers)
	}, true
}

// HostUDP returns a dialer when host:port is an active UDP intercept / preview
// target. Used by the local SOCKS bridge so host TUN UDP does not hairpin
// through the Gateway ClusterIP.
func (m *Manager) HostUDP(host string, port uint16) (func(context.Context) (net.Conn, error), bool) {
	m.mu.Lock()
	route := m.hostRoutes[hostRouteKey{host: strings.ToLower(strings.TrimSpace(host)), port: port}]
	if route == nil {
		m.mu.Unlock()
		return nil, false
	}
	copyRoute := *route
	gatewayAddress := m.gatewayAddress
	dialers := m.traffic
	m.mu.Unlock()

	return func(ctx context.Context) (net.Conn, error) {
		return m.dialHostUDP(ctx, gatewayAddress, copyRoute, dialers)
	}, true
}

func (m *Manager) serveHostTCP(
	ctx context.Context,
	gatewayAddress string,
	client net.Conn,
	route hostRoute,
	dialers TrafficDialers,
) {
	defer client.Close()
	host := route.local.LocalHost
	if host == "" {
		host = "127.0.0.1"
	}
	localTarget := net.JoinHostPort(host, fmt.Sprintf("%d", route.local.LocalPort))

	if route.mode == ModeMirror {
		m.serveMirrorTCP(
			ctx, gatewayAddress, client, route.primaryAddr, host, route.local.LocalPort,
			dialers.MirrorShadow,
		)
		return
	}

	localDialer := dialers.Exchange
	if route.preview {
		localDialer = dialers.Preview
	}
	localConn, err := dialTraffic(ctx, localDialer, "tcp", localTarget)
	if err != nil {
		return
	}
	defer localConn.Close()
	relayTCP(client, localConn)
}

func (m *Manager) dialHostUDP(
	ctx context.Context,
	gatewayAddress string,
	route hostRoute,
	dialers TrafficDialers,
) (net.Conn, error) {
	host := route.local.LocalHost
	if host == "" {
		host = "127.0.0.1"
	}
	localTarget := net.JoinHostPort(host, fmt.Sprintf("%d", route.local.LocalPort))

	if route.mode == ModeMirror {
		return m.dialHostMirrorUDP(
			ctx, gatewayAddress, route.primaryAddr, host, route.local.LocalPort, dialers,
		)
	}

	// Dial the local process directly. Re-entering sing-box via exchange/preview
	// SOCKS UDP ASSOCIATE from the kubernetes outbound path times out on Linux with
	// auto_redirect, while the same HostTCP CONNECT re-entry works.
	var dialer net.Dialer
	return dialer.DialContext(ctx, "udp", localTarget)
}

func (m *Manager) dialHostMirrorUDP(
	ctx context.Context,
	gatewayAddress, primaryAddr, localHost string,
	localPort int,
	dialers TrafficDialers,
) (net.Conn, error) {
	if primaryAddr == "" {
		return nil, fmt.Errorf("mirror primary address is required")
	}
	primary, primaryFramed, err := dialMirrorPrimary(
		ctx, gatewayAddress, primaryAddr, tunnel.NetworkUDP,
	)
	if err != nil {
		return nil, err
	}
	localAddr := net.JoinHostPort(localHost, fmt.Sprintf("%d", localPort))
	var dialer net.Dialer
	localConn, err := dialer.DialContext(ctx, "udp", localAddr)
	if err != nil {
		localConn = nil
	}
	return newHostMirrorUDPConn(primary, primaryFramed, localConn), nil
}

func (m *Manager) allocateListenPort() int32 {
	return int32(atomic.AddUint32(&m.nextPort, 1))
}

func (m *Manager) rollbackRegisters(control *controlClient, portKeys map[string]PortMapping) {
	for subID := range portKeys {
		_ = control.unregister(subID)
	}
}

func (m *Manager) handleReady(interceptSubID string, network byte, streamID uint64) {
	m.mu.Lock()
	gatewayAddress := m.gatewayAddress
	var local PortMapping
	var primaryAddr string
	mode := ModeExchange
	preview := false
	found := false
	for _, runtime := range m.byID {
		if mapping, ok := runtime.portKeys[interceptSubID]; ok {
			local = mapping
			mode = runtime.info.Mode
			if mode == "" {
				mode = ModeExchange
			}
			if runtime.primaryAddrs != nil {
				primaryAddr = runtime.primaryAddrs[interceptSubID]
			}
			preview = runtime.info.Preview
			found = true
			break
		}
	}
	ctx := m.ctx
	dialers := m.traffic
	m.mu.Unlock()
	if !found || gatewayAddress == "" {
		return
	}
	localDialer := dialers.Exchange
	if preview {
		localDialer = dialers.Preview
	}
	go m.serveInbound(
		ctx, gatewayAddress, streamID, network, local, mode, primaryAddr,
		localDialer, dialers.MirrorShadow,
	)
}

func (m *Manager) serveInbound(
	ctx context.Context,
	gatewayAddress string,
	streamID uint64,
	network byte,
	local PortMapping,
	mode string,
	primaryAddr string,
	localDialer TrafficDialer,
	mirrorShadowDialer TrafficDialer,
) {
	tunnelConn, err := acceptStream(ctx, gatewayAddress, streamID)
	if err != nil {
		return
	}
	host := local.LocalHost
	if host == "" {
		host = "127.0.0.1"
	}
	if mode == ModeMirror {
		m.serveMirror(
			ctx, gatewayAddress, tunnelConn, network, primaryAddr, host, local.LocalPort,
			mirrorShadowDialer,
		)
		return
	}
	target := net.JoinHostPort(host, fmt.Sprintf("%d", local.LocalPort))
	switch network {
	case tunnel.NetworkUDP:
		localConn, err := dialTraffic(ctx, localDialer, "udp", target)
		if err != nil {
			_ = tunnelConn.Close()
			return
		}
		relayUDPConn(tunnelConn, localConn)
	default:
		localConn, err := dialTraffic(ctx, localDialer, "tcp", target)
		if err != nil {
			_ = tunnelConn.Close()
			return
		}
		defer localConn.Close()
		relayTCP(tunnelConn, localConn)
	}
}

func (m *Manager) serveMirror(
	ctx context.Context,
	gatewayAddress string,
	client net.Conn,
	network byte,
	primaryAddr, localHost string,
	localPort int,
	shadowDialer TrafficDialer,
) {
	if primaryAddr == "" {
		_ = client.Close()
		return
	}
	if network == tunnel.NetworkUDP {
		m.serveMirrorUDP(
			ctx, gatewayAddress, client, primaryAddr, localHost, localPort, shadowDialer,
		)
		return
	}
	m.serveMirrorTCP(
		ctx, gatewayAddress, client, primaryAddr, localHost, localPort, shadowDialer,
	)
}

func (m *Manager) serveMirrorTCP(
	ctx context.Context,
	gatewayAddress string,
	client net.Conn,
	primaryAddr, localHost string,
	localPort int,
	shadowDialer TrafficDialer,
) {
	primary, _, err := dialMirrorPrimary(
		ctx, gatewayAddress, primaryAddr, tunnel.NetworkTCP,
	)
	if err != nil {
		_ = client.Close()
		return
	}
	localConn, err := dialTraffic(
		ctx, shadowDialer, "tcp", net.JoinHostPort(localHost, fmt.Sprintf("%d", localPort)),
	)
	if err != nil {
		localConn = nil
	}
	mirrorTCP(client, primary, localConn)
}

func (m *Manager) serveMirrorUDP(
	ctx context.Context,
	gatewayAddress string,
	client net.Conn,
	primaryAddr, localHost string,
	localPort int,
	shadowDialer TrafficDialer,
) {
	primary, primaryFramed, err := dialMirrorPrimary(
		ctx, gatewayAddress, primaryAddr, tunnel.NetworkUDP,
	)
	if err != nil {
		_ = client.Close()
		return
	}
	localAddr := net.JoinHostPort(localHost, fmt.Sprintf("%d", localPort))
	localConn, err := dialTraffic(ctx, shadowDialer, "udp", localAddr)
	if err != nil {
		localConn = nil
	}
	mirrorUDP(client, primary, primaryFramed, localConn)
}

// dialMirrorPrimary reaches the original Pod backend via Gateway outbound dial
// so Pod IPs work without a host route/TUN. Loopback addresses used in unit
// tests fall back to a direct dial.
// framed is true when the returned conn uses tunnel datagram framing (Gateway UDP).
func dialMirrorPrimary(
	ctx context.Context,
	gatewayAddress, primaryAddr string,
	network byte,
) (conn net.Conn, framed bool, err error) {
	host, portStr, err := net.SplitHostPort(primaryAddr)
	if err != nil {
		return nil, false, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return nil, false, fmt.Errorf("invalid primary port %q", portStr)
	}
	command := tunnel.CommandTCP
	if network == tunnel.NetworkUDP {
		command = tunnel.CommandUDP
	}
	if gatewayAddress != "" && !isLoopbackHost(host) {
		conn, err := dialGatewayOpen(ctx, gatewayAddress, command, host, uint16(port))
		if err == nil {
			return conn, network == tunnel.NetworkUDP, nil
		}
	}
	if network == tunnel.NetworkUDP {
		udpAddr, err := net.ResolveUDPAddr("udp", primaryAddr)
		if err != nil {
			return nil, false, err
		}
		conn, err := net.DialUDP("udp", nil, udpAddr)
		return conn, false, err
	}
	var dialer net.Dialer
	conn, err = dialer.DialContext(ctx, "tcp", primaryAddr)
	return conn, false, err
}

func dialTraffic(
	ctx context.Context, dialer TrafficDialer, network, address string,
) (net.Conn, error) {
	if dialer != nil {
		return dialer.DialContext(ctx, network, address)
	}
	var direct net.Dialer
	return direct.DialContext(ctx, network, address)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func dialGatewayOpen(
	ctx context.Context, gatewayAddress string, command byte, host string, port uint16,
) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", gatewayAddress)
	if err != nil {
		return nil, fmt.Errorf("connect gateway: %w", err)
	}
	if err := tunnel.WriteOpen(conn, tunnel.OpenRequest{
		Command: command, Host: host, Port: port,
	}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := tunnel.ReadStatus(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func normalizePreviewPorts(ports []PortMapping) ([]PortMapping, error) {
	if len(ports) == 0 {
		return nil, fmt.Errorf("at least one port mapping is required")
	}
	locals := make([]PortMapping, len(ports))
	seen := make(map[string]struct{}, len(ports))
	for i, port := range ports {
		if port.ServicePort <= 0 || port.ServicePort > 65535 {
			return nil, fmt.Errorf("invalid service port %d", port.ServicePort)
		}
		if port.LocalPort <= 0 || port.LocalPort > 65535 {
			return nil, fmt.Errorf("invalid local port %d", port.LocalPort)
		}
		if port.LocalHost == "" {
			port.LocalHost = "127.0.0.1"
		}
		if port.Protocol == "" {
			port.Protocol = "TCP"
		}
		key := fmt.Sprintf("%s:%d", normalizeProtocol(port.Protocol), port.ServicePort)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate service port %s", key)
		}
		seen[key] = struct{}{}
		locals[i] = port
	}
	return locals, nil
}

func buildPreviewPorts(
	locals []PortMapping, allocate func() int32,
) ([]cluster.InterceptPort, error) {
	ports := make([]cluster.InterceptPort, 0, len(locals))
	for _, local := range locals {
		protocol := corev1.ProtocolTCP
		if normalizeProtocol(local.Protocol) == "UDP" {
			protocol = corev1.ProtocolUDP
		}
		name := fmt.Sprintf("%s-%d", strings.ToLower(string(protocol)), local.ServicePort)
		ports = append(ports, cluster.InterceptPort{
			Name:        name,
			Protocol:    protocol,
			ServicePort: local.ServicePort,
			ListenPort:  allocate(),
		})
	}
	return ports, nil
}

func buildPortsForLocals(
	service *corev1.Service,
	locals []PortMapping,
	allocate func() int32,
) ([]cluster.InterceptPort, error) {
	ports := make([]cluster.InterceptPort, 0, len(locals))
	for _, local := range locals {
		found := false
		for _, servicePort := range service.Spec.Ports {
			protocol := servicePort.Protocol
			if protocol == "" {
				protocol = corev1.ProtocolTCP
			}
			if servicePort.Port != local.ServicePort || !equalProtocol(local.Protocol, string(protocol)) {
				continue
			}
			name := servicePort.Name
			if name == "" {
				name = networkName(protocolToNetwork(protocol)) + fmt.Sprintf("-%d", servicePort.Port)
			}
			ports = append(ports, cluster.InterceptPort{
				Name:        name,
				Protocol:    protocol,
				ServicePort: servicePort.Port,
				ListenPort:  allocate(),
			})
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf(
				"service port %d/%s not found on %s/%s",
				local.ServicePort, local.Protocol, service.Namespace, service.Name,
			)
		}
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("no ports to intercept")
	}
	return ports, nil
}

func protocolToNetwork(protocol corev1.Protocol) byte {
	if protocol == corev1.ProtocolUDP {
		return tunnel.NetworkUDP
	}
	return tunnel.NetworkTCP
}

func conflictError(key, wantMode string, existing *runtimeIntercept) error {
	if existing == nil {
		return fmt.Errorf("service %s is already in use", key)
	}
	if existing.info.Preview {
		return fmt.Errorf("service %s is already used by preview", key)
	}
	have := existing.info.Mode
	if have == "" {
		have = ModeExchange
	}
	if (wantMode == ModeExchange || wantMode == ModeMirror) &&
		(have == ModeExchange || have == ModeMirror) &&
		wantMode != have {
		return fmt.Errorf("exchange and mirror are mutually exclusive; stop %s on %s first", have, key)
	}
	return fmt.Errorf("service %s is already in %s", key, have)
}

func (m *Manager) installHostRoutes(
	service *corev1.Service,
	ports []cluster.InterceptPort,
	portKeys map[string]PortMapping,
	primaryAddrs map[string]string,
	mode string,
	preview bool,
	interceptID string,
) []hostRouteKey {
	if service == nil {
		return nil
	}
	hosts := serviceRewriteHosts(service)
	if len(hosts) == 0 {
		return nil
	}
	if mode == "" {
		mode = ModeExchange
	}
	keys := make([]hostRouteKey, 0, len(hosts)*len(ports))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hostRoutes == nil {
		m.hostRoutes = make(map[hostRouteKey]*hostRoute)
	}
	for _, port := range ports {
		network := protocolToNetwork(port.Protocol)
		subID := fmt.Sprintf("%s:%s:%d", interceptID, networkName(network), port.ServicePort)
		local := localFor(port, nil)
		if mapped, ok := portKeys[subID]; ok {
			local = mapped
		}
		primary := ""
		if primaryAddrs != nil {
			primary = primaryAddrs[subID]
		}
		for _, host := range hosts {
			key := hostRouteKey{host: host, port: uint16(port.ServicePort)}
			m.hostRoutes[key] = &hostRoute{
				mode: mode, preview: preview, local: local, primaryAddr: primary,
			}
			keys = append(keys, key)
		}
	}
	return keys
}

func serviceRewriteHosts(service *corev1.Service) []string {
	if service == nil {
		return nil
	}
	hosts := make([]string, 0, 4)
	seen := map[string]struct{}{}
	add := func(host string) {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" || host == corev1.ClusterIPNone {
			return
		}
		if _, ok := seen[host]; ok {
			return
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	add(service.Spec.ClusterIP)
	name := service.Name
	ns := service.Namespace
	if name != "" && ns != "" {
		add(name + "." + ns + ".svc.cluster.local")
		add(name + "." + ns + ".svc")
		add(name + "." + ns)
	}
	return hosts
}

func (m Mapping) resolveLocals(service *corev1.Service) ([]PortMapping, error) {
	if len(m.Ports) == 0 {
		locals := make([]PortMapping, 0, len(service.Spec.Ports))
		for _, port := range service.Spec.Ports {
			protocol := string(port.Protocol)
			if protocol == "" {
				protocol = "TCP"
			}
			locals = append(locals, PortMapping{
				ServicePort: port.Port,
				Protocol:    protocol,
				LocalHost:   "127.0.0.1",
				LocalPort:   int(port.Port),
			})
		}
		if len(locals) == 0 {
			return nil, fmt.Errorf("service has no ports")
		}
		return locals, nil
	}
	for i := range m.Ports {
		if m.Ports[i].LocalHost == "" {
			m.Ports[i].LocalHost = "127.0.0.1"
		}
		if m.Ports[i].LocalPort == 0 {
			m.Ports[i].LocalPort = int(m.Ports[i].ServicePort)
		}
		if m.Ports[i].Protocol == "" {
			m.Ports[i].Protocol = "TCP"
		}
	}
	return m.Ports, nil
}

func localFor(port cluster.InterceptPort, locals []PortMapping) PortMapping {
	for _, local := range locals {
		if local.ServicePort == port.ServicePort && equalProtocol(local.Protocol, string(port.Protocol)) {
			return local
		}
	}
	return PortMapping{
		ServicePort: port.ServicePort,
		Protocol:    string(port.Protocol),
		LocalHost:   "127.0.0.1",
		LocalPort:   int(port.ServicePort),
	}
}

func equalProtocol(a, b string) bool {
	if a == "" {
		a = "TCP"
	}
	if b == "" {
		b = "TCP"
	}
	return normalizeProtocol(a) == normalizeProtocol(b)
}

func normalizeProtocol(value string) string {
	switch value {
	case "udp", "UDP", "Udp":
		return "UDP"
	default:
		return "TCP"
	}
}

func networkName(network byte) string {
	if network == tunnel.NetworkUDP {
		return "udp"
	}
	return "tcp"
}
