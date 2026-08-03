package inspectoragent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

const (
	defaultMaxSessions = 16
	eventQueueSize     = 512
	maxEventQueueBytes = 50 << 20
)

type Server struct {
	Logger      *log.Logger
	MaxSessions int
	DialTimeout time.Duration

	mu       sync.Mutex
	sessions map[string]*agentSession
}

type agentSession struct {
	id          string
	maxBodySize int64
	tls         *tlsAuthority
	logf        func(string, ...any)

	mu          sync.RWMutex
	targets     map[string]tunnel.InspectorTarget
	tlsBypass   map[string]string
	connections map[net.Conn]struct{}
	bridges     map[string]*bridgePair
	subscribed  bool
	closed      bool
	events      chan tunnel.InspectorEvent
	eventBytes  atomic.Int64
	dropped     atomic.Uint64
	degraded    atomic.Bool
	done        chan struct{}
	closeOnce   sync.Once
}

func NewServer(logger *log.Logger) *Server {
	return &Server{
		Logger: logger, MaxSessions: defaultMaxSessions, DialTimeout: 10 * time.Second,
		sessions: make(map[string]*agentSession),
	}
}

func (s *Server) Serve(listener net.Listener) error {
	for {
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go s.handle(connection)
	}
}

func (s *Server) handle(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReader(connection)
	value, err := readRequest(reader)
	if err != nil {
		_ = writeJSON(connection, response{Error: err.Error()})
		return
	}
	_ = connection.SetReadDeadline(time.Time{})
	switch value.Op {
	case opPing:
		_ = writeJSON(connection, response{OK: true})
	case opStart:
		err = s.start(value)
		_ = writeRPCResult(connection, err)
	case opUpdate:
		err = s.update(value)
		_ = writeRPCResult(connection, err)
	case opStop:
		err = s.stop(value.SessionID)
		_ = writeRPCResult(connection, err)
	case opDial:
		s.handleDial(connection, reader, value)
	case opEvents:
		s.handleEvents(connection, value.SessionID)
	case opBridgeClient, opBridgeUpstream:
		s.handleBridge(connection, reader, value)
	default:
		_ = writeJSON(connection, response{Error: "unsupported Inspector Agent operation"})
	}
}

type bridgePair struct {
	target         tunnel.InspectorTarget
	client         net.Conn
	clientReader   *bufio.Reader
	upstream       net.Conn
	upstreamReader *bufio.Reader
	done           chan struct{}
	started        chan struct{}
	startOnce      sync.Once
	doneOnce       sync.Once
}

func (s *Server) handleBridge(
	connection net.Conn, reader *bufio.Reader, value request,
) {
	session := s.session(value.SessionID)
	if session == nil || value.Target == nil || value.PairID == "" {
		_ = writeJSON(connection, response{Error: "Inspector bridge request is invalid"})
		return
	}
	target, err := session.authorizeLogicalTarget(*value.Target)
	if err != nil {
		_ = writeJSON(connection, response{Error: err.Error()})
		return
	}
	if !session.attach(connection) {
		_ = writeJSON(connection, response{Error: "Inspector worker is stopping"})
		return
	}
	defer session.detach(connection)

	session.mu.Lock()
	pair := session.bridges[value.PairID]
	if pair == nil {
		pair = &bridgePair{
			target: target, done: make(chan struct{}), started: make(chan struct{}),
		}
		session.bridges[value.PairID] = pair
	}
	if pair.target.ID != target.ID {
		session.mu.Unlock()
		_ = writeJSON(connection, response{Error: "Inspector bridge target mismatch"})
		return
	}
	switch value.Op {
	case opBridgeClient:
		if pair.client != nil {
			session.mu.Unlock()
			_ = writeJSON(connection, response{Error: "Inspector bridge client already attached"})
			return
		}
		pair.client, pair.clientReader = connection, reader
	case opBridgeUpstream:
		if pair.upstream != nil {
			session.mu.Unlock()
			_ = writeJSON(connection, response{Error: "Inspector bridge upstream already attached"})
			return
		}
		pair.upstream, pair.upstreamReader = connection, reader
	}
	if pair.client != nil && pair.upstream != nil {
		pair.startOnce.Do(func() {
			close(pair.started)
			go session.serveBridge(value.PairID, pair)
		})
	}
	session.mu.Unlock()
	if err := writeJSON(connection, response{OK: true}); err != nil {
		return
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-pair.started:
		select {
		case <-pair.done:
		case <-session.done:
		}
	case <-pair.done:
	case <-session.done:
	case <-timer.C:
		select {
		case <-pair.started:
			select {
			case <-pair.done:
			case <-session.done:
			}
		default:
			session.cancelBridge(value.PairID, pair)
		}
	}
}

func (p *bridgePair) finish() {
	p.doneOnce.Do(func() { close(p.done) })
}

