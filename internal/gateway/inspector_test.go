package gateway

import (
	"context"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

type fakeInspectorEngine struct {
	mu        sync.Mutex
	sessionID string
	config    tunnel.InspectorConfig
	endpoint  *fakeInspectorEndpoint
	err       error
}

func (e *fakeInspectorEngine) StartSession(
	_ context.Context, sessionID string, config tunnel.InspectorConfig,
) (InspectorEndpoint, error) {
	if e.err != nil {
		return nil, e.err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessionID = sessionID
	e.config = config
	e.endpoint = &fakeInspectorEndpoint{
		events: make(chan tunnel.InspectorEvent, 4),
	}
	return e.endpoint, nil
}

type fakeInspectorEndpoint struct {
	mu             sync.Mutex
	dialedTarget   tunnel.InspectorTarget
	dialedAddress  string
	updatedTargets []tunnel.InspectorTarget
	events         chan tunnel.InspectorEvent
	closeOnce      sync.Once
}

func (e *fakeInspectorEndpoint) DialContext(
	_ context.Context, target tunnel.InspectorTarget, address string,
) (net.Conn, error) {
	e.mu.Lock()
	e.dialedTarget = target
	e.dialedAddress = address
	e.mu.Unlock()
	left, right := net.Pipe()
	_ = right.Close()
	return left, nil
}

func (e *fakeInspectorEndpoint) BridgeContext(
	_ context.Context, _ tunnel.InspectorTarget,
) (net.Conn, net.Conn, error) {
	clientLeft, clientRight := net.Pipe()
	upstreamLeft, upstreamRight := net.Pipe()
	_ = clientRight.Close()
	_ = upstreamRight.Close()
	return clientLeft, upstreamLeft, nil
}

func (e *fakeInspectorEndpoint) UpdateTargets(
	_ context.Context, targets []tunnel.InspectorTarget,
) error {
	e.mu.Lock()
	e.updatedTargets = append([]tunnel.InspectorTarget(nil), targets...)
	e.mu.Unlock()
	return nil
}

func (e *fakeInspectorEndpoint) Events() <-chan tunnel.InspectorEvent {
	return e.events
}

func (e *fakeInspectorEndpoint) Close() error {
	e.closeOnce.Do(func() { close(e.events) })
	return nil
}

func TestInspectorSessionPolicyEventsAndCleanup(t *testing.T) {
	server := NewServer(log.New(io.Discard, "", 0), time.Second)
	engine := &fakeInspectorEngine{}
	server.SetInspectorEngine(engine, "fake")
	if !server.Capabilities.Inspector ||
		server.Capabilities.Engine != "fake" ||
		len(server.Capabilities.Protocols) != 4 {
		t.Fatalf("unexpected capabilities %#v", server.Capabilities)
	}

	control := &controlSession{
		server:  server,
		version: tunnel.ProtocolV2,
		token:   gatewaySessionToken(4),
	}
	config := tunnel.InspectorConfig{
		MaxBodySize: 4096,
		Targets: []tunnel.InspectorTarget{{
			ID: "default/api:8080", Host: "api.default.svc", Port: 8080, Protocol: "http",
		}},
	}
	if err := server.startInspector(control, config); err != nil {
		t.Fatal(err)
	}
	if engine.sessionID == "" || engine.sessionID == string(control.token[:]) {
		t.Fatalf("unsafe Inspector session ID %q", engine.sessionID)
	}

	connection, matched, err := control.dialInspector(
		context.Background(),
		tunnel.OpenRequest{
			Command: tunnel.CommandTCP, Host: "API.DEFAULT.SVC", Port: 8080,
		},
		"10.0.0.8:8080",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !matched || connection == nil {
		t.Fatal("selected target did not enter Inspector")
	}
	_ = connection.Close()
	if engine.endpoint.dialedAddress != "10.0.0.8:8080" {
		t.Fatalf("dialed address %q", engine.endpoint.dialedAddress)
	}

	if connection, matched, err := control.dialInspector(
		context.Background(),
		tunnel.OpenRequest{
			Command: tunnel.CommandTCP, Host: "other.default.svc", Port: 8080,
		},
		"10.0.0.9:8080",
	); err != nil || matched || connection != nil {
		t.Fatalf("unselected target matched: conn=%v matched=%t err=%v", connection, matched, err)
	}

	eventsWriter, eventsReader := net.Pipe()
	defer eventsReader.Close()
	if err := control.attachEvents(eventsWriter); err != nil {
		t.Fatal(err)
	}
	wantEvent := tunnel.InspectorEvent{
		Version: tunnel.InspectorEventVersion1,
		Type:    tunnel.InspectorEventHeaders,
		FlowID:  "flow-1",
		Payload: []byte(`{"method":"GET","path":"/health"}`),
	}
	engine.endpoint.events <- wantEvent
	_ = eventsReader.SetReadDeadline(time.Now().Add(3 * time.Second))
	gotEvent, err := tunnel.ReadInspectorEvent(eventsReader)
	if err != nil {
		t.Fatal(err)
	}
	if gotEvent.FlowID != wantEvent.FlowID ||
		string(gotEvent.Payload) != string(wantEvent.Payload) {
		t.Fatalf("got %#v, want %#v", gotEvent, wantEvent)
	}

	targets := []tunnel.InspectorTarget{{
		ID: "default/web:80", Host: "web.default.svc", Port: 80, Protocol: "http",
	}}
	if err := server.updateInspectorTargets(control, targets); err != nil {
		t.Fatal(err)
	}
	if len(engine.endpoint.updatedTargets) != 1 {
		t.Fatalf("updated targets %#v", engine.endpoint.updatedTargets)
	}
	if err := server.stopInspector(control); err != nil {
		t.Fatal(err)
	}
	if connection, matched, err := control.dialInspector(
		context.Background(),
		tunnel.OpenRequest{
			Command: tunnel.CommandTCP, Host: "web.default.svc", Port: 80,
		},
		"10.0.0.10:80",
	); err != nil || matched || connection != nil {
		t.Fatalf("stopped Inspector still matched: conn=%v matched=%t err=%v", connection, matched, err)
	}
}

func TestReverseInspectorTargetMatchesServiceRegistration(t *testing.T) {
	target := tunnel.InspectorTarget{
		ID: "default/api", Host: "api.default.svc", Port: 8080, Protocol: "http",
	}
	endpoint := &fakeInspectorEndpoint{}
	control := &controlSession{inspector: &inspectorSession{
		endpoint: endpoint,
		targets:  inspectorTargetMap([]tunnel.InspectorTarget{target}),
	}}
	got, gotEndpoint, ok := control.reverseInspectorTarget("default/api:tcp:8080")
	if !ok || got.ID != target.ID || gotEndpoint != endpoint {
		t.Fatalf("reverse target=%+v endpoint=%T matched=%v", got, gotEndpoint, ok)
	}
	if _, _, ok := control.reverseInspectorTarget("default/api:udp:8080"); ok {
		t.Fatal("UDP reverse registration unexpectedly matched Inspector")
	}
}

func TestInspectorPolicyIsIsolatedByControlSession(t *testing.T) {
	server := NewServer(log.New(io.Discard, "", 0), time.Second)
	server.SetInspectorEngine(&fakeInspectorEngine{}, "fake")
	first := &controlSession{
		server: server, version: tunnel.ProtocolV2, token: gatewaySessionToken(1),
	}
	second := &controlSession{
		server: server, version: tunnel.ProtocolV2, token: gatewaySessionToken(2),
	}
	config := tunnel.InspectorConfig{Targets: []tunnel.InspectorTarget{{
		ID: "api", Host: "api.default.svc", Port: 80, Protocol: "http",
	}}}
	if err := server.startInspector(first, config); err != nil {
		t.Fatal(err)
	}
	defer server.stopInspector(first)

	connection, matched, err := second.dialInspector(
		context.Background(),
		tunnel.OpenRequest{
			Command: tunnel.CommandTCP, Host: "api.default.svc", Port: 80,
		},
		"10.0.0.8:80",
	)
	if err != nil || matched || connection != nil {
		t.Fatalf("second session used first policy: conn=%v matched=%t err=%v", connection, matched, err)
	}
}
