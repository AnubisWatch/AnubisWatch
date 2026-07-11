#!/usr/bin/env bash

# Enforce exact statement coverage for project-owned Go code. The package scope
# is intentionally limited to the shipped command and internal implementation;
# generated protobuf bindings are the sole source exception.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

PROFILE="${GO_COVERAGE_PROFILE:-coverage.out}"
FILTERED_PROFILE="${GO_FILTERED_COVERAGE_PROFILE:-coverage-filtered.out}"

# Race-enabled coverage requires atomic counters.
go test -race -covermode=atomic -coverprofile="$PROFILE" ./cmd/... ./internal/...

# Preserve the profile header and remove only generated protobuf source files.
awk '
  NR == 1 || $1 !~ /internal\/grpcapi\/v1\/[^/:]+\.pb\.go:/
' "$PROFILE" > "$FILTERED_PROFILE"

go tool cover -func="$FILTERED_PROFILE"

# Compare covered statement blocks directly instead of trusting the rounded
# percentage printed by `go tool cover` (99.96% can display as 100.0%).
awk '
  NR > 1 {
    total += $2
    if ($3 > 0) {
      covered += $2
    }
  }
  END {
    if (total == 0) {
      print "Go coverage gate: filtered profile contains no statements" > "/dev/stderr"
      exit 1
    }

    printf "Go coverage gate: %d/%d statements covered\n", covered, total
    if (covered != total) {
      printf "Go coverage gate: exact 100%% required (%d statements uncovered)\n", total - covered > "/dev/stderr"
      exit 1
    }
  }
' "$FILTERED_PROFILE"