func (s *agentSession) cancelBridge(pairID string, pair *bridgePair) {
	s.mu.Lock()
	if s.bridges[pairID] == pair {
		delete(s.bridges, pairID)
	}
	client := pair.client
	upstream := pair.upstream
	s.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
	if upstream != nil {
		_ = upstream.Close()
	}
	pair.finish()
}

func (s *agentSession) authorizeLogicalTarget(
	target tunnel.InspectorTarget,
) (tunnel.InspectorTarget, error) {
	s.mu.RLock()
	expected, exists := s.targets[tunnel.InspectorTargetKey(target.Host, target.Port)]
	closed := s.closed
	s.mu.RUnlock()
	if closed || !exists || expected.ID != target.ID {
		return tunnel.InspectorTarget{}, errors.New("Inspector target is not authorized")
	}
	return expected, nil
}

func (s *agentSession) serveBridge(pairID string, pair *bridgePair) {
	defer func() {
		s.mu.Lock()
		delete(s.bridges, pairID)
		s.mu.Unlock()
		pair.finish()
	}()
	target := pair.target
	target.FlowSource = "cluster"
	if target.Protocol == "https" || target.Protocol == "http2" ||
		target.Protocol == "grpc" {
		serveHTTPSConnection(
			s, pair.client, pair.clientReader, pair.upstream, target,
		)
		return
	}
	serveHTTPConnection(
		s, pair.client, pair.clientReader, pair.upstream, target, nil,
	)
}

func (s *Server) start(value request) error {
	if err := validateSessionID(value.SessionID); err != nil {
		return err
	}
	if value.Config == nil {
		return errors.New("Inspector config is required")
	}
	if err := value.Config.Validate(); err != nil {
		return err
	}
	authority, err := newTLSAuthority(value.Config.TLS)
	if err != nil {
		return fmt.Errorf("prepare Inspector TLS: %w", err)
	}
	maxSessions := s.MaxSessions
	if maxSessions <= 0 {
		maxSessions = defaultMaxSessions
	}
	session := &agentSession{
		id:          value.SessionID,
		maxBodySize: value.Config.MaxBodySize,
		tls:         authority,
		logf:        s.logf,
		targets:     inspectorTargets(value.Config.Targets),
		tlsBypass:   make(map[string]string),
		connections: make(map[net.Conn]struct{}),
		bridges:     make(map[string]*bridgePair),
		events:      make(chan tunnel.InspectorEvent, eventQueueSize),
		done:        make(chan struct{}),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[value.SessionID] != nil {
		return errors.New("Inspector worker is already active")
	}
	if len(s.sessions) >= maxSessions {
		return fmt.Errorf("Inspector worker limit %d reached", maxSessions)
	}
	s.sessions[value.SessionID] = session
	s.logf("worker %s started with %d targets", value.SessionID, len(value.Config.Targets))
	return nil
}

func (s *Server) update(value request) error {
	if err := tunnel.ValidateInspectorTargets(value.Targets); err != nil {
		return err
	}
	session := s.session(value.SessionID)
	if session == nil {
		return errors.New("Inspector worker is not active")
	}
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return errors.New("Inspector worker is not active")
	}
	if session.tls == nil {
		for _, target := range value.Targets {
			if target.Protocol == "https" || target.Protocol == "http2" ||
				target.Protocol == "grpc" {
				session.mu.Unlock()
				return errors.New("adding HTTPS targets requires restarting Inspector with TLS")
			}
		}
	}
	nextTargets := inspectorTargets(value.Targets)
	for key := range session.tlsBypass {
		if _, exists := nextTargets[key]; !exists {
			delete(session.tlsBypass, key)
		}
	}
	session.targets = nextTargets
	session.mu.Unlock()
	s.logf("worker %s applied %d targets", value.SessionID, len(value.Targets))
	return nil
}

func (s *Server) stop(sessionID string) error {
	s.mu.Lock()
	session := s.sessions[sessionID]
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	if session == nil {
		return nil
	}
	session.close()
	s.logf(
		"worker %s stopped: dropped_events=%d",
		sessionID, session.dropped.Load(),
	)
	return nil
}

