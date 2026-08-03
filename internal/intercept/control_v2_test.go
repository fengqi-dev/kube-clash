package intercept

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

func TestDialControlFallsBackToKCG1(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		unsupported, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		var kcg2Prefix [5]byte
		_, _ = io.ReadFull(unsupported, kcg2Prefix[:])
		_ = unsupported.Close()

		legacy, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer legacy.Close()
		command, readErr := tunnel.ReadSessionHeader(legacy)
		if readErr != nil || command != tunnel.CommandControl {
			return
		}
		if tunnel.WriteStatus(legacy, nil) != nil {
			return
		}
		_, _ = io.Copy(io.Discard, legacy)
	}()

	token, err := tunnel.NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	client, err := dialControl(
		context.Background(), listener.Addr().String(), token, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !client.token.IsZero() {
		t.Fatal("fallback retained the KCG2 token")
	}
	if client.capabilities.ProtocolVersion != int(tunnel.ProtocolV1) ||
		client.capabilities.Inspector {
		t.Fatalf("unexpected fallback capabilities %#v", client.capabilities)
	}
	_ = client.close()
	<-serverDone
}
