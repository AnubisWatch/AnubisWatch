#!/bin/bash
# AnubisWatch Installation Script
# ═══════════════════════════════════════════════════════════
# Usage: curl -fsSL https://get.anubis.watch | bash

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REPO="AnubisWatch/anubiswatch"
INSTALL_DIR="/usr/local/bin"
DATA_DIR="/var/lib/anubis"
CONFIG_DIR="/etc/anubis"

# Functions
print_banner() {
    echo -e "${BLUE}"
    echo '    _                _     _       _       _   _               _'
    echo '   / \   _ __  _   _| |__ (_) __ _| |__   | | | | _____      _| |_'
    echo '  / _ \ | '"'"'_ \| | | | '"'"'_ \| |/ _` | '"'"'_ \  | |_| |/ _ \ \ /\ / / __|'
    echo ' / ___ \| | | | |_| | |_) | | (_| | | | | |  _  | (_) \ V  V /\__ \'
    echo '/_/   \_\_| |_|\__,_|_.__/|_|\__,_|_| |_| |_| |_|\___/ \_/\_/ |___/'
    echo ''
    echo -e "${NC}"
    echo -e "${GREEN}The Judgment Never Sleeps${NC}"
    echo ''
}

detect_arch() {
    local arch=$(uname -m)
    case $arch in
        x86_64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        armv7l)
            echo "armv7"
            ;;
        *)
            echo "unsupported"
            ;;
    esac
}

detect_os() {
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case $os in
        linux|darwin)
            echo $os
            ;;
        *)
            echo "unsupported"
            ;;
    esac
}

check_prerequisites() {
    local os=$1
    local arch=$2

    if [ "$os" = "unsupported" ]; then
        echo -e "${RED}✗ Unsupported operating system${NC}"
        exit 1
    fi

    if [ "$arch" = "unsupported" ]; then
        echo -e "${RED}✗ Unsupported architecture${NC}"
        exit 1
    fi

    echo -e "${BLUE}Detected: $os/$arch${NC}"
}

get_latest_version() {
    curl -s "https://api.github.com/repos/$REPO/releases/latest" | \
        grep '"tag_name":' | \
        sed -E 's/.*"([^"]+)".*/\1/' || \
        echo "latest"
}

download_binary() {
    local version=$1
    local os=$2
    local arch=$3
    local tmpdir=$4

    local binary_name="anubis-${os}-${arch}"
    if [ "$os" = "windows" ]; then
        binary_name="${binary_name}.exe"
    fi

    local download_url="https://github.com/${REPO}/releases/download/${version}/${binary_name}"

    echo -e "${BLUE}Downloading AnubisWatch ${version}...${NC}"

    if ! curl -fsSL -o "${tmpdir}/anubis" "$download_url"; then
        echo -e "${RED}✗ Failed to download binary${NC}"
        return 1
    fi

    chmod +x "${tmpdir}/anubis"
    echo -e "${GREEN}✓ Downloaded successfully${NC}"
}

install_binary() {
    local tmpdir=$1

    echo -e "${BLUE}Installing to ${INSTALL_DIR}...${NC}"

    if [ -w "$INSTALL_DIR" ]; then
        mv "${tmpdir}/anubis" "${INSTALL_DIR}/anubis"
    else
        sudo mv "${tmpdir}/anubis" "${INSTALL_DIR}/anubis"
    fi

    echo -e "${GREEN}✓ Installed to ${INSTALL_DIR}/anubis${NC}"
}

create_directories() {
    echo -e "${BLUE}Creating directories...${NC}"

    if [ -w "/var/lib" ]; then
        mkdir -p "$DATA_DIR"
        mkdir -p "$CONFIG_DIR"
    else
        sudo mkdir -p "$DATA_DIR"
        sudo mkdir -p "$CONFIG_DIR"
        sudo chown $(id -u):$(id -g) "$DATA_DIR"
    fi

    echo -e "${GREEN}✓ Created data directory: ${DATA_DIR}${NC}"
    echo -e "${GREEN}✓ Created config directory: ${CONFIG_DIR}${NC}"
}

