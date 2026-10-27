#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/k8s-drill.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
SELECTED_SCENARIOS=1
IMAGE=repo/name:review
KUBECONFIG="$work/explicit-kubeconfig"
CONTEXT=disposable
fixture='{"items":[{"metadata":{"name":"observed-pod","uid":"observed-uid"},"spec":{"nodeName":"worker-a","containers":[{"name":"anubis","image":"repo/name:review","env":[{"name":"PASSWORD","value":"must-not-be-published"}]}]},"status":{"containerStatuses":[{"name":"anubis","imageID":"containerd://sha256:observed"}]}}]}'
mode=ok
kubectl() {
    [[ " $* " == *" --kubeconfig $KUBECONFIG --context disposable "* ]] || return 1
    [[ " $* " == *' --namespace isolated --request-timeout=15s get pods -l app.kubernetes.io/instance=release -o json '* ]] || return 1
    [[ ! -f "$OUTPUT_DIR/cleaned" ]] || return 1
    case "$mode" in
        ok) printf '%s' "$fixture" ;;
        denied) return 1 ;;
        empty) printf '{"items":[]}' ;;
        no-id) printf '%s' "$fixture" | jq 'del(.items[0].status.containerStatuses[0].imageID)' ;;
        no-uid) printf '%s' "$fixture" | jq 'del(.items[0].metadata.uid)' ;;
        malformed) printf '{invalid' ;;
    esac
}
drill_cleanup() { printf 'cleaned\n' >"$OUTPUT_DIR/cleaned"; }
for mode in ok denied empty no-id no-uid malformed; do
    OUTPUT_DIR="$work/$mode"
    resolve_output_paths
    mkdir -p "$STATE_DIR"
    printf 'helm isolated release\n' >"$LEDGER_FILE"
    printf '{"id":1,"name":"control","status":"passed","checks":{},"evidence":[]}\n' >"$RESULTS_FILE"
    rc=0
    (trap on_exit EXIT; exit 0) >"$work/$mode.log" 2>&1 || rc=$?
    [[ -f "$OUTPUT_DIR/cleaned" ]]
    if [[ "$mode" == ok ]]; then
        [[ "$rc" == 0 ]]
        jq -e '.status == "passed" and .runtime_images.status == "available" and .runtime_images.pods[0].uid == "observed-uid" and .runtime_images.pods[0].containers[0].image_id == "containerd://sha256:observed"' "$OUTPUT_DIR/results.json" >/dev/null
        ! grep -R -q 'must-not-be-published' "$OUTPUT_DIR"
    else
        [[ "$rc" != 0 ]]
        jq -e '.status == "failed" and .runtime_images.status == "unavailable"' "$OUTPUT_DIR/results.json" >/dev/null
    fi
done
# Multiple owned namespaces and a sidecar are retained, terminating pods ignored.
OUTPUT_DIR="$work/multiple"
resolve_output_paths
mkdir -p "$STATE_DIR"
printf 'helm isolated release\nhelm second other\n' >"$LEDGER_FILE"
fixture=$(jq '.items[0].spec.containers += [{"name":"sidecar","image":"side:review"}] | .items[0].status.containerStatuses += [{"name":"sidecar","imageID":"cri://side-id"}] | .items += [{"metadata":{"deletionTimestamp":"2026-01-01"}}]' <<<"$fixture")
kubectl() { printf '%s' "$fixture"; }
capture_runtime_images
jq -e '.status == "available" and (.pods | length == 2) and (.pods[0].containers | length == 2) and .pods[1].namespace == "second"' "$STATE_DIR/runtime-images.json" >/dev/null
: >"$LEDGER_FILE"
capture_runtime_images
jq -e '.status == "not_observed" and .pods == []' "$STATE_DIR/runtime-images.json" >/dev/null
echo 'Runtime image tests: PASS (observed IDs, multiple releases, sidecar, errors, redaction, pre-cleanup ordering)'
echo 'FIX VERIFIED'
