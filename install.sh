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
VERSION="${VERSION:-latest}"

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

get_latest_version() {
    if command -v curl &> /dev/null; then
        curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | \
            grep '"tag_name":' | \
            sed -E 's/.*"([^"]+)".*/\1/'
    else
        wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | \
            grep '"tag_name":' | \
            sed -E 's/.*"([^"]+)".*/\1/'
    fi
}

# verify_checksum checks the archive against the release's checksums.txt.
# Set ANUBIS_SKIP_CHECKSUM=1 to bypass (not recommended).
verify_checksum() {
    local dir="$1" file="$2"
    local sums_url="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

    if [ "${ANUBIS_SKIP_CHECKSUM:-0}" = "1" ]; then
        log_warn "Skipping checksum verification (ANUBIS_SKIP_CHECKSUM=1)"
        return 0
    fi

    if command -v curl &> /dev/null; then
        curl -fsSL "$sums_url" -o "${dir}/checksums.txt" || true
    else
        wget -q "$sums_url" -O "${dir}/checksums.txt" || true
    fi
    if [ ! -s "${dir}/checksums.txt" ]; then
        log_error "Could not download checksums.txt for ${VERSION}; set ANUBIS_SKIP_CHECKSUM=1 to install anyway"
        return 1
    fi

    local expected actual
    expected="$(awk -v f="$file" '$2 == f || $2 == "*"f {print $1}' "${dir}/checksums.txt")"
    if [ -z "$expected" ]; then
        log_error "No checksum listed for ${file}"
        return 1
    fi
    if command -v sha256sum &> /dev/null; then
        actual="$(sha256sum "${dir}/${file}" | awk '{print $1}')"
    elif command -v shasum &> /dev/null; then
        actual="$(shasum -a 256 "${dir}/${file}" | awk '{print $1}')"
    else
        log_error "sha256sum or shasum required to verify the download"
        return 1
    fi
    if [ "$expected" != "$actual" ]; then
        log_error "Checksum mismatch for ${file} (expected ${expected}, got ${actual})"
        return 1
    fi
    log_info "Checksum verified"
}

download_binary() {
    if [ "$VERSION" = "latest" ]; then
        VERSION="$(get_latest_version)"
    fi

    local binary_name="anubis-${PLATFORM}-${ARCH}"
    local filename="${binary_name}.tar.gz"
    local url="https://github.com/${REPO}/releases/download/${VERSION}/${filename}"
    local tmpdir
    tmpdir=$(mktemp -d)

    log_info "Downloading ${VERSION}..."
    if command -v curl &> /dev/null; then
        curl -fsSL "$url" -o "${tmpdir}/${filename}" || {
            log_error "Download failed: $url"
            rm -rf "$tmpdir"
            return 1
        }
    else
        wget -q "$url" -O "${tmpdir}/${filename}" || {
            log_error "Download failed: $url"
            rm -rf "$tmpdir"
            return 1
        }
    fi

    verify_checksum "$tmpdir" "$filename" || {
        rm -rf "$tmpdir"
        return 1
    }

    tar -xzf "${tmpdir}/${filename}" -C "$tmpdir"
    
    if [ -w "$INSTALL_DIR" ]; then
        mv "${tmpdir}/${binary_name}" "${INSTALL_DIR}/anubis"
        chmod +x "${INSTALL_DIR}/anubis"
    else
        sudo mv "${tmpdir}/${binary_name}" "${INSTALL_DIR}/anubis"
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
