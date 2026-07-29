package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client talks to the privileged helper over a local socket/pipe.
type Client struct {
	Token string
	Dial  func(context.Context) (net.Conn, error)
}

func NewClient() (*Client, error) {
	token, err := ReadUserToken()
	if err != nil {
		return nil, err
	}
	return &Client{Token: token, Dial: dialHelper}, nil
}

func (c *Client) Ping(ctx context.Context) (Response, error) {
	return c.roundTrip(ctx, Request{Op: OpPing})
}

func (c *Client) Status(ctx context.Context) (Response, error) {
	return c.roundTrip(ctx, Request{Op: OpStatus})
}

func (c *Client) Start(ctx context.Context, workDir, binaryPath string) (Response, error) {
	return c.roundTrip(ctx, Request{
		Op: OpStart, WorkDir: workDir, BinaryPath: binaryPath,
	})
}

func (c *Client) Stop(ctx context.Context, workDir string) (Response, error) {
	return c.roundTrip(ctx, Request{Op: OpStop, WorkDir: workDir})
}

func (c *Client) roundTrip(ctx context.Context, request Request) (Response, error) {
	if c.Token == "" {
		return Response{}, fmt.Errorf("helper token is required")
	}
	request.Token = c.Token
	dial := c.Dial
	if dial == nil {
		dial = dialHelper
	}
	conn, err := dial(ctx)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(request); err != nil {
		return Response{}, fmt.Errorf("write helper request: %w", err)
	}
	reader := bufio.NewReader(conn)
	decoder := json.NewDecoder(reader)
	var response Response
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("read helper response: %w", err)
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "helper request failed"
		}
		return response, fmt.Errorf("%s", response.Error)
	}
	return response, nil
}

// Probe returns whether the helper service answers ping.
func Probe(ctx context.Context) (Response, error) {
	client, err := NewClient()
	if err != nil {
		return Response{}, err
	}
	return client.Ping(ctx)
}
