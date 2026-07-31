//go:build !darwin && !linux && !windows

package helper

import (
	"os"

	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

func applyPlatformDNS(string, singbox.DNSMeta) error   { return nil }
func restorePlatformDNS(string, singbox.DNSMeta) error { return nil }
func applyLinkDNS(string, singbox.DNSMeta) error       { return nil }
func restoreLinkDNS(string) error                      { return nil }
func cleanupPlatformRoutes([]string)                   {}
func stopManagedProcess(process *os.Process) error     { return process.Kill() }
