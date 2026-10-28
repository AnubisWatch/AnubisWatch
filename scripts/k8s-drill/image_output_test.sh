#!/usr/bin/env bash
# Offline regressions for immutable image references and evidence ownership.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/k8s-drill.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
digest="sha256:$(printf 'a%.0s' {1..64})"
for repository in team/anubis registry.local:5000/team/anubis; do
    parse_image "$repository@$digest"
    [[ "$IMAGE_REPOSITORY" == "$repository" && -z "$IMAGE_TAG" && "$IMAGE_DIGEST" == "$digest" ]]
    parse_image "$repository:review@$digest"
    [[ "$IMAGE_REPOSITORY" == "$repository" && "$IMAGE_TAG" == review && "$IMAGE_DIGEST" == "$digest" ]]
    parse_image "$repository:next"
    [[ "$IMAGE_REPOSITORY" == "$repository" && "$IMAGE_TAG" == next && -z "$IMAGE_DIGEST" ]]
done
parse_image "docker://team/anubis@$digest"
[[ "$IMAGE_REPOSITORY" == team/anubis ]]
parse_image "team/anubis:latest@$digest"
[[ "$IMAGE_DIGEST" == "$digest" ]]
for image in team/anubis team/anubis:latest team/anubis: team/anubis@ team/anubis@sha256:abc "@$digest" "team/anubis@@$digest"; do
    if (parse_image "$image") >/dev/null 2>&1; then
        echo "invalid image accepted: $image" >&2
        exit 1
    fi
done
echo 'image identity: PASS (digest, tag+digest, port, reset, invalid inputs)'

OUTPUT_DIR="$work/nested/fresh"
resolve_output_paths
prepare_output_directory
[[ -d "$OUTPUT_DIR" && ! -e "$STATE_DIR" ]]
printf 'original\n' >"$OUTPUT_DIR/sentinel"
if (prepare_output_directory) >/dev/null 2>&1; then exit 1; fi
[[ $(cat "$OUTPUT_DIR/sentinel") == original ]]
mkdir "$work/foreign"
printf 'foreign\n' >"$work/foreign/sentinel"
ln -s "$work/foreign" "$work/symlink"
for output in "$work/symlink" "$work/foreign/sentinel"; do
    OUTPUT_DIR="$output"
    if (prepare_output_directory) >/dev/null 2>&1; then exit 1; fi
done
[[ $(cat "$work/foreign/sentinel") == foreign ]]

# Gate two runs at the exclusive mkdir: A wins, B resumes afterwards.
for contender in A B; do
    mkfifo "$work/ready-$contender" "$work/go-$contender"
done
mkdir() {
    if [[ "$*" == "-- $work/contended" ]]; then
        printf 'ready\n' >"$work/ready-$contender"
        read -r release <"$work/go-$contender"
    fi
    command mkdir "$@"
}
contend() {
    local contender="$1"
    OUTPUT_DIR="$work/contended"
    prepare_output_directory >/dev/null 2>&1
    printf '%s\n' "$contender" >"$OUTPUT_DIR/owner"
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
[[ $(cat "$work/contended/owner") == A ]]
echo 'output ownership: PASS (fresh, existing, file, symlink, gated contention)'
echo 'FIX VERIFIED'
