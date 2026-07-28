package session

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kube-clash/kube-clash/internal/cluster"
)

type Phase string

const (
	PhaseIdle        Phase = "idle"
	PhaseChecking    Phase = "checking"
	PhaseInstalling  Phase = "installing-gateway"
	PhaseDiscovering Phase = "discovering-network"
	PhaseReady       Phase = "ready-for-tunnel"
	PhaseError       Phase = "error"
)

type Request struct {
	Context   string
	Namespace string
}

type State struct {
	Phase     Phase              `json:"phase"`
	Context   string             `json:"context"`
	Namespace string             `json:"namespace"`
	Message   string             `json:"message"`
	Error     string             `json:"error,omitempty"`
	Discovery *cluster.Discovery `json:"discovery,omitempty"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

type ClusterProvider interface {
	Contexts() ([]cluster.ContextInfo, error)
	Namespaces(context.Context, string) ([]string, error)
	Discover(context.Context, string) (cluster.Discovery, error)
}

type Manager struct {
	provider  ClusterProvider
	mu        sync.RWMutex
	state     State
	cancel    context.CancelFunc
	listeners []func(State)
}

func NewManager(provider ClusterProvider) *Manager {
	return &Manager{
		provider: provider,
		state:    State{Phase: PhaseIdle, Message: "未连接", UpdatedAt: time.Now()},
	}
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
		return errors.New("a connection attempt is already running")
	}
	ctx, cancel := context.WithCancel(parent)
	m.cancel = cancel
	m.mu.Unlock()

	go m.run(ctx, request)
	return nil
}

func (m *Manager) run(ctx context.Context, request Request) {
	defer func() {
		m.mu.Lock()
		m.cancel = nil
		m.mu.Unlock()
	}()
	m.publish(State{
		Phase: PhaseChecking, Context: request.Context, Namespace: request.Namespace,
		Message: "正在检查 Kubernetes 访问权限",
	})
	discovery, err := m.provider.Discover(ctx, request.Context)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		m.publish(State{
			Phase: PhaseError, Context: request.Context, Namespace: request.Namespace,
			Message: "无法读取集群网络信息", Error: err.Error(),
		})
		return
	}
	if ctx.Err() != nil {
		return
	}
	m.publish(State{
		Phase: PhaseDiscovering, Context: request.Context, Namespace: request.Namespace,
		Message: "已发现 Pod、Service 和集群 DNS", Discovery: &discovery,
	})
	m.publish(State{
		Phase: PhaseReady, Context: request.Context, Namespace: request.Namespace,
		Message: "网络发现完成，等待启动 Mihomo TUN", Discovery: &discovery,
	})
}

func (m *Manager) Disconnect() error {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.mu.Unlock()
	m.publish(State{Phase: PhaseIdle, Message: "未连接"})
	return nil
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
