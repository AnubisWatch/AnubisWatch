#!/usr/bin/env bash
# Exercise a built/downloaded AnubisWatch binary as a running service.

set -euo pipefail

usage() {
    cat <<'USAGE'
Usage:
  scripts/downloaded-binary-smoke.sh BINARY

Environment:
  ANUBIS_INTEGRATION_PORT      HTTP port, default 18080.
  ANUBIS_INTEGRATION_EMAIL     Admin email, default admin@anubis.watch.
  ANUBIS_INTEGRATION_PASSWORD  Admin password used only for this ephemeral run.
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage
    exit 0
fi
if [[ $# -ne 1 ]]; then
    usage >&2
    exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY="$1"
PORT="${ANUBIS_INTEGRATION_PORT:-18080}"
EMAIL="${ANUBIS_INTEGRATION_EMAIL:-admin@anubis.watch}"
PASSWORD="${ANUBIS_INTEGRATION_PASSWORD:-CI-Smoke-Password-47!}"
TMP_DIR="$(mktemp -d)"
CONFIG="$TMP_DIR/anubis.json"
DATA_DIR="$TMP_DIR/data"
SERVER_LOG="$TMP_DIR/server.log"
SERVER_PID=""

cleanup() {
    if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

fail() {
    printf '[binary-smoke] FAIL: %s\n' "$*" >&2
    if [[ -s "$SERVER_LOG" ]]; then
        printf '%s\n' '----- server log -----' >&2
        cat "$SERVER_LOG" >&2
    fi
    exit 1
}

if [[ ! -x "$BINARY" ]]; then
    fail "binary is not executable: $BINARY"
fi
if ! [[ "$PORT" =~ ^[0-9]+$ ]] || (( PORT < 1 || PORT > 65535 )); then
    fail "ANUBIS_INTEGRATION_PORT must be an integer from 1 to 65535"
fi
if ! command -v curl >/dev/null 2>&1; then
    fail "curl is required"
fi

"$BINARY" version
ANUBIS_DATA_DIR="$DATA_DIR" "$BINARY" init --output "$CONFIG" >"$TMP_DIR/init.log"

ANUBIS_CONFIG="$CONFIG" \
ANUBIS_DATA_DIR="$DATA_DIR" \
ANUBIS_HOST=127.0.0.1 \
ANUBIS_PORT="$PORT" \
ANUBIS_ADMIN_PASSWORD="$PASSWORD" \
    "$BINARY" serve --config "$CONFIG" --single >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

BASE_URL="http://127.0.0.1:$PORT"
ready=false
for _ in $(seq 1 100); do
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        wait "$SERVER_PID" || true
        fail "server exited before becoming healthy"
    fi
    if curl -fsS --connect-timeout 1 --max-time 2 "$BASE_URL/health" >/dev/null 2>&1; then
        ready=true
        break
    fi
    sleep 0.1
done
if [[ "$ready" != "true" ]]; then
    fail "server did not become healthy within 10 seconds"
fi

ANUBIS_SMOKE_EMAIL="$EMAIL" \
ANUBIS_SMOKE_PASSWORD="$PASSWORD" \
    "$SCRIPT_DIR/production-smoke.sh" "$BASE_URL"

if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    wait "$SERVER_PID" || true
    fail "server exited during smoke checks"
fi

printf '[binary-smoke] downloaded binary smoke passed\n'
