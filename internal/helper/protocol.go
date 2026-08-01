package helper

import "github.com/fengqi-dev/kube-loop/internal/helperapi"

const (
	ProtocolVersion = helperapi.ProtocolVersion

	OpPing      = helperapi.OpPing
	OpStart     = helperapi.OpStart
	OpStop      = helperapi.OpStop
	OpStopAll   = helperapi.OpStopAll
	OpStatus    = helperapi.OpStatus
	OpUpdateDNS = helperapi.OpUpdateDNS
)

type Request = helperapi.Request
type Response = helperapi.Response
type AuthFile = helperapi.AuthFile
type Status = helperapi.Status
