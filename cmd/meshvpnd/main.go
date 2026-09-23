package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/meshvpn/meshvpn/internal/acl"
	"github.com/meshvpn/meshvpn/internal/diagnostics"
	"github.com/meshvpn/meshvpn/internal/identity"
	"github.com/meshvpn/meshvpn/internal/ipc"
	"github.com/meshvpn/meshvpn/internal/nat"
	"github.com/meshvpn/meshvpn/internal/wireguard"
	"github.com/meshvpn/meshvpn/pkg/protocol"
)

type DaemonState struct {
	ControlURL      string
	ActiveNetwork   string
	VirtualIP       string
	Subnet          string
	Identity        *identity.Identity
	WgManager       *wireguard.Manager
	NatEngine       *nat.Engine
	AclEngine       *acl.Engine
	Peers           []protocol.PeerNode
	ACLRules        []protocol.ACLRule
	LocalEndpoints  []protocol.EndpointCandidate
	PublicEndpoint  string
	RelayAddr       string
}

func main() {
	controlURL := flag.String("control", "http://127.0.0.1:8080", "Control plane URL")
	interfaceName := flag.String("interface", "mesh0", "WireGuard interface name")
	relayAddr := flag.String("relay", "127.0.0.1:41641", "Relay server address")
	configDir := flag.String("config-dir", defaultConfigDir(), "Directory to store identity and configs")
	flag.Parse()

	log.Printf("[meshvpnd] Headless MeshVPN Daemon starting on OS: %s", runtime.GOOS)

	// 1. Load or Generate Cryptographic Identity
	idPath := filepath.Join(*configDir, "identity.conf")
	id, err := identity.LoadIdentity(idPath)
	if err != nil {
		log.Printf("[meshvpnd] Generating new Ed25519 node identity...")
		id, err = identity.GenerateIdentity()
		if err != nil {
			log.Fatalf("Failed to generate identity: %v", err)
		}
		if err := identity.SaveIdentity(id, idPath); err != nil {
			log.Printf("Warning: failed to save identity to disk: %v", err)
		}
	}

	log.Printf("[meshvpnd] Node Identity loaded. Node ID: %s", id.NodeID)

	// 2. Initialize State
	state := &DaemonState{
		ControlURL: *controlURL,
		Identity:   id,
		WgManager:  wireguard.NewManager(*interfaceName),
		NatEngine:  nat.NewEngine([]string{"127.0.0.1:3478"}, 51820),
		AclEngine:  acl.NewEngine(nil),
		RelayAddr:  *relayAddr,
	}

	// 3. Start Local IPC Listener for CLI Immediately
	ipcListener, err := ipc.ListenIPC()
	if err != nil {
		log.Printf("[meshvpnd] Warning: Failed to start IPC socket: %v", err)
	} else {
		defer ipcListener.Close()
		go serveIPC(ipcListener, state)
		log.Printf("[meshvpnd] IPC listening on %s", ipc.GetSocketPath())
	}

	// 4. Register Node Identity with Control Server (with timeout)
	go func() {
		if err := registerNodeWithControl(state); err != nil {
			log.Printf("[meshvpnd] Notice: Initial control plane registration: %v (will auto-retry)", err)
		} else {
			log.Printf("[meshvpnd] Successfully registered node with control plane")
		}
	}()

	// 5. Start Background Network Sync Worker Loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runBackgroundWorker(ctx, state)

	// Graceful Shutdown on SIGINT/SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("[meshvpnd] Shutting down daemon service cleanly...")
}

func defaultConfigDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "MeshVPN")
	}
	return "/etc/meshvpn"
}

func registerNodeWithControl(state *DaemonState) error {
	hostname, _ := os.Hostname()
	req := protocol.DeviceRegisterRequest{
		Ed25519PublicKey: fmt.Sprintf("%x", state.Identity.Ed25519PublicKey),
		Hostname:         hostname,
		OSType:           runtime.GOOS,
	}

	body, _ := json.Marshal(req)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(state.ControlURL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status: %s", resp.Status)
	}

	return nil
}

func runBackgroundWorker(ctx context.Context, state *DaemonState) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if state.ActiveNetwork == "" {
				continue
			}
			performSync(ctx, state)
		}
	}
}

