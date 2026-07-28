package main

import (
	"context"
	"errors"
	"sync"

	"github.com/kube-clash/kube-clash/internal/cluster"
	"github.com/kube-clash/kube-clash/internal/session"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx     context.Context
	manager *session.Manager
	once    sync.Once
}

type BootstrapData struct {
	Contexts   []cluster.ContextInfo `json:"contexts"`
	Namespaces []string              `json:"namespaces"`
	Session    session.State         `json:"session"`
}

func NewApp() *App {
	provider := cluster.NewProvider()
	return &App{manager: session.NewManager(provider)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.once.Do(func() {
		a.manager.Subscribe(func(state session.State) {
			runtime.EventsEmit(ctx, "session:state", state)
		})
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
	return BootstrapData{Contexts: contexts, Namespaces: namespaces, Session: a.manager.State()}, nil
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
