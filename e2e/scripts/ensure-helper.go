//go:build ignore

// Ensures the privileged helper is installed (may prompt for admin on macOS).
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/helper"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	src := filepath.Join(root, "build", "embedded", "kubeloop-helper")
	if _, err := os.Stat(src); err != nil {
		fatal(fmt.Errorf("helper binary missing at %s; run ./build/bundle-helper.sh", src))
	}
	dest := filepath.Join(root, "build", "bin", "kubeloop-helper")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal(err)
	}
	if err := copyFile(src, dest); err != nil {
		fatal(err)
	}
	_ = os.Chmod(dest, 0o755)

	singBox := filepath.Join(root, "build", "bin", "sing-box")
	if _, err := os.Stat(singBox); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		path, ensureErr := (&singbox.Installer{}).Ensure(ctx)
		cancel()
		if ensureErr != nil {
			fatal(ensureErr)
		}
		if err := copyFile(path, singBox); err != nil {
			fatal(err)
		}
		_ = os.Chmod(singBox, 0o755)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := helper.EnsureInstall(ctx); err != nil {
		fatal(err)
	}
	fmt.Println("helper ready")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dst)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
