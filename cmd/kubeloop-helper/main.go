package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/fengqi-dev/kube-loop/internal/helper"
)

var version = "dev"

func main() {
	if version != "" && version != "dev" {
		helper.Version = version
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "install":
		fs := flag.NewFlagSet("install", flag.ExitOnError)
		source := fs.String("source", "", "path to helper binary to install")
		token := fs.String("token", "", "IPC token")
		uid := fs.Int("uid", 0, "owner uid")
		ver := fs.String("version", helper.Version, "helper version")
		home := fs.String("home", "", "user home directory for session allowlist")
		_ = fs.Parse(os.Args[2:])
		if err := helper.InstallFromCLI(*source, *token, *uid, *ver, *home); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "uninstall":
		if err := helper.UninstallFromCLI(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "run":
		auth, err := helper.ReadSystemAuth()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		server := helper.NewServer(auth)
		if err := helper.RunService(server); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "version", "--version", "-version":
		fmt.Println(helper.Version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `KubeLoop privileged helper

Usage:
  kubeloop-helper install --source PATH --token TOKEN [--uid N] [--version V]
  kubeloop-helper uninstall
  kubeloop-helper run
  kubeloop-helper version
`)
}
