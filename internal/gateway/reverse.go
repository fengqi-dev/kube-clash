package gateway

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

const (
	pendingAcceptTimeout = 15 * time.Second
	udpAssociationIdle   = 60 * time.Second
)

type controlSession struct {
	conn    net.Conn
	server  *Server
	version byte
	token   tunnel.SessionToken
	mu      sync.Mutex
	eventMu sync.Mutex
	events  net.Conn

	inspectorMu sync.RWMutex
	inspector   *inspectorSession
}

type interceptListener struct {
	id      string
	network byte
	port    uint16
	tcp     net.Listener
	udp     net.PacketConn
	cancel  chan struct{}
	server  *Server
	control *controlSession
}

type pendingStream struct {
	id        uint64
	network   byte
	control   *controlSession
	ready     chan net.Conn
	tcpConn   net.Conn
	udpPacket net.PacketConn
	assoc     *udpAssociation
	timer     *time.Timer
}

type udpAssociation struct {
	remote    net.Addr
	first     []byte
	tunnelMu  sync.Mutex
	tunnel    net.Conn
	lastSeen  time.Time
	pendingID uint64
}

func (s *Server) handleControl(client net.Conn, header tunnel.SessionHeader) {
	session := &controlSession{
		conn: client, server: s, version: header.Version, token: header.Token,
	}
	var replaced *controlSession
	s.mu.Lock()
	if header.Version == tunnel.ProtocolV2 {
		replaced = s.controlsByToken[header.Token]
		s.controlsByToken[header.Token] = session
	}
	s.controls[session] = struct{}{}
	s.mu.Unlock()
	if replaced != nil {
		_ = replaced.conn.Close()
	}
	defer func() {
		s.removeControl(session)
		_ = client.Close()
	}()
	if err := tunnel.WriteStatus(client, nil); err != nil {
		return
	}
	if header.Version == tunnel.ProtocolV2 {
		s.mu.Lock()
		capabilities := s.Capabilities
		s.mu.Unlock()
		if err := tunnel.WriteCapabilities(client, capabilities); err != nil {
			return
		}
	}
	for {
		message, err := tunnel.ReadControlMessage(client)
		if err != nil {
			return
		}
		switch message.Type {
		case tunnel.CtrlRegister:
			if err := s.registerIntercept(session, message); err != nil {
				_ = session.reply(tunnel.ControlMessage{Type: tunnel.CtrlError, Error: err.Error()})
				continue
			}
			_ = session.reply(tunnel.ControlMessage{Type: tunnel.CtrlAck})
		case tunnel.CtrlUnregister:
			s.unregisterIntercept(message.InterceptID)
			_ = session.reply(tunnel.ControlMessage{Type: tunnel.CtrlAck})
		case tunnel.CtrlInspectorStart:
			if session.version != tunnel.ProtocolV2 {
				_ = session.reply(tunnel.ControlMessage{
					Type: tunnel.CtrlError, Error: "Inspector requires KCG2",
				})
				continue
			}
			if err := s.startInspector(session, *message.Inspector); err != nil {
				_ = session.reply(tunnel.ControlMessage{Type: tunnel.CtrlError, Error: err.Error()})
				continue
			}
			_ = session.reply(tunnel.ControlMessage{Type: tunnel.CtrlAck})
		case tunnel.CtrlInspectorUpdateTargets:
			if err := s.updateInspectorTargets(session, message.Targets); err != nil {
				_ = session.reply(tunnel.ControlMessage{Type: tunnel.CtrlError, Error: err.Error()})
				continue
			}
			_ = session.reply(tunnel.ControlMessage{Type: tunnel.CtrlAck})
		case tunnel.CtrlInspectorStop:
			if err := s.stopInspector(session); err != nil {
				_ = session.reply(tunnel.ControlMessage{Type: tunnel.CtrlError, Error: err.Error()})
				continue
			}
			_ = session.reply(tunnel.ControlMessage{Type: tunnel.CtrlAck})
		default:
			_ = session.reply(tunnel.ControlMessage{
				Type: tunnel.CtrlError, Error: fmt.Sprintf("unsupported control type %d", message.Type),
			})
		}
	}
}

