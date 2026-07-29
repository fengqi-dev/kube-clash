package portfwd

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

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
	RemotePort uint16 `json:"remotePort"`
	LocalPort  uint16 `json:"localPort"`
	Address    string `json:"address"`
}

type ClusterAPI interface {
	ResolveServiceBackend(context.Context, string, string, string, int32) (string, uint16, error)
	StartPodPortForward(context.Context, string, string, string, uint16, uint16) (cluster.PortForward, error)
}

type Manager struct {
	cluster ClusterAPI

	mu     sync.Mutex
	nextID atomic.Uint64
	active map[string]*runtimeForward
}

type runtimeForward struct {
	info      Info
	forwarder cluster.PortForward
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

	kind := request.Kind
	if kind == "" {
		kind = KindPod
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
		RemotePort: request.RemotePort,
		LocalPort:  localPort,
		Address:    forwarder.Address(),
	}

	m.mu.Lock()
	m.active[id] = &runtimeForward{info: info, forwarder: forwarder}
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
