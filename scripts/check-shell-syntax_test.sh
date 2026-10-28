#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/scripts/nested directory" "$work/empty"
printf '#!/bin/bash\nprintf "ok\\n"\n' >"$work/scripts/a-valid.sh"
check() { bash "$SCRIPT_DIR/check-shell-syntax.sh" "$1" >"$work/output" 2>&1; }
check "$work/scripts"
printf 'if then\n' >"$work/scripts/z-invalid.sh"
if check "$work/scripts"; then echo 'later script was not checked' >&2; exit 1; fi
rm "$work/scripts/z-invalid.sh"
printf 'if then\n' >"$work/scripts/nested directory/with spaces.sh"
if check "$work/scripts"; then echo 'nested script was not checked' >&2; exit 1; fi
rm "$work/scripts/nested directory/with spaces.sh"
printf 'if then\n' >"$work/scripts/nested directory/line
break.sh"
if check "$work/scripts"; then echo 'newline filename was not checked' >&2; exit 1; fi
rm "$work/scripts/nested directory/line
break.sh"
check "$work/scripts"
if check "$work/empty"; then echo 'empty scope accepted' >&2; exit 1; fi
if check "$work/missing"; then echo 'missing scope accepted' >&2; exit 1; fi
echo 'Shell syntax regression tests: PASS'
