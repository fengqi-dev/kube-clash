package store

import (
	"path/filepath"
	"testing"
)

func TestKubeconfigFilesAddRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	first := filepath.Join(dir, "a.yaml")
	second := filepath.Join(dir, "b.yaml")
	firstAbs, _ := filepath.Abs(first)
	secondAbs, _ := filepath.Abs(second)

	if err := s.AddKubeconfigFile(first); err != nil {
		t.Fatalf("Add first: %v", err)
	}
	if err := s.AddKubeconfigFile(first); err != nil {
		t.Fatalf("Add duplicate: %v", err)
	}
	if err := s.AddKubeconfigFile(second); err != nil {
		t.Fatalf("Add second: %v", err)
	}
	files := s.KubeconfigFiles()
	if len(files) != 2 || files[0] != firstAbs || files[1] != secondAbs {
		t.Fatalf("files = %v, want [%s %s]", files, firstAbs, secondAbs)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := reopened.KubeconfigFiles(); len(got) != 2 {
		t.Fatalf("persisted files = %v", got)
	}

	if err := reopened.RemoveKubeconfigFile(first); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := reopened.KubeconfigFiles(); len(got) != 1 || got[0] != secondAbs {
		t.Fatalf("after remove = %v, want [%s]", got, secondAbs)
	}
	if err := reopened.RemoveKubeconfigFile(first); err == nil {
		t.Fatal("expected missing remove error")
	}
}
