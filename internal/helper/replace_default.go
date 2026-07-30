//go:build !windows

package helper

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
