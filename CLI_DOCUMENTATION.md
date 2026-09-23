# MeshVPN CLI Documentation

Complete command reference for `meshvpn`, the command-line administration and status tool for MeshVPN.

---

## 1. Overview & Execution Modes

`meshvpn` can be executed directly on a client or server host, or invoked through Docker containers managing the control plane.

### Native Execution Syntax
```bash
meshvpn <command> [subcommand] [arguments...] [flags]
```
*(On Windows PowerShell, use `.\meshvpn.exe` or `.\bin\meshvpn.exe`)*

### Docker Execution Syntax
When running the control server inside Docker (`meshvpn-control` container):
```bash
docker exec -it meshvpn-control meshvpn <command> [arguments...]
```

---

## 2. Command Index

| Category | Command | Description |
| :--- | :--- | :--- |
| **Status & Health** | [`meshvpn status`](#meshvpn-status) | View node identity, virtual IP, active network, and peer topology. |
| | [`meshvpn diagnose`](#meshvpn-diagnose) | Run comprehensive STUN, control server, and P2P path health diagnostics. |
| **Network Management** | [`meshvpn network create`](#meshvpn-network-create-name-subnet) | Create a isolated virtual network subnet. |
| | [`meshvpn network list`](#meshvpn-network-list) | List all registered virtual networks. |
| | [`meshvpn join`](#meshvpn-join-invite-token) | Join a virtual network using an invitation token. |
| | [`meshvpn leave`](#meshvpn-leave) | Disconnect and leave the currently active network. |
| **Invitations** | [`meshvpn invite create`](#meshvpn-invite-create-network-name) | Generate a temporary, single-use network invitation token. |
| | [`meshvpn invite revoke`](#meshvpn-invite-revoke-token) | Revoke an active invitation token. |
| **Peer Management** | [`meshvpn peer list`](#meshvpn-peer-list) | List all peers in active network, virtual IPs, and connection modes (`DIRECT` vs `RELAYED`). |
| | [`meshvpn peer ping`](#meshvpn-peer-ping-virtual-ip) | Probe latency to a mesh peer over virtual IPv4. |
| | [`meshvpn peer remove`](#meshvpn-peer-remove-node-id) | Disconnect and remove a peer node from the network. |
| **Access Control (ACL)**| [`meshvpn acl list`](#meshvpn-acl-list-network-name) | List active ACL rules for a network. |
| | [`meshvpn acl add`](#meshvpn-acl-add-net-src-dst-proto-port-action) | Add a deny-by-default packet filtering rule. |
| | [`meshvpn acl remove`](#meshvpn-acl-remove-net-rule-id) | Remove an ACL policy rule. |
| **Device Identity** | [`meshvpn device register`](#meshvpn-device-register) | Register device cryptographic identity with control plane. |
| | [`meshvpn device list`](#meshvpn-device-list) | List all authorized device identities. |
| | [`meshvpn device revoke`](#meshvpn-device-revoke-node-id) | Immediately revoke device authorization and keys. |
| **Authentication** | [`meshvpn login`](#meshvpn-login) | Authenticate local CLI session against control plane URL. |
| | [`meshvpn logout`](#meshvpn-logout) | Clear local authentication session. |

---

## 3. Command Reference

### `meshvpn status`

Display local node status, WireGuard virtual interface, assigned virtual IPv4, active network, candidate endpoints, and peer topology.

#### Flags
- `--json` : Output machine-readable JSON format.

#### Example (Human-Readable)
```bash
meshvpn status
```
```text
==================================================================
                   MeshVPN Status Overview                        
==================================================================
Node ID:            a1b2c3d4e5f678901234567890abcdef
Control Server:     http://127.0.0.1:8080 [CONNECTED]
WireGuard Adapter:  mesh0 [UP]
Virtual IPv4:       10.100.0.10
Active Network:     minecraft
Local Endpoints:    192.168.1.50:51820 (LAN)
Public STUN Ep:     203.0.113.45:3478
------------------------------------------------------------------
Peers (2 active):
  • player-pc       | Virtual IP: 10.100.0.11   | Mode: DIRECT (P2P)
  • admin-laptop    | Virtual IP: 10.100.0.12   | Mode: RELAYED (Fallback)
==================================================================
```

#### Example (JSON Format)
```bash
meshvpn status --json
```
```json
{
  "node_id": "a1b2c3d4e5f678901234567890abcdef",
  "control_server": "http://127.0.0.1:8080",
  "control_status": "CONNECTED",
  "wireguard_interface": "mesh0",
  "interface_status": "UP",
  "virtual_ip": "10.100.0.10",
  "active_network": "minecraft",
  "local_endpoints": [
    "192.168.1.50:51820 (LAN)"
  ],
  "public_endpoint": "203.0.113.45:3478",
  "peers": [
    {
      "node_id": "b2c3d4e5f678901234567890abcdef01",
      "hostname": "player-pc",
      "virtual_ip": "10.100.0.11",
      "wireguard_public_key": "c2FtcGxlX3dnbF9rZXk=",
      "connection_state": "DIRECT"
    }
  ]
}
```

---

### `meshvpn diagnose`

Execute full diagnostic probes for Control Server API, authentication state, STUN discovery reflectors, UDP path probing, per-peer latency, and packet loss.

#### Example
```bash
meshvpn diagnose
```
```json
{
  "timestamp": "2026-09-24T00:45:10Z",
  "control_server": "http://127.0.0.1:8080",
  "control_connected": true,
  "auth_status": "OK (12ms)",
  "wireguard_status": "OK",
  "virtual_interface": "UP",
  "overall_health": "HEALTHY",
  "peer_diagnostics": [
    {
      "peer_node_id": "b2c3d4e5f678901234567890abcdef01",
      "hostname": "player-pc",
      "virtual_ip": "10.100.0.11",
      "endpoint_discovery": "OK",
      "nat_traversal_status": "SUCCESS",
      "connection_type": "DIRECT",
      "active_endpoint": "198.51.100.22:51820",
      "latency_ms": 14.2,
      "packet_loss_percent": 0
    }
  ]
}
```

---

### `meshvpn network create <name> [subnet]`

Create a new isolated virtual network subnet.

#### Parameters
- `<name>` *(Required)*: Unique network identifier (e.g. `minecraft`, `homelab`, `dev`).
- `[subnet]` *(Optional)*: Subnet CIDR block. Default is `10.100.0.0/16`.

#### Example
```bash
meshvpn network create minecraft 10.100.0.0/16
```
```text
Network 'minecraft' (10.100.0.0/16) created successfully.
```

---

### `meshvpn network list`

List all created virtual networks registered on the control plane.

#### Example
```bash
meshvpn network list
```

---

### `meshvpn invite create <network-name>`

Generate a cryptographically secure, temporary invitation token for joining a virtual network.

#### Parameters
- `<network-name>` *(Required)*: Name of target virtual network.

#### Example
```bash
meshvpn invite create minecraft
```
```text
=========================================================
             MeshVPN Invite Token Generated              
=========================================================
Invite Token:  2bc22a90c780370c086eab5c09d83b71
Network:       minecraft
Expires At:    2026-09-25 00:45:22
---------------------------------------------------------
Share with player/node: meshvpn join 2bc22a90c780370c086eab5c09d83b71
=========================================================
```

---

### `meshvpn join <invite-token>`

Join a virtual network using a valid invitation token. Assigns a unique virtual IPv4 address (`10.100.x.x`).

#### Parameters
- `<invite-token>` *(Required)*: 32-character hex invite token.

#### Example
```bash
meshvpn join 2bc22a90c780370c086eab5c09d83b71
```
```text
SUCCESS: Successfully joined network 'minecraft'. Assigned Virtual IP: 10.100.0.10
```

---

### `meshvpn leave`

Disconnect and leave the currently active virtual network, tearing down virtual interface routes.

#### Example
```bash
meshvpn leave
```
```text
Left network successfully.
```

---

### `meshvpn peer list`

List active peers in the network with Node IDs, hostnames, assigned virtual IPs, and connectivity mode (`DIRECT` vs `RELAYED`).

#### Example
```bash
meshvpn peer list
```
```text
ID               HOSTNAME        VIRTUAL IP      MODE
b2c3d4e5f67801   player-pc       10.100.0.11     DIRECT
c3d4e5f6789002   friend-laptop   10.100.0.12     RELAYED
```

---

### `meshvpn peer ping <virtual-ip>`

Probe mesh connection latency to a specific peer virtual IPv4 address.

#### Example
```bash
meshvpn peer ping 10.100.0.10
```
```text
Probing mesh latency to 10.100.0.10...
Reply from 10.100.0.10: time=12.4ms mode=DIRECT
```

---

### `meshvpn acl list <network-name>`

View active Access Control List (ACL) policy rules for a virtual network.

#### Example
```bash
meshvpn acl list minecraft
```

---

### `meshvpn acl add <net> <src> <dst> <proto> <port> <action>`

Add a packet filtering policy rule to a virtual network.

#### Parameters
- `<net>`: Network name.
- `<src>`: Source selector (`*`, CIDR, or Virtual IP).
- `<dst>`: Destination selector (Virtual IP).
- `<proto>`: Protocol (`tcp`, `udp`, or `any`).
- `<port>`: Target port number (`0` for any).
- `<action>`: Action (`allow` or `deny`).

#### Example (Restrict traffic to Minecraft port 25565 only)
```bash
meshvpn acl add minecraft * 10.100.0.10 tcp 25565 allow
meshvpn acl add minecraft * 10.100.0.10 udp 25565 allow
```
```text
ACL policy rule added successfully.
```

---

### `meshvpn device register` / `list` / `revoke`

Manage node identity registrations and revokations.

#### Examples
```bash
meshvpn device list
meshvpn device revoke b2c3d4e5f67801
```

---

## 4. Exit Codes

| Code | Meaning |
| :---: | :--- |
| `0` | Command executed successfully. |
| `1` | Command error, invalid argument, or daemon communication failure. |
