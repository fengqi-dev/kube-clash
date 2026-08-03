package inspectoragent

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

func TestHTTP2GRPCStreamingEvents(t *testing.T) {
	upstreamClient, upstreamServer := net.Pipe()
	defer upstreamClient.Close()
	defer upstreamServer.Close()
	go (&http2.Server{}).ServeConn(upstreamServer, &http2.ServeConnOpts{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, _ = io.Copy(io.Discard, request.Body)
			writer.Header().Set("Content-Type", "application/grpc")
			writer.Header().Set("Trailer", "Grpc-Status")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(grpcFrame([]byte("response-one")))
			_, _ = writer.Write(grpcFrame([]byte("response-two")))
			writer.Header().Set("Grpc-Status", "0")
		}),
	})

	session := &agentSession{
		id: "http2-test", maxBodySize: 1024,
		events: make(chan tunnel.InspectorEvent, 32),
		done:   make(chan struct{}),
	}
	agentClient, application := net.Pipe()
	defer agentClient.Close()
	defer application.Close()
	target := tunnel.InspectorTarget{
		ID: "grpc", Host: "service.default.svc", Port: 443,
		Protocol: "grpc", CaptureBody: true,
	}
	go serveHTTP2Connection(session, agentClient, upstreamClient, target, nil)

	transport := &http2.Transport{}
	connection, err := transport.NewClientConn(application)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	body := append(grpcFrame([]byte("request-one")), grpcFrame([]byte("request-two"))...)
	request, err := http.NewRequestWithContext(
		context.Background(), http.MethodPost,
		"http://service.default.svc/example.Stream/Chat",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/grpc")
	request.Header.Set("Authorization", "secret")
	response, err := connection.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if string(payload) != string(append(
		grpcFrame([]byte("response-one")), grpcFrame([]byte("response-two"))...,
	)) {
		t.Fatalf("response payload changed: %x", payload)
	}

	var starts, ends, requestMessages, responseMessages int
	deadline := time.After(5 * time.Second)
	for ends == 0 {
		select {
		case event := <-session.events:
			switch event.Type {
			case tunnel.InspectorEventFlowStart:
				starts++
				if !bytes.Contains(event.Payload, []byte(`"httpVersion":"HTTP/2.0"`)) ||
					!bytes.Contains(event.Payload, []byte(`"protocol":"grpc"`)) {
					t.Fatalf("unexpected start payload %s", event.Payload)
				}
			case tunnel.InspectorEventHeaders:
				if bytes.Contains(event.Payload, []byte("secret")) {
					t.Fatalf("sensitive metadata leaked: %s", event.Payload)
				}
			case tunnel.InspectorEventGRPCMessage:
				var value struct {
					Direction string `json:"direction"`
				}
				if err := json.Unmarshal(event.Payload, &value); err != nil {
					t.Fatal(err)
				}
				if value.Direction == "request" {
					requestMessages++
				} else if value.Direction == "response" {
					responseMessages++
				}
			case tunnel.InspectorEventFlowEnd:
				ends++
				if !bytes.Contains(event.Payload, []byte(`"grpcStatus":"0"`)) {
					t.Fatalf("missing grpc status: %s", event.Payload)
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for HTTP/2 Inspector events")
		}
	}
	if starts != 1 || requestMessages != 2 || responseMessages != 2 {
		t.Fatalf(
			"events start=%d requestMessages=%d responseMessages=%d",
			starts, requestMessages, responseMessages,
		)
	}
}

func TestGRPCFrameObserverHandlesSplitAndMultipleFrames(t *testing.T) {
	session := &agentSession{
		events: make(chan tunnel.InspectorEvent, 8),
	}
	emitter := &flowEmitter{session: session, flowID: "flow", next: 1}
	observer := &grpcFrameObserver{
		emitter: emitter, direction: "request", limit: 1024,
	}
	value := append(grpcFrame([]byte("one")), grpcFrame([]byte("two"))...)
	observer.write(value[:2])
	observer.write(value[2:7])
	observer.write(value[7:])
	if len(session.events) != 2 {
		t.Fatalf("message events=%d want 2", len(session.events))
	}
}

func TestGRPCDescriptorDecodesMessages(t *testing.T) {
	encoded := testGRPCDescriptorSet(t)
	descriptor := newGRPCDescriptor(encoded)
	decoded := descriptor.decode(
		"/example.Echo/Chat", "request", []byte{0x0a, 0x03, 'o', 'n', 'e'},
	)
	if string(decoded) != `{"name":"one"}` {
		t.Fatalf("decoded=%s", decoded)
	}
}

func TestGRPCFrameObserverDecodesGZIPMessage(t *testing.T) {
	session := &agentSession{events: make(chan tunnel.InspectorEvent, 4)}
	observer := &grpcFrameObserver{
		emitter:   &flowEmitter{session: session, flowID: "gzip", next: 1},
		direction: "request", limit: 1024, path: "/example.Echo/Chat",
		encoding: "gzip", descriptor: newGRPCDescriptor(testGRPCDescriptorSet(t)),
	}
	observer.write(grpcCompressedFrame(t, []byte{0x0a, 0x03, 'o', 'n', 'e'}))
	select {
	case event := <-session.events:
		if !bytes.Contains(event.Payload, []byte(`"encoding":"gzip"`)) ||
			!bytes.Contains(event.Payload, []byte(`"decoded":{"name":"one"}`)) {
			t.Fatalf("compressed message payload = %s", event.Payload)
		}
	default:
		t.Fatal("compressed gRPC message event was not emitted")
	}
}

func TestAuthorizeTargetRestrictsServiceAddresses(t *testing.T) {
	target := tunnel.InspectorTarget{
		ID: "api", Host: "api.default.svc", Port: 443, Protocol: "https",
		Addresses: []string{"10.96.0.10"},
	}
	session := &agentSession{
		targets: inspectorTargets([]tunnel.InspectorTarget{target}),
	}
	if _, err := session.authorizeTarget(target, "10.96.0.10:443"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.authorizeTarget(target, "10.96.0.11:443"); err == nil {
		t.Fatal("expected a different private Service IP to be rejected")
	}
}

func TestHTTPModePassesThroughUnknownTCP(t *testing.T) {
	agentClient, application := net.Pipe()
	agentUpstream, targetServer := net.Pipe()
	defer application.Close()
	defer targetServer.Close()
	session := &agentSession{events: make(chan tunnel.InspectorEvent, 2)}
	go serveHTTPConnection(
		session, agentClient, bufio.NewReader(agentClient), agentUpstream,
		tunnel.InspectorTarget{
			ID: "mixed-port", Host: "mixed.default.svc", Port: 8080,
			Protocol: "http",
		},
		nil,
	)
	request := []byte("*1\r\n$4\r\nPING\r\n")
	targetDone := make(chan error, 1)
	go func() {
		received := make([]byte, len(request))
		if _, err := io.ReadFull(targetServer, received); err != nil {
			targetDone <- err
			return
		}
		if !bytes.Equal(received, request) {
			targetDone <- fmt.Errorf("passthrough request changed: %q", received)
			return
		}
		_, err := targetServer.Write([]byte("+PONG\r\n"))
		targetDone <- err
	}()
	if _, err := application.Write(request); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("+PONG\r\n"))
	if _, err := io.ReadFull(application, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != "+PONG\r\n" {
		t.Fatalf("passthrough response = %q", response)
	}
	if err := <-targetDone; err != nil {
		t.Fatal(err)
	}
	if len(session.events) != 0 {
		t.Fatalf("unknown protocol emitted %d events", len(session.events))
	}
}

func testGRPCDescriptorSet(t *testing.T) []byte {
	t.Helper()
	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{{
		Name:    proto.String("echo.proto"),
		Package: proto.String("example"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Request"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name: proto.String("name"), Number: proto.Int32(1),
					Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:  descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				}},
			},
			{Name: proto.String("Response")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Echo"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Chat"),
				InputType:  proto.String(".example.Request"),
				OutputType: proto.String(".example.Response"),
			}},
		}},
	}}}
	encoded, err := proto.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func grpcFrame(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}

func grpcCompressedFrame(t *testing.T, payload []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	frame := grpcFrame(compressed.Bytes())
	frame[0] = 1
	return frame
}
