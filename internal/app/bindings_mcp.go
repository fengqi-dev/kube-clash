package app

import (
	"errors"

	loopmcp "github.com/fengqi-dev/kube-loop/internal/mcp"
)

func (a *App) GetMCPStatus() loopmcp.Status {
	if a.mcp == nil {
		return loopmcp.Status{}
	}
	return a.mcp.Status()
}

func (a *App) SetMCPEnabled(enabled bool) error {
	if a.mcp == nil {
		return errors.New("mcp server unavailable")
	}
	return a.mcp.SetEnabled(enabled)
}

func (a *App) SetMCPPort(port int) error {
	if a.mcp == nil {
		return errors.New("mcp server unavailable")
	}
	return a.mcp.SetPort(port)
}

func (a *App) SetMCPTokenEnabled(enabled bool) error {
	if a.mcp == nil {
		return errors.New("mcp server unavailable")
	}
	return a.mcp.SetTokenEnabled(enabled)
}

func (a *App) RegenerateMCPToken() (string, error) {
	if a.mcp == nil {
		return "", errors.New("mcp server unavailable")
	}
	return a.mcp.RegenerateToken()
}

func (a *App) InstallMCPClient(client string) (loopmcp.InstallResult, error) {
	if a.mcp == nil {
		return loopmcp.InstallResult{}, errors.New("mcp server unavailable")
	}
	return a.mcp.InstallClient(client)
}
