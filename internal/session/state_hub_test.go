package session

import (
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

func TestStateHubPreservesEventsAndConnectedMetrics(t *testing.T) {
	events := []LogEvent{{Level: "INFO", Message: "connected"}}
	metrics := &singbox.Metrics{DownloadTotal: 42}
	hub := newStateHub(State{
		Phase:   PhaseConnected,
		Events:  events,
		Metrics: metrics,
	})

	hub.publish(State{Phase: PhaseConnected, Message: "inventory updated"})
	got := hub.snapshot()
	if len(got.Events) != 1 || got.Events[0].Message != "connected" {
		t.Fatalf("events were not preserved: %#v", got.Events)
	}
	if got.Metrics != metrics {
		t.Fatalf("metrics pointer = %p, want %p", got.Metrics, metrics)
	}

	hub.publish(State{Phase: PhaseIdle, Message: "disconnected"})
	if got := hub.snapshot(); got.Metrics != nil {
		t.Fatalf("idle state retained connected metrics: %#v", got.Metrics)
	}
}

func TestStatePublishingDoesNotWaitForRuntimeLock(t *testing.T) {
	manager := NewManager(&fakeProvider{})
	published := make(chan struct{}, 1)
	manager.Subscribe(func(State) {
		published <- struct{}{}
	})

	manager.mu.Lock()
	defer manager.mu.Unlock()
	go manager.publish(State{Phase: PhaseChecking, Message: "checking"})

	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("state publishing blocked on the runtime lifecycle lock")
	}
}

func TestStateHubCallsListenersOutsideItsLock(t *testing.T) {
	hub := newStateHub(State{Phase: PhaseIdle})
	published := make(chan State, 1)
	hub.subscribe(func(State) {
		published <- hub.snapshot()
	})

	go hub.publish(State{Phase: PhaseChecking, Message: "checking"})
	select {
	case got := <-published:
		if got.Phase != PhaseChecking {
			t.Fatalf("phase = %s, want %s", got.Phase, PhaseChecking)
		}
	case <-time.After(time.Second):
		t.Fatal("listener deadlocked while reading the published state")
	}
}
