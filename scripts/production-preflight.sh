#!/usr/bin/env bash
# Production preflight checks for an AnubisWatch Helm deployment.

set -euo pipefail

usage() {
    cat <<'USAGE'
Usage:
  scripts/production-preflight.sh

Environment:
  NAMESPACE                         Kubernetes namespace, default anubiswatch.
  RELEASE                           Helm release, default anubiswatch.
  CHART                             Helm chart path, default deploy/helm/anubiswatch.
  VALUES                            Production Helm values file. Required.
  ANUBIS_PREFLIGHT_RENDERED         Rendered manifest path, default /tmp/anubiswatch-rendered.yaml.
  ANUBIS_PREFLIGHT_CREATE_NAMESPACE Create namespace when missing, default false.
  ANUBIS_PREFLIGHT_SKIP_CLUSTER     Run Helm-only checks without kubectl, default false.
  ANUBIS_PREFLIGHT_TIMEOUT          Kubernetes server dry-run timeout, default 120s.

Examples:
  VALUES=values-production.yaml scripts/production-preflight.sh

  NAMESPACE=anubiswatch RELEASE=anubiswatch \
  VALUES=values-production.yaml \
  ANUBIS_PREFLIGHT_CREATE_NAMESPACE=true \
    scripts/production-preflight.sh
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage
    exit 0
fi

NAMESPACE="${NAMESPACE:-anubiswatch}"
RELEASE="${RELEASE:-anubiswatch}"
CHART="${CHART:-deploy/helm/anubiswatch}"
VALUES="${VALUES:-${ANUBIS_PREFLIGHT_VALUES:-}}"
RENDERED="${ANUBIS_PREFLIGHT_RENDERED:-/tmp/anubiswatch-rendered.yaml}"
CREATE_NAMESPACE="${ANUBIS_PREFLIGHT_CREATE_NAMESPACE:-false}"
SKIP_CLUSTER="${ANUBIS_PREFLIGHT_SKIP_CLUSTER:-false}"
TIMEOUT="${ANUBIS_PREFLIGHT_TIMEOUT:-120s}"

log() {
    printf '[preflight] %s\n' "$*"
}

fail() {
    printf '[preflight] FAIL: %s\n' "$*" >&2
    exit 1
}

require_cmd() {
    local command="$1"
    command -v "$command" >/dev/null 2>&1 || fail "$command is required"
}

run() {
    log "running: $*"
    "$@"
}

require_file() {
    local path="$1"
    [[ -f "$path" ]] || fail "file not found: $path"
}

require_dir() {
    local path="$1"
    [[ -d "$path" ]] || fail "directory not found: $path"
}

if [[ -z "$VALUES" ]]; then
    fail "VALUES or ANUBIS_PREFLIGHT_VALUES must point to the production values file"
fi

require_cmd helm
require_dir "$CHART"
require_file "$VALUES"

if [[ "$SKIP_CLUSTER" != "true" ]]; then
    require_cmd kubectl
fi

mkdir -p "$(dirname "$RENDERED")"

if [[ "$SKIP_CLUSTER" != "true" ]]; then
    run kubectl config current-context
    if ! kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
        if [[ "$CREATE_NAMESPACE" == "true" ]]; then
            run kubectl create namespace "$NAMESPACE"
        else
            fail "namespace $NAMESPACE does not exist; set ANUBIS_PREFLIGHT_CREATE_NAMESPACE=true to create it"
        fi
    fi
else
    log "skipping Kubernetes cluster checks"
fi

run helm lint "$CHART" -f "$VALUES"
log "rendering Helm chart to $RENDERED"
helm template "$RELEASE" "$CHART" \
    --namespace "$NAMESPACE" \
    -f "$VALUES" >"$RENDERED"

if [[ ! -s "$RENDERED" ]]; then
    fail "rendered manifest is empty: $RENDERED"
fi

if [[ "$SKIP_CLUSTER" != "true" ]]; then
    run kubectl apply --dry-run=server "--request-timeout=$TIMEOUT" -f "$RENDERED"
else
    log "skipping Kubernetes server dry-run"
fi

log "preflight passed"
log "rendered manifest: $RENDERED"
