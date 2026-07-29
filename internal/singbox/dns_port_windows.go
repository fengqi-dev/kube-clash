//go:build windows

package singbox

// NRPT NameServers do not carry a port; Windows DNS clients expect UDP/TCP 53.
func selectDNSPort() (int, error) {
	return 53, nil
}
