//go:build !darwin && !linux && !windows

package singbox

func splitDNSSetupCommands(string) string   { return "" }
func splitDNSRestoreCommands(string) string { return "" }
