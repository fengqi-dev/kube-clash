//go:build !windows

package helper

import "context"

// RunService starts the helper RPC server until the process is terminated.
func RunService(server *Server) error {
	return server.Serve(context.Background())
}
