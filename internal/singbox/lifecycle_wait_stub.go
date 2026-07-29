//go:build !darwin && !linux

package singbox

import "time"

func waitLifecycleCleanup(string, time.Duration) {}
