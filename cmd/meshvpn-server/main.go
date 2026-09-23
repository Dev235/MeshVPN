package main

import (
	"flag"
	"log"
	"os"

	"github.com/meshvpn/meshvpn/internal/control"
	"github.com/meshvpn/meshvpn/internal/relay"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP REST Control Server listen address")
	stunAddr := flag.String("stun-addr", ":3478", "UDP STUN reflector listen address")
	relayAddr := flag.String("relay-addr", ":41641", "UDP Relay server listen address (empty to disable)")
	dbPath := flag.String("db", "meshvpn-control.json", "Path to control database file")
	flag.Parse()

	log.Printf("[MeshVPN Server] Starting all-in-one native backend on %s...", *addr)

	// Start embedded zero-knowledge relay forwarder
	if *relayAddr != "" {
		go func() {
			log.Printf("[MeshVPN Relay] Embedded zero-knowledge relay forwarder listening on %s (UDP)...", *relayAddr)
			relaySrv := relay.NewServer(*relayAddr)
			if err := relaySrv.Start(); err != nil {
				log.Printf("[MeshVPN Relay] Relay error: %v", err)
			}
		}()
	}

	srv, err := control.NewServer(*addr, *stunAddr, *dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize control server: %v", err)
		os.Exit(1)
	}

	if err := srv.Start(); err != nil {
		log.Fatalf("Control server error: %v", err)
	}
}
