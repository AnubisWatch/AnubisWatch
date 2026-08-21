#!/usr/bin/env bash

# Enforce the repository's minimum statement-coverage policy for project-owned
# Go code. The package scope is intentionally limited to the shipped command
# and internal implementation; generated protobuf bindings are excluded.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

PROFILE="${GO_COVERAGE_PROFILE:-coverage.out}"
FILTERED_PROFILE="${GO_FILTERED_COVERAGE_PROFILE:-coverage-filtered.out}"
MIN_COVERAGE="${GO_MIN_COVERAGE:-80}"

# Race-enabled coverage requires atomic counters.
go test -race -covermode=atomic -coverprofile="$PROFILE" ./cmd/... ./internal/...

# Preserve the profile header and remove only generated protobuf source files.
awk '
  NR == 1 || $1 !~ /internal\/grpcapi\/v1\/[^/:]+\.pb\.go:/
' "$PROFILE" > "$FILTERED_PROFILE"

go tool cover -func="$FILTERED_PROFILE"

# Compare covered statement blocks directly instead of trusting the rounded
# percentage printed by `go tool cover`.
awk -v minimum="$MIN_COVERAGE" '
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
    if (minimum !~ /^[0-9]+([.][0-9]+)?$/ || minimum < 0 || minimum > 100) {
      printf "Go coverage gate: invalid GO_MIN_COVERAGE value %q\n", minimum > "/dev/stderr"
      exit 1
    }

    percentage = covered * 100 / total
    printf "Go coverage gate: %d/%d statements covered (%.2f%%; minimum %.2f%%)\n", covered, total, percentage, minimum
    if (percentage + 0.0000001 < minimum) {
      printf "Go coverage gate: %.2f%% is below the %.2f%% minimum\n", percentage, minimum > "/dev/stderr"
      exit 1
    }
  }
' "$FILTERED_PROFILE"