create_config() {
    local config_file="${CONFIG_DIR}/anubis.json"

    if [ -f "$config_file" ]; then
        echo -e "${YELLOW}⚠ Config file already exists, skipping${NC}"
        return
    fi

    echo -e "${BLUE}Creating default configuration...${NC}"

    cat > "$config_file" << 'EOF'
{
  "server": {
    "host": "0.0.0.0",
    "port": 8080,
    "tls": {
      "enabled": false
    }
  },
  "storage": {
    "path": "/var/lib/anubis/data"
  },
  "souls": [],
  "channels": [],
  "logging": {
    "level": "info",
    "format": "json"
  }
}
EOF

    echo -e "${GREEN}✓ Created config file: ${config_file}${NC}"
}

create_systemd_service() {
    if ! command -v systemctl &> /dev/null; then
        echo -e "${YELLOW}⚠ systemd not detected, skipping service creation${NC}"
        return
    fi

    local service_file="/etc/systemd/system/anubis.service"

    if [ -f "$service_file" ]; then
        echo -e "${YELLOW}⚠ Service file already exists, skipping${NC}"
        return
    fi

    echo -e "${BLUE}Creating systemd service...${NC}"

    cat > /tmp/anubis.service << EOF
[Unit]
Description=AnubisWatch - The Judgment Never Sleeps
After=network.target

[Service]
Type=simple
User=anubis
Group=anubis
ExecStart=${INSTALL_DIR}/anubis serve --config ${CONFIG_DIR}/anubis.json
Restart=always
RestartSec=5
WorkingDirectory=${DATA_DIR}

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${DATA_DIR}

# Resource limits
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

    sudo mv /tmp/anubis.service "$service_file"
    sudo systemctl daemon-reload

    echo -e "${GREEN}✓ Created systemd service${NC}"
    echo -e "${BLUE}  Enable: sudo systemctl enable anubis${NC}"
    echo -e "${BLUE}  Start:  sudo systemctl start anubis${NC}"
}

verify_installation() {
    echo -e "${BLUE}Verifying installation...${NC}"

    if command -v anubis &> /dev/null; then
        local version=$(anubis version 2>/dev/null || echo "unknown")
        echo -e "${GREEN}✓ AnubisWatch installed: ${version}${NC}"
    else
        echo -e "${RED}✗ Installation verification failed${NC}"
        return 1
    fi
}

print_next_steps() {
    echo ''
    echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║              AnubisWatch Installation Complete              ║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ''
    echo -e "${BLUE}Next steps:${NC}"
    echo ''
    echo -e "  1. ${YELLOW}Edit configuration:${NC}"
    echo -e "     sudo nano ${CONFIG_DIR}/anubis.json"
    echo ''
    echo -e "  2. ${YELLOW}Add your first monitor:${NC}"
    echo -e "     anubis watch https://example.com --name 'Example API'"
    echo ''
    echo -e "  3. ${YELLOW}Start the server:${NC}"
    echo -e "     sudo systemctl start anubis"
    echo -e "     # or: anubis serve"
    echo ''
    echo -e "  4. ${YELLOW}Access the dashboard:${NC}"
    echo -e "     http://localhost:8080"
    echo ''
    echo -e "  5. ${YELLOW}View documentation:${NC}"
    echo -e "     https://docs.anubis.watch"
    echo ''
    echo -e "${BLUE}For help:${NC} anubis --help"
    echo -e "${GREEN}The Judgment Never Sleeps ⚖️${NC}"
}

# Main
main() {
    print_banner

    # Detect system
    local os=$(detect_os)
    local arch=$(detect_arch)

    check_prerequisites "$os" "$arch"

    # Get version
    local version=$(get_latest_version)
    echo -e "${BLUE}Version: ${version}${NC}"

    # Create temp directory
    local tmpdir=$(mktemp -d)
    trap "rm -rf $tmpdir" EXIT

    # Download and install
    download_binary "$version" "$os" "$arch" "$tmpdir"
    install_binary "$tmpdir"
    create_directories
    create_config
    create_systemd_service
    verify_installation

    # Cleanup
    rm -rf "$tmpdir"

    print_next_steps
}

# Run main
main "$@"
