package socksbridge

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
)

func negotiate(reader *bufio.Reader, writer io.Writer) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	if header[0] != socksVersion || header[1] == 0 {
		return errors.New("invalid SOCKS greeting")
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return err
	}
	if slices.Contains(methods, methodNone) {
		_, err := writer.Write([]byte{socksVersion, methodNone})
		return err
	}
	_, _ = writer.Write([]byte{socksVersion, 0xff})
	return errors.New("SOCKS client does not support no-auth")
}

func readRequest(reader *bufio.Reader) (byte, string, uint16, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, "", 0, err
	}
	if header[0] != socksVersion || header[2] != 0 {
		return 0, "", 0, errors.New("invalid SOCKS request")
	}
	host, err := readAddress(reader, header[3])
	if err != nil {
		return 0, "", 0, err
	}
	var port [2]byte
	if _, err := io.ReadFull(reader, port[:]); err != nil {
		return 0, "", 0, err
	}
	return header[1], host, binary.BigEndian.Uint16(port[:]), nil
}

func parseUDPPacket(packet []byte) (string, uint16, []byte, error) {
	if len(packet) < 4 || packet[0] != 0 || packet[1] != 0 || packet[2] != 0 {
		return "", 0, nil, errors.New("fragmented or invalid SOCKS UDP packet")
	}
	reader := bufio.NewReaderSize(newSliceReader(packet[4:]), len(packet))
	host, err := readAddress(reader, packet[3])
	if err != nil {
		return "", 0, nil, err
	}
	var port [2]byte
	if _, err := io.ReadFull(reader, port[:]); err != nil {
		return "", 0, nil, err
	}
	payload, err := io.ReadAll(reader)
	return host, binary.BigEndian.Uint16(port[:]), payload, err
}

func encodeUDPPacket(host string, port uint16, payload []byte) ([]byte, error) {
	address, err := encodeAddress(host)
	if err != nil {
		return nil, err
	}
	packet := make([]byte, 3+len(address)+2+len(payload))
	copy(packet[3:], address)
	offset := 3 + len(address)
	binary.BigEndian.PutUint16(packet[offset:offset+2], port)
	copy(packet[offset+2:], payload)
	return packet, nil
}

func readAddress(reader io.Reader, addressType byte) (string, error) {
	switch addressType {
	case addressIPv4:
		value := make([]byte, net.IPv4len)
		_, err := io.ReadFull(reader, value)
		return net.IP(value).String(), err
	case addressIPv6:
		value := make([]byte, net.IPv6len)
		_, err := io.ReadFull(reader, value)
		return net.IP(value).String(), err
	case addressDomain:
		var size [1]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			return "", err
		}
		if size[0] == 0 {
			return "", errors.New("empty SOCKS domain")
		}
		value := make([]byte, int(size[0]))
		_, err := io.ReadFull(reader, value)
		return string(value), err
	default:
		return "", fmt.Errorf("unsupported SOCKS address type %d", addressType)
	}
}

func encodeAddress(host string) ([]byte, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			return append([]byte{addressIPv4}, ipv4...), nil
		}
		return append([]byte{addressIPv6}, ip.To16()...), nil
	}
	if len(host) == 0 || len(host) > 255 {
		return nil, errors.New("SOCKS domain length is invalid")
	}
	return append([]byte{addressDomain, byte(len(host))}, []byte(host)...), nil
}

func writeReply(writer io.Writer, status byte, address net.Addr) error {
	host := "0.0.0.0"
	port := uint16(0)
	if value, ok := address.(*net.TCPAddr); ok {
		host, port = value.IP.String(), uint16(value.Port)
	}
	if value, ok := address.(*net.UDPAddr); ok {
		host, port = value.IP.String(), uint16(value.Port)
	}
	encoded, err := encodeAddress(host)
	if err != nil {
		return err
	}
	reply := append([]byte{socksVersion, status, 0}, encoded...)
	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], port)
	reply = append(reply, portBytes[:]...)
	_, err = writer.Write(reply)
	return err
}

func relay(left, right net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(left, right); done <- struct{}{} }()
	go func() { _, _ = io.Copy(right, left); done <- struct{}{} }()
	<-done
}

type sliceReader struct {
	value []byte
}

func newSliceReader(value []byte) *sliceReader { return &sliceReader{value: value} }

func (r *sliceReader) Read(destination []byte) (int, error) {
	if len(r.value) == 0 {
		return 0, io.EOF
	}
	read := copy(destination, r.value)
	r.value = r.value[read:]
	return read, nil
}
