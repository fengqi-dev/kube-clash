package mcp

import (
	"context"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/helper"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/store"
)

// Backend is the control-plane surface exposed to MCP tools.
// Implementations typically wrap the Wails App / session.Manager.
type Backend interface {
	SessionState() session.State
	ReloadContexts() (cluster.ClusterInventory, error)
	ProbeContext(ctx context.Context, contextName string) (cluster.ProbeResult, error)
	Namespaces(ctx context.Context, contextName string) ([]string, error)
	ListServices(ctx context.Context, contextName, namespace string) ([]cluster.ServiceInfo, error)
	ListPods(ctx context.Context, contextName, namespace string) ([]cluster.PodInfo, error)
	Connect(ctx context.Context, contextName, namespace string) error
	Disconnect() error
	GetManualNetwork(contextName string) cluster.ManualNetwork
	SetManualNetwork(contextName string, network cluster.ManualNetwork) error
	GetHostAliases(contextName string) []store.HostAliasSpec
	SetHostAliases(contextName string, items []store.HostAliasSpec) error
	StartIntercept(ctx context.Context, mapping intercept.Mapping) (intercept.Info, error)
	StartMirror(ctx context.Context, mapping intercept.Mapping) (intercept.Info, error)
	StopIntercept(ctx context.Context, id string) error
	ListIntercepts() []intercept.Info
	ListMirrors() []intercept.Info
	StartPreview(ctx context.Context, request intercept.PreviewRequest) (intercept.Info, error)
	StopPreview(ctx context.Context, id string) error
	ListPreviews() []intercept.Info
	StartPortForward(ctx context.Context, request portfwd.Request) (portfwd.Info, error)
	StopPortForward(id string) error
	ListPortForwards() []portfwd.Info
	HelperStatus(ctx context.Context) helper.Status
	InstallHelper(ctx context.Context) error
	UninstallHelper(ctx context.Context) error
	SingBoxConfig() ([]byte, error)
}
