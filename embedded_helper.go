package main

import (
	"embed"
	"runtime"

	"github.com/fengqi-dev/kube-loop/internal/helper"
)

// The README keeps the pattern valid for ordinary source builds. Release and
// IDE builds generate the platform helper in this directory before compiling
// the desktop application.
//
//go:embed build/embedded/*
var embeddedHelperFiles embed.FS

func init() {
	name := "kubeloop-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	content, err := embeddedHelperFiles.ReadFile("build/embedded/" + name)
	if err == nil {
		helper.SetBundledBinary(content)
	}
}
