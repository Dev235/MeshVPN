package main

import (
	"flag"
	"log"
	"os"

	"github.com/meshvpn/meshvpn/internal/relay"
)

func main() {
	listenAddr := flag.String("addr", ":41641", "UDP Relay server listen address")
	flag.Parse()

	log.Printf("[MeshVPN Relay Server] Starting zero-knowledge forwarder on %s...", *listenAddr)

	srv := relay.NewServer(*listenAddr)
	if err := srv.Start(); err != nil {
		log.Fatalf("Relay server error: %v", err)
		os.Exit(1)
	}
}
