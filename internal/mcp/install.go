package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const clientServerName = "kubeloop"

// Supported MCP client identifiers for InstallClientConfig.
const (
	ClientClaude = "claude"
	ClientCodex  = "codex"
	ClientCursor = "cursor"
	ClientVSCode = "vscode"
)

// InstallResult describes a successful client config write.
type InstallResult struct {
	Client string `json:"client"`
	Path   string `json:"path"`
}

// ClientConfigPath returns the user-scoped MCP config path for a client.
func ClientConfigPath(client string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(client)) {
	case ClientClaude:
		if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
			return filepath.Join(dir, ".claude.json"), nil
		}
		return filepath.Join(home, ".claude.json"), nil
	case ClientCodex:
		if dir := strings.TrimSpace(os.Getenv("CODEX_HOME")); dir != "" {
			return filepath.Join(dir, "config.toml"), nil
		}
		return filepath.Join(home, ".codex", "config.toml"), nil
	case ClientCursor:
		return filepath.Join(home, ".cursor", "mcp.json"), nil
	case ClientVSCode:
		return vscodeUserMCPPath(home)
	default:
		return "", fmt.Errorf("unsupported mcp client %q (use claude, codex, cursor, or vscode)", client)
	}
}

func vscodeUserMCPPath(home string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json"), nil
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Code", "User", "mcp.json"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "Code", "User", "mcp.json"), nil
	default:
		return filepath.Join(home, ".config", "Code", "User", "mcp.json"), nil
	}
}

// InstallClientConfig merges the KubeLoop MCP HTTP endpoint into a client's
// user-scoped configuration file. token may be empty when Bearer auth is off.
func InstallClientConfig(client, url, token string) (InstallResult, error) {
	client = strings.ToLower(strings.TrimSpace(client))
	if url == "" {
		return InstallResult{}, fmt.Errorf("mcp url is required")
	}
	path, err := ClientConfigPath(client)
	if err != nil {
		return InstallResult{}, err
	}
	switch client {
	case ClientClaude:
		err = installJSONMCPServers(path, url, token, true)
	case ClientCursor:
		err = installJSONMCPServers(path, url, token, false)
	case ClientVSCode:
		err = installVSCodeMCP(path, url, token)
	case ClientCodex:
		err = installCodexTOML(path, url, token)
	default:
		return InstallResult{}, fmt.Errorf("unsupported mcp client %q", client)
	}
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Client: client, Path: path}, nil
}

func installJSONMCPServers(path, url, token string, requireType bool) error {
	server := map[string]any{"url": url}
	if token != "" {
		server["headers"] = map[string]string{
			"Authorization": "Bearer " + token,
		}
	}
	if requireType {
		server["type"] = "http"
	}
	return upsertJSONMap(path, "mcpServers", clientServerName, server)
}

func installVSCodeMCP(path, url, token string) error {
	server := map[string]any{
		"type": "http",
		"url":  url,
	}
	if token != "" {
		server["headers"] = map[string]string{
			"Authorization": "Bearer " + token,
		}
	}
	return upsertJSONMap(path, "servers", clientServerName, server)
}

func upsertJSONMap(path, rootKey, serverName string, server map[string]any) error {
	raw, err := readOptionalFile(path)
	if err != nil {
		return err
	}
	root := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&root); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	servers, _ := root[rootKey].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[serverName] = server
	root[rootKey] = servers

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	out = append(out, '\n')
	return writeFileAtomic(path, out, 0o600)
}

func installCodexTOML(path, url, token string) error {
	raw, err := readOptionalFile(path)
	if err != nil {
		return err
	}
	content := removeTOMLTables(string(raw), "mcp_servers."+clientServerName)
	content = strings.TrimRight(content, "\n")
	var block string
	if token == "" {
		block = fmt.Sprintf(`
[mcp_servers.%s]
url = %q
`, clientServerName, url)
	} else {
		block = fmt.Sprintf(`
[mcp_servers.%s]
url = %q

[mcp_servers.%s.http_headers]
Authorization = %q
`, clientServerName, url, clientServerName, "Bearer "+token)
	}
	if content != "" {
		content += "\n"
	}
	content += strings.TrimPrefix(block, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return writeFileAtomic(path, []byte(content), 0o600)
}

func removeTOMLTables(content, prefix string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trim, "["), "]"))
			skipping = name == prefix || strings.HasPrefix(name, prefix+".")
		}
		if !skipping {
			out = append(out, line)
		}
	}
	// Drop trailing empty lines left by removals; keep a single trailing newline later.
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

func readOptionalFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return raw, nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	temp := path + ".kubeloop-tmp"
	if err := os.WriteFile(temp, data, mode); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
