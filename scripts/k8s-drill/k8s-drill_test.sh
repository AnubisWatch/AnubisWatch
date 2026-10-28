#!/usr/bin/env bash
# Offline tests for the Kubernetes acceptance drill runner.
#
# These tests never contact a cluster. They exercise the parts of the runner
# that decide what the evidence document says: the scenario result recorder,
# the dispatch wrapper, the results assembler and the argument parser.
#
# Usage:
#   scripts/k8s-drill/k8s-drill_test.sh
#
# Exit status is 0 when every check passed.

set -euo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

TEST_FAILURES=0

check() {
    local label="$1" result="$2" detail="${3:-}"
    if [[ "$result" == "ok" ]]; then
        printf 'ok   %s%s\n' "$label" "${detail:+ - $detail}"
    else
        printf 'FAIL %s%s\n' "$label" "${detail:+ - $detail}"
        TEST_FAILURES=$((TEST_FAILURES + 1))
    fi
}

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/anubis-drill-test.XXXXXX")"
trap 'rm -rf "$WORK_DIR"' EXIT

# --------------------------------------------------------------------------
# Load the runner. Sourcing it defines the functions without running them.
# --------------------------------------------------------------------------

# shellcheck source=scripts/k8s-drill/k8s-drill.sh
source "$SCRIPT_DIR/k8s-drill.sh"

OUTPUT_DIR="$WORK_DIR/out"
STATE_DIR="$OUTPUT_DIR/state"
TMP_DIR="$OUTPUT_DIR/tmp"
EVIDENCE_DIR="$OUTPUT_DIR/evidence"
LEDGER_FILE="$STATE_DIR/ledger.txt"
RESULTS_FILE="$STATE_DIR/scenario-results.frag"
TLS_DIR="$STATE_DIR/certs"
mkdir -p "$STATE_DIR" "$TMP_DIR" "$EVIDENCE_DIR"
: >"$LEDGER_FILE"
: >"$RESULTS_FILE"

# --------------------------------------------------------------------------
# Test 1: the scenario recorder writes a valid result fragment.
# --------------------------------------------------------------------------

CURRENT_SCENARIO=1
scenario_begin "a passing scenario" >/dev/null
scenario_check "first check" ok "a detail"
scenario_check "second check" ok ""
scenario_end passed "" >/dev/null

fragment_count="$(jq -s 'length' "$RESULTS_FILE" 2>/dev/null || printf '0')"
if [[ "$fragment_count" == "1" ]]; then
    check "the recorder writes one fragment per scenario" ok
else
    check "the recorder writes one fragment per scenario" failed "found $fragment_count fragments"
fi

if jq -e -s '.[0].checks | type == "object"' "$RESULTS_FILE" >/dev/null 2>&1; then
    check "the recorded checks are a valid JSON object" ok
else
    check "the recorded checks are a valid JSON object" failed
fi

if jq -e -s '.[0].checks["first check"].status == "passed"' "$RESULTS_FILE" >/dev/null 2>&1; then
    check "a passing check is recorded as passed" ok
else
    check "a passing check is recorded as passed" failed
fi

if jq -e -s '.[0].checks | has("second check")' "$RESULTS_FILE" >/dev/null 2>&1; then
    check "every check of a scenario is recorded" ok
else
    check "every check of a scenario is recorded" failed
fi

# --------------------------------------------------------------------------
# Test 2: a scenario that fails without recording a result is still reported.
# --------------------------------------------------------------------------

scenario_test_no_record() {
    return 1
}

SCENARIO_FUNCS[1]="scenario_test_no_record"
CURRENT_SCENARIO=2
CURRENT_SCENARIO_NAME="ha-topology"
if run_scenario 2 >/dev/null 2>&1; then
    run_rc=0
else
    run_rc=$?
fi

if [[ "$run_rc" -ne 0 ]]; then
    check "a failing scenario returns non-zero" ok
else
    check "a failing scenario returns non-zero" failed "returned 0"
fi

if jq -e -s 'map(select(.id == 2)) | length == 1' "$RESULTS_FILE" >/dev/null 2>&1; then
    check "a scenario that recorded no result is still reported" ok
else
    check "a scenario that recorded no result is still reported" failed
fi

# --------------------------------------------------------------------------
# Test 3: the assembler completes the document for scenarios that never ran.
# --------------------------------------------------------------------------

SELECTED_SCENARIOS="1 2 3"
CURRENT_SCENARIO=""
reconcile_results

if jq -e -s 'length == 3' "$RESULTS_FILE" >/dev/null 2>&1; then
    check "every selected scenario appears exactly once" ok
else
    check "every selected scenario appears exactly once" failed "found $(jq -s 'length' "$RESULTS_FILE" 2>/dev/null || printf '?')"
fi

if jq -e -s 'map(select(.status == "not run")) | length == 1' "$RESULTS_FILE" >/dev/null 2>&1; then
    check "a scenario that never ran is reported as not run" ok
else
    check "a scenario that never ran is reported as not run" failed
fi

# --------------------------------------------------------------------------
# Test 4: the results document has the documented shape.
# --------------------------------------------------------------------------

write_results "failed"

if jq -e . "$OUTPUT_DIR/results.json" >/dev/null 2>&1; then
    check "the results document is valid JSON" ok
else
    check "the results document is valid JSON" failed
