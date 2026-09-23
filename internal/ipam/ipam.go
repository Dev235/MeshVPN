package ipam

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
)

// IPAM manages virtual IPv4 assignments within defined network subnets.
type IPAM struct {
	mu           sync.RWMutex
	subnetCIDR   string
	baseIP       net.IP
	mask         net.IPMask
	allocated    map[string]string // nodeID -> IP string
	reverseAlloc map[string]string // IP string -> nodeID
}

// NewIPAM initializes an IP Address Manager for a CIDR subnet (default: 10.100.0.0/16).
func NewIPAM(subnetCIDR string) (*IPAM, error) {
	if subnetCIDR == "" {
		subnetCIDR = "10.100.0.0/16"
	}

	_, ipNet, err := net.ParseCIDR(subnetCIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet CIDR %s: %w", subnetCIDR, err)
	}

	return &IPAM{
		subnetCIDR:   subnetCIDR,
		baseIP:       ipNet.IP.To4(),
		mask:         ipNet.Mask,
		allocated:    make(map[string]string),
		reverseAlloc: make(map[string]string),
	}, nil
}

// Allocate assigns the next available virtual IP to a nodeID or retains existing assignment.
func (i *IPAM) Allocate(nodeID string) (string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Retain existing IP if already assigned to this node
	if existingIP, found := i.allocated[nodeID]; found {
		return existingIP, nil
	}

	baseUint := binary.BigEndian.Uint32(i.baseIP)
	maskUint := binary.BigEndian.Uint32(i.mask)
	broadcastUint := baseUint | ^maskUint

	// Reserve 10.100.0.1 for server control gateway / interface fallback
	// Nodes start allocation from baseUint + 2 (10.100.0.2)
	for current := baseUint + 2; current < broadcastUint; current++ {
		// Skip subnet host IDs ending in .0 or .255 for clean /24 blocks
		if current%256 == 0 || current%256 == 255 {
			continue
		}

		ipBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(ipBytes, current)
		candidateIP := net.IP(ipBytes).String()

		if _, taken := i.reverseAlloc[candidateIP]; !taken {
			i.allocated[nodeID] = candidateIP
			i.reverseAlloc[candidateIP] = nodeID
			return candidateIP, nil
		}
	}

	return "", errors.New("subnet IP address pool exhausted")
}

// AssignStatic assigns a specific static IP to a nodeID if available.
func (i *IPAM) AssignStatic(nodeID, ipStr string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	parsed := net.ParseIP(ipStr).To4()
	if parsed == nil {
		return fmt.Errorf("invalid IPv4 address: %s", ipStr)
	}

	if existingNode, taken := i.reverseAlloc[ipStr]; taken {
		if existingNode != nodeID {
			return fmt.Errorf("IP address %s already allocated to node %s", ipStr, existingNode)
		}
		return nil
	}

	i.allocated[nodeID] = ipStr
	i.reverseAlloc[ipStr] = nodeID
	return nil
}

// Release frees an assigned virtual IP address when a node leaves or is revoked.
func (i *IPAM) Release(nodeID string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if ip, found := i.allocated[nodeID]; found {
		delete(i.allocated, nodeID)
		delete(i.reverseAlloc, ip)
	}
}

// GetAssignment returns the virtual IP assigned to a nodeID.
func (i *IPAM) GetAssignment(nodeID string) (string, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	ip, found := i.allocated[nodeID]
	return ip, found
}
