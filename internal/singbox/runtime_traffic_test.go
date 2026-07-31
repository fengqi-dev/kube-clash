package singbox

import "testing"

func TestTrafficEndpointsShareListenAndDyeUsers(t *testing.T) {
	endpoints := trafficEndpoints(TrafficInboundPorts{Listen: 18081}, "password-32-chars-minimum-length!!")
	if err := endpoints.Validate(); err != nil {
		t.Fatal(err)
	}
	if endpoints.Exchange.Address != endpoints.Preview.Address {
		t.Fatalf("exchange/preview addresses differ: %q vs %q", endpoints.Exchange.Address, endpoints.Preview.Address)
	}
	if endpoints.Exchange.Username != TrafficUserExchange {
		t.Fatalf("exchange user = %q", endpoints.Exchange.Username)
	}
	if endpoints.Preview.Username != TrafficUserPreview {
		t.Fatalf("preview user = %q", endpoints.Preview.Username)
	}
	if endpoints.Exchange.Username == endpoints.Preview.Username {
		t.Fatal("exchange and preview must use distinct auth_user dyes")
	}
}
