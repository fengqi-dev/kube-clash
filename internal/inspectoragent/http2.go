package inspectoragent

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

type flowEmitter struct {
	session *agentSession
	flowID  string
	mu      sync.Mutex
	next    uint64
	ended   bool
}

func (e *flowEmitter) emit(eventType byte, payload any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ended {
		return
	}
	sequence := e.next
	e.next++
	emitJSON(e.session, tunnel.InspectorEvent{
		Version:  tunnel.InspectorEventVersion1,
		Type:     eventType,
		FlowID:   e.flowID,
		Sequence: sequence,
	}, payload)
}

func (e *flowEmitter) end(payload any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ended {
		return
	}
	sequence := e.next
	e.next++
	emitJSON(e.session, tunnel.InspectorEvent{
		Version:  tunnel.InspectorEventVersion1,
		Type:     tunnel.InspectorEventFlowEnd,
		FlowID:   e.flowID,
		Sequence: sequence,
	}, payload)
	e.ended = true
}

type observedBody struct {
	source   io.ReadCloser
	capture  *bodyCapture
	grpc     *grpcFrameObserver
	finished sync.Once
	onDone   func(*bodyCapture)
}

func (b *observedBody) Read(value []byte) (int, error) {
	read, err := b.source.Read(value)
	if read > 0 {
		if b.capture != nil {
			b.capture.capture(value[:read])
		}
		if b.grpc != nil {
			b.grpc.write(value[:read])
		}
	}
	if err != nil {
		b.finish()
	}
	return read, err
}

func (b *observedBody) Close() error {
	err := b.source.Close()
	b.finish()
	return err
}

func (b *observedBody) finish() {
	b.finished.Do(func() {
		if b.onDone != nil {
			b.onDone(b.capture)
		}
	})
}

func (c *bodyCapture) capture(value []byte) {
	remaining := c.limit - int64(c.buffer.Len())
	if remaining > 0 {
		captured := int64(len(value))
		if captured > remaining {
			captured = remaining
		}
		_, _ = c.buffer.Write(value[:captured])
	}
	if int64(len(value)) > remaining {
		c.truncated = true
	}
}

type grpcFrameObserver struct {
	emitter    *flowEmitter
	direction  string
	limit      int64
	buffer     []byte
	index      uint64
	path       string
	encoding   string
	descriptor *grpcDescriptor
	disabled   bool
}

func (o *grpcFrameObserver) write(value []byte) {
	if o.disabled {
		return
	}
	o.buffer = append(o.buffer, value...)
	for len(o.buffer) >= 5 {
		size := int(uint32(o.buffer[1])<<24 |
			uint32(o.buffer[2])<<16 |
			uint32(o.buffer[3])<<8 |
			uint32(o.buffer[4]))
		if size > 1<<20 {
			o.index++
			o.emitter.emit(tunnel.InspectorEventGRPCMessage, map[string]any{
				"direction":  o.direction,
				"index":      o.index,
				"compressed": o.buffer[0] != 0,
				"size":       size,
				"truncated":  true,
				"error":      "gRPC message exceeds 1 MiB inspection limit",
			})
			o.buffer = nil
			o.disabled = true
			return
		}
		if len(o.buffer) < 5+size {
			return
		}
		o.index++
		payload := o.buffer[5 : 5+size]
		captured := payload
		truncated := false
		if o.limit >= 0 && int64(len(captured)) > o.limit {
			captured = captured[:o.limit]
			truncated = true
		}
		event := map[string]any{
			"direction":  o.direction,
			"index":      o.index,
			"compressed": o.buffer[0] != 0,
			"size":       size,
			"message":    captured,
			"truncated":  truncated,
		}
		decodedPayload := payload
		if o.buffer[0] != 0 {
			event["encoding"] = o.encoding
			if strings.EqualFold(o.encoding, "gzip") {
				if value, ok := decompressGRPCGZIP(payload); ok {
					decodedPayload = value
				} else {
					decodedPayload = nil
					event["decodeError"] = "invalid gzip-compressed gRPC message"
				}
			} else {
				decodedPayload = nil
			}
		}
		if decodedPayload != nil {
			if decoded := o.descriptor.decode(
				o.path, o.direction, decodedPayload,
			); len(decoded) > 0 {
				event["decoded"] = decoded
			}
		}
		o.emitter.emit(tunnel.InspectorEventGRPCMessage, event)
		o.buffer = o.buffer[5+size:]
	}
}

func decompressGRPCGZIP(payload []byte) ([]byte, bool) {
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, false
	}
	defer reader.Close()
	value, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil || len(value) > 1<<20 {
		return nil, false
	}
	return value, true
}

func serveHTTP2Connection(
	session *agentSession,
	client net.Conn,
	upstream net.Conn,
	target tunnel.InspectorTarget,
	tlsMetadata *tlsFlowMetadata,
) {
	transport := &http2.Transport{}
	upstreamConnection, err := transport.NewClientConn(upstream)
	if err != nil {
		return
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveHTTP2Request(
			session, upstreamConnection, writer, request, target, tlsMetadata,
		)
	})
	server := &http2.Server{}
	server.ServeConn(client, &http2.ServeConnOpts{Handler: handler})
	_ = upstreamConnection.Close()
}

