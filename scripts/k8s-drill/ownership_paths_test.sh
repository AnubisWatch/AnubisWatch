#!/usr/bin/env bash
# Offline regression checks for namespace ownership and explicit output paths.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/k8s-drill.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
LEDGER_FILE="$work/ledger"
KUBECONFIG="$work/kubeconfig"
CONTEXT=drill
NAMESPACE=drill
SA_NAMESPACE=drill
touch "$KUBECONFIG"
# Model exclusive server-side create: an existing namespace or API failure
# cannot be claimed. The second namespace failure leaves only the first owned.
kubectl() {
    printf '%s\n' "$*" >>"$work/calls"
    [[ " $* " == *' create namespace '* ]] || return 99
    case "$MODE" in
        fresh) return 0 ;;
        existing|forbidden) return 1 ;;
        second-fails) [[ "${*: -1}" != standalone ]] ;;
        *) return 99 ;;
    esac
}
MODE=fresh
: >"$LEDGER_FILE"
prepare_namespace > /dev/null
[[ $(wc -l <"$LEDGER_FILE") == 1 ]]
[[ $(wc -l <"$work/calls") == 1 ]]
grep -qx 'namespace drill' "$LEDGER_FILE"
SA_NAMESPACE=standalone
: >"$LEDGER_FILE"
prepare_namespace > /dev/null
[[ $(wc -l <"$LEDGER_FILE") == 2 ]]
for MODE in existing forbidden; do
    : >"$LEDGER_FILE"
    if (prepare_namespace) >/dev/null 2>&1; then
        echo "accepted unowned namespace: $MODE" >&2
        exit 1
    fi
    [[ ! -s "$LEDGER_FILE" ]]
done
MODE=second-fails
: >"$LEDGER_FILE"
if (prepare_namespace) >/dev/null 2>&1; then exit 1; fi
[[ $(cat "$LEDGER_FILE") == 'namespace drill' ]]
echo 'namespace ownership: PASS (shared, separate, existing, forbidden, partial failure)'

# Both contenders reach create before either may finish. The API's atomic
# create grants ownership to A; B must fail without recording A's namespace.
for contender in A B; do
    mkfifo "$work/ready-$contender" "$work/go-$contender"
done
kubectl() {
    printf 'ready\n' >"$work/ready-$contender"
    read -r release <"$work/go-$contender"
    mkdir "$work/api-namespace" 2>/dev/null
}
contend() {
    local contender="$1"
    LEDGER_FILE="$work/ledger-$contender"
    NAMESPACE=drill
    SA_NAMESPACE=drill
    prepare_namespace >/dev/null 2>&1
}
contend A &
pid_a=$!
contend B &
pid_b=$!
read -r ready <"$work/ready-A"
read -r ready <"$work/ready-B"
printf 'go\n' >"$work/go-A"
wait "$pid_a"
printf 'go\n' >"$work/go-B"
if wait "$pid_b"; then exit 1; fi
[[ $(cat "$work/ledger-A") == 'namespace drill' ]]
[[ ! -s "$work/ledger-B" ]]
[[ -d "$work/api-namespace" ]]
echo 'namespace contention: PASS (gated winner then loser)'

for output in "$work/custom" "$work/with spaces" "$work/nested/out"; do
    parse_args --kubeconfig "$KUBECONFIG" --context drill --image repo/name:1 --output-dir "$output"
    [[ "$OUTPUT_DIR" == "$output" ]]
    [[ "$STATE_DIR" == "$output/state" ]]
    [[ "$TLS_DIR" == "$output/state/certs" ]]
    [[ "$TMP_DIR" == "$output/tmp" ]]
    [[ "$EVIDENCE_DIR" == "$output/evidence" ]]
    [[ "$LEDGER_FILE" == "$output/state/ledger.txt" ]]
    [[ "$RESULTS_FILE" == "$output/state/scenario-results.frag" ]]
    [[ ! -d "$output" ]]
done
echo 'output paths: PASS (custom, spaces, repeated argument parsing)'
echo 'FIX VERIFIED'
