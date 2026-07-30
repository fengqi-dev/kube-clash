//go:build !windows

package helper

import "os"

func configureHelperSocketAccess(path, _ string) error {
	return os.Chmod(path, 0o666)
}
