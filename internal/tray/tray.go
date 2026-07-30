package tray

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/energye/systray"
	"github.com/fengqi-dev/kube-loop/internal/session"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const maxRecentClusters = 5

// Controller owns the system tray icon and menu.
type Controller struct {
	host     Host
	strings  stringsLocal
	quitting atomic.Bool
	ready    atomic.Bool

	mu             sync.Mutex
	statusItem     *systray.MenuItem
	connectItem    *systray.MenuItem
	disconnectItem *systray.MenuItem
	showItem       *systray.MenuItem
	quitItem       *systray.MenuItem
	recentRoot     *systray.MenuItem
	recentItems    []*systray.MenuItem
	recentEmpty    *systray.MenuItem
}

// Start launches the tray in a background goroutine.
// Call this before wails.Run so the tray message loop is independent of WebView.
func Start(host Host) *Controller {
	c := &Controller{host: host, strings: locale()}
	go systray.Run(c.onReady, c.onExit)
	return c
}

// Stop removes the tray icon.
func (c *Controller) Stop() {
	if c == nil {
		return
	}
	c.quitting.Store(true)
	systray.Quit()
}

// BeforeClose hides the window instead of quitting, unless Quit was confirmed.
func (c *Controller) BeforeClose(ctx context.Context) (prevent bool) {
	if c != nil && c.quitting.Load() {
		return false
	}
	runtime.WindowHide(ctx)
	return true
}

func (c *Controller) onReady() {
	systray.SetIcon(iconBytes())
	systray.SetTitle("KubeLoop")
	systray.SetTooltip(c.strings.tooltipDisconnected)

	systray.SetOnClick(func(systray.IMenu) {
		c.showWindow()
	})
	systray.SetOnDClick(func(systray.IMenu) {
		c.showWindow()
	})
	// Refresh labels immediately before showing the menu so recent clusters
	// reflect the latest UI selection / kubeconfig, not only session events.
	systray.SetOnRClick(func(menu systray.IMenu) {
		c.refresh(c.host.SessionState())
		_ = menu.ShowMenu()
	})

	c.mu.Lock()
	c.statusItem = systray.AddMenuItem(c.strings.statusIdle, "")
	c.statusItem.Disable()
	systray.AddSeparator()

	c.recentRoot = systray.AddMenuItem(c.strings.recent, "")
	c.recentEmpty = c.recentRoot.AddSubMenuItem(c.strings.noRecent, "")
	c.recentEmpty.Disable()
	c.recentItems = make([]*systray.MenuItem, 0, maxRecentClusters)
	for i := 0; i < maxRecentClusters; i++ {
		item := c.recentRoot.AddSubMenuItem("", "")
		item.Hide()
		c.recentItems = append(c.recentItems, item)
	}

	systray.AddSeparator()
	c.connectItem = systray.AddMenuItem(c.strings.connect, "")
	c.connectItem.Click(func() { c.connectPreferred() })
	c.disconnectItem = systray.AddMenuItem(c.strings.disconnect, "")
	c.disconnectItem.Click(func() { c.disconnect() })
	c.disconnectItem.Disable()

	systray.AddSeparator()
	c.showItem = systray.AddMenuItem(c.strings.showWindow, "")
	c.showItem.Click(func() { c.showWindow() })
	c.quitItem = systray.AddMenuItem(c.strings.quit, "")
	c.quitItem.Click(func() { c.requestQuit() })
	c.mu.Unlock()

	c.ready.Store(true)
	log.Printf("system tray ready")
	c.refresh(c.host.SessionState())
	c.host.Subscribe(func(state session.State) {
		c.refresh(state)
	})
}

func (c *Controller) onExit() {}

func (c *Controller) showWindow() {
	ctx := c.host.Context()
	if ctx == nil {
		return
	}
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
	runtime.WindowSetAlwaysOnTop(ctx, true)
	runtime.WindowSetAlwaysOnTop(ctx, false)
}

