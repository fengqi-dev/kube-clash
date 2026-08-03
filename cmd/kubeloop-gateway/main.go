package main

import (
	"flag"
	"log"
	"net"
	"os"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/gateway"
	"github.com/fengqi-dev/kube-loop/internal/inspectoragent"
)

var version = "dev"

func main() {
	listenAddress := flag.String("listen", ":1080", "gateway listen address")
	inspectorSocket := flag.String(
		"inspector-agent-socket", "", "Inspector Agent Unix socket path",
	)
	flag.Parse()

	logger := log.New(os.Stdout, "gateway: ", log.LstdFlags|log.LUTC)
	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Printf("kube-loop gateway %s listening on %s", version, *listenAddress)
	server := gateway.NewServer(logger, 10*time.Second)
	if *inspectorSocket != "" {
		server.SetInspectorEngine(
			&inspectoragent.Client{SocketPath: *inspectorSocket},
			"native-http/1",
		)
		logger.Printf("Inspector Agent configured at %s", *inspectorSocket)
	}
	if err := server.Serve(listener); err != nil {
		logger.Fatal(err)
	}
}
