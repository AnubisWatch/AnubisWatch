#!/usr/bin/env bash
# Compatibility wrapper: the Kubernetes acceptance drill runner lives at
# scripts/k8s-drill/k8s-drill.sh. This path is kept so the acceptance card's
# file_exists criterion (scripts/k8s-acceptance-drill.sh) matches the tree.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SCRIPT_DIR/k8s-drill/k8s-drill.sh" "$@"
