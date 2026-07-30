//go:build ignore

// Command bundle-singbox downloads the pinned sing-box release for the target
// platform, verifies SHA-256, and stages stable filenames under build/bin.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

func main() {
	target := runtime.GOOS + "/" + runtime.GOARCH
	if len(os.Args) > 1 {
		target = os.Args[1]
	}
	goos, goarch, ok := strings.Cut(target, "/")
	if !ok || goos == "" || goarch == "" {
		fatalf("invalid target platform %q", target)
	}

	root, err := findRepositoryRoot()
	if err != nil {
		fatalf("%v", err)
	}
	outDir := filepath.Join(root, "build", "bin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}

	fmt.Printf("==> Fetching sing-box %s for %s/%s\n", singbox.Version, goos, goarch)
	if err := singbox.BundleRelease(goos, goarch, outDir); err != nil {
		fatalf("bundle sing-box: %v", err)
	}
	fmt.Printf("==> Staged sing-box into %s\n", outDir)
}

func findRepositoryRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above working directory")
		}
		dir = parent
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
