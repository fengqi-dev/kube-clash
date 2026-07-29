//go:build windows

package helper

import (
	"fmt"
	"os/exec"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func enableService(binaryPath string) error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer manager.Disconnect()

	if service, openErr := manager.OpenService(ServiceNameWin); openErr == nil {
		_ = service.Close()
		manager.Disconnect()
		_ = disableService()
		manager, err = mgr.Connect()
		if err != nil {
			return err
		}
		defer manager.Disconnect()
	}

	cfg := mgr.Config{
		DisplayName: "KubeLoop Helper",
		Description: "Privileged helper for KubeLoop TUN networking",
		StartType:   mgr.StartAutomatic,
	}
	service, err := manager.CreateService(ServiceNameWin, binaryPath, cfg, "run")
	if err != nil {
		return fmt.Errorf("create windows service: %w", err)
	}
	defer service.Close()
	if err := service.Start("run"); err != nil {
		return fmt.Errorf("start windows service: %w", err)
	}
	return waitServiceRunning(service, 15*time.Second)
}

func disableService() error {
	manager, err := mgr.Connect()
	if err != nil {
		_ = exec.Command("sc.exe", "stop", ServiceNameWin).Run()
		_ = exec.Command("sc.exe", "delete", ServiceNameWin).Run()
		return nil
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(ServiceNameWin)
	if err != nil {
		return nil
	}
	defer service.Close()
	_, _ = service.Control(svc.Stop)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		status, queryErr := service.Query()
		if queryErr != nil || status.State == svc.Stopped {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = service.Delete()
	return nil
}

func waitServiceRunning(service *mgr.Service, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return err
		}
		if status.State == svc.Running {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s to run", ServiceNameWin)
}
