package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const currentVersion = 1

// State is the on-disk multi-cluster client state.
type State struct {
	Version  int                      `json:"version"`
	UI       UIState                  `json:"ui"`
	Clusters map[string]*ClusterState `json:"clusters"`
}

// UIState remembers the last selected context in the desktop UI.
type UIState struct {
	LastContext     string   `json:"lastContext,omitempty"`
	LastNamespace   string   `json:"lastNamespace,omitempty"`
	KubeconfigFiles []string `json:"kubeconfigFiles,omitempty"` // absolute paths, user-added
}

// ClusterState stores restore intents for one kubeconfig context.
type ClusterState struct {
	Namespace     string            `json:"namespace,omitempty"`
	Connected     bool              `json:"connected,omitempty"`
	PortForwards  []PortForwardSpec `json:"portForwards,omitempty"`
	Exchanges     []ExchangeSpec    `json:"exchanges,omitempty"`
	Mirrors       []MirrorSpec      `json:"mirrors,omitempty"`
	Previews      []PreviewSpec     `json:"previews,omitempty"`
	HostAliases   []HostAliasSpec   `json:"hostAliases,omitempty"`
	ManualNetwork *ManualNetwork    `json:"manualNetwork,omitempty"`
}

// HostAliasSpec maps a DNS name to an IPv4 address for the local tunnel DNS.
type HostAliasSpec struct {
	Domain string `json:"domain"`
	IP     string `json:"ip"`
}

// ManualNetwork is user-supplied Pod/Service CIDR and CoreDNS when auto-discovery fails.
type ManualNetwork struct {
	PodCIDRs     []string `json:"podCIDRs,omitempty"`
	ServiceCIDRs []string `json:"serviceCIDRs,omitempty"`
	DNSServer    string   `json:"dnsServer,omitempty"`
}

