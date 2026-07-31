package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateToken returns a high-entropy opaque bearer token.
func GenerateToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate mcp token: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
