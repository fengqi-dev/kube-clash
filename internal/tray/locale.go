package tray

import (
	"os"
	"strings"
)

type stringsLocal struct {
	tooltipDisconnected string
	tooltipConnected    string
	statusIdle          string
	statusConnecting    string
	statusConnected     string
	statusError         string
	recent              string
	noRecent            string
	connect             string
	disconnect          string
	showWindow          string
	quit                string
	quitTitle           string
	quitMessage         string
}

func locale() stringsLocal {
	lang := strings.ToLower(os.Getenv("LANG"))
	if lang == "" {
		lang = strings.ToLower(os.Getenv("LC_ALL"))
	}
	if strings.HasPrefix(lang, "zh") || isWindowsChinese() {
		return stringsLocal{
			tooltipDisconnected: "KubeLoop — 未连接",
			tooltipConnected:    "KubeLoop — 已连接：%s",
			statusIdle:          "状态：未连接",
			statusConnecting:    "状态：连接中…",
			statusConnected:     "状态：已连接 — %s",
			statusError:         "状态：连接失败",
			recent:              "最近集群",
			noRecent:            "暂无最近集群",
			connect:             "连接",
			disconnect:          "断开连接",
			showWindow:          "打开主窗口",
			quit:                "退出",
			quitTitle:           "退出 KubeLoop",
			quitMessage:         "确定要退出 KubeLoop 吗？关闭窗口只会隐藏到托盘，不会断开连接。",
		}
	}
	return stringsLocal{
		tooltipDisconnected: "KubeLoop — Disconnected",
		tooltipConnected:    "KubeLoop — Connected: %s",
		statusIdle:          "Status: Disconnected",
		statusConnecting:    "Status: Connecting…",
		statusConnected:     "Status: Connected — %s",
		statusError:         "Status: Connection failed",
		recent:              "Recent clusters",
		noRecent:            "No recent clusters",
		connect:             "Connect",
		disconnect:          "Disconnect",
		showWindow:          "Open main window",
		quit:                "Quit",
		quitTitle:           "Quit KubeLoop",
		quitMessage:         "Are you sure you want to quit KubeLoop? Closing the window only hides it to the tray and does not disconnect.",
	}
}
