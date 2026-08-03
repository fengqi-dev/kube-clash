//go:build e2e

package inspector

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/e2e/harness"
	"github.com/fengqi-dev/kube-loop/internal/inspectorca"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/tunnel"
	"github.com/zalando/go-keyring"
)

type testSecretStore struct {
	value string
}

func (s *testSecretStore) Get(_, _ string) (string, error) {
	if s.value == "" {
		return "", keyring.ErrNotFound
	}
	return s.value, nil
}

func (s *testSecretStore) Set(_, _, value string) error {
	s.value = value
	return nil
}

func (s *testSecretStore) Delete(_, _ string) error {
	s.value = ""
	return nil
}

func TestHTTPSInspectorDataPath(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 3*time.Minute)
	defer cancel()
	live := harness.ConnectSession(t, ctx, session.Request{}, nil)
	if !live.State.GatewayCapabilities.Inspector {
		t.Skip("Gateway Inspector Agent is unavailable")
	}

	restConfig, err := live.Provider.RESTConfig(harness.KubeContext())
	if err != nil {
		t.Fatal(err)
	}
	upstreamCA := append([]byte(nil), restConfig.CAData...)
	if len(upstreamCA) == 0 && restConfig.CAFile != "" {
		upstreamCA, err = os.ReadFile(restConfig.CAFile)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(upstreamCA) == 0 {
		t.Fatal("Kubernetes API CA is unavailable")
	}

	now := time.Now().UTC()
	caManager := &inspectorca.Manager{
		Secrets: &testSecretStore{},
		Now:     func() time.Time { return now },
	}
	root, err := caManager.EnsureRoot()
	if err != nil {
		t.Fatal(err)
	}
	material, err := caManager.IssueIntermediate("minikube-https-e2e", upstreamCA)
	if err != nil {
		t.Fatal(err)
	}
	target := tunnel.InspectorTarget{
		ID: "kubernetes-api", Host: "kubernetes.default.svc", Port: 443,
		Protocol: "https", CaptureBody: true,
	}
	if err := live.Manager.StartInspector(ctx, tunnel.InspectorConfig{
		MaxBodySize: 64 << 10,
		Targets:     []tunnel.InspectorTarget{target},
		TLS:         &material,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := live.Manager.StopInspector(); err != nil {
			t.Logf("stop Inspector: %v", err)
		}
	})

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(root.CertificatePEM) {
		t.Fatal("append Inspector Root CA")
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    roots,
			},
		},
	}
	response, err := client.Get(
		"https://kubernetes.default.svc/version?inspector_token=must-not-appear",
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 499 {
		t.Fatalf("unexpected Kubernetes API status %d", response.StatusCode)
	}

	var flowID string
	sawEnd := false
	for !sawEnd {
		select {
		case event := <-live.Manager.InspectorEvents():
			switch event.Type {
			case tunnel.InspectorEventFlowStart:
				var payload struct {
					Protocol    string `json:"protocol"`
					HTTPVersion string `json:"httpVersion"`
					Path        string `json:"path"`
					TLS         struct {
						ServerName       string `json:"serverName"`
						Version          string `json:"version"`
						UpstreamVerified bool   `json:"upstreamVerified"`
					} `json:"tls"`
				}
				if err := json.Unmarshal(event.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Protocol != "https" ||
					payload.HTTPVersion != "HTTP/1.1" ||
					payload.Path != "/version" ||
					payload.TLS.ServerName != target.Host ||
					payload.TLS.Version == "" ||
					!payload.TLS.UpstreamVerified {
					t.Fatalf("unexpected HTTPS FlowStart payload: %+v", payload)
				}
				if strings.Contains(string(event.Payload), "must-not-appear") {
					t.Fatal("query secret appeared in FlowStart payload")
				}
				flowID = event.FlowID
			case tunnel.InspectorEventFlowEnd:
				if flowID != "" && event.FlowID == flowID {
					sawEnd = true
				}
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestMain(m *testing.M) {
	harness.RunMain(m)
}
