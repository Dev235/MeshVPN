package control

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/meshvpn/meshvpn/pkg/protocol"
)

// StorePersist defines serializable database layout.
type StorePersist struct {
	Nodes       map[string]*NodeRecord       `json:"nodes"`
	Networks    map[string]*NetworkRecord    `json:"networks"`
	Memberships map[string]*MembershipRecord `json:"memberships"` // key: network_name + ":" + node_id
	Invites     map[string]*InviteRecord     `json:"invites"`     // key: token
	ACLRules    map[string][]protocol.ACLRule `json:"acl_rules"`   // key: network_name
}

type NodeRecord struct {
	NodeID           string    `json:"node_id"`
	Ed25519PublicKey string    `json:"ed25519_public_key"`
	Hostname         string    `json:"hostname"`
	OSType           string    `json:"os_type"`
	IsApproved       bool      `json:"is_approved"`
	CreatedAt        time.Time `json:"created_at"`
	LastSeen         time.Time `json:"last_seen"`
}

type NetworkRecord struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Subnet    string    `json:"subnet"`
	CreatedAt time.Time `json:"created_at"`
}

type MembershipRecord struct {
	NodeID             string                       `json:"node_id"`
	NetworkName        string                       `json:"network_name"`
	VirtualIP          string                       `json:"virtual_ip"`
	WireGuardPublicKey string                       `json:"wireguard_public_key"`
	Endpoints          []protocol.EndpointCandidate `json:"endpoints"`
	JoinedAt           time.Time                    `json:"joined_at"`
	LastSeen           time.Time                    `json:"last_seen"`
}

type InviteRecord struct {
	Code        string    `json:"code"`
	NetworkName string    `json:"network_name"`
	CreatedBy   string    `json:"created_by"`
	ExpiresAt   time.Time `json:"expires_at"`
	MaxUses     int       `json:"max_uses"`
	UsesCount   int       `json:"uses_count"`
}

// Store handles persistent memory/JSON storage for the control server.
type Store struct {
	mu       sync.RWMutex
	filePath string
	data     StorePersist
}

// NewStore initializes a persistent control store.
func NewStore(dbPath string) (*Store, error) {
	if dbPath == "" {
		dbPath = "meshvpn-control.json"
	}

	store := &Store{
		filePath: dbPath,
		data: StorePersist{
			Nodes:       make(map[string]*NodeRecord),
			Networks:    make(map[string]*NetworkRecord),
			Memberships: make(map[string]*MembershipRecord),
			Invites:     make(map[string]*InviteRecord),
			ACLRules:    make(map[string][]protocol.ACLRule),
		},
	}

	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load database file: %w", err)
	}

	return store, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &s.data)
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0755); err != nil && filepath.Dir(s.filePath) != "." {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, b, 0644)
}

func (s *Store) RegisterNode(node *NodeRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Nodes[node.NodeID] = node
	return s.saveLocked()
}

func (s *Store) GetNode(nodeID string) (*NodeRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n, found := s.data.Nodes[nodeID]
	return n, found
}

func (s *Store) RevokeNode(nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data.Nodes, nodeID)
	// Remove memberships
	for key, mem := range s.data.Memberships {
		if mem.NodeID == nodeID {
			delete(s.data.Memberships, key)
		}
	}
	return s.saveLocked()
}

func (s *Store) CreateNetwork(netRec *NetworkRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Networks[netRec.Name] = netRec
	return s.saveLocked()
}

func (s *Store) GetNetwork(name string) (*NetworkRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n, found := s.data.Networks[name]
	return n, found
}

func (s *Store) ListNetworks() []*NetworkRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []*NetworkRecord
	for _, netRec := range s.data.Networks {
		list = append(list, netRec)
	}
	return list
}

func (s *Store) SaveInvite(inv *InviteRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Invites[inv.Code] = inv
	return s.saveLocked()
}

func (s *Store) ConsumeInvite(token string) (*InviteRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	inv, found := s.data.Invites[token]
	if !found {
		return nil, fmt.Errorf("invite token not found")
	}

	if time.Now().After(inv.ExpiresAt) {
		return nil, fmt.Errorf("invite token expired")
	}

	if inv.UsesCount >= inv.MaxUses {
		return nil, fmt.Errorf("invite token usage limit reached")
	}

	inv.UsesCount++
	s.data.Invites[token] = inv
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return inv, nil
}

func (s *Store) SaveMembership(mem *MembershipRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := mem.NetworkName + ":" + mem.NodeID
	s.data.Memberships[key] = mem
	return s.saveLocked()
}

func (s *Store) GetNetworkMemberships(networkName string) []*MembershipRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []*MembershipRecord
	for _, mem := range s.data.Memberships {
		if mem.NetworkName == networkName {
			list = append(list, mem)
		}
	}
	return list
}

func (s *Store) UpdateEndpoints(networkName, nodeID string, endpoints []protocol.EndpointCandidate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := networkName + ":" + nodeID
	if mem, found := s.data.Memberships[key]; found {
		mem.Endpoints = endpoints
		mem.LastSeen = time.Now()
		return s.saveLocked()
	}
	return fmt.Errorf("membership not found for node %s in network %s", nodeID, networkName)
}

func (s *Store) SetACLRules(networkName string, rules []protocol.ACLRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.ACLRules[networkName] = rules
	return s.saveLocked()
}

func (s *Store) GetACLRules(networkName string) []protocol.ACLRule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.data.ACLRules[networkName]
}

func GenerateInviteToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
