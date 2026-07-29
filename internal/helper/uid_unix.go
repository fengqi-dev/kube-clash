//go:build unix

package helper

import "os"

func currentUID() int { return os.Getuid() }
