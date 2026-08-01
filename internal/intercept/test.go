package intercept

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const connectivityTestTimeout = 3 * time.Second

// Test verifies that every TCP local target in an Exchange, Mirror, or Preview
// session accepts a connection.
func (m *Manager) Test(parent context.Context, id string) error {
	m.mu.Lock()
	runtime := m.registry.get(id)
	if runtime == nil {
		m.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}
	locals := append([]PortMapping{}, runtime.info.Locals...)
	m.mu.Unlock()
	if parent == nil {
		parent = context.Background()
	}

	for _, local := range locals {
		if strings.ToLower(local.Protocol) != "tcp" {
			return errors.New("generic connectivity tests are only supported for TCP sessions")
		}
		host := strings.TrimSpace(local.LocalHost)
		if host == "" || host == "0.0.0.0" {
			host = "127.0.0.1"
		} else if host == "::" {
			host = "::1"
		}
		address := net.JoinHostPort(host, strconv.Itoa(local.LocalPort))
		ctx, cancel := context.WithTimeout(parent, connectivityTestTimeout)
		connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		cancel()
		if err != nil {
			return fmt.Errorf("dial local target %s: %w", address, err)
		}
		if err := connection.Close(); err != nil {
			return fmt.Errorf("close local target %s: %w", address, err)
		}
	}
	return nil
}
