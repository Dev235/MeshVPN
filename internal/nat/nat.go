package nat

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/meshvpn/meshvpn/pkg/protocol"
)

// Engine performs NAT traversal, local endpoint discovery, STUN queries, and hole punching.
type Engine struct {
	stunServers []string
	localPort   int
	mu          sync.RWMutex
}

// NewEngine initializes a NAT traversal engine.
func NewEngine(stunServers []string, localPort int) *Engine {
	if len(stunServers) == 0 {
		stunServers = []string{"127.0.0.1:3478"} // Default embedded STUN reflector
	}
	if localPort == 0 {
		localPort = 51820
	}
	return &Engine{
		stunServers: stunServers,
		localPort:   localPort,
	}
}

// DiscoverCandidates enumerates local LAN IPs and queries STUN reflectors for public WAN IPs.
func (e *Engine) DiscoverCandidates() ([]protocol.EndpointCandidate, error) {
	var candidates []protocol.EndpointCandidate

	// 1. Gather Local LAN IPs
	localIPs, err := getLocalLANIPs()
	if err == nil {
		for i, ip := range localIPs {
			candidates = append(candidates, protocol.EndpointCandidate{
				Type:      protocol.EndpointLAN,
				IP:        ip,
				Port:      e.localPort,
				Priority:  100 - i,
				UpdatedAt: time.Now(),
			})
		}
	}

	// 2. Query STUN Reflectors for Public WAN Mapped Address
	for _, stunServer := range e.stunServers {
		pubIP, pubPort, err := querySTUNServer(stunServer)
		if err == nil && pubIP != "" && pubPort > 0 {
			candidates = append(candidates, protocol.EndpointCandidate{
				Type:      protocol.EndpointSTUN,
				IP:        pubIP,
				Port:      pubPort,
				Priority:  80,
				UpdatedAt: time.Now(),
			})
			break
		}
	}

	return candidates, nil
}

// ProbeDirectPath attempts parallel UDP hole punching to candidates within timeout.
// Returns winning direct endpoint string ("IP:Port") if direct P2P succeeds.
func (e *Engine) ProbeDirectPath(ctx context.Context, targetNodeID string, candidates []protocol.EndpointCandidate) (string, error) {
	if len(candidates) == 0 {
		return "", fmt.Errorf("no candidate endpoints available for node %s", targetNodeID)
	}

	type result struct {
		endpoint string
		err      error
	}

	resChan := make(chan result, len(candidates))
	var wg sync.WaitGroup

	for _, cand := range candidates {
		if cand.IP == "" || cand.Port <= 0 {
			continue
		}

		wg.Add(1)
		go func(c protocol.EndpointCandidate) {
			defer wg.Done()

			targetAddrStr := fmt.Sprintf("%s:%d", c.IP, c.Port)
			err := e.sendHolePunchProbe(ctx, targetAddrStr)
			if err == nil {
				resChan <- result{endpoint: targetAddrStr, err: nil}
			}
		}(cand)
	}

	// Wait for first winning candidate or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case res := <-resChan:
		return res.endpoint, nil
	case <-done:
		select {
		case res := <-resChan:
			return res.endpoint, nil
		default:
			return "", fmt.Errorf("direct UDP hole punching probes timed out for all candidates")
		}
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (e *Engine) sendHolePunchProbe(ctx context.Context, targetAddr string) error {
	raddr, err := net.ResolveUDPAddr("udp", targetAddr)
	if err != nil {
		return err
	}

	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Lightweight UDP Probe Packet
	probeMsg := []byte("MVPN_HOLE_PUNCH_PROBE_V1")

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(probeMsg); err != nil {
		return err
	}

	buf := make([]byte, 256)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return err
	}

	if n > 0 {
		return nil // Active bi-directional UDP path confirmed
	}

	return fmt.Errorf("no probe reply received")
}

func getLocalLANIPs() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var ips []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}

			// Filter out APIPA / Link Local 169.254.x.x
			if ip[0] == 169 && ip[1] == 254 {
				continue
			}

			ips = append(ips, ip.String())
		}
	}
	return ips, nil
}

func querySTUNServer(serverAddr string) (string, int, error) {
	raddr, err := net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		return "", 0, err
	}

	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return "", 0, err
	}
	defer conn.Close()

	// STUN Binding Request (20 bytes)
	req := make([]byte, 20)
	req[0] = 0x00 // Binding Request Message Type
	req[1] = 0x01
	// STUN Magic Cookie 0x2112A442
	binary.BigEndian.PutUint32(req[4:8], 0x2112A442)
	// Transaction ID (12 random bytes)
	for i := 8; i < 20; i++ {
		req[i] = byte(i)
	}

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(req); err != nil {
		return "", 0, err
	}

	buf := make([]byte, 512)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil || n < 20 {
		return "", 0, fmt.Errorf("failed STUN response read: %v", err)
	}

	// Parse XOR-MAPPED-ADDRESS
	return parseSTUNResponse(buf[:n])
}

func parseSTUNResponse(resp []byte) (string, int, error) {
	if len(resp) < 20 {
		return "", 0, fmt.Errorf("response buffer too short")
	}

	offset := 20
	length := len(resp)

	for offset+4 <= length {
		attrType := binary.BigEndian.Uint16(resp[offset : offset+2])
		attrLen := int(binary.BigEndian.Uint16(resp[offset+2 : offset+4]))
		offset += 4

		if offset+attrLen > length {
			break
		}

		if attrType == 0x0020 { // XOR-MAPPED-ADDRESS
			if attrLen >= 8 {
				family := resp[offset+1]
				if family == 0x01 { // IPv4
					xorPort := binary.BigEndian.Uint16(resp[offset+2 : offset+4])
					port := int(xorPort ^ 0x2112)

					magicCookie := []byte{0x21, 0x12, 0xA4, 0x42}
					ipBytes := make([]byte, 4)
					for i := 0; i < 4; i++ {
						ipBytes[i] = resp[offset+4+i] ^ magicCookie[i]
					}
					ip := net.IP(ipBytes).String()
					return ip, port, nil
				}
			}
		}

		offset += attrLen
		// STUN attributes are padded to 32-bit boundary
		if pad := attrLen % 4; pad != 0 {
			offset += 4 - pad
		}
	}

	return "", 0, fmt.Errorf("XOR-MAPPED-ADDRESS attribute not found in STUN response")
}
