package app

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/locale"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) Bootstrap() (BootstrapData, error) {
	contexts, err := a.manager.Contexts()
	if err != nil {
		return BootstrapData{}, err
	}
	preferredContext, preferredNamespace := a.manager.PreferredSelection()
	selected := preferredContext
	if selected == "" || !contextExists(contexts, selected) {
		selected = ""
		for _, item := range contexts {
			if item.Current {
				selected = item.Name
				break
			}
		}
		if selected == "" && len(contexts) > 0 {
			selected = contexts[0].Name
		}
	}
	namespaces := []string{"default"}
	if selected != "" {
		if found, listErr := a.manager.Namespaces(a.ctx, selected); listErr == nil && len(found) > 0 {
			namespaces = found
		}
	}
	if preferredNamespace == "" || !slices.Contains(namespaces, preferredNamespace) {
		if slices.Contains(namespaces, "default") {
			preferredNamespace = "default"
		} else if len(namespaces) > 0 {
			preferredNamespace = namespaces[0]
		}
	}
	if preferredContext == "" || !contextExists(contexts, preferredContext) {
		preferredContext = selected
	}
	a.updateMu.RLock()
	updateState := a.updateState
	a.updateMu.RUnlock()
	return BootstrapData{
		Contexts: contexts, Namespaces: namespaces, Session: a.manager.State(),
		Update: updateState, PreferredContext: preferredContext,
		PreferredNamespace: preferredNamespace, KubeconfigFiles: a.provider.KubeconfigFiles(),
	}, nil
}

func (a *App) ReloadContexts() (cluster.ClusterInventory, error) {
	return a.provider.Inventory()
}

func (a *App) AddKubeconfig() (cluster.ClusterInventory, error) {
	if a.ctx == nil {
		return cluster.ClusterInventory{}, errors.New("application is not ready")
	}
	s := locale.T()
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: s.SelectKubeconfig,
		Filters: []runtime.FileFilter{{
			DisplayName: s.KubeconfigFilter, Pattern: "*.yaml;*.yml;*.conf;*",
		}},
	})
	if err != nil {
		return cluster.ClusterInventory{}, err
	}
	if path == "" {
		return a.provider.Inventory()
	}
	return a.AddKubeconfigPath(path)
}

func (a *App) AddKubeconfigPath(path string) (cluster.ClusterInventory, error) {
	if path == "" {
		return cluster.ClusterInventory{}, errors.New("kubeconfig path is required")
	}
	if err := cluster.ValidateKubeconfigFile(path); err != nil {
		return cluster.ClusterInventory{}, err
	}
	if a.store != nil {
		if err := a.store.AddKubeconfigFile(path); err != nil {
			return cluster.ClusterInventory{}, err
		}
		a.provider.SetExtraKubeconfigFiles(a.store.KubeconfigFiles())
	} else {
		a.provider.SetExtraKubeconfigFiles(append(a.provider.ExtraKubeconfigFiles(), path))
	}
	return a.provider.Inventory()
}

func (a *App) RemoveKubeconfig(path string) (cluster.ClusterInventory, error) {
	if path == "" {
		return cluster.ClusterInventory{}, errors.New("kubeconfig path is required")
	}
	state := a.manager.State()
	if sessionActive(state.Phase) {
		contexts, err := a.provider.Contexts()
		if err != nil {
			return cluster.ClusterInventory{}, err
		}
		for _, item := range contexts {
			if item.Name == state.Context && item.Source == path {
				return cluster.ClusterInventory{}, errors.New(
					"disconnect before removing the active kubeconfig",
				)
			}
		}
	}
	if a.store != nil {
		if err := a.store.RemoveKubeconfigFile(path); err != nil {
			return cluster.ClusterInventory{}, err
		}
		a.provider.SetExtraKubeconfigFiles(a.store.KubeconfigFiles())
	} else {
		remaining := make([]string, 0)
		for _, existing := range a.provider.ExtraKubeconfigFiles() {
			if existing != path {
				remaining = append(remaining, existing)
			}
		}
		a.provider.SetExtraKubeconfigFiles(remaining)
	}
	return a.provider.Inventory()
}

func sessionActive(phase session.Phase) bool {
	switch phase {
	case session.PhaseConnected, session.PhaseChecking, session.PhaseInstalling,
		session.PhaseDiscovering, session.PhaseStarting:
		return true
	default:
		return false
	}
}

func (a *App) ProbeContext(contextName string) (cluster.ProbeResult, error) {
	if contextName == "" {
		return cluster.ProbeResult{}, errors.New("context is required")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result := a.provider.Probe(probeCtx, contextName)
	state := a.manager.State()
	if result.OK && result.Version != "" &&
		state.Context == contextName && sessionActive(state.Phase) {
		a.manager.SetKubernetesVersion(result.Version)
	}
	return result, nil
}

func (a *App) RememberSelection(contextName, namespace string) error {
	return a.manager.RememberSelection(contextName, namespace)
}

func (a *App) Namespaces(contextName string) ([]string, error) {
	if contextName == "" {
		return nil, errors.New("context is required")
	}
	return a.manager.Namespaces(a.ctx, contextName)
}

func (a *App) ListServices(contextName, namespace string) ([]cluster.ServiceInfo, error) {
	if contextName == "" {
		return nil, errors.New("context is required")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.manager.ListServices(ctx, contextName, namespace)
}

func (a *App) ListPods(contextName, namespace string) ([]cluster.PodInfo, error) {
	if contextName == "" {
		return nil, errors.New("context is required")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.manager.ListPods(ctx, contextName, namespace)
}

func contextExists(contexts []cluster.ContextInfo, name string) bool {
	for _, item := range contexts {
		if item.Name == name {
			return true
		}
	}
	return false
}
