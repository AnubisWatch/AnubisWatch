#!/bin/bash
# E2E Test Script for AnubisWatch

set -euo pipefail

echo "⚖️  AnubisWatch E2E Test Suite"
echo "================================"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test functions
run_backend_tests() {
    echo -e "${YELLOW}Running backend tests...${NC}"
    go test -v -coverprofile=coverage.out ./... 2>&1 | grep -aE "(^ok|^FAIL|coverage:)"
    
    # Check coverage threshold, excluding generated protobuf code.
    grep -v "internal/grpcapi/v1/" coverage.out > coverage-filtered.out || true
    COVERAGE=$(go tool cover -func=coverage-filtered.out | grep total | awk '{print $3}' | sed 's/%//')
    echo -e "Coverage (excluding generated): ${GREEN}$COVERAGE%${NC}"
    
    if ! awk -v coverage="$COVERAGE" 'BEGIN { exit !(coverage >= 80) }'; then
        echo -e "${RED}FAIL: Coverage below 80%${NC}"
        exit 1
    fi
}

run_frontend_tests() {
    echo -e "${YELLOW}Running frontend tests...${NC}"
    cd web
    npm run test 2>&1 | tail -10
    cd ..
}

run_lint() {
    echo -e "${YELLOW}Running linter...${NC}"
    cd web
    npm run lint 2>&1 | tail -5
    cd ..
}

run_build() {
    echo -e "${YELLOW}Building embedded dashboard...${NC}"
    cd web
    npm run build:embed
    cd ..

    echo -e "${YELLOW}Building binary...${NC}"
    mkdir -p .tmp
    CGO_ENABLED=0 go build -o .tmp/anubis-e2e ./cmd/anubis
    CGO_ENABLED=0 go build -ldflags "-s -w" -o /tmp/anubis-test ./cmd/anubis
    /tmp/anubis-test version
}

detect_chrome_executable() {
    if [[ -n "${PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH:-}" ]]; then
        return 0
    fi

    for browser in google-chrome google-chrome-stable chromium chromium-browser; do
        if command -v "$browser" >/dev/null 2>&1; then
            PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=$(command -v "$browser")
            export PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
            return 0
        fi
    done
}

run_browser_e2e() {
    if [[ "${SKIP_BROWSER_E2E:-false}" == "true" ]]; then
        echo -e "${YELLOW}Skipping browser E2E because SKIP_BROWSER_E2E=true${NC}"
        return
    fi

    detect_chrome_executable || true
    if [[ -n "${PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH:-}" ]]; then
        echo -e "${YELLOW}Running browser E2E with ${PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH}...${NC}"
    else
        echo -e "${YELLOW}Running browser E2E with Playwright-managed Chromium...${NC}"
    fi

    cd web
    npm run e2e
    cd ..
}

run_docker_build() {
    if [[ "${SKIP_DOCKER:-false}" == "true" ]]; then
        echo -e "${YELLOW}Skipping Docker build because SKIP_DOCKER=true${NC}"
        return
    fi
    if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
        echo -e "${YELLOW}Skipping Docker build because Docker is unavailable${NC}"
        return
    fi

    echo -e "${YELLOW}Building Docker image...${NC}"
    docker build -t anubiswatch:test . > /dev/null 2>&1
    echo -e "${GREEN}Docker build successful${NC}"
}

# Main
echo ""
run_backend_tests
echo ""
run_frontend_tests
echo ""
run_lint
echo ""
run_build
echo ""
run_browser_e2e
echo ""
run_docker_build

echo ""
echo -e "${GREEN}✅ All E2E tests passed!${NC}"
