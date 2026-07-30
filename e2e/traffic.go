//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/cluster"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
	"github.com/fengqi-dev/kube-loop/internal/socksbridge"
	"github.com/fengqi-dev/kube-loop/internal/traffic"
)

// trafficDataPlane runs the production-generated fixed SOCKS inbounds without
// the privileged TUN inbound. This lets feature E2E tests exercise their real
// sing-box routes while keeping the test process unprivileged.
type trafficDataPlane struct {
	endpoints singbox.TrafficEndpoints
}

func startTrafficDataPlane(
	t *testing.T,
	ctx context.Context,
	provider *cluster.Provider,
	gatewayAddress string,
) *trafficDataPlane {
	t.Helper()

	binary, err := (&singbox.Installer{}).Ensure(ctx)
	if err != nil {
		t.Fatalf("ensure sing-box: %v", err)
	}
	bridge, err := socksbridge.Listen(ctx, gatewayAddress)
	if err != nil {
		t.Fatalf("start SOCKS bridge: %v", err)
	}
	t.Cleanup(func() { _ = bridge.Close() })

	discovery, err := provider.Discover(ctx, kubeContext(), []string{echoNamespace})
	if err != nil {
		t.Fatalf("discover cluster network: %v", err)
	}
	bridgeHost, bridgePortText, err := net.SplitHostPort(bridge.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	bridgePort, err := strconv.Atoi(bridgePortText)
	if err != nil {
		t.Fatal(err)
	}
	reserved := reserveDataPlanePorts(t)
	const username = "kubeloop-e2e-traffic-user"
	const password = "kubeloop-e2e-traffic-password-0123456789"
	content, err := singbox.Generate(discovery, singbox.Options{
		BridgeHost:       bridgeHost,
		BridgePort:       bridgePort,
		ControllerPort:   reserved.controller,
		ControllerSecret: "kubeloop-e2e-controller-secret-0123456789",
		DNSPort:          reserved.dns,
		TrafficPorts:     reserved.traffic,
		TrafficUsername:  username,
		TrafficPassword:  password,
	})
	if err != nil {
		t.Fatalf("generate sing-box config: %v", err)
	}
	content = featureOnlyConfig(t, content)
	configPath := filepath.Join(t.TempDir(), "sing-box.json")
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	processCtx, stopProcess := context.WithCancel(ctx)
	command := exec.CommandContext(processCtx, binary, "run", "-c", configPath)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		stopProcess()
		t.Fatalf("start sing-box: %v", err)
	}
	done := make(chan struct{})
	var processErr error
	go func() {
		processErr = command.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		stopProcess()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
	})

	endpoints := trafficEndpoints(reserved.traffic, username, password)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if trafficEndpointsReady(endpoints) {
			break
		}
		select {
		case <-done:
			stopProcess()
			t.Fatalf("sing-box exited during startup: %v\n%s", processErr, output.String())
		default:
		}
		if time.Now().After(deadline) {
			stopProcess()
			t.Fatalf("sing-box traffic inbounds not ready\n%s", output.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	return &trafficDataPlane{endpoints: endpoints}
}

func featureOnlyConfig(t *testing.T, content []byte) []byte {
	t.Helper()
	var config map[string]any
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		singbox.PortForwardInbound:   true,
		singbox.ExchangeInbound:      true,
		singbox.PreviewInbound:       true,
		singbox.MirrorPrimaryInbound: true,
		singbox.MirrorShadowInbound:  true,
	}
	rawInbounds, ok := config["inbounds"].([]any)
	if !ok {
		t.Fatal("generated config has no inbounds")
	}
	inbounds := make([]any, 0, len(allowed))
	for _, raw := range rawInbounds {
		inbound, ok := raw.(map[string]any)
		if ok && allowed[fmt.Sprint(inbound["tag"])] {
			inbounds = append(inbounds, inbound)
		}
	}
	if len(inbounds) != len(allowed) {
		t.Fatalf("generated config has %d feature inbounds, want %d", len(inbounds), len(allowed))
	}
	config["inbounds"] = inbounds
	config["log"] = map[string]any{"disabled": true}
	delete(config, "experimental")
	result, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type dataPlanePorts struct {
	controller int
	dns        int
	traffic    singbox.TrafficInboundPorts
}

func reserveDataPlanePorts(t *testing.T) dataPlanePorts {
	t.Helper()
	listeners := make([]net.Listener, 0, 7)
	ports := make([]int, 0, 7)
	for range 7 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, listener)
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	for _, listener := range listeners {
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return dataPlanePorts{
		controller: ports[0],
		dns:        ports[1],
		traffic: singbox.TrafficInboundPorts{
			PortForward: ports[2], Exchange: ports[3], Preview: ports[4],
			MirrorPrimary: ports[5], MirrorShadow: ports[6],
		},
	}
}

func trafficEndpoints(
	ports singbox.TrafficInboundPorts, username, password string,
) singbox.TrafficEndpoints {
	endpoint := func(port int) singbox.TrafficEndpoint {
		return singbox.TrafficEndpoint{
			Address:  net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
			Username: username, Password: password,
		}
	}
	return singbox.TrafficEndpoints{
		PortForward:   endpoint(ports.PortForward),
		Exchange:      endpoint(ports.Exchange),
		Preview:       endpoint(ports.Preview),
		MirrorPrimary: endpoint(ports.MirrorPrimary),
		MirrorShadow:  endpoint(ports.MirrorShadow),
	}
}

func trafficEndpointsReady(endpoints singbox.TrafficEndpoints) bool {
	for _, endpoint := range []singbox.TrafficEndpoint{
		endpoints.PortForward, endpoints.Exchange, endpoints.Preview,
		endpoints.MirrorPrimary, endpoints.MirrorShadow,
	} {
		connection, err := net.DialTimeout("tcp", endpoint.Address, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = connection.Close()
	}
	return true
}

func (p *trafficDataPlane) dialer(endpoint singbox.TrafficEndpoint) traffic.Dialer {
	return traffic.Dialer{Endpoint: traffic.Endpoint{
		Address: endpoint.Address, Username: endpoint.Username, Password: endpoint.Password,
	}}
}
