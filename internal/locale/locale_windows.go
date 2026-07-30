//go:build windows

package locale

import "golang.org/x/sys/windows"

var procGetUserDefaultUILanguage = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

func isChineseUI() bool {
	r, _, err := procGetUserDefaultUILanguage.Call()
	if r == 0 && err != nil {
		return false
	}
	// Primary language ID for Chinese is 0x04 (zh).
	return r&0xFF == 0x04
}
