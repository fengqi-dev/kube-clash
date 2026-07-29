package helper

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateWorkDir ensures workDir is inside {home}/.kubeloop/sessions and has config.json.
func ValidateWorkDir(workDir, homeDir string) error {
	if workDir == "" {
		return fmt.Errorf("workDir is required")
	}
	clean, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("resolve workDir: %w", err)
	}
	info, err := os.Stat(clean)
	if err != nil {
		return fmt.Errorf("stat workDir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workDir is not a directory")
	}
	if homeDir == "" {
		homeDir, err = UserHomeDir()
		if err != nil {
			return err
		}
	}
	rootAbs, err := filepath.Abs(filepath.Join(homeDir, ".kubeloop", "sessions"))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("workDir must be under %s", rootAbs)
	}
	if _, err := os.Stat(filepath.Join(clean, "config.json")); err != nil {
		return fmt.Errorf("missing config.json in workDir: %w", err)
	}
	return nil
}
