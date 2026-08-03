package tunnel

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

const maxCapabilitiesSize = 16 * 1024

type Capabilities struct {
	ProtocolVersion int      `json:"protocolVersion"`
	Inspector       bool     `json:"inspector"`
	Protocols       []string `json:"protocols,omitempty"`
	MaxBodySize     int64    `json:"maxBodySize,omitempty"`
	MaxTargets      int      `json:"maxTargets,omitempty"`
	Engine          string   `json:"engine,omitempty"`
}

func WriteCapabilities(w io.Writer, capabilities Capabilities) error {
	payload, err := json.Marshal(capabilities)
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > maxCapabilitiesSize {
		return errors.New("gateway capabilities size is invalid")
	}
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(payload)))
	if err := writeAll(w, size[:]); err != nil {
		return err
	}
	return writeAll(w, payload)
}

func ReadCapabilities(r io.Reader) (Capabilities, error) {
	var size [2]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return Capabilities{}, err
	}
	payloadSize := int(binary.BigEndian.Uint16(size[:]))
	if payloadSize == 0 || payloadSize > maxCapabilitiesSize {
		return Capabilities{}, errors.New("gateway capabilities size is invalid")
	}
	payload := make([]byte, payloadSize)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Capabilities{}, err
	}
	var capabilities Capabilities
	if err := json.Unmarshal(payload, &capabilities); err != nil {
		return Capabilities{}, err
	}
	return capabilities, nil
}
