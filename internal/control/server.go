package control

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/meshvpn/meshvpn/internal/identity"
	"github.com/meshvpn/meshvpn/internal/ipam"
	"github.com/meshvpn/meshvpn/pkg/protocol"
)

// Server represents the MeshVPN control plane HTTP server.
type Server struct {
	addr       string
	store      *Store
	ipams      map[string]*ipam.IPAM
	ipamMu     sync.RWMutex
	httpServer *http.Server
	stunAddr   string
}

// NewServer initializes a Control Server instance.
func NewServer(addr, stunAddr, dbPath string) (*Server, error) {
	store, err := NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to init store: %w", err)
	}

	if stunAddr == "" {
		stunAddr = ":3478"
	}

	srv := &Server{
		addr:     addr,
		store:    store,
		ipams:    make(map[string]*ipam.IPAM),
		stunAddr: stunAddr,
	}

	return srv, nil
}

func (s *Server) getIPAM(networkName, subnet string) (*ipam.IPAM, error) {
	s.ipamMu.Lock()
	defer s.ipamMu.Unlock()

	if mgr, exists := s.ipams[networkName]; exists {
		return mgr, nil
	}

	mgr, err := ipam.NewIPAM(subnet)
	if err != nil {
		return nil, err
	}
	s.ipams[networkName] = mgr
	return mgr, nil
}

