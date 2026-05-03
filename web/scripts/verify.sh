#!/bin/bash
# Full System Verification Script

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "═══════════════════════════════════════════════════"
echo "⚖️  AnubisWatch Full System Verification"
echo "═══════════════════════════════════════════════════"
echo ""

PASS=0
FAIL=0

check() {
    if [ "$1" -eq 0 ]; then
        echo "✓ $2"
        PASS=$((PASS+1))
    else
        echo "✗ $2"
        FAIL=$((FAIL+1))
    fi
}

run_check() {
    local name="$1"
    shift
    if "$@"; then
        check 0 "$name"
    else
        check 1 "$name"
    fi
}

cd "$PROJECT_ROOT"

echo "1. Build Tests"
echo "───────────────────────────────────────────────────"
run_check "Embedded frontend build" bash -c "cd web && npm run build:embed >/dev/null"
run_check "Go binary build" bash -c "CGO_ENABLED=0 go build -o /tmp/anubis-verify ./cmd/anubis >/dev/null"

echo ""
echo "2. Unit Tests"
echo "───────────────────────────────────────────────────"
run_check "Go unit tests" go test -short ./...
run_check "Frontend unit tests" bash -c "cd web && npm run test >/dev/null"

echo ""
echo "3. Deployment Files"
echo "───────────────────────────────────────────────────"
run_check "Dockerfile exists" test -f Dockerfile
run_check "docker-compose.yml exists" test -f docker-compose.yml
run_check "K8s manifests exist" test -d deploy/k8s
run_check "Helm chart exists" test -f deploy/helm/anubiswatch/Chart.yaml
run_check "CI workflow exists" test -f .github/workflows/ci.yml

echo ""
echo "═══════════════════════════════════════════════════"
echo "Results: $PASS passed, $FAIL failed"
echo "═══════════════════════════════════════════════════"

if [ $FAIL -gt 0 ]; then
    exit 1
else
    echo "✓ All systems operational"
    exit 0
fi
