package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallClientConfigWithoutToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	url := "http://127.0.0.1:30808/mcp"
	if _, err := InstallClientConfig(ClientCursor, url, ""); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cursor map[string]any
	if err := json.Unmarshal(raw, &cursor); err != nil {
		t.Fatal(err)
	}
	entry := cursor["mcpServers"].(map[string]any)["kubeloop"].(map[string]any)
	if entry["url"] != url {
		t.Fatalf("url=%v", entry["url"])
	}
	if _, ok := entry["headers"]; ok {
		t.Fatalf("headers should be omitted: %#v", entry["headers"])
	}
}

func TestInstallClientConfigCursorAndClaude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	url := "http://127.0.0.1:30808/mcp"
	token := "abc123"

	got, err := InstallClientConfig(ClientCursor, url, token)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(home, ".cursor", "mcp.json")
	if got.Path != wantPath {
		t.Fatalf("path=%q want %q", got.Path, wantPath)
	}
	raw, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	var cursor map[string]any
	if err := json.Unmarshal(raw, &cursor); err != nil {
		t.Fatal(err)
	}
	servers := cursor["mcpServers"].(map[string]any)
	entry := servers["kubeloop"].(map[string]any)
	if entry["url"] != url {
		t.Fatalf("url=%v", entry["url"])
	}
	if _, ok := entry["type"]; ok {
		t.Fatal("cursor entry should not force type")
	}

	// Preserve existing Claude keys while updating mcpServers.
	claudePath := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(claudePath, []byte(`{"userID":"u1","mcpServers":{"other":{"type":"http","url":"https://example.com"}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallClientConfig(ClientClaude, url, token); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	var claude map[string]any
	if err := json.Unmarshal(raw, &claude); err != nil {
		t.Fatal(err)
	}
	if claude["userID"] != "u1" {
		t.Fatalf("lost userID: %#v", claude["userID"])
	}
	servers = claude["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatal("lost existing server")
	}
	entry = servers["kubeloop"].(map[string]any)
	if entry["type"] != "http" {
		t.Fatalf("claude type=%v", entry["type"])
	}
	headers := entry["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer abc123" {
		t.Fatalf("headers=%#v", headers)
	}
}

func TestInstallClientConfigVSCodeAndCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	url := "http://127.0.0.1:30808/mcp"
	token := "tok"

	if _, err := InstallClientConfig(ClientVSCode, url, token); err != nil {
		t.Fatal(err)
	}
	path, err := ClientConfigPath(ClientVSCode)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var vscode map[string]any
	if err := json.Unmarshal(raw, &vscode); err != nil {
		t.Fatal(err)
	}
	servers := vscode["servers"].(map[string]any)
	entry := servers["kubeloop"].(map[string]any)
	if entry["type"] != "http" || entry["url"] != url {
		t.Fatalf("vscode entry=%#v", entry)
	}

	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := `
model = "gpt-5"

[mcp_servers.other]
command = "echo"

[mcp_servers.kubeloop]
url = "http://old"

[mcp_servers.kubeloop.http_headers]
Authorization = "Bearer old"
`
	if err := os.WriteFile(codexPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallClientConfig(ClientCodex, url, token); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	if !strings.Contains(text, `model = "gpt-5"`) || !strings.Contains(text, `[mcp_servers.other]`) {
		t.Fatalf("lost unrelated config:\n%s", text)
	}
	if strings.Count(text, "[mcp_servers.kubeloop]") != 1 {
		t.Fatalf("duplicate kubeloop section:\n%s", text)
	}
	if !strings.Contains(text, url) || !strings.Contains(text, `Authorization = "Bearer tok"`) {
		t.Fatalf("missing kubeloop values:\n%s", text)
	}
	if strings.Contains(text, "http://old") || strings.Contains(text, "Bearer old") {
		t.Fatalf("old kubeloop values remain:\n%s", text)
	}
}

func TestInstallClientConfigRejectsUnknown(t *testing.T) {
	if _, err := InstallClientConfig("windsurf", "http://127.0.0.1:1/mcp", "t"); err == nil {
		t.Fatal("expected error")
	}
}
