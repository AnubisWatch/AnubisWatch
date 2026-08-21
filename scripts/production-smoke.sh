#!/usr/bin/env bash
# Production smoke checks for a deployed AnubisWatch instance.

set -euo pipefail

usage() {
    cat <<'USAGE'
Usage:
  scripts/production-smoke.sh [BASE_URL]

Environment:
  ANUBIS_BASE_URL              Base URL when not passed as an argument.
  ANUBIS_SMOKE_EMAIL           Optional admin email for authenticated checks.
  ANUBIS_SMOKE_PASSWORD        Optional admin password for authenticated checks.
  ANUBIS_SMOKE_INSECURE=true   Pass -k to curl for non-public test certs.
  ANUBIS_SMOKE_NAMESPACE       Optional Kubernetes namespace for rollout checks.
  ANUBIS_SMOKE_WORKLOAD        Optional workload, default statefulset/anubiswatch.

Examples:
  scripts/production-smoke.sh https://anubiswatch.example.com
  ANUBIS_SMOKE_EMAIL=admin@example.com ANUBIS_SMOKE_PASSWORD='...' \
    scripts/production-smoke.sh https://anubiswatch.example.com
USAGE
}

BASE_URL="${1:-${ANUBIS_BASE_URL:-}}"
if [[ -z "$BASE_URL" || "$BASE_URL" == "-h" || "$BASE_URL" == "--help" ]]; then
    usage
    [[ -z "$BASE_URL" ]] && exit 1 || exit 0
fi

BASE_URL="${BASE_URL%/}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

CURL_FLAGS=(-fsS --connect-timeout 5 --max-time 20)
if [[ "${ANUBIS_SMOKE_INSECURE:-false}" == "true" ]]; then
    CURL_FLAGS+=(-k)
fi

log() {
    printf '[smoke] %s\n' "$*"
}

fail() {
    printf '[smoke] FAIL: %s\n' "$*" >&2
    exit 1
}

expect_status() {
    local method="$1"
    local path="$2"
    local expected="$3"
    local label="$4"
    shift 4
    local body_file="$TMP_DIR/body"
    local status

    status="$(curl "${CURL_FLAGS[@]}" -X "$method" -o "$body_file" -w '%{http_code}' "$BASE_URL$path")" || {
        cat "$body_file" >&2 2>/dev/null || true
        fail "$label request failed"
    }

    if [[ "$status" != "$expected" ]]; then
        cat "$body_file" >&2 2>/dev/null || true
        fail "$label returned HTTP $status, expected $expected"
    fi

    local pattern
    for pattern in "$@"; do
        if ! grep -Eq "$pattern" "$body_file"; then
            cat "$body_file" >&2 2>/dev/null || true
            fail "$label response did not match expected content: $pattern"
        fi
    done

    log "ok: $label ($method $path -> $status)"
}

expect_authenticated_status() {
    local cookie_jar="$1"
    local path="$2"
    local expected="$3"
    local label="$4"
    shift 4
    local body_file="$TMP_DIR/body-auth"
    local status

    status="$(curl "${CURL_FLAGS[@]}" \
        --cookie "$cookie_jar" \
        -o "$body_file" \
        -w '%{http_code}' \
        "$BASE_URL$path")" || {
        cat "$body_file" >&2 2>/dev/null || true
        fail "$label request failed"
    }

    if [[ "$status" != "$expected" ]]; then
        cat "$body_file" >&2 2>/dev/null || true
        fail "$label returned HTTP $status, expected $expected"
    fi

    local pattern
    for pattern in "$@"; do
        if ! grep -Eq "$pattern" "$body_file"; then
            cat "$body_file" >&2 2>/dev/null || true
            fail "$label response did not match expected content: $pattern"
        fi
    done

    log "ok: $label (GET $path -> $status)"
}

json_escape() {
    local value="$1"
    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    value="${value//$'\n'/\\n}"
    value="${value//$'\r'/\\r}"
    value="${value//$'\t'/\\t}"
    printf '%s' "$value"
}

