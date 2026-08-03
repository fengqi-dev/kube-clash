package tunnel

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	InspectorEventVersion1 byte = 1

	InspectorEventFlowStart byte = 1
	InspectorEventHeaders   byte = 2
	InspectorEventBody      byte = 3
	InspectorEventFlowEnd   byte = 4
	InspectorEventError     byte = 5

	maxInspectorEventFrameSize = 1 << 20
	maxInspectorFlowIDSize     = 256
)

type InspectorEvent struct {
	Version  byte            `json:"version"`
	Type     byte            `json:"type"`
	FlowID   string          `json:"flowId"`
	Sequence uint64          `json:"sequence"`
	Payload  json.RawMessage `json:"payload"`
}

func WriteInspectorEvent(w io.Writer, event InspectorEvent) error {
	if event.Version != InspectorEventVersion1 {
		return fmt.Errorf("unsupported Inspector event version %d", event.Version)
	}
	if event.Type < InspectorEventFlowStart || event.Type > InspectorEventError {
		return fmt.Errorf("unsupported Inspector event type %d", event.Type)
	}
	if event.FlowID == "" || len(event.FlowID) > maxInspectorFlowIDSize {
		return errors.New("Inspector event flow ID length is invalid")
	}
	if len(event.Payload) == 0 || !json.Valid(event.Payload) {
		return errors.New("Inspector event payload must be valid JSON")
	}
	frameSize := 12 + len(event.FlowID) + len(event.Payload)
	if frameSize > maxInspectorEventFrameSize {
		return errors.New("Inspector event frame is too large")
	}
	frame := make([]byte, 4+frameSize)
	binary.BigEndian.PutUint32(frame[:4], uint32(frameSize))
	frame[4] = event.Version
	frame[5] = event.Type
	binary.BigEndian.PutUint16(frame[6:8], uint16(len(event.FlowID)))
	copy(frame[8:8+len(event.FlowID)], event.FlowID)
	offset := 8 + len(event.FlowID)
	binary.BigEndian.PutUint64(frame[offset:offset+8], event.Sequence)
	copy(frame[offset+8:], event.Payload)
	return writeAll(w, frame)
}

func ReadInspectorEvent(r io.Reader) (InspectorEvent, error) {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return InspectorEvent{}, err
	}
	frameSize := int(binary.BigEndian.Uint32(size[:]))
	if frameSize < 13 || frameSize > maxInspectorEventFrameSize {
		return InspectorEvent{}, errors.New("Inspector event frame size is invalid")
	}
	frame := make([]byte, frameSize)
	if _, err := io.ReadFull(r, frame); err != nil {
		return InspectorEvent{}, err
	}
	event := InspectorEvent{Version: frame[0], Type: frame[1]}
	if event.Version != InspectorEventVersion1 {
		return InspectorEvent{}, fmt.Errorf(
			"unsupported Inspector event version %d", event.Version,
		)
	}
	if event.Type < InspectorEventFlowStart || event.Type > InspectorEventError {
		return InspectorEvent{}, fmt.Errorf("unsupported Inspector event type %d", event.Type)
	}
	flowIDSize := int(binary.BigEndian.Uint16(frame[2:4]))
	if flowIDSize == 0 || flowIDSize > maxInspectorFlowIDSize ||
		4+flowIDSize+8 >= len(frame) {
		return InspectorEvent{}, errors.New("Inspector event flow ID length is invalid")
	}
	event.FlowID = string(frame[4 : 4+flowIDSize])
	offset := 4 + flowIDSize
	event.Sequence = binary.BigEndian.Uint64(frame[offset : offset+8])
	event.Payload = append(json.RawMessage(nil), frame[offset+8:]...)
	if !json.Valid(event.Payload) {
		return InspectorEvent{}, errors.New("Inspector event payload must be valid JSON")
	}
	return event, nil
}
