//go:build !darwin && !linux && !windows

package singbox

func usesLifecycleWrapper() bool { return false }
