//go:build e2e

package e2e

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func openSOCKSControl(t *testing.T, address string) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte{5, 1, 0}); err != nil {
		connection.Close()
		t.Fatal(err)
	}
	response := make([]byte, 2)
	if _, err := io.ReadFull(connection, response); err != nil {
		connection.Close()
		t.Fatal(err)
	}
	if response[0] != 5 || response[1] != 0 {
		connection.Close()
		t.Fatalf("SOCKS negotiation failed: %v", response)
	}
	return connection
}

func socksRequest(t *testing.T, command byte, host string, port uint16) []byte {
	t.Helper()
	value := append([]byte{5, command, 0}, socksAddress(t, host)...)
	var encodedPort [2]byte
	binary.BigEndian.PutUint16(encodedPort[:], port)
	return append(value, encodedPort[:]...)
}

func socksAddress(t *testing.T, host string) []byte {
	t.Helper()
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		t.Fatalf("test requires IPv4 target, got %q", host)
	}
	return append([]byte{1}, ip.To4()...)
}

func readSOCKSReply(t *testing.T, connection net.Conn) byte {
	t.Helper()
	status, _ := readSOCKSReplyAddress(t, connection)
	return status
}

func readSOCKSReplyAddress(t *testing.T, connection net.Conn) (byte, *net.UDPAddr) {
	t.Helper()
	header := make([]byte, 4)
	if _, err := io.ReadFull(connection, header); err != nil {
		t.Fatal(err)
	}
	if header[0] != 5 || header[3] != 1 {
		t.Fatalf("unexpected SOCKS reply: %v", header)
	}
	address := make([]byte, 6)
	if _, err := io.ReadFull(connection, address); err != nil {
		t.Fatal(err)
	}
	return header[1], &net.UDPAddr{
		IP: net.IP(address[:4]), Port: int(binary.BigEndian.Uint16(address[4:])),
	}
}