func (c *controlSession) reply(message tunnel.ControlMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return tunnel.WriteControlMessage(c.conn, message)
}

func (c *controlSession) attachEvents(connection net.Conn) error {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	if c.events != nil {
		return errors.New("Inspector event channel is already connected")
	}
	c.events = connection
	return nil
}

func (c *controlSession) detachEvents(connection net.Conn) {
	c.eventMu.Lock()
	if c.events == connection {
		c.events = nil
	}
	c.eventMu.Unlock()
}

func (s *Server) removeControl(session *controlSession) {
	_ = s.stopInspector(session)
	session.eventMu.Lock()
	events := session.events
	session.events = nil
	session.eventMu.Unlock()
	if events != nil {
		_ = events.Close()
	}
	s.mu.Lock()
	delete(s.controls, session)
	if session.version == tunnel.ProtocolV2 && s.controlsByToken[session.token] == session {
		delete(s.controlsByToken, session.token)
	}
	var toClose []*interceptListener
	for id, listener := range s.listeners {
		if listener.control == session {
			toClose = append(toClose, listener)
			delete(s.listeners, id)
		}
	}
	s.mu.Unlock()
	for _, listener := range toClose {
		listener.stop()
	}
}

func (s *Server) registerIntercept(session *controlSession, message tunnel.ControlMessage) error {
	if message.ListenPort < 1024 {
		return errors.New("listen port must be >= 1024")
	}
	s.mu.Lock()
	if _, exists := s.listeners[message.InterceptID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("intercept %q already registered", message.InterceptID)
	}
	for _, listener := range s.listeners {
		if listener.port == message.ListenPort && listener.network == message.Network {
			s.mu.Unlock()
			return fmt.Errorf("listen port %d already in use", message.ListenPort)
		}
	}
	s.mu.Unlock()

	listener := &interceptListener{
		id:      message.InterceptID,
		network: message.Network,
		port:    message.ListenPort,
		cancel:  make(chan struct{}),
		server:  s,
		control: session,
	}
	address := fmt.Sprintf(":%d", message.ListenPort)
	switch message.Network {
	case tunnel.NetworkTCP:
		tcp, err := net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("listen tcp: %w", err)
		}
		listener.tcp = tcp
		go listener.acceptTCP()
	case tunnel.NetworkUDP:
		udp, err := net.ListenPacket("udp4", "0.0.0.0:"+fmt.Sprintf("%d", message.ListenPort))
		if err != nil {
			return fmt.Errorf("listen udp: %w", err)
		}
		listener.udp = udp
		go listener.acceptUDP()
	default:
		return fmt.Errorf("unsupported network %d", message.Network)
	}

	s.mu.Lock()
	s.listeners[message.InterceptID] = listener
	s.mu.Unlock()
	s.logf("registered intercept %s on %s/%d", message.InterceptID, networkName(message.Network), message.ListenPort)
	return nil
}

func (s *Server) unregisterIntercept(id string) {
	s.mu.Lock()
	listener := s.listeners[id]
	delete(s.listeners, id)
	s.mu.Unlock()
	if listener != nil {
		listener.stop()
		s.logf("unregistered intercept %s", id)
	}
}

func (l *interceptListener) stop() {
	select {
	case <-l.cancel:
	default:
		close(l.cancel)
	}
	if l.tcp != nil {
		_ = l.tcp.Close()
	}
	if l.udp != nil {
		_ = l.udp.Close()
	}
}

