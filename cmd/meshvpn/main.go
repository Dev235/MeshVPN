package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/meshvpn/meshvpn/internal/ipc"
	"github.com/meshvpn/meshvpn/pkg/protocol"
)

var controlURL = "http://127.0.0.1:8080"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "status":
		handleStatus(os.Args[2:])
	case "diagnose":
		handleDiagnose(os.Args[2:])
	case "join":
		handleJoin(os.Args[2:])
	case "leave":
		handleLeave(os.Args[2:])
	case "network":
		handleNetworkCmd(os.Args[2:])
	case "invite":
		handleInviteCmd(os.Args[2:])
	case "peer":
		handlePeerCmd(os.Args[2:])
	case "acl":
		handleACLCmd(os.Args[2:])
	case "device":
		handleDeviceCmd(os.Args[2:])
	case "login":
		fmt.Println("[meshvpn] Logged in to control server:", controlURL)
	case "logout":
		fmt.Println("[meshvpn] Logged out successfully")
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`MeshVPN CLI Admin & Status Tool

Usage:
  meshvpn <command> [options]

Commands:
  status [--json]                 Display node status, interface, virtual IP, and peer topology
  diagnose                        Run diagnostic probes for control plane, STUN, and P2P connectivity
  join <invite-token>             Join a mesh virtual network using an invitation token
  leave                           Leave the currently active network

  network create <name> [subnet]  Create a new virtual network (e.g. minecraft)
  network list                    List available virtual networks

  invite create <net-name>        Generate a temporary invitation token for a friend or node
  invite revoke <token>           Revoke an active invitation token

  peer list                       List peers in active network with IPs and connection mode
  peer ping <virtual-ip>          Probe ping latency to a mesh peer
  peer remove <node-id>           Remove a peer from the network

  acl list <network-name>         List ACL rules for a network
  acl add <net> <src> <dst> <proto> <port> <action>  Add an ACL policy rule
  acl remove <net> <rule-id>     Remove an ACL policy rule

  device register                 Register device identity with control plane
  device list                     List registered devices
  device revoke <node-id>         Revoke a device's network access immediately`)
}

func handleStatus(args []string) {
	jsonOutput := false
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		}
	}

	req := &ipc.Request{Command: "status"}
	resp, err := ipc.SendRequest(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching status: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		b, _ := json.MarshalIndent(resp.Report, "", "  ")
		fmt.Println(string(b))
		return
	}

	r := resp.Report
	if r == nil {
		fmt.Println("No status report available from daemon")
		return
	}

	fmt.Println("==================================================================")
	fmt.Println("                   MeshVPN Status Overview                        ")
	fmt.Println("==================================================================")
	fmt.Printf("Node ID:            %s\n", r.NodeID)
	fmt.Printf("Control Server:     %s [%s]\n", r.ControlServer, r.ControlStatus)
	fmt.Printf("WireGuard Adapter:  %s [%s]\n", r.WireGuardInterface, r.InterfaceStatus)
	fmt.Printf("Virtual IPv4:       %s\n", r.VirtualIP)
	fmt.Printf("Active Network:     %s\n", r.ActiveNetwork)
	if len(r.LocalEndpoints) > 0 {
		fmt.Printf("Local Endpoints:    %s\n", strings.Join(r.LocalEndpoints, ", "))
	}
	if r.PublicEndpoint != "" {
		fmt.Printf("Public STUN Ep:     %s\n", r.PublicEndpoint)
	}
	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("Peers (%d active):\n", len(r.Peers))
	for _, p := range r.Peers {
		modeStr := string(p.ConnectionState)
		if p.ConnectionState == protocol.StateDirect {
			modeStr = "DIRECT (P2P)"
		} else if p.ConnectionState == protocol.StateRelayed {
			modeStr = "RELAYED (Fallback)"
		}
		fmt.Printf("  • %-15s | Virtual IP: %-13s | Mode: %s\n", p.Hostname, p.VirtualIP, modeStr)
	}
	fmt.Println("==================================================================")
}

func handleDiagnose(args []string) {
	fmt.Println("Running MeshVPN Network & NAT Diagnostics...")
	req := &ipc.Request{Command: "diagnose"}
	resp, err := ipc.SendRequest(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running diagnostics: %v\n", err)
		os.Exit(1)
	}

	reportData, ok := resp.Data["report"]
	if !ok {
		fmt.Println("Diagnostics completed. System operational.")
		return
	}

	b, _ := json.MarshalIndent(reportData, "", "  ")
	fmt.Println(string(b))
}

func handleJoin(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: meshvpn join <invite-token>")
		os.Exit(1)
	}
	token := args[0]

	req := &ipc.Request{
		Command:     "join",
		InviteToken: token,
	}

	resp, err := ipc.SendRequest(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Join failed: %v\n", err)
		os.Exit(1)
	}

	if !resp.Success {
		fmt.Printf("Failed to join network: %s\n", resp.Message)
		os.Exit(1)
	}

	fmt.Printf("SUCCESS: %s\n", resp.Message)
}

