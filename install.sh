#!/bin/bash
set -e

# MeshVPN One-Liner Installer for Linux & Headless Servers
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Dev235/MeshVPN/master/install.sh | bash
#   curl -fsSL .../install.sh | bash -s -- join <network> <password>

BOLD='\033[1m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BOLD}${BLUE}"
echo "=========================================================="
echo "           MeshVPN Automated System Installer             "
echo "=========================================================="
echo -e "${NC}"

# Check for root / sudo
SUDO=""
if [ "$EUID" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
    else
        echo "Error: Please run this installer as root or with sudo installed."
        exit 1
    fi
fi

INSTALL_DIR="/usr/local/bin"
SERVICE_DIR="/etc/systemd/system"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"

echo -e "Detecting installation sources..."

# 1. Install or compile binaries
if [ -f "$SCRIPT_DIR/bin/meshvpn" ] && [ -f "$SCRIPT_DIR/bin/meshvpnd" ]; then
    echo -e "Installing precompiled binaries from local repo..."
    $SUDO cp "$SCRIPT_DIR/bin/meshvpn" "$INSTALL_DIR/meshvpn"
    $SUDO cp "$SCRIPT_DIR/bin/meshvpnd" "$INSTALL_DIR/meshvpnd"
elif command -v go >/dev/null 2>&1 && [ -f "$SCRIPT_DIR/go.mod" ]; then
    echo -e "Compiling optimized native binaries using Go..."
    cd "$SCRIPT_DIR"
    CGO_ENABLED=0 go build -ldflags="-w -s" -o "$INSTALL_DIR/meshvpn" ./cmd/meshvpn
    CGO_ENABLED=0 go build -ldflags="-w -s" -o "$INSTALL_DIR/meshvpnd" ./cmd/meshvpnd
else
    # Fallback to downloading or cloning repo if invoked directly via pipe
    TMP_DIR=$(mktemp -d)
    echo -e "Cloning latest MeshVPN repository..."
    git clone --depth 1 https://github.com/Dev235/MeshVPN.git "$TMP_DIR/meshvpn" || git clone --depth 1 https://github.com/Dev235/Java.git "$TMP_DIR/meshvpn"
    cd "$TMP_DIR/meshvpn"
    if command -v go >/dev/null 2>&1; then
        CGO_ENABLED=0 go build -ldflags="-w -s" -o "$INSTALL_DIR/meshvpn" ./cmd/meshvpn
        CGO_ENABLED=0 go build -ldflags="-w -s" -o "$INSTALL_DIR/meshvpnd" ./cmd/meshvpnd
    elif [ -f "./bin/meshvpn" ]; then
        $SUDO cp "./bin/meshvpn" "$INSTALL_DIR/meshvpn"
        $SUDO cp "./bin/meshvpnd" "$INSTALL_DIR/meshvpnd"
    fi
    rm -rf "$TMP_DIR"
fi

$SUDO chmod +x "$INSTALL_DIR/meshvpn"
$SUDO chmod +x "$INSTALL_DIR/meshvpnd"

echo -e "${GREEN}✓ Installed meshvpn & meshvpnd to $INSTALL_DIR${NC}"

# 2. Setup Systemd Service
if [ -d "$SERVICE_DIR" ] && command -v systemctl >/dev/null 2>&1; then
    echo "Configuring background systemd service..."
    $SUDO tee "$SERVICE_DIR/meshvpn.service" > /dev/null <<EOF
[Unit]
Description=MeshVPN Headless Daemon Service
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/meshvpnd -interface mesh0
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

    $SUDO systemctl daemon-reload
    $SUDO systemctl enable --now meshvpn.service
    echo -e "${GREEN}✓ MeshVPN background service enabled and active!${NC}"
else
    echo "Notice: systemd not detected. You can run daemon in background with: meshvpn daemon start"
fi

echo ""
echo -e "${BOLD}${GREEN}Installation Successful!${NC}"
echo "Commands:"
echo "  meshvpn status                    - View current node status and IP"
echo "  meshvpn join <network> [password] - Join a virtual mesh network"
echo "  meshvpn listnetworks              - View available networks"
echo "  meshvpn leave                     - Disconnect from network"
echo ""

# 3. Handle optional trailing arguments (e.g. bash -s -- join minecraft secret123)
if [ $# -gt 0 ]; then
    echo -e "Executing requested command: meshvpn $@"
    sleep 1
    "$INSTALL_DIR/meshvpn" "$@"
fi
