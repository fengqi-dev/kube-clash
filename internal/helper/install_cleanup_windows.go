//go:build windows

package helper

import "os"

func cleanupDisplacedHelperBinaries(current string) {
	for _, path := range windowsDisplacedHelperPaths(current) {
		_ = os.Remove(path)
	}
}
