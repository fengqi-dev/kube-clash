package intercept

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
	corev1 "k8s.io/api/core/v1"
)

func (m *Manager) StartInspector(
	ctx context.Context, config tunnel.InspectorConfig,
) error {
	m.inspectorOp.Lock()
	defer m.inspectorOp.Unlock()
	targets, err := m.prepareInspectorTargets(ctx, config.Targets)
	if err != nil {
		return err
	}
	config.Targets = targets
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
	m.mu.Lock()
	ctx := m.ctx
	m.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	var err error
	targets, err = m.prepareInspectorTargets(ctx, targets)
	if err != nil {
		return err
	}
	if err := tunnel.ValidateInspectorTargets(targets); err != nil {
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

func (m *Manager) prepareInspectorTargets(
	ctx context.Context, targets []tunnel.InspectorTarget,
) ([]tunnel.InspectorTarget, error) {
	result := make([]tunnel.InspectorTarget, len(targets))
	copy(result, targets)
	m.mu.Lock()
	contextName := m.contextName
	active := m.active && !m.stopping
	m.mu.Unlock()
	for index := range result {
		target := &result[index]
		if target.Namespace == "" && target.Service == "" &&
			target.ServiceUID == "" && len(target.Addresses) == 0 {
			continue
		}
		if !active || contextName == "" {
			return nil, fmt.Errorf("session is not connected")
		}
		namespace := strings.ToLower(strings.TrimSpace(target.Namespace))
		serviceName := strings.ToLower(strings.TrimSpace(target.Service))
		if namespace == "" || serviceName == "" {
			return nil, fmt.Errorf(
				"Inspector target %q Service namespace and name are required", target.ID,
			)
		}
		service, err := m.cluster.GetService(ctx, contextName, namespace, serviceName)
		if err != nil {
			return nil, fmt.Errorf("validate Inspector target %q: %w", target.ID, err)
		}
		if target.ServiceUID != "" && target.ServiceUID != string(service.UID) {
			return nil, fmt.Errorf(
				"Inspector target %q Service identity changed; select it again", target.ID,
			)
		}
		if !serviceHasTCPPort(service, target.Port) {
			return nil, fmt.Errorf(
				"Inspector target %q Service %s/%s has no TCP port %d",
				target.ID, namespace, serviceName, target.Port,
			)
		}
		addresses := serviceClusterIPs(service)
		if len(addresses) == 0 {
			return nil, fmt.Errorf(
				"Inspector target %q Service %s/%s has no ClusterIP",
				target.ID, namespace, serviceName,
			)
		}
		target.Namespace = namespace
		target.Service = serviceName
		target.ServiceUID = string(service.UID)
		target.Host = serviceName + "." + namespace + ".svc"
		target.Addresses = addresses
	}
	return result, nil
}

func serviceHasTCPPort(service *corev1.Service, port uint16) bool {
	if service == nil {
		return false
	}
	for _, servicePort := range service.Spec.Ports {
		if servicePort.Port == int32(port) &&
			(servicePort.Protocol == "" || servicePort.Protocol == corev1.ProtocolTCP) {
			return true
		}
	}
	return false
}

func serviceClusterIPs(service *corev1.Service) []string {
	if service == nil {
		return nil
	}
	values := service.Spec.ClusterIPs
	if len(values) == 0 {
		values = []string{service.Spec.ClusterIP}
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && value != corev1.ClusterIPNone {
			result = append(result, value)
		}
	}
	return result
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
	current := connection
	for {
		event, err := tunnel.ReadInspectorEvent(current)
		if err != nil {
			_ = current.Close()
			m.mu.Lock()
			ownsConnection := m.inspectorConn == current
			if ownsConnection {
				m.inspectorConn = nil
			}
			active := ownsConnection && m.inspector != nil && m.active && !m.stopping
			address := m.gatewayAddress
			token := m.sessionToken
			sessionContext := m.ctx
			m.mu.Unlock()
			if !active {
				return
			}
			reconnected := false
			for attempt := 0; attempt < 8; attempt++ {
				delay := time.Duration(1<<attempt) * 50 * time.Millisecond
				if delay > 2*time.Second {
					delay = 2 * time.Second
				}
				timer := time.NewTimer(delay)
				if sessionContext == nil {
					sessionContext = context.Background()
				}
				select {
				case <-sessionContext.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				dialContext, cancel := context.WithTimeout(sessionContext, 5*time.Second)
				next, openErr := openInspectorEvents(dialContext, address, token)
				cancel()
				if openErr != nil {
					continue
				}
				m.mu.Lock()
				if m.inspector == nil || !m.active || m.stopping ||
					m.inspectorConn != nil || m.sessionToken != token {
					m.mu.Unlock()
					_ = next.Close()
					return
				}
				m.inspectorConn = next
				m.mu.Unlock()
				current = next
				reconnected = true
				break
			}
			if !reconnected {
				return
			}
			continue
		}
		select {
		case m.inspectorEvents <- event:
		default:
			if event.Type == tunnel.InspectorEventBody ||
				event.Type == tunnel.InspectorEventGRPCMessage {
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
