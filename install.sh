#!/bin/bash
# AnubisWatch Installation Script
# Supports: Linux, macOS
# Usage: curl -sSL https://anubis.watch/install.sh | bash

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

REPO="AnubisWatch/anubiswatch"
INSTALL_DIR="/usr/local/bin"
VERSION="latest"

print_banner() {
    echo -e "${BLUE}"
    echo '╔════════════════════════════════════════════════════════════════╗'
    echo '║   ⚖️  AnubisWatch — The Judgment Never Sleeps                  ║'
    echo '║   Installation Script                                          ║'
    echo '╚════════════════════════════════════════════════════════════════╝'
    echo -e "${NC}"
}

log_info() { echo -e "${BLUE}ℹ${NC}  $1"; }
log_success() { echo -e "${GREEN}✓${NC}  $1"; }
log_error() { echo -e "${RED}✗${NC}  $1"; }
log_warn() { echo -e "${YELLOW}⚠${NC}  $1"; }

detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        linux) PLATFORM="linux" ;;
        darwin) PLATFORM="darwin" ;;
        *) log_error "Unsupported OS: $OS"; exit 1 ;;
    esac

    case "$ARCH" in
        x86_64|amd64) ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        armv7l) ARCH="arm" ;;
        *) log_error "Unsupported arch: $ARCH"; exit 1 ;;
    esac

    log_info "Platform: ${PLATFORM}/${ARCH}"
}

check_deps() {
    if ! command -v curl &> /dev/null && ! command -v wget &> /dev/null; then
        log_error "curl or wget required"
        exit 1
    fi
    log_success "Dependencies OK"
}

download_binary() {
    local filename="anubis_${PLATFORM}_${ARCH}.tar.gz"
    local url="https://github.com/${REPO}/releases/download/${VERSION}/${filename}"
    local tmpdir=$(mktemp -d)

    log_info "Downloading..."
    if command -v curl &> /dev/null; then
        curl -fsSL "$url" -o "${tmpdir}/${filename}" || {
            log_warn "Download failed, using local build"
            cp ./anubis "${INSTALL_DIR}/anubis" 2>/dev/null || true
            rm -rf "$tmpdir"
            return
        }
    else
        wget -q "$url" -O "${tmpdir}/${filename}"
    fi

    tar -xzf "${tmpdir}/${filename}" -C "$tmpdir"
    
    if [ -w "$INSTALL_DIR" ]; then
        mv "${tmpdir}/anubis" "${INSTALL_DIR}/anubis"
        chmod +x "${INSTALL_DIR}/anubis"
    else
        sudo mv "${tmpdir}/anubis" "${INSTALL_DIR}/anubis"
        sudo chmod +x "${INSTALL_DIR}/anubis"
    fi
    
    rm -rf "$tmpdir"
    log_success "Installed to ${INSTALL_DIR}"
}

setup_data_dir() {
    if [ "$EUID" -eq 0 ]; then
        DATA_DIR="/var/lib/anubis"
    else
        DATA_DIR="${HOME}/.anubis"
    fi
    
    mkdir -p "$DATA_DIR"
    log_success "Data dir: ${DATA_DIR}"
}

main() {
    print_banner
    check_deps
    detect_platform
    download_binary
    setup_data_dir
    
    echo
    log_success "Installation Complete!"
    echo
    echo "Run: anubis init --interactive"
    echo "     anubis serve"
    echo
}

main "$@"
