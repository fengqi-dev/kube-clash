package gateway

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

type Server struct {
	Logger          *log.Logger
	DialTimeout     time.Duration
	Capabilities    tunnel.Capabilities
	InspectorEngine InspectorEngine

	mu              sync.Mutex
	nextStream      atomic.Uint64
	controls        map[*controlSession]struct{}
	controlsByToken map[tunnel.SessionToken]*controlSession
	listeners       map[string]*interceptListener
	pending         map[uint64]*pendingStream
}

func NewServer(logger *log.Logger, dialTimeout time.Duration) *Server {
	if dialTimeout == 0 {
		dialTimeout = 10 * time.Second
	}
	return &Server{
		Logger:      logger,
		DialTimeout: dialTimeout,
		Capabilities: tunnel.Capabilities{
			ProtocolVersion: int(tunnel.ProtocolV2),
			Inspector:       false,
		},
		controls:        make(map[*controlSession]struct{}),
		controlsByToken: make(map[tunnel.SessionToken]*controlSession),
		listeners:       make(map[string]*interceptListener),
		pending:         make(map[uint64]*pendingStream),
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

func (s *Server) handle(client net.Conn) {
	_ = client.SetReadDeadline(time.Now().Add(15 * time.Second))
	header, err := tunnel.ReadSessionHeaderInfo(client)
	if err != nil {
		_ = client.Close()
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			s.logf("reject handshake from %s: %v", client.RemoteAddr(), err)
		}
		return
	}
	_ = client.SetReadDeadline(time.Time{})

	switch header.Command {
	case tunnel.CommandTCP, tunnel.CommandUDP:
		s.handleOutbound(client, header)
	case tunnel.CommandControl:
		s.handleControl(client, header)
	case tunnel.CommandAccept:
		s.handleAccept(client, header)
	case tunnel.CommandInspectorEvents:
		s.handleInspectorEvents(client, header)
	default:
		_ = tunnel.WriteStatus(client, fmt.Errorf("unsupported command %d", header.Command))
		_ = client.Close()
	}
}

func (s *Server) handleOutbound(client net.Conn, header tunnel.SessionHeader) {
	defer client.Close()
	control, err := s.authorizeSession(header)
	if err != nil {
		// WriteOpen sends the header and open body together. Drain that bounded
		// body before closing so Windows does not turn the close into a TCP RST
		// that discards the status response.
		_ = client.SetReadDeadline(time.Now().Add(time.Second))
		_, _ = tunnel.ReadOpenBody(client, header.Command)
		_ = client.SetReadDeadline(time.Time{})
		_ = tunnel.WriteStatus(client, err)
		return
	}
	request, err := tunnel.ReadOpenBody(client, header.Command)
	if err != nil {
		s.logf("reject open from %s: %v", client.RemoteAddr(), err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.DialTimeout)
	defer cancel()
	targetAddress, err := resolvePrivate(ctx, request.Host, request.Port)
	if err != nil {
		_ = tunnel.WriteStatus(client, err)
		s.logf("deny %s: %v", request.Address(), err)
		return
	}
	if control != nil {
		inspected, matched, inspectErr := control.dialInspector(ctx, request, targetAddress)
		if matched && inspectErr == nil {
			defer inspected.Close()
			if err := tunnel.WriteStatus(client, nil); err != nil {
				return
			}
			relayTCP(client, inspected)
			return
		}
		if matched {
			s.logf(
				"Inspector fail-open for target %s: %v",
				request.Address(), inspectErr,
			)
		}
	}

	network := "tcp"
	if request.Command == tunnel.CommandUDP {
		network = "udp"
	}
	target, err := (&net.Dialer{}).DialContext(ctx, network, targetAddress)
	if err != nil {
		_ = tunnel.WriteStatus(client, fmt.Errorf("dial target: %w", err))
		return
	}
	defer target.Close()
	if err := tunnel.WriteStatus(client, nil); err != nil {
		return
	}

	if request.Command == tunnel.CommandUDP {
		s.relayUDP(client, target)
		return
	}
	relayTCP(client, target)
}

func (s *Server) handleAccept(client net.Conn, header tunnel.SessionHeader) {
	streamID, err := tunnel.ReadAcceptStreamID(client)
	if err != nil {
		_ = client.Close()
		return
	}
	pending := s.takePendingForSession(streamID, header)
	if pending == nil {
		_ = tunnel.WriteStatus(client, fmt.Errorf("unknown stream %d", streamID))
		_ = client.Close()
		return
	}
	if err := tunnel.WriteStatus(client, nil); err != nil {
		pending.close()
		_ = client.Close()
		return
	}
	pending.serve(client)
}

func (s *Server) authorizeSession(
	header tunnel.SessionHeader,
) (*controlSession, error) {
	if header.Version == tunnel.ProtocolV1 {
		return nil, nil
	}
	s.mu.Lock()
	control := s.controlsByToken[header.Token]
	s.mu.Unlock()
	if control == nil {
		return nil, errors.New("unknown or expired KCG2 session")
	}
	return control, nil
}

func (s *Server) handleInspectorEvents(client net.Conn, header tunnel.SessionHeader) {
	if header.Version != tunnel.ProtocolV2 {
		_ = tunnel.WriteStatus(client, errors.New("Inspector events require KCG2"))
		_ = client.Close()
		return
	}
	s.mu.Lock()
	control := s.controlsByToken[header.Token]
	s.mu.Unlock()
	if control == nil {
		_ = tunnel.WriteStatus(client, errors.New("unknown or expired KCG2 session"))
		_ = client.Close()
		return
	}
	if err := control.attachEvents(client); err != nil {
		_ = tunnel.WriteStatus(client, err)
		_ = client.Close()
		return
	}
	if err := tunnel.WriteStatus(client, nil); err != nil {
		control.detachEvents(client)
		_ = client.Close()
		return
	}
	s.logf("Inspector event channel connected")
	_, _ = io.Copy(io.Discard, client)
	control.detachEvents(client)
	_ = client.Close()
	s.logf("Inspector event channel disconnected")
}

func (s *Server) relayUDP(client, target net.Conn) {
	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(done)
			_ = target.Close()
			_ = client.Close()
		})
	}
	go func() {
		defer stop()
		reader := bufio.NewReader(client)
		var buffer []byte
		for {
			payload, err := tunnel.ReadDatagram(reader, buffer)
			if err != nil {
				return
			}
			buffer = payload[:0]
			if _, err := target.Write(payload); err != nil {
				return
			}
		}
	}()

	buffer := make([]byte, tunnel.MaxDatagramSize)
	for {
		read, err := target.Read(buffer)
		if err != nil {
			stop()
			<-done
			return
		}
		if err := tunnel.WriteDatagram(client, buffer[:read]); err != nil {
			stop()
			<-done
			return
		}
	}
}

func relayTCP(left, right net.Conn) {
	done := make(chan struct{}, 2)
	copyStream := func(destination, source net.Conn) {
		_, _ = io.Copy(destination, source)
		if value, ok := destination.(interface{ CloseWrite() error }); ok {
			_ = value.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyStream(left, right)
	go copyStream(right, left)
	<-done
}

func resolvePrivate(ctx context.Context, host string, port uint16) (string, error) {
	if strings.EqualFold(host, "localhost") {
		return "", errors.New("loopback targets are not allowed")
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return "", fmt.Errorf("resolve target: %w", err)
	}
	for _, address := range addresses {
		ip, ok := netip.AddrFromSlice(address.AsSlice())
		if !ok {
			continue
		}
		ip = ip.Unmap()
		if isClusterAddress(ip) {
			return net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port)), nil
		}
	}
	return "", fmt.Errorf("target %q does not resolve to a private cluster address", host)
}

func isClusterAddress(ip netip.Addr) bool {
	return ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsMulticast()
}

func (s *Server) logf(format string, arguments ...any) {
	if s.Logger != nil {
		s.Logger.Printf(format, arguments...)
	}
}
