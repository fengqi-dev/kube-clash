package session

import (
	"context"
	"errors"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

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
	m.mu.RUnlock()
	phase := m.State().Phase
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
	m.mu.RUnlock()
	phase := m.State().Phase
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
	m.mu.RUnlock()
	phase := m.State().Phase
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
	return m.stateHub.snapshot()
}

// SetKubernetesVersion records the API server version for the sidebar/overview.
func (m *Manager) SetKubernetesVersion(version string) {
	if version == "" {
		return
	}
	m.stateHub.mu.Lock()
	if m.stateHub.state.KubernetesVersion == version {
		m.stateHub.mu.Unlock()
		return
	}
	next := m.stateHub.state
	next.KubernetesVersion = version
	m.stateHub.mu.Unlock()
	m.publish(next)
}

func (m *Manager) Subscribe(listener func(State)) {
	m.stateHub.subscribe(listener)
}

// SubscribeMetrics receives high-frequency connection/traffic snapshots without
// re-emitting the full session inventory on every poll.
func (m *Manager) SubscribeMetrics(listener func(*singbox.Metrics)) {
	m.stateHub.subscribeMetrics(listener)
}
