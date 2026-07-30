package main

import (
	"context"
	"sort"

	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/tray"
)

const maxTrayRecentClusters = 5

// trayHost adapts App to tray.Host without exporting those methods on App
// (Wails binds all exported App methods to the frontend).
type trayHost struct {
	app *App
}

func (h *trayHost) Context() context.Context { return h.app.ctx }

func (h *trayHost) SessionState() session.State { return h.app.manager.State() }

func (h *trayHost) Subscribe(listener func(session.State)) {
	h.app.manager.Subscribe(listener)
}

func (h *trayHost) RecentClusters() []tray.Cluster {
	seen := map[string]struct{}{}
	var out []tray.Cluster
	add := func(contextName, namespace string) {
		if contextName == "" {
			return
		}
		if _, ok := seen[contextName]; ok {
			return
		}
		seen[contextName] = struct{}{}
		if namespace == "" {
			namespace = "default"
		}
		out = append(out, tray.Cluster{Context: contextName, Namespace: namespace})
	}

	state := h.app.manager.State()
	add(state.Context, state.Namespace)

	prefContext, prefNamespace := h.app.manager.PreferredSelection()
	add(prefContext, prefNamespace)

	if h.app.store != nil {
		snap := h.app.store.Snapshot()
		add(snap.UI.LastContext, snap.UI.LastNamespace)
		names := make([]string, 0, len(snap.Clusters))
		for name := range snap.Clusters {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			ns := ""
			if cluster := snap.Clusters[name]; cluster != nil {
				ns = cluster.Namespace
			}
			add(name, ns)
		}
	}

	if contexts, err := h.app.manager.Contexts(); err == nil {
		for _, item := range contexts {
			if item.Current {
				add(item.Name, prefNamespace)
			}
		}
		for _, item := range contexts {
			add(item.Name, prefNamespace)
			if len(out) >= maxTrayRecentClusters {
				break
			}
		}
	}

	if len(out) > maxTrayRecentClusters {
		out = out[:maxTrayRecentClusters]
	}
	return out
}

func (h *trayHost) Connect(contextName, namespace string) error {
	return h.app.Connect(contextName, namespace)
}

func (h *trayHost) Disconnect() error {
	return h.app.Disconnect()
}
