//go:build !windows

package tray

func iconBytes() []byte {
	return iconPNG
}
