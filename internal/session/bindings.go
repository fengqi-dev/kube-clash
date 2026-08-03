package session

import (
	"context"
	"fmt"

	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/store"
	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

const (
	TestLayerGatewayControl = "gateway-control"
	TestLayerLocalListener  = "local-listener"
	TestLayerLocalTarget    = "local-target"
)

type ConnectivityTestResult struct {
	Passed      bool   `json:"passed"`
	FailedLayer string `json:"failedLayer,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (m *Manager) StartIntercept(ctx context.Context, mapping intercept.Mapping) (intercept.Info, error) {
	m.AppendLog("INFO", fmt.Sprintf("starting exchange %s/%s", mapping.Namespace, mapping.Service))
	info, err := m.intercept.StartIntercept(ctx, mapping)
	if err != nil {
		m.AppendLog("ERROR", fmt.Sprintf(
			"start exchange %s/%s: %v", mapping.Namespace, mapping.Service, err,
		))
	} else if !m.isRestoring() {
		m.persistExchanges(m.State().Context)
		m.AppendLog("INFO", fmt.Sprintf("started exchange %s/%s", mapping.Namespace, mapping.Service))
	}
	return info, err
}

func (m *Manager) StartMirror(ctx context.Context, mapping intercept.Mapping) (intercept.Info, error) {
	m.AppendLog("INFO", fmt.Sprintf("starting mirror %s/%s", mapping.Namespace, mapping.Service))
	info, err := m.intercept.StartMirror(ctx, mapping)
	if err != nil {
		m.AppendLog("ERROR", fmt.Sprintf(
			"start mirror %s/%s: %v", mapping.Namespace, mapping.Service, err,
		))
	} else if !m.isRestoring() {
		m.persistMirrors(m.State().Context)
		m.AppendLog("INFO", fmt.Sprintf("started mirror %s/%s", mapping.Namespace, mapping.Service))
	}
	return info, err
}

func (m *Manager) StopIntercept(ctx context.Context, id string) error {
	m.AppendLog("INFO", fmt.Sprintf("stopping intercept %s", id))
	err := m.intercept.Stop(ctx, id)
	if err != nil {
		m.AppendLog("ERROR", fmt.Sprintf("stop intercept %s: %v", id, err))
	} else if !m.isRestoring() {
		m.persistExchanges(m.State().Context)
		m.persistMirrors(m.State().Context)
		m.AppendLog("INFO", fmt.Sprintf("stopped intercept %s", id))
	}
	return err
}

func (m *Manager) TestIntercept(ctx context.Context, id string) ConnectivityTestResult {
	m.AppendLog("INFO", fmt.Sprintf("testing intercept %s", id))
	if err := m.intercept.TestControl(id); err != nil {
		m.AppendLog("ERROR", fmt.Sprintf("test intercept %s control: %v", id, err))
		return ConnectivityTestResult{
			FailedLayer: TestLayerGatewayControl,
			Error:       err.Error(),
		}
	}
	if err := m.intercept.Test(ctx, id); err != nil {
		m.AppendLog("ERROR", fmt.Sprintf("test intercept %s local target: %v", id, err))
		return ConnectivityTestResult{
			FailedLayer: TestLayerLocalTarget,
			Error:       err.Error(),
		}
	}
	m.AppendLog("INFO", fmt.Sprintf("session connectivity test passed: %s", id))
	return ConnectivityTestResult{Passed: true}
}

func (m *Manager) ListIntercepts() []intercept.Info {
	return m.intercept.List()
}

func (m *Manager) ListMirrors() []intercept.Info {
	return m.intercept.ListMirrors()
}

func (m *Manager) StartInspector(
	ctx context.Context, config tunnel.InspectorConfig,
) error {
	m.AppendLog("INFO", fmt.Sprintf(
		"starting traffic Inspector: targets=%d maxBodySize=%d",
		len(config.Targets), config.MaxBodySize,
	))
	if err := m.intercept.StartInspector(ctx, config); err != nil {
		m.AppendLog("ERROR", fmt.Sprintf("start traffic Inspector: %v", err))
		return err
	}
	m.AppendLog("INFO", fmt.Sprintf(
		"traffic Inspector started: targets=%d", len(config.Targets),
	))
	return nil
}

func (m *Manager) UpdateInspectorTargets(targets []tunnel.InspectorTarget) error {
	m.AppendLog("INFO", fmt.Sprintf(
		"updating traffic Inspector targets: targets=%d", len(targets),
	))
	if err := m.intercept.UpdateInspectorTargets(targets); err != nil {
		m.AppendLog("ERROR", fmt.Sprintf("update traffic Inspector targets: %v", err))
		return err
	}
	m.AppendLog("INFO", fmt.Sprintf(
		"traffic Inspector targets updated: targets=%d", len(targets),
	))
	return nil
}

func (m *Manager) StopInspector() error {
	m.AppendLog("INFO", "stopping traffic Inspector")
	if err := m.intercept.StopInspector(); err != nil {
		m.AppendLog("ERROR", fmt.Sprintf("stop traffic Inspector: %v", err))
		return err
	}
	m.AppendLog("INFO", "traffic Inspector stopped")
	return nil
}

func (m *Manager) InspectorEvents() <-chan tunnel.InspectorEvent {
	return m.intercept.InspectorEvents()
}

func (m *Manager) InspectorState() tunnel.InspectorState {
	return m.intercept.InspectorState()
}

func (m *Manager) StartPreview(ctx context.Context, request intercept.PreviewRequest) (intercept.Info, error) {
	m.AppendLog("INFO", fmt.Sprintf("starting preview %s/%s", request.Namespace, request.Name))
	info, err := m.intercept.StartPreview(ctx, request)
	if err != nil {
		m.AppendLog("ERROR", fmt.Sprintf(
			"start preview %s/%s: %v", request.Namespace, request.Name, err,
		))
	} else if !m.isRestoring() {
		m.persistPreviews(m.State().Context)
		m.AppendLog("INFO", fmt.Sprintf("started preview %s/%s", request.Namespace, request.Name))
	}
	return info, err
}

func (m *Manager) StopPreview(ctx context.Context, id string) error {
	m.AppendLog("INFO", fmt.Sprintf("stopping preview %s", id))
	err := m.intercept.Stop(ctx, id)
	if err != nil {
		m.AppendLog("ERROR", fmt.Sprintf("stop preview %s: %v", id, err))
	} else if !m.isRestoring() {
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
	m.AppendLog("INFO", fmt.Sprintf(
		"starting port-forward %s/%s/%s:%d/%s on local port %d",
		request.Kind, request.Namespace, request.Name, request.RemotePort,
		request.Protocol, request.LocalPort,
	))
	info, err := m.portfwd.Start(ctx, request)
	if err != nil {
		m.AppendLog("ERROR", fmt.Sprintf(
			"start port-forward %s/%s/%s:%d/%s: %v",
			request.Kind, request.Namespace, request.Name, request.RemotePort,
			request.Protocol, err,
		))
	} else {
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
	m.AppendLog("INFO", fmt.Sprintf("stopping port-forward %s", id))
	err := m.portfwd.Stop(id)
	if err != nil {
		m.AppendLog("ERROR", fmt.Sprintf("stop port-forward %s: %v", id, err))
	} else {
		m.persistPortForwards()
		m.AppendLog("INFO", fmt.Sprintf("stopped port-forward %s", id))
	}
	return err
}

func (m *Manager) TestPortForward(ctx context.Context, id string) ConnectivityTestResult {
	m.AppendLog("INFO", fmt.Sprintf("testing port-forward %s", id))
	if err := m.portfwd.Test(ctx, id); err != nil {
		m.AppendLog("ERROR", fmt.Sprintf("test port-forward %s: %v", id, err))
		return ConnectivityTestResult{
			FailedLayer: TestLayerLocalListener,
			Error:       err.Error(),
		}
	}
	m.AppendLog("INFO", fmt.Sprintf("port-forward connectivity test passed: %s", id))
	return ConnectivityTestResult{Passed: true}
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
	m.AppendLog("INFO", fmt.Sprintf(
		"resetting sessions: portForwards=%d exchanges=%d mirrors=%d",
		len(m.portfwd.List()), len(m.intercept.List()), len(m.intercept.ListMirrors()),
	))
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
		m.AppendLog("ERROR", fmt.Sprintf("clear persisted session intents: %v", err))
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