func (s *Server) handleDial(
	client net.Conn, reader *bufio.Reader, value request,
) {
	session := s.session(value.SessionID)
	if session == nil {
		_ = writeJSON(client, response{Error: "Inspector worker is not active"})
		return
	}
	if value.Target == nil {
		_ = writeJSON(client, response{Error: "Inspector target is required"})
		return
	}
	target, err := session.authorizeTarget(*value.Target, value.TargetAddress)
	if err != nil {
		_ = writeJSON(client, response{Error: err.Error()})
		return
	}
	if reason := session.tlsBypassReason(target); reason != "" {
		_ = writeJSON(client, response{Error: reason})
		return
	}
	timeout := s.DialTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	upstream, err := net.DialTimeout("tcp", value.TargetAddress, timeout)
	if err != nil {
		_ = writeJSON(client, response{Error: fmt.Sprintf("dial target: %v", err)})
		return
	}
	defer upstream.Close()
	if !session.attach(client) {
		_ = writeJSON(client, response{Error: "Inspector worker is stopping"})
		return
	}
	defer session.detach(client)
	if !session.attach(upstream) {
		_ = writeJSON(client, response{Error: "Inspector worker is stopping"})
		return
	}
	defer session.detach(upstream)
	if target.Protocol == "https" || target.Protocol == "http2" ||
		target.Protocol == "grpc" {
		upstreamTLS, metadata, err := prepareHTTPSUpstream(session, upstream, target)
		if err != nil {
			_ = writeJSON(client, response{Error: err.Error()})
			return
		}
		if err := writeJSON(client, response{OK: true}); err != nil {
			return
		}
		serveHTTPSClient(session, client, reader, upstreamTLS, target, metadata)
		return
	}
	if err := writeJSON(client, response{OK: true}); err != nil {
		return
	}
	serveHTTPConnection(session, client, reader, upstream, target, nil)
}

func (s *Server) handleEvents(connection net.Conn, sessionID string) {
	session := s.session(sessionID)
	if session == nil {
		_ = writeJSON(connection, response{Error: "Inspector worker is not active"})
		return
	}
	session.mu.Lock()
	if session.closed || session.subscribed {
		session.mu.Unlock()
		_ = writeJSON(connection, response{Error: "Inspector event subscriber is unavailable"})
		return
	}
	session.subscribed = true
	session.mu.Unlock()
	defer func() {
		session.mu.Lock()
		session.subscribed = false
		session.mu.Unlock()
		s.logf("worker %s event subscriber disconnected", sessionID)
	}()
	if err := writeJSON(connection, response{OK: true}); err != nil {
		return
	}
	s.logf("worker %s event subscriber connected", sessionID)
	for {
		select {
		case event := <-session.events:
			session.releaseEvent(event)
			if session.eventBytes.Load() < maxEventQueueBytes/2 &&
				session.degraded.CompareAndSwap(true, false) {
				session.logf("worker %s event queue recovered", session.id)
			}
			_ = connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := tunnel.WriteInspectorEvent(connection, event); err != nil {
				return
			}
		case <-session.done:
			return
		}
	}
}

func (s *Server) session(id string) *agentSession {
	s.mu.Lock()
	session := s.sessions[id]
	s.mu.Unlock()
	return session
}

func (s *Server) Close() error {
	s.mu.Lock()
	sessions := make([]*agentSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.sessions = make(map[string]*agentSession)
	s.mu.Unlock()
	for _, session := range sessions {
		session.close()
	}
	return nil
}

func (s *agentSession) authorizeTarget(
	target tunnel.InspectorTarget, targetAddress string,
) (tunnel.InspectorTarget, error) {
	s.mu.RLock()
	expected, exists := s.targets[tunnel.InspectorTargetKey(target.Host, target.Port)]
	closed := s.closed
	s.mu.RUnlock()
	if closed || !exists || expected.ID != target.ID {
		return tunnel.InspectorTarget{}, errors.New("Inspector target is not authorized")
	}
	host, rawPort, err := net.SplitHostPort(targetAddress)
	if err != nil || rawPort == "" {
		return tunnel.InspectorTarget{}, errors.New("Inspector target address is invalid")
	}
	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil || uint16(port) != expected.Port {
		return tunnel.InspectorTarget{}, errors.New("Inspector target port is not authorized")
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return tunnel.InspectorTarget{}, errors.New("Inspector target address must be an IP")
	}
	address = address.Unmap()
	if len(expected.Addresses) > 0 {
		for _, allowed := range expected.Addresses {
			allowedAddress, parseErr := netip.ParseAddr(allowed)
			if parseErr == nil && allowedAddress.Unmap() == address {
				return expected, nil
			}
		}
		return tunnel.InspectorTarget{}, errors.New(
			"Inspector target address is outside the authorized Service",
		)
	}
	if !address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsMulticast() {
		return tunnel.InspectorTarget{}, errors.New("Inspector target address is not private")
	}
	return expected, nil
}

func (s *agentSession) tlsBypassReason(target tunnel.InspectorTarget) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tlsBypass[tunnel.InspectorTargetKey(target.Host, target.Port)]
}

