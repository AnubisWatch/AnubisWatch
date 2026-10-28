#!/usr/bin/env bash
# Provenance checks in an isolated checkout, without new commits or staging.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/k8s-drill.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git clone --quiet --shared "$REPO_ROOT" "$work/repo"
REPO_ROOT="$work/repo"
IMAGE=repo/name:review
SELECTED_SCENARIOS=1
snapshot() {
    OUTPUT_DIR="$work/output-$1"
    resolve_output_paths
    mkdir -p "$STATE_DIR"
    : >"$RESULTS_FILE"
    : >"$LEDGER_FILE"
    write_results failed
    jq -cS .source "$OUTPUT_DIR/results.json"
}
clean=$(snapshot clean)
repeat=$(snapshot repeat)
[[ "$clean" == "$repeat" ]]
jq -e '.identity_status == "available" and .dirty == false and (.commit | test("^[a-f0-9]{40,64}$")) and (.worktree_sha256 | test("^[a-f0-9]{64}$"))' <<<"$clean" >/dev/null

cp "$REPO_ROOT/internal/core/config.go" "$work/original"
printf '\n// tracked regression fixture\n' >>"$REPO_ROOT/internal/core/config.go"
tracked=$(snapshot tracked)
[[ "$tracked" != "$clean" ]]
[[ $(jq -r .commit <<<"$tracked") == $(jq -r .commit <<<"$clean") ]]
jq -e .dirty <<<"$tracked" >/dev/null
cat "$work/original" >"$REPO_ROOT/internal/core/config.go"
[[ $(snapshot restored) == "$clean" ]]

# Untracked bytes and unusual filenames must contribute, not just git diff.
odd="$REPO_ROOT/untracked file
with newline.txt"
printf 'first\n' >"$odd"
untracked=$(snapshot untracked)
[[ "$untracked" != "$clean" ]]
printf 'second\n' >"$odd"
[[ $(snapshot untracked-edited) != "$untracked" ]]
rm "$odd"

chmod +x "$REPO_ROOT/internal/core/config.go"
[[ $(snapshot mode) != "$clean" ]]
chmod -x "$REPO_ROOT/internal/core/config.go"
ln -s 'target' "$REPO_ROOT/fixture-link"
link=$(snapshot link)
rm "$REPO_ROOT/fixture-link"
ln -s $'target\n' "$REPO_ROOT/fixture-link"
[[ $(snapshot changed-link) != "$link" ]]
rm "$REPO_ROOT/fixture-link"
mv "$REPO_ROOT/internal/core/config.go" "$work/deleted"
[[ $(snapshot deleted) != "$clean" ]]
mv "$work/deleted" "$REPO_ROOT/internal/core/config.go"
[[ $(snapshot restored-again) == "$clean" ]]

# The generated output tree excludes itself even when it is not gitignored.
OUTPUT_DIR="$REPO_ROOT/drill output"
resolve_output_paths
mkdir -p "$STATE_DIR"
: >"$RESULTS_FILE"
    : >"$LEDGER_FILE"
write_results failed
[[ $(jq -cS .source "$OUTPUT_DIR/results.json") == "$clean" ]]
printf 'generated API scratch\n' >"$OUTPUT_DIR/scratch"
printf 'ignored secret fixture\n' >"$REPO_ROOT/.env"
capture_source_identity
write_results failed
[[ $(jq -cS .source "$OUTPUT_DIR/results.json") == "$clean" ]]
# Captured identity stays the start snapshot when results are assembled later.
printf '\n// later source mutation\n' >>"$REPO_ROOT/internal/core/config.go"
write_results failed
[[ $(jq -cS .source "$OUTPUT_DIR/results.json") == "$clean" ]]

# Archives without Git and a failing hash operation must be explicit unknowns.
mkdir "$work/archive"
REPO_ROOT="$work/archive"
unavailable=$(snapshot archive)
jq -e '.identity_status == "unavailable" and .dirty == null and .worktree_sha256 == null' <<<"$unavailable" >/dev/null
REPO_ROOT="$work/repo"
sha256sum() { return 1; }
unavailable=$(snapshot failed-hash)
jq -e '.identity_status == "unavailable" and .worktree_sha256 == null' <<<"$unavailable" >/dev/null
unset -f sha256sum
echo 'Source provenance tests: PASS (tracked, untracked, modes, links, deletion, output exclusion, unavailable)'
echo 'FIX VERIFIED'
