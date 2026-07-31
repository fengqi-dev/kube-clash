package helper

import (
	"runtime"
	"strings"
	"testing"
)

func TestDevBuildServiceIdentity(t *testing.T) {
	prev := Version
	t.Cleanup(func() { Version = prev })

	Version = "dev"
	if !IsDevBuild() {
		t.Fatal("expected IsDevBuild for Version=dev")
	}
	if got := ServiceLabel(); got != ServiceLabelDev {
		t.Fatalf("ServiceLabel=%q want %q", got, ServiceLabelDev)
	}
	if got := ServiceNameWin(); got != ServiceNameWinDev {
		t.Fatalf("ServiceNameWin=%q want %q", got, ServiceNameWinDev)
	}
	if got := HelperBinaryBaseName(); got != "kubeloop-helper-dev" {
		t.Fatalf("HelperBinaryBaseName=%q", got)
	}
	if got := InstallProductDir(); got != "KubeLoop-Dev" {
		t.Fatalf("InstallProductDir=%q", got)
	}
	if got := SystemdUnitName(); got != "kubeloop-helper-dev.service" {
		t.Fatalf("SystemdUnitName=%q", got)
	}
	if !strings.Contains(ServiceDisplayName(), "dev") {
		t.Fatalf("ServiceDisplayName=%q missing dev", ServiceDisplayName())
	}
	if runtime.GOOS != "windows" {
		if got := SocketPath(); !strings.Contains(got, "kubeloop-dev") {
			t.Fatalf("SocketPath=%q want kubeloop-dev", got)
		}
		if got := SystemStateDir(); !strings.HasSuffix(got, "kubeloop-dev") {
			t.Fatalf("SystemStateDir=%q", got)
		}
	}
}

func TestReleaseBuildServiceIdentity(t *testing.T) {
	prev := Version
	t.Cleanup(func() { Version = prev })

	Version = "v1.2.3"
	if IsDevBuild() {
		t.Fatal("expected release build for Version=v1.2.3")
	}
	if got := ServiceLabel(); got != ServiceLabelRelease {
		t.Fatalf("ServiceLabel=%q want %q", got, ServiceLabelRelease)
	}
	if got := ServiceNameWin(); got != ServiceNameWinRelease {
		t.Fatalf("ServiceNameWin=%q want %q", got, ServiceNameWinRelease)
	}
	if got := HelperBinaryBaseName(); got != "kubeloop-helper" {
		t.Fatalf("HelperBinaryBaseName=%q", got)
	}
	if got := InstallProductDir(); got != "KubeLoop" {
		t.Fatalf("InstallProductDir=%q", got)
	}
	if runtime.GOOS != "windows" {
		if got := SocketPath(); got != "/var/run/kubeloop/helper.sock" {
			t.Fatalf("SocketPath=%q", got)
		}
		if got := SystemStateDir(); got != "/var/lib/kubeloop" {
			t.Fatalf("SystemStateDir=%q", got)
		}
	}
}
