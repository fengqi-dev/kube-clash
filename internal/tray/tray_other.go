//go:build !darwin

package tray

import "github.com/energye/systray"

func (c *Controller) startPlatform() {
	c.active.Store(true)
	// Own a platform message loop independent of the WebView toolkit (Win32 / DBus).
	go systray.Run(c.onReady, c.onExit)
}

func (c *Controller) stopPlatform() {
	systray.Quit()
	c.active.Store(false)
}
