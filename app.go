package main

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/helper"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/store"
	"github.com/fengqi-dev/kube-loop/internal/update"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx         context.Context
	provider    *cluster.Provider
	manager     *session.Manager
	store       *store.Store
	updater     *update.Checker
	once        sync.Once
	updateMu    sync.RWMutex
	updateCheck sync.Mutex
	updateState update.Info
}

type BootstrapData struct {
	Contexts           []cluster.ContextInfo        `json:"contexts"`
	Namespaces         []string                     `json:"namespaces"`
	Session            session.State                `json:"session"`
	Update             update.Info                  `json:"update"`
	PreferredContext   string                       `json:"preferredContext,omitempty"`
	PreferredNamespace string                       `json:"preferredNamespace,omitempty"`
	KubeconfigFiles    []cluster.KubeconfigFileInfo `json:"kubeconfigFiles,omitempty"`
}

func NewApp() *App {
	if version != "" {
		helper.Version = version
	}
	provider := cluster.NewProvider()
	provider.SetUserAgent(version)
	stateStore, err := store.Open("")
	if err != nil {
		log.Printf("open state store: %v", err)
		stateStore = nil
	}
	if stateStore != nil {
		provider.SetExtraKubeconfigFiles(stateStore.KubeconfigFiles())
	}
	options := []session.Option{}
	if stateStore != nil {
		options = append(options, session.WithStore(stateStore))
	}
	return &App{
		provider: provider,
		manager:  session.NewManager(provider, options...),
		store:    stateStore,
		updater:  &update.Checker{CurrentVersion: version},
		updateState: update.Info{
			CurrentVersion: version,
			URL:            "https://github.com/fengqi-dev/kube-loop/releases",
		},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.once.Do(func() {
		a.manager.Subscribe(func(state session.State) {
			runtime.EventsEmit(ctx, "session:state", state)
		})
		go func() {
			state := a.checkForUpdates(ctx)
			runtime.EventsEmit(ctx, "update:state", state)
		}()
		go a.manager.RestoreStartup(ctx)
	})
}

func (a *App) shutdown(context.Context) {
	_ = a.manager.Shutdown()
}

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
	if preferredNamespace == "" || !containsString(namespaces, preferredNamespace) {
		if containsString(namespaces, "default") {
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
		Contexts:           contexts,
		Namespaces:         namespaces,
		Session:            a.manager.State(),
		Update:             updateState,
		PreferredContext:   preferredContext,
		PreferredNamespace: preferredNamespace,
		KubeconfigFiles:    a.provider.KubeconfigFiles(),
	}, nil
}

func (a *App) ReloadContexts() (cluster.ClusterInventory, error) {
	return a.provider.Inventory()
}

func (a *App) AddKubeconfig() (cluster.ClusterInventory, error) {
	if a.ctx == nil {
		return cluster.ClusterInventory{}, errors.New("application is not ready")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select kubeconfig",
		Filters: []runtime.FileFilter{
			{DisplayName: "Kubeconfig", Pattern: "*.yaml;*.yml;*.conf;*"},
		},
	})
	if err != nil {
		return cluster.ClusterInventory{}, err
	}
	if path == "" {
		return a.provider.Inventory()
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
		files := append(a.provider.ExtraKubeconfigFiles(), path)
		a.provider.SetExtraKubeconfigFiles(files)
	}
	return a.provider.Inventory()
}

func (a *App) RemoveKubeconfig(path string) (cluster.ClusterInventory, error) {
	if path == "" {
		return cluster.ClusterInventory{}, errors.New("kubeconfig path is required")
	}
	state := a.manager.State()
	if state.Phase == session.PhaseConnected ||
		state.Phase == session.PhaseChecking ||
		state.Phase == session.PhaseInstalling ||
		state.Phase == session.PhaseDiscovering ||
		state.Phase == session.PhaseStarting {
		contexts, err := a.provider.Contexts()
		if err != nil {
			return cluster.ClusterInventory{}, err
		}
		for _, item := range contexts {
			if item.Name == state.Context && item.Source == path {
				return cluster.ClusterInventory{}, errors.New("disconnect before removing the active kubeconfig")
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
	return a.provider.Probe(probeCtx, contextName), nil
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

func (a *App) Connect(contextName, namespace string) error {
	_ = a.manager.RememberSelection(contextName, namespace)
	return a.manager.Connect(a.ctx, session.Request{
		Context:   contextName,
		Namespace: namespace,
	})
}

func (a *App) Disconnect() error {
	return a.manager.Disconnect()
}

func (a *App) GetManualNetwork(contextName string) cluster.ManualNetwork {
	return a.manager.ManualNetwork(contextName)
}

func (a *App) SetManualNetwork(contextName string, network cluster.ManualNetwork) error {
	return a.manager.SetManualNetwork(contextName, network)
}

func (a *App) GatewayInstallManifest() string {
	return a.manager.GatewayInstallManifest()
}

func (a *App) StartIntercept(mapping intercept.Mapping) (intercept.Info, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.manager.StartIntercept(ctx, mapping)
}

func (a *App) StopIntercept(id string) error {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.manager.StopIntercept(ctx, id)
}

func (a *App) ListIntercepts() []intercept.Info {
	return a.manager.ListIntercepts()
}

func (a *App) StartPreview(request intercept.PreviewRequest) (intercept.Info, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.manager.StartPreview(ctx, request)
}

func (a *App) StopPreview(id string) error {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.manager.StopPreview(ctx, id)
}

func (a *App) ListPreviews() []intercept.Info {
	return a.manager.ListPreviews()
}

func (a *App) StartPortForward(request portfwd.Request) (portfwd.Info, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.manager.StartPortForwardSession(ctx, request)
}

func (a *App) StopPortForward(id string) error {
	return a.manager.StopPortForward(id)
}

func (a *App) ListPortForwards() []portfwd.Info {
	return a.manager.ListPortForwards()
}

func (a *App) CheckForUpdates() update.Info {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	checkContext, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	state := a.checkForUpdates(checkContext)
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "update:state", state)
	}
	return state
}

func (a *App) HelperStatus() helper.Status {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return helper.GetStatus(ctx)
}

func (a *App) InstallHelper() error {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return helper.EnsureInstall(ctx)
}

func (a *App) UninstallHelper() error {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return helper.Uninstall(ctx)
}

func (a *App) OpenUpdatePage() error {
	a.updateMu.RLock()
	target := a.updateState.URL
	a.updateMu.RUnlock()
	if target == "" {
		target = "https://github.com/fengqi-dev/kube-loop/releases"
	}
	if a.ctx == nil {
		return errors.New("application is not ready")
	}
	runtime.BrowserOpenURL(a.ctx, target)
	return nil
}

func (a *App) checkForUpdates(ctx context.Context) update.Info {
	a.updateCheck.Lock()
	defer a.updateCheck.Unlock()
	state, err := a.updater.Check(ctx)
	if err != nil {
		state.Error = err.Error()
	}
	a.updateMu.Lock()
	a.updateState = state
	a.updateMu.Unlock()
	return state
}

func contextExists(contexts []cluster.ContextInfo, name string) bool {
	for _, item := range contexts {
		if item.Name == name {
			return true
		}
	}
	return false
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
