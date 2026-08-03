package tunnel

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	CtrlRegister     byte = 1
	CtrlUnregister   byte = 2
	CtrlInboundReady byte = 3
	CtrlAck          byte = 4
	CtrlError        byte = 5

	NetworkTCP byte = 1
	NetworkUDP byte = 2

	maxControlPayloadSize = 60 << 10
)

type ControlMessage struct {
	Type        byte
	InterceptID string
	Network     byte
	ListenPort  uint16
	StreamID    uint64
	Error       string
	Inspector   *InspectorConfig
	Targets     []InspectorTarget
}

func WriteControlMessage(w io.Writer, message ControlMessage) error {
	payload, err := encodeControlPayload(message)
	if err != nil {
		return err
	}
	if len(payload) > maxControlPayloadSize {
		return errors.New("control message is too large")
	}
	header := make([]byte, 3)
	header[0] = message.Type
	binary.BigEndian.PutUint16(header[1:3], uint16(len(payload)))
	if err := writeAll(w, header); err != nil {
		return err
	}
	return writeAll(w, payload)
}

func ReadControlMessage(r io.Reader) (ControlMessage, error) {
	header := make([]byte, 3)
	if _, err := io.ReadFull(r, header); err != nil {
		return ControlMessage{}, err
	}
	messageType := header[0]
	size := int(binary.BigEndian.Uint16(header[1:3]))
	if size > maxControlPayloadSize {
		return ControlMessage{}, errors.New("control message is too large")
	}
	payload := make([]byte, size)
	if size > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return ControlMessage{}, err
		}
	}
	return decodeControlPayload(messageType, payload)
}

func encodeControlPayload(message ControlMessage) ([]byte, error) {
	switch message.Type {
	case CtrlRegister:
		if err := validateInterceptID(message.InterceptID); err != nil {
			return nil, err
		}
		if message.Network != NetworkTCP && message.Network != NetworkUDP {
			return nil, fmt.Errorf("unsupported network %d", message.Network)
		}
		if message.ListenPort == 0 {
			return nil, errors.New("listen port is required")
		}
		payload := make([]byte, 2+len(message.InterceptID)+1+2)
		binary.BigEndian.PutUint16(payload[0:2], uint16(len(message.InterceptID)))
		copy(payload[2:], message.InterceptID)
		offset := 2 + len(message.InterceptID)
		payload[offset] = message.Network
		binary.BigEndian.PutUint16(payload[offset+1:], message.ListenPort)
		return payload, nil
	case CtrlUnregister:
		if err := validateInterceptID(message.InterceptID); err != nil {
			return nil, err
		}
		payload := make([]byte, 2+len(message.InterceptID))
		binary.BigEndian.PutUint16(payload[0:2], uint16(len(message.InterceptID)))
		copy(payload[2:], message.InterceptID)
		return payload, nil
	case CtrlInboundReady:
		if err := validateInterceptID(message.InterceptID); err != nil {
			return nil, err
		}
		if message.StreamID == 0 {
			return nil, errors.New("stream id is required")
		}
		if message.Network != NetworkTCP && message.Network != NetworkUDP {
			return nil, fmt.Errorf("unsupported network %d", message.Network)
		}
		payload := make([]byte, 8+2+len(message.InterceptID)+1)
		binary.BigEndian.PutUint64(payload[0:8], message.StreamID)
		binary.BigEndian.PutUint16(payload[8:10], uint16(len(message.InterceptID)))
		copy(payload[10:], message.InterceptID)
		payload[10+len(message.InterceptID)] = message.Network
		return payload, nil
	case CtrlAck:
		return nil, nil
	case CtrlError:
		if message.Error == "" {
			return nil, errors.New("error message is required")
		}
		if len(message.Error) > maxErrorSize {
			message.Error = message.Error[:maxErrorSize]
		}
		return []byte(message.Error), nil
	case CtrlInspectorStart:
		if message.Inspector == nil {
			return nil, errors.New("Inspector config is required")
		}
		if err := message.Inspector.Validate(); err != nil {
			return nil, err
		}
		return json.Marshal(message.Inspector)
	case CtrlInspectorUpdateTargets:
		if err := ValidateInspectorTargets(message.Targets); err != nil {
			return nil, err
		}
		return json.Marshal(message.Targets)
	case CtrlInspectorStop:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported control message type %d", message.Type)
	}
}

