package inspectoragent

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/inspectorca"
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
	if s.value == "" {
		return keyring.ErrNotFound
	}
	s.value = ""
	return nil
}

func TestServeHTTPSConnection(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	caManager := &inspectorca.Manager{
		Secrets: &testSecretStore{},
		Now:     func() time.Time { return now },
	}
	root, err := caManager.EnsureRoot()
	if err != nil {
		t.Fatal(err)
	}
	upstreamCertificate, upstreamCAPEM := newUpstreamCertificate(t, now, "api.default.svc")
	material, err := caManager.IssueIntermediate("session-https", upstreamCAPEM)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := newTLSAuthority(&material)
	if err != nil {
		t.Fatal(err)
	}
	session := &agentSession{
		id:          "session-https",
		maxBodySize: 1024,
		tls:         authority,
		targets:     make(map[string]tunnel.InspectorTarget),
		connections: make(map[net.Conn]struct{}),
		events:      make(chan tunnel.InspectorEvent, eventQueueSize),
		done:        make(chan struct{}),
	}
	target := tunnel.InspectorTarget{
		ID: "target-https", Host: "api.default.svc", Port: 443,
		Protocol: "https", CaptureBody: true,
	}

	agentUpstream, serverUpstream := net.Pipe()
	upstreamDone := make(chan error, 1)
	go func() {
		defer serverUpstream.Close()
		serverTLS := tls.Server(serverUpstream, &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{upstreamCertificate},
			NextProtos:   []string{"http/1.1"},
		})
		request, readErr := http.ReadRequest(bufio.NewReader(serverTLS))
		if readErr != nil {
			upstreamDone <- readErr
			return
		}
		if request.URL.Path != "/ready" || request.URL.RawQuery != "token=secret" {
			upstreamDone <- io.ErrUnexpectedEOF
			return
		}
		upstreamDone <- (&http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(http.NoBody),
			Request:    request,
			Close:      true,
		}).Write(serverTLS)
	}()

	agentClient, rawClient := net.Pipe()
	proxyDone := make(chan struct{})
	go func() {
		defer close(proxyDone)
		defer agentClient.Close()
		defer agentUpstream.Close()
		serveHTTPSConnection(
			session, agentClient, bufio.NewReader(agentClient), agentUpstream, target,
		)
	}()

	rootPool := x509.NewCertPool()
	if !rootPool.AppendCertsFromPEM(root.CertificatePEM) {
		t.Fatal("append Inspector Root CA")
	}
	clientTLS := tls.Client(rawClient, &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: target.Host,
		RootCAs:    rootPool,
		NextProtos: []string{"http/1.1"},
	})
	request, err := http.NewRequest(
		http.MethodGet, "https://api.default.svc/ready?token=secret", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Close = true
	if err := request.Write(clientTLS); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(clientTLS), request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	_ = clientTLS.Close()
	<-proxyDone
	if err := <-upstreamDone; err != nil {
		t.Fatal(err)
	}

	var startPayload struct {
		Protocol    string `json:"protocol"`
		HTTPVersion string `json:"httpVersion"`
		Path        string `json:"path"`
		TLS         struct {
			ServerName       string `json:"serverName"`
			Version          string `json:"version"`
			ALPN             string `json:"alpn"`
			UpstreamVerified bool   `json:"upstreamVerified"`
		} `json:"tls"`
	}
	for {
		select {
		case event := <-session.events:
			if event.Type != tunnel.InspectorEventFlowStart {
				continue
			}
			if err := json.Unmarshal(event.Payload, &startPayload); err != nil {
				t.Fatal(err)
			}
			if startPayload.Protocol != "https" ||
				startPayload.HTTPVersion != "HTTP/1.1" ||
				startPayload.Path != "/ready" ||
				startPayload.TLS.ServerName != target.Host ||
				startPayload.TLS.Version == "" ||
				startPayload.TLS.ALPN != "http/1.1" ||
				!startPayload.TLS.UpstreamVerified {
				t.Fatalf("unexpected HTTPS FlowStart payload: %+v", startPayload)
			}
			return
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for HTTPS FlowStart event")
		}
	}
}

