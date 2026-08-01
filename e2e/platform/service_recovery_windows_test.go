//go:build e2e && windows

package platform

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows/registry"

	"github.com/fengqi-dev/kube-loop/internal/helper"
)

func TestWindowsInstallRecoversMissingServiceImagePath(t *testing.T) {
	requirePlatformE2E(t)

	serviceKeyPath := `SYSTEM\CurrentControlSet\Services\` + helper.ServiceNameWin()
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		serviceKeyPath,
		registry.QUERY_VALUE|registry.SET_VALUE,
	)
	if err != nil {
		t.Fatalf("open service registry key: %v", err)
	}
	original, _, err := key.GetStringValue("ImagePath")
	if err != nil {
		_ = key.Close()
		t.Fatalf("read service ImagePath: %v", err)
	}
	if err := key.DeleteValue("ImagePath"); err != nil {
		_ = key.Close()
		t.Fatalf("remove service ImagePath: %v", err)
	}
	if err := key.Close(); err != nil {
		t.Fatalf("close service registry key: %v", err)
	}
	t.Cleanup(func() {
		restore, openErr := registry.OpenKey(
			registry.LOCAL_MACHINE,
			serviceKeyPath,
			registry.SET_VALUE,
		)
		if openErr != nil {
			t.Errorf("reopen service registry key: %v", openErr)
			return
		}
		defer restore.Close()
		if err := restore.SetStringValue("ImagePath", original); err != nil {
			t.Errorf("restore service ImagePath: %v", err)
		}
	})

	token, err := helper.ReadUserToken()
	if err != nil {
		t.Fatal(err)
	}
	home, err := helper.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	source, err := filepath.Abs(filepath.Join("..", "..", "build", "embedded", "kubeloop-helper.exe"))
	if err != nil {
		t.Fatal(err)
	}
	singBox, err := filepath.Abs(filepath.Join("..", "..", "build", "bin", "sing-box"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("helper source: %v", err)
	}
	if _, err := os.Stat(singBox); err != nil {
		t.Fatalf("sing-box source: %v", err)
	}

	if err := helper.InstallFromCLI(
		source,
		token,
		0,
		helper.Version,
		home,
		current.Uid,
		singBox,
	); err != nil {
		t.Fatalf("reinstall helper with missing ImagePath: %v", err)
	}
}