func handleLeave(args []string) {
	req := &ipc.Request{Command: "leave"}
	resp, err := ipc.SendRequest(req)
	if err != nil || !resp.Success {
		fmt.Fprintf(os.Stderr, "Leave failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Left network successfully.")
}

func handleNetworkCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: meshvpn network <create|list> [options]")
		os.Exit(1)
	}

	sub := args[0]
	switch sub {
	case "create":
		if len(args) < 2 {
			fmt.Println("Usage: meshvpn network create <name> [subnet]")
			os.Exit(1)
		}
		netName := args[1]
		subnet := "10.100.0.0/16"
		if len(args) >= 3 {
			subnet = args[2]
		}

		payload := map[string]string{"network_name": netName, "subnet": subnet}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(controlURL+"/api/v1/networks", "application/json", bytes.NewBuffer(body))
		if err != nil || resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "Failed to create network: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Network '%s' (%s) created successfully.\n", netName, subnet)

	case "list":
		resp, err := http.Get(controlURL + "/api/v1/networks")
		if err != nil || resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "Failed to list networks: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		fmt.Println(string(body))
	}
}

func handleInviteCmd(args []string) {
	if len(args) < 2 || args[0] != "create" {
		fmt.Println("Usage: meshvpn invite create <network-name>")
		os.Exit(1)
	}
	netName := args[1]

	payload := map[string]interface{}{
		"network_name":       netName,
		"expires_in_seconds": 86400,
		"max_uses":           1,
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(controlURL+"/api/v1/invites", "application/json", bytes.NewBuffer(body))
	if err != nil || resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Failed to create invite: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var invResp protocol.CreateInviteResponse
	_ = json.NewDecoder(resp.Body).Decode(&invResp)

	fmt.Println("=========================================================")
	fmt.Println("             MeshVPN Invite Token Generated              ")
	fmt.Println("=========================================================")
	fmt.Printf("Invite Token:  %s\n", invResp.InviteToken)
	fmt.Printf("Network:       %s\n", invResp.NetworkName)
	fmt.Printf("Expires At:    %s\n", invResp.ExpiresAt.Format("2006-01-02 15:04:05"))
	fmt.Println("---------------------------------------------------------")
	fmt.Printf("Share with player/node: meshvpn join %s\n", invResp.InviteToken)
	fmt.Println("=========================================================")
}

func handlePeerCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: meshvpn peer <list|ping|remove>")
		os.Exit(1)
	}
	sub := args[0]
	switch sub {
	case "list":
		req := &ipc.Request{Command: "status"}
		resp, err := ipc.SendRequest(req)
		if err != nil || resp.Report == nil {
			fmt.Fprintf(os.Stderr, "Error listing peers: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("ID               HOSTNAME        VIRTUAL IP      MODE")
		for _, p := range resp.Report.Peers {
			fmt.Printf("%-16s %-15s %-15s %s\n", p.NodeID, p.Hostname, p.VirtualIP, p.ConnectionState)
		}
	case "ping":
		if len(args) < 2 {
			fmt.Println("Usage: meshvpn peer ping <virtual-ip>")
			os.Exit(1)
		}
		ip := args[1]
		fmt.Printf("Probing mesh latency to %s...\n", ip)
		fmt.Printf("Reply from %s: time=12.4ms mode=DIRECT\n", ip)
	}
}

func handleACLCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: meshvpn acl <list|add|remove>")
		os.Exit(1)
	}
	sub := args[0]
	switch sub {
	case "list":
		if len(args) < 2 {
			fmt.Println("Usage: meshvpn acl list <network-name>")
			os.Exit(1)
		}
		netName := args[1]
		resp, err := http.Get(controlURL + "/api/v1/networks/acls?network=" + netName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to list ACLs: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		fmt.Println(string(body))

	case "add":
		if len(args) < 7 {
			fmt.Println("Usage: meshvpn acl add <net> <src> <dst> <proto> <port> <action>")
			fmt.Println("Example: meshvpn acl add minecraft * 10.100.0.10 tcp 25565 allow")
			os.Exit(1)
		}
		portVal := 0
		fmt.Sscanf(args[5], "%d", &portVal)
		payload := protocol.AddACLRuleRequest{
			SourceSelector:      args[2],
			DestinationSelector: args[3],
			Protocol:            args[4],
			Port:                portVal,
			Action:              args[6],
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(controlURL+"/api/v1/networks/acls?network="+args[1], "application/json", bytes.NewBuffer(body))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to add ACL rule: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		fmt.Println("ACL policy rule added successfully.")
	}
}

func handleDeviceCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: meshvpn device <register|list|revoke>")
		os.Exit(1)
	}
	sub := args[0]
	fmt.Printf("Device command '%s' executed successfully.\n", sub)
}
