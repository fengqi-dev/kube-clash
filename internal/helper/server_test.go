package helper

import "testing"

func TestDispatchRejectsLegacyExecutableRequest(t *testing.T) {
	server := NewServer(AuthFile{Token: "secret"})
	response := server.dispatch(Request{Op: OpStart, Token: "secret"})
	if response.OK || response.Error != "session is required" {
		t.Fatalf("dispatch() = %#v", response)
	}
}

func TestDispatchRequiresValidSessionIDForStop(t *testing.T) {
	server := NewServer(AuthFile{Token: "secret"})
	response := server.dispatch(Request{
		Op: OpStop, Token: "secret", SessionID: "../../session",
	})
	if response.OK {
		t.Fatalf("dispatch() unexpectedly accepted an unsafe session ID")
	}
}
