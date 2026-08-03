package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/helper"
	"github.com/fengqi-dev/kube-loop/internal/inspectorca"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/store"
	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

func (a *App) StartIntercept(mapping intercept.Mapping) (intercept.Info, error) {
	return a.manager.StartIntercept(a.context(), mapping)
}

func (a *App) StartMirror(mapping intercept.Mapping) (intercept.Info, error) {
	return a.manager.StartMirror(a.context(), mapping)
}

func (a *App) StopIntercept(id string) error {
	return a.manager.StopIntercept(a.context(), id)
}

func (a *App) TestIntercept(id string) session.ConnectivityTestResult {
	return a.manager.TestIntercept(a.context(), id)
}

func (a *App) ListIntercepts() []intercept.Info {
	return a.manager.ListIntercepts()
}

func (a *App) ListMirrors() []intercept.Info {
	return a.manager.ListMirrors()
}

func (a *App) StartInspector(config tunnel.InspectorConfig) error {
	needsTLS := false
	for _, target := range config.Targets {
		if strings.EqualFold(strings.TrimSpace(target.Protocol), "https") {
			needsTLS = true
			break
		}
	}
	if needsTLS {
		status, err := a.InspectorCAStatus()
		if err != nil {
			return err
		}
		if !status.Trusted {
			return errors.New("Inspector Root CA must be installed and trusted before HTTPS capture")
		}
		manager := inspectorca.NewManager()
		material, err := manager.IssueIntermediate(
			"desktop-"+time.Now().UTC().Format("20060102T150405Z"), nil,
		)
		if err != nil {
			return err
		}
		config.TLS = &material
	}
	return a.manager.StartInspector(a.context(), config)
}

type InspectorCAState struct {
	Present     bool   `json:"present"`
	Trusted     bool   `json:"trusted"`
	Fingerprint string `json:"fingerprint,omitempty"`
	NotAfter    string `json:"notAfter,omitempty"`
	TrustError  string `json:"trustError,omitempty"`
}

func (a *App) InspectorCAStatus() (InspectorCAState, error) {
	manager := inspectorca.NewManager()
	status, err := manager.Status()
	if err != nil {
		return InspectorCAState{}, err
	}
	state := InspectorCAState{
		Present: status.Present, Fingerprint: status.Fingerprint,
	}
	if !status.NotAfter.IsZero() {
		state.NotAfter = status.NotAfter.UTC().Format(time.RFC3339)
	}
	if !status.Present {
		return state, nil
	}
	root, err := manager.LoadRoot()
	if err != nil {
		return InspectorCAState{}, err
	}
	client, err := helper.NewClient()
	if err != nil {
		state.TrustError = err.Error()
		return state, nil
	}
	response, err := client.InspectorCAStatus(a.context(), root.CertificatePEM)
	if err != nil {
		state.TrustError = err.Error()
		return state, nil
	}
	state.Trusted = response.CertificateTrusted
	return state, nil
}

// InstallInspectorCA is intentionally only exposed as an explicit UI action.
func (a *App) InstallInspectorCA() (InspectorCAState, error) {
	manager := inspectorca.NewManager()
	root, err := manager.EnsureRoot()
	if err != nil {
		return InspectorCAState{}, err
	}
	client, err := helper.NewClient()
	if err != nil {
		return InspectorCAState{}, err
	}
	response, err := client.InstallInspectorCA(a.context(), root.CertificatePEM)
	if err != nil {
		return InspectorCAState{}, err
	}
	if !response.CertificateTrusted {
		return InspectorCAState{}, errors.New("Helper did not confirm Inspector Root CA trust")
	}
	return a.InspectorCAStatus()
}

// RemoveInspectorCA removes system trust before deleting the keyring material.
func (a *App) RemoveInspectorCA() error {
	manager := inspectorca.NewManager()
	root, err := manager.LoadRoot()
	if errors.Is(err, inspectorca.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	client, err := helper.NewClient()
	if err != nil {
		return err
	}
	response, err := client.RemoveInspectorCA(a.context(), root.CertificatePEM)
	if err != nil {
		return err
	}
	if response.CertificateTrusted {
		return errors.New("Helper still reports Inspector Root CA as trusted")
	}
	return manager.DeleteRoot()
}

func (a *App) UpdateInspectorTargets(targets []tunnel.InspectorTarget) error {
	return a.manager.UpdateInspectorTargets(targets)
}

func (a *App) StopInspector() error {
	return a.manager.StopInspector()
}

func (a *App) GetInspectorState() tunnel.InspectorState {
	return a.manager.InspectorState()
}

func (a *App) StartPreview(request intercept.PreviewRequest) (intercept.Info, error) {
	return a.manager.StartPreview(a.context(), request)
}

func (a *App) StopPreview(id string) error {
	return a.manager.StopPreview(a.context(), id)
}

func (a *App) ListPreviews() []intercept.Info {
	return a.manager.ListPreviews()
}

func (a *App) StartPortForward(request portfwd.Request) (portfwd.Info, error) {
	return a.manager.StartPortForwardSession(a.context(), request)
}

func (a *App) StopPortForward(id string) error {
	return a.manager.StopPortForward(id)
}

func (a *App) TestPortForward(id string) session.ConnectivityTestResult {
	return a.manager.TestPortForward(a.context(), id)
}

func (a *App) ListPortForwards() []portfwd.Info {
	return a.manager.ListPortForwards()
}

func (a *App) ResetSessions() error {
	return a.manager.ResetSessions(a.context())
}

func (a *App) SessionIntentCounts() store.SessionIntentCounts {
	return a.manager.SessionIntentCounts()
}

func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}
