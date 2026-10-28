#!/usr/bin/env bash
# Exercise the real EXIT trap, including corrupt evidence and interrupted work.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/k8s-drill.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
SELECTED_SCENARIOS=1
passed='{"id":1,"name":"control","status":"passed","checks":{},"evidence":[]}'
drill_cleanup() { printf 'cleaned\n' >"$OUTPUT_DIR/cleaned"; }
run_case() {
    local label="$1" fragment="$2" body_rc="$3" expected_rc="$4"
    OUTPUT_DIR="$work/$label"
    resolve_output_paths
    mkdir -p "$STATE_DIR"
    printf '%s\n' "$fragment" >"$RESULTS_FILE"
    : >"$LEDGER_FILE"
    local rc=0
    (trap on_exit EXIT; exit "$body_rc") >"$work/$label.log" 2>&1 || rc=$?
    [[ "$rc" == "$expected_rc" && -f "$OUTPUT_DIR/cleaned" ]] || { cat "$work/$label.log"; exit 1; }
    if [[ "$expected_rc" != 0 ]]; then
        ! jq -e '.status == "passed"' "$OUTPUT_DIR/results.json" >/dev/null 2>&1
    else
        jq -e '.status == "passed" and (.scenarios | length == 1)' "$OUTPUT_DIR/results.json" >/dev/null
    fi
}
run_case valid "$passed" 0 0
run_case corrupt '{invalid' 0 1
run_case duplicate "$passed
$passed" 0 1
run_case invalid-status '{"id":1,"name":"bad","status":"bogus","checks":{}}' 0 1
run_case contradiction '{"id":1,"name":"bad","status":"passed","checks":{"step":{"status":"failed"}}}' 0 1
run_case failed '{"id":1,"name":"bad","status":"failed","checks":{}}' 0 1
run_case missing '' 0 1
run_case preserve-exit "$passed" 23 23

CURRENT_SCENARIO=1
OUTPUT_DIR="$work/interrupted"
resolve_output_paths
mkdir -p "$STATE_DIR"
: >"$RESULTS_FILE"
: >"$LEDGER_FILE"
scenario_begin interrupted >/dev/null
scenario_check 'completed before interrupt' ok 'durable detail' >/dev/null
rc=0
(trap on_exit EXIT; exit 130) >"$work/interrupted.log" 2>&1 || rc=$?
[[ "$rc" == 130 && -f "$OUTPUT_DIR/cleaned" ]]
jq -e '.status == "failed" and (.scenarios | length == 1) and .scenarios[0].checks["completed before interrupt"].detail == "durable detail"' "$OUTPUT_DIR/results.json" >/dev/null
CURRENT_SCENARIO=""
# An actual publication failure cannot escape the trap as exit 0.
mv() { return 1; }
run_case cannot-publish "$passed" 0 1
unset -f mv

echo 'Result integrity tests: PASS (corrupt, duplicate, contradictory, missing, failed, interrupted, publish error)'
echo 'FIX VERIFIED'
