//go:build darwin

package helper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const launchdPlistPath = "/Library/LaunchDaemons/" + ServiceLabel + ".plist"

func enableService(binaryPath string) error {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>run</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/var/log/kubeloop-helper.log</string>
  <key>StandardErrorPath</key><string>/var/log/kubeloop-helper.log</string>
</dict>
</plist>
`, ServiceLabel, binaryPath)
	if err := os.MkdirAll(filepath.Dir(launchdPlistPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(launchdPlistPath, []byte(plist), 0o644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", "system/"+ServiceLabel).Run()
	cmd := exec.Command("launchctl", "bootstrap", "system", launchdPlistPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Older macOS fallback.
		cmd = exec.Command("launchctl", "load", "-w", launchdPlistPath)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("launchctl load helper: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	_ = exec.Command("launchctl", "enable", "system/"+ServiceLabel).Run()
	_ = exec.Command("launchctl", "kickstart", "-k", "system/"+ServiceLabel).Run()
	return nil
}

func disableService() error {
	_ = exec.Command("launchctl", "bootout", "system/"+ServiceLabel).Run()
	_ = exec.Command("launchctl", "unload", "-w", launchdPlistPath).Run()
	_ = os.Remove(launchdPlistPath)
	return nil
}
