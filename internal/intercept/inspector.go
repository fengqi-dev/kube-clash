package intercept

import (
	"context"
	"fmt"
	"net"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

func (m *Manager) StartInspector(
	ctx context.Context, config tunnel.InspectorConfig,
) error {
	m.inspectorOp.Lock()
	defer m.inspectorOp.Unlock()
	if err := config.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	if !m.active || m.stopping || !m.control.ready() {
		m.mu.Unlock()
		return fmt.Errorf("session is not connected")
	}
	if m.inspector != nil {
		m.mu.Unlock()
		return fmt.Errorf("Inspector is already active")
	}
	control := m.control.current()
	address := m.gatewayAddress
	token := control.token
	m.mu.Unlock()

	if err := control.startInspector(config); err != nil {
		return err
	}
	connection, err := openInspectorEvents(ctx, address, token)
	if err != nil {
		_ = control.stopInspector()
		return fmt.Errorf("open Inspector events: %w", err)
	}

	m.mu.Lock()
	if !m.active || m.stopping || m.control.current() != control || m.inspector != nil {
		m.mu.Unlock()
		_ = connection.Close()
		_ = control.stopInspector()
		return fmt.Errorf("session changed while starting Inspector")
	}
	copy := config
	copy.Targets = append([]tunnel.InspectorTarget(nil), config.Targets...)
	m.inspector = &copy
	m.inspectorConn = connection
	m.mu.Unlock()
	go m.readInspectorEvents(connection)
	return nil
}

func (m *Manager) UpdateInspectorTargets(
	targets []tunnel.InspectorTarget,
) error {
	m.inspectorOp.Lock()
	defer m.inspectorOp.Unlock()
	config := tunnel.InspectorConfig{Targets: targets}
	if err := config.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.inspector == nil || !m.control.ready() {
		m.mu.Unlock()
		return fmt.Errorf("Inspector is not active")
	}
	control := m.control.current()
	m.mu.Unlock()

	if err := control.updateInspectorTargets(targets); err != nil {
		return err
	}
	m.mu.Lock()
	if m.inspector != nil && m.control.current() == control {
		m.inspector.Targets = append([]tunnel.InspectorTarget(nil), targets...)
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) StopInspector() error {
	m.inspectorOp.Lock()
	defer m.inspectorOp.Unlock()
	m.mu.Lock()
	if m.inspector == nil {
		m.mu.Unlock()
		return nil
	}
	connection := m.inspectorConn
	m.inspectorConn = nil
	m.inspector = nil
	control := m.control.current()
	m.mu.Unlock()

	if connection != nil {
		_ = connection.Close()
	}
	if control == nil || control.closed.Load() {
		return nil
	}
	return control.stopInspector()
}

func (m *Manager) InspectorEvents() <-chan tunnel.InspectorEvent {
	return m.inspectorEvents
}

func (m *Manager) InspectorState() tunnel.InspectorState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inspector == nil {
		return tunnel.InspectorState{}
	}
	return tunnel.InspectorState{
		Active:      true,
		MaxBodySize: m.inspector.MaxBodySize,
		Targets:     append([]tunnel.InspectorTarget(nil), m.inspector.Targets...),
	}
}

func openInspectorEvents(
	ctx context.Context, address string, token tunnel.SessionToken,
) (net.Conn, error) {
	if token.IsZero() {
		return nil, fmt.Errorf("Inspector events require KCG2")
	}
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial Gateway Inspector events: %w", err)
	}
	if err := tunnel.WriteInspectorEventsSession(connection, token); err != nil {
		_ = connection.Close()
		return nil, err
	}
	if err := tunnel.ReadStatus(connection); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("Inspector events handshake: %w", err)
	}
	return connection, nil
}

func (m *Manager) readInspectorEvents(connection net.Conn) {
	for {
		event, err := tunnel.ReadInspectorEvent(connection)
		if err != nil {
			m.mu.Lock()
			if m.inspectorConn == connection {
				m.inspectorConn = nil
			}
			m.mu.Unlock()
			return
		}
		select {
		case m.inspectorEvents <- event:
		default:
			if event.Type == tunnel.InspectorEventBody {
				continue
			}
			select {
			case <-m.inspectorEvents:
			default:
			}
			select {
			case m.inspectorEvents <- event:
			default:
			}
		}
	}
}
