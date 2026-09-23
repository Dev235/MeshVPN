package relay

import (
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/meshvpn/meshvpn/pkg/protocol"
)

// Server represents the standalone zero-knowledge meshvpn-relay server.
type Server struct {
	listenAddr string
	conn       *net.UDPConn
	sessions   map[string]*net.UDPAddr // Hex Node ID -> Remote UDP Address
	mu         sync.RWMutex
}

// NewServer initializes a Relay Server.
func NewServer(listenAddr string) *Server {
	if listenAddr == "" {
		listenAddr = ":41641"
	}
	return &Server{
		listenAddr: listenAddr,
		sessions:   make(map[string]*net.UDPAddr),
	}
}

// Start runs the UDP relay packet forwarding loop.
func (s *Server) Start() error {
	addr, err := net.ResolveUDPAddr("udp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve relay address %s: %w", s.listenAddr, err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on relay UDP %s: %w", s.listenAddr, err)
	}
	s.conn = conn
	defer s.conn.Close()

	log.Printf("[Zero-Knowledge Relay] Listening on UDP %s (End-to-End WireGuard Encrypted)", s.listenAddr)

	buf := make([]byte, 65535)
	for {
		n, remoteAddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		if n < protocol.RelayHeaderLen {
			continue
		}

		header, err := protocol.DeserializeRelayHeader(buf[:n])
		if err != nil {
			continue
		}

		s.handleRelayPacket(header, buf[protocol.RelayHeaderLen:n], remoteAddr)
	}
}

func (s *Server) handleRelayPacket(header *protocol.RelayHeader, payload []byte, remoteAddr *net.UDPAddr) {
	targetHex := hex.EncodeToString(header.TargetNodeID[:])

	switch header.PacketType {
	case protocol.RelayTypeRegister:
		// Client registering presence on relay
		s.mu.Lock()
		s.sessions[targetHex] = remoteAddr
		s.mu.Unlock()

		// Send Pong acknowledgement
		respHeader := protocol.RelayHeader{
			Magic:         protocol.RelayMagic,
			Version:       protocol.RelayVersion,
			PacketType:    protocol.RelayTypePong,
			PayloadLength: 0,
			TargetNodeID:  header.TargetNodeID,
		}
		_, _ = s.conn.WriteToUDP(respHeader.Serialize(), remoteAddr)

	case protocol.RelayTypeData:
		// Forward opaque WireGuard ciphertext payload to target node
		s.mu.RLock()
		targetAddr, exists := s.sessions[targetHex]
		s.mu.RUnlock()

		if exists && targetAddr != nil {
			// Re-wrap relay packet
			outBuf := append(header.Serialize(), payload...)
			_, _ = s.conn.WriteToUDP(outBuf, targetAddr)
		}
	}
}

// Client Tunnel handles local relay client encapsulation for fallback mode.
type ClientTunnel struct {
	relayAddr  string
	nodeIDHex  string
	nodeIDBytes [16]byte
	conn       *net.UDPConn
	mu         sync.RWMutex
}

// NewClientTunnel initializes a client relay tunnel.
func NewClientTunnel(relayAddr, nodeIDHex string) (*ClientTunnel, error) {
	var nBytes [16]byte
	b, err := hex.DecodeString(nodeIDHex)
	if err == nil && len(b) == 16 {
		copy(nBytes[:], b)
	}

	raddr, err := net.ResolveUDPAddr("udp", relayAddr)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, err
	}

	client := &ClientTunnel{
		relayAddr:   relayAddr,
		nodeIDHex:   nodeIDHex,
		nodeIDBytes: nBytes,
		conn:        conn,
	}

	// Register presence on relay
	if err := client.Register(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return client, nil
}

// Register sends registration packet to keep relay session alive.
func (c *ClientTunnel) Register() error {
	header := protocol.RelayHeader{
		Magic:         protocol.RelayMagic,
		Version:       protocol.RelayVersion,
		PacketType:    protocol.RelayTypeRegister,
		PayloadLength: 0,
		TargetNodeID:  c.nodeIDBytes,
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err := c.conn.Write(header.Serialize())
	return err
}

// SendRelayedPayload wraps opaque WireGuard payload and sends to target Node ID via relay.
func (c *ClientTunnel) SendRelayedPayload(targetNodeIDHex string, wireguardCiphertext []byte) error {
	var targetBytes [16]byte
	b, err := hex.DecodeString(targetNodeIDHex)
	if err != nil || len(b) != 16 {
		return fmt.Errorf("invalid target node id hex: %s", targetNodeIDHex)
	}
	copy(targetBytes[:], b)

	header := protocol.RelayHeader{
		Magic:         protocol.RelayMagic,
		Version:       protocol.RelayVersion,
		PacketType:    protocol.RelayTypeData,
		PayloadLength: uint16(len(wireguardCiphertext)),
		TargetNodeID:  targetBytes,
	}

	frame := append(header.Serialize(), wireguardCiphertext...)
	_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err = c.conn.Write(frame)
	return err
}

// Close terminates the relay client tunnel connection.
func (c *ClientTunnel) Close() error {
	return c.conn.Close()
}
