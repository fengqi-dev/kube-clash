package helper

import (
	"path"
	"strings"
)

// installRootFromWindowsExe maps a packaged Windows binary path to the app
// install root (the directory that contains KubeLoop.exe and resources\).
// Parsing uses slash-separated paths so unit tests can run on non-Windows hosts.
func installRootFromWindowsExe(exe string) string {
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return ""
	}
	// Always normalize Windows separators — filepath.ToSlash is a no-op for '\' on Unix.
	normalized := strings.ReplaceAll(exe, `\`, "/")
	normalized = strings.TrimSuffix(normalized, "/")
	dir := path.Dir(normalized)
	if dir == "." || dir == "/" {
		return ""
	}
	if strings.EqualFold(path.Base(dir), "resources") {
		dir = path.Clean(path.Join(dir, ".."))
	}
	if dir == "." || dir == "/" {
		return ""
	}
	// Keep Windows separators so results match os.Executable() on Windows.
	return strings.ReplaceAll(dir, "/", `\`)
}