func serveHTTP2Request(
	session *agentSession,
	upstream *http2.ClientConn,
	writer http.ResponseWriter,
	request *http.Request,
	target tunnel.InspectorTarget,
	tlsMetadata *tlsFlowMetadata,
) {
	startedAt := time.Now()
	emitter := &flowEmitter{
		session: session,
		flowID:  fmt.Sprintf("%s-%d", session.id, nextFlowID.Add(1)),
		next:    1,
	}
	protocol := target.Protocol
	isGRPC := strings.HasPrefix(
		strings.ToLower(request.Header.Get("Content-Type")),
		"application/grpc",
	)
	if isGRPC {
		protocol = "grpc"
	}
	start := map[string]any{
		"targetID":    target.ID,
		"protocol":    protocol,
		"httpVersion": "HTTP/2.0",
		"method":      request.Method,
		"authority":   request.Host,
		"path":        request.URL.EscapedPath(),
		"startedAt":   startedAt.UTC(),
		"source":      inspectorFlowSource(target),
	}
	if tlsMetadata != nil {
		start["tls"] = tlsMetadata
	}
	emitter.emit(tunnel.InspectorEventFlowStart, start)
	emitter.emit(tunnel.InspectorEventHeaders, map[string]any{
		"direction": "request",
		"headers":   redactHeaders(request.Header),
	})

	requestCapture := newHTTP2ObservedBody(
		session, emitter, request.Body, target, "request", isGRPC,
		request.URL.EscapedPath(), request.Header.Get("Grpc-Encoding"),
	)
	if requestCapture != nil {
		request.Body = requestCapture
	}
	outbound := request.Clone(request.Context())
	outbound.RequestURI = ""
	if outbound.URL.Scheme == "" {
		if tlsMetadata == nil {
			outbound.URL.Scheme = "http"
		} else {
			outbound.URL.Scheme = "https"
		}
	}
	if outbound.URL.Host == "" {
		outbound.URL.Host = request.Host
	}
	response, err := upstream.RoundTrip(outbound)
	if err != nil {
		emitter.emit(tunnel.InspectorEventError, map[string]any{
			"error": "HTTP/2 upstream request failed",
		})
		emitter.end(map[string]any{
			"error":      "HTTP/2 upstream request failed",
			"durationMs": time.Since(startedAt).Milliseconds(),
		})
		return
	}
	defer response.Body.Close()

	emitter.emit(tunnel.InspectorEventHeaders, map[string]any{
		"direction":  "response",
		"statusCode": response.StatusCode,
		"headers":    redactHeaders(response.Header),
	})
	copyHeaders(writer.Header(), response.Header)
	for name := range response.Trailer {
		writer.Header().Add("Trailer", name)
	}
	writer.WriteHeader(response.StatusCode)

	responseCapture := newHTTP2ObservedBody(
		session, emitter, response.Body, target, "response", isGRPC,
		request.URL.EscapedPath(), response.Header.Get("Grpc-Encoding"),
	)
	var responseBody io.Reader = response.Body
	if responseCapture != nil {
		responseBody = responseCapture
	}
	_, copyErr := io.Copy(writer, responseBody)
	if responseCapture != nil {
		responseCapture.finish()
	}
	if len(response.Trailer) > 0 {
		copyHeaders(writer.Header(), response.Trailer)
		emitter.emit(tunnel.InspectorEventHeaders, map[string]any{
			"direction":   "response",
			"trailers":    true,
			"headers":     redactHeaders(response.Trailer),
			"grpcStatus":  response.Trailer.Get("Grpc-Status"),
			"grpcMessage": response.Trailer.Get("Grpc-Message"),
		})
	}
	if copyErr != nil {
		emitter.emit(tunnel.InspectorEventError, map[string]any{
			"error": "HTTP/2 response relay failed",
		})
		emitter.end(map[string]any{
			"statusCode": response.StatusCode,
			"grpcStatus": response.Trailer.Get("Grpc-Status"),
			"error":      "HTTP/2 response relay failed",
			"durationMs": time.Since(startedAt).Milliseconds(),
		})
		return
	}
	emitter.end(map[string]any{
		"statusCode": response.StatusCode,
		"grpcStatus": response.Trailer.Get("Grpc-Status"),
		"durationMs": time.Since(startedAt).Milliseconds(),
		"truncated": captureTruncatedFromObserved(requestCapture) ||
			captureTruncatedFromObserved(responseCapture),
	})
}

func newHTTP2ObservedBody(
	session *agentSession,
	emitter *flowEmitter,
	source io.ReadCloser,
	target tunnel.InspectorTarget,
	direction string,
	grpc bool,
	path string,
	encoding string,
) *observedBody {
	if source == nil {
		return nil
	}
	var capture *bodyCapture
	if target.CaptureBody && session.maxBodySize > 0 {
		capture = &bodyCapture{limit: session.maxBodySize}
	}
	var observer *grpcFrameObserver
	if grpc {
		observer = &grpcFrameObserver{
			emitter: emitter, direction: direction, limit: session.maxBodySize,
			path: path, encoding: encoding,
			descriptor: newGRPCDescriptor(target.DescriptorSet),
		}
	}
	if capture == nil && observer == nil {
		return nil
	}
	return &observedBody{
		source:  source,
		capture: capture,
		grpc:    observer,
		onDone: func(value *bodyCapture) {
			if value != nil && value.buffer.Len() > 0 {
				emitter.emit(tunnel.InspectorEventBody, map[string]any{
					"direction": direction,
					"body":      value.buffer.Bytes(),
					"truncated": value.truncated,
				})
			}
		},
	}
}

func captureTruncatedFromObserved(body *observedBody) bool {
	return body != nil && body.capture != nil && body.capture.truncated
}

func copyHeaders(destination, source http.Header) {
	for name, values := range source {
		destination[name] = append([]string(nil), values...)
	}
}
