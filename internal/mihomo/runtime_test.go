package mihomo

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProcessSnapshot(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/connections" {
			t.Fatalf("unexpected request path %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatalf("unexpected authorization header %q", request.Header.Get("Authorization"))
		}
		body := `{
			"downloadTotal": 2048,
			"uploadTotal": 1024,
			"memory": 4096,
			"connections": [{
				"id": "connection-1",
				"metadata": {
					"network": "tcp",
					"type": "Tun",
					"destinationIP": "10.96.0.1",
					"destinationPort": "443",
					"process": "curl"
				},
				"upload": 100,
				"download": 200,
				"chains": ["KUBERNETES"],
				"rule": "IPCIDR"
			}]
		}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Request:    request,
		}, nil
	})}
	process := &Process{
		controllerAddress: "127.0.0.1:9090",
		controllerSecret:  "test-secret",
		httpClient:        client,
	}
	metrics, err := process.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.DownloadTotal != 2048 || len(metrics.Connections) != 1 {
		t.Fatalf("unexpected metrics: %#v", metrics)
	}
	connection := metrics.Connections[0]
	if connection.Metadata.DestinationPort != "443" || connection.Metadata.Process != "curl" {
		t.Fatalf("unexpected connection: %#v", connection)
	}
}

func TestTUNStartupStatus(t *testing.T) {
	tests := []struct {
		name    string
		log     string
		ready   bool
		wantErr string
	}{
		{name: "pending", log: "RESTful API listening at: 127.0.0.1:9090"},
		{
			name:  "ready",
			log:   `[TUN] Tun adapter listening at: utun9([198.18.0.1/30]), mtu: 1500, auto route: true`,
			ready: true,
		},
		{
			name:  "listener ready",
			log:   `Tun[KubeClash] proxy listening at: utun6([198.19.0.1/30],[]), mtu: 9000`,
			ready: true,
		},
		{
			name:    "failed",
			log:     "level=error msg=\"Start TUN listening error: configure tun interface: Connect: operation not permitted\"\n",
			wantErr: "operation not permitted",
		},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			ready, err := tunStartupStatus(item.log)
			if ready != item.ready {
				t.Fatalf("ready = %v, want %v", ready, item.ready)
			}
			if item.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if item.wantErr != "" && (err == nil || !strings.Contains(err.Error(), item.wantErr)) {
				t.Fatalf("error = %v, want containing %q", err, item.wantErr)
			}
		})
	}
}
