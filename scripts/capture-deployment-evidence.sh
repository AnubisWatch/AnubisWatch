#!/usr/bin/env bash
# Capture production deployment evidence without copying secret-bearing values.

set -euo pipefail

usage() {
    cat <<'USAGE'
Usage:
  scripts/capture-deployment-evidence.sh [OUTPUT_DIR]

Environment:
  NAMESPACE                       Kubernetes namespace, default anubiswatch.
  RELEASE                         Helm release, default anubiswatch.
  ANUBIS_EVIDENCE_WORKLOAD        Workload to inspect, default statefulset/anubiswatch.
  ANUBIS_EVIDENCE_VALUES          Optional values file to checksum only.
  ANUBIS_EVIDENCE_IMAGE           Optional expected image tag or digest to record.
  ANUBIS_EVIDENCE_BASE_URL        Optional URL to run production smoke checks against.
  ANUBIS_EVIDENCE_RUN_SMOKE=true  Run smoke checks when ANUBIS_EVIDENCE_BASE_URL is set.
  ANUBIS_EVIDENCE_ROLLOUT_TIMEOUT Rollout timeout, default 120s.

Smoke test environment is forwarded to scripts/production-smoke.sh:
  ANUBIS_SMOKE_EMAIL, ANUBIS_SMOKE_PASSWORD, ANUBIS_SMOKE_INSECURE,
  ANUBIS_SMOKE_NAMESPACE, ANUBIS_SMOKE_WORKLOAD

Examples:
  NAMESPACE=anubiswatch RELEASE=anubiswatch \
    scripts/capture-deployment-evidence.sh

  ANUBIS_EVIDENCE_VALUES=values-production.yaml \
  ANUBIS_EVIDENCE_BASE_URL=https://anubiswatch.example.com \
  ANUBIS_EVIDENCE_RUN_SMOKE=true \
    scripts/capture-deployment-evidence.sh evidence/prod-20260509
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage
    exit 0
fi

NAMESPACE="${NAMESPACE:-anubiswatch}"
RELEASE="${RELEASE:-anubiswatch}"
WORKLOAD="${ANUBIS_EVIDENCE_WORKLOAD:-statefulset/anubiswatch}"
VALUES_FILE="${ANUBIS_EVIDENCE_VALUES:-}"
IMAGE_REF="${ANUBIS_EVIDENCE_IMAGE:-}"
BASE_URL="${ANUBIS_EVIDENCE_BASE_URL:-}"
RUN_SMOKE="${ANUBIS_EVIDENCE_RUN_SMOKE:-false}"
ROLLOUT_TIMEOUT="${ANUBIS_EVIDENCE_ROLLOUT_TIMEOUT:-120s}"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUTPUT_DIR="${1:-deployment-evidence/$TIMESTAMP}"

log() {
    printf '[evidence] %s\n' "$*"
}

warn() {
    printf '[evidence] WARN: %s\n' "$*" >&2
}

capture_cmd() {
    local name="$1"
    shift
    local output="$OUTPUT_DIR/$name.txt"

    {
        printf '$'
        printf ' %q' "$@"
        printf '\n\n'
        "$@"
    } >"$output" 2>&1 || {
        local status=$?
        printf '\n[command exited with status %s]\n' "$status" >>"$output"
        warn "$name failed with status $status; see $output"
        return 0
    }

    log "captured $name"
}

capture_shell() {
    local name="$1"
    local command="$2"
    local output="$OUTPUT_DIR/$name.txt"

    {
        printf '$ %s\n\n' "$command"
        bash -o pipefail -c "$command"
    } >"$output" 2>&1 || {
        local status=$?
        printf '\n[command exited with status %s]\n' "$status" >>"$output"
        warn "$name failed with status $status; see $output"
        return 0
    }

    log "captured $name"
}

require_cmd() {
    local command="$1"
    if ! command -v "$command" >/dev/null 2>&1; then
        warn "$command is not available; Kubernetes/Helm evidence may be incomplete"
        return 1
    fi
}

mkdir -p "$OUTPUT_DIR"

{
    printf 'timestamp_utc=%s\n' "$TIMESTAMP"
    printf 'namespace=%s\n' "$NAMESPACE"
    printf 'release=%s\n' "$RELEASE"
    printf 'workload=%s\n' "$WORKLOAD"
    printf 'rollout_timeout=%s\n' "$ROLLOUT_TIMEOUT"
    printf 'base_url=%s\n' "${BASE_URL:-not-set}"
    printf 'expected_image=%s\n' "${IMAGE_REF:-not-set}"
    printf 'git_branch=%s\n' "$(git branch --show-current 2>/dev/null || printf 'unknown')"
    printf 'git_commit=%s\n' "$(git rev-parse HEAD 2>/dev/null || printf 'unknown')"
    printf 'git_status_short=%s\n' "$(git status --short 2>/dev/null | wc -l | tr -d ' ')"
} >"$OUTPUT_DIR/metadata.env"
log "wrote metadata"

if [[ -n "$VALUES_FILE" ]]; then
    if [[ -f "$VALUES_FILE" ]]; then
        if command -v sha256sum >/dev/null 2>&1; then
            sha256sum "$VALUES_FILE" >"$OUTPUT_DIR/values.sha256"
        else
            shasum -a 256 "$VALUES_FILE" >"$OUTPUT_DIR/values.sha256"
        fi
        log "captured values checksum for $VALUES_FILE"
    else
        warn "ANUBIS_EVIDENCE_VALUES points to a missing file: $VALUES_FILE"
    fi
fi

if require_cmd kubectl; then
    capture_cmd kubectl-context kubectl config current-context
    capture_cmd rollout-status kubectl -n "$NAMESPACE" rollout status "$WORKLOAD" "--timeout=$ROLLOUT_TIMEOUT"
    capture_cmd pods kubectl -n "$NAMESPACE" get pods -o wide
    capture_cmd services kubectl -n "$NAMESPACE" get svc -o wide
    capture_cmd ingress kubectl -n "$NAMESPACE" get ingress -o wide
    capture_cmd workload-yaml kubectl -n "$NAMESPACE" get "$WORKLOAD" -o yaml
    capture_shell events "kubectl -n $(printf '%q' "$NAMESPACE") get events --sort-by=.lastTimestamp"
fi

if require_cmd helm; then
    capture_cmd helm-history helm history "$RELEASE" --namespace "$NAMESPACE"
    capture_cmd helm-status helm status "$RELEASE" --namespace "$NAMESPACE"
fi

if [[ "$RUN_SMOKE" == "true" ]]; then
    if [[ -z "$BASE_URL" ]]; then
        warn "ANUBIS_EVIDENCE_RUN_SMOKE=true but ANUBIS_EVIDENCE_BASE_URL is not set"
    elif [[ -x scripts/production-smoke.sh ]]; then
        ANUBIS_SMOKE_NAMESPACE="${ANUBIS_SMOKE_NAMESPACE:-$NAMESPACE}" \
        ANUBIS_SMOKE_WORKLOAD="${ANUBIS_SMOKE_WORKLOAD:-$WORKLOAD}" \
            scripts/production-smoke.sh "$BASE_URL" >"$OUTPUT_DIR/smoke.txt" 2>&1 || {
                status=$?
                printf '\n[smoke exited with status %s]\n' "$status" >>"$OUTPUT_DIR/smoke.txt"
                warn "smoke checks failed with status $status; see $OUTPUT_DIR/smoke.txt"
            }
        log "captured smoke result"
    else
        warn "scripts/production-smoke.sh is not executable"
    fi
fi

cat >"$OUTPUT_DIR/README.md" <<README
# AnubisWatch Deployment Evidence

- Timestamp: \`$TIMESTAMP\`
- Namespace: \`$NAMESPACE\`
- Release: \`$RELEASE\`
- Workload: \`$WORKLOAD\`
- Git commit: \`$(git rev-parse HEAD 2>/dev/null || printf 'unknown')\`
- Expected image: \`${IMAGE_REF:-not-set}\`
- Base URL: \`${BASE_URL:-not-set}\`

This directory contains command output captured during deployment validation.
It intentionally records only the checksum of the values file, not the values
file contents, because production values may include secrets.
README

log "evidence written to $OUTPUT_DIR"
