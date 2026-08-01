package intercept

import (
	"context"
	"fmt"
	"maps"
	"net"
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
	ctx            context.Context
	contextName    string
	gatewayIP      string
	gatewayAddress string
	control        *controlSession
	nextPort       uint32
	registry       *runtimeRegistry
	routes         *hostRouteRegistry
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

type runtimeIntercept struct {
	info         Info
	snapshot     cluster.ServiceInterceptSnapshot
	preview      *cluster.PreviewServiceSnapshot
	portKeys     map[string]PortMapping // subID -> local mapping
	primaryAddrs map[string]string      // subID -> pod host:port (mirror)
	hostKeys     []hostRouteKey
	stopping     bool
}

func NewManager(api ClusterAPI) *Manager {
	manager := &Manager{
		cluster:  api,
		nextPort: 20000,
		registry: newRuntimeRegistry(),
		routes:   newHostRouteRegistry(),
	}
	manager.control = newControlSession(manager.handleReady)
	return manager
}

func (m *Manager) Start(
	ctx context.Context, contextName, gatewayIP, gatewayAddress string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active {
		return fmt.Errorf("intercept manager already started")
	}
	if err := m.control.connect(ctx, gatewayAddress); err != nil {
		return err
	}
	m.active = true
	m.stopping = false
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
	return m.control.lostSignal()
}

// RecoverControl redials the Gateway control channel on the existing
// port-forward address and re-registers active Exchange/Mirror/Preview ports.
func (m *Manager) RecoverControl(ctx context.Context) error {
	m.mu.Lock()
	if !m.active || m.stopping {
		m.mu.Unlock()
		return fmt.Errorf("session is not connected")
	}
	if m.control.recovering {
		m.mu.Unlock()
		return fmt.Errorf("control recovery already in progress")
	}
	if m.gatewayAddress == "" {
		m.mu.Unlock()
		return fmt.Errorf("gateway address is unavailable")
	}
	address := m.gatewayAddress
	registrations := m.registry.registrations()
	old, generation := m.control.beginRecovery()
	m.mu.Unlock()

	if old != nil {
		_ = old.close()
	}

	control, lost, err := m.control.redial(ctx, address, registrations)
	if err != nil {
		m.mu.Lock()
		m.control.recoveryFailed(generation)
		m.mu.Unlock()
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active || m.stopping || !m.control.finishRecovery(generation, control, lost) {
		_ = control.close()
		return fmt.Errorf("session is not connected")
	}
	return nil
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
	ids := m.registry.ids()
	control := m.control.stop()
	m.active = false
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
	return m.registry.listPreviews()
}

func (m *Manager) listByMode(mode string) []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registry.listByMode(mode)
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
	control, controlGeneration, controlReady := m.control.snapshot()
	if !m.active || !controlReady {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("session is not connected")
	}
	key := mapping.Namespace + "/" + mapping.Service
	if existing := m.registry.getByKey(key); existing != nil {
		m.mu.Unlock()
		return Info{}, conflictError(key, mode, existing)
	}
	reservation, reserved := m.registry.reserve(key)
	if !reserved {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("service %s start is already in progress", key)
	}
	contextName := m.contextName
	gatewayIP := m.gatewayIP
	m.mu.Unlock()
	defer m.releaseStartReservation(key, reservation)

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
	transaction := newStartTransaction(control)
	defer transaction.rollback()
	if err := transaction.registerPorts(interceptID, ports, locals); err != nil {
		return Info{}, err
	}
	portKeys := transaction.portKeys

	selector := map[string]string{}
	maps.Copy(selector, service.Spec.Selector)
	snapshot := &cluster.ServiceInterceptSnapshot{
		Namespace: mapping.Namespace,
		Service:   mapping.Service,
		Selector:  selector,
		Ports:     ports,
		GatewayIP: gatewayIP,
	}
	if err := m.cluster.ApplyServiceIntercept(ctx, contextName, snapshot, interceptID); err != nil {
		return Info{}, err
	}
	transaction.compensate(func() {
		_ = m.cluster.RestoreServiceIntercept(ctx, contextName, *snapshot)
	})

	var primaryAddrs map[string]string
	if mode == ModeMirror {
		primaryAddrs, err = buildPrimaryAddrs(*snapshot, ports, portKeys, interceptID)
		if err != nil {
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
	m.mu.Lock()
	if !m.active ||
		!m.control.matches(control, controlGeneration) ||
		!m.registry.reserved(key, reservation) {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("session changed while starting service %s", key)
	}
	hostKeys := m.routes.install(service, ports, portKeys, primaryAddrs, mode, false, interceptID)
	m.registry.add(&runtimeIntercept{
		info: info, snapshot: *snapshot, portKeys: portKeys, primaryAddrs: primaryAddrs, hostKeys: hostKeys,
	})
	m.registry.release(key, reservation)
	m.mu.Unlock()
	transaction.commit()
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
	control, controlGeneration, controlReady := m.control.snapshot()
	if !m.active || !controlReady {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("session is not connected")
	}
	key := request.Namespace + "/" + request.Name
	if m.registry.containsKey(key) {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("service %s is already in use", key)
	}
	reservation, reserved := m.registry.reserve(key)
	if !reserved {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("service %s start is already in progress", key)
	}
	contextName := m.contextName
	gatewayIP := m.gatewayIP
	m.mu.Unlock()
	defer m.releaseStartReservation(key, reservation)

	ports, err := buildPreviewPorts(locals, m.allocateListenPort)
	if err != nil {
		return Info{}, err
	}

	previewID := fmt.Sprintf("%s/%s", request.Namespace, request.Name)
	transaction := newStartTransaction(control)
	defer transaction.rollback()
	if err := transaction.registerPorts(previewID, ports, locals); err != nil {
		return Info{}, err
	}
	portKeys := transaction.portKeys

	snapshot := cluster.PreviewServiceSnapshot{
		Namespace: request.Namespace,
		Service:   request.Name,
		Ports:     ports,
		GatewayIP: gatewayIP,
	}
	service, err := m.cluster.CreatePreviewService(ctx, contextName, snapshot, previewID)
	if err != nil {
		return Info{}, err
	}
	transaction.compensate(func() {
		_ = m.cluster.DeletePreviewService(ctx, contextName, snapshot)
	})

	info := Info{
		ID:        previewID,
		Namespace: request.Namespace,
		Service:   request.Name,
		ClusterIP: service.Spec.ClusterIP,
		Preview:   true,
		Ports:     ports,
		Locals:    locals,
	}
	m.mu.Lock()
	if !m.active ||
		!m.control.matches(control, controlGeneration) ||
		!m.registry.reserved(key, reservation) {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("session changed while starting service %s", key)
	}
	hostKeys := m.routes.install(service, ports, portKeys, nil, ModeExchange, true, previewID)
	m.registry.add(&runtimeIntercept{
		info: info, preview: &snapshot, portKeys: portKeys, hostKeys: hostKeys,
	})
	m.registry.release(key, reservation)
	m.mu.Unlock()
	transaction.commit()
	return info, nil
}

func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.Lock()
	runtime := m.registry.get(id)
	if runtime == nil {
		m.mu.Unlock()
		return fmt.Errorf("intercept %q not found", id)
	}
	if runtime.stopping {
		m.mu.Unlock()
		return fmt.Errorf("intercept %q stop already in progress", id)
	}
	runtime.stopping = true
	contextName := m.contextName
	snapshot := runtime.snapshot
	preview := runtime.preview
	m.mu.Unlock()

	var err error
	if preview != nil {
		err = m.cluster.DeletePreviewService(ctx, contextName, *preview)
	} else {
		err = m.cluster.RestoreServiceIntercept(ctx, contextName, snapshot)
	}
	if err != nil {
		m.mu.Lock()
		if m.registry.get(id) == runtime {
			runtime.stopping = false
		}
		m.mu.Unlock()
		return err
	}

	m.mu.Lock()
	if m.registry.get(id) != runtime {
		m.mu.Unlock()
		return nil
	}
	m.registry.remove(id)
	m.routes.remove(runtime.hostKeys)
	control := m.control.current()
	portKeys := runtime.portKeys
	m.mu.Unlock()

	if control != nil {
		unregisterPorts(control, portKeys)
	}
	return nil
}

func (m *Manager) allocateListenPort() int32 {
	return int32(atomic.AddUint32(&m.nextPort, 1))
}

func (m *Manager) releaseStartReservation(key string, reservation uint64) {
	m.mu.Lock()
	m.registry.release(key, reservation)
	m.mu.Unlock()
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
