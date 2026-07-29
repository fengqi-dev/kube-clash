package session

import (
	"context"
	"log"

	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/store"
)

func WithStore(stateStore *store.Store) Option {
	return func(manager *Manager) { manager.store = stateStore }
}

func (m *Manager) Store() *store.Store { return m.store }

func (m *Manager) RememberSelection(contextName, namespace string) error {
	if m.store == nil {
		return nil
	}
	return m.store.SetUI(contextName, namespace)
}

func (m *Manager) PreferredSelection() (contextName, namespace string) {
	if m.store == nil {
		return "", ""
	}
	ui := m.store.Snapshot().UI
	return ui.LastContext, ui.LastNamespace
}

func (m *Manager) persistPortForwards() {
	if m.store == nil {
		return
	}
	grouped := map[string][]store.PortForwardSpec{}
	for _, item := range m.portfwd.List() {
		grouped[item.Context] = append(grouped[item.Context], store.PortForwardSpec{
			Namespace:  item.Namespace,
			Kind:       item.Kind,
			Name:       item.Name,
			RemotePort: item.RemotePort,
			LocalPort:  item.LocalPort,
		})
	}
	snap := m.store.Snapshot()
	for name, cluster := range snap.Clusters {
		if cluster == nil {
			continue
		}
		if _, ok := grouped[name]; !ok && len(cluster.PortForwards) > 0 {
			grouped[name] = nil
		}
	}
	for contextName, items := range grouped {
		if err := m.store.SetPortForwards(contextName, items); err != nil {
			log.Printf("persist port-forwards for %s: %v", contextName, err)
		}
	}
}

func (m *Manager) persistExchanges(contextName string) {
	if m.store == nil || contextName == "" {
		return
	}
	items := m.intercept.List()
	specs := make([]store.ExchangeSpec, 0, len(items))
	for _, item := range items {
		specs = append(specs, store.ExchangeSpec{
			Namespace: item.Namespace,
			Service:   item.Service,
			Ports:     toStorePorts(item.Locals),
		})
	}
	if err := m.store.SetExchanges(contextName, specs); err != nil {
		log.Printf("persist exchanges for %s: %v", contextName, err)
	}
}

func (m *Manager) persistMirrors(contextName string) {
	if m.store == nil || contextName == "" {
		return
	}
	items := m.intercept.ListMirrors()
	specs := make([]store.MirrorSpec, 0, len(items))
	for _, item := range items {
		specs = append(specs, store.MirrorSpec{
			Namespace: item.Namespace,
			Service:   item.Service,
			Ports:     toStorePorts(item.Locals),
		})
	}
	if err := m.store.SetMirrors(contextName, specs); err != nil {
		log.Printf("persist mirrors for %s: %v", contextName, err)
	}
}

func (m *Manager) persistPreviews(contextName string) {
	if m.store == nil || contextName == "" {
		return
	}
	items := m.intercept.ListPreviews()
	specs := make([]store.PreviewSpec, 0, len(items))
	for _, item := range items {
		specs = append(specs, store.PreviewSpec{
			Namespace: item.Namespace,
			Name:      item.Service,
			Ports:     toStorePorts(item.Locals),
		})
	}
	if err := m.store.SetPreviews(contextName, specs); err != nil {
		log.Printf("persist previews for %s: %v", contextName, err)
	}
}

func (m *Manager) PersistShutdown() {
	if m.store == nil {
		return
	}
	m.persistPortForwards()
	state := m.State()
	contextName := state.Context
	namespace := state.Namespace
	if contextName == "" {
		contextName, namespace = m.PreferredSelection()
	}
	connected := state.Phase == PhaseConnected
	if contextName != "" {
		if connected {
			m.persistExchanges(contextName)
			m.persistMirrors(contextName)
			m.persistPreviews(contextName)
		}
		if err := m.store.SetConnected(contextName, namespace, connected); err != nil {
			log.Printf("persist connected flag: %v", err)
		}
	}
}

// RestoreStartup reapplies port-forwards and optionally reconnects the last cluster.
func (m *Manager) RestoreStartup(ctx context.Context) {
	if m.store == nil {
		return
	}
	snap := m.store.Snapshot()
	for contextName, cluster := range snap.Clusters {
		if cluster == nil {
			continue
		}
		for _, item := range cluster.PortForwards {
			_, err := m.portfwd.Start(ctx, portfwd.Request{
				Context:    contextName,
				Namespace:  item.Namespace,
				Kind:       item.Kind,
				Name:       item.Name,
				RemotePort: item.RemotePort,
				LocalPort:  item.LocalPort,
			})
			if err != nil {
				log.Printf("restore port-forward %s/%s/%s: %v", contextName, item.Kind, item.Name, err)
			}
		}
	}

	contextName := snap.UI.LastContext
	cluster := snap.Clusters[contextName]
	if contextName == "" || cluster == nil || !cluster.Connected {
		return
	}
	namespace := cluster.Namespace
	if namespace == "" {
		namespace = snap.UI.LastNamespace
	}
	if namespace == "" {
		namespace = "default"
	}
	if err := m.Connect(ctx, Request{Context: contextName, Namespace: namespace}); err != nil {
		log.Printf("restore connect %s: %v", contextName, err)
	}
}

func (m *Manager) restoreBindings(ctx context.Context, contextName string) {
	if m.store == nil || contextName == "" {
		return
	}
	m.mu.Lock()
	m.restoring = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.restoring = false
		m.mu.Unlock()
	}()

	cluster := m.store.Cluster(contextName)
	for _, item := range cluster.Exchanges {
		_, err := m.intercept.StartIntercept(ctx, intercept.Mapping{
			Namespace: item.Namespace,
			Service:   item.Service,
			Ports:     toInterceptPorts(item.Ports),
		})
		if err != nil {
			log.Printf("restore exchange %s/%s: %v", item.Namespace, item.Service, err)
		}
	}
	for _, item := range cluster.Mirrors {
		_, err := m.intercept.StartMirror(ctx, intercept.Mapping{
			Namespace: item.Namespace,
			Service:   item.Service,
			Ports:     toInterceptPorts(item.Ports),
		})
		if err != nil {
			log.Printf("restore mirror %s/%s: %v", item.Namespace, item.Service, err)
		}
	}
	for _, item := range cluster.Previews {
		_, err := m.intercept.StartPreview(ctx, intercept.PreviewRequest{
			Namespace: item.Namespace,
			Name:      item.Name,
			Ports:     toInterceptPorts(item.Ports),
		})
		if err != nil {
			log.Printf("restore preview %s/%s: %v", item.Namespace, item.Name, err)
		}
	}
}

func (m *Manager) isRestoring() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.restoring
}

func toStorePorts(items []intercept.PortMapping) []store.PortMapping {
	out := make([]store.PortMapping, 0, len(items))
	for _, item := range items {
		out = append(out, store.PortMapping{
			ServicePort: item.ServicePort,
			Protocol:    item.Protocol,
			LocalHost:   item.LocalHost,
			LocalPort:   item.LocalPort,
		})
	}
	return out
}

func toInterceptPorts(items []store.PortMapping) []intercept.PortMapping {
	out := make([]intercept.PortMapping, 0, len(items))
	for _, item := range items {
		out = append(out, intercept.PortMapping{
			ServicePort: item.ServicePort,
			Protocol:    item.Protocol,
			LocalHost:   item.LocalHost,
			LocalPort:   item.LocalPort,
		})
	}
	return out
}