func TestNewTLSAuthorityRejectsUntrustedIntermediate(t *testing.T) {
	now := time.Now().UTC()
	first := &inspectorca.Manager{
		Secrets: &testSecretStore{},
		Now:     func() time.Time { return now },
	}
	if _, err := first.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	material, err := first.IssueIntermediate("session-one", nil)
	if err != nil {
		t.Fatal(err)
	}
	second := &inspectorca.Manager{
		Secrets: &testSecretStore{},
		Now:     func() time.Time { return now },
	}
	secondRoot, err := second.EnsureRoot()
	if err != nil {
		t.Fatal(err)
	}
	material.ChainPEM = secondRoot.CertificatePEM
	if _, err := newTLSAuthority(&material); err == nil {
		t.Fatal("expected untrusted Intermediate CA to be rejected")
	}
}

func TestPrepareHTTPSUpstreamRejectsUntrustedCertificate(t *testing.T) {
	now := time.Now().UTC()
	manager := &inspectorca.Manager{
		Secrets: &testSecretStore{},
		Now:     func() time.Time { return now },
	}
	if _, err := manager.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	material, err := manager.IssueIntermediate("session-untrusted", nil)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := newTLSAuthority(&material)
	if err != nil {
		t.Fatal(err)
	}
	upstreamCertificate, _ := newUpstreamCertificate(t, now, "api.default.svc")
	agentUpstream, serverUpstream := net.Pipe()
	defer agentUpstream.Close()
	serverDone := make(chan error, 1)
	go func() {
		defer serverUpstream.Close()
		serverDone <- tls.Server(serverUpstream, &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{upstreamCertificate},
			NextProtos:   []string{"http/1.1"},
		}).Handshake()
	}()
	session := &agentSession{tls: authority}
	if _, _, err := prepareHTTPSUpstream(
		session, agentUpstream,
		tunnel.InspectorTarget{Host: "api.default.svc", Port: 443, Protocol: "https"},
	); err == nil {
		t.Fatal("expected untrusted upstream TLS certificate to be rejected")
	}
	if err := <-serverDone; err == nil {
		t.Fatal("expected upstream server to observe a rejected handshake")
	}
}

func TestLearnTLSBypassFallsBackAfterClientRejection(t *testing.T) {
	target := tunnel.InspectorTarget{
		ID: "pinned-api", Host: "api.default.svc", Port: 443, Protocol: "https",
	}
	key := tunnel.InspectorTargetKey(target.Host, target.Port)
	session := &agentSession{
		id: "session-pinning",
		targets: map[string]tunnel.InspectorTarget{
			key: target,
		},
		tlsBypass:   make(map[string]string),
		connections: make(map[net.Conn]struct{}),
		events:      make(chan tunnel.InspectorEvent, eventQueueSize),
		done:        make(chan struct{}),
	}
	session.learnTLSBypass(target)
	if reason := session.tlsBypassReason(target); reason == "" {
		t.Fatal("expected subsequent connections to use TLS bypass")
	}
	event := <-session.events
	if event.Type != tunnel.InspectorEventError {
		t.Fatalf("event type=%d want error", event.Type)
	}
	var payload struct {
		TargetID        string `json:"targetID"`
		Stage           string `json:"stage"`
		PossiblePinning bool   `json:"possiblePinning"`
		Fallback        string `json:"fallback"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TargetID != target.ID ||
		payload.Stage != "client-tls-handshake" ||
		!payload.PossiblePinning ||
		payload.Fallback != "subsequent-connections" {
		t.Fatalf("unexpected TLS bypass event: %+v", payload)
	}
	session.learnTLSBypass(target)
	select {
	case duplicate := <-session.events:
		t.Fatalf("unexpected duplicate TLS bypass event: %+v", duplicate)
	default:
	}

	server := NewServer(nil)
	server.sessions[session.id] = session
	if err := server.update(request{SessionID: session.id}); err != nil {
		t.Fatal(err)
	}
	if reason := session.tlsBypassReason(target); reason != "" {
		t.Fatal("removing target did not clear learned TLS bypass")
	}
}

func newUpstreamCertificate(
	t *testing.T, now time.Time, host string,
) (tls.Certificate, []byte) {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Upstream Root"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(
		rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	rootCertificate, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		DNSNames:     []string{host},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(
		rand.Reader, serverTemplate, rootCertificate, &serverKey.PublicKey, rootKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{
		Certificate: [][]byte{serverDER, rootDER},
		PrivateKey:  serverKey,
	}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})
}
