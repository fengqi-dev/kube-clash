package inspectoragent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

var nextFlowID atomic.Uint64

type bodyCapture struct {
	source    io.ReadCloser
	limit     int64
	buffer    bytes.Buffer
	truncated bool
}

func (c *bodyCapture) Read(value []byte) (int, error) {
	read, err := c.source.Read(value)
	if read > 0 {
		remaining := c.limit - int64(c.buffer.Len())
		if remaining > 0 {
			captured := int64(read)
			if captured > remaining {
				captured = remaining
			}
			_, _ = c.buffer.Write(value[:captured])
		}
		if int64(read) > remaining {
			c.truncated = true
		}
	}
	return read, err
}

func (c *bodyCapture) Close() error {
	return c.source.Close()
}

func serveHTTPConnection(
	session *agentSession,
	client net.Conn,
	clientReader *bufio.Reader,
	upstream net.Conn,
	target tunnel.InspectorTarget,
	tlsMetadata *tlsFlowMetadata,
) {
	if target.Protocol == "http2" || target.Protocol == "grpc" {
		serveHTTP2Connection(
			session,
			&bufferedConn{Conn: client, reader: clientReader},
			upstream,
			target,
			tlsMetadata,
		)
		return
	}
	if target.Protocol == "http" {
		_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
		prefix, err := clientReader.Peek(8)
		_ = client.SetReadDeadline(time.Time{})
		if err != nil || !isHTTP1Prefix(prefix) {
			relayInspectorConnections(
				&bufferedConn{Conn: client, reader: clientReader}, upstream,
			)
			return
		}
		if string(prefix[:3]) == "PRI" {
			serveHTTP2Connection(
				session,
				&bufferedConn{Conn: client, reader: clientReader},
				upstream,
				target,
				tlsMetadata,
			)
			return
		}
	}
	upstreamReader := bufio.NewReader(upstream)
	for {
		request, err := http.ReadRequest(clientReader)
		if err != nil {
			return
		}
		startedAt := time.Now()
		flowID := fmt.Sprintf("%s-%d", session.id, nextFlowID.Add(1))
		sequence := uint64(1)
		start := map[string]any{
			"targetID":    target.ID,
			"protocol":    target.Protocol,
			"httpVersion": request.Proto,
			"method":      request.Method,
			"authority":   request.Host,
			"path":        request.URL.EscapedPath(),
			"startedAt":   startedAt.UTC(),
			"source":      inspectorFlowSource(target),
		}
		if tlsMetadata != nil {
			start["tls"] = tlsMetadata
		}
		emitJSON(session, tunnel.InspectorEvent{
			Version:  tunnel.InspectorEventVersion1,
			Type:     tunnel.InspectorEventFlowStart,
			FlowID:   flowID,
			Sequence: sequence,
		}, start)
		sequence++
		emitJSON(session, tunnel.InspectorEvent{
			Version:  tunnel.InspectorEventVersion1,
			Type:     tunnel.InspectorEventHeaders,
			FlowID:   flowID,
			Sequence: sequence,
		}, map[string]any{
			"direction": "request",
			"headers":   redactHeaders(request.Header),
		})
		sequence++

		var requestCapture *bodyCapture
		if target.CaptureBody && session.maxBodySize > 0 && request.Body != nil {
			requestCapture = &bodyCapture{source: request.Body, limit: session.maxBodySize}
			request.Body = requestCapture
		}
		request.RequestURI = ""
		if err := request.Write(upstream); err != nil {
			emitFlowError(session, flowID, sequence, "write upstream request")
			return
		}
		if request.Body != nil {
			_ = request.Body.Close()
		}
		if target.CaptureBody && requestCapture != nil && requestCapture.buffer.Len() > 0 {
			emitBody(session, flowID, sequence, "request", requestCapture)
			sequence++
		}

		response, err := http.ReadResponse(upstreamReader, request)
		if err != nil {
			emitFlowError(session, flowID, sequence, "read upstream response")
			return
		}
		emitJSON(session, tunnel.InspectorEvent{
			Version:  tunnel.InspectorEventVersion1,
			Type:     tunnel.InspectorEventHeaders,
			FlowID:   flowID,
			Sequence: sequence,
		}, map[string]any{
			"direction":  "response",
			"statusCode": response.StatusCode,
			"headers":    redactHeaders(response.Header),
		})
		sequence++
		var responseCapture *bodyCapture
		if target.CaptureBody && session.maxBodySize > 0 && response.Body != nil {
			responseCapture = &bodyCapture{source: response.Body, limit: session.maxBodySize}
			response.Body = responseCapture
		}
		if err := response.Write(client); err != nil {
			return
		}
		if response.Body != nil {
			_ = response.Body.Close()
		}
		if target.CaptureBody && responseCapture != nil && responseCapture.buffer.Len() > 0 {
			emitBody(session, flowID, sequence, "response", responseCapture)
			sequence++
		}
		emitJSON(session, tunnel.InspectorEvent{
			Version:  tunnel.InspectorEventVersion1,
			Type:     tunnel.InspectorEventFlowEnd,
			FlowID:   flowID,
			Sequence: sequence,
		}, map[string]any{
			"statusCode": response.StatusCode,
			"durationMs": time.Since(startedAt).Milliseconds(),
			"truncated":  captureTruncated(requestCapture) || captureTruncated(responseCapture),
		})
		if request.Close || response.Close {
			return
		}
	}
}

