package inspectoragent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

const (
	opPing     = "ping"
	opStart    = "start"
	opUpdate   = "update"
	opStop     = "stop"
	opDial     = "dial"
	opEvents   = "events"
	maxRPCSize = 64 << 10
)

type request struct {
	Op            string                   `json:"op"`
	SessionID     string                   `json:"sessionID,omitempty"`
	Config        *tunnel.InspectorConfig  `json:"config,omitempty"`
	Targets       []tunnel.InspectorTarget `json:"targets,omitempty"`
	Target        *tunnel.InspectorTarget  `json:"target,omitempty"`
	TargetAddress string                   `json:"targetAddress,omitempty"`
}

type response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func writeJSON(w io.Writer, value any) error {
	return json.NewEncoder(w).Encode(value)
}

func readRequest(reader *bufio.Reader) (request, error) {
	line, err := readBoundedLine(reader)
	if err != nil {
		return request{}, err
	}
	var value request
	if err := json.Unmarshal(line, &value); err != nil {
		return request{}, fmt.Errorf("decode Inspector Agent request: %w", err)
	}
	return value, nil
}

func readResponse(reader *bufio.Reader) error {
	line, err := readBoundedLine(reader)
	if err != nil {
		return err
	}
	var value response
	if err := json.Unmarshal(line, &value); err != nil {
		return fmt.Errorf("decode Inspector Agent response: %w", err)
	}
	if !value.OK {
		if value.Error == "" {
			value.Error = "Inspector Agent request failed"
		}
		return errors.New(value.Error)
	}
	return nil
}

func readBoundedLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, min(maxRPCSize, reader.Size()))
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > maxRPCSize {
			return nil, errors.New("Inspector Agent RPC message is too large")
		}
		line = append(line, fragment...)
		switch {
		case err == nil:
			if len(line) == 0 {
				return nil, errors.New("Inspector Agent RPC message is empty")
			}
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return nil, err
		}
	}
}
