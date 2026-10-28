#!/usr/bin/env bash
# Local, isolated monitoring soak and encrypted-storage restart check.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."
exec go run ./scripts/productionsoak "$@"
