package intercept

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/gateway"
	"github.com/fengqi-dev/kube-loop/internal/tunnel"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type managerInspectorEngine struct {
	mu         sync.Mutex
	active     bool
	startCount int
	endpoint   *managerInspectorEndpoint
}

func (e *managerInspectorEngine) StartSession(
	_ context.Context, _ string, _ tunnel.InspectorConfig,
) (gateway.InspectorEndpoint, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active {
		return nil, fmt.Errorf("Inspector worker is already active")
	}
	e.active = true
	e.startCount++
	endpoint := &managerInspectorEndpoint{
		engine: e,
		events: make(chan tunnel.InspectorEvent, 4),
	}
	e.endpoint = endpoint
	return endpoint, nil
}

func (e *managerInspectorEngine) current() *managerInspectorEndpoint {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.endpoint
}

type managerInspectorEndpoint struct {
	engine    *managerInspectorEngine
	events    chan tunnel.InspectorEvent
	closeOnce sync.Once
}

func (e *managerInspectorEndpoint) DialContext(
	context.Context, tunnel.InspectorTarget, string,
) (net.Conn, error) {
	return nil, fmt.Errorf("not used")
}

func (e *managerInspectorEndpoint) BridgeContext(
	context.Context, tunnel.InspectorTarget,
) (net.Conn, net.Conn, error) {
	return nil, nil, fmt.Errorf("not used")
}

func (e *managerInspectorEndpoint) UpdateTargets(
	context.Context, []tunnel.InspectorTarget,
) error {
	return nil
}

func (e *managerInspectorEndpoint) Events() <-chan tunnel.InspectorEvent {
	return e.events
}

func (e *managerInspectorEndpoint) Close() error {
	e.closeOnce.Do(func() {
		e.engine.mu.Lock()
		e.engine.active = false
		e.engine.mu.Unlock()
		close(e.events)
	})
	return nil
}

func TestPrepareInspectorTargetsValidatesServiceIdentity(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api", Namespace: "default", UID: types.UID("service-v2"),
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:  "10.96.1.10",
			ClusterIPs: []string{"10.96.1.10", "fd00::10"},
			Ports: []corev1.ServicePort{{
				Name: "https", Port: 443, Protocol: corev1.ProtocolTCP,
			}},
		},
	}
	manager := NewManager(&fakeCluster{service: service})
	manager.active = true
	manager.contextName = "minikube"
	target := tunnel.InspectorTarget{
		ID: "api", Host: "api.default.svc", Port: 443, Protocol: "https",
		Namespace: "default", Service: "api", ServiceUID: "service-v1",
		Addresses: []string{"10.96.1.9"},
	}
	if _, err := manager.prepareInspectorTargets(
		context.Background(), []tunnel.InspectorTarget{target},
	); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("stale Service UID error = %v", err)
	}
	target.ServiceUID = "service-v2"
	prepared, err := manager.prepareInspectorTargets(
		context.Background(), []tunnel.InspectorTarget{target},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := prepared[0].Addresses; len(got) != 2 ||
		got[0] != "10.96.1.10" || got[1] != "fd00::10" {
		t.Fatalf("Service addresses = %v", got)
	}
}

func TestManagerInspectorLifecycleAndRecovery(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := gateway.NewServer(log.New(io.Discard, "", 0), time.Second)
	engine := &managerInspectorEngine{}
	server.SetInspectorEngine(engine, "test")
	go func() { _ = server.Serve(listener) }()

	manager := NewManager(nil)
	token, err := tunnel.NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.StartSession(
		context.Background(), "test", "10.0.0.1", listener.Addr().String(), token,
	); err != nil {
		t.Fatal(err)
	}
	defer manager.StopAll(context.Background())

	config := tunnel.InspectorConfig{
		MaxBodySize: 1024,
		Targets: []tunnel.InspectorTarget{{
			ID: "api", Host: "api.default.svc", Port: 80, Protocol: "http",
		}},
	}
	if err := manager.StartInspector(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if state := manager.InspectorState(); !state.Active ||
		state.MaxBodySize != config.MaxBodySize || len(state.Targets) != 1 {
		t.Fatalf("unexpected Inspector state %#v", state)
	}

	firstEvent := tunnel.InspectorEvent{
		Version: tunnel.InspectorEventVersion1,
		Type:    tunnel.InspectorEventFlowStart,
		FlowID:  "flow-before-recovery",
		Payload: json.RawMessage(`{"method":"GET"}`),
	}
	engine.current().events <- firstEvent
	select {
	case got := <-manager.InspectorEvents():
		if got.FlowID != firstEvent.FlowID {
			t.Fatalf("event flow ID = %q", got.FlowID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Inspector event")
	}

	manager.mu.Lock()
	oldEvents := manager.inspectorConn
	manager.mu.Unlock()
	if oldEvents == nil {
		t.Fatal("Inspector event connection is unavailable")
	}
	if err := oldEvents.Close(); err != nil {
		t.Fatal(err)
	}
	reconnectDeadline := time.Now().Add(8 * time.Second)
	for {
		manager.mu.Lock()
		reconnected := manager.inspectorConn != nil &&
			manager.inspectorConn != oldEvents
		manager.mu.Unlock()
		if reconnected {
			break
		}
		if time.Now().After(reconnectDeadline) {
			t.Fatal("Inspector event channel did not reconnect")
		}
		time.Sleep(25 * time.Millisecond)
	}
	reconnectedEvent := firstEvent
	reconnectedEvent.FlowID = "flow-after-event-reconnect"
	engine.current().events <- reconnectedEvent
	select {
	case got := <-manager.InspectorEvents():
		if got.FlowID != reconnectedEvent.FlowID {
			t.Fatalf("reconnected event flow ID = %q", got.FlowID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for reconnected Inspector event")
	}

	if err := manager.control.close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-manager.ControlLost():
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for control loss")
	}
	if err := manager.RecoverControl(context.Background()); err != nil {
		t.Fatalf("recover control: %v", err)
	}
	if state := manager.InspectorState(); !state.Active {
		t.Fatalf("Inspector was not restored: %#v", state)
	}
	engine.mu.Lock()
	startCount := engine.startCount
	engine.mu.Unlock()
	if startCount != 2 {
		t.Fatalf("Inspector start count = %d, want 2", startCount)
	}

	targets := []tunnel.InspectorTarget{{
		ID: "web", Host: "web.default.svc", Port: 8080, Protocol: "http",
	}}
	if err := manager.UpdateInspectorTargets(targets); err != nil {
		t.Fatal(err)
	}
	if state := manager.InspectorState(); len(state.Targets) != 1 ||
		state.Targets[0].Host != "web.default.svc" {
		t.Fatalf("targets were not updated: %#v", state)
	}
	if err := manager.StopInspector(); err != nil {
		t.Fatal(err)
	}
	if state := manager.InspectorState(); state.Active {
		t.Fatalf("Inspector is still active: %#v", state)
	}
}
