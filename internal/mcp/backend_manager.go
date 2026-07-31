package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/helper"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/store"
)

// managerBackend implements Backend against session.Manager + cluster.Provider.
type managerBackend struct {
	provider *cluster.Provider
	manager  *session.Manager
}

func (b managerBackend) SessionState() session.State { return b.manager.State() }

func (b managerBackend) ReloadContexts() (cluster.ClusterInventory, error) {
	return b.provider.Inventory()
}

func (b managerBackend) ProbeContext(ctx context.Context, contextName string) (cluster.ProbeResult, error) {
	if contextName == "" {
		return cluster.ProbeResult{}, errors.New("context is required")
	}
	probeCtx, cancel := context.WithTimeout(ctxOrBackground(ctx), 3*time.Second)
	defer cancel()
	result := b.provider.Probe(probeCtx, contextName)
	if result.OK && result.Version != "" {
		b.manager.SetKubernetesVersion(result.Version)
	}
	return result, nil
}

func (b managerBackend) Namespaces(ctx context.Context, contextName string) ([]string, error) {
	if contextName == "" {
		return nil, errors.New("context is required")
	}
	return b.manager.Namespaces(ctxOrBackground(ctx), contextName)
}

func (b managerBackend) ListServices(ctx context.Context, contextName, namespace string) ([]cluster.ServiceInfo, error) {
	if contextName == "" {
		return nil, errors.New("context is required")
	}
	return b.manager.ListServices(ctxOrBackground(ctx), contextName, namespace)
}

func (b managerBackend) ListPods(ctx context.Context, contextName, namespace string) ([]cluster.PodInfo, error) {
	if contextName == "" {
		return nil, errors.New("context is required")
	}
	return b.manager.ListPods(ctxOrBackground(ctx), contextName, namespace)
}

func (b managerBackend) Connect(ctx context.Context, contextName, namespace string) error {
	_ = b.manager.RememberSelection(contextName, namespace)
	return b.manager.Connect(ctxOrBackground(ctx), session.Request{
		Context:   contextName,
		Namespace: namespace,
	})
}

func (b managerBackend) Disconnect() error { return b.manager.Disconnect() }

func (b managerBackend) GetManualNetwork(contextName string) cluster.ManualNetwork {
	return b.manager.ManualNetwork(contextName)
}

func (b managerBackend) SetManualNetwork(contextName string, network cluster.ManualNetwork) error {
	return b.manager.SetManualNetwork(contextName, network)
}

func (b managerBackend) GetHostAliases(contextName string) []store.HostAliasSpec {
	return b.manager.HostAliases(contextName)
}

func (b managerBackend) SetHostAliases(contextName string, items []store.HostAliasSpec) error {
	return b.manager.SetHostAliases(contextName, items)
}

func (b managerBackend) StartIntercept(ctx context.Context, mapping intercept.Mapping) (intercept.Info, error) {
	return b.manager.StartIntercept(ctxOrBackground(ctx), mapping)
}

func (b managerBackend) StartMirror(ctx context.Context, mapping intercept.Mapping) (intercept.Info, error) {
	return b.manager.StartMirror(ctxOrBackground(ctx), mapping)
}

func (b managerBackend) StopIntercept(ctx context.Context, id string) error {
	return b.manager.StopIntercept(ctxOrBackground(ctx), id)
}

func (b managerBackend) ListIntercepts() []intercept.Info { return b.manager.ListIntercepts() }

func (b managerBackend) ListMirrors() []intercept.Info { return b.manager.ListMirrors() }

func (b managerBackend) StartPreview(ctx context.Context, request intercept.PreviewRequest) (intercept.Info, error) {
	return b.manager.StartPreview(ctxOrBackground(ctx), request)
}

func (b managerBackend) StopPreview(ctx context.Context, id string) error {
	return b.manager.StopPreview(ctxOrBackground(ctx), id)
}

func (b managerBackend) ListPreviews() []intercept.Info { return b.manager.ListPreviews() }

func (b managerBackend) StartPortForward(ctx context.Context, request portfwd.Request) (portfwd.Info, error) {
	return b.manager.StartPortForwardSession(ctxOrBackground(ctx), request)
}

func (b managerBackend) StopPortForward(id string) error { return b.manager.StopPortForward(id) }

func (b managerBackend) ListPortForwards() []portfwd.Info { return b.manager.ListPortForwards() }

func (b managerBackend) HelperStatus(ctx context.Context) helper.Status {
	return helper.GetStatus(ctxOrBackground(ctx))
}

func (b managerBackend) InstallHelper(ctx context.Context) error {
	return helper.EnsureInstall(ctxOrBackground(ctx))
}

func (b managerBackend) UninstallHelper(ctx context.Context) error {
	return helper.Uninstall(ctxOrBackground(ctx))
}

func (b managerBackend) SingBoxConfig() ([]byte, error) {
	return b.manager.SingBoxConfig()
}

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
