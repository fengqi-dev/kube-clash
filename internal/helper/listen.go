package helper

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func dialHelper(ctx context.Context) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", SocketPath())
}

func listenHelper() (net.Listener, error) {
	path := SocketPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen helper socket: %w", err)
	}
	mode := os.FileMode(0o666)
	if runtime.GOOS == "windows" {
		mode = 0o600
	}
	if err := os.Chmod(path, mode); err != nil && runtime.GOOS != "windows" {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

func withDialTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, 3*time.Second)
}
