package inspectoragent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

func TestAgentHTTPFlowAndRedaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socketDir, err := os.MkdirTemp("", "ki-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socketPath := socketDir + "/agent.sock"
	listener, err := ListenUnix(ctx, socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := NewServer(log.New(io.Discard, "", 0))
	defer server.Close()
	go func() { _ = server.Serve(listener) }()

	targetListener, targetAddress := listenPrivateTestTarget(t)
	defer targetListener.Close()
	_, targetPortText, err := net.SplitHostPort(targetAddress)
	if err != nil {
		t.Fatal(err)
	}
	targetPort, err := strconv.Atoi(targetPortText)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		connection, acceptErr := targetListener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		request, readErr := http.ReadRequest(bufio.NewReader(connection))
		if readErr != nil {
			return
		}
		_, _ = io.Copy(io.Discard, request.Body)
		_ = request.Body.Close()
		response := &http.Response{
			StatusCode: http.StatusCreated,
			Status:     "201 Created",
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header: http.Header{
				"Content-Type": {"text/plain"},
				"Set-Cookie":   {"session=response-secret"},
			},
			Body:          io.NopCloser(strings.NewReader("response-body")),
			ContentLength: int64(len("response-body")),
			Close:         true,
		}
		_ = response.Write(connection)
	}()

	client := &Client{SocketPath: socketPath}
	target := tunnel.InspectorTarget{
		ID:          fmt.Sprintf("default/api:%d", targetPort),
		Host:        "api.default.svc",
		Port:        uint16(targetPort),
		Protocol:    "http",
		CaptureBody: true,
	}
	rawEndpoint, err := client.StartSession(
		context.Background(),
		"0123456789abcdef0123456789abcdef",
		tunnel.InspectorConfig{MaxBodySize: 1024, Targets: []tunnel.InspectorTarget{target}},
	)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := rawEndpoint.(*Endpoint)
	defer endpoint.Close()

	connection, err := endpoint.DialContext(
		context.Background(), target, targetAddress,
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = fmt.Fprintf(
		connection,
		"POST /orders?token=hidden HTTP/1.1\r\n"+
			"Host: api.default.svc\r\n"+
			"Authorization: Bearer request-secret\r\n"+
			"Cookie: session=request-secret\r\n"+
			"Content-Length: 12\r\n"+
			"Connection: close\r\n\r\nrequest-body",
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	_ = connection.Close()
	if response.StatusCode != http.StatusCreated || string(body) != "response-body" {
		t.Fatalf("unexpected response status=%d body=%q", response.StatusCode, body)
	}

	var payloads []string
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-endpoint.Events():
			if !ok {
				t.Fatal("event stream closed before FlowEnd")
			}
			payloads = append(payloads, string(event.Payload))
			if event.Type == tunnel.InspectorEventFlowEnd {
				joined := strings.Join(payloads, "\n")
				if strings.Contains(joined, "request-secret") ||
					strings.Contains(joined, "response-secret") ||
					strings.Contains(joined, "token=hidden") {
					t.Fatalf("sensitive data leaked in events: %s", joined)
				}
				if !strings.Contains(joined, "[REDACTED]") ||
					!strings.Contains(joined, "cmVxdWVzdC1ib2R5") ||
					!strings.Contains(joined, "cmVzcG9uc2UtYm9keQ==") {
					t.Fatalf("missing redaction or captured body: %s", joined)
				}
				var summary map[string]any
				if err := json.Unmarshal(event.Payload, &summary); err != nil {
					t.Fatal(err)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for Inspector events")
		}
	}
}

func TestAgentReverseBridgeHTTPFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socketDir, err := os.MkdirTemp("", "ki-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socketPath := socketDir + "/agent.sock"
	listener, err := ListenUnix(ctx, socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := NewServer(log.New(io.Discard, "", 0))
	defer server.Close()
	go func() { _ = server.Serve(listener) }()

	client := &Client{SocketPath: socketPath}
	target := tunnel.InspectorTarget{
		ID: "default/api", Host: "api.default.svc", Port: 8080,
		Protocol: "http", CaptureBody: true,
	}
	rawEndpoint, err := client.StartSession(
		ctx, "0123456789abcdef0123456789abcdef",
		tunnel.InspectorConfig{
			MaxBodySize: 1024, Targets: []tunnel.InspectorTarget{target},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := rawEndpoint.(*Endpoint)
	defer endpoint.Close()
	clusterSide, desktopSide, err := endpoint.BridgeContext(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer clusterSide.Close()
	defer desktopSide.Close()
	go func() {
		request, readErr := http.ReadRequest(bufio.NewReader(desktopSide))
		if readErr != nil {
			return
		}
		_, _ = io.Copy(io.Discard, request.Body)
		_ = request.Body.Close()
		response := &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK",
			Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
			Header:        http.Header{"Content-Type": {"text/plain"}},
			Body:          io.NopCloser(strings.NewReader("local-response")),
			ContentLength: int64(len("local-response")), Close: true,
		}
		_ = response.Write(desktopSide)
	}()
	_, err = fmt.Fprint(
		clusterSide,
		"GET /reverse HTTP/1.1\r\nHost: api.default.svc\r\nConnection: close\r\n\r\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(clusterSide), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if string(body) != "local-response" {
		t.Fatalf("response=%q", body)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-endpoint.Events():
			if event.Type == tunnel.InspectorEventFlowStart &&
				!strings.Contains(string(event.Payload), `"source":"cluster"`) {
				t.Fatalf("missing cluster source: %s", event.Payload)
			}
			if event.Type == tunnel.InspectorEventFlowEnd {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for reverse Inspector FlowEnd")
		}
	}
}

func TestAgentRejectsUnselectedAndPublicTargets(t *testing.T) {
	server := NewServer(log.New(io.Discard, "", 0))
	sessionID := "abcdef0123456789abcdef0123456789"
	if err := server.start(request{
		Op: opStart, SessionID: sessionID,
		Config: &tunnel.InspectorConfig{Targets: []tunnel.InspectorTarget{{
			ID: "api", Host: "api.default.svc", Port: 80, Protocol: "http",
		}}},
	}); err != nil {
		t.Fatal(err)
	}
	defer server.stop(sessionID)
	session := server.session(sessionID)
	if _, err := session.authorizeTarget(tunnel.InspectorTarget{
		ID: "other", Host: "other.default.svc", Port: 80, Protocol: "http",
	}, "10.0.0.8:80"); err == nil {
		t.Fatal("unselected target was authorized")
	}
	if _, err := session.authorizeTarget(tunnel.InspectorTarget{
		ID: "api", Host: "api.default.svc", Port: 80, Protocol: "http",
	}, "8.8.8.8:80"); err == nil {
		t.Fatal("public target was authorized")
	}
	if _, err := session.authorizeTarget(tunnel.InspectorTarget{
		ID: "api", Host: "api.default.svc", Port: 80, Protocol: "http",
	}, "10.0.0.8:8080"); err == nil {
		t.Fatal("different target port was authorized")
	}
}

func TestRPCLineLimit(t *testing.T) {
	oversized := strings.Repeat("x", maxRPCSize+1) + "\n"
	if _, err := readRequest(bufio.NewReader(strings.NewReader(oversized))); err == nil {
		t.Fatal("oversized RPC request was accepted")
	}
}

func listenPrivateTestTarget(t *testing.T) (net.Listener, string) {
	t.Helper()
	var privateIP net.IP
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		ip, _, parseErr := net.ParseCIDR(address.String())
		if parseErr == nil && ip.To4() != nil && ip.IsPrivate() && !ip.IsLoopback() {
			privateIP = ip
			break
		}
	}
	if privateIP == nil {
		t.Skip("no private non-loopback IPv4 address")
	}
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return listener, net.JoinHostPort(privateIP.String(), fmt.Sprintf("%d", port))
}