fi

if jq -e '
    .drill == "anubiswatch-kubernetes-acceptance" and
    (.status | type == "string") and
    (.source | type == "object") and
    (.source.image | type == "string") and
    (.source.commit | type == "string") and
    (.cluster | type == "object") and
    (.cluster.context | type == "string") and
    (.scenarios | type == "array")
' "$OUTPUT_DIR/results.json" >/dev/null 2>&1; then
    check "the results document carries the drill, source, cluster and scenarios sections" ok
else
    check "the results document carries the drill, source, cluster and scenarios sections" failed
fi

if jq -e '.scenarios | length == 3' "$OUTPUT_DIR/results.json" >/dev/null 2>&1; then
    check "the results document carries every selected scenario" ok
else
    check "the results document carries every selected scenario" failed
fi

failed_count="$(jq '[.scenarios[] | select(.status != "passed")] | length' "$OUTPUT_DIR/results.json" 2>/dev/null || printf '0')"
if [[ "$failed_count" == "2" ]]; then
    check "the failure count matches the recorded statuses" ok
else
    check "the failure count matches the recorded statuses" failed "counted $failed_count"
fi

# --------------------------------------------------------------------------
# Test 5: the argument parser refuses an ambient cluster.
#
# parse_args exits the shell when it refuses an argument set, so every call
# runs in a subshell: only the subshell may die.
# --------------------------------------------------------------------------

printf 'apiVersion: v1\nkind: Config\n' >"$WORK_DIR/kubeconfig"

if ( parse_args --kubeconfig "$WORK_DIR/kubeconfig" --context drill --image repo/name:1.0.0 ) >/dev/null 2>&1; then
    check "a complete argument set is accepted" ok
else
    check "a complete argument set is accepted" failed
fi

if ( parse_args --context drill --image repo/name:1.0.0 ) >/dev/null 2>&1; then
    check "the runner refuses to start without a kubeconfig" failed "it accepted the arguments"
else
    check "the runner refuses to start without a kubeconfig" ok
fi

if ( parse_args --kubeconfig "$WORK_DIR/kubeconfig" --image repo/name:1.0.0 ) >/dev/null 2>&1; then
    check "the runner refuses to start without a context" failed "it accepted the arguments"
else
    check "the runner refuses to start without a context" ok
fi

if ( parse_args --kubeconfig "$WORK_DIR/kubeconfig" --context drill --image repo/name:latest ) >/dev/null 2>&1; then
    check "the runner refuses the latest tag" failed "it accepted the arguments"
else
    check "the runner refuses the latest tag" ok
fi

if ( parse_args --kubeconfig "$WORK_DIR/kubeconfig" --context drill --image repo/name ) >/dev/null 2>&1; then
    check "the runner refuses an untagged image" failed "it accepted the arguments"
else
    check "the runner refuses an untagged image" ok
fi

SELECTED_SCENARIOS=""
resolve_scenarios "2-4,7"
if [[ "$SELECTED_SCENARIOS" == "2 3 4 7" ]]; then
    check "scenario ranges and lists resolve" ok
else
    check "scenario ranges and lists resolve" failed "resolved to '$SELECTED_SCENARIOS'"
fi

resolve_scenarios "quorum-loss,raft-mtls"
if [[ "$SELECTED_SCENARIOS" == "5 7" ]]; then
    check "scenario names resolve" ok
else
    check "scenario names resolve" failed "resolved to '$SELECTED_SCENARIOS'"
fi

if ( resolve_scenarios "99" ) >/dev/null 2>&1; then
    check "an out of range scenario is refused" failed "it accepted the argument"
else
    check "an out of range scenario is refused" ok
fi

# --------------------------------------------------------------------------

if bash "$SCRIPT_DIR/ownership_paths_test.sh"; then
    check "namespace ownership and output paths" ok
else
    check "namespace ownership and output paths" failed
fi

if bash "$SCRIPT_DIR/image_output_test.sh"; then
    check "immutable images and exclusive evidence directory" ok
else
    check "immutable images and exclusive evidence directory" failed
fi

if bash "$SCRIPT_DIR/source_test.sh"; then
    check "checkout source provenance" ok
else
    check "checkout source provenance" failed
fi

if bash "$SCRIPT_DIR/results_failure_test.sh"; then
    check "result integrity and interrupted scenario evidence" ok
else
    check "result integrity and interrupted scenario evidence" failed
fi

if bash "$SCRIPT_DIR/runtime_test.sh"; then
    check "observed runtime image evidence" ok
else
    check "observed runtime image evidence" failed
fi

if bash "$SCRIPT_DIR/cleanup_test.sh"; then
    check "cleanup results and ownership ledger failures" ok
else
    check "cleanup results and ownership ledger failures" failed
fi

if bash "$SCRIPT_DIR/replacement_readiness_test.sh"; then
    check "ready replacement observed after stale rollout completion" ok
else
    check "ready replacement observed after stale rollout completion" failed
fi

if bash "$SCRIPT_DIR/mtls_identity_test.sh"; then
    check "server identity on mutual TLS endpoints" ok
else
    check "server identity on mutual TLS endpoints" failed
fi

printf '\n%s\n' "offline tests: $TEST_FAILURES failure(s)"
if [[ "$TEST_FAILURES" -gt 0 ]]; then
    exit 1
fi
exit 0
