#!/usr/bin/env bash
# Offline deletion/ledger error handling and explicit completion ordering.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/k8s-drill.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
SELECTED_SCENARIOS=1
pf_stop_all() { [[ "$mode" != pf-denied ]]; }
kubectl() {
    if [[ " $* " == *' get pods '* ]]; then
        printf '%s' '{"items":[{"metadata":{"name":"pod","uid":"uid"},"spec":{"containers":[{"name":"anubis","image":"repo/name:review"}]},"status":{"containerStatuses":[{"name":"anubis","imageID":"cri://id"}]}}]}'
        return 0
    fi
    printf '%s\n' "$*" >>"$OUTPUT_DIR/deletes"
    [[ " $* " == *' --request-timeout=15s '* ]] || return 1
    if [[ " $* " == *' delete namespace '* ]]; then
        [[ " $* " == *' --wait=true '* ]] || return 1
    fi
    [[ "$mode" != api-denied ]]
}
helm_cmd() {
    printf 'helm %s\n' "$*" >>"$OUTPUT_DIR/deletes"
    [[ "$mode" != helm-denied ]]
}
setup() {
    OUTPUT_DIR="$work/$1"
    resolve_output_paths
    mkdir -p "$STATE_DIR"
    printf 'namespace owned\nsecret cert\nhelm owned release\ncordon worker\n' >"$LEDGER_FILE"
    printf '{"id":1,"name":"control","status":"passed","checks":{},"evidence":[]}\n' >"$RESULTS_FILE"
}
for mode in ok api-denied helm-denied pf-denied; do
    setup "$mode"
    rc=0
    (trap on_exit EXIT; exit 0) >"$work/$mode.log" 2>&1 || rc=$?
    [[ $(wc -l <"$OUTPUT_DIR/deletes") == 4 ]]
    if [[ "$mode" == ok ]]; then
        [[ "$rc" == 0 ]]
        jq -e '.status == "passed" and .cleanup.status == "completed" and .cleanup.failed_operations == 0' "$OUTPUT_DIR/results.json" >/dev/null
    else
        [[ "$rc" != 0 ]]
        jq -e '.status == "failed" and .cleanup.status == "failed" and .cleanup.failed_operations > 0' "$OUTPUT_DIR/results.json" >/dev/null
    fi
done
mode=ok
KEEP_ON_FAIL=1
setup keep
rc=0
(trap on_exit EXIT; exit 7) >"$work/keep.log" 2>&1 || rc=$?
[[ "$rc" == 7 && ! -f "$OUTPUT_DIR/deletes" ]]
jq -e '.cleanup.status == "kept" and .status == "failed"' "$OUTPUT_DIR/results.json" >/dev/null
KEEP_ON_FAIL=0

# Do not publish public success while cleanup is still in flight. A ledger
# replacement after the snapshot cannot grant authority over a foreign name.
setup gated
printf 'namespace owned-a\nnamespace owned-b\n' >"$LEDGER_FILE"
mkfifo "$work/ready" "$work/release"
kubectl() {
    printf '%s\n' "$*" >>"$OUTPUT_DIR/deletes"
    if [[ " $* " == *' namespace owned-b '* ]]; then
        printf 'ready\n' >"$work/ready"
        read -r release <"$work/release"
    fi
}
(trap on_exit EXIT; exit 0) >"$work/gated.log" 2>&1 &
pid=$!
read -r ready <"$work/ready"
[[ ! -f "$OUTPUT_DIR/results.json" ]]
printf 'namespace foreign\n' >"$LEDGER_FILE"
printf 'go\n' >"$work/release"
wait "$pid"
! grep -q foreign "$OUTPUT_DIR/deletes"
[[ $(wc -l <"$OUTPUT_DIR/deletes") == 2 ]]
jq -e '.status == "passed" and .cleanup.status == "completed"' "$OUTPUT_DIR/results.json" >/dev/null

# Missing, unreadable and malformed authority never means an empty ledger.
kubectl() { printf 'unsafe\n' >>"$OUTPUT_DIR/deletes"; return 1; }
for failure in missing directory malformed malformed-helm; do
    setup "ledger-$failure"
    case "$failure" in
        missing) rm "$LEDGER_FILE" ;;
        directory) rm "$LEDGER_FILE"; mkdir "$LEDGER_FILE" ;;
        malformed) printf 'namespace partial\nunknown foreign\n' >"$LEDGER_FILE" ;;
        malformed-helm) printf 'helm incomplete\n' >"$LEDGER_FILE" ;;
    esac
    if capture_runtime_images >"$work/read.log" 2>&1; then exit 1; fi
    jq -e '.status == "unavailable"' "$STATE_DIR/runtime-images.json" >/dev/null
    if drill_cleanup >"$work/cleanup.log" 2>&1; then exit 1; fi
    [[ ! -f "$OUTPUT_DIR/deletes" ]]
    rc=0
    (trap on_exit EXIT; exit 0) >"$work/invalid-exit.log" 2>&1 || rc=$?
    [[ "$rc" != 0 && ! -f "$OUTPUT_DIR/deletes" ]]
    jq -e '.status == "failed" and .cleanup.status == "failed"' "$OUTPUT_DIR/results.json" >/dev/null
done
setup empty
: >"$LEDGER_FILE"
capture_runtime_images
jq -e '.status == "not_observed"' "$STATE_DIR/runtime-images.json" >/dev/null
drill_cleanup
[[ ! -f "$OUTPUT_DIR/deletes" ]]
setup io-error
awk() { return 1; }
if capture_runtime_images >/dev/null 2>&1; then exit 1; fi
if drill_cleanup >/dev/null 2>&1; then exit 1; fi
[[ ! -f "$OUTPUT_DIR/deletes" ]]
unset -f awk

echo 'Cleanup tests: PASS (API/Helm/PF errors, remaining attempts, keep, gated publication/snapshot, ledger failures)'
echo 'FIX VERIFIED'
