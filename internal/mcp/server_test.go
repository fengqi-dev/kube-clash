package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/helper"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/portfwd"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/store"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeBackend struct {
	connected contextNamespace
}

type contextNamespace struct {
	context   string
	namespace string
}

func (f *fakeBackend) SessionState() session.State {
	return session.State{Phase: session.PhaseIdle, Context: f.connected.context, Namespace: f.connected.namespace}
}
func (f *fakeBackend) ReloadContexts() (cluster.ClusterInventory, error) {
	return cluster.ClusterInventory{Contexts: []cluster.ContextInfo{{Name: "minikube"}}}, nil
}
func (f *fakeBackend) ProbeContext(context.Context, string) (cluster.ProbeResult, error) {
	return cluster.ProbeResult{OK: true, Version: "v1.30.0"}, nil
}
func (f *fakeBackend) Namespaces(context.Context, string) ([]string, error) {
	return []string{"default", "kube-system"}, nil
}
func (f *fakeBackend) ListServices(context.Context, string, string) ([]cluster.ServiceInfo, error) {
	return []cluster.ServiceInfo{{Name: "api", Namespace: "default"}}, nil
}
func (f *fakeBackend) ListPods(context.Context, string, string) ([]cluster.PodInfo, error) {
	return []cluster.PodInfo{{Name: "api-0", Namespace: "default"}}, nil
}
func (f *fakeBackend) Connect(_ context.Context, contextName, namespace string) error {
	f.connected = contextNamespace{context: contextName, namespace: namespace}
	return nil
}
func (f *fakeBackend) Disconnect() error {
	f.connected = contextNamespace{}
	return nil
}
func (f *fakeBackend) GetManualNetwork(string) cluster.ManualNetwork        { return cluster.ManualNetwork{} }
func (f *fakeBackend) SetManualNetwork(string, cluster.ManualNetwork) error { return nil }
func (f *fakeBackend) GetHostAliases(string) []store.HostAliasSpec          { return nil }
func (f *fakeBackend) SetHostAliases(string, []store.HostAliasSpec) error   { return nil }
func (f *fakeBackend) StartIntercept(context.Context, intercept.Mapping) (intercept.Info, error) {
	return intercept.Info{ID: "ex-1"}, nil
}
func (f *fakeBackend) StartMirror(context.Context, intercept.Mapping) (intercept.Info, error) {
	return intercept.Info{ID: "mi-1"}, nil
}
func (f *fakeBackend) StopIntercept(context.Context, string) error { return nil }
func (f *fakeBackend) ListIntercepts() []intercept.Info            { return nil }
func (f *fakeBackend) ListMirrors() []intercept.Info               { return nil }
func (f *fakeBackend) StartPreview(context.Context, intercept.PreviewRequest) (intercept.Info, error) {
	return intercept.Info{ID: "pr-1", Preview: true}, nil
}
func (f *fakeBackend) StopPreview(context.Context, string) error { return nil }
func (f *fakeBackend) ListPreviews() []intercept.Info            { return nil }
func (f *fakeBackend) StartPortForward(context.Context, portfwd.Request) (portfwd.Info, error) {
	return portfwd.Info{ID: "pf-1", LocalPort: 18080}, nil
}
func (f *fakeBackend) StopPortForward(string) error     { return nil }
func (f *fakeBackend) ListPortForwards() []portfwd.Info { return nil }
func (f *fakeBackend) HelperStatus(context.Context) helper.Status {
	return helper.Status{Installed: true, Running: true, Expected: "dev"}
}
func (f *fakeBackend) InstallHelper(context.Context) error   { return nil }
func (f *fakeBackend) UninstallHelper(context.Context) error { return nil }
func (f *fakeBackend) SingBoxConfig() ([]byte, error) {
	return []byte(`{"log":{"level":"info"}}`), nil
}

func TestGenerateToken(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 64 {
		t.Fatalf("token length=%d", len(token))
	}
}

func TestServerRejectsMissingBearer(t *testing.T) {
	backend := &fakeBackend{}
	srv := NewServer(backend, "test")
	port := freePort(t)
	srv.Configure(store.MCPConfig{Enabled: true, Port: port, TokenEnabled: true, Token: "secret-token"})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Stop() }()

	status := srv.Status()
	if !status.Listening || status.URL == "" || !status.TokenEnabled {
		t.Fatalf("status=%#v", status)
	}

	req, err := http.NewRequest(http.MethodPost, status.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestServerAllowsMissingBearerWhenTokenDisabled(t *testing.T) {
	backend := &fakeBackend{}
	srv := NewServer(backend, "test")
	port := freePort(t)
	srv.Configure(store.MCPConfig{Enabled: true, Port: port, TokenEnabled: false})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Stop() }()

	status := srv.Status()
	if !status.Listening || status.TokenEnabled || status.Token != "" {
		t.Fatalf("status=%#v", status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: status.URL}, nil)
	if err != nil {
		t.Fatalf("connect without bearer: %v", err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("expected tools")
	}
}

func TestServerToolsConnectAndList(t *testing.T) {
	backend := &fakeBackend{}
	srv := NewServer(backend, "test")
	token := "test-bearer-token"
	port := freePort(t)
	srv.Configure(store.MCPConfig{Enabled: true, Port: port, TokenEnabled: true, Token: token})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Stop() }()

	endpoint := srv.Status().URL
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{
		Endpoint: endpoint,
		HTTPClient: &http.Client{
			Transport: &bearerRoundTripper{token: token, base: http.DefaultTransport},
		},
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 26 {
		t.Fatalf("expected 26 tools, got %d", len(tools.Tools))
	}

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "connect",
		Arguments: map[string]any{"context": "minikube", "namespace": "default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %#v", res)
	}
	if backend.connected.context != "minikube" || backend.connected.namespace != "default" {
		t.Fatalf("connected=%#v", backend.connected)
	}

	listRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "list_namespaces",
		Arguments: map[string]any{"context": "minikube"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if listRes.IsError {
		t.Fatalf("list error: %#v", listRes)
	}
	raw, _ := json.Marshal(listRes.StructuredContent)
	if !strings.Contains(string(raw), "default") {
		t.Fatalf("namespaces=%s", raw)
	}
}

func TestServerStopWithHangingGET(t *testing.T) {
	backend := &fakeBackend{}
	srv := NewServer(backend, "test")
	port := freePort(t)
	srv.Configure(store.MCPConfig{Enabled: true, Port: port, TokenEnabled: false})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: srv.Status().URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	done := make(chan error, 1)
	go func() { done <- srv.Stop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop with hanging GET: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop blocked on hanging Streamable HTTP GET")
	}
}

func TestServerStartIdempotentKeepsSession(t *testing.T) {
	backend := &fakeBackend{}
	srv := NewServer(backend, "test")
	port := freePort(t)
	srv.Configure(store.MCPConfig{Enabled: true, Port: port, TokenEnabled: false})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Stop() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: srv.Status().URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatalf("list tools after redundant Start: %v", err)
	}
}

func TestServerRejectsWrongBearer(t *testing.T) {
	backend := &fakeBackend{}
	srv := NewServer(backend, "test")
	port := freePort(t)
	srv.Configure(store.MCPConfig{Enabled: true, Port: port, TokenEnabled: true, Token: "correct"})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Stop() }()

	req, err := http.NewRequest(http.MethodPost, srv.Status().URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (b *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(clone)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}
