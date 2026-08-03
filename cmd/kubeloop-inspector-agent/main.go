package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/inspectoragent"
)

var version = "dev"

func main() {
	socketPath := flag.String(
		"socket",
		"/var/run/kubeloop-inspector/agent.sock",
		"Inspector Agent Unix socket path",
	)
	probe := flag.Bool("probe", false, "probe an existing Inspector Agent")
	flag.Parse()

	if *probe {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := (&inspectoragent.Client{SocketPath: *socketPath}).Ping(ctx); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}

	logger := log.New(os.Stdout, "inspector-agent: ", log.LstdFlags|log.LUTC)
	ctx, cancel := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM,
	)
	defer cancel()
	listener, err := inspectoragent.ListenUnix(ctx, *socketPath)
	if err != nil {
		logger.Fatal(err)
	}
	defer listener.Close()
	server := inspectoragent.NewServer(logger)
	defer server.Close()
	logger.Printf("KubeLoop Inspector Agent %s listening on %s", version, *socketPath)
	if err := server.Serve(listener); err != nil {
		logger.Fatal(err)
	}
}
