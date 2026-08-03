package intercept

import (
	"context"
	"fmt"
	"net"
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
	sessionToken   tunnel.SessionToken
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
	return m.start(ctx, contextName, gatewayIP, gatewayAddress, tunnel.SessionToken{})
}

func (m *Manager) StartSession(
	ctx context.Context,
	contextName, gatewayIP, gatewayAddress string,
	token tunnel.SessionToken,
) error {
	if token.IsZero() {
		return fmt.Errorf("KCG2 session token is required")
	}
	return m.start(ctx, contextName, gatewayIP, gatewayAddress, token)
}

func (m *Manager) start(
	ctx context.Context,
	contextName, gatewayIP, gatewayAddress string,
	token tunnel.SessionToken,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active {
		return fmt.Errorf("intercept manager already started")
	}
	m.control.token = token
	if err := m.control.connect(ctx, gatewayAddress); err != nil {
		return err
	}
	m.active = true
	m.stopping = false
	m.ctx = ctx
	m.contextName = contextName
	m.gatewayIP = gatewayIP
	m.gatewayAddress = gatewayAddress
	m.sessionToken = m.control.current().token
	return nil
}

func (m *Manager) GatewayCapabilities() tunnel.Capabilities {
	m.mu.Lock()
	defer m.mu.Unlock()
	if client := m.control.current(); client != nil {
		return client.capabilities
	}
	return tunnel.Capabilities{}
}

func (m *Manager) GatewaySessionToken() tunnel.SessionToken {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionToken
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
	gatewayIP := m.gatewayIP
	gatewayAddress := m.gatewayAddress
	m.mu.Unlock()
	return m.recoverControlAt(ctx, gatewayIP, gatewayAddress)
}

// RecoverControlAt replaces a failed API-server port-forward, reconnects the
// Gateway control channel, and makes new feature streams use the new address.
func (m *Manager) RecoverControlAt(
	ctx context.Context, gatewayIP, gatewayAddress string,
) error {
	return m.recoverControlAt(ctx, gatewayIP, gatewayAddress)
}

func (m *Manager) recoverControlAt(
	ctx context.Context, gatewayIP, gatewayAddress string,
) error {
	m.mu.Lock()
	if !m.active || m.stopping {
		m.mu.Unlock()
		return fmt.Errorf("session is not connected")
	}
	if m.control.recovering {
		m.mu.Unlock()
		return fmt.Errorf("control recovery already in progress")
	}
	if gatewayAddress == "" {
		m.mu.Unlock()
		return fmt.Errorf("gateway address is unavailable")
	}
	registrations := m.registry.registrations()
	old, generation := m.control.beginRecovery()
	m.mu.Unlock()

	if old != nil {
		_ = old.close()
	}

	control, lost, err := m.control.redial(ctx, gatewayAddress, registrations)
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
	m.gatewayIP = gatewayIP
	m.gatewayAddress = gatewayAddress
	m.sessionToken = control.token
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
