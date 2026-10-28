#!/usr/bin/env bash
# Parse every shell script under the supplied directory (default: scripts).
set -euo pipefail
root="${1:-scripts}"
list=$(mktemp)
trap 'rm -f "$list"' EXIT
find "$root" -type f -name '*.sh' -print0 >"$list"
count=0
failed=0
while IFS= read -r -d '' script; do
    count=$((count + 1))
    if ! bash -n "$script"; then
        printf 'FAIL: %s\n' "$script" >&2
        failed=1
    fi
done <"$list"
if [[ "$count" == 0 ]]; then
    printf 'No shell scripts found under %s\n' "$root" >&2
    exit 1
fi
printf 'Shell syntax: checked %s script(s)\n' "$count"
exit "$failed"
