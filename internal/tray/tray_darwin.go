//go:build darwin

package tray

import "log"

func (c *Controller) startPlatform() {
	// energye/systray cannot share AppKit with Wails v2 on macOS today:
	//   - systray.Run calls [NSApp run] on a background goroutine → SIGTRAP
	//   - RunWithExternalLoop/nativeStart must create NSStatusItem on the main
	//     thread, but Wails OnStartup runs off the AppKit main thread → abort
	// Keep window lifecycle without a status item until we have a main-queue
	// integration that does not replace NSApp.delegate.
	log.Printf("system tray disabled on macOS (Wails AppKit conflict)")
}

func (c *Controller) stopPlatform() {}
