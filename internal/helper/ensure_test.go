package helper

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestMaterializeBundledHelper(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	SetBundledBinary([]byte("first helper"))
	t.Cleanup(func() { SetBundledBinary(nil) })

	path, ok, err := materializeBundledHelper()
	if err != nil {
		t.Fatalf("materialize bundled helper: %v", err)
	}
	if !ok {
		t.Fatal("materialize bundled helper reported no embedded binary")
	}

	name := "kubeloop-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	wantPath := filepath.Join(home, ".kubeloop", "helper", name)
	if path != wantPath {
		t.Fatalf("materialized path = %q, want %q", path, wantPath)
	}
	assertFileContent(t, path, "first helper")

	SetBundledBinary([]byte("updated helper"))
	updatedPath, ok, err := materializeBundledHelper()
	if err != nil {
		t.Fatalf("update bundled helper: %v", err)
	}
	if !ok || updatedPath != path {
		t.Fatalf("updated helper = (%q, %v), want (%q, true)", updatedPath, ok, path)
	}
	assertFileContent(t, path, "updated helper")

	SetBundledBinary([]byte("concurrent helper"))
	var wait sync.WaitGroup
	errors := make(chan error, 16)
	for i := 0; i < cap(errors); i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			concurrentPath, concurrentOK, concurrentErr := materializeBundledHelper()
			if concurrentErr != nil {
				errors <- concurrentErr
				return
			}
			if !concurrentOK || concurrentPath != path {
				errors <- fmt.Errorf("materialized helper = (%q, %v), want (%q, true)", concurrentPath, concurrentOK, path)
			}
		}()
	}
	wait.Wait()
	close(errors)
	for concurrentErr := range errors {
		t.Error(concurrentErr)
	}
	assertFileContent(t, path, "concurrent helper")

	if err := os.WriteFile(path, []byte("replaced after materialization"), 0o700); err != nil {
		t.Fatalf("replace materialized helper: %v", err)
	}
	gotSHA256, err := bundledHelperSHA256(path)
	if err != nil {
		t.Fatalf("hash bundled helper: %v", err)
	}
	wantSHA256 := fmt.Sprintf("%x", sha256.Sum256([]byte("concurrent helper")))
	if gotSHA256 != wantSHA256 {
		t.Fatalf("bundled helper SHA-256 = %q, want %q", gotSHA256, wantSHA256)
	}
	if _, _, err := materializeBundledHelper(); err != nil {
		t.Fatalf("restore bundled helper: %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat materialized helper: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("materialized helper mode = %o, want 700", got)
		}
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if got := string(content); got != want {
		t.Fatalf("%s content = %q, want %q", path, got, want)
	}
}
