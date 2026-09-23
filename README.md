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

## Quickstart: Self-Hosting Server (Native or Docker)

### Option A: Native All-in-One Server (Without Docker - Easiest)

MeshVPN can run the **entire backend infrastructure in a single binary** without Docker. It automatically bundles the HTTP REST Control Plane (`:8080`), UDP STUN Reflector (`:3478`), and Zero-Knowledge Relay Forwarder (`:41641`):

```bash
# Windows: Double-click start-server.bat or run:
.\bin\meshvpn-server.exe

# Linux / macOS:
./bin/meshvpn-server
```

### Option B: Docker Compose

```bash
cp .env.example .env
docker-compose up -d

# Create a password-protected network:
docker exec -it meshvpn-control meshvpn network create minecraft mysecretpass
```

---

## ⚡ Client Setup: CLI & Headless Environments

### 🚀 One-Liner Automated Install & Run (Linux / Homelab / VPS)

Install the daemon, configure the systemd background service, and connect to a network **in a single command**:

```bash
# Install and join in one line:
curl -fsSL https://raw.githubusercontent.com/Dev235/MeshVPN/master/install.sh | bash -s -- join minecraft mysecretpass

# Or install first, then run commands at your leisure:
curl -fsSL https://raw.githubusercontent.com/Dev235/MeshVPN/master/install.sh | bash
```

### ZeroTier-Style Clean CLI Usage

No manual background daemon management required — `meshvpn` automatically launches the background service if it is offline:

```bash
# Create a network (you are automatically placed inside it immediately!):
meshvpn network create minecraft mysecretpass

# Join an existing network:
meshvpn join minecraft mysecretpass

# Check your assigned Virtual IP and status:
meshvpn status

# List networks:
meshvpn listnetworks

# Leave network:
meshvpn leave
```

---

## 🖥️ Windows Desktop GUI Setup (Non-Technical Users)

Designed from the ground up for non-technical users who want a seamless, zero-configuration desktop experience:

1. **One-Liner Windows Install**:
   ```powershell
   irm https://raw.githubusercontent.com/Dev235/MeshVPN/master/install.ps1 | iex
   ```
   Or double-click `start-gui.bat` / run `.\bin\meshvpn-gui.exe`.

2. **Zero Configuration**:
   - **Silent Background Engine**: The background daemon starts automatically in the background — no terminal windows to manage.
   - **Auto-Join on Create**: Click **Network** -> **Create new network...**, enter a name and password, and you are **instantly placed inside it** with your Virtual IPv4 assigned!
   - **One-Click Connect**: Friends click **Network** -> **Join an existing network...**, type credentials, and connect.
   - **System Tray ("Hidden Icons")**: Minimizes cleanly to the Windows taskbar notification area so your mesh connection stays alive in the background.
   - **Power Switch**: Toggle your VPN connection On/Off with one click.

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
