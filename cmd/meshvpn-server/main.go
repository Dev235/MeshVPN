package main

import (
	"flag"
	"log"
	"os"

	"github.com/meshvpn/meshvpn/internal/control"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP REST Control Server listen address")
	stunAddr := flag.String("stun-addr", ":3478", "UDP STUN reflector listen address")
	dbPath := flag.String("db", "meshvpn-control.json", "Path to control database file")
	flag.Parse()

	log.Printf("[MeshVPN Control Server] Starting on %s...", *addr)

	srv, err := control.NewServer(*addr, *stunAddr, *dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize control server: %v", err)
		os.Exit(1)
	}

	if err := srv.Start(); err != nil {
		log.Fatalf("Control server error: %v", err)
	}
}
