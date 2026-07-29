package intercept

import (
	"context"
	"fmt"
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
}

// PreviewRequest creates a new ClusterIP Service that exposes a local process.
type PreviewRequest struct {
	Namespace string        `json:"namespace"`
	Name      string        `json:"name"`
	Ports     []PortMapping `json:"ports"`
}

// Info is a running intercept or preview visible to the UI.
type Info struct {
	ID        string                  `json:"id"`
	Namespace string                  `json:"namespace"`
	Service   string                  `json:"service"`
	ClusterIP string                  `json:"clusterIP,omitempty"`
	Preview   bool                    `json:"preview,omitempty"`
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

type Manager struct {
	cluster ClusterAPI

	mu             sync.Mutex
	active         bool
	ctx            context.Context
	contextName    string
	gatewayIP      string
	gatewayAddress string
	control        *controlClient
	nextPort       uint32
	byID           map[string]*runtimeIntercept
	byKey          map[string]string // namespace/service -> id
}

type runtimeIntercept struct {
	info     Info
	snapshot cluster.ServiceInterceptSnapshot
	preview  *cluster.PreviewServiceSnapshot
	portKeys map[string]PortMapping // interceptID -> local mapping
}

func NewManager(api ClusterAPI) *Manager {
	return &Manager{
		cluster:  api,
		nextPort: 20000,
		byID:     make(map[string]*runtimeIntercept),
		byKey:    make(map[string]string),
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
	control.onReady = m.handleReady
	m.active = true
	m.ctx = ctx
	m.contextName = contextName
	m.gatewayIP = gatewayIP
	m.gatewayAddress = gatewayAddress
	m.control = control
	return nil
}

func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.byID))
	for id := range m.byID {
		ids = append(ids, id)
	}
	control := m.control
	m.control = nil
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
	return m.list(false)
}

func (m *Manager) ListPreviews() []Info {
	return m.list(true)
}

func (m *Manager) list(preview bool) []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Info, 0, len(m.byID))
	for _, item := range m.byID {
		if item.info.Preview != preview {
			continue
		}
		items = append(items, item.info)
	}
	return items
}

func (m *Manager) StartIntercept(ctx context.Context, mapping Mapping) (Info, error) {
	if mapping.Namespace == "" {
		mapping.Namespace = "default"
	}
	if mapping.Service == "" {
		return Info{}, fmt.Errorf("service is required")
	}

	m.mu.Lock()
	if !m.active || m.control == nil {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("session is not connected")
	}
	key := mapping.Namespace + "/" + mapping.Service
	if _, exists := m.byKey[key]; exists {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("service %s is already intercepted", key)
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

	info := Info{
		ID:        interceptID,
		Namespace: mapping.Namespace,
		Service:   mapping.Service,
		Ports:     ports,
		Locals:    locals,
	}
	m.mu.Lock()
	m.byID[interceptID] = &runtimeIntercept{info: info, snapshot: *snapshot, portKeys: portKeys}
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
	if !m.active || m.control == nil {
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
	m.mu.Lock()
	m.byID[previewID] = &runtimeIntercept{
		info: info, preview: &snapshot, portKeys: portKeys,
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
	found := false
	for _, runtime := range m.byID {
		if mapping, ok := runtime.portKeys[interceptSubID]; ok {
			local = mapping
			found = true
			break
		}
	}
	ctx := m.ctx
	m.mu.Unlock()
	if !found || gatewayAddress == "" {
		return
	}
	go m.serveInbound(ctx, gatewayAddress, streamID, network, local)
}

func (m *Manager) serveInbound(
	ctx context.Context, gatewayAddress string, streamID uint64, network byte, local PortMapping,
) {
	tunnelConn, err := acceptStream(ctx, gatewayAddress, streamID)
	if err != nil {
		return
	}
	host := local.LocalHost
	if host == "" {
		host = "127.0.0.1"
	}
	switch network {
	case tunnel.NetworkUDP:
		relayUDP(tunnelConn, host, local.LocalPort)
	default:
		var dialer net.Dialer
		localConn, err := dialer.DialContext(
			ctx, "tcp", net.JoinHostPort(host, fmt.Sprintf("%d", local.LocalPort)),
		)
		if err != nil {
			_ = tunnelConn.Close()
			return
		}
		defer localConn.Close()
		relayTCP(tunnelConn, localConn)
	}
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
