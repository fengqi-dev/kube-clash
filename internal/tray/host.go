package tray

import (
	"context"

	"github.com/fengqi-dev/kube-loop/internal/session"
)

// Cluster is a kubeconfig context entry for the recent menu.
type Cluster struct {
	Context   string
	Namespace string
}

// Host is the desktop app surface the tray needs.
type Host interface {
	Context() context.Context
	SessionState() session.State
	Subscribe(func(session.State))
	RecentClusters() []Cluster
	Connect(contextName, namespace string) error
	Disconnect() error
}
