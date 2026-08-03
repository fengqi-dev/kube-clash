package inspectoragent

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/gateway"
	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

type Client struct {
	SocketPath string
}

func (c *Client) StartSession(
	ctx context.Context, sessionID string, config tunnel.InspectorConfig,
) (gateway.InspectorEndpoint, error) {
	if err := c.roundTrip(ctx, request{
		Op: opStart, SessionID: sessionID, Config: &config,
	}); err != nil {
		return nil, err
	}
	endpoint := &Endpoint{
		client: c, sessionID: sessionID, events: make(chan tunnel.InspectorEvent, 256),
		done: make(chan struct{}),
	}
	go endpoint.readEvents()
	return endpoint, nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.roundTrip(ctx, request{Op: opPing})
}

func (c *Client) roundTrip(ctx context.Context, value request) error {
	connection, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	setConnectionDeadline(connection, ctx, 10*time.Second)
	if err := writeJSON(connection, value); err != nil {
		return err
	}
	return readResponse(bufio.NewReader(connection))
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	if c.SocketPath == "" {
		return nil, fmt.Errorf("Inspector Agent socket path is required")
	}
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("dial Inspector Agent: %w", err)
	}
	return connection, nil
}

type Endpoint struct {
	client    *Client
	sessionID string
	events    chan tunnel.InspectorEvent
	done      chan struct{}
	closeOnce sync.Once
}

func (e *Endpoint) DialContext(
	ctx context.Context, target tunnel.InspectorTarget, targetAddress string,
) (net.Conn, error) {
	connection, err := e.client.dial(ctx)
	if err != nil {
		return nil, err
	}
	setConnectionDeadline(connection, ctx, 10*time.Second)
	if err := writeJSON(connection, request{
		Op: opDial, SessionID: e.sessionID, Target: &target, TargetAddress: targetAddress,
	}); err != nil {
		_ = connection.Close()
		return nil, err
	}
	if err := readResponse(bufio.NewReader(connection)); err != nil {
		_ = connection.Close()
		return nil, err
	}
	_ = connection.SetDeadline(time.Time{})
	return connection, nil
}

func (e *Endpoint) UpdateTargets(
	ctx context.Context, targets []tunnel.InspectorTarget,
) error {
	return e.client.roundTrip(ctx, request{
		Op: opUpdate, SessionID: e.sessionID, Targets: targets,
	})
}

func (e *Endpoint) Events() <-chan tunnel.InspectorEvent {
	return e.events
}

func (e *Endpoint) Close() error {
	var result error
	e.closeOnce.Do(func() {
		close(e.done)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result = e.client.roundTrip(ctx, request{
			Op: opStop, SessionID: e.sessionID,
		})
	})
	return result
}

func setConnectionDeadline(
	connection net.Conn, ctx context.Context, fallback time.Duration,
) {
	deadline := time.Now().Add(fallback)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	_ = connection.SetDeadline(deadline)
}

func (e *Endpoint) readEvents() {
	defer close(e.events)
	connection, err := e.client.dial(context.Background())
	if err != nil {
		return
	}
	defer connection.Close()
	if err := writeJSON(connection, request{
		Op: opEvents, SessionID: e.sessionID,
	}); err != nil {
		return
	}
	reader := bufio.NewReader(connection)
	if err := readResponse(reader); err != nil {
		return
	}
	for {
		event, err := tunnel.ReadInspectorEvent(reader)
		if err != nil {
			return
		}
		select {
		case e.events <- event:
		case <-e.done:
			return
		default:
			if event.Type != tunnel.InspectorEventBody {
				select {
				case <-e.events:
				default:
				}
				select {
				case e.events <- event:
				default:
				}
			}
		}
	}
}
