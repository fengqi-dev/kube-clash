//go:build !windows

package singbox

func selectDNSPort() (int, error) {
	return availablePort()
}
