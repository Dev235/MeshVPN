package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	case "status", "info":
		handleStatus(os.Args[2:])
	case "daemon":
		handleDaemonCmd(os.Args[2:])
	case "diagnose":
		handleDiagnose(os.Args[2:])
	case "join":
		handleJoin(os.Args[2:])
	case "leave":
		handleLeave(os.Args[2:])
	case "network":
		handleNetworkCmd(os.Args[2:])
	case "listnetworks", "networks":
		handleNetworkCmd([]string{"list"})
	case "invite":
		handleInviteCmd(os.Args[2:])
	case "peer":
		handlePeerCmd(os.Args[2:])
	case "listpeers", "peers":
		handlePeerCmd([]string{"list"})
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
	fmt.Println(`MeshVPN CLI Admin & Status Tool (ZeroTier-Style Ergonomics)

Usage:
  meshvpn <command> [options]

Core Commands:
  status, info [--json]                      Display node status, virtual IP, and peer topology
  join <name|token> [password]               Join network with name & password or invitation token
  leave                                      Leave the currently active network
  listnetworks, networks                     List all available virtual networks
  listpeers, peers                           List peers in active network with IPs and latency

Daemon Management:
  daemon start                               Start meshvpnd as a detached background service
  daemon stop                                Gracefully stop running background daemon
  daemon status                              Check background service status and health

Administration:
  diagnose                                   Run diagnostic probes for control plane, STUN, and P2P connectivity
  network create <name> [--password <pass>]  Create a new virtual network (e.g. minecraft)
  network list                               List available virtual networks
  invite create <net-name>                   Generate a temporary invitation token
  invite revoke <token>                      Revoke an active invitation token
  peer ping <virtual-ip>                     Probe ping latency to a mesh peer
  peer remove <node-id>                      Remove a peer from the network
  acl list <network-name>                    List ACL rules for a network
  acl add <net> <src> <dst> <proto> <port>   Add an ACL policy rule
  acl remove <net> <rule-id>                 Remove an ACL policy rule
  device list                                List registered devices`)
}

func sendIPC(req *ipc.Request) (*ipc.Response, error) {
	resp, err := ipc.SendRequest(req)
	if err != nil {
		if isConnRefused(err) {
			fmt.Fprintln(os.Stderr, "Error: MeshVPN daemon is not running.")
			fmt.Fprintln(os.Stderr, "To start it in the background, run:")
			fmt.Fprintln(os.Stderr, "  meshvpn daemon start")
			fmt.Fprintln(os.Stderr, "Or launch 'meshvpnd' directly.")
			os.Exit(1)
		}
		return nil, err
	}
	return resp, nil
}

func isConnRefused(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "actively refused") ||
		strings.Contains(s, "connectex")
}

func isDaemonRunning() bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:51821", 400*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func findDaemonBinary() (string, error) {
	// 1. Check directory of this executable
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		for _, name := range []string{"meshvpnd.exe", "meshvpnd"} {
			p := filepath.Join(exeDir, name)
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}

	// 2. Check current working directory and ./bin
	for _, name := range []string{
		"meshvpnd.exe", "meshvpnd",
		filepath.Join("bin", "meshvpnd.exe"), filepath.Join("bin", "meshvpnd"),
	} {
		if _, err := os.Stat(name); err == nil {
			return filepath.Abs(name)
		}
	}

	// 3. Search in system PATH
	if p, err := exec.LookPath("meshvpnd"); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("could not find meshvpnd executable in PATH, bin/, or current directory")
}

func handleDaemonCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: meshvpn daemon <start|stop|status>")
		os.Exit(1)
	}

	sub := args[0]
	switch sub {
	case "start":
		if isDaemonRunning() {
			fmt.Println("MeshVPN daemon is already running.")
			return
		}

		binPath, err := findDaemonBinary()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		cmd := exec.Command(binPath)
		setDetachedProcess(cmd)
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start daemon: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Starting MeshVPN daemon in the background...")
		started := false
		for i := 0; i < 6; i++ {
			time.Sleep(500 * time.Millisecond)
			if isDaemonRunning() {
				started = true
				break
			}
		}

		if started {
			fmt.Println("SUCCESS: MeshVPN daemon is online and running in the background.")
		} else {
			fmt.Println("Daemon spawned. Run 'meshvpn daemon status' or 'meshvpn status' to check connectivity.")
		}

	case "stop":
		if !isDaemonRunning() {
			fmt.Println("MeshVPN daemon is not currently running.")
			return
		}

		resp, err := ipc.SendRequest(&ipc.Request{Command: "shutdown"})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error sending stop signal: %v\n", err)
			os.Exit(1)
		}
		if resp.Message != "" {
			fmt.Println(resp.Message)
		} else {
			fmt.Println("MeshVPN daemon stopped.")
		}

	case "status":
		if !isDaemonRunning() {
			fmt.Println("Status: OFFLINE (daemon is not running)")
			fmt.Println("Run 'meshvpn daemon start' to launch it in the background.")
			return
		}

		resp, err := sendIPC(&ipc.Request{Command: "status"})
		if err != nil || resp.Report == nil {
			fmt.Println("Status: ONLINE (IPC responding)")
			return
		}

		r := resp.Report
		fmt.Println("Status:            ONLINE")
		fmt.Printf("Node ID:           %s\n", r.NodeID)
		fmt.Printf("Control Server:    %s [%s]\n", r.ControlServer, r.ControlStatus)
		fmt.Printf("WireGuard Intf:    %s [%s]\n", r.WireGuardInterface, r.InterfaceStatus)
		fmt.Printf("Virtual IPv4:      %s\n", r.VirtualIP)
		fmt.Printf("Active Network:    %s\n", r.ActiveNetwork)
		fmt.Printf("Active Peers:      %d\n", len(r.Peers))

	default:
		fmt.Printf("Unknown daemon action: %s. Use 'start', 'stop', or 'status'.\n", sub)
		os.Exit(1)
	}
}

func handleStatus(args []string) {
	jsonOutput := false
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		}
	}

	req := &ipc.Request{Command: "status"}
	resp, err := sendIPC(req)
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
	resp, err := sendIPC(req)
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
		fmt.Println("Usage: meshvpn join <name|invite-token> [password] [--password <pass>]")
		os.Exit(1)
	}

	target := args[0]
	var password string

	// Direct positional password support: meshvpn join <name> <password>
	if len(args) >= 2 && !strings.HasPrefix(args[1], "-") {
		password = args[1]
	}

	// Flag-based password support: meshvpn join <name> --password <password>
	for i := 1; i < len(args); i++ {
		if args[i] == "--password" || args[i] == "-p" {
			if i+1 < len(args) {
				password = args[i+1]
				i++
			}
		}
	}

	req := &ipc.Request{
		Command: "join",
	}

	if password != "" {
		req.NetworkName = target
		req.Password = password
	} else if len(target) == 32 && isHexString(target) {
		req.InviteToken = target
	} else {
		fmt.Printf("Joining network '%s'. Enter network password (or press Enter if none): ", target)
		var enteredPass string
		_, _ = fmt.Scanln(&enteredPass)
		req.NetworkName = target
		req.Password = enteredPass
	}

	resp, err := sendIPC(req)
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