func (l *interceptListener) acceptTCP() {
	for {
		conn, err := l.tcp.Accept()
		if err != nil {
			select {
			case <-l.cancel:
				return
			default:
				if errors.Is(err, net.ErrClosed) {
					return
				}
				l.server.logf("tcp accept %s: %v", l.id, err)
				continue
			}
		}
		streamID := l.server.nextStream.Add(1)
		pending := &pendingStream{
			id:      streamID,
			network: tunnel.NetworkTCP,
			control: l.control,
			ready:   make(chan net.Conn, 1),
			tcpConn: conn,
		}
		if !l.server.offerPending(pending) {
			_ = conn.Close()
			continue
		}
		if err := l.control.reply(tunnel.ControlMessage{
			Type:        tunnel.CtrlInboundReady,
			InterceptID: l.id,
			Network:     tunnel.NetworkTCP,
			StreamID:    streamID,
		}); err != nil {
			l.server.takePending(streamID)
			_ = conn.Close()
			return
		}
	}
}

func (l *interceptListener) acceptUDP() {
	buffer := make([]byte, tunnel.MaxDatagramSize)
	associations := make(map[string]*udpAssociation)
	var mu sync.Mutex

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-l.cancel:
				return
			case <-ticker.C:
				cutoff := time.Now().Add(-udpAssociationIdle)
				mu.Lock()
				for key, assoc := range associations {
					assoc.tunnelMu.Lock()
					idle := assoc.tunnel == nil && assoc.lastSeen.Before(cutoff)
					if assoc.tunnel != nil {
						idle = assoc.lastSeen.Before(cutoff)
					}
					if idle {
						if assoc.tunnel != nil {
							_ = assoc.tunnel.Close()
						}
						if assoc.pendingID != 0 {
							if pending := l.server.takePending(assoc.pendingID); pending != nil {
								pending.close()
							}
						}
						delete(associations, key)
					}
					assoc.tunnelMu.Unlock()
				}
				mu.Unlock()
			}
		}
	}()

	for {
		_ = l.udp.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, remote, err := l.udp.ReadFrom(buffer)
		if err != nil {
			select {
			case <-l.cancel:
				mu.Lock()
				for _, assoc := range associations {
					assoc.tunnelMu.Lock()
					if assoc.tunnel != nil {
						_ = assoc.tunnel.Close()
					}
					if assoc.pendingID != 0 {
						if pending := l.server.takePending(assoc.pendingID); pending != nil {
							pending.close()
						}
					}
					assoc.tunnelMu.Unlock()
				}
				mu.Unlock()
				return
			default:
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				if errors.Is(err, net.ErrClosed) {
					return
				}
				l.server.logf("udp read %s: %v", l.id, err)
				continue
			}
		}
		payload := append([]byte(nil), buffer[:n]...)
		key := remote.String()
		mu.Lock()
		assoc := associations[key]
		if assoc != nil {
			assoc.lastSeen = time.Now()
			assoc.tunnelMu.Lock()
			tunnelConn := assoc.tunnel
			assoc.tunnelMu.Unlock()
			if tunnelConn != nil {
				mu.Unlock()
				if err := tunnel.WriteDatagram(tunnelConn, payload); err != nil {
					mu.Lock()
					assoc.tunnelMu.Lock()
					_ = assoc.tunnel.Close()
					assoc.tunnel = nil
					assoc.tunnelMu.Unlock()
					delete(associations, key)
					mu.Unlock()
				}
				continue
			}
			assoc.first = payload
			mu.Unlock()
			continue
		}

		streamID := l.server.nextStream.Add(1)
		assoc = &udpAssociation{
			remote:    remote,
			first:     payload,
			lastSeen:  time.Now(),
			pendingID: streamID,
		}
		associations[key] = assoc
		pending := &pendingStream{
			id:        streamID,
			network:   tunnel.NetworkUDP,
			control:   l.control,
			ready:     make(chan net.Conn, 1),
			udpPacket: l.udp,
			assoc:     assoc,
		}
		mu.Unlock()

		if !l.server.offerPending(pending) {
			mu.Lock()
			delete(associations, key)
			mu.Unlock()
			continue
		}
		if err := l.control.reply(tunnel.ControlMessage{
			Type:        tunnel.CtrlInboundReady,
			InterceptID: l.id,
			Network:     tunnel.NetworkUDP,
			StreamID:    streamID,
		}); err != nil {
			l.server.takePending(streamID)
			mu.Lock()
			delete(associations, key)
			mu.Unlock()
			return
		}
	}
}

