package session

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/fengqi-dev/kube-loop/internal/singbox"
	"github.com/fengqi-dev/kube-loop/internal/traffic"
)

type stubNetDialer struct {
	conn net.Conn
}

func (d stubNetDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return d.conn, nil
}

func TestMergeTrafficTrackerDyesAndInjects(t *testing.T) {
	tracker := traffic.NewTracker()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}()
	raw, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	dialer := traffic.TrackedDialer{
		Inner:   stubNetDialer{conn: raw},
		Feature: singbox.TrafficUserMirrorShadow,
		Tracker: tracker,
	}
	conn, err := dialer.DialContext(context.Background(), "tcp", "127.0.0.1:8000")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}

	live := tracker.Snapshot()
	if len(live) != 1 {
		t.Fatalf("live = %d", len(live))
	}
	_, port, err := net.SplitHostPort(live[0].Source)
	if err != nil {
		t.Fatal(err)
	}

	metrics := singbox.Metrics{
		Connections: []singbox.Connection{{
			ID:      "clash-1",
			Network: "tcp",
			Source:  net.JoinHostPort("127.0.0.1", port),
			Inbound: singbox.TrafficInbound,
		}},
	}
	merged := mergeTrafficTracker(metrics, tracker)
	if merged.Connections[0].Feature != singbox.TrafficUserMirrorShadow {
		t.Fatalf("feature = %q", merged.Connections[0].Feature)
	}

	merged = mergeTrafficTracker(singbox.Metrics{}, tracker)
	if len(merged.Connections) != 1 {
		t.Fatalf("injected = %d", len(merged.Connections))
	}
	if merged.Connections[0].Feature != singbox.TrafficUserMirrorShadow {
		t.Fatalf("injected feature = %q", merged.Connections[0].Feature)
	}
	if merged.Connections[0].Outbound != singbox.LocalOutbound {
		t.Fatalf("outbound = %q", merged.Connections[0].Outbound)
	}
}
