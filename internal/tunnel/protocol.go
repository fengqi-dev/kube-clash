package tunnel

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

const (
	CommandTCP             byte = 1
	CommandUDP             byte = 2
	CommandControl         byte = 3
	CommandAccept          byte = 4
	CommandInspectorEvents byte = 5

	ProtocolV1 byte = 1
	ProtocolV2 byte = 2

	SessionTokenSize = 32

	StatusOK    byte = 0
	StatusError byte = 1

	MaxDatagramSize = 65507
	maxHostSize     = 1024
	maxErrorSize    = 4096
	maxIDSize       = 256
)

var magicV1 = [4]byte{'K', 'C', 'G', ProtocolV1}
var magicV2 = [4]byte{'K', 'C', 'G', ProtocolV2}

type SessionToken [SessionTokenSize]byte

func NewSessionToken() (SessionToken, error) {
	var token SessionToken
	if _, err := io.ReadFull(rand.Reader, token[:]); err != nil {
		return SessionToken{}, fmt.Errorf("generate session token: %w", err)
	}
	return token, nil
}

func (t SessionToken) IsZero() bool {
	return t == SessionToken{}
}

type SessionHeader struct {
	Version byte
	Command byte
	Token   SessionToken
}

type OpenRequest struct {
	Command byte
	Host    string
	Port    uint16
}

func (r OpenRequest) Address() string {
	return net.JoinHostPort(r.Host, strconv.Itoa(int(r.Port)))
}

func WriteOpen(w io.Writer, request OpenRequest) error {
	return writeOpen(w, SessionToken{}, request)
}

func WriteOpenV2(w io.Writer, token SessionToken, request OpenRequest) error {
	return writeOpen(w, token, request)
}

func writeOpen(w io.Writer, token SessionToken, request OpenRequest) error {
	if request.Command != CommandTCP && request.Command != CommandUDP {
		return fmt.Errorf("unsupported command %d", request.Command)
	}
	if request.Host == "" || len(request.Host) > maxHostSize {
		return errors.New("target host length is invalid")
	}
	if request.Port == 0 {
		return errors.New("target port is required")
	}
	prefix, err := sessionPrefix(token, request.Command)
	if err != nil {
		return err
	}
	header := make([]byte, len(prefix)+4+len(request.Host))
	copy(header, prefix)
	offset := len(prefix)
	binary.BigEndian.PutUint16(header[offset:offset+2], uint16(len(request.Host)))
	copy(header[offset+2:offset+2+len(request.Host)], request.Host)
	binary.BigEndian.PutUint16(header[offset+2+len(request.Host):], request.Port)
	return writeAll(w, header)
}

func ReadOpen(r io.Reader) (OpenRequest, error) {
	command, err := ReadSessionHeader(r)
	if err != nil {
		return OpenRequest{}, err
	}
	return ReadOpenBody(r, command)
}

// ReadOpenBody reads host/port after magic+command were already consumed.
func ReadOpenBody(r io.Reader, command byte) (OpenRequest, error) {
	if command != CommandTCP && command != CommandUDP {
		return OpenRequest{}, fmt.Errorf("unsupported command %d", command)
	}
	var sizeBuf [2]byte
	if _, err := io.ReadFull(r, sizeBuf[:]); err != nil {
		return OpenRequest{}, err
	}
	hostSize := int(binary.BigEndian.Uint16(sizeBuf[:]))
	if hostSize == 0 || hostSize > maxHostSize {
		return OpenRequest{}, errors.New("target host length is invalid")
	}
	target := make([]byte, hostSize+2)
	if _, err := io.ReadFull(r, target); err != nil {
		return OpenRequest{}, err
	}
	port := binary.BigEndian.Uint16(target[hostSize:])
	if port == 0 {
		return OpenRequest{}, errors.New("target port is required")
	}
	return OpenRequest{Command: command, Host: string(target[:hostSize]), Port: port}, nil
}

// ReadSessionHeader reads the shared session prefix and returns its command.
// Use ReadSessionHeaderInfo when the caller needs KCG2 session identity.
func ReadSessionHeader(r io.Reader) (command byte, err error) {
	header, err := ReadSessionHeaderInfo(r)
	return header.Command, err
}

