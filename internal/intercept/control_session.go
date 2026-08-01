package intercept

import (
	"context"
	"fmt"
	"sync"
)

// controlSession owns the current Gateway control channel and its recovery
// state. Manager.mu protects state-changing calls.
type controlSession struct {
	client     *controlClient
	lost       chan struct{}
	recovering bool
	generation uint64
	onReady    func(interceptID string, network byte, streamID uint64)
}

func newControlSession(
	onReady func(interceptID string, network byte, streamID uint64),
) *controlSession {
	return &controlSession{onReady: onReady}
}

func (s *controlSession) connect(ctx context.Context, address string) error {
	client, lost, err := s.open(ctx, address)
	if err != nil {
		return err
	}
	s.generation++
	s.attach(client, lost)
	return nil
}

func (s *controlSession) open(
	ctx context.Context,
	address string,
) (*controlClient, chan struct{}, error) {
	lost := make(chan struct{})
	client, err := dialControl(
		ctx,
		address,
		s.onReady,
		sync.OnceFunc(func() { close(lost) }),
	)
	if err != nil {
		return nil, nil, err
	}
	return client, lost, nil
}

func (s *controlSession) redial(
	ctx context.Context,
	address string,
	registrations []controlRegistration,
) (*controlClient, chan struct{}, error) {
	client, lost, err := s.open(ctx, address)
	if err != nil {
		return nil, nil, err
	}
	for _, registration := range registrations {
		if err := client.register(
			registration.id,
			registration.network,
			registration.listenPort,
		); err != nil {
			_ = client.close()
			return nil, nil, fmt.Errorf("re-register %s: %w", registration.id, err)
		}
	}
	return client, lost, nil
}

func (s *controlSession) attach(client *controlClient, lost chan struct{}) {
	s.client = client
	s.lost = lost
	s.recovering = false
}

func (s *controlSession) ready() bool {
	return s.client != nil && !s.recovering
}

func (s *controlSession) current() *controlClient {
	return s.client
}

func (s *controlSession) snapshot() (*controlClient, uint64, bool) {
	return s.client, s.generation, s.ready()
}

func (s *controlSession) matches(client *controlClient, generation uint64) bool {
	return s.client == client &&
		s.generation == generation &&
		s.ready() &&
		!s.client.closed.Load()
}

func (s *controlSession) lostSignal() <-chan struct{} {
	return s.lost
}

func (s *controlSession) beginRecovery() (*controlClient, uint64) {
	s.generation++
	s.recovering = true
	old := s.client
	s.client = nil
	return old, s.generation
}

func (s *controlSession) finishRecovery(
	generation uint64,
	client *controlClient,
	lost chan struct{},
) bool {
	if generation != s.generation || !s.recovering {
		return false
	}
	s.attach(client, lost)
	return true
}

func (s *controlSession) recoveryFailed(generation uint64) {
	if generation == s.generation {
		s.recovering = false
	}
}

func (s *controlSession) stop() *controlClient {
	s.generation++
	client := s.client
	s.client = nil
	s.recovering = false
	return client
}

// close is used by package tests to simulate an unexpected control loss.
func (s *controlSession) close() error {
	if s.client == nil {
		return nil
	}
	return s.client.close()
}
