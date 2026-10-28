#!/usr/bin/env bash
# Launch one standalone instance; existing configuration and other processes are preserved.
set -euo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY="${ANUBIS_BINARY:-$SCRIPT_DIR/anubis}"
DATA_DIR="${ANUBIS_DATA_DIR:-$HOME/.anubiswatch}"
PORT="${ANUBIS_PORT:-8443}"
ADMIN_PASSWORD="${ANUBIS_ADMIN_PASSWORD:-}"
LOG_LEVEL="${ANUBIS_LOG_LEVEL:-info}"

usage() {
    cat <<'USAGE'
Usage: ./run.sh [OPTIONS]
  --port PORT        HTTP port (default: ANUBIS_PORT or 8443)
  --data-dir DIR     Config/data directory (default: ~/.anubiswatch)
  --password PASS    Initial admin password; environment input is preferred
  --log-level LEVEL debug, info, warn or error (default: info)
  --help            Show help

Environment: ANUBIS_BINARY, ANUBIS_DATA_DIR, ANUBIS_PORT,
ANUBIS_ADMIN_PASSWORD, ANUBIS_LOG_LEVEL and ANUBIS_HOST.
Existing config is preserved. Password changes use anubis admin reset-password.
The launched process runs in the foreground; Ctrl+C stops only this instance.
USAGE
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --help|-h) usage; exit 0 ;;
        --port|--data-dir|--password|--log-level)
            if [[ $# -lt 2 || "$2" == --* ]]; then
                printf 'Missing value for %s\n' "$1" >&2
                exit 2
            fi
            case "$1" in
                --port) PORT="$2" ;;
                --data-dir) DATA_DIR="$2" ;;
                --password) ADMIN_PASSWORD="$2" ;;
                --log-level) LOG_LEVEL="$2" ;;
            esac
            shift 2 ;;
        *) printf 'Unknown option: %s\n' "$1" >&2; usage >&2; exit 2 ;;
    esac
done

if ! [[ "$PORT" =~ ^[0-9]{1,5}$ ]] || (( 10#$PORT < 1 || 10#$PORT > 65535 )); then
    printf 'Port must be an integer from 1 to 65535\n' >&2
    exit 2
fi
case "$LOG_LEVEL" in debug|info|warn|error) ;; *) printf 'Invalid log level\n' >&2; exit 2 ;; esac
if [[ ! -x "$BINARY" ]]; then
    if [[ "$BINARY" != "$SCRIPT_DIR/anubis" ]]; then
        printf 'ANUBIS_BINARY is not executable: %s\n' "$BINARY" >&2
        exit 1
    fi
    (cd "$SCRIPT_DIR" && go build -o "$BINARY" ./cmd/anubis)
fi
mkdir -p "$DATA_DIR"
DATA_DIR="$(cd "$DATA_DIR" && pwd)"
CONFIG="$DATA_DIR/anubis.json"
export ANUBIS_DATA_DIR="$DATA_DIR" ANUBIS_PORT="$PORT" ANUBIS_LOG_LEVEL="$LOG_LEVEL"
if [[ -n "$ADMIN_PASSWORD" ]]; then export ANUBIS_ADMIN_PASSWORD="$ADMIN_PASSWORD"; fi
if [[ ! -e "$CONFIG" ]]; then
    "$BINARY" init --output "$CONFIG"
fi
printf 'Starting standalone AnubisWatch on port %s with config %s\n' "$PORT" "$CONFIG"
exec "$BINARY" serve --single --config "$CONFIG"
