#!/usr/bin/env bash
# Kubernetes acceptance drill runner for AnubisWatch.
#
# The runner deploys a disposable standalone release and a disposable
# three-voter cluster release into an explicitly named namespace, drives the
# acceptance scenarios in scripts/k8s-drill/scenarios.sh, and removes every
# resource it created.
#
# It never falls back to the ambient kubeconfig or context. Both are required
# arguments, and every kubectl and helm call names them explicitly, so a drill
# can not be pointed at a production cluster by accident.
#
# Usage:
#   scripts/k8s-drill/k8s-drill.sh --kubeconfig PATH --context NAME --image IMAGE
#
# The drill has been executed against a disposable kind cluster; see
# docs/deployment/k8s-acceptance-drill.md for the current status and
# .temp_files/k8s-drill-live/DRILL-EVIDENCE.md for the run evidence.

set -euo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# --------------------------------------------------------------------------
# Configuration
# --------------------------------------------------------------------------

usage() {
    cat <<'USAGE'
Usage:
  scripts/k8s-drill/k8s-drill.sh --kubeconfig PATH --context NAME --image IMAGE [options]

Required:
  --kubeconfig PATH    Kubeconfig of the target cluster. The ambient KUBECONFIG
                       environment variable is deliberately ignored.
  --context NAME       Context inside that kubeconfig. There is no default and
                       no fallback to the current context of another cluster.
  --image IMAGE        Image under test: REPOSITORY:TAG, REPOSITORY:TAG@DIGEST
                       or REPOSITORY@DIGEST. Cluster mode refuses "latest".

Options:
  --namespace NAME            Namespace for the drill objects (default anubiswatch-drill).
  --standalone-namespace NAME Namespace for the standalone release (default: --namespace).
  --chart PATH                Helm chart directory (default deploy/helm/anubiswatch).
  --output-dir PATH           New evidence directory; must not exist
                              (default .temp_files/k8s-drill-<UTC time>).
  --scenarios LIST            Scenarios to run: all, numbers (1,3,5), a range (2-6),
                              or names. Default: all.
  --timeout DURATION          Rollout timeout for helm and kubectl waits (default 300s).
  --probe-wait SECONDS        Wait for the first judgment of a probe (default 120).
  --leader-failover SECONDS   Wait for a new leader after a failure (default 180).
  --quorum-loss SECONDS       Wait for quorum loss to take effect (default 120).
  --quorum-recovery SECONDS   Wait for quorum to return (default 180).
  --restart-wait SECONDS      Wait for the voters to become ready (default 180).
  --keep-on-fail              Keep the drill objects when the drill fails.
  --dry-run                   Print the resolved plan and exit without touching a cluster.
  --help                      Show this help.

Scenario names:
  1 standalone       install, authenticated smoke, encrypted PVC, rollout, rollback
  2 ha-topology      three voters on distinct workers, one leader, two followers
  3 follower-write   a follower-originated write readable on all replicas
  4 session-rotation one session everywhere, global rotation, logout revocation
  5 raft-mtls        verified server identity, trusted-client RPC, no-cert refusal
  6 probe-failover   probes across a leader failure, judgment preservation
  7 quorum-loss      quorum loss refuses authorization, recovery keeps the data
  8 rolling-restart  rolling restart preserves sessions, config and history

Exit status:
  0  every selected scenario passed
  1  a scenario failed, the preflight failed, or the drill was interrupted
  2  the arguments were invalid
USAGE
}

fail() {
    printf '[drill] FAIL: %s\n' "$*" >&2
    exit 1
}

log() {
    printf '[drill] %s\n' "$*"
}

usage_error() {
    printf '[drill] FAIL: %s\n' "$*" >&2
    printf '\n' >&2
    usage >&2
    exit 2
}

KUBECONFIG=""
CONTEXT=""
NAMESPACE="anubiswatch-drill"
SA_NAMESPACE=""
CHART="$REPO_ROOT/deploy/helm/anubiswatch"
IMAGE=""
OUTPUT_DIR=""
SCENARIO_SELECTION="all"
KEEP_ON_FAIL=0
DRY_RUN=0
TIMEOUT="300s"
PROBE_WAIT_SECONDS=120
LEADER_FAILOVER_SECONDS=180
QUORUM_LOSS_SECONDS=120
QUORUM_RECOVERY_SECONDS=180
RESTART_WAIT_SECONDS=180

# Release names. They are distinct so the chart's "cannot change an existing
# standalone release into a cluster in place" guard never fires, and so the
# standalone Deployment and the cluster StatefulSet have distinct full names.
SA_RELEASE="drill-standalone"
HA_RELEASE="drill-cluster"

