//go:build !darwin

package singbox

func kubeLoopResolversPresent() bool { return false }
