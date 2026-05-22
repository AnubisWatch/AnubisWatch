#!/bin/bash
# AnubisWatch - One Click Run Script
# The Judgment Never Sleeps

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Defaults
DATA_DIR="${ANUBIS_DATA_DIR:-$HOME/.anubiswatch}"
PORT="${ANUBIS_PORT:-8443}"
ADMIN_PASSWORD="${ANUBIS_ADMIN_PASSWORD:-}"
LOG_LEVEL="${ANUBIS_LOG_LEVEL:-info}"

# Binary name
BINARY="./anubis"

print_banner() {
    echo -e "${BLUE}"
    echo "⚖️  AnubisWatch — The Judgment Never Sleeps"
    echo -e "${NC}"
}

print_status() {
    echo -e "${GREEN}[✓]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[!]${NC} $1"
}

print_error() {
    echo -e "${RED}[✗]${NC} $1"
}

# Check if binary exists, build if not
check_binary() {
    if [[ ! -f "$BINARY" ]]; then
        print_warning "Binary not found, building..."
        go build -o anubis ./cmd/anubis/
        print_status "Binary built: $BINARY"
    else
        print_status "Binary found: $BINARY"
    fi
}

# Create config file
create_config() {
    local config_file="$DATA_DIR/anubis.json"
    
    # Ensure data directory exists
    mkdir -p "$DATA_DIR"
    
    # Generate random admin password if not set
    if [[ -z "$ADMIN_PASSWORD" ]]; then
        ADMIN_PASSWORD=$(openssl rand -base64 24 2>/dev/null || head -c 24 /dev/urandom | base64)
    fi
    
    cat > "$config_file" << EOF
{
  "server": {
    "host": "0.0.0.0",
    "port": $PORT
  },
  "storage": {
    "path": "$DATA_DIR"
  },
  "auth": {
    "enabled": true,
    "type": "local",
    "local": {
      "admin_email": "admin@anubis.local",
      "admin_password": "$ADMIN_PASSWORD"
    }
  },
  "dashboard": {
    "enabled": true
  },
  "logging": {
    "level": "$LOG_LEVEL",
    "format": "json"
  }
}
EOF
    
    print_status "Config created: $config_file"
    echo ""
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${YELLOW}Admin Password:${NC} $ADMIN_PASSWORD"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    
    # Save password to file
    echo "$ADMIN_PASSWORD" > "$DATA_DIR/.admin_password"
}

# Clean up old processes
cleanup() {
    local running=$(pgrep -f "anubis serve" 2>/dev/null | wc -l)
    if [[ $running -gt 0 ]]; then
        print_warning "Stopping $running existing AnubisWatch instance(s)..."
        pkill -f "anubis serve" 2>/dev/null || true
        sleep 2
    fi
}

# Wait for server to be ready
wait_for_server() {
    local max_attempts=30
    local attempt=1
    
    echo -n "Waiting for server"
    while [[ $attempt -le $max_attempts ]]; do
        if curl -s --max-time 2 http://localhost:$PORT/health >/dev/null 2>&1; then
            echo ""
            print_status "Server is ready!"
            return 0
        fi
        echo -n "."
        sleep 1
        ((attempt++))
    done
    
    echo ""
    print_error "Server failed to start within ${max_attempts}s"
    return 1
}

# Run the server
run_server() {
    print_status "Starting AnubisWatch..."
    print_status "  Port: $PORT"
    print_status "  Data: $DATA_DIR"
    print_status "  Logs: $LOG_LEVEL"
    echo ""
    
    # Export variables
    export ANUBIS_DATA_DIR="$DATA_DIR"
    export ANUBIS_LOG_LEVEL="$LOG_LEVEL"
    
    # Run in background
    "$BINARY" serve --config "$DATA_DIR/anubis.json" &
    SERVER_PID=$!
    
    # Wait for server to be ready
    if wait_for_server; then
        echo ""
        echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo -e "${GREEN}  ⚖️  AnubisWatch is ready!${NC}"
        echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo ""
        echo -e "  Dashboard: ${CYAN}http://localhost:$PORT${NC}"
        echo -e "  Health:    ${CYAN}http://localhost:$PORT/health${NC}"
        echo ""
        echo -e "  Press ${YELLOW}Ctrl+C${NC} to stop"
        echo ""
        
        # Wait for server (or interrupt)
        trap "cleanup_and_exit" INT TERM
        wait $SERVER_PID
    else
        print_error "Failed to start server"
        kill $SERVER_PID 2>/dev/null || true
        return 1
    fi
}

cleanup_and_exit() {
    echo ""
    print_warning "Shutting down..."
    pkill -f "anubis serve" 2>/dev/null || true
    sleep 1
    print_status "AnubisWatch stopped"
    exit 0
}

# Show help
show_help() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --port PORT         Set server port (default: 8443)"
    echo "  --data-dir DIR      Set data directory (default: ~/.anubiswatch)"
    echo "  --password PASS     Set admin password"
    echo "  --log-level LEVEL   Set log level: debug, info, warn, error (default: info)"
    echo "  --help              Show this help"
    echo ""
    echo "Environment Variables:"
    echo "  ANUBIS_PORT              Server port"
    echo "  ANUBIS_DATA_DIR          Data directory"
    echo "  ANUBIS_ADMIN_PASSWORD    Admin password"
    echo "  ANUBIS_LOG_LEVEL         Log level"
}

# Parse arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --port)
                PORT="$2"
                shift 2
                ;;
            --data-dir)
                DATA_DIR="$2"
                shift 2
                ;;
            --password)
                ADMIN_PASSWORD="$2"
                shift 2
                ;;
            --log-level)
                LOG_LEVEL="$2"
                shift 2
                ;;
            --help|-h)
                show_help
                exit 0
                ;;
            *)
                print_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
}

# Main
main() {
    print_banner
    
    parse_args "$@"
    
    check_binary
    create_config
    cleanup
    run_server
}

main "$@"