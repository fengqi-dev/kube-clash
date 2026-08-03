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
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

const (
	defaultMaxSessions = 16
	eventQueueSize     = 512
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

	mu          sync.RWMutex
	targets     map[string]tunnel.InspectorTarget
	connections map[net.Conn]struct{}
	subscribed  bool
	closed      bool
	events      chan tunnel.InspectorEvent
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
	default:
		_ = writeJSON(connection, response{Error: "unsupported Inspector Agent operation"})
	}
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
		targets:     inspectorTargets(value.Config.Targets),
		connections: make(map[net.Conn]struct{}),
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
			if target.Protocol == "https" {
				session.mu.Unlock()
				return errors.New("adding HTTPS targets requires restarting Inspector with TLS")
			}
		}
	}
	session.targets = inspectorTargets(value.Targets)
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
	s.logf("worker %s stopped", sessionID)
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
	if target.Protocol == "https" {
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
	}()
	if err := writeJSON(connection, response{OK: true}); err != nil {
		return
	}
	for {
		select {
		case event := <-session.events:
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
	if !address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsMulticast() {
		return tunnel.InspectorTarget{}, errors.New("Inspector target address is not private")
	}
	return expected, nil
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
	select {
	case s.events <- event:
	default:
		if event.Type == tunnel.InspectorEventBody {
			return
		}
		select {
		case <-s.events:
		default:
		}
		select {
		case s.events <- event:
		default:
		}
	}
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
