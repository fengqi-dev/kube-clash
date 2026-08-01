package session

import (
	"context"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

func TestControlRecoveryDelay(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 0},
		{attempt: 1, want: time.Second},
		{attempt: 2, want: time.Second},
		{attempt: 3, want: 2 * time.Second},
		{attempt: 4, want: 2 * time.Second},
	}
	for _, test := range tests {
		if got := controlRecoveryDelay(test.attempt); got != test.want {
			t.Fatalf("attempt %d delay = %s, want %s", test.attempt, got, test.want)
		}
	}
}

func TestUpdateCoreMetricsPublishesOnlyWhileConnected(t *testing.T) {
	manager := NewManager(&fakeProvider{})
	process := &fakeProcess{done: make(chan struct{})}
	published := make(chan *singbox.Metrics, 1)
	manager.SubscribeMetrics(func(metrics *singbox.Metrics) {
		published <- metrics
	})

	manager.publish(State{Phase: PhaseConnected, Message: "connected"})
	manager.updateCoreMetrics(context.Background(), process)
	select {
	case metrics := <-published:
		if metrics == nil {
			t.Fatal("published nil metrics")
		}
	case <-time.After(time.Second):
		t.Fatal("connected metrics were not published")
	}

	manager.publish(State{Phase: PhaseIdle, Message: "disconnected"})
	manager.updateCoreMetrics(context.Background(), process)
	select {
	case metrics := <-published:
		t.Fatalf("published metrics while disconnected: %#v", metrics)
	default:
	}
}
