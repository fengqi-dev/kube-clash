package restoreplan

import (
	"sort"

	"github.com/fengqi-dev/kube-loop/internal/store"
)

type PortForward struct {
	Context string
	Spec    store.PortForwardSpec
}

type Reconnect struct {
	Context   string
	Namespace string
	Mode      string
}

type Startup struct {
	ContextCount int
	LastContext  string
	PortForwards []PortForward
	Reconnect    *Reconnect
}

// BuildStartup separates restore policy from runtime execution. Port-forwards
// belonging to the auto-reconnected context are deferred until its transport
// mode is ready; all other contexts can be restored immediately.
func BuildStartup(snapshot store.State) Startup {
	plan := Startup{
		ContextCount: len(snapshot.Clusters),
		LastContext:  snapshot.UI.LastContext,
	}
	contexts := make([]string, 0, len(snapshot.Clusters))
	for contextName := range snapshot.Clusters {
		contexts = append(contexts, contextName)
	}
	sort.Strings(contexts)
	for _, contextName := range contexts {
		cluster := snapshot.Clusters[contextName]
		if cluster == nil {
			continue
		}
		if cluster.Connected && contextName == snapshot.UI.LastContext {
			continue
		}
		for _, item := range cluster.PortForwards {
			plan.PortForwards = append(plan.PortForwards, PortForward{
				Context: contextName,
				Spec:    item,
			})
		}
	}

	contextName := snapshot.UI.LastContext
	cluster := snapshot.Clusters[contextName]
	if contextName == "" || cluster == nil || !cluster.Connected {
		return plan
	}
	namespace := cluster.Namespace
	if namespace == "" {
		namespace = snapshot.UI.LastNamespace
	}
	if namespace == "" {
		namespace = "default"
	}
	mode := cluster.ConnectionMode
	if mode != "socks" {
		mode = "tun"
	}
	plan.Reconnect = &Reconnect{
		Context:   contextName,
		Namespace: namespace,
		Mode:      mode,
	}
	return plan
}
