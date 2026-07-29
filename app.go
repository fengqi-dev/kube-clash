package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/update"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx         context.Context
	manager     *session.Manager
	updater     *update.Checker
	once        sync.Once
	updateMu    sync.RWMutex
	updateCheck sync.Mutex
	updateState update.Info
}

type BootstrapData struct {
	Contexts   []cluster.ContextInfo `json:"contexts"`
	Namespaces []string              `json:"namespaces"`
	Session    session.State         `json:"session"`
	Update     update.Info           `json:"update"`
}

func NewApp() *App {
	provider := cluster.NewProvider()
	return &App{
		manager: session.NewManager(provider),
		updater: &update.Checker{CurrentVersion: version},
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
	})
}

func (a *App) shutdown(context.Context) {
	_ = a.manager.Disconnect()
}

func (a *App) Bootstrap() (BootstrapData, error) {
	contexts, err := a.manager.Contexts()
	if err != nil {
		return BootstrapData{}, err
	}
	selected := ""
	for _, item := range contexts {
		if item.Current {
			selected = item.Name
			break
		}
	}
	namespaces := []string{"default"}
	if selected != "" {
		if found, listErr := a.manager.Namespaces(a.ctx, selected); listErr == nil && len(found) > 0 {
			namespaces = found
		}
	}
	a.updateMu.RLock()
	updateState := a.updateState
	a.updateMu.RUnlock()
	return BootstrapData{
		Contexts: contexts, Namespaces: namespaces,
		Session: a.manager.State(), Update: updateState,
	}, nil
}

func (a *App) Namespaces(contextName string) ([]string, error) {
	if contextName == "" {
		return nil, errors.New("context is required")
	}
	return a.manager.Namespaces(a.ctx, contextName)
}

func (a *App) Connect(contextName, namespace string) error {
	return a.manager.Connect(a.ctx, session.Request{
		Context:   contextName,
		Namespace: namespace,
	})
}

func (a *App) Disconnect() error {
	return a.manager.Disconnect()
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
