package mcp

import (
	"errors"
	"fmt"
	"log"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/store"
)

// Controller owns the embedded MCP server and persists settings via store.
type Controller struct {
	server *Server
	store  *store.Store
}

// NewController wires a Backend over provider/manager and loads store config.
func NewController(
	provider *cluster.Provider,
	manager *session.Manager,
	stateStore *store.Store,
	version string,
) *Controller {
	c := &Controller{
		server: NewServer(managerBackend{provider: provider, manager: manager}, version),
		store:  stateStore,
	}
	if stateStore != nil {
		c.server.Configure(stateStore.MCP())
	}
	return c
}

// StartFromStore enables the listener when persisted config says Enabled.
func (c *Controller) StartFromStore() {
	if c == nil || c.server == nil || c.store == nil {
		return
	}
	cfg := c.store.MCP()
	if !cfg.Enabled {
		c.server.Configure(cfg)
		return
	}
	var err error
	cfg, err = ensureToken(cfg)
	if err != nil {
		log.Printf("mcp token: %v", err)
		return
	}
	if err := c.persist(cfg); err != nil {
		log.Printf("persist mcp: %v", err)
		return
	}
	if err := c.server.Apply(); err != nil {
		log.Printf("start mcp: %v", err)
	}
}

// Stop shuts down the HTTP listener.
func (c *Controller) Stop() error {
	if c == nil || c.server == nil {
		return nil
	}
	return c.server.Stop()
}

// Status returns runtime + config for the UI.
func (c *Controller) Status() Status {
	if c == nil || c.server == nil {
		return Status{Port: store.DefaultMCPPort}
	}
	return c.server.Status()
}

// SetEnabled turns the MCP server on or off and persists the choice.
func (c *Controller) SetEnabled(enabled bool) error {
	if c == nil || c.server == nil {
		return errors.New("mcp server unavailable")
	}
	cfg := c.server.Config()
	cfg.Enabled = enabled
	var err error
	cfg, err = ensureToken(cfg)
	if err != nil {
		return err
	}
	if err := c.persist(cfg); err != nil {
		return err
	}
	return c.server.SetEnabled(enabled)
}

// SetPort updates the listen port and persists it.
func (c *Controller) SetPort(port int) error {
	if c == nil || c.server == nil {
		return errors.New("mcp server unavailable")
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid mcp port %d", port)
	}
	cfg := c.server.Config()
	cfg.Port = port
	if err := c.persist(cfg); err != nil {
		return err
	}
	return c.server.SetPort(port)
}

// SetTokenEnabled turns Bearer token auth on or off and persists the choice.
func (c *Controller) SetTokenEnabled(enabled bool) error {
	if c == nil || c.server == nil {
		return errors.New("mcp server unavailable")
	}
	cfg := c.server.Config()
	cfg.TokenEnabled = enabled
	var err error
	cfg, err = ensureToken(cfg)
	if err != nil {
		return err
	}
	if err := c.persist(cfg); err != nil {
		return err
	}
	return c.server.SetTokenEnabled(enabled)
}

// RegenerateToken replaces the bearer token when token auth is enabled.
func (c *Controller) RegenerateToken() (string, error) {
	if c == nil || c.server == nil {
		return "", errors.New("mcp server unavailable")
	}
	cfg := c.server.Config()
	if !cfg.TokenEnabled {
		return "", errors.New("enable MCP token auth first")
	}
	token, err := GenerateToken()
	if err != nil {
		return "", err
	}
	cfg.Token = token
	if err := c.persist(cfg); err != nil {
		return "", err
	}
	if err := c.server.SetToken(token); err != nil {
		return "", err
	}
	return token, nil
}

// InstallClient writes the KubeLoop MCP endpoint into a client user config
// (claude, codex, cursor, or vscode). Enables the local MCP server if needed.
func (c *Controller) InstallClient(client string) (InstallResult, error) {
	if c == nil || c.server == nil {
		return InstallResult{}, errors.New("mcp server unavailable")
	}
	status := c.server.Status()
	if !status.Enabled || !status.Listening {
		if err := c.SetEnabled(true); err != nil {
			return InstallResult{}, err
		}
		status = c.server.Status()
	}
	if status.URL == "" {
		return InstallResult{}, errors.New("mcp server is not ready")
	}
	if status.TokenEnabled && status.Token == "" {
		return InstallResult{}, errors.New("mcp token is not ready")
	}
	token := ""
	if status.TokenEnabled {
		token = status.Token
	}
	return InstallClientConfig(client, status.URL, token)
}

func (c *Controller) persist(cfg store.MCPConfig) error {
	if c.store == nil {
		return errors.New("state store unavailable")
	}
	cfg = store.MCPConfig{
		Enabled:      cfg.Enabled,
		Port:         cfg.Port,
		TokenEnabled: cfg.TokenEnabled,
		Token:        cfg.Token,
	}
	if cfg.Port <= 0 {
		cfg.Port = store.DefaultMCPPort
	}
	if err := c.store.SetMCP(cfg); err != nil {
		return err
	}
	c.server.Configure(cfg)
	return nil
}

func ensureToken(cfg store.MCPConfig) (store.MCPConfig, error) {
	if !cfg.TokenEnabled || cfg.Token != "" {
		return cfg, nil
	}
	token, err := GenerateToken()
	if err != nil {
		return cfg, err
	}
	cfg.Token = token
	if cfg.Port <= 0 {
		cfg.Port = store.DefaultMCPPort
	}
	return cfg, nil
}
