package main

import (
	"flag"
	"log"
	"net"
	"os"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/gateway"
)

var version = "dev"

func main() {
	listenAddress := flag.String("listen", ":1080", "gateway listen address")
	flag.Parse()

	logger := log.New(os.Stdout, "gateway: ", log.LstdFlags|log.LUTC)
	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Printf("kube-loop gateway %s listening on %s", version, *listenAddress)
	server := &gateway.Server{Logger: logger, DialTimeout: 10 * time.Second}
	if err := server.Serve(listener); err != nil {
		logger.Fatal(err)
	}
}