// PortForwardSpec is a port-forward intent (not runtime listen state).
type PortForwardSpec struct {
	Namespace  string `json:"namespace"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	RemotePort uint16 `json:"remotePort"`
	LocalPort  uint16 `json:"localPort,omitempty"`
}

// PortMapping maps a service/local port pair.
type PortMapping struct {
	ServicePort int32  `json:"servicePort"`
	Protocol    string `json:"protocol"`
	LocalHost   string `json:"localHost"`
	LocalPort   int    `json:"localPort"`
}

// ExchangeSpec replaces an existing Service with a local process.
type ExchangeSpec struct {
	Namespace string        `json:"namespace"`
	Service   string        `json:"service"`
	Ports     []PortMapping `json:"ports"`
}

// MirrorSpec hijacks a Service through the Gateway while keeping cluster
// Pods as the primary response path and teeing requests to a local process.
type MirrorSpec struct {
	Namespace string        `json:"namespace"`
	Service   string        `json:"service"`
	Ports     []PortMapping `json:"ports"`
}

// PreviewSpec exposes a local process as a new ClusterIP Service.
type PreviewSpec struct {
	Namespace string        `json:"namespace"`
	Name      string        `json:"name"`
	Ports     []PortMapping `json:"ports"`
}

// Store persists State as a JSON file.
type Store struct {
	path string

	mu    sync.Mutex
	state State
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find user home directory: %w", err)
	}
	return filepath.Join(home, ".kubeloop", "state.json"), nil
}

func Open(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	store := &Store{
		path: path,
		state: State{
			Version:  currentVersion,
			Clusters: map[string]*ClusterState{},
		},
	}
	if err := store.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return store, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("decode state: %w", err)
	}
	if state.Clusters == nil {
		state.Clusters = map[string]*ClusterState{}
	}
	if state.Version == 0 {
		state.Version = currentVersion
	}
	s.state = state
	return nil
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.state)
}

func (s *Store) SetUI(contextName, namespace string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.UI.LastContext = contextName
	s.state.UI.LastNamespace = namespace
	if contextName != "" {
		cluster := s.ensureClusterLocked(contextName)
		if namespace != "" {
			cluster.Namespace = namespace
		}
	}
	return s.saveLocked()
}

// KubeconfigFiles returns a copy of user-added kubeconfig paths.
func (s *Store) KubeconfigFiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneStrings(s.state.UI.KubeconfigFiles)
}

// AddKubeconfigFile appends an absolute kubeconfig path if not already present.
func (s *Store) AddKubeconfigFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := normalizeStorePath(path)
	if err != nil {
		return err
	}
	for _, existing := range s.state.UI.KubeconfigFiles {
		if existing == path {
			return nil
		}
	}
	s.state.UI.KubeconfigFiles = append(s.state.UI.KubeconfigFiles, path)
	return s.saveLocked()
}

// RemoveKubeconfigFile removes a user-added kubeconfig path.
func (s *Store) RemoveKubeconfigFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := normalizeStorePath(path)
	if err != nil {
		return err
	}
	files := s.state.UI.KubeconfigFiles
	next := make([]string, 0, len(files))
	for _, existing := range files {
		if existing != path {
			next = append(next, existing)
		}
	}
	if len(next) == len(files) {
		return fmt.Errorf("kubeconfig file not found: %s", path)
	}
	if len(next) == 0 {
		s.state.UI.KubeconfigFiles = nil
	} else {
		s.state.UI.KubeconfigFiles = next
	}
	return s.saveLocked()
}

func (s *Store) SetConnected(contextName, namespace string, connected bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if contextName == "" {
		return nil
	}
	s.state.UI.LastContext = contextName
	if namespace != "" {
		s.state.UI.LastNamespace = namespace
	}
	cluster := s.ensureClusterLocked(contextName)
	cluster.Connected = connected
	if namespace != "" {
		cluster.Namespace = namespace
	}
	// Only one context may auto-reconnect.
	if connected {
		for name, item := range s.state.Clusters {
			if name != contextName {
				item.Connected = false
			}
		}
	}
	return s.saveLocked()
}

func (s *Store) SetPortForwards(contextName string, items []PortForwardSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if contextName == "" {
		return nil
	}
	cluster := s.ensureClusterLocked(contextName)
	cluster.PortForwards = clonePortForwards(items)
	return s.saveLocked()
}

func (s *Store) SetExchanges(contextName string, items []ExchangeSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if contextName == "" {
		return nil
	}
	cluster := s.ensureClusterLocked(contextName)
	cluster.Exchanges = cloneExchanges(items)
	return s.saveLocked()
}

func (s *Store) SetMirrors(contextName string, items []MirrorSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if contextName == "" {
		return nil
	}
	cluster := s.ensureClusterLocked(contextName)
	cluster.Mirrors = cloneMirrors(items)
	return s.saveLocked()
}

func (s *Store) SetPreviews(contextName string, items []PreviewSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if contextName == "" {
		return nil
	}
	cluster := s.ensureClusterLocked(contextName)
	cluster.Previews = clonePreviews(items)
	return s.saveLocked()
}

func (s *Store) HostAliases(contextName string) []HostAliasSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.state.Clusters[contextName]
	if item == nil {
		return nil
	}
	return cloneHostAliases(item.HostAliases)
}

// SetHostAliases replaces host aliases for a context.
// An empty or nil list clears the stored configuration.
func (s *Store) SetHostAliases(contextName string, items []HostAliasSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if contextName == "" {
		return nil
	}
	cluster := s.ensureClusterLocked(contextName)
	if len(items) == 0 {
		cluster.HostAliases = nil
	} else {
		cluster.HostAliases = cloneHostAliases(items)
	}
	return s.saveLocked()
}

func (s *Store) ManualNetwork(contextName string) ManualNetwork {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.state.Clusters[contextName]
	if item == nil || item.ManualNetwork == nil {
		return ManualNetwork{}
	}
	return cloneManualNetwork(*item.ManualNetwork)
}

func (s *Store) SetManualNetwork(contextName string, network ManualNetwork) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if contextName == "" {
		return nil
	}
	cluster := s.ensureClusterLocked(contextName)
	if len(network.PodCIDRs) == 0 && len(network.ServiceCIDRs) == 0 && network.DNSServer == "" {
		cluster.ManualNetwork = nil
	} else {
		copyItem := cloneManualNetwork(network)
		cluster.ManualNetwork = &copyItem
	}
	return s.saveLocked()
}

func (s *Store) Cluster(contextName string) ClusterState {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.state.Clusters[contextName]
	if item == nil {
		return ClusterState{}
	}
	return cloneCluster(*item)
}

func (s *Store) ensureClusterLocked(contextName string) *ClusterState {
	if s.state.Clusters == nil {
		s.state.Clusters = map[string]*ClusterState{}
	}
	item := s.state.Clusters[contextName]
	if item == nil {
		item = &ClusterState{}
		s.state.Clusters[contextName] = item
	}
	return item
}

func (s *Store) saveLocked() error {
	s.state.Version = currentVersion
	if s.state.Clusters == nil {
		s.state.Clusters = map[string]*ClusterState{}
	}
	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, raw, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(temp, s.path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

func cloneState(state State) State {
	out := State{
		Version: state.Version,
		UI: UIState{
			LastContext:     state.UI.LastContext,
			LastNamespace:   state.UI.LastNamespace,
			KubeconfigFiles: cloneStrings(state.UI.KubeconfigFiles),
		},
		Clusters: make(map[string]*ClusterState, len(state.Clusters)),
	}
	for name, item := range state.Clusters {
		if item == nil {
			continue
		}
		copyItem := cloneCluster(*item)
		out.Clusters[name] = &copyItem
	}
	return out
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	copy(out, items)
	return out
}

func normalizeStorePath(path string) (string, error) {
	path = filepath.Clean(path)
	if path == "" || path == "." {
		return "", fmt.Errorf("kubeconfig path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve kubeconfig path: %w", err)
	}
	return abs, nil
}

func cloneCluster(item ClusterState) ClusterState {
	out := ClusterState{
		Namespace:    item.Namespace,
		Connected:    item.Connected,
		PortForwards: clonePortForwards(item.PortForwards),
		Exchanges:    cloneExchanges(item.Exchanges),
		Previews:     clonePreviews(item.Previews),
		HostAliases:  cloneHostAliases(item.HostAliases),
	}
	if item.ManualNetwork != nil {
		copyItem := cloneManualNetwork(*item.ManualNetwork)
		out.ManualNetwork = &copyItem
	}
	return out
}

func cloneHostAliases(items []HostAliasSpec) []HostAliasSpec {
	if len(items) == 0 {
		return nil
	}
	out := make([]HostAliasSpec, len(items))
	copy(out, items)
	return out
}

func cloneManualNetwork(item ManualNetwork) ManualNetwork {
	return ManualNetwork{
		PodCIDRs:     cloneStrings(item.PodCIDRs),
		ServiceCIDRs: cloneStrings(item.ServiceCIDRs),
		DNSServer:    item.DNSServer,
	}
}

func clonePortForwards(items []PortForwardSpec) []PortForwardSpec {
	if len(items) == 0 {
		return nil
	}
	out := make([]PortForwardSpec, len(items))
	copy(out, items)
	return out
}

func cloneExchanges(items []ExchangeSpec) []ExchangeSpec {
	if len(items) == 0 {
		return nil
	}
	out := make([]ExchangeSpec, len(items))
	for i, item := range items {
		out[i] = ExchangeSpec{
			Namespace: item.Namespace,
			Service:   item.Service,
			Ports:     clonePortMappings(item.Ports),
		}
	}
	return out
}

func cloneMirrors(items []MirrorSpec) []MirrorSpec {
	if len(items) == 0 {
		return nil
	}
	out := make([]MirrorSpec, len(items))
	for i, item := range items {
		out[i] = MirrorSpec{
			Namespace: item.Namespace,
			Service:   item.Service,
			Ports:     clonePortMappings(item.Ports),
		}
	}
	return out
}

func clonePreviews(items []PreviewSpec) []PreviewSpec {
	if len(items) == 0 {
		return nil
	}
	out := make([]PreviewSpec, len(items))
	for i, item := range items {
		out[i] = PreviewSpec{
			Namespace: item.Namespace,
			Name:      item.Name,
			Ports:     clonePortMappings(item.Ports),
		}
	}
	return out
}

func clonePortMappings(items []PortMapping) []PortMapping {
	if len(items) == 0 {
		return nil
	}
	out := make([]PortMapping, len(items))
	copy(out, items)
	return out
}
