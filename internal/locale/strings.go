package locale

// Strings holds native UI copy (tray, system dialogs). Frontend has its own i18n.
type Strings struct {
	TooltipDisconnected string
	TooltipConnected    string
	StatusIdle          string
	StatusConnecting    string
	StatusConnected     string
	StatusError         string
	Recent              string
	NoRecent            string
	Connect             string
	Disconnect          string
	ShowWindow          string
	Quit                string
	QuitTitle           string
	QuitMessage         string

	SelectKubeconfig string
	KubeconfigFilter string
}

// T returns the string table for the preferred OS language.
func T() Strings {
	if IsChinese() {
		return zh
	}
	return en
}

var en = Strings{
	TooltipDisconnected: "KubeLoop — Disconnected",
	TooltipConnected:    "KubeLoop — Connected: %s",
	StatusIdle:          "Status: Disconnected",
	StatusConnecting:    "Status: Connecting…",
	StatusConnected:     "Status: Connected — %s",
	StatusError:         "Status: Connection failed",
	Recent:              "Recent clusters",
	NoRecent:            "No recent clusters",
	Connect:             "Connect",
	Disconnect:          "Disconnect",
	ShowWindow:          "Open main window",
	Quit:                "Quit",
	QuitTitle:           "Quit KubeLoop",
	QuitMessage:         "Are you sure you want to quit KubeLoop? Closing the window only hides it to the tray and does not disconnect.",

	SelectKubeconfig: "Select kubeconfig",
	KubeconfigFilter: "Kubeconfig",
}

var zh = Strings{
	TooltipDisconnected: "KubeLoop — 未连接",
	TooltipConnected:    "KubeLoop — 已连接：%s",
	StatusIdle:          "状态：未连接",
	StatusConnecting:    "状态：连接中…",
	StatusConnected:     "状态：已连接 — %s",
	StatusError:         "状态：连接失败",
	Recent:              "最近集群",
	NoRecent:            "暂无最近集群",
	Connect:             "连接",
	Disconnect:          "断开连接",
	ShowWindow:          "打开主窗口",
	Quit:                "退出",
	QuitTitle:           "退出 KubeLoop",
	QuitMessage:         "确定要退出 KubeLoop 吗？关闭窗口只会隐藏到托盘，不会断开连接。",

	SelectKubeconfig: "选择 kubeconfig",
	KubeconfigFilter: "Kubeconfig",
}
