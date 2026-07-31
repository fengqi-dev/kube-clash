package singbox

import "testing"

func TestMapClashMetricsExtractsInboundTagAndFeature(t *testing.T) {
	raw := clashConnections{Connections: []clashConnection{{ID: "connection-1"}}}
	raw.Connections[0].Metadata.Type = "socks/traffic-in"
	raw.Connections[0].Metadata.User = TrafficUserPreview
	raw.Connections[0].Chains = []string{LocalOutbound}
	metrics := mapClashMetrics(raw)
	if len(metrics.Connections) != 1 {
		t.Fatalf("connections = %d", len(metrics.Connections))
	}
	connection := metrics.Connections[0]
	if connection.Inbound != TrafficInbound {
		t.Fatalf("inbound = %q", connection.Inbound)
	}
	if connection.Feature != TrafficUserPreview {
		t.Fatalf("feature = %q", connection.Feature)
	}
	if connection.Outbound != LocalOutbound {
		t.Fatalf("outbound = %q", connection.Outbound)
	}
}

func TestMapClashMetricsUsesProcessPath(t *testing.T) {
	raw := clashConnections{Connections: []clashConnection{{ID: "connection-1"}}}
	raw.Connections[0].Metadata.ProcessPath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome (alice)"
	metrics := mapClashMetrics(raw)
	if got := metrics.Connections[0].Process; got != "Google Chrome" {
		t.Fatalf("process = %q", got)
	}
}