// Start launches the Control Server REST listener and embedded STUN reflector.
func (s *Server) Start() error {
	// Initialize default 'minecraft' network if none exists
	if _, found := s.store.GetNetwork("minecraft"); !found {
		_ = s.store.CreateNetwork(&NetworkRecord{
			ID:        "net-minecraft",
			Name:      "minecraft",
			Subnet:    "10.100.0.0/16",
			CreatedAt: time.Now(),
		})
	}

	// Initialize STUN server in background
	go s.startEmbeddedSTUN()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/register", s.handleRegisterDevice)
	mux.HandleFunc("/api/v1/networks", s.handleNetworks)
	mux.HandleFunc("/api/v1/invites", s.handleInvites)
	mux.HandleFunc("/api/v1/networks/join", s.handleJoinNetwork)
	mux.HandleFunc("/api/v1/peers/endpoints", s.handleReportEndpoints)
	mux.HandleFunc("/api/v1/networks/peers", s.handleGetPeers)
	mux.HandleFunc("/api/v1/networks/acls", s.handleACLs)
	mux.HandleFunc("/api/v1/health", s.handleHealth)

	s.httpServer = &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[Control Server] Listening on HTTPS/REST %s and STUN UDP %s", s.addr, s.stunAddr)
	return s.httpServer.ListenAndServe()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"OK","server":"meshvpn-control"}`))
}

func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req protocol.DeviceRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pubBytes, err := hex.DecodeString(req.Ed25519PublicKey)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		http.Error(w, "Invalid ed25519_public_key format", http.StatusBadRequest)
		return
	}

	nodeID := identity.DeriveNodeID(ed25519.PublicKey(pubBytes))

	nodeRec := &NodeRecord{
		NodeID:           nodeID,
		Ed25519PublicKey: req.Ed25519PublicKey,
		Hostname:         req.Hostname,
		OSType:           req.OSType,
		IsApproved:       true, // Default auto-approve for self-hosted setup
		CreatedAt:        time.Now(),
		LastSeen:         time.Now(),
	}

	if err := s.store.RegisterNode(nodeRec); err != nil {
		http.Error(w, fmt.Sprintf("Failed to register node: %v", err), http.StatusInternalServerError)
		return
	}

	resp := protocol.DeviceRegisterResponse{
		NodeID:     nodeID,
		IsApproved: true,
		Message:    "Device identity registered successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleNetworks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		nets := s.store.ListNetworks()
		type networkSummary struct {
			ID          string    `json:"id"`
			Name        string    `json:"name"`
			Subnet      string    `json:"subnet"`
			HasPassword bool      `json:"has_password"`
			MemberCount int       `json:"member_count"`
			CreatedAt   time.Time `json:"created_at"`
		}
		summaries := make([]networkSummary, len(nets))
		for i, n := range nets {
			mems := s.store.GetNetworkMemberships(n.Name)
			summaries[i] = networkSummary{
				ID:          n.ID,
				Name:        n.Name,
				Subnet:      n.Subnet,
				HasPassword: n.PasswordHash != "",
				MemberCount: len(mems),
				CreatedAt:   n.CreatedAt,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(summaries)

	case http.MethodPost:
		var req protocol.CreateNetworkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.NetworkName == "" {
			http.Error(w, "network_name is required", http.StatusBadRequest)
			return
		}

		if req.Subnet == "" {
			req.Subnet = "10.100.0.0/16"
		}

		var pwHash string
		if req.Password != "" {
			var err error
			pwHash, err = HashPassword(req.Password)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to hash password: %v", err), http.StatusInternalServerError)
				return
			}
		}

		netRec := &NetworkRecord{
			ID:           "net-" + req.NetworkName,
			Name:         req.NetworkName,
			Subnet:       req.Subnet,
			PasswordHash: pwHash,
			CreatedAt:    time.Now(),
		}

		if err := s.store.CreateNetwork(netRec); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		respRec := *netRec
		respRec.PasswordHash = ""
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(respRec)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleInvites(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req protocol.CreateInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ExpiresIn <= 0 {
		req.ExpiresIn = 86400 // Default 24 hours
	}

	if req.MaxUses <= 0 {
		req.MaxUses = 1
	}

	token := GenerateInviteToken()
	expiresAt := time.Now().Add(time.Duration(req.ExpiresIn) * time.Second)

	invRec := &InviteRecord{
		Code:        token,
		NetworkName: req.NetworkName,
		CreatedBy:   req.NodeID,
		ExpiresAt:   expiresAt,
		MaxUses:     req.MaxUses,
		UsesCount:   0,
	}

	if err := s.store.SaveInvite(invRec); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := protocol.CreateInviteResponse{
		InviteToken: token,
		NetworkName: req.NetworkName,
		ExpiresAt:   expiresAt,
		MaxUses:     req.MaxUses,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleJoinNetwork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req protocol.JoinNetworkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var netRec *NetworkRecord
	if req.NetworkName != "" {
		// Authenticate via network name and password
		targetNet, found := s.store.GetNetwork(req.NetworkName)
		if !found {
			http.Error(w, fmt.Sprintf("Network '%s' does not exist", req.NetworkName), http.StatusNotFound)
			return
		}
		if !targetNet.CheckPassword(req.Password) {
			http.Error(w, "Invalid network password", http.StatusUnauthorized)
			return
		}
		netRec = targetNet
	} else if req.InviteToken != "" {
		// Authenticate via invite token
		inv, err := s.store.ConsumeInvite(req.InviteToken)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid or expired invite token: %v", err), http.StatusForbidden)
			return
		}
		targetNet, found := s.store.GetNetwork(inv.NetworkName)
		if !found {
			http.Error(w, "Target network does not exist", http.StatusNotFound)
			return
		}
		netRec = targetNet
	} else {
		http.Error(w, "Either network_name with password, or invite_token must be provided", http.StatusBadRequest)
		return
	}

	ipamMgr, err := s.getIPAM(netRec.Name, netRec.Subnet)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	virtualIP, err := ipamMgr.Allocate(req.NodeID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to allocate virtual IP: %v", err), http.StatusInternalServerError)
		return
	}

	memRec := &MembershipRecord{
		NodeID:             req.NodeID,
		NetworkName:        netRec.Name,
		VirtualIP:          virtualIP,
		WireGuardPublicKey: req.WireGuardPublicKey,
		JoinedAt:           time.Now(),
		LastSeen:           time.Now(),
	}

	if err := s.store.SaveMembership(memRec); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch network peers
	rawMems := s.store.GetNetworkMemberships(netRec.Name)
	var peers []protocol.PeerNode
	for _, m := range rawMems {
		if m.NodeID == req.NodeID {
			continue // Exclude self
		}
		nodeRec, _ := s.store.GetNode(m.NodeID)
		hostname := m.NodeID
		if nodeRec != nil {
			hostname = nodeRec.Hostname
		}
		peers = append(peers, protocol.PeerNode{
			NodeID:             m.NodeID,
			Hostname:           hostname,
			VirtualIP:          m.VirtualIP,
			WireGuardPublicKey: m.WireGuardPublicKey,
			Endpoints:          m.Endpoints,
			ConnectionState:    protocol.StateConnecting,
			LastSeen:           m.LastSeen,
		})
	}

	acls := s.store.GetACLRules(netRec.Name)

	resp := protocol.JoinNetworkResponse{
		NetworkName: netRec.Name,
		VirtualIP:   virtualIP,
		Subnet:      netRec.Subnet,
		Peers:       peers,
		ACLRules:    acls,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleReportEndpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req protocol.ReportEndpointsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.store.UpdateEndpoints(req.NetworkName, req.NodeID, req.Endpoints); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"OK"}`))
}