func (s *Server) offerPending(pending *pendingStream) bool {
	pending.timer = time.AfterFunc(pendingAcceptTimeout, func() {
		if taken := s.takePending(pending.id); taken != nil {
			taken.close()
		}
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.pending[pending.id]; exists {
		return false
	}
	s.pending[pending.id] = pending
	return true
}

func (s *Server) takePending(streamID uint64) *pendingStream {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending[streamID]
	delete(s.pending, streamID)
	if pending != nil && pending.timer != nil {
		pending.timer.Stop()
	}
	return pending
}

func (s *Server) takePendingForSession(
	streamID uint64, header tunnel.SessionHeader,
) *pendingStream {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending[streamID]
	if pending == nil {
		return nil
	}
	if header.Version == tunnel.ProtocolV2 {
		if pending.control == nil ||
			pending.control.version != tunnel.ProtocolV2 ||
			pending.control.token != header.Token {
			return nil
		}
	} else if pending.control != nil && pending.control.version != tunnel.ProtocolV1 {
		return nil
	}
	delete(s.pending, streamID)
	if pending.timer != nil {
		pending.timer.Stop()
	}
	return pending
}

func (p *pendingStream) close() {
	if p.tcpConn != nil {
		_ = p.tcpConn.Close()
	}
}

func (p *pendingStream) serve(tunnelConn net.Conn) {
	switch p.network {
	case tunnel.NetworkTCP:
		defer tunnelConn.Close()
		defer p.tcpConn.Close()
		relayTCP(tunnelConn, p.tcpConn)
	case tunnel.NetworkUDP:
		p.serveUDP(tunnelConn)
	default:
		_ = tunnelConn.Close()
	}
}

func (p *pendingStream) serveUDP(tunnelConn net.Conn) {
	assoc := p.assoc
	assoc.tunnelMu.Lock()
	assoc.tunnel = tunnelConn
	assoc.pendingID = 0
	first := assoc.first
	assoc.first = nil
	remote := assoc.remote
	assoc.tunnelMu.Unlock()

	if len(first) > 0 {
		if err := tunnel.WriteDatagram(tunnelConn, first); err != nil {
			_ = tunnelConn.Close()
			return
		}
	}

	// Own desktop → cluster UDP writes; inbound packets are demuxed by acceptUDP.
	reader := bufio.NewReader(tunnelConn)
	var buffer []byte
	packetConn := p.packetConn()
	for {
		_ = tunnelConn.SetReadDeadline(time.Now().Add(udpAssociationIdle))
		payload, err := tunnel.ReadDatagram(reader, buffer)
		if err != nil {
			_ = tunnelConn.Close()
			assoc.tunnelMu.Lock()
			if assoc.tunnel == tunnelConn {
				assoc.tunnel = nil
			}
			assoc.tunnelMu.Unlock()
			return
		}
		buffer = payload[:0]
		assoc.lastSeen = time.Now()
		if packetConn == nil {
			_ = tunnelConn.Close()
			return
		}
		if _, err := packetConn.WriteTo(payload, remote); err != nil {
			_ = tunnelConn.Close()
			assoc.tunnelMu.Lock()
			if assoc.tunnel == tunnelConn {
				assoc.tunnel = nil
			}
			assoc.tunnelMu.Unlock()
			return
		}
	}
}

func (p *pendingStream) packetConn() net.PacketConn {
	return p.udpPacket
}

func networkName(network byte) string {
	if network == tunnel.NetworkUDP {
		return "udp"
	}
	return "tcp"
}