# --------------------------------------------------------------------------
# Arguments
# --------------------------------------------------------------------------

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --kubeconfig)
                [[ $# -ge 2 ]] || usage_error "--kubeconfig requires a path"
                KUBECONFIG="$2"
                shift 2
                ;;
            --context)
                [[ $# -ge 2 ]] || usage_error "--context requires a name"
                CONTEXT="$2"
                shift 2
                ;;
            --namespace)
                [[ $# -ge 2 ]] || usage_error "--namespace requires a name"
                NAMESPACE="$2"
                shift 2
                ;;
            --standalone-namespace)
                [[ $# -ge 2 ]] || usage_error "--standalone-namespace requires a name"
                SA_NAMESPACE="$2"
                shift 2
                ;;
            --chart)
                [[ $# -ge 2 ]] || usage_error "--chart requires a path"
                CHART="$2"
                shift 2
                ;;
            --image)
                [[ $# -ge 2 ]] || usage_error "--image requires REPOSITORY:TAG"
                IMAGE="$2"
                shift 2
                ;;
            --output-dir)
                [[ $# -ge 2 ]] || usage_error "--output-dir requires a path"
                OUTPUT_DIR="$2"
                shift 2
                ;;
            --scenarios)
                [[ $# -ge 2 ]] || usage_error "--scenarios requires a list"
                SCENARIO_SELECTION="$2"
                shift 2
                ;;
            --timeout)
                [[ $# -ge 2 ]] || usage_error "--timeout requires a duration"
                TIMEOUT="$2"
                shift 2
                ;;
            --probe-wait)
                [[ $# -ge 2 ]] || usage_error "--probe-wait requires seconds"
                PROBE_WAIT_SECONDS="$2"
                shift 2
                ;;
            --leader-failover)
                [[ $# -ge 2 ]] || usage_error "--leader-failover requires seconds"
                LEADER_FAILOVER_SECONDS="$2"
                shift 2
                ;;
            --quorum-loss)
                [[ $# -ge 2 ]] || usage_error "--quorum-loss requires seconds"
                QUORUM_LOSS_SECONDS="$2"
                shift 2
                ;;
            --quorum-recovery)
                [[ $# -ge 2 ]] || usage_error "--quorum-recovery requires seconds"
                QUORUM_RECOVERY_SECONDS="$2"
                shift 2
                ;;
            --restart-wait)
                [[ $# -ge 2 ]] || usage_error "--restart-wait requires seconds"
                RESTART_WAIT_SECONDS="$2"
                shift 2
                ;;
            --keep-on-fail)
                KEEP_ON_FAIL=1
                shift
                ;;
            --dry-run)
                DRY_RUN=1
                shift
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                usage_error "unknown argument: $1"
                ;;
        esac
    done

    # The standalone release lives in its own namespace by default only when the
    # caller asks for it; otherwise it shares the drill namespace.
    [[ -n "$SA_NAMESPACE" ]] || SA_NAMESPACE="$NAMESPACE"

    if [[ -z "$KUBECONFIG" ]]; then
        usage_error "--kubeconfig is required; the drill never uses the ambient KUBECONFIG"
    fi
    if [[ -z "$CONTEXT" ]]; then
        usage_error "--context is required; the drill never uses the current context of another cluster"
    fi
    if [[ -z "$IMAGE" ]] && [[ "$DRY_RUN" -eq 0 ]]; then
        usage_error "--image is required"
    fi
    if [[ ! -d "$CHART" ]]; then
        fail "chart directory not found: $CHART"
    fi
    if [[ ! -f "$KUBECONFIG" ]]; then
        if [[ "$DRY_RUN" -eq 1 ]]; then
            drill_warn "kubeconfig not found: $KUBECONFIG (dry run continues)"
        else
            fail "kubeconfig not found: $KUBECONFIG"
        fi
    fi

    # The image identity is parsed here so that every later user of it sees
    # the same validated values.
    if [[ -n "$IMAGE" ]]; then
        parse_image "$IMAGE"
    fi

    # The scenario selection is resolved here for the same reason: it depends
    # on the parsed arguments.
    resolve_scenarios "$SCENARIO_SELECTION"
    resolve_output_paths
}


# --------------------------------------------------------------------------
# Image identity
# --------------------------------------------------------------------------

IMAGE_REPOSITORY=""
IMAGE_TAG=""
IMAGE_DIGEST=""

parse_image() {
    local image="$1" last
    IMAGE_REPOSITORY=""
    IMAGE_TAG=""
    IMAGE_DIGEST=""
    image="${image#docker://}"
    if [[ "$image" == *@* ]]; then
        IMAGE_DIGEST="${image#*@}"
        image="${image%@*}"
        if [[ ! "$IMAGE_DIGEST" =~ ^sha256:[a-f0-9]{64}$ ]]; then
            fail "--image digest must be sha256 followed by 64 lowercase hex characters"
        fi
    fi
    last="${image##*/}"
    if [[ "$last" == *:* ]]; then
        IMAGE_REPOSITORY="${image%:*}"
        IMAGE_TAG="${image##*:}"
        [[ -n "$IMAGE_TAG" ]] || fail "--image has an empty tag"
    else
        [[ -n "$IMAGE_DIGEST" ]] || fail "--image must name an explicit tag or digest"
        IMAGE_REPOSITORY="$image"
    fi
    [[ -n "$IMAGE_REPOSITORY" ]] || fail "--image has an empty repository"
    if [[ -z "$IMAGE_DIGEST" ]] && [[ "$(printf '%s' "$IMAGE_TAG" | tr '[:upper:]' '[:lower:]')" == "latest" ]]; then
        fail "--image must not use the latest tag; cluster mode requires a pinned tag or a digest"
    fi
}

# --------------------------------------------------------------------------
# Scenario selection
# --------------------------------------------------------------------------

SCENARIO_NAMES=(standalone ha-topology follower-write session-rotation raft-mtls probe-failover quorum-loss rolling-restart)
SCENARIO_FUNCS=(scenario_standalone scenario_ha_topology scenario_follower_write scenario_session_rotation scenario_raft_mtls scenario_probe_failover scenario_quorum_loss scenario_rolling_restart)
SELECTED_SCENARIOS=""

scenario_name_for_index() {
    printf '%s' "${SCENARIO_NAMES[$(( $1 - 1 ))]}"
}

resolve_scenarios() {
    local spec="$1" part normalised
    local -A want=()
    local i name from to

    # The selection is rebuilt from scratch on every call.
    SELECTED_SCENARIOS=""

    if [[ "$(printf '%s' "$spec" | tr '[:upper:]' '[:lower:]')" == "all" ]]; then
        for i in 1 2 3 4 5 6 7 8; do
            want[$i]=1
        done
    else
        normalised="$(printf '%s' "$spec" | tr ',' ' ')"
        for part in $normalised; do
            if [[ "$part" =~ ^[0-9]+$ ]]; then
                if [[ "$part" -lt 1 || "$part" -gt 8 ]]; then
                    fail "scenario number out of range: $part"
                fi
                want[$part]=1
            elif [[ "$part" =~ ^[0-9]+-[0-9]+$ ]]; then
                from="${part%-*}"
                to="${part#*-}"
                if [[ "$from" -lt 1 || "$to" -gt 8 || "$from" -gt "$to" ]]; then
                    fail "invalid scenario range: $part"
                fi
                for ((i = from; i <= to; i++)); do
                    want[$i]=1
                done
            else
                local lowered matched=""
                lowered="$(printf '%s' "$part" | tr '[:upper:]' '[:lower:]')"
                for i in 1 2 3 4 5 6 7 8; do
                    name="${SCENARIO_NAMES[$(( i - 1 ))]}"
                    if [[ "$name" == "$lowered" ]]; then
                        matched="$i"
                        break
                    fi
                done
                if [[ -z "$matched" ]]; then
                    fail "unknown scenario: $part"
                fi
                want[$matched]=1
            fi
        done
    fi

    for i in 1 2 3 4 5 6 7 8; do
        if [[ -n "${want[$i]:-}" ]]; then
            SELECTED_SCENARIOS="$SELECTED_SCENARIOS $i"
        fi
    done
    SELECTED_SCENARIOS="${SELECTED_SCENARIOS# }"

    if [[ -z "$SELECTED_SCENARIOS" ]]; then
        fail "no scenarios selected"
    fi
}

resolve_scenarios "$SCENARIO_SELECTION"

# --------------------------------------------------------------------------
# Shared state
# --------------------------------------------------------------------------

if [[ -z "$OUTPUT_DIR" ]]; then
    OUTPUT_DIR="$REPO_ROOT/.temp_files/k8s-drill-$(date -u +%Y%m%dT%H%M%SZ)"
fi

resolve_output_paths() {
    STATE_DIR="$OUTPUT_DIR/state"
    TMP_DIR="$OUTPUT_DIR/tmp"
    EVIDENCE_DIR="$OUTPUT_DIR/evidence"
    LEDGER_FILE="$STATE_DIR/ledger.txt"
    RESULTS_FILE="$STATE_DIR/scenario-results.frag"
    TLS_DIR="$STATE_DIR/certs"
}

resolve_output_paths

prepare_output_directory() {
    # A previous ledger must never become this run's cleanup authority.
    # mkdir is the exclusive ownership boundary, including concurrent runs.
    if ! mkdir -p -- "$(dirname -- "$OUTPUT_DIR")" || ! mkdir -- "$OUTPUT_DIR"; then
        fail "cannot exclusively create evidence directory $OUTPUT_DIR; choose a new output directory"
    fi
}

API_BASE=""
API_STATUS=""
API_BODY=""
TOKEN=""

DRILL_STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
DRILL_FINISHED_AT=""
DRILL_STATUS="failed"
CLEANUP_STATUS="not_started"
CLEANUP_FAILURES=0
SCENARIO_FAILURES=0
CURRENT_SCENARIO=""
CURRENT_SCENARIO_NAME=""
CURRENT_SCENARIO_START=0

SA_FULLNAME=""
SA_ADMIN_EMAIL=""
SA_ADMIN_PASSWORD=""
SA_ENCRYPTION_KEY=""
HA_FULLNAME=""
HA_ADMIN_EMAIL=""
HA_ADMIN_PASSWORD=""
HA_CLUSTER_SECRET=""
HA_ENCRYPTION_KEY=""
RAFT_TLS_SECRET="drill-raft-tls"
RAFT_CLIENT_ORIGIN="drill-client"
API_PORT=8080
RAFT_PORT=7946
CLUSTER_DOMAIN="cluster.local"

# --------------------------------------------------------------------------
# Load the shared helpers
# --------------------------------------------------------------------------

# shellcheck source=scripts/k8s-drill/lib.sh
source "$SCRIPT_DIR/lib.sh"
# shellcheck source=scripts/k8s-drill/source.sh
source "$SCRIPT_DIR/source.sh"
# shellcheck source=scripts/k8s-drill/runtime.sh
source "$SCRIPT_DIR/runtime.sh"
# shellcheck source=scripts/k8s-drill/scenarios.sh
source "$SCRIPT_DIR/scenarios.sh"

# --------------------------------------------------------------------------
# Small helpers
# --------------------------------------------------------------------------

# duration_to_seconds DURATION FALLBACK converts an "Ns" duration to seconds.
duration_to_seconds() {
    local d="$1" fallback="$2"
    if [[ "$d" =~ ^([0-9]+)s$ ]]; then
        printf '%s' "${BASH_REMATCH[1]}"
        return 0
    fi
    printf '%s' "$fallback"
}

# count_fragments counts the scenario result fragments recorded so far.
count_fragments() {
    if [[ -f "$RESULTS_FILE" ]]; then
        grep -c '"id":' "$RESULTS_FILE" 2>/dev/null || true
    else
        printf '0'
    fi
}

# count_failed_scenarios counts the recorded scenarios whose status is not
# "passed".
#
# The result fragments are the single source of truth: the drill status is
# derived from them, so a scenario that failed without recording a result of
# its own is counted as well.
count_failed_scenarios() {
    if [[ ! -f "$RESULTS_FILE" ]]; then
        return 1
    fi
    jq -es '
        if all(.[]; type == "object" and (.id | type == "number") and
            (.checks | type == "object") and
            (.status == "passed" or .status == "failed" or .status == "not run"))
        then [.[] | select(.status != "passed" or any(.checks[]; .status != "passed"))] | length
        else error("invalid scenario record") end
    ' "$RESULTS_FILE" 2>/dev/null
}

# --------------------------------------------------------------------------
# Cleanup
#
# The ledger records every resource this run created. Teardown walks it in
# reverse, so a resource is only removed if this run made it.
# --------------------------------------------------------------------------

drill_cleanup() {
    local kind entry ns rel name target
    local reverse="$STATE_DIR/cleanup-ledger-reversed.txt"
    CLEANUP_FAILURES=0
    pf_stop_all || CLEANUP_FAILURES=$((CLEANUP_FAILURES + 1))

    if ! ledger_snapshot "$STATE_DIR/cleanup-ledger.txt" ||
        ! tac "$STATE_DIR/cleanup-ledger.txt" >"$reverse"; then
        drill_warn "ownership ledger unavailable; refusing destructive cleanup"
        CLEANUP_FAILURES=$((CLEANUP_FAILURES + 1))
        return 1
    fi

    # Keep dependency order while attempting every owned operation, even if
    # an earlier API call failed. Snapshot validation precedes any deletion.
    for target in cordon helm secret namespace; do
        while read -r kind entry; do
            [[ "$kind" == "$target" ]] || continue
            case "$kind" in
                cordon)
                    kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" --request-timeout=15s \
                        uncordon "$entry" >/dev/null 2>>"$STATE_DIR/cleanup-errors.log" ||
                        CLEANUP_FAILURES=$((CLEANUP_FAILURES + 1))
                    ;;
                helm)
                    ns="${entry%% *}"
                    rel="${entry#* }"
                    log "removing release $rel from namespace $ns"
                    helm_cmd uninstall "$rel" --namespace "$ns" --wait --timeout "$TIMEOUT" \
                        >/dev/null 2>>"$STATE_DIR/cleanup-errors.log" ||
                        CLEANUP_FAILURES=$((CLEANUP_FAILURES + 1))
                    ;;
                secret)
                    kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" --request-timeout=15s \
                        --namespace "$NAMESPACE" delete secret "$entry" --ignore-not-found \
                        --timeout="$TIMEOUT" >/dev/null 2>>"$STATE_DIR/cleanup-errors.log" ||
                        CLEANUP_FAILURES=$((CLEANUP_FAILURES + 1))
                    ;;
                namespace)
                    name="$entry"
                    log "removing namespace $name"
                    kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" --request-timeout=15s \
                        delete namespace "$name" --ignore-not-found --wait=true --timeout="$TIMEOUT" \
                        >/dev/null 2>>"$STATE_DIR/cleanup-errors.log" ||
                        CLEANUP_FAILURES=$((CLEANUP_FAILURES + 1))
                    ;;
            esac
        done <"$reverse"
    done
    [[ "$CLEANUP_FAILURES" == 0 ]]
}

# reconcile_results makes sure every selected scenario appears exactly once in
# the results document.
#
# A scenario that returns non-zero without recording its own result, and a
# scenario that never started because the run aborted, both still have to
# show up: an evidence document that silently omits a scenario is worse than
# one that reports it as failed or not run.
reconcile_results() {
    local index recorded status error checks
    mkdir -p "$STATE_DIR" || return 1
    recorded="$(jq -s -r 'map(.id) | join(",")' "$RESULTS_FILE" 2>/dev/null)" || return 1

    for index in $SELECTED_SCENARIOS; do
        if [[ ",$recorded," == *",$index,"* ]]; then
            continue
        fi
        status="not run"
        error="the drill ended before this scenario started"
        checks=""
        if [[ "$index" == "$CURRENT_SCENARIO" ]]; then
            status="failed"
            error="the drill aborted inside this scenario"
            # Keep the checks the scenario managed to record before it died.
            checks="$(cat "$STATE_DIR/checks.json" 2>/dev/null || true)"
            checks="${checks%,}"
            [[ -z "$checks" ]] || checks="{$checks}"
        fi
        if [[ -z "$checks" ]]; then
            checks="{}"
        fi
        jq -n \
            --argjson id "$index" \
            --arg name "$(scenario_name_for_index "$index")" \
            --arg status "$status" \
            --arg error "$error" \
            --argjson checks "$checks" \
            '{
                id: $id,
                name: $name,
                status: $status,
                duration_seconds: 0,
                checks: $checks,
                evidence: [],
                error: $error
            }' >>"$RESULTS_FILE" 2>/dev/null || return 1
    done
}

write_results() {
    local status="$1" chart_version="" chart_name=""
    local publication="${2:-$OUTPUT_DIR/results.json}"
    local fragments="$RESULTS_FILE"
    local collection_failed=0

    mkdir -p "$OUTPUT_DIR" "$STATE_DIR" || return 1
    [[ -f "$fragments" ]] || : >"$fragments" || return 1
    reconcile_results || collection_failed=1
    if ! SCENARIO_FAILURES="$(count_failed_scenarios)"; then
        SCENARIO_FAILURES=0
        collection_failed=1
    fi

    [[ -f "$STATE_DIR/source-identity.json" ]] || capture_source_identity || return 1
    if [[ ! -f "$STATE_DIR/runtime-images.json" ]]; then
        capture_runtime_images || collection_failed=1
    fi
    if ! jq -e '.status == "available" or .status == "not_observed"' "$STATE_DIR/runtime-images.json" >/dev/null 2>&1; then
        collection_failed=1
    fi
    if [[ "$collection_failed" != 0 ]]; then status=failed; fi
    chart_name="$(drill_chart_name 2>/dev/null || printf '')"
    chart_version="$(sed -n 's/^version:[[:space:]]*//p' "$CHART/Chart.yaml" 2>/dev/null | head -1 | tr -d '"'"'"' ')"

    jq -s \
        --arg started_at "$DRILL_STARTED_AT" \
        --arg finished_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --arg kubeconfig "$KUBECONFIG" \
        --arg context "$CONTEXT" \
        --arg namespace "$NAMESPACE" \
        --arg standalone_namespace "$SA_NAMESPACE" \
        --arg chart "$CHART" \
        --arg status "$status" \
        --arg cleanup_status "$CLEANUP_STATUS" \
        --argjson cleanup_failures "$CLEANUP_FAILURES" \
        --arg image "${IMAGE:-}" \
        --slurpfile identity "$STATE_DIR/source-identity.json" \
        --slurpfile runtime "$STATE_DIR/runtime-images.json" \
        --arg chart_name "$chart_name" \
        --arg chart_version "$chart_version" \
        --arg selected "${SELECTED_SCENARIOS# }" \
        '($selected | split(" ") | map(tonumber) | sort) as $expected |
        if ([.[].id] | sort) != $expected then error("missing or duplicate scenario results") else . end |
        if all(.[]; type == "object" and (.checks | type == "object") and
            (.status == "passed" or .status == "failed" or .status == "not run")) and
            ($status != "passed" or all(.[]; .status == "passed" and all(.checks[]; .status == "passed")))
        then . else error("invalid or contradictory scenario results") end |
        {
            drill: "anubiswatch-kubernetes-acceptance",
            status: $status,
            cleanup: {status: $cleanup_status, failed_operations: $cleanup_failures},
            started_at: $started_at,
            finished_at: $finished_at,
            source: ($identity[0] + {
                chart: $chart_name,
                chart_version: $chart_version,
                image: $image
            }),
            runtime_images: $runtime[0],
            cluster: {
                kubeconfig: $kubeconfig,
                context: $context,
                namespace: $namespace,
                standalone_namespace: $standalone_namespace
            },
            selected_scenarios: ($selected | split(" ") | map(tonumber)),
            scenarios: .
        }' "$fragments" >"$STATE_DIR/results.json.tmp" 2>"$STATE_DIR/results-error.log" || {
        printf '{"drill":"anubiswatch-kubernetes-acceptance","status":"failed","evidence_error":"result assembly failed","scenarios":[]}\n' >"$STATE_DIR/results.json.tmp" &&
            mv -f -- "$STATE_DIR/results.json.tmp" "$publication"
        return 1
    }
    mv -f -- "$STATE_DIR/results.json.tmp" "$publication" || return 1

    return "$collection_failed"
}

print_summary() {
    local status="$1"
    printf '\n[drill] === summary ===\n'
    if [[ -f "$OUTPUT_DIR/results.json" ]]; then
        jq -r '.scenarios[]? | "  \(.id). \(.status): \(.name)\(if (.error // "") != "" then " - " + .error else "" end)"' \
            "$OUTPUT_DIR/results.json" 2>/dev/null || true
    fi
    printf '[drill] status: %s (%s scenario(s) failed)\n' "$status" "$SCENARIO_FAILURES"
    printf '[drill] evidence: %s\n' "$OUTPUT_DIR"
    printf '[drill] results: %s\n' "$OUTPUT_DIR/results.json"
}

on_exit() {
    local rc=$?
    trap - EXIT INT TERM
    set +e

    DRILL_FINISHED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    # The results document is the single source of truth for the drill
    # status: every selected scenario is reconciled into it first, the
    # failures are counted from it, and only then is the status derived. A
    # scenario that failed without recording a result of its own must never
    # be reported as a passed drill.
    if ! reconcile_results; then
        drill_warn "scenario evidence reconciliation failed"
        [[ "$rc" != 0 ]] || rc=1
    fi
    if ! SCENARIO_FAILURES="$(count_failed_scenarios)"; then
        drill_warn "scenario evidence is invalid"
        SCENARIO_FAILURES=0
        [[ "$rc" != 0 ]] || rc=1
    fi
    if [[ "$SCENARIO_FAILURES" != 0 ]]; then
        [[ "$rc" != 0 ]] || rc=1
    fi
    if ! capture_runtime_images; then
        drill_warn "runtime image evidence collection failed"
        [[ "$rc" != 0 ]] || rc=1
    fi

    if [[ "$rc" -eq 0 ]] && [[ "$SCENARIO_FAILURES" -eq 0 ]]; then
        DRILL_STATUS="passed"
    else
        DRILL_STATUS="failed"
    fi

    # Validate/stage the report privately before deciding keep-on-fail.
    # A public passing report is published only after cleanup completes.
    if ! write_results "$DRILL_STATUS" "$STATE_DIR/results-before-cleanup.json"; then
        drill_warn "result evidence could not be completed"
        DRILL_STATUS=failed
        [[ "$rc" != 0 ]] || rc=1
    fi

    if [[ "$KEEP_ON_FAIL" -eq 1 && "$DRILL_STATUS" != "passed" ]]; then
        log "keeping the drill objects for inspection (--keep-on-fail)"
        CLEANUP_STATUS=kept
        if ! pf_stop_all; then
            CLEANUP_STATUS=failed
            CLEANUP_FAILURES=$((CLEANUP_FAILURES + 1))
            [[ "$rc" != 0 ]] || rc=1
        fi
    else
        if drill_cleanup; then
            CLEANUP_STATUS=completed
        else
            drill_warn "cleanup incomplete; inspect state/cleanup-errors.log and the ownership ledger"
            CLEANUP_STATUS=failed
            DRILL_STATUS=failed
            [[ "$rc" != 0 ]] || rc=1
        fi
    fi

    if ! write_results "$DRILL_STATUS"; then
        DRILL_STATUS=failed
        [[ "$rc" != 0 ]] || rc=1
    fi

    print_summary "$DRILL_STATUS"
    exit "$rc"
}

on_interrupt() {
    log "interrupted; cleaning up"
    exit 130
}

# --------------------------------------------------------------------------
# Preflight
# --------------------------------------------------------------------------

preflight() {
    drill_require_cmd kubectl
    drill_require_cmd helm
    drill_require_cmd jq
    drill_require_cmd openssl
    drill_require_cmd curl

    if ! kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" \
        cluster-info >"$STATE_DIR/cluster-info.txt" 2>&1; then
        drill_fail "the cluster for context '$CONTEXT' is not reachable; see $STATE_DIR/cluster-info.txt"
    fi
    log "cluster reachable: context $CONTEXT"

    local nodes
    nodes="$(kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" get nodes --no-headers 2>/dev/null | grep -c . || true)"
    log "cluster has $nodes schedulable node(s)"
    if [[ "$nodes" -lt 3 ]]; then
        drill_fail "the drill needs at least 3 schedulable nodes; scenario 2 requires three voters on three distinct workers (found $nodes)"
    fi
}

# --------------------------------------------------------------------------
# Namespace, names, credentials and certificates
# --------------------------------------------------------------------------

prepare_namespace() {
    local ns created_namespace=""
    for ns in "$NAMESPACE" "$SA_NAMESPACE"; do
        # The two releases may share the namespace created in this call.
        if [[ "$ns" == "$created_namespace" ]]; then
            continue
        fi
        # An exclusive create also closes the get/create race. Never run
        # upgrade --install against a namespace containing another release.
        if ! kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" create namespace "$ns" >/dev/null; then
            drill_fail "cannot exclusively create namespace $ns; choose a new disposable namespace"
        fi
        ledger_add "namespace $ns"
        log "created namespace $ns"
        created_namespace="$ns"
    done
}

resolve_names() {
    local chart_name
    chart_name="$(drill_chart_name)"
    HA_FULLNAME="$(drill_fullname "$HA_RELEASE" "$chart_name")"
    SA_FULLNAME="$(drill_fullname "$SA_RELEASE" "$chart_name")"
}

prepare_credentials() {
    # Disposable credentials. They exist only for the duration of the drill
    # and are never written to stdout.
    SA_ADMIN_PASSWORD="$(drill_gen_password)"
    SA_ENCRYPTION_KEY="$(drill_gen_hex 32)"
    HA_ADMIN_PASSWORD="$(drill_gen_password)"
    HA_ENCRYPTION_KEY="$(drill_gen_hex 32)"
    HA_CLUSTER_SECRET="$(drill_gen_hex 32)"

    # The local admin account is seeded from auth.local.admin_email, so the
    # drill logs in with the chart's configured address.
    SA_ADMIN_EMAIL="$(sed -n 's/^[[:space:]]*adminEmail:[[:space:]]*//p' "$CHART/values.yaml" | head -1 | tr -d '"'"'"' ')"
    if [[ -z "$SA_ADMIN_EMAIL" ]]; then
        SA_ADMIN_EMAIL="admin@anubiswatch.local"
    fi
    HA_ADMIN_EMAIL="$SA_ADMIN_EMAIL"
}

prepare_certificates() {
    local san
    san="${HA_FULLNAME}-0.${HA_FULLNAME}-headless.${NAMESPACE}.svc.${CLUSTER_DOMAIN}"
    san="$san ${HA_FULLNAME}-1.${HA_FULLNAME}-headless.${NAMESPACE}.svc.${CLUSTER_DOMAIN}"
    san="$san ${HA_FULLNAME}-2.${HA_FULLNAME}-headless.${NAMESPACE}.svc.${CLUSTER_DOMAIN}"

    # The CA and leaf carry both serverAuth and clientAuth, so the same leaf
    # can serve the voter TLS endpoint and act as the trusted client.
    drill_gen_certs "$TLS_DIR" $san
    chmod 600 "$TLS_DIR"/* 2>/dev/null || true

    kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" \
        --namespace "$NAMESPACE" create secret generic "$RAFT_TLS_SECRET" \
        --from-file=tls.crt="$TLS_DIR/leaf.crt" \
        --from-file=tls.key="$TLS_DIR/leaf.key" \
        --from-file=ca.crt="$TLS_DIR/ca.crt" >/dev/null
    ledger_add "secret $RAFT_TLS_SECRET"
    log "created the drill Raft TLS secret $RAFT_TLS_SECRET"
}

# --------------------------------------------------------------------------
# Releases
# --------------------------------------------------------------------------

write_standalone_values() {
    local values="$STATE_DIR/standalone-values.yaml"
    cat >"$values" <<EOF
image:
  repository: ${IMAGE_REPOSITORY}
  tag: "${IMAGE_TAG}"
  digest: "${IMAGE_DIGEST}"
replicaCount: 1
config:
  necropolis:
    enabled: false
  auth:
    type: local
    local:
      adminEmail: "${SA_ADMIN_EMAIL}"
  storage:
    path: /data
    encryption:
      enabled: true
secrets:
  adminPassword: "${SA_ADMIN_PASSWORD}"
  encryptionKey: "${SA_ENCRYPTION_KEY}"
persistence:
  enabled: true
  size: 1Gi
monitoring:
  enabled: false
autoscaling:
  enabled: false
pdb:
  enabled: false
EOF
    chmod 600 "$values"
    printf '%s' "$values"
}

write_cluster_values() {
    local values="$STATE_DIR/ha-values.yaml"
    cat >"$values" <<EOF
image:
  repository: ${IMAGE_REPOSITORY}
  tag: "${IMAGE_TAG}"
  digest: "${IMAGE_DIGEST}"
config:
  auth:
    type: local
    local:
      adminEmail: "${HA_ADMIN_EMAIL}"
  necropolis:
    enabled: true
  storage:
    path: /data
    encryption:
      enabled: true
cluster:
  domain: ${CLUSTER_DOMAIN}
  raftTLSSecret: ${RAFT_TLS_SECRET}
statefulSet:
  replicas: 3
  podManagementPolicy: Parallel
  updateStrategy:
    type: RollingUpdate
persistence:
  enabled: true
  size: 1Gi
monitoring:
  enabled: false
autoscaling:
  enabled: false
pdb:
  enabled: true
  minAvailable: 2
secrets:
  adminPassword: "${HA_ADMIN_PASSWORD}"
  encryptionKey: "${HA_ENCRYPTION_KEY}"
  clusterSecret: "${HA_CLUSTER_SECRET}"
affinity: {}
EOF
    chmod 600 "$values"
    printf '%s' "$values"
}

# relax_ssrf NAMESPACE KIND NAME
#
# The drill probes an in-cluster service address, which the default SSRF
# policy refuses. The chart exposes no setting for it, so the runner sets the
# documented environment override on the workload it owns. The override is
# OR-ed with the configured policy, so it only widens what the drill may
# reach inside its own namespace.
relax_ssrf() {
    local ns="$1" kind="$2" name="$3"
    if ! kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" --namespace "$ns" \
        set env "$kind/$name" ANUBIS_SSRF_ALLOW_PRIVATE=1 >/dev/null 2>&1; then
        drill_warn "could not relax the SSRF policy on $kind/$name; in-cluster probes may fail"
        return 0
    fi
    log "allowed the drill to probe private addresses on $kind/$name"
    # The override only takes effect after the pods restart with it.
    kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" --namespace "$ns" \
        rollout status "$kind/$name" --timeout="$TIMEOUT" >/dev/null 2>&1 || true
}

install_standalone() {
    local values
    values="$(write_standalone_values)"
    log "installing the standalone release $SA_RELEASE"
    helm_cmd upgrade --install "$SA_RELEASE" "$CHART" \
        --namespace "$SA_NAMESPACE" \
        --values "$values" \
        --wait --timeout "$TIMEOUT" \
        >"$STATE_DIR/standalone-install.log" 2>&1 || {
        drill_fail "the standalone install failed; see $STATE_DIR/standalone-install.log"
    }
    ledger_add "helm $SA_NAMESPACE $SA_RELEASE"
    relax_ssrf "$SA_NAMESPACE" deployment "$SA_FULLNAME"
}

install_cluster() {
    local values
    values="$(write_cluster_values)"
    log "installing the three-voter cluster release $HA_RELEASE"
    helm_cmd upgrade --install "$HA_RELEASE" "$CHART" \
        --namespace "$NAMESPACE" \
        --values "$values" \
        --wait --timeout "$TIMEOUT" \
        >"$STATE_DIR/cluster-install.log" 2>&1 || {
        drill_fail "the cluster install failed; see $STATE_DIR/cluster-install.log"
    }
    ledger_add "helm $NAMESPACE $HA_RELEASE"
    relax_ssrf "$NAMESPACE" statefulset "$HA_FULLNAME"
}

wait_for_voters() {
    local ready waited=0 limit
    limit="$(duration_to_seconds "$TIMEOUT" 300)"
    while [[ "$waited" -lt "$limit" ]]; do
        ready="$(kube_ns get pods -l "app.kubernetes.io/instance=$HA_RELEASE" \
            -o jsonpath='{range .items[?(@.status.phase=="Running")]}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}' 2>/dev/null \
            | grep -c True || true)"
        if [[ "$ready" -eq 3 ]]; then
            log "all three voters are ready"
            return 0
        fi
        sleep 5
        waited=$((waited + 5))
    done
    drill_fail "only $ready of 3 voters became ready within ${limit}s"
}

# --------------------------------------------------------------------------
# Scenario dispatch
# --------------------------------------------------------------------------

run_scenario() {
    local index="$1"
    local func="${SCENARIO_FUNCS[$(( index - 1 ))]}"
    local name="${SCENARIO_NAMES[$(( index - 1 ))]}"
    local before after rc

    CURRENT_SCENARIO="$index"
    CURRENT_SCENARIO_NAME="$name"
    CURRENT_SCENARIO_START="$(drill_epoch)"
    before="$(count_fragments)"

    log "running scenario $index ($name)"
    # The scenario is called from an "if" so errexit stays off for its body: a
    # scenario that returns non-zero must not abort the whole run. The global
    # shell state is deliberately left alone.
    if "$func"; then
        rc=0
    else
        rc=$?
    fi
    after="$(count_fragments)"

    if [[ "$rc" -ne 0 ]] && [[ "$after" -eq "$before" ]]; then
        # The scenario failed without recording a result of its own. The
        # checks it managed to record are kept, and the failure is counted:
        # a scenario that returned non-zero is never a passed scenario.
        drill_warn "scenario $index ($name) failed without recording a result"
        local checks
        checks="$(cat "$STATE_DIR/checks.json" 2>/dev/null || true)"
        jq -n \
            --argjson id "$index" \
            --arg name "$name" \
            --argjson seconds "$(( $(drill_epoch) - CURRENT_SCENARIO_START ))" \
            --arg raw_checks "$checks" \
            '{
                id: $id,
                name: $name,
                status: "failed",
                duration_seconds: $seconds,
                checks: (if ($raw_checks | gsub("\\s"; "") | length) == 0 then {}
                         else (($raw_checks | sub(",$"; "") | ("{" + . + "}")) | fromjson? // {})
                         end),
                evidence: [],
                error: "the scenario returned non-zero without recording a result"
            }' >>"$RESULTS_FILE" 2>/dev/null || true
        SCENARIO_FAILURES=$((SCENARIO_FAILURES + 1))
    fi
    return "$rc"
}

main() {
    # --------------------------------------------------------------------------
    # Main
    # --------------------------------------------------------------------------

    needs_cluster=0
    needs_standalone_install=0
    scenario_one_selected=0
    for index in $SELECTED_SCENARIOS; do
        if [[ "$index" -ge 2 ]]; then
            needs_cluster=1
        fi
        if [[ "$index" -eq 1 ]]; then
            scenario_one_selected=1
        fi
        if [[ "$index" -eq 6 ]]; then
            needs_standalone_install=1
        fi
    done
    # Scenario 1 installs the standalone release itself.
    if [[ "$scenario_one_selected" -eq 1 ]]; then
        needs_standalone_install=0
    fi

    if [[ "$DRY_RUN" -eq 1 ]]; then
        log "dry run: no cluster calls will be made"
        printf '\n[drill] === resolved plan ===\n'
        printf '  kubeconfig         %s\n' "$KUBECONFIG"
        printf '  context            %s\n' "$CONTEXT"
        printf '  namespace          %s\n' "$NAMESPACE"
        printf '  standalone ns      %s\n' "$SA_NAMESPACE"
        printf '  chart              %s\n' "$CHART"
        printf '  image              %s\n' "${IMAGE:-<none>}"
        printf '  output dir         %s\n' "$OUTPUT_DIR"
        printf '  scenarios          %s\n' "${SELECTED_SCENARIOS# }"
        for index in $SELECTED_SCENARIOS; do
            printf '    %s. %s\n' "$index" "$(scenario_name_for_index "$index")"
        done
        printf '  releases           %s (standalone), %s (cluster)\n' "$SA_RELEASE" "$HA_RELEASE"
        printf '  cluster release    %s\n' "$([[ $needs_cluster -eq 1 ]] && echo required || echo 'not required')"
        printf '  standalone release %s\n' "$([[ $needs_standalone_install -eq 1 ]] && echo 'installed by the runner' || echo 'installed by scenario 1')"
        printf '  timeout            %s\n' "$TIMEOUT"
        # A dry run touches nothing, so it must not run the cleanup or the
        # results assembler either.
        trap - EXIT INT TERM
        exit 0
    fi

    prepare_output_directory
    trap on_exit EXIT
    trap on_interrupt INT TERM
    mkdir -p "$STATE_DIR" "$TMP_DIR" "$EVIDENCE_DIR"
    : >"$LEDGER_FILE"
    : >"$RESULTS_FILE"

    capture_source_identity
    preflight
    prepare_namespace
    resolve_names
    prepare_credentials

    if [[ $needs_cluster -eq 1 ]]; then
        prepare_certificates
        install_cluster
        wait_for_voters
    fi

    # The standalone release is installed by scenario 1 itself. It is installed
    # up front only when another selected scenario needs the service it exposes.
    if [[ $needs_standalone_install -eq 1 ]]; then
        install_standalone
    fi

    for index in $SELECTED_SCENARIOS; do
        if ! run_scenario "$index"; then
            drill_warn "scenario $index ($(scenario_name_for_index "$index")) failed"
        fi
    done

    reconcile_results
    SCENARIO_FAILURES="$(count_failed_scenarios)"

    if [[ "$SCENARIO_FAILURES" -gt 0 ]]; then
        log "$SCENARIO_FAILURES scenario(s) failed"
        exit 1
    fi

    log "all selected scenarios passed"
    exit 0
}

# Sourcing this file only defines the runner; the offline tests use that to
# exercise the results assembler without touching a cluster.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    parse_args "$@"
    main
fi
