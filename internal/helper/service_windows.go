//go:build windows

package helper

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func enableService(binaryPath string) error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer manager.Disconnect()

	service, openErr := manager.OpenService(ServiceNameWin)
	if openErr == nil {
		defer service.Close()
		cfg, configErr := service.Config()
		if configErr != nil {
			return fmt.Errorf("read windows service config: %w", configErr)
		}
		cfg.BinaryPathName = syscall.EscapeArg(binaryPath) + " run"
		cfg.DisplayName = "KubeLoop Helper"
		cfg.Description = "Privileged helper for KubeLoop TUN networking"
		cfg.StartType = mgr.StartAutomatic
		if err := service.UpdateConfig(cfg); err != nil {
			return fmt.Errorf("update windows service: %w", err)
		}
		if err := service.Start(); err != nil {
			return fmt.Errorf("restart windows service: %w", err)
		}
		return waitServiceRunning(service, 15*time.Second)
	}
	if !errors.Is(openErr, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return fmt.Errorf("open windows service: %w", openErr)
	}

	cfg := mgr.Config{
		DisplayName: "KubeLoop Helper",
		Description: "Privileged helper for KubeLoop TUN networking",
		StartType:   mgr.StartAutomatic,
	}
	service, err = manager.CreateService(ServiceNameWin, binaryPath, cfg, "run")
	if err != nil {
		return fmt.Errorf("create windows service: %w", err)
	}
	defer service.Close()
	if err := service.Start(); err != nil {
		return fmt.Errorf("start windows service: %w", err)
	}
	return waitServiceRunning(service, 15*time.Second)
}

func stopServiceForUpgrade() error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(ServiceNameWin)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open windows service: %w", err)
	}
	defer service.Close()
	return stopWindowsService(service, 20*time.Second)
}

func stopWindowsService(service *mgr.Service, timeout time.Duration) error {
	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("query windows service: %w", err)
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, err := service.Control(svc.Stop); err != nil &&
		!errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("stop windows service: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, queryErr := service.Query()
		if queryErr != nil {
			return fmt.Errorf("query stopping windows service: %w", queryErr)
		}
		if status.State == svc.Stopped {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s to stop", ServiceNameWin)
}

func disableService() error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(ServiceNameWin)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open windows service: %w", err)
	}
	if err := stopWindowsService(service, 20*time.Second); err != nil {
		_ = service.Close()
		return err
	}
	if err := service.Delete(); err != nil {
		_ = service.Close()
		return fmt.Errorf("delete windows service: %w", err)
	}
	if err := service.Close(); err != nil {
		return fmt.Errorf("close windows service: %w", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		probe, openErr := manager.OpenService(ServiceNameWin)
		if errors.Is(openErr, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		if openErr != nil {
			return fmt.Errorf("verify windows service deletion: %w", openErr)
		}
		_ = probe.Close()
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s deletion", ServiceNameWin)
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