func (c *Controller) connectPreferred() {
	recents := c.host.RecentClusters()
	if len(recents) == 0 {
		c.showWindow()
		return
	}
	contextName, namespace := recents[0].Context, recents[0].Namespace
	if namespace == "" {
		namespace = "default"
	}
	go func() {
		if err := c.host.Connect(contextName, namespace); err != nil {
			log.Printf("tray connect %s/%s: %v", contextName, namespace, err)
		}
	}()
}

func (c *Controller) connectContext(contextName, namespace string) {
	if contextName == "" {
		return
	}
	if namespace == "" {
		namespace = "default"
	}
	go func() {
		if err := c.host.Connect(contextName, namespace); err != nil {
			log.Printf("tray connect %s/%s: %v", contextName, namespace, err)
		}
	}()
}

func (c *Controller) disconnect() {
	go func() {
		if err := c.host.Disconnect(); err != nil {
			log.Printf("tray disconnect: %v", err)
		}
	}()
}

func (c *Controller) requestQuit() {
	ctx := c.host.Context()
	if ctx == nil {
		c.forceQuit()
		return
	}
	c.showWindow()
	selection, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         c.strings.quitTitle,
		Message:       c.strings.quitMessage,
		DefaultButton: "No",
		CancelButton:  "No",
	})
	if err != nil {
		log.Printf("tray quit dialog: %v", err)
		return
	}
	normalized := strings.ToLower(strings.TrimSpace(selection))
	if normalized != "yes" && normalized != "ok" && selection != "确定" && selection != "是" {
		return
	}
	c.forceQuit()
}

func (c *Controller) forceQuit() {
	c.quitting.Store(true)
	if ctx := c.host.Context(); ctx != nil {
		runtime.Quit(ctx)
		return
	}
	systray.Quit()
}

func (c *Controller) refresh(state session.State) {
	if !c.ready.Load() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	status := c.strings.statusIdle
	tooltip := c.strings.tooltipDisconnected
	connecting := state.Phase == session.PhaseChecking ||
		state.Phase == session.PhaseInstalling ||
		state.Phase == session.PhaseDiscovering ||
		state.Phase == session.PhaseStarting
	switch {
	case state.Phase == session.PhaseConnected:
		label := state.Context
		if state.Namespace != "" {
			label = state.Context + " / " + state.Namespace
		}
		status = fmt.Sprintf(c.strings.statusConnected, label)
		tooltip = fmt.Sprintf(c.strings.tooltipConnected, label)
		c.connectItem.Disable()
		c.disconnectItem.Enable()
	case connecting:
		status = c.strings.statusConnecting
		tooltip = c.strings.statusConnecting
		c.connectItem.Disable()
		c.disconnectItem.Enable()
	case state.Phase == session.PhaseError:
		status = c.strings.statusError
		tooltip = c.strings.statusError
		c.connectItem.Enable()
		c.disconnectItem.Disable()
	default:
		c.connectItem.Enable()
		c.disconnectItem.Disable()
	}
	c.statusItem.SetTitle(status)
	systray.SetTooltip(tooltip)
	c.refreshRecentLocked(state)
}

func (c *Controller) refreshRecentLocked(state session.State) {
	recents := c.host.RecentClusters()
	if len(recents) == 0 {
		c.recentEmpty.SetTitle(c.strings.noRecent)
		c.recentEmpty.Show()
		c.recentEmpty.Disable()
		for _, item := range c.recentItems {
			item.Hide()
			item.Click(nil)
		}
		return
	}
	c.recentEmpty.Hide()
	for i, item := range c.recentItems {
		if i >= len(recents) {
			item.Hide()
			item.Click(nil)
			continue
		}
		entry := recents[i]
		title := entry.Context
		if entry.Namespace != "" {
			title = entry.Context + " / " + entry.Namespace
		}
		active := state.Phase == session.PhaseConnected && entry.Context == state.Context
		if active {
			title = "● " + title
		}
		item.SetTitle(title)
		contextName := entry.Context
		namespace := entry.Namespace
		if active {
			item.Click(nil)
			item.Disable()
		} else {
			item.Click(func() { c.connectContext(contextName, namespace) })
			item.Enable()
		}
		item.Show()
	}
}
