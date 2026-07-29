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
	Logger      *log.Logger
	DialTimeout time.Duration

	mu         sync.Mutex
	nextStream atomic.Uint64
	controls   map[*controlSession]struct{}
	listeners  map[string]*interceptListener
	pending    map[uint64]*pendingStream
}

func NewServer(logger *log.Logger, dialTimeout time.Duration) *Server {
	if dialTimeout == 0 {
		dialTimeout = 10 * time.Second
	}
	return &Server{
		Logger:      logger,
		DialTimeout: dialTimeout,
		controls:    make(map[*controlSession]struct{}),
		listeners:   make(map[string]*interceptListener),
		pending:     make(map[uint64]*pendingStream),
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
	command, err := tunnel.ReadSessionHeader(client)
	if err != nil {
		_ = client.Close()
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			s.logf("reject handshake from %s: %v", client.RemoteAddr(), err)
		}
		return
	}
	_ = client.SetReadDeadline(time.Time{})

	switch command {
	case tunnel.CommandTCP, tunnel.CommandUDP:
		s.handleOutbound(client, command)
	case tunnel.CommandControl:
		s.handleControl(client)
	case tunnel.CommandAccept:
		s.handleAccept(client)
	default:
		_ = tunnel.WriteStatus(client, fmt.Errorf("unsupported command %d", command))
		_ = client.Close()
	}
}

func (s *Server) handleOutbound(client net.Conn, command byte) {
	defer client.Close()
	request, err := tunnel.ReadOpenBody(client, command)
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

func (s *Server) handleAccept(client net.Conn) {
	streamID, err := tunnel.ReadAcceptStreamID(client)
	if err != nil {
		_ = client.Close()
		return
	}
	pending := s.takePending(streamID)
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
