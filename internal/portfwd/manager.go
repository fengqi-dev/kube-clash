package portfwd

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

const (
	KindPod     = "pod"
	KindService = "service"
)

// Request starts a local API Server port-forward to a Pod or Service.
type Request struct {
	Context    string `json:"context"`
	Namespace  string `json:"namespace"`
	Kind       string `json:"kind"` // pod | service
	Name       string `json:"name"`
	Protocol   string `json:"protocol,omitempty"` // tcp | udp
	RemotePort uint16 `json:"remotePort"`
	LocalPort  uint16 `json:"localPort"` // 0 = allocate
}

// Info describes an active port-forward.
type Info struct {
	ID         string `json:"id"`
	Context    string `json:"context"`
	Namespace  string `json:"namespace"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	PodName    string `json:"podName"`
	Protocol   string `json:"protocol"`
	RemotePort uint16 `json:"remotePort"`
	LocalPort  uint16 `json:"localPort"`
	Address    string `json:"address"`
}

type ClusterAPI interface {
	ResolveServiceBackend(context.Context, string, string, string, int32) (string, uint16, error)
	StartPodPortForward(context.Context, string, string, string, uint16, uint16) (cluster.PortForward, error)
}

type routedTargetAPI interface {
	ListPods(context.Context, string, string) ([]cluster.PodInfo, error)
	ListServices(context.Context, string, string) ([]cluster.ServiceInfo, error)
}

type TrafficDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type Manager struct {
	cluster ClusterAPI

	mu             sync.Mutex
	nextID         atomic.Uint64
	active         map[string]*runtimeForward
	trafficDialer  TrafficDialer
	trafficContext string
}

func (m *Manager) SetTrafficDialer(contextName string, dialer TrafficDialer) {
	m.mu.Lock()
	m.trafficContext = contextName
	m.trafficDialer = dialer
	m.mu.Unlock()
}

type runtimeForward struct {
	info      Info
	forwarder cluster.PortForward
	routed    bool
}

func NewManager(api ClusterAPI) *Manager {
	return &Manager{
		cluster: api,
		active:  make(map[string]*runtimeForward),
	}
}

func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Info, 0, len(m.active))
	for _, item := range m.active {
		items = append(items, item.info)
	}
	return items
}

func (m *Manager) Start(ctx context.Context, request Request) (Info, error) {
	if request.Context == "" {
		return Info{}, fmt.Errorf("context is required")
	}
	if request.Namespace == "" {
		request.Namespace = "default"
	}
	if request.Name == "" {
		return Info{}, fmt.Errorf("target name is required")
	}
	if request.RemotePort == 0 {
		return Info{}, fmt.Errorf("remote port is required")
	}
	request.Protocol = strings.ToLower(strings.TrimSpace(request.Protocol))
	if request.Protocol == "" {
		request.Protocol = "tcp"
	}
	if request.Protocol != "tcp" && request.Protocol != "udp" {
		return Info{}, fmt.Errorf("unsupported protocol %q", request.Protocol)
	}

	kind := request.Kind
	if kind == "" {
		kind = KindPod
	}
	request.Kind = kind
	m.mu.Lock()
	trafficDialer := m.trafficDialer
	trafficContext := m.trafficContext
	m.mu.Unlock()
	if trafficDialer != nil && request.Context == trafficContext {
		target, err := m.resolveRoutedTarget(ctx, request)
		if err != nil {
			return Info{}, err
		}
		return m.startRouted(request, target, trafficDialer)
	}
	if request.Protocol == "udp" {
		return Info{}, fmt.Errorf("UDP port-forward requires an active KubeLoop session")
	}

	podName := request.Name
	remotePort := request.RemotePort
	switch kind {
	case KindPod:
	case KindService:
		resolvedPod, targetPort, err := m.cluster.ResolveServiceBackend(
			ctx, request.Context, request.Namespace, request.Name, int32(request.RemotePort),
		)
		if err != nil {
			return Info{}, err
		}
		podName = resolvedPod
		remotePort = targetPort
	default:
		return Info{}, fmt.Errorf("unsupported kind %q", kind)
	}

	forwarder, err := m.cluster.StartPodPortForward(
		ctx, request.Context, request.Namespace, podName, request.LocalPort, remotePort,
	)
	if err != nil {
		return Info{}, err
	}

	localPort, err := localPortFromAddress(forwarder.Address())
	if err != nil {
		_ = forwarder.Close()
		return Info{}, err
	}

	id := fmt.Sprintf("pf-%d", m.nextID.Add(1))
	info := Info{
		ID:         id,
		Context:    request.Context,
		Namespace:  request.Namespace,
		Kind:       kind,
		Name:       request.Name,
		PodName:    podName,
		Protocol:   request.Protocol,
		RemotePort: request.RemotePort,
		LocalPort:  localPort,
		Address:    forwarder.Address(),
	}

	m.mu.Lock()
	m.active[id] = &runtimeForward{info: info, forwarder: forwarder}
	m.mu.Unlock()
	return info, nil
}

func (m *Manager) resolveRoutedTarget(ctx context.Context, request Request) (string, error) {
	api, ok := m.cluster.(routedTargetAPI)
	if !ok {
		return "", fmt.Errorf("cluster provider does not support routed port-forward targets")
	}
	switch request.Kind {
	case KindPod:
		pods, err := api.ListPods(ctx, request.Context, request.Namespace)
		if err != nil {
			return "", err
		}
		for _, pod := range pods {
			if pod.Namespace == request.Namespace && pod.Name == request.Name {
				if pod.IP == "" {
					return "", fmt.Errorf("pod %s/%s has no IP", request.Namespace, request.Name)
				}
				return net.JoinHostPort(pod.IP, strconv.Itoa(int(request.RemotePort))), nil
			}
		}
		return "", fmt.Errorf("pod %s/%s not found", request.Namespace, request.Name)
	case KindService:
		services, err := api.ListServices(ctx, request.Context, request.Namespace)
		if err != nil {
			return "", err
		}
		for _, service := range services {
			if service.Namespace == request.Namespace && service.Name == request.Name {
				if service.ClusterIP == "" {
					return "", fmt.Errorf("service %s/%s has no ClusterIP", request.Namespace, request.Name)
				}
				return net.JoinHostPort(
					service.ClusterIP, strconv.Itoa(int(request.RemotePort)),
				), nil
			}
		}
		return "", fmt.Errorf("service %s/%s not found", request.Namespace, request.Name)
	default:
		return "", fmt.Errorf("unsupported kind %q", request.Kind)
	}
}

func (m *Manager) startRouted(
	request Request, target string, dialer TrafficDialer,
) (Info, error) {
	listenAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(request.LocalPort)))
	var forwarder cluster.PortForward
	if request.Protocol == "udp" {
		address, err := net.ResolveUDPAddr("udp", listenAddress)
		if err != nil {
			return Info{}, fmt.Errorf("resolve UDP port-forward listener: %w", err)
		}
		socket, err := net.ListenUDP("udp", address)
		if err != nil {
			return Info{}, fmt.Errorf("listen for UDP port-forward: %w", err)
		}
		forwarder = newRoutedUDPForwarder(socket, target, dialer)
	} else {
		listener, err := net.Listen("tcp", listenAddress)
		if err != nil {
			return Info{}, fmt.Errorf("listen for port-forward: %w", err)
		}
		forwarder = newRoutedForwarder(listener, target, dialer)
	}
	localPort, err := localPortFromAddress(forwarder.Address())
	if err != nil {
		_ = forwarder.Close()
		return Info{}, err
	}
	id := fmt.Sprintf("pf-%d", m.nextID.Add(1))
	info := Info{
		ID:         id,
		Context:    request.Context,
		Namespace:  request.Namespace,
		Kind:       request.Kind,
		Name:       request.Name,
		Protocol:   request.Protocol,
		RemotePort: request.RemotePort,
		LocalPort:  localPort,
		Address:    forwarder.Address(),
	}
	m.mu.Lock()
	m.active[id] = &runtimeForward{info: info, forwarder: forwarder, routed: true}
	m.mu.Unlock()
	return info, nil
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	runtime := m.active[id]
	delete(m.active, id)
	m.mu.Unlock()
	if runtime == nil {
		return fmt.Errorf("port-forward %q not found", id)
	}
	return runtime.forwarder.Close()
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	items := make([]*runtimeForward, 0, len(m.active))
	for id, item := range m.active {
		items = append(items, item)
		delete(m.active, id)
	}
	m.mu.Unlock()
	for _, item := range items {
		_ = item.forwarder.Close()
	}
}

func (m *Manager) StopRouted() {
	m.mu.Lock()
	items := make([]*runtimeForward, 0)
	for id, item := range m.active {
		if !item.routed {
			continue
		}
		items = append(items, item)
		delete(m.active, id)
	}
	m.mu.Unlock()
	for _, item := range items {
		_ = item.forwarder.Close()
	}
}

func localPortFromAddress(address string) (uint16, error) {
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		return 0, fmt.Errorf("parse forward address: %w", err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return 0, fmt.Errorf("invalid local port %q", portText)
	}
	return uint16(port), nil
}

type routedForwarder struct {
	listener net.Listener
	target   string
	dialer   TrafficDialer
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	once     sync.Once
	connMu   sync.Mutex
	conns    map[net.Conn]struct{}
}

func newRoutedForwarder(
	listener net.Listener, target string, dialer TrafficDialer,
) *routedForwarder {
	ctx, cancel := context.WithCancel(context.Background())
	forwarder := &routedForwarder{
		listener: listener, target: target, dialer: dialer, ctx: ctx, cancel: cancel,
		conns: make(map[net.Conn]struct{}),
	}
	forwarder.wg.Add(1)
	go forwarder.serve()
	return forwarder
}

func (f *routedForwarder) Address() string { return f.listener.Addr().String() }

func (f *routedForwarder) Close() error {
	var err error
	f.once.Do(func() {
		f.cancel()
		err = f.listener.Close()
		f.connMu.Lock()
		for conn := range f.conns {
			_ = conn.Close()
		}
		f.connMu.Unlock()
		f.wg.Wait()
	})
	return err
}

func (f *routedForwarder) serve() {
	defer f.wg.Done()
	for {
		client, err := f.listener.Accept()
		if err != nil {
			return
		}
		if !f.track(client) {
			continue
		}
		f.wg.Go(func() {
			defer f.untrack(client)
			f.forward(client)
		})
	}
}

func (f *routedForwarder) forward(client net.Conn) {
	defer client.Close()
	target, err := f.dialer.DialContext(f.ctx, "tcp", f.target)
	if err != nil {
		return
	}
	if !f.track(target) {
		return
	}
	defer f.untrack(target)
	defer target.Close()
	done := make(chan struct{}, 2)
	copyStream := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if value, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = value.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyStream(target, client)
	go copyStream(client, target)
	<-done
}

func (f *routedForwarder) track(conn net.Conn) bool {
	f.connMu.Lock()
	defer f.connMu.Unlock()
	if f.ctx.Err() != nil {
		_ = conn.Close()
		return false
	}
	f.conns[conn] = struct{}{}
	return true
}

func (f *routedForwarder) untrack(conn net.Conn) {
	f.connMu.Lock()
	delete(f.conns, conn)
	f.connMu.Unlock()
}

type routedUDPForwarder struct {
	socket       *net.UDPConn
	target       string
	dialer       TrafficDialer
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	associations map[string]*udpAssociation
	wg           sync.WaitGroup
	once         sync.Once
}

type udpAssociation struct {
	upstream net.Conn
	client   *net.UDPAddr
}

func newRoutedUDPForwarder(
	socket *net.UDPConn, target string, dialer TrafficDialer,
) *routedUDPForwarder {
	ctx, cancel := context.WithCancel(context.Background())
	forwarder := &routedUDPForwarder{
		socket: socket, target: target, dialer: dialer, ctx: ctx, cancel: cancel,
		associations: make(map[string]*udpAssociation),
	}
	forwarder.wg.Add(1)
	go forwarder.serve()
	return forwarder
}

func (f *routedUDPForwarder) Address() string { return f.socket.LocalAddr().String() }

func (f *routedUDPForwarder) Close() error {
	var err error
	f.once.Do(func() {
		f.cancel()
		err = f.socket.Close()
		f.mu.Lock()
		items := make([]*udpAssociation, 0, len(f.associations))
		for key, item := range f.associations {
			items = append(items, item)
			delete(f.associations, key)
		}
		f.mu.Unlock()
		for _, item := range items {
			_ = item.upstream.Close()
		}
		f.wg.Wait()
	})
	return err
}

func (f *routedUDPForwarder) serve() {
	defer f.wg.Done()
	buffer := make([]byte, 65535)
	for {
		n, client, err := f.socket.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		item, err := f.association(client)
		if err != nil {
			continue
		}
		if _, err := item.upstream.Write(buffer[:n]); err != nil {
			f.removeAssociation(client.String(), item)
		}
	}
}

func (f *routedUDPForwarder) association(client *net.UDPAddr) (*udpAssociation, error) {
	key := client.String()
	f.mu.Lock()
	if item := f.associations[key]; item != nil {
		f.mu.Unlock()
		return item, nil
	}
	f.mu.Unlock()

	upstream, err := f.dialer.DialContext(f.ctx, "udp", f.target)
	if err != nil {
		return nil, err
	}
	item := &udpAssociation{upstream: upstream, client: client}
	f.mu.Lock()
	if f.ctx.Err() != nil {
		f.mu.Unlock()
		_ = upstream.Close()
		return nil, context.Canceled
	}
	if existing := f.associations[key]; existing != nil {
		f.mu.Unlock()
		_ = upstream.Close()
		return existing, nil
	}
	f.associations[key] = item
	f.mu.Unlock()
	f.wg.Add(1)
	go f.readReplies(key, item)
	return item, nil
}

func (f *routedUDPForwarder) readReplies(key string, item *udpAssociation) {
	defer f.wg.Done()
	defer f.removeAssociation(key, item)
	buffer := make([]byte, 65535)
	for {
		_ = item.upstream.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, err := item.upstream.Read(buffer)
		if err != nil {
			return
		}
		if _, err := f.socket.WriteToUDP(buffer[:n], item.client); err != nil {
			return
		}
	}
}

func (f *routedUDPForwarder) removeAssociation(key string, item *udpAssociation) {
	f.mu.Lock()
	if f.associations[key] == item {
		delete(f.associations, key)
	}
	f.mu.Unlock()
	_ = item.upstream.Close()
}