func performSync(ctx context.Context, state *DaemonState) {
	// 1. Discover local LAN & STUN candidate endpoints
	cands, err := state.NatEngine.DiscoverCandidates()
	if err == nil {
		state.LocalEndpoints = cands
		for _, c := range cands {
			if c.Type == protocol.EndpointSTUN {
				state.PublicEndpoint = fmt.Sprintf("%s:%d", c.IP, c.Port)
			}
		}

		// Report endpoints to control plane
		reportReq := protocol.ReportEndpointsRequest{
			NodeID:      state.Identity.NodeID,
			NetworkName: state.ActiveNetwork,
			Endpoints:   cands,
		}
		body, _ := json.Marshal(reportReq)
		resp, err := http.Post(state.ControlURL+"/api/v1/peers/endpoints", "application/json", bytes.NewBuffer(body))
		if err == nil {
			_ = resp.Body.Close()
		}
	}

	// 2. Fetch updated peer list from control plane
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/networks/peers?network=%s&node_id=%s", state.ControlURL, state.ActiveNetwork, state.Identity.NodeID))
	if err == nil && resp.StatusCode == http.StatusOK {
		var peers []protocol.PeerNode
		if err := json.NewDecoder(resp.Body).Decode(&peers); err == nil {
			state.Peers = peers

			// 3. Perform UDP hole punching probing for each peer
			for i, p := range state.Peers {
				probeCtx, probeCancel := context.WithTimeout(ctx, 4*time.Second)
				directEp, err := state.NatEngine.ProbeDirectPath(probeCtx, p.NodeID, p.Endpoints)
				probeCancel()

				if err == nil && directEp != "" {
					state.Peers[i].ConnectionState = protocol.StateDirect
				} else {
					state.Peers[i].ConnectionState = protocol.StateRelayed
				}
			}

			// 4. Sync WireGuard Interface configuration
			wgPeers := wireguard.BuildPeersFromProtocol(state.Peers)
			wgCfg := wireguard.Config{
				InterfaceName: state.WgManager.InterfaceName,
				PrivateKey:    state.Identity.WireGuardPrivateKey,
				VirtualIP:     fmt.Sprintf("%s/16", state.VirtualIP),
				ListenPort:    51820,
				MTU:           1420,
				Peers:         wgPeers,
			}
			_ = state.WgManager.SyncConfig(wgCfg)
		}
		_ = resp.Body.Close()
	}
}

func serveIPC(l net.Listener, state *DaemonState) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go handleIPCConn(conn, state)
	}
}

