#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
ANUBIS_ADMIN_PASSWORD=Fixture-Compose47 ANUBIS_CLUSTER_SECRET=Fixture-Cluster47 docker compose config --quiet
echo 'CONTROL EXPECTED: complete cluster environment parses; ACTUAL: success'
if env -u ANUBIS_CLUSTER_SECRET ANUBIS_ADMIN_PASSWORD=Fixture-Compose47 docker compose config --quiet; then
  echo 'EXPECTED: standalone parses without cluster secret; ACTUAL: success'
  echo 'FIX VERIFIED'
else
  echo 'EXPECTED: standalone parses without cluster secret; ACTUAL: interpolation error'
  echo 'PROBLEM CONFIRMED'
  exit 1
fi
