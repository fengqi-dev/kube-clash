package session

import (
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

// stateHub owns the published session state and its subscribers. Runtime
// lifecycle fields use Manager.mu, so high-frequency inventory and metrics
// updates do not contend with Connect or Disconnect bookkeeping.
type stateHub struct {
	mu sync.RWMutex

	state            State
	listeners        []func(State)
	metricsListeners []func(*singbox.Metrics)
}

func newStateHub(initial State) *stateHub {
	return &stateHub{state: initial}
}

func (h *stateHub) snapshot() State {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.state
}

func (h *stateHub) subscribe(listener func(State)) {
	h.mu.Lock()
	h.listeners = append(h.listeners, listener)
	h.mu.Unlock()
}

func (h *stateHub) subscribeMetrics(listener func(*singbox.Metrics)) {
	h.mu.Lock()
	h.metricsListeners = append(h.metricsListeners, listener)
	h.mu.Unlock()
}

func (h *stateHub) publish(state State) {
	state.UpdatedAt = time.Now()
	h.mu.Lock()
	if state.Events == nil {
		state.Events = h.state.Events
	}
	// Keep the latest high-frequency metrics when a slower state publish
	// (inventory, phase change) does not carry a fresher snapshot.
	if state.Metrics == nil && h.state.Metrics != nil && state.Phase == PhaseConnected {
		state.Metrics = h.state.Metrics
	}
	h.state = state
	listeners := append([]func(State){}, h.listeners...)
	h.mu.Unlock()
	for _, listener := range listeners {
		listener(state)
	}
}

func (h *stateHub) publishMetrics(metrics *singbox.Metrics) {
	if metrics == nil {
		return
	}
	h.mu.RLock()
	listeners := append([]func(*singbox.Metrics){}, h.metricsListeners...)
	h.mu.RUnlock()
	for _, listener := range listeners {
		listener(metrics)
	}
}
