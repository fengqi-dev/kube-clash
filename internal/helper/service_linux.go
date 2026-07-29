//go:build linux

package helper

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const systemdUnitPath = "/etc/systemd/system/kubeloop-helper.service"

func enableService(binaryPath string) error {
	unit := fmt.Sprintf(`[Unit]
Description=KubeLoop Privileged Helper
After=network.target

[Service]
Type=simple
ExecStart=%s run
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
`, binaryPath)
	if err := os.WriteFile(systemdUnitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	commands := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "--now", "kubeloop-helper.service"},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func disableService() error {
	_ = exec.Command("systemctl", "disable", "--now", "kubeloop-helper.service").Run()
	_ = os.Remove(systemdUnitPath)
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return nil
}
