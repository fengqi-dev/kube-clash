//go:build !windows

package session

func inspectDNSPort() *NetworkDiagnostic {
	return nil
}
