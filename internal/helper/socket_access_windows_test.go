//go:build windows

package helper

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestConfigureHelperSocketAccess(t *testing.T) {
	path := t.TempDir() + `\helper.sock`
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("create socket placeholder: %v", err)
	}
	ownerSID, err := currentUserSID()
	if err != nil {
		t.Fatalf("find current user SID: %v", err)
	}
	if err := configureHelperSocketAccess(path, ownerSID); err != nil {
		t.Fatalf("configure helper socket access: %v", err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("read helper socket security descriptor: %v", err)
	}
	if descriptor == nil || !descriptor.IsValid() {
		t.Fatal("helper socket security descriptor is invalid")
	}
	allowed, err := daclAllowsSID(descriptor, ownerSID)
	if err != nil {
		t.Fatalf("inspect helper socket DACL: %v", err)
	}
	if !allowed {
		t.Fatalf("helper socket DACL %q does not allow owner SID %q", descriptor.String(), ownerSID)
	}
}

func TestConfigureHelperSocketAccessRejectsMissingSID(t *testing.T) {
	if err := configureHelperSocketAccess(t.TempDir()+`\missing.sock`, ""); err == nil {
		t.Fatal("configure helper socket access accepted an empty owner SID")
	}
}