func decodeControlPayload(messageType byte, payload []byte) (ControlMessage, error) {
	message := ControlMessage{Type: messageType}
	switch messageType {
	case CtrlRegister:
		id, rest, err := readLengthPrefixed(payload)
		if err != nil {
			return ControlMessage{}, err
		}
		if len(rest) != 3 {
			return ControlMessage{}, errors.New("invalid register payload")
		}
		message.InterceptID = id
		message.Network = rest[0]
		message.ListenPort = binary.BigEndian.Uint16(rest[1:3])
		if message.Network != NetworkTCP && message.Network != NetworkUDP {
			return ControlMessage{}, fmt.Errorf("unsupported network %d", message.Network)
		}
		if message.ListenPort == 0 {
			return ControlMessage{}, errors.New("listen port is required")
		}
		return message, nil
	case CtrlUnregister:
		id, rest, err := readLengthPrefixed(payload)
		if err != nil {
			return ControlMessage{}, err
		}
		if len(rest) != 0 {
			return ControlMessage{}, errors.New("invalid unregister payload")
		}
		message.InterceptID = id
		return message, nil
	case CtrlInboundReady:
		if len(payload) < 11 {
			return ControlMessage{}, errors.New("invalid inbound-ready payload")
		}
		message.StreamID = binary.BigEndian.Uint64(payload[0:8])
		id, rest, err := readLengthPrefixed(payload[8:])
		if err != nil {
			return ControlMessage{}, err
		}
		if len(rest) != 1 {
			return ControlMessage{}, errors.New("invalid inbound-ready payload")
		}
		message.InterceptID = id
		message.Network = rest[0]
		if message.StreamID == 0 {
			return ControlMessage{}, errors.New("stream id is required")
		}
		if message.Network != NetworkTCP && message.Network != NetworkUDP {
			return ControlMessage{}, fmt.Errorf("unsupported network %d", message.Network)
		}
		return message, nil
	case CtrlAck:
		if len(payload) != 0 {
			return ControlMessage{}, errors.New("invalid ack payload")
		}
		return message, nil
	case CtrlError:
		if len(payload) == 0 {
			return ControlMessage{}, errors.New("error message is required")
		}
		message.Error = string(payload)
		return message, nil
	case CtrlInspectorStart:
		if len(payload) == 0 {
			return ControlMessage{}, errors.New("Inspector config is required")
		}
		var config InspectorConfig
		if err := json.Unmarshal(payload, &config); err != nil {
			return ControlMessage{}, fmt.Errorf("decode Inspector config: %w", err)
		}
		if err := config.Validate(); err != nil {
			return ControlMessage{}, err
		}
		message.Inspector = &config
		return message, nil
	case CtrlInspectorUpdateTargets:
		if len(payload) == 0 {
			return ControlMessage{}, errors.New("Inspector targets are required")
		}
		if err := json.Unmarshal(payload, &message.Targets); err != nil {
			return ControlMessage{}, fmt.Errorf("decode Inspector targets: %w", err)
		}
		if err := ValidateInspectorTargets(message.Targets); err != nil {
			return ControlMessage{}, err
		}
		return message, nil
	case CtrlInspectorStop:
		if len(payload) != 0 {
			return ControlMessage{}, errors.New("invalid Inspector stop payload")
		}
		return message, nil
	default:
		return ControlMessage{}, fmt.Errorf("unsupported control message type %d", messageType)
	}
}

func readLengthPrefixed(payload []byte) (string, []byte, error) {
	if len(payload) < 2 {
		return "", nil, errors.New("missing length prefix")
	}
	size := int(binary.BigEndian.Uint16(payload[0:2]))
	if size == 0 || size > maxIDSize {
		return "", nil, errors.New("id length is invalid")
	}
	if len(payload) < 2+size {
		return "", nil, errors.New("truncated id payload")
	}
	return string(payload[2 : 2+size]), payload[2+size:], nil
}

func validateInterceptID(id string) error {
	if id == "" || len(id) > maxIDSize {
		return errors.New("intercept id length is invalid")
	}
	return nil
}