func isHTTP1Prefix(prefix []byte) bool {
	value := string(prefix)
	for _, method := range []string{
		"GET ", "POST ", "PUT ", "PATCH ", "DELETE ", "HEAD ",
		"OPTIONS ", "CONNECT ", "TRACE ", "PRI ",
	} {
		if strings.HasPrefix(value, method) {
			return true
		}
	}
	return false
}

func relayInspectorConnections(left, right net.Conn) {
	done := make(chan struct{}, 2)
	copyStream := func(destination, source net.Conn) {
		_, _ = io.Copy(destination, source)
		if value, ok := destination.(interface{ CloseWrite() error }); ok {
			_ = value.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyStream(left, right)
	go copyStream(right, left)
	<-done
}

func inspectorFlowSource(target tunnel.InspectorTarget) string {
	if target.FlowSource != "" {
		return target.FlowSource
	}
	return "workstation"
}

func emitBody(
	session *agentSession,
	flowID string,
	sequence uint64,
	direction string,
	capture *bodyCapture,
) {
	emitJSON(session, tunnel.InspectorEvent{
		Version:  tunnel.InspectorEventVersion1,
		Type:     tunnel.InspectorEventBody,
		FlowID:   flowID,
		Sequence: sequence,
	}, map[string]any{
		"direction": direction,
		"body":      capture.buffer.Bytes(),
		"truncated": capture.truncated,
	})
}

func emitFlowError(
	session *agentSession, flowID string, sequence uint64, message string,
) {
	emitJSON(session, tunnel.InspectorEvent{
		Version:  tunnel.InspectorEventVersion1,
		Type:     tunnel.InspectorEventError,
		FlowID:   flowID,
		Sequence: sequence,
	}, map[string]any{"error": message})
}

func emitJSON(session *agentSession, event tunnel.InspectorEvent, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	event.Payload = payload
	session.emit(event)
}

func redactHeaders(headers http.Header) http.Header {
	result := make(http.Header, len(headers))
	for name, values := range headers {
		if sensitiveHeader(name) {
			result[name] = []string{"[REDACTED]"}
			continue
		}
		result[name] = append([]string(nil), values...)
	}
	return result
}

func sensitiveHeader(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if strings.HasSuffix(normalized, "-bin") {
		return true
	}
	switch normalized {
	case "authorization", "proxy-authorization", "cookie", "set-cookie",
		"x-api-key", "x-auth-token":
		return true
	default:
		return false
	}
}

func captureTruncated(capture *bodyCapture) bool {
	return capture != nil && capture.truncated
}