func (s *agentSession) learnTLSBypass(target tunnel.InspectorTarget) {
	key := tunnel.InspectorTargetKey(target.Host, target.Port)
	reason := "Inspector TLS was rejected by the client; using direct passthrough for subsequent connections"
	s.mu.Lock()
	if s.tlsBypass == nil {
		s.tlsBypass = make(map[string]string)
	}
	_, exists := s.tlsBypass[key]
	s.tlsBypass[key] = reason
	s.mu.Unlock()
	if exists {
		return
	}
	if s.logf != nil {
		s.logf(
			"worker %s learned TLS passthrough for %s after client certificate rejection",
			s.id, key,
		)
	}
	flowID := fmt.Sprintf("%s-tls-%d", s.id, nextFlowID.Add(1))
	emitJSON(s, tunnel.InspectorEvent{
		Version:  tunnel.InspectorEventVersion1,
		Type:     tunnel.InspectorEventError,
		FlowID:   flowID,
		Sequence: 1,
	}, map[string]any{
		"targetID":        target.ID,
		"stage":           "client-tls-handshake",
		"error":           "client rejected Inspector TLS certificate",
		"possiblePinning": true,
		"fallback":        "subsequent-connections",
	})
}

func (s *agentSession) attach(connection net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.connections[connection] = struct{}{}
	return true
}

func (s *agentSession) detach(connection net.Conn) {
	s.mu.Lock()
	delete(s.connections, connection)
	s.mu.Unlock()
}

func (s *agentSession) close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		connections := make([]net.Conn, 0, len(s.connections))
		for connection := range s.connections {
			connections = append(connections, connection)
		}
		s.connections = make(map[net.Conn]struct{})
		close(s.done)
		s.mu.Unlock()
		for _, connection := range connections {
			_ = connection.Close()
		}
	})
}

func (s *agentSession) emit(event tunnel.InspectorEvent) {
	if !s.reserveEvent(event) {
		if event.Type == tunnel.InspectorEventBody ||
			event.Type == tunnel.InspectorEventGRPCMessage {
			s.recordEventDrop("memory-limit")
			return
		}
		reserved := false
		for !reserved {
			select {
			case discarded := <-s.events:
				s.releaseEvent(discarded)
				s.recordEventDrop("metadata-priority")
				reserved = s.reserveEvent(event)
			default:
				s.recordEventDrop("memory-limit")
				return
			}
		}
	}
	select {
	case s.events <- event:
	default:
		if event.Type == tunnel.InspectorEventBody ||
			event.Type == tunnel.InspectorEventGRPCMessage {
			s.releaseEvent(event)
			s.recordEventDrop("queue-full")
			return
		}
		select {
		case discarded := <-s.events:
			s.releaseEvent(discarded)
			s.recordEventDrop("metadata-priority")
		default:
		}
		select {
		case s.events <- event:
		default:
			s.releaseEvent(event)
			s.recordEventDrop("queue-race")
		}
	}
}

func (s *agentSession) recordEventDrop(reason string) {
	s.dropped.Add(1)
	if s.degraded.CompareAndSwap(false, true) && s.logf != nil {
		s.logf("worker %s event queue degraded: reason=%s", s.id, reason)
	}
}

func inspectorEventSize(event tunnel.InspectorEvent) int64 {
	return int64(len(event.FlowID) + len(event.Payload) + 16)
}

func (s *agentSession) reserveEvent(event tunnel.InspectorEvent) bool {
	size := inspectorEventSize(event)
	for {
		current := s.eventBytes.Load()
		if current+size > maxEventQueueBytes {
			return false
		}
		if s.eventBytes.CompareAndSwap(current, current+size) {
			return true
		}
	}
}

func (s *agentSession) releaseEvent(event tunnel.InspectorEvent) {
	s.eventBytes.Add(-inspectorEventSize(event))
}

func ListenUnix(ctx context.Context, socketPath string) (net.Listener, error) {
	if socketPath == "" {
		return nil, errors.New("Inspector Agent socket path is required")
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o750); err != nil {
		return nil, fmt.Errorf("create Inspector socket directory: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale Inspector socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		_ = listener.Close()
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	return listener, nil
}

func writeRPCResult(w io.Writer, err error) error {
	if err != nil {
		return writeJSON(w, response{Error: err.Error()})
	}
	return writeJSON(w, response{OK: true})
}

func validateSessionID(value string) error {
	if value == "" || len(value) > 128 {
		return errors.New("Inspector session ID length is invalid")
	}
	for _, character := range value {
		if (character < 'a' || character > 'f') &&
			(character < '0' || character > '9') {
			return errors.New("Inspector session ID is invalid")
		}
	}
	return nil
}

func inspectorTargets(
	targets []tunnel.InspectorTarget,
) map[string]tunnel.InspectorTarget {
	result := make(map[string]tunnel.InspectorTarget, len(targets))
	for _, target := range targets {
		result[tunnel.InspectorTargetKey(target.Host, target.Port)] = target
	}
	return result
}

func (s *Server) logf(format string, values ...any) {
	if s.Logger != nil {
		s.Logger.Printf(format, values...)
	}
}
