package singbox

import (
	"net"
	"strconv"
	"testing"
)

func TestAvailableTCPUDPPortSupportsBothProtocols(t *testing.T) {
	port, err := availableTCPUDPPort()
	if err != nil {
		t.Fatal(err)
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	tcpListener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listen TCP on selected port: %v", err)
	}
	defer tcpListener.Close()
	udpListener, err := net.ListenPacket("udp", address)
	if err != nil {
		t.Fatalf("listen UDP on selected port: %v", err)
	}
	defer udpListener.Close()
}

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
	if endpoints.MirrorShadow.Username != TrafficUserMirrorShadow {
		t.Fatalf("mirror-shadow user = %q", endpoints.MirrorShadow.Username)
	}
	if endpoints.PortForward.Username != TrafficUserPortForward {
		t.Fatalf("port-forward user = %q", endpoints.PortForward.Username)
	}
}
