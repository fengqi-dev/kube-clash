package session

import (
	"context"
	"errors"
	"testing"

	"github.com/kube-clash/kube-clash/internal/cluster"
)

type fakeProvider struct {
	discovery cluster.Discovery
	err       error
}

func (f fakeProvider) Contexts() ([]cluster.ContextInfo, error) { return nil, nil }
func (f fakeProvider) Namespaces(context.Context, string) ([]string, error) {
	return []string{"default"}, nil
}
func (f fakeProvider) Discover(context.Context, string) (cluster.Discovery, error) {
	return f.discovery, f.err
}

func TestManagerPublishesReadyState(t *testing.T) {
	want := cluster.Discovery{PodCIDRs: []string{"10.244.0.0/16"}, ServiceIPs: []string{"10.96.0.1"}}
	manager := NewManager(fakeProvider{discovery: want})
	ready := make(chan State, 1)
	manager.Subscribe(func(state State) {
		if state.Phase == PhaseReady {
			ready <- state
		}
	})
	if err := manager.Connect(context.Background(), Request{Context: "dev"}); err != nil {
		t.Fatal(err)
	}
	state := <-ready
	if state.Discovery == nil || len(state.Discovery.PodCIDRs) != 1 {
		t.Fatalf("unexpected discovery: %#v", state.Discovery)
	}
}

func TestManagerPublishesErrorState(t *testing.T) {
	manager := NewManager(fakeProvider{err: errors.New("forbidden")})
	failed := make(chan State, 1)
	manager.Subscribe(func(state State) {
		if state.Phase == PhaseError {
			failed <- state
		}
	})
	if err := manager.Connect(context.Background(), Request{Context: "dev"}); err != nil {
		t.Fatal(err)
	}
	state := <-failed
	if state.Error != "forbidden" {
		t.Fatalf("unexpected error: %q", state.Error)
	}
}
