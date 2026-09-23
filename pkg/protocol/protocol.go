package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// EndpointType identifies the candidate connection type.
type EndpointType string

const (
	EndpointLAN   EndpointType = "LAN"
	EndpointSTUN  EndpointType = "STUN"
	EndpointRelay EndpointType = "RELAY"
)

// ConnectionState represents the connection state between peers.
type ConnectionState string

const (
	StateDisconnected ConnectionState = "DISCONNECTED"
	StateConnecting   ConnectionState = "CONNECTING"
	StateDirect       ConnectionState = "DIRECT"
	StateRelayed      ConnectionState = "RELAYED"
)

// EndpointCandidate describes a network candidate endpoint for a peer node.
type EndpointCandidate struct {
	Type      EndpointType `json:"type"`
	IP        string       `json:"ip"`
	Port      int          `json:"port"`
	Priority  int          `json:"priority"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// PeerNode represents peer metadata distributed by the control plane.
type PeerNode struct {
	NodeID             string              `json:"node_id"`
	Hostname           string              `json:"hostname"`
	VirtualIP          string              `json:"virtual_ip"`
	WireGuardPublicKey string              `json:"wireguard_public_key"`
	Endpoints          []EndpointCandidate `json:"endpoints"`
	ConnectionState    ConnectionState     `json:"connection_state"`
	LastSeen           time.Time           `json:"last_seen"`
}

// DeviceRegisterRequest defines request payload to register a device identity.
type DeviceRegisterRequest struct {
	Ed25519PublicKey string `json:"ed25519_public_key"`
	Hostname         string `json:"hostname"`
	OSType           string `json:"os_type"`
}

// DeviceRegisterResponse contains registration status.
type DeviceRegisterResponse struct {
	NodeID     string `json:"node_id"`
	IsApproved bool   `json:"is_approved"`
	Message    string `json:"message"`
}

// CreateNetworkRequest defines request to create a virtual network.
type CreateNetworkRequest struct {
	NetworkName string `json:"network_name"`
	Subnet      string `json:"subnet,omitempty"` // Default: 10.100.0.0/16
}

// JoinNetworkRequest defines request payload to join a network using an invite token.
type JoinNetworkRequest struct {
	InviteToken        string `json:"invite_token"`
	NodeID             string `json:"node_id"`
	WireGuardPublicKey string `json:"wireguard_public_key"`
	Signature          string `json:"signature"` // Ed25519 signature of (NodeID + InviteToken)
}

// JoinNetworkResponse contains network membership & IP assignment.
type JoinNetworkResponse struct {
	NetworkName string     `json:"network_name"`
	VirtualIP   string     `json:"virtual_ip"`
	Subnet      string     `json:"subnet"`
	Peers       []PeerNode `json:"peers"`
	ACLRules    []ACLRule  `json:"acl_rules"`
}

// CreateInviteRequest defines request to generate an invite token.
type CreateInviteRequest struct {
	NetworkName string `json:"network_name"`
	NodeID      string `json:"node_id"`
	Signature   string `json:"signature"`
	ExpiresIn   int64  `json:"expires_in_seconds"` // e.g., 3600
	MaxUses     int    `json:"max_uses"`           // Default: 1
}

// CreateInviteResponse contains generated token.
type CreateInviteResponse struct {
	InviteToken string    `json:"invite_token"`
	NetworkName string    `json:"network_name"`
	ExpiresAt   time.Time `json:"expires_at"`
	MaxUses     int       `json:"max_uses"`
}

// ReportEndpointsRequest sends local and STUN public endpoints to control plane.
type ReportEndpointsRequest struct {
	NodeID      string              `json:"node_id"`
	NetworkName string              `json:"network_name"`
	Endpoints   []EndpointCandidate `json:"endpoints"`
	Signature   string              `json:"signature"`
}

// ACLRule defines an access control policy rule.
type ACLRule struct {
	ID                  string `json:"id"`
	NetworkName         string `json:"network_name"`
	SourceSelector      string `json:"source_selector"`      // e.g. '*', '10.100.0.11', 'tag:players'
	DestinationSelector string `json:"destination_selector"` // e.g. '10.100.0.10'
	Protocol            string `json:"protocol"`             // 'tcp', 'udp', 'any'
	Port                int    `json:"port"`                 // 0 for any
	Action              string `json:"action"`               // 'allow', 'deny'
	Order               int    `json:"order"`
}

// AddACLRuleRequest defines payload to add an ACL rule.
type AddACLRuleRequest struct {
	NetworkName         string `json:"network_name"`
	SourceSelector      string `json:"source_selector"`
	DestinationSelector string `json:"destination_selector"`
	Protocol            string `json:"protocol"`
	Port                int    `json:"port"`
	Action              string `json:"action"`
}

// StatusReport provides complete status details for CLI `meshvpn status`.
type StatusReport struct {
	NodeID            string     `json:"node_id"`
	Hostname          string     `json:"hostname"`
	ControlServer     string     `json:"control_server"`
	ControlStatus     string     `json:"control_status"`
	WireGuardInterface string    `json:"wireguard_interface"`
	InterfaceStatus   string     `json:"interface_status"`
	VirtualIP         string     `json:"virtual_ip"`
	ActiveNetwork     string     `json:"active_network"`
	LocalEndpoints    []string   `json:"local_endpoints"`
	PublicEndpoint    string     `json:"public_endpoint"`
	Peers             []PeerNode `json:"peers"`
}

// DiagnoseReport provides comprehensive troubleshooting output for CLI `meshvpn diagnose`.
type DiagnoseReport struct {
	Timestamp          time.Time            `json:"timestamp"`
	ControlServer      string               `json:"control_server"`
	ControlConnected   bool                 `json:"control_connected"`
	AuthStatus         string               `json:"auth_status"`
	WireGuardStatus    string               `json:"wireguard_status"`
	VirtualInterface   string               `json:"virtual_interface"`
	LocalEndpoint      string               `json:"local_endpoint"`
	PublicEndpoint     string               `json:"public_endpoint"`
	PeerDiagnostics    []PeerDiagnosticItem `json:"peer_diagnostics"`
	OverallHealth      string               `json:"overall_health"`
}

// PeerDiagnosticItem details connectivity for a specific peer node.
type PeerDiagnosticItem struct {
	PeerNodeID         string          `json:"peer_node_id"`
	Hostname           string          `json:"hostname"`
	VirtualIP          string          `json:"virtual_ip"`
	EndpointDiscovery  string          `json:"endpoint_discovery"`
	NATTraversalStatus string          `json:"nat_traversal_status"`
	ConnectionType     ConnectionState `json:"connection_type"`
	ActiveEndpoint     string          `json:"active_endpoint"`
	LatencyMs          float64         `json:"latency_ms"`
	PacketLossPercent  float64         `json:"packet_loss_percent"`
	FallbackReason     string          `json:"fallback_reason,omitempty"`
}

// Wire Format Constants & Encapsulation Header for Zero-Knowledge Relay
const (
	RelayMagic   uint32 = 0x4D56504E // ASCII "MVPN"
	RelayVersion uint8  = 0x01
	RelayHeaderLen      = 24 // 4 (Magic) + 1 (Ver) + 1 (Type) + 2 (Len) + 16 (Target Node ID)
)

const (
	RelayTypeData      uint8 = 0x01
	RelayTypePing      uint8 = 0x02
	RelayTypePong      uint8 = 0x03
	RelayTypeRegister  uint8 = 0x04
)

// RelayHeader is the 24-byte binary header prepended to opaque WireGuard packets routed via meshvpn-relay.
type RelayHeader struct {
	Magic         uint32
	Version       uint8
	PacketType    uint8
	PayloadLength uint16
	TargetNodeID  [16]byte
}

// Serialize encodes a RelayHeader into a 24-byte slice.
func (h *RelayHeader) Serialize() []byte {
	buf := make([]byte, RelayHeaderLen)
	binary.BigEndian.PutUint32(buf[0:4], h.Magic)
	buf[4] = h.Version
	buf[5] = h.PacketType
	binary.BigEndian.PutUint16(buf[6:8], h.PayloadLength)
	copy(buf[8:24], h.TargetNodeID[:])
	return buf
}

// DeserializeRelayHeader parses a 24-byte slice into a RelayHeader struct.
func DeserializeRelayHeader(buf []byte) (*RelayHeader, error) {
	if len(buf) < RelayHeaderLen {
		return nil, errors.New("buffer too short for relay header")
	}
	magic := binary.BigEndian.Uint32(buf[0:4])
	if magic != RelayMagic {
		return nil, fmt.Errorf("invalid relay magic header: 0x%X", magic)
	}
	header := &RelayHeader{
		Magic:         magic,
		Version:       buf[4],
		PacketType:    buf[5],
		PayloadLength: binary.BigEndian.Uint16(buf[6:8]),
	}
	copy(header.TargetNodeID[:], buf[8:24])
	return header, nil
}
