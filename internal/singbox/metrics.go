package singbox

// Metrics is the KubeLoop-owned session metrics contract (not Clash-shaped).
type Metrics struct {
	DownloadTotal     int64        `json:"downloadTotal"`
	UploadTotal       int64        `json:"uploadTotal"`
	Memory            uint64       `json:"memory,omitempty"`
	ActiveConnections int          `json:"activeConnections"`
	Connections       []Connection `json:"connections"`
}

type Connection struct {
	ID            string `json:"id"`
	Network       string `json:"network"`
	Source        string `json:"source"`
	Destination   string `json:"destination"`
	Process       string `json:"process"`
	Upload        int64  `json:"upload"`
	Download      int64  `json:"download"`
	UploadSpeed   int64  `json:"uploadSpeed,omitempty"`
	DownloadSpeed int64  `json:"downloadSpeed,omitempty"`
	StartedAt     string `json:"startedAt"`
	Outbound      string `json:"outbound"`
	Rule          string `json:"rule"`
}
