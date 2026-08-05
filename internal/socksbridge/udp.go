package socksbridge

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"sync"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

type udpAssociation struct {
	server   *Server
	listener *net.UDPConn
	mu       sync.Mutex
	client   *net.UDPAddr
	tunnels  map[string]*udpTunnel
	closed   bool
}

type udpTunnel struct {
	connection net.Conn
	host       string
	port       uint16
	// framed is true for Gateway UDP tunnels (length-prefixed datagrams).
	// HostUDP bypass connections use raw Read/Write payloads.
	framed  bool
	writeMu sync.Mutex
}

func (a *udpAssociation) serve() {
	buffer := make([]byte, tunnel.MaxDatagramSize+512)
	for {
		read, client, err := a.listener.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		host, port, payload, err := parseUDPPacket(buffer[:read])
		if err != nil {
			continue
		}
		a.mu.Lock()
		a.client = client
		item := a.tunnels[net.JoinHostPort(host, strconv.Itoa(int(port)))]
		a.mu.Unlock()
		if item == nil {
			item, err = a.newTunnel(host, port)
			if err != nil {
				continue
			}
		}
		item.writeMu.Lock()
		if item.framed {
			err = tunnel.WriteDatagram(item.connection, payload)
		} else {
			_, err = item.connection.Write(payload)
		}
		item.writeMu.Unlock()
		if err != nil {
			a.removeTunnel(item)
		}
	}
}

func (a *udpAssociation) newTunnel(host string, port uint16) (*udpTunnel, error) {
	connection, framed, err := a.openUDP(host, port)
	if err != nil {
		return nil, err
	}
	item := &udpTunnel{connection: connection, host: host, port: port, framed: framed}
	key := net.JoinHostPort(host, strconv.Itoa(int(port)))
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		connection.Close()
		return nil, net.ErrClosed
	}
	if existing := a.tunnels[key]; existing != nil {
		a.mu.Unlock()
		connection.Close()
		return existing, nil
	}
	a.tunnels[key] = item
	a.mu.Unlock()
	go a.readReplies(item)
	return item, nil
}

func (a *udpAssociation) openUDP(host string, port uint16) (net.Conn, bool, error) {
	if a.server.HostUDP != nil {
		if dial, ok := a.server.HostUDP(host, port); ok && dial != nil {
			conn, err := dial(context.Background())
			if err != nil {
				return nil, false, err
			}
			return conn, false, nil
		}
	}
	conn, err := a.server.openGateway(tunnel.CommandUDP, host, port)
	if err != nil {
		return nil, false, err
	}
	return conn, true, nil
}

func (a *udpAssociation) readReplies(item *udpTunnel) {
	if item.framed {
		a.readFramedReplies(item)
		return
	}
	buffer := make([]byte, tunnel.MaxDatagramSize)
	for {
		n, err := item.connection.Read(buffer)
		if err != nil {
			a.removeTunnel(item)
			return
		}
		packet, err := encodeUDPPacket(item.host, item.port, buffer[:n])
		if err != nil {
			continue
		}
		a.mu.Lock()
		client := a.client
		a.mu.Unlock()
		if client != nil {
			_, _ = a.listener.WriteToUDP(packet, client)
		}
	}
}

func (a *udpAssociation) readFramedReplies(item *udpTunnel) {
	reader := bufio.NewReader(item.connection)
	var buffer []byte
	for {
		payload, err := tunnel.ReadDatagram(reader, buffer)
		if err != nil {
			a.removeTunnel(item)
			return
		}
		buffer = payload[:0]
		packet, err := encodeUDPPacket(item.host, item.port, payload)
		if err != nil {
			continue
		}
		a.mu.Lock()
		client := a.client
		a.mu.Unlock()
		if client != nil {
			_, _ = a.listener.WriteToUDP(packet, client)
		}
	}
}

func (a *udpAssociation) removeTunnel(item *udpTunnel) {
	key := net.JoinHostPort(item.host, strconv.Itoa(int(item.port)))
	a.mu.Lock()
	if a.tunnels[key] == item {
		delete(a.tunnels, key)
	}
	a.mu.Unlock()
	item.connection.Close()
}

func (a *udpAssociation) close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	items := make([]*udpTunnel, 0, len(a.tunnels))
	for _, item := range a.tunnels {
		items = append(items, item)
	}
	a.tunnels = make(map[string]*udpTunnel)
	a.mu.Unlock()
	a.listener.Close()
	for _, item := range items {
		item.connection.Close()
	}
}