func handleIPCConn(conn net.Conn, state *DaemonState) {
	defer conn.Close()

	var req ipc.Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}

	resp := ipc.Response{Success: true}

	switch req.Command {
	case "status":
		localEpStrs := []string{}
		for _, ep := range state.LocalEndpoints {
			localEpStrs = append(localEpStrs, fmt.Sprintf("%s:%d (%s)", ep.IP, ep.Port, ep.Type))
		}

		resp.Report = &protocol.StatusReport{
			NodeID:             state.Identity.NodeID,
			ControlServer:      state.ControlURL,
			ControlStatus:      "CONNECTED",
			WireGuardInterface: state.WgManager.InterfaceName,
			InterfaceStatus:    "UP",
			VirtualIP:          state.VirtualIP,
			ActiveNetwork:      state.ActiveNetwork,
			LocalEndpoints:     localEpStrs,
			PublicEndpoint:     state.PublicEndpoint,
			Peers:              state.Peers,
		}

	case "diagnose":
		runner := diagnostics.NewRunner(state.ControlURL, state.Identity.NodeID)
		diagReport := runner.RunDiagnostics(state.VirtualIP, state.ActiveNetwork, state.Peers)
		resp.Data = map[string]interface{}{"report": diagReport}

	case "join":
		joinReq := protocol.JoinNetworkRequest{
			NetworkName:        req.NetworkName,
			Password:           req.Password,
			InviteToken:        req.InviteToken,
			NodeID:             state.Identity.NodeID,
			WireGuardPublicKey: state.Identity.WireGuardPublicKey,
		}

		body, _ := json.Marshal(joinReq)
		httpResp, err := http.Post(state.ControlURL+"/api/v1/networks/join", "application/json", bytes.NewBuffer(body))
		if err != nil {
			resp.Success = false
			resp.Message = fmt.Sprintf("Failed to contact control plane: %v", err)
		} else {
			defer httpResp.Body.Close()
			if httpResp.StatusCode != http.StatusOK {
				resp.Success = false
				respBytes, _ := io.ReadAll(httpResp.Body)
				errMsg := strings.TrimSpace(string(respBytes))
				if errMsg == "" {
					errMsg = httpResp.Status
				}
				resp.Message = fmt.Sprintf("Control plane rejected join request: %s", errMsg)
			} else {
				var joinResp protocol.JoinNetworkResponse
				if err := json.NewDecoder(httpResp.Body).Decode(&joinResp); err == nil {
					state.ActiveNetwork = joinResp.NetworkName
					state.VirtualIP = joinResp.VirtualIP
					state.Subnet = joinResp.Subnet
					state.Peers = joinResp.Peers
					state.ACLRules = joinResp.ACLRules
					state.AclEngine.SetRules(joinResp.ACLRules)

					resp.Message = fmt.Sprintf("Successfully joined network '%s'. Assigned Virtual IP: %s", joinResp.NetworkName, joinResp.VirtualIP)

					// Trigger immediate sync
					go performSync(context.Background(), state)
				}
			}
		}

	case "network_create":
		subnet := req.Subnet
		if subnet == "" {
			subnet = "10.100.0.0/16"
		}
		createReq := protocol.CreateNetworkRequest{
			NetworkName: req.NetworkName,
			Subnet:      subnet,
			Password:    req.Password,
		}
		body, _ := json.Marshal(createReq)
		httpResp, err := http.Post(state.ControlURL+"/api/v1/networks", "application/json", bytes.NewBuffer(body))
		if err != nil {
			resp.Success = false
			resp.Message = fmt.Sprintf("Failed to contact control plane: %v", err)
		} else {
			defer httpResp.Body.Close()
			if httpResp.StatusCode != http.StatusOK {
				resp.Success = false
				respBytes, _ := io.ReadAll(httpResp.Body)
				errMsg := strings.TrimSpace(string(respBytes))
				if errMsg == "" {
					errMsg = httpResp.Status
				}
				resp.Message = fmt.Sprintf("Failed to create network: %s", errMsg)
			} else {
				// Automatically join the newly created network immediately
				joinReq := protocol.JoinNetworkRequest{
					NetworkName:        req.NetworkName,
					Password:           req.Password,
					NodeID:             state.Identity.NodeID,
					WireGuardPublicKey: state.Identity.WireGuardPublicKey,
				}
				joinBody, _ := json.Marshal(joinReq)
				joinHttpResp, joinErr := http.Post(state.ControlURL+"/api/v1/networks/join", "application/json", bytes.NewBuffer(joinBody))
				if joinErr == nil && joinHttpResp.StatusCode == http.StatusOK {
					defer joinHttpResp.Body.Close()
					var joinResp protocol.JoinNetworkResponse
					if err := json.NewDecoder(joinHttpResp.Body).Decode(&joinResp); err == nil {
						state.ActiveNetwork = joinResp.NetworkName
						state.VirtualIP = joinResp.VirtualIP
						state.Subnet = joinResp.Subnet
						state.Peers = joinResp.Peers
						state.ACLRules = joinResp.ACLRules
						state.AclEngine.SetRules(joinResp.ACLRules)

						resp.Message = fmt.Sprintf("Network '%s' created and joined. Assigned Virtual IP: %s", req.NetworkName, joinResp.VirtualIP)
						resp.Data = map[string]interface{}{
							"virtual_ip": joinResp.VirtualIP,
							"network":    joinResp.NetworkName,
						}
						go performSync(context.Background(), state)
					}
				} else {
					errMsg := "join error"
					if joinErr != nil {
						errMsg = joinErr.Error()
					} else if joinHttpResp != nil {
						b, _ := io.ReadAll(joinHttpResp.Body)
						_ = joinHttpResp.Body.Close()
						errMsg = fmt.Sprintf("status %d: %s", joinHttpResp.StatusCode, strings.TrimSpace(string(b)))
					}
					resp.Message = fmt.Sprintf("Network '%s' created, but auto-join failed: %s", req.NetworkName, errMsg)
				}
			}
		}

	case "leave":
		state.ActiveNetwork = ""
		state.VirtualIP = ""
		state.Peers = nil
		resp.Message = "Left network successfully"

	case "shutdown":
		resp.Message = "MeshVPN daemon shutting down..."
		_ = json.NewEncoder(conn).Encode(resp)
		go func() {
			time.Sleep(200 * time.Millisecond)
			os.Exit(0)
		}()
		return

	default:
		resp.Success = false
		resp.Message = fmt.Sprintf("Unknown command: %s", req.Command)
	}

	_ = json.NewEncoder(conn).Encode(resp)
}
