// Package helperapi defines the versioned RPC contract shared by the desktop
// process and the privileged helper.
package helperapi

import "github.com/fengqi-dev/kube-loop/internal/singbox"

const (
	ProtocolVersion = 6

	OpPing               = "ping"
	OpStart              = "start"
	OpStop               = "stop"
	OpStopAll            = "stop-all"
	OpStatus             = "status"
	OpUpdateDNS          = "update-dns"
	OpInstallInspectorCA = "install-inspector-ca"
	OpRemoveInspectorCA  = "remove-inspector-ca"
	OpInspectorCAStatus  = "inspector-ca-status"
)

// Request is a single JSON-line RPC request.
type Request struct {
	Op             string               `json:"op"`
	Token          string               `json:"token,omitempty"`
	Session        *singbox.SessionSpec `json:"session,omitempty"`
	SessionID      string               `json:"sessionId,omitempty"`
	DNS            *singbox.DNSMeta     `json:"dns,omitempty"`
	CertificatePEM []byte               `json:"certificatePEM,omitempty"`
}

// Response is a single JSON-line RPC response.
type Response struct {
	OK                 bool     `json:"ok"`
	Error              string   `json:"error,omitempty"`
	Version            string   `json:"version,omitempty"`
	Protocol           int      `json:"protocol,omitempty"`
	Installed          bool     `json:"installed,omitempty"`
	Running            bool     `json:"running,omitempty"`
	CoreReady          bool     `json:"coreReady,omitempty"`
	PID                int      `json:"pid,omitempty"`
	ActiveSessions     []string `json:"activeSessions,omitempty"`
	CertificateTrusted bool     `json:"certificateTrusted,omitempty"`
}

// AuthFile is persisted under the system state directory.
type AuthFile struct {
	Token       string `json:"token"`
	UID         int    `json:"uid"`
	Version     string `json:"version"`
	HomeDir     string `json:"homeDir,omitempty"`
	OwnerSID    string `json:"ownerSid,omitempty"`
	SingBoxPath string `json:"singBoxPath,omitempty"`
}

// Status describes helper installation from the desktop app's point of view.
type Status struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	CoreReady bool   `json:"coreReady"`
	Version   string `json:"version,omitempty"`
	Protocol  int    `json:"protocol,omitempty"`
	Expected  string `json:"expected"`
	Socket    string `json:"socket"`
	Error     string `json:"error,omitempty"`
}
