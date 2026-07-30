package main

import (
	"embed"
	"io/fs"
	"path"
	"strings"

	"github.com/fengqi-dev/kube-loop/internal/helper"
)

// The README keeps the pattern valid for ordinary source builds. Release and
// IDE builds generate the platform helper in this directory before compiling
// the desktop application.
//
//go:embed build/embedded/*
var embeddedHelperFiles embed.FS

func init() {
	entries, err := fs.ReadDir(embeddedHelperFiles, "build/embedded")
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "README.md" || strings.HasPrefix(name, ".") {
			continue
		}
		content, readErr := embeddedHelperFiles.ReadFile(path.Join("build/embedded", name))
		if readErr != nil || len(content) == 0 {
			continue
		}
		helper.SetBundledFile(name, content)
	}
}
