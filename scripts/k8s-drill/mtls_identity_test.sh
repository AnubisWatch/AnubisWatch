#!/usr/bin/env bash
# Deterministic OpenSSL seam; live Kubernetes acceptance exercises real TLS.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/k8s-drill.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
CURRENT_SCENARIO=5
HA_FULLNAME=cluster
NAMESPACE=fixture
replica_pods() { printf 'voter-0\n'; }
pf_start() { PF_PORT=1; }
build_identify_frame() { printf '{}'; }
sleep() { :; }
timeout() { shift; "$@"; }
openssl() {
    if [[ " $* " == *' -verify_hostname '* ]]; then
        [[ " $* " == *' -cert '* && " $* " == *' -key '* && " $* " == *' -CAfile '* && " $* " == *' -verify_return_error '* ]] || return 1
        [[ " $* " == *' -verify_hostname voter-0.cluster-headless.fixture.svc.cluster.local '* ]] || return 1
        case "$mode" in
            valid) printf 'Verify return code: 0 (ok)\n' ;;
            wrong-ca) printf 'unable to get local issuer certificate\n'; return 1 ;;
            wrong-name) printf 'hostname mismatch\n'; return 1 ;;
            missing-verification) printf 'no verification result\n' ;;
        esac
    elif [[ " $* " == *' -cert '* ]]; then
        printf '{"awaiting_join":false}\n'
    else
        printf 'tlsv13 alert certificate required\n' >&2
        return 1
    fi
}
for mode in valid wrong-ca wrong-name missing-verification; do
    OUTPUT_DIR="$work/$mode"
    resolve_output_paths
    mkdir -p "$STATE_DIR"
    : >"$RESULTS_FILE"
    rc=0
    scenario_raft_mtls >"$work/$mode.log" 2>&1 || rc=$?
    if [[ "$mode" == valid ]]; then
        [[ "$rc" == 0 ]]
        jq -se '.[0].status == "passed"' "$RESULTS_FILE" >/dev/null
    else
        [[ "$rc" != 0 ]]
        jq -se '.[0].status == "failed" and (.[0].checks|[.[]|select(.status == "failed")]|length == 1)' "$RESULTS_FILE" >/dev/null
    fi
done
echo 'mTLS identity tests: PASS (required client credentials, trusted identity, CA/name/verification failures)'
echo 'FIX VERIFIED'
