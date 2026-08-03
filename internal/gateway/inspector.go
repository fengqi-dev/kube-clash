package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

type InspectorEngine interface {
	StartSession(
		context.Context, string, tunnel.InspectorConfig,
	) (InspectorEndpoint, error)
}

type InspectorEndpoint interface {
	DialContext(
		context.Context, tunnel.InspectorTarget, string,
	) (net.Conn, error)
	UpdateTargets(context.Context, []tunnel.InspectorTarget) error
	Events() <-chan tunnel.InspectorEvent
	Close() error
}

type inspectorSession struct {
	id       string
	endpoint InspectorEndpoint
	targets  map[string]tunnel.InspectorTarget
}

func (s *Server) SetInspectorEngine(engine InspectorEngine, name string) {
	s.mu.Lock()
	s.InspectorEngine = engine
	s.Capabilities.Inspector = engine != nil
	if engine == nil {
		s.Capabilities.Protocols = nil
		s.Capabilities.MaxBodySize = 0
		s.Capabilities.MaxTargets = 0
		s.Capabilities.Engine = ""
	} else {
		s.Capabilities.Protocols = []string{"http", "https"}
		s.Capabilities.MaxBodySize = 1 << 20
		s.Capabilities.MaxTargets = tunnel.MaxInspectorTargets
		s.Capabilities.Engine = name
	}
	s.mu.Unlock()
}

func (s *Server) startInspector(
	control *controlSession, config tunnel.InspectorConfig,
) error {
	if control.version != tunnel.ProtocolV2 {
		return errors.New("Inspector requires KCG2")
	}
	s.mu.Lock()
	engine := s.InspectorEngine
	capabilities := s.Capabilities
	s.mu.Unlock()
	if engine == nil || !capabilities.Inspector {
		return errors.New("Inspector Agent is unavailable")
	}
	if len(config.Targets) > capabilities.MaxTargets {
		return fmt.Errorf("Inspector target count exceeds %d", capabilities.MaxTargets)
	}
	if config.MaxBodySize > capabilities.MaxBodySize {
		return fmt.Errorf("Inspector body limit exceeds %d", capabilities.MaxBodySize)
	}

	control.inspectorMu.Lock()
	if control.inspector != nil {
		control.inspectorMu.Unlock()
		return errors.New("Inspector session is already active")
	}
	control.inspectorMu.Unlock()

	sessionID := inspectorSessionID(control.token)
	endpoint, err := engine.StartSession(context.Background(), sessionID, config)
	if err != nil {
		return fmt.Errorf("start Inspector worker: %w", err)
	}
	session := &inspectorSession{
		id:       sessionID,
		endpoint: endpoint,
		targets:  inspectorTargetMap(config.Targets),
	}
	control.inspectorMu.Lock()
	if control.inspector != nil {
		control.inspectorMu.Unlock()
		_ = endpoint.Close()
		return errors.New("Inspector session is already active")
	}
	control.inspector = session
	control.inspectorMu.Unlock()
	s.logf("Inspector session %s started with %d targets", sessionID, len(config.Targets))
	go control.forwardInspectorEvents(session)
	return nil
}

func (s *Server) updateInspectorTargets(
	control *controlSession, targets []tunnel.InspectorTarget,
) error {
	control.inspectorMu.RLock()
	session := control.inspector
	control.inspectorMu.RUnlock()
	if session == nil {
		return errors.New("Inspector session is not active")
	}
	if err := session.endpoint.UpdateTargets(context.Background(), targets); err != nil {
		return fmt.Errorf("update Inspector targets: %w", err)
	}
	control.inspectorMu.Lock()
	if control.inspector == session {
		session.targets = inspectorTargetMap(targets)
	}
	control.inspectorMu.Unlock()
	s.logf("Inspector session %s applied %d targets", session.id, len(targets))
	return nil
}

func (s *Server) stopInspector(control *controlSession) error {
	control.inspectorMu.Lock()
	session := control.inspector
	control.inspector = nil
	control.inspectorMu.Unlock()
	if session == nil {
		return nil
	}
	err := session.endpoint.Close()
	s.logf("Inspector session %s stopped", session.id)
	return err
}

func (c *controlSession) dialInspector(
	ctx context.Context, request tunnel.OpenRequest, targetAddress string,
) (net.Conn, bool, error) {
	if request.Command != tunnel.CommandTCP {
		return nil, false, nil
	}
	c.inspectorMu.RLock()
	session := c.inspector
	if session == nil {
		c.inspectorMu.RUnlock()
		return nil, false, nil
	}
	target, matched := session.targets[tunnel.InspectorTargetKey(request.Host, request.Port)]
	endpoint := session.endpoint
	c.inspectorMu.RUnlock()
	if !matched {
		return nil, false, nil
	}
	connection, err := endpoint.DialContext(ctx, target, targetAddress)
	return connection, true, err
}

func (c *controlSession) forwardInspectorEvents(session *inspectorSession) {
	events := session.endpoint.Events()
	if events == nil {
		return
	}
	for event := range events {
		c.inspectorMu.RLock()
		active := c.inspector == session
		c.inspectorMu.RUnlock()
		if !active {
			return
		}
		c.writeInspectorEvent(event)
	}
}

func (c *controlSession) writeInspectorEvent(event tunnel.InspectorEvent) {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	if c.events == nil {
		return
	}
	_ = c.events.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := tunnel.WriteInspectorEvent(c.events, event); err != nil {
		_ = c.events.Close()
		c.events = nil
	}
}

func inspectorTargetMap(
	targets []tunnel.InspectorTarget,
) map[string]tunnel.InspectorTarget {
	result := make(map[string]tunnel.InspectorTarget, len(targets))
	for _, target := range targets {
		result[tunnel.InspectorTargetKey(target.Host, target.Port)] = target
	}
	return result
}

func inspectorSessionID(token tunnel.SessionToken) string {
	sum := sha256.Sum256(token[:])
	return hex.EncodeToString(sum[:16])
}
