package intercept

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	corev1 "k8s.io/api/core/v1"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

const (
	ModeExchange = "exchange"
	ModeMirror   = "mirror"
)

// primaryAddress picks the first ready Pod IP:targetPort for a Service port.
func primaryAddress(
	subsets []corev1.EndpointSubset,
	port cluster.InterceptPort,
) (string, error) {
	protocol := port.Protocol
	if protocol == "" {
		protocol = corev1.ProtocolTCP
	}
	for _, subset := range subsets {
		portNum, ok := matchEndpointPort(subset.Ports, port, protocol)
		if !ok {
			continue
		}
		for _, addr := range subset.Addresses {
			if addr.IP == "" {
				continue
			}
			return net.JoinHostPort(addr.IP, strconv.Itoa(int(portNum))), nil
		}
	}
	return "", fmt.Errorf(
		"no ready backend for service port %s/%d", port.Name, port.ServicePort,
	)
}

func matchEndpointPort(
	ports []corev1.EndpointPort,
	want cluster.InterceptPort,
	protocol corev1.Protocol,
) (int32, bool) {
	for _, port := range ports {
		portProtocol := port.Protocol
		if portProtocol == "" {
			portProtocol = corev1.ProtocolTCP
		}
		if portProtocol != protocol {
			continue
		}
		if want.Name != "" && port.Name == want.Name {
			return port.Port, true
		}
		if port.Port == want.ServicePort {
			return port.Port, true
		}
	}
	if len(ports) == 1 {
		portProtocol := ports[0].Protocol
		if portProtocol == "" {
			portProtocol = corev1.ProtocolTCP
		}
		if portProtocol == protocol {
			return ports[0].Port, true
		}
	}
	return 0, false
}

func buildPrimaryAddrs(
	snapshot cluster.ServiceInterceptSnapshot,
	ports []cluster.InterceptPort,
	portKeys map[string]PortMapping,
	interceptID string,
) (map[string]string, error) {
	if !snapshot.HasEndpoints || len(snapshot.EndpointsSubsets) == 0 {
		return nil, fmt.Errorf("service has no endpoints to mirror")
	}
	out := make(map[string]string, len(portKeys))
	for _, port := range ports {
		network := tunnelNetwork(port.Protocol)
		subID := fmt.Sprintf("%s:%s:%d", interceptID, networkName(network), port.ServicePort)
		if _, ok := portKeys[subID]; !ok {
			continue
		}
		addr, err := primaryAddress(snapshot.EndpointsSubsets, port)
		if err != nil {
			return nil, err
		}
		out[subID] = addr
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no mirror backends resolved")
	}
	return out, nil
}

func tunnelNetwork(protocol corev1.Protocol) byte {
	return protocolToNetwork(protocol)
}

func closeWrite(conn net.Conn) {
	if value, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = value.CloseWrite()
	}
}

// mirrorTCP forwards client traffic to primary (response path) and tees
// requests to local (responses discarded). local may be nil.
func mirrorTCP(client, primary net.Conn, local net.Conn) {
	defer client.Close()
	defer primary.Close()
	if local != nil {
		defer local.Close()
		go func() { _, _ = io.Copy(io.Discard, local) }()
	}

	done := make(chan struct{}, 2)
	go func() {
		dst := io.Writer(primary)
		if local != nil {
			dst = io.MultiWriter(primary, local)
		}
		_, _ = io.Copy(dst, client)
		closeWrite(primary)
		if local != nil {
			closeWrite(local)
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, primary)
		closeWrite(client)
		done <- struct{}{}
	}()
	<-done
}

// mirrorUDP forwards client datagrams to primary (response path) and tees
// requests to local (responses discarded). primaryFramed is true when primary
// is a Gateway UDP tunnel that uses length-prefixed datagrams.
func mirrorUDP(client, primary net.Conn, primaryFramed bool, local *net.UDPConn) {
	defer client.Close()
	defer primary.Close()
	if local != nil {
		defer local.Close()
		go func() {
			buf := make([]byte, tunnel.MaxDatagramSize)
			for {
				if _, err := local.Read(buf); err != nil {
					return
				}
			}
		}()
	}

	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }

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
			if err := writeMirrorUDP(primary, primaryFramed, payload); err != nil {
				return
			}
			if local != nil {
				_, _ = local.Write(payload)
			}
		}
	}()

	go func() {
		defer stop()
		if primaryFramed {
			reader := bufio.NewReader(primary)
			var buffer []byte
			for {
				payload, err := tunnel.ReadDatagram(reader, buffer)
				if err != nil {
					return
				}
				buffer = payload[:0]
				if err := tunnel.WriteDatagram(client, payload); err != nil {
					return
				}
			}
		}
		buf := make([]byte, tunnel.MaxDatagramSize)
		for {
			n, err := primary.Read(buf)
			if err != nil {
				return
			}
			if err := tunnel.WriteDatagram(client, buf[:n]); err != nil {
				return
			}
		}
	}()

	<-done
}

func writeMirrorUDP(primary net.Conn, framed bool, payload []byte) error {
	if framed {
		return tunnel.WriteDatagram(primary, payload)
	}
	_, err := primary.Write(payload)
	return err
}
