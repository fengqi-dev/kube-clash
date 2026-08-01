package session

import (
	"context"
	"fmt"

	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/store"
)

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

func (m *Manager) TestIntercept(ctx context.Context, id string) error {
	if err := m.intercept.Test(ctx, id); err != nil {
		return err
	}
	m.AppendLog("INFO", fmt.Sprintf("session connectivity test passed: %s", id))
	return nil
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
			"started port-forward %s/%s/%s:%d/%s → :%d",
			request.Kind, request.Namespace, request.Name, request.RemotePort,
			info.Protocol, info.LocalPort,
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

func (m *Manager) TestPortForward(ctx context.Context, id string) error {
	if err := m.portfwd.Test(ctx, id); err != nil {
		return err
	}
	m.AppendLog("INFO", fmt.Sprintf("port-forward connectivity test passed: %s", id))
	return nil
}

func (m *Manager) ListPortForwards() []portfwd.Info {
	return m.portfwd.List()
}

func (m *Manager) StopAllPortForwards() {
	m.portfwd.StopAll()
}

// ResetSessions stops every port-forward, exchange, and mirror, then clears their
// persisted intents from state.json. This still clears disk state when the
// cluster is unavailable and live stop fails. Previews are left alone.
func (m *Manager) ResetSessions(ctx context.Context) error {
	for _, item := range m.portfwd.List() {
		if err := m.StopPortForward(item.ID); err != nil {
			m.AppendLog("WARN", fmt.Sprintf("reset stop port-forward %s: %v", item.ID, err))
		}
	}
	for _, item := range m.intercept.List() {
		if err := m.StopIntercept(ctx, item.ID); err != nil {
			m.AppendLog("WARN", fmt.Sprintf("reset stop exchange %s: %v", item.ID, err))
		}
	}
	for _, item := range m.intercept.ListMirrors() {
		if err := m.StopIntercept(ctx, item.ID); err != nil {
			m.AppendLog("WARN", fmt.Sprintf("reset stop mirror %s: %v", item.ID, err))
		}
	}
	if err := m.clearPersistedSessions(); err != nil {
		return err
	}
	m.AppendLog("INFO", "reset sessions: cleared port-forwards, exchanges, and mirrors")
	return nil
}

// SessionIntentCounts returns persisted restore intents from state.json.
func (m *Manager) SessionIntentCounts() store.SessionIntentCounts {
	if m.store == nil {
		return store.SessionIntentCounts{}
	}
	return m.store.SessionIntentCounts()
}