func ReadSessionHeaderInfo(r io.Reader) (SessionHeader, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return SessionHeader{}, err
	}
	result := SessionHeader{Command: header[4]}
	switch string(header[:4]) {
	case string(magicV1[:]):
		result.Version = ProtocolV1
	case string(magicV2[:]):
		result.Version = ProtocolV2
		if _, err := io.ReadFull(r, result.Token[:]); err != nil {
			return SessionHeader{}, err
		}
		if result.Token.IsZero() {
			return SessionHeader{}, errors.New("KCG2 session token is required")
		}
	default:
		return SessionHeader{}, errors.New("invalid tunnel protocol magic")
	}
	return result, nil
}

func WriteControlSession(w io.Writer) error {
	return writeSessionHeader(w, SessionToken{}, CommandControl)
}

func WriteControlSessionV2(w io.Writer, token SessionToken) error {
	return writeSessionHeader(w, token, CommandControl)
}

func WriteInspectorEventsSession(w io.Writer, token SessionToken) error {
	if token.IsZero() {
		return errors.New("KCG2 session token is required")
	}
	return writeSessionHeader(w, token, CommandInspectorEvents)
}

func WriteAccept(w io.Writer, streamID uint64) error {
	return writeAccept(w, SessionToken{}, streamID)
}

func WriteAcceptV2(w io.Writer, token SessionToken, streamID uint64) error {
	return writeAccept(w, token, streamID)
}

func writeAccept(w io.Writer, token SessionToken, streamID uint64) error {
	prefix, err := sessionPrefix(token, CommandAccept)
	if err != nil {
		return err
	}
	header := make([]byte, len(prefix)+8)
	copy(header, prefix)
	binary.BigEndian.PutUint64(header[len(prefix):], streamID)
	return writeAll(w, header)
}

func writeSessionHeader(w io.Writer, token SessionToken, command byte) error {
	header, err := sessionPrefix(token, command)
	if err != nil {
		return err
	}
	return writeAll(w, header)
}

func sessionPrefix(token SessionToken, command byte) ([]byte, error) {
	if token.IsZero() {
		header := make([]byte, 5)
		copy(header[:4], magicV1[:])
		header[4] = command
		return header, nil
	}
	header := make([]byte, 5+SessionTokenSize)
	copy(header[:4], magicV2[:])
	header[4] = command
	copy(header[5:], token[:])
	return header, nil
}

func ReadAcceptStreamID(r io.Reader) (uint64, error) {
	var raw [8]byte
	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(raw[:]), nil
}

func WriteStatus(w io.Writer, err error) error {
	if err == nil {
		return writeAll(w, []byte{StatusOK})
	}
	message := err.Error()
	if len(message) > maxErrorSize {
		message = message[:maxErrorSize]
	}
	value := make([]byte, 3+len(message))
	value[0] = StatusError
	binary.BigEndian.PutUint16(value[1:3], uint16(len(message)))
	copy(value[3:], message)
	return writeAll(w, value)
}

func ReadStatus(r io.Reader) error {
	var status [1]byte
	if _, err := io.ReadFull(r, status[:]); err != nil {
		return err
	}
	switch status[0] {
	case StatusOK:
		return nil
	case StatusError:
		var size [2]byte
		if _, err := io.ReadFull(r, size[:]); err != nil {
			return err
		}
		messageSize := int(binary.BigEndian.Uint16(size[:]))
		if messageSize > maxErrorSize {
			return errors.New("gateway error message is too large")
		}
		message := make([]byte, messageSize)
		if _, err := io.ReadFull(r, message); err != nil {
			return err
		}
		return errors.New(string(message))
	default:
		return fmt.Errorf("invalid gateway status %d", status[0])
	}
}

func WriteDatagram(w io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > MaxDatagramSize {
		return fmt.Errorf("invalid datagram size %d", len(payload))
	}
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(payload)))
	if err := writeAll(w, size[:]); err != nil {
		return err
	}
	return writeAll(w, payload)
}

func ReadDatagram(r *bufio.Reader, buffer []byte) ([]byte, error) {
	var size [2]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return nil, err
	}
	payloadSize := int(binary.BigEndian.Uint16(size[:]))
	if payloadSize == 0 || payloadSize > MaxDatagramSize {
		return nil, fmt.Errorf("invalid datagram size %d", payloadSize)
	}
	if cap(buffer) < payloadSize {
		buffer = make([]byte, payloadSize)
	}
	buffer = buffer[:payloadSize]
	if _, err := io.ReadFull(r, buffer); err != nil {
		return nil, err
	}
	return buffer, nil
}

func writeAll(w io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := w.Write(value)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		value = value[written:]
	}
	return nil
}
