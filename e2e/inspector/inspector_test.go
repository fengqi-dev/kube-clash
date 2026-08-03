//go:build e2e

package inspector

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/e2e/harness"
	"github.com/fengqi-dev/kube-loop/internal/inspectorca"
	"github.com/fengqi-dev/kube-loop/internal/intercept"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/fengqi-dev/kube-loop/internal/tunnel"
	"github.com/zalando/go-keyring"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	kubernetesService, err := live.Client.CoreV1().Services("default").Get(
		ctx, "kubernetes", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	target := tunnel.InspectorTarget{
		ID: "kubernetes-api", Host: "kubernetes.default.svc", Port: 443,
		Protocol: "https", CaptureBody: true,
		Namespace: "default", Service: "kubernetes",
		ServiceUID: string(kubernetesService.UID),
		Addresses:  append([]string(nil), kubernetesService.Spec.ClusterIPs...),
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

	target.Protocol = "http2"
	if err := live.Manager.UpdateInspectorTargets([]tunnel.InspectorTarget{target}); err != nil {
		t.Fatal(err)
	}
	h2Client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:             nil,
			ForceAttemptHTTP2: true,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    roots,
			},
		},
	}
	h2Response, err := h2Client.Get("https://kubernetes.default.svc/version")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, h2Response.Body)
	_ = h2Response.Body.Close()
	if h2Response.ProtoMajor != 2 {
		t.Fatalf("HTTP/2 Inspector response protocol = %s", h2Response.Proto)
	}
	flowID = ""
	for {
		select {
		case event := <-live.Manager.InspectorEvents():
			if event.Type == tunnel.InspectorEventFlowStart {
				var payload struct {
					Protocol    string `json:"protocol"`
					HTTPVersion string `json:"httpVersion"`
				}
				if err := json.Unmarshal(event.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Protocol == "http2" && payload.HTTPVersion == "HTTP/2.0" {
					flowID = event.FlowID
				}
			}
			if event.Type == tunnel.InspectorEventFlowEnd &&
				flowID != "" && event.FlowID == flowID {
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestReverseServiceInspectorDataPath(t *testing.T) {
	harness.RequireE2E(t)
	ctx, cancel := harness.TestContext(t, 3*time.Minute)
	defer cancel()
	provider := harness.NewProvider(t)
	client := harness.KubeClient(t, provider)
	if err := harness.EnsureEchoWorkload(ctx, client); err != nil {
		t.Fatal(err)
	}
	service, err := client.CoreV1().Services(harness.EchoNamespace).Get(
		ctx, "echo", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		writer.Header().Set("X-KubeLoop-E2E", "reverse-inspector")
		_, _ = writer.Write([]byte("reverse-inspector-ok"))
	}))
	defer local.Close()
	localPort := local.Listener.Addr().(*net.TCPAddr).Port

	live := harness.ConnectSession(t, ctx, session.Request{
		Context: harness.KubeContext(), Namespace: harness.EchoNamespace,
	}, nil)
	if !live.State.GatewayCapabilities.Inspector {
		t.Skip("Gateway Inspector Agent is unavailable")
	}
	target := tunnel.InspectorTarget{
		ID: "reverse-echo", Host: "echo." + harness.EchoNamespace + ".svc",
		Port: 8080, Protocol: "http", CaptureBody: true,
		Namespace: harness.EchoNamespace, Service: "echo",
		ServiceUID: string(service.UID),
		Addresses:  append([]string(nil), service.Spec.ClusterIPs...),
	}
	if err := live.Manager.StartInspector(ctx, tunnel.InspectorConfig{
		MaxBodySize: 64 << 10,
		Targets:     []tunnel.InspectorTarget{target},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := live.Manager.StopInspector(); err != nil {
			t.Logf("stop Inspector: %v", err)
		}
	})
	info, err := live.Manager.StartIntercept(ctx, intercept.Mapping{
		Namespace: harness.EchoNamespace,
		Service:   "echo",
		Ports: []intercept.PortMapping{{
			ServicePort: 8080, Protocol: "TCP",
			LocalHost: "127.0.0.1", LocalPort: localPort,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := live.Provider.GetGateway(ctx, harness.KubeContext())
	if err != nil {
		t.Fatal(err)
	}
	tcpListenPort := harness.InterceptListenPort(
		t, info.Ports, 8080, corev1.ProtocolTCP,
	)
	interceptStopped := false
	t.Cleanup(func() {
		if interceptStopped {
			return
		}
		if err := live.Manager.StopIntercept(context.Background(), info.ID); err != nil {
			t.Logf("stop reverse Inspector intercept: %v", err)
		}
	})

	request := "GET /reverse-inspector HTTP/1.1\r\n" +
		"Host: echo." + harness.EchoNamespace + ".svc\r\n" +
		"Connection: close\r\n\r\n"
	_ = harness.WaitClusterProbe(
		t, ctx, client, gateway.IP, tcpListenPort, "tcp",
		request, "HTTP/1.1 200",
	)

	var flowID string
	for {
		select {
		case event := <-live.Manager.InspectorEvents():
			switch event.Type {
			case tunnel.InspectorEventFlowStart:
				var payload struct {
					Path   string `json:"path"`
					Source string `json:"source"`
				}
				if err := json.Unmarshal(event.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Path == "/reverse-inspector" {
					if payload.Source != "cluster" {
						t.Fatalf("reverse Flow source = %q", payload.Source)
					}
					flowID = event.FlowID
				}
			case tunnel.InspectorEventFlowEnd:
				if flowID != "" && event.FlowID == flowID {
					if err := live.Manager.StopIntercept(ctx, info.ID); err != nil {
						t.Fatal(err)
					}
					interceptStopped = true
					return
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