run_kubernetes_check() {
    local namespace="${ANUBIS_SMOKE_NAMESPACE:-}"
    local workload="${ANUBIS_SMOKE_WORKLOAD:-statefulset/anubiswatch}"

    if [[ -z "$namespace" ]]; then
        return
    fi
    if ! command -v kubectl >/dev/null 2>&1; then
        fail "ANUBIS_SMOKE_NAMESPACE is set but kubectl is not available"
    fi

    log "checking Kubernetes rollout: $workload in namespace $namespace"
    kubectl -n "$namespace" rollout status "$workload" --timeout=120s
}

run_public_checks() {
    expect_status GET /health 200 "health endpoint" '"status"[[:space:]]*:[[:space:]]*"healthy"'
    expect_status GET /ready 200 "readiness endpoint" \
        '"status"[[:space:]]*:[[:space:]]*"ready"' \
        '"checks"[[:space:]]*:'
    expect_status GET /metrics 200 "metrics endpoint" 'anubis_build_info'
    expect_status GET /api/openapi.json 200 "OpenAPI document" \
        '"openapi"[[:space:]]*:[[:space:]]*"3\.[0-9]+\.[0-9]+"' \
        '"/api/v1/auth/login"[[:space:]]*:'
    expect_status GET / 200 "dashboard shell" '<(html|!doctype html)'
}

run_authenticated_checks() {
    local email="${ANUBIS_SMOKE_EMAIL:-}"
    local password="${ANUBIS_SMOKE_PASSWORD:-}"
    local login_body="$TMP_DIR/login.json"
    local login_response="$TMP_DIR/login-response.json"
    local cookie_jar="$TMP_DIR/cookies.txt"
    local status

    if [[ -z "$email" && -z "$password" ]]; then
        log "skipping authenticated checks; ANUBIS_SMOKE_EMAIL and ANUBIS_SMOKE_PASSWORD are not set"
        return
    fi
    if [[ -z "$email" || -z "$password" ]]; then
        fail "set both ANUBIS_SMOKE_EMAIL and ANUBIS_SMOKE_PASSWORD for authenticated checks"
    fi

    printf '{"email":"%s","password":"%s"}' "$(json_escape "$email")" "$(json_escape "$password")" > "$login_body"
    status="$(curl "${CURL_FLAGS[@]}" \
        -H 'Content-Type: application/json' \
        -X POST \
        --data-binary "@$login_body" \
        --cookie-jar "$cookie_jar" \
        -o "$login_response" \
        -w '%{http_code}' \
        "$BASE_URL/api/v1/auth/login")" || {
        cat "$login_response" >&2 2>/dev/null || true
        fail "login request failed"
    }

    if [[ "$status" != "200" ]]; then
        cat "$login_response" >&2 2>/dev/null || true
        fail "login returned HTTP $status, expected 200"
    fi

    if ! grep -Eq '"user"[[:space:]]*:' "$login_response"; then
        cat "$login_response" >&2 2>/dev/null || true
        fail "login succeeded but the response did not contain a user"
    fi
    if ! awk '$6 == "auth_token" && length($7) > 0 { found = 1 } END { exit !found }' "$cookie_jar"; then
        fail "login succeeded but no auth_token session cookie was issued"
    fi

    log "ok: authenticated login"
    expect_authenticated_status "$cookie_jar" /api/v1/auth/me 200 "current user endpoint" \
        '"email"[[:space:]]*:' \
        '"workspace"[[:space:]]*:'
    expect_authenticated_status "$cookie_jar" '/api/v1/souls?offset=0&limit=1' 200 "souls list endpoint" \
        '"data"[[:space:]]*:' \
        '"pagination"[[:space:]]*:'
    expect_authenticated_status "$cookie_jar" /api/v1/stats/overview 200 "stats overview endpoint" \
        '"souls"[[:space:]]*:' \
        '"judgments"[[:space:]]*:'
}

if [[ "$BASE_URL" != http://* && "$BASE_URL" != https://* ]]; then
    fail "BASE_URL must start with http:// or https://"
fi

if [[ "$BASE_URL" == https://* && "${ANUBIS_SMOKE_INSECURE:-false}" != "true" ]]; then
    log "TLS certificate validation is enabled"
fi

run_kubernetes_check
run_public_checks
run_authenticated_checks

log "all smoke checks passed for $BASE_URL"
