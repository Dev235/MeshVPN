# MeshVPN: Self-Hosted Encrypted Mesh Network

MeshVPN is a fully self-hosted, high-performance mesh VPN system built with Go and WireGuard. It is designed for internal gaming networks (e.g., Minecraft), homelabs, and server infrastructure across Linux and Windows.

> [!WARNING]
> **AI-Driven Project Notice**:
> This codebase was built with AI assistance. As a result, please expect potential bugs, edge-case vulnerabilities, or unoptimized network handling. Security audits, bug reports, and Pull Requests / fixes are warmly welcomed!

---

## Architecture Overview

- **Control Server (`meshvpn-server`)**: Centralized identity management, device authorization, IPAM allocation (`10.100.0.0/16`), peer candidate exchange, ACL rule distribution, and embedded STUN reflector. (HTTPS/REST on `:8080`, STUN on `:3478/udp`).
- **Zero-Knowledge Relay (`meshvpn-relay`)**: Standalone, zero-knowledge UDP forwarder (`:41641/udp`). Forwards E2E WireGuard encrypted ciphertext frames. Possesses **zero decryption keys**.
- **Client Daemon (`meshvpnd`)**: Headless service daemon for Linux and Windows. Handles STUN discovery, parallel UDP hole punching probing, WireGuard interface sync, and ACL enforcement.
- **CLI (`meshvpn`)**: Command-line administrative tool for headless environments (Linux servers, Docker, scripts) to manage password-protected networks, peers, and status.
- **Desktop GUI (`meshvpn-gui`)**: Seamless, standalone desktop app for Windows and desktop environments with system tray support, power toggle switch, one-click copy IP, Network menus, and live peer tree.

---

## Quickstart: Self-Hosting Control & Relay Server

### Option A: Docker Compose (Recommended)
```bash
cp .env.example .env
docker-compose up -d

# Create a password-protected network:
docker exec -it meshvpn-control meshvpn network create minecraft --password mysecretpass
```

### Option B: Native Execution (Without Docker)

1. **Build Binaries**:
   ```bash
   go build -o bin/meshvpn-server ./cmd/meshvpn-server
   go build -o bin/meshvpn-relay ./cmd/meshvpn-relay
   go build -o bin/meshvpnd ./cmd/meshvpnd
   go build -o bin/meshvpn ./cmd/meshvpn
   go build -o bin/meshvpn-gui ./cmd/meshvpn-gui
   ```

2. **Start Control Server & Embedded STUN Reflector**:
   ```bash
   ./bin/meshvpn-server -addr :8080 -stun-addr :3478 -db meshvpn-control.json
   ```

3. **Start Zero-Knowledge Relay Server**:
   ```bash
   ./bin/meshvpn-relay -addr :41641
   ```

---

## Headless Linux Minecraft Server Setup

### 1. Build and Install Daemon

```bash
sudo cp meshvpnd /usr/local/bin/
sudo cp meshvpn /usr/local/bin/
sudo cp deploy/meshvpn.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now meshvpn
```

### 2. Join Network (Headless CLI)

```bash
# Join network using Network Name + Password:
meshvpn join minecraft --password mysecretpass

# Or join using a legacy invite token:
meshvpn join <invite-token>
```

Verify Virtual IPv4:
```bash
meshvpn status
```
Output:
```text
Virtual IPv4: 10.100.0.10
Active Network: minecraft
```

---

## Windows Desktop GUI Setup

For a seamless Windows desktop experience with System Tray ("hidden icons") support:

1. **Start the GUI Application**:
   ```powershell
   # Run the standalone desktop GUI:
   .\bin\meshvpn-gui.exe
   ```
2. **Create or Join Networks**:
   - Click **Network** -> **Create new network...**: Enter **Network Name** and **Password**.
   - Click **Network** -> **Join an existing network...**: Enter credentials to connect.
   - **One-Click Copy**: Click the **Copy** button next to your assigned Virtual IPv4 (e.g. `10.100.0.11`).
   - **Live Peer Tree**: View all online friends/servers, latency, connection mode (Direct P2P vs Relayed), and ping peers.
   - **Power Switch**: Turn VPN connection On/Off instantly with the power button.
   - **System Tray**: Minimizes cleanly to the Windows taskbar hidden icons area with live status and quick action menu.

### 3. Bind Minecraft Server

Configure `server.properties` on your Minecraft server:
```properties
server-ip=10.100.0.10
server-port=25565
```

Players can now connect directly to `10.100.0.10:25565` over the encrypted mesh VPN without router port forwarding!

---

## CLI Reference Cheatsheet

For full command details, syntax flags, Docker execution examples, and JSON schemas, see the complete [CLI Documentation](CLI_DOCUMENTATION.md).

```bash
# Connection & Status
meshvpn status              # Show status summary
meshvpn status --json       # Machine-readable JSON output
meshvpn diagnose            # Comprehensive diagnostic probe report

# Network & Invites
meshvpn network create <name> [subnet]
meshvpn network list
meshvpn invite create <net-name>
meshvpn join <invite-token>
meshvpn leave

# Peer Management
meshvpn peer list
meshvpn peer ping <virtual-ip>
meshvpn peer remove <node-id>

# Access Control Lists (ACLs)
meshvpn acl list <network-name>
meshvpn acl add minecraft * 10.100.0.10 tcp 25565 allow
```

---

## Verification & Unit Testing

Run automated package tests:
```bash
go test -v ./...
```
