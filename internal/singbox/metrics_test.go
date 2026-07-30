package singbox

import "testing"

func TestMapClashMetricsExtractsInboundTag(t *testing.T) {
	raw := clashConnections{Connections: []clashConnection{{ID: "connection-1"}}}
	raw.Connections[0].Metadata.Type = "socks/preview-in"
	raw.Connections[0].Chains = []string{LocalOutbound}
	metrics := mapClashMetrics(raw)
	if len(metrics.Connections) != 1 {
		t.Fatalf("connections = %d", len(metrics.Connections))
	}
	connection := metrics.Connections[0]
	if connection.Inbound != PreviewInbound {
		t.Fatalf("inbound = %q", connection.Inbound)
	}
	if connection.Outbound != LocalOutbound {
		t.Fatalf("outbound = %q", connection.Outbound)
	}
}
