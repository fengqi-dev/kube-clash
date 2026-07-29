package helper

const (
	OpPing   = "ping"
	OpStart  = "start"
	OpStop   = "stop"
	OpStatus = "status"
)

// Request is a single JSON-line RPC request.
type Request struct {
	Op         string `json:"op"`
	Token      string `json:"token,omitempty"`
	WorkDir    string `json:"workDir,omitempty"`
	BinaryPath string `json:"binaryPath,omitempty"`
}

// Response is a single JSON-line RPC response.
type Response struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Version   string `json:"version,omitempty"`
	Installed bool   `json:"installed,omitempty"`
	Running   bool   `json:"running,omitempty"`
	PID       int    `json:"pid,omitempty"`
}

// AuthFile is persisted under the system state directory.
type AuthFile struct {
	Token   string `json:"token"`
	UID     int    `json:"uid"`
	Version string `json:"version"`
	HomeDir string `json:"homeDir,omitempty"`
}

// Status describes helper installation from the desktop app's point of view.
type Status struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Version   string `json:"version,omitempty"`
	Expected  string `json:"expected"`
	Socket    string `json:"socket"`
	Error     string `json:"error,omitempty"`
}
