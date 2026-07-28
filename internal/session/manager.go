package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-clash/internal/cluster"
	"github.com/fengqi-dev/kube-clash/internal/mihomo"
	"github.com/fengqi-dev/kube-clash/internal/socksbridge"
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

const DefaultGatewayImage = "ghcr.io/fengqi-dev/kube-clash/gateway:latest"

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
	Metrics     *mihomo.Metrics    `json:"metrics,omitempty"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

type ClusterProvider interface {
	Contexts() ([]cluster.ContextInfo, error)
	Namespaces(context.Context, string) ([]string, error)
	Discover(context.Context, string) (cluster.Discovery, error)
	EnsureGateway(context.Context, string, string) (string, error)
	StartPortForward(context.Context, string, string, uint16) (cluster.PortForward, error)
}

type Core interface {
	Start(
		context.Context,
		cluster.Discovery,
		string,
		string,
	) (mihomo.RunningCore, error)
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
}

func NewManager(provider ClusterProvider, options ...Option) *Manager {
	image := os.Getenv("KUBE_CLASH_GATEWAY_IMAGE")
	if image == "" {
		image = DefaultGatewayImage
	}
	manager := &Manager{
		provider:      provider,
		core:          &mihomo.Runtime{},
		bridgeFactory: socksbridge.Listen,
		gatewayImage:  image,
		state: State{
			Phase: PhaseIdle, Message: "未连接", CoreVersion: mihomo.Version, UpdatedAt: time.Now(),
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
		Message: "正在检查 Kubernetes 访问权限", CoreVersion: mihomo.Version,
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
	state.Message = "正在安装并启动 Mihomo TUN"
	m.publish(state)
	core, err := m.core.Start(ctx, discovery, bridge.Addr().String(), request.Namespace)
	if err != nil {
		m.fail(ctx, state, "无法启动 Mihomo TUN", err)
		return
	}
	resources = append(resources, core)

	connectedAt := time.Now()
	state.Phase = PhaseConnected
	state.Message = "已连接，可访问 Pod、Service 和集群 DNS"
	state.ConnectedAt = &connectedAt
	state.Metrics = &mihomo.Metrics{}
	m.publish(state)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.publish(State{
				Phase: PhaseIdle, Message: "未连接", CoreVersion: mihomo.Version,
			})
			return
		case <-core.Done():
			if ctx.Err() == nil {
				err := core.Err()
				if err == nil {
					err = errors.New("mihomo stopped unexpectedly")
				}
				m.fail(ctx, state, "Mihomo TUN 意外退出", err)
			}
			return
		case <-ticker.C:
			metrics, err := core.Snapshot(ctx)
			if err == nil {
				state.Metrics = &metrics
				m.publish(state)
			}
		}
	}
}

func (m *Manager) Disconnect() error {
	m.mu.RLock()
	cancel, done := m.cancel, m.done
	m.mu.RUnlock()
	if cancel == nil {
		m.publish(State{Phase: PhaseIdle, Message: "未连接", CoreVersion: mihomo.Version})
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
