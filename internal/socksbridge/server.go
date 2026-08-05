package socksbridge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

const (
	socksVersion  = 5
	methodNone    = 0
	commandTCP    = 1
	commandUDP    = 3
	addressIPv4   = 1
	addressDomain = 3
	addressIPv6   = 4
)

// HostTCPHandler claims intercepted Service destinations on the host TUN path.
// When ok is true, the bridge writes the SOCKS success reply then calls serve.
type HostTCPHandler func(host string, port uint16) (serve func(net.Conn), ok bool)

// HostUDPHandler claims intercepted UDP destinations on the host TUN path.
// dial opens a connection that exchanges raw datagram payloads via Read/Write
// (not tunnel length-prefix framing).
type HostUDPHandler func(host string, port uint16) (dial func(context.Context) (net.Conn, error), ok bool)

type Server struct {
	GatewayAddress string
	DialTimeout    time.Duration
	HostTCP        HostTCPHandler
	HostUDP        HostUDPHandler

	gatewayMu sync.RWMutex
}

// Bridge is the local SOCKS listener used by sing-box's kubernetes outbound.
type Bridge struct {
	net.Listener
	server *Server
}

func (b *Bridge) SetHostTCPHandler(handler HostTCPHandler) {
	b.server.HostTCP = handler
}

func (b *Bridge) SetHostUDPHandler(handler HostUDPHandler) {
	b.server.HostUDP = handler
}

// SetGatewayAddress switches new SOCKS requests to a replacement Kubernetes
// API port-forward without interrupting the local sing-box listener.
func (b *Bridge) SetGatewayAddress(address string) {
	b.server.gatewayMu.Lock()
	b.server.GatewayAddress = address
	b.server.gatewayMu.Unlock()
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

func (s *Server) handle(control net.Conn) {
	defer control.Close()
	reader := bufio.NewReader(control)
	if err := negotiate(reader, control); err != nil {
		return
	}
	command, host, port, err := readRequest(reader)
	if err != nil {
		_ = writeReply(control, 1, nil)
		return
	}
	switch command {
	case commandTCP:
		s.handleTCP(control, host, port)
	case commandUDP:
		s.handleUDP(control)
	default:
		_ = writeReply(control, 7, nil)
	}
}

func (s *Server) handleTCP(client net.Conn, host string, port uint16) {
	if s.HostTCP != nil {
		if serve, ok := s.HostTCP(host, port); ok && serve != nil {
			if err := writeReply(client, 0, client.LocalAddr()); err != nil {
				return
			}
			serve(client)
			return
		}
	}
	gateway, err := s.openGateway(tunnel.CommandTCP, host, port)
	if err != nil {
		_ = writeReply(client, 5, nil)
		return
	}
	defer gateway.Close()
	if err := writeReply(client, 0, client.LocalAddr()); err != nil {
		return
	}
	relay(client, gateway)
}

func (s *Server) handleUDP(control net.Conn) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		_ = writeReply(control, 1, nil)
		return
	}
	defer listener.Close()
	if err := writeReply(control, 0, listener.LocalAddr()); err != nil {
		return
	}
	association := &udpAssociation{
		server: s, listener: listener, tunnels: make(map[string]*udpTunnel),
	}
	go association.serve()
	_, _ = io.Copy(io.Discard, control)
	association.close()
}

func (s *Server) openGateway(command byte, host string, port uint16) (net.Conn, error) {
	timeout := s.DialTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	s.gatewayMu.RLock()
	gatewayAddress := s.GatewayAddress
	s.gatewayMu.RUnlock()
	connection, err := net.DialTimeout("tcp", gatewayAddress, timeout)
	if err != nil {
		return nil, fmt.Errorf("connect gateway: %w", err)
	}
	if err := tunnel.WriteOpen(connection, tunnel.OpenRequest{
		Command: command, Host: host, Port: port,
	}); err != nil {
		connection.Close()
		return nil, err
	}
	if err := tunnel.ReadStatus(connection); err != nil {
		connection.Close()
		return nil, err
	}
	return connection, nil
}

func Listen(ctx context.Context, gatewayAddress, listenAddress string) (*Bridge, error) {
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return nil, err
	}
	server := &Server{GatewayAddress: gatewayAddress}
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	go func() { _ = server.Serve(listener) }()
	return &Bridge{Listener: listener, server: server}, nil
}