func (s *Server) handleGetPeers(w http.ResponseWriter, r *http.Request) {
	netName := r.URL.Query().Get("network")
	if netName == "" {
		http.Error(w, "network parameter required", http.StatusBadRequest)
		return
	}

	nodeID := r.URL.Query().Get("node_id")

	mems := s.store.GetNetworkMemberships(netName)
	var peers []protocol.PeerNode
	for _, m := range mems {
		if nodeID != "" && m.NodeID == nodeID {
			continue
		}
		nodeRec, _ := s.store.GetNode(m.NodeID)
		hostname := m.NodeID
		if nodeRec != nil {
			hostname = nodeRec.Hostname
		}
		peers = append(peers, protocol.PeerNode{
			NodeID:             m.NodeID,
			Hostname:           hostname,
			VirtualIP:          m.VirtualIP,
			WireGuardPublicKey: m.WireGuardPublicKey,
			Endpoints:          m.Endpoints,
			ConnectionState:    protocol.StateConnecting,
			LastSeen:           m.LastSeen,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(peers)
}

func (s *Server) handleACLs(w http.ResponseWriter, r *http.Request) {
	netName := r.URL.Query().Get("network")
	if netName == "" {
		http.Error(w, "network parameter required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		acls := s.store.GetACLRules(netName)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(acls)

	case http.MethodPost:
		var req protocol.AddACLRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		existing := s.store.GetACLRules(netName)
		newRule := protocol.ACLRule{
			ID:                  fmt.Sprintf("%s-rule-%d", netName, len(existing)+1),
			NetworkName:         netName,
			SourceSelector:      req.SourceSelector,
			DestinationSelector: req.DestinationSelector,
			Protocol:            req.Protocol,
			Port:                req.Port,
			Action:              req.Action,
			Order:               len(existing),
		}

		updated := append(existing, newRule)
		if err := s.store.SetACLRules(netName, updated); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(newRule)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// startEmbeddedSTUN starts an embedded STUN UDP reflector server for endpoint discovery.
func (s *Server) startEmbeddedSTUN() {
	addr, err := net.ResolveUDPAddr("udp", s.stunAddr)
	if err != nil {
		log.Printf("[STUN] Failed to resolve UDP address %s: %v", s.stunAddr, err)
		return
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("[STUN] Failed to listen on UDP %s: %v", s.stunAddr, err)
		return
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		// Check if simple STUN Binding Request (STUN magic cookie 0x2112A442)
		if n >= 20 {
			// Respond with STUN Binding Response containing remoteAddr (Mapped Address)
			resp := buildSTUNBindingResponse(buf[:20], remoteAddr)
			_, _ = conn.WriteToUDP(resp, remoteAddr)
		}
	}
}

func buildSTUNBindingResponse(reqHeader []byte, addr *net.UDPAddr) []byte {
	resp := make([]byte, 32)
	// STUN Binding Response Message Type: 0x0101
	resp[0] = 0x01
	resp[1] = 0x01
	// Message Length: 12 bytes payload
	resp[2] = 0x00
	resp[3] = 0x0C
	// Copy Transaction ID from request (16 bytes starting at offset 4)
	copy(resp[4:20], reqHeader[4:20])

	// Attribute: XOR-MAPPED-ADDRESS (0x0020)
	resp[20] = 0x00
	resp[21] = 0x20
	// Attribute Length: 8 bytes
	resp[22] = 0x00
	resp[23] = 0x08
	// Reserved: 0x00, Family: IPv4 (0x01)
	resp[24] = 0x00
	resp[25] = 0x01

	// XOR Port with STUN magic cookie top 16 bits (0x2112)
	xorPort := uint16(addr.Port) ^ 0x2112
	resp[26] = byte(xorPort >> 8)
	resp[27] = byte(xorPort)

	// XOR IP with STUN magic cookie (0x2112A442)
	ip4 := addr.IP.To4()
	if ip4 == nil {
		ip4 = net.ParseIP("127.0.0.1").To4()
	}
	magicCookie := []byte{0x21, 0x12, 0xA4, 0x42}
	for i := 0; i < 4; i++ {
		resp[28+i] = ip4[i] ^ magicCookie[i]
	}

	return resp
}