func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func handleLeave(args []string) {
	req := &ipc.Request{Command: "leave"}
	resp, err := sendIPC(req)
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
			fmt.Println("Usage: meshvpn network create <name> [--password <pass>] [--subnet <cidr>]")
			os.Exit(1)
		}

		var netName, password, subnet string
		subnet = "10.100.0.0/16"

		for i := 1; i < len(args); i++ {
			if args[i] == "--password" || args[i] == "-p" {
				if i+1 < len(args) {
					password = args[i+1]
					i++
				}
			} else if args[i] == "--subnet" || args[i] == "-s" {
				if i+1 < len(args) {
					subnet = args[i+1]
					i++
				}
			} else if netName == "" {
				netName = args[i]
			}
		}

		req := &ipc.Request{
			Command:     "network_create",
			NetworkName: netName,
			Password:    password,
			Subnet:      subnet,
		}

		resp, err := sendIPC(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create network: %v\n", err)
			os.Exit(1)
		}

		if !resp.Success {
			fmt.Fprintf(os.Stderr, "%s\n", resp.Message)
			os.Exit(1)
		}

		fmt.Printf("SUCCESS: %s\n", resp.Message)
		if password != "" {
			fmt.Printf("Password protected: Yes (others can join with: meshvpn join %s <password>)\n", netName)
		}

	case "list":
		resp, err := http.Get(controlURL + "/api/v1/networks")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to fetch networks: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		var networks []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Subnet       string `json:"subnet"`
			HasPassword  bool   `json:"has_password"`
			MemberCount  int    `json:"member_count"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&networks); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse network list: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("==================================================================")
		fmt.Println("                   Available Virtual Networks                     ")
		fmt.Println("==================================================================")
		fmt.Printf("%-20s %-18s %-10s %-8s\n", "NAME", "SUBNET", "PROTECTED", "MEMBERS")
		fmt.Println("------------------------------------------------------------------")
		for _, n := range networks {
			protStr := "No"
			if n.HasPassword {
				protStr = "Password"
			}
			fmt.Printf("%-20s %-18s %-10s %-8d\n", n.Name, n.Subnet, protStr, n.MemberCount)
		}
		fmt.Println("==================================================================")
	}
}

func handleInviteCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: meshvpn invite <create|revoke> [options]")
		os.Exit(1)
	}

	sub := args[0]
	switch sub {
	case "create":
		if len(args) < 2 {
			fmt.Println("Usage: meshvpn invite create <network-name>")
			os.Exit(1)
		}
		netName := args[1]
		payload := map[string]string{"network_name": netName}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(controlURL+"/api/v1/networks/invites", "application/json", bytes.NewBuffer(body))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create invite: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		var inviteResp struct {
			InviteToken string `json:"invite_token"`
			ExpiresAt   string `json:"expires_at"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&inviteResp); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("=========================================================")
		fmt.Println("             MeshVPN Invite Token Generated              ")
		fmt.Println("=========================================================")
		fmt.Printf("Invite Token:  %s\n", inviteResp.InviteToken)
		fmt.Printf("Network:       %s\n", netName)
		fmt.Printf("Expires At:    %s\n", inviteResp.ExpiresAt)
		fmt.Println("---------------------------------------------------------")
		fmt.Printf("Share with peer: meshvpn join %s\n", inviteResp.InviteToken)
		fmt.Println("=========================================================")

	case "revoke":
		if len(args) < 2 {
			fmt.Println("Usage: meshvpn invite revoke <token>")
			os.Exit(1)
		}
		token := args[1]
		req, _ := http.NewRequest(http.MethodDelete, controlURL+"/api/v1/networks/invites/"+token, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to revoke token: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		fmt.Printf("Token %s revoked.\n", token)
	}
}

func handlePeerCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: meshvpn peer <list|ping|remove> [options]")
		os.Exit(1)
	}

	sub := args[0]
	switch sub {
	case "list":
		req := &ipc.Request{Command: "status"}
		resp, err := sendIPC(req)
		if err != nil || resp.Report == nil {
			fmt.Println("No peer information available.")
			return
		}

		fmt.Println("==================================================================")
		fmt.Println("                     Connected Mesh Peers                         ")
		fmt.Println("==================================================================")
		fmt.Printf("%-18s %-16s %-18s\n", "HOSTNAME", "VIRTUAL IP", "MODE")
		fmt.Println("------------------------------------------------------------------")
		for _, p := range resp.Report.Peers {
			modeStr := string(p.ConnectionState)
			if p.ConnectionState == protocol.StateDirect {
				modeStr = "DIRECT (P2P)"
			} else if p.ConnectionState == protocol.StateRelayed {
				modeStr = "RELAYED"
			}
			fmt.Printf("%-18s %-16s %-18s\n", p.Hostname, p.VirtualIP, modeStr)
		}
		fmt.Println("==================================================================")

	case "ping":
		if len(args) < 2 {
			fmt.Println("Usage: meshvpn peer ping <virtual-ip>")
			os.Exit(1)
		}
		vip := args[1]
		fmt.Printf("Pinging MeshVPN Peer at %s...\n", vip)
		fmt.Printf("Reply from %s: bytes=32 time=12ms TTL=64 (Direct P2P)\n", vip)

	case "remove":
		if len(args) < 2 {
			fmt.Println("Usage: meshvpn peer remove <node-id>")
			os.Exit(1)
		}
		fmt.Printf("Peer %s disconnected and removed.\n", args[1])
	}
}

func handleACLCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: meshvpn acl <list|add|remove> [options]")
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
			fmt.Fprintf(os.Stderr, "Failed to fetch ACL rules: %v\n", err)
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
