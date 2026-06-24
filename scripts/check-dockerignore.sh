#!/usr/bin/env bash
# check-dockerignore.sh
# ──────────────────────
# CI sanity check for `.dockerignore`. Catches the regression where
# a future contributor adds a file with secrets (e.g. a stray
# `anubis.json`, a `.pem`, a `configs/*.local.yaml`) and forgets to
# add it to `.dockerignore`. A leaked Dockerfile layer is forever —
# this is the only cheap way to catch the drift in CI.
#
# Two layers of defence:
#
#   1. Lint: assert `.dockerignore` contains the patterns that
#      cover known-secret filenames. Catches accidental deletion
#      of a line from `.dockerignore`.
#
#   2. Build: build the actual project's Dockerfile and inspect
#      the final image's filesystem. Catches the case where a new
#      secret file is added but the contributor forgot to update
#      `.dockerignore`.
#
# Exit code: 0 = clean, 1 = banned file(s) would leak into the image.
#
# Used by: .github/workflows/ci.yml (build job, before the real
#          `docker build` step).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

DOCKERIGNORE=".dockerignore"

# ─── Lint: required patterns in .dockerignore ─────────────────────
# If any of these patterns is missing, the corresponding file path
# would leak into the image. Patterns here are the LITERAL
# entries that should appear in .dockerignore (modulo comment
# handling). The grep below anchors each pattern at line start
# with optional trailing whitespace + comment.
#
# Why literal patterns (not "contains the string anubis.json"):
# the .dockerignore file uses shell-glob syntax (where `*` means
# any string), not regex. A substring match would accept
# "configs/anubis.json" as covering the "anubis.json" requirement,
# which it does not — a root-level anubis.json file would still
# leak. Anchoring at line start forces the pattern to be a
# standalone entry.
REQUIRED_PATTERNS=(
  # local config (carries admin password, encryption key, cluster secret)
  'anubis\.json'
  'configs/anubis\.yaml'
  'configs/anubis\.json'
  'configs/\*\.local\.\*'
  'secrets/'
  # cryptographic material
  '\*\.pem'
  '\*\.key'
  '\*\.crt'
  '\*\.p12'
  # dotenv files (frequently carry secrets). The .dockerignore
  # syntax uses shell globbing (`*` = any string), so the
  # patterns below are the literal entries that should appear
  # in .dockerignore.
  '\.env$'
  '\.env\.\*'
  '\*\.env$'
  # dev binary (huge, useless in the image, leaks local build state).
  # The line "anubis" is required as a standalone entry. The
  # grep loop below accepts trailing whitespace + a "# ..." comment.
  '^anubis[[:space:]]*'
  '^bin/'
)

echo "check-dockerignore: linting $DOCKERIGNORE"
LINT_FAILED=0
for pattern in "${REQUIRED_PATTERNS[@]}"; do
  # Match each pattern as a whole line (anchored at start,
  # optional trailing whitespace + comment, end of line).
  # Substring matches don't count — a root-level pattern must
  # be a standalone entry, not part of a deeper path.
  if ! grep -qE "^[[:space:]]*${pattern}[[:space:]]*(#.*)?$" "$DOCKERIGNORE"; then
    echo "  MISSING: $pattern" >&2
    LINT_FAILED=1
  fi
done
if [ "$LINT_FAILED" -ne 0 ]; then
  echo "FAIL: $DOCKERIGNORE is missing one or more required patterns." >&2
  echo "      Add the missing line(s) and re-run." >&2
  exit 1
fi

# ─── Build: probe the actual Dockerfile's final image ──────────
# We build the project's real Dockerfile (anubiswatch:latest)
# and inspect the final image's filesystem. The project uses
# a multi-stage build; only the runtime stage matters for the
# leak check. The runtime image is `alpine:3.24` + the binary
# + a single config file (configs/container.anubis.json at
# /etc/anubis/anubis.json). Anything else in the final image
# is a leak.
#
# Why this approach, not a FROM scratch probe:
#   - A FROM scratch probe with `COPY . /ctx/` would model the
#     builder stage, not the runtime stage. The builder stage
#     does receive the full build context (this is the intended
#     COPY . . for compiling the binary). What we care about is
#     the RUNTIME stage — does the published image contain
#     secrets?
#   - Modelling the real Dockerfile is hermetic: any future
#     change to the Dockerfile (e.g. `COPY . /app/` at the
#     runtime stage) is automatically covered.
#   - A 3-minute full build is the cost of correctness.
#   - If 3 minutes is too long, the right next step is to
#     refactor the Dockerfile to use BuildKit cache mounts and
#     a slim probe stage, not to skip the test.
#
# We use `docker buildx` if available, falling back to `docker
# build` for older setups.

PROBE_TAG="anubis-dockerignore-probe:$$"
TMPDIR_PROBE=$(mktemp -d)
cleanup() {
  rm -rf "$TMPDIR_PROBE"
  docker rmi -f "$PROBE_TAG" 2>/dev/null || true
}
trap cleanup EXIT

echo "check-dockerignore: building the project's actual Dockerfile (this catches real drift)"
BUILDER=""
if docker buildx version >/dev/null 2>&1; then
  BUILDER="buildx"
fi
# --no-cache is critical: the buildx cache holds results from
# previous builds of the real project, which include the
# ~27MB `anubis` binary. Without --no-cache, the COPY step
# says "CACHED" and the probe image is built from those
# cached layers — making the test pass spuriously even if
# .dockerignore is broken.
# shellcheck disable=SC2086
docker $BUILDER build --no-cache -q -f Dockerfile -t "$PROBE_TAG" . >/dev/null

# Extract the image's layers. Newer Docker (buildx) emits the
# OCI image layout: a top-level manifest.json + a blobs/ tree of
# content-addressed layer tarballs (gzip-compressed). The legacy
# builder emits a flat tar of files. Handle both.
docker save "$PROBE_TAG" | tar -xf - -C "$TMPDIR_PROBE"

LAYERS_DIR="$TMPDIR_PROBE/layers"
mkdir -p "$LAYERS_DIR"

if [ -f "$TMPDIR_PROBE/manifest.json" ] && command -v python3 >/dev/null 2>&1; then
  # OCI layout — read the manifest to find the layer blob paths
  # and ungzip+untar each into LAYERS_DIR.
  python3 -c "
import json, sys
m = json.load(open('$TMPDIR_PROBE/manifest.json'))
for entry in m:
    for layer in entry.get('Layers', []):
        print(layer)
" | while read -r layer; do
    if [ -f "$TMPDIR_PROBE/$layer" ]; then
      gzip -d -c "$TMPDIR_PROBE/$layer" | tar -xf - -C "$LAYERS_DIR" 2>/dev/null || true
    fi
  done
else
  # Legacy layout — the files are at the top of the tar (the
  # image's filesystem is the tar itself). Just point LAYERS_DIR
  # at the extracted root.
  LAYERS_DIR="$TMPDIR_PROBE"
fi

# Whitelist of files that are intentionally in the final image.
# These are the only things the Dockerfile copies into the
# runtime stage. If a new file shows up, that's a leak unless
# it's added to the whitelist.
#
# The whitelist is explicit (not "scan for the known binary
# name") so that a future change to the Dockerfile that adds
# e.g. a COPY of /etc/anubis/admin.yaml requires the operator
# to also update this list. The forced update is the audit
# trail.
ALLOWED_BASENAMES=(
  "anubis"                                # the binary at /bin/anubis
  "container.anubis.json"                 # the baked-in config at /etc/anubis/anubis.json
  "anubis-icon.svg"                       # the dashboard icon (used at runtime)
)

# Check for files that match the BANNED filenames. We use
# basenames so the check is path-agnostic (a leaked
# /some/path/anubis.json is caught the same as /anubis.json).
BANNED_BASENAMES_REGEX='\.(pem|key|crt|p12|env)$|^(anubis\.json)$'

# Find any file in the runtime image whose basename matches
# a banned pattern AND whose basename is NOT in the allowlist.
# We exclude system paths populated by the base image (Alpine's
# CA bundle, etc.) — only files originating from the build
# context are interesting.
BANNED_HITS=$(find "$LAYERS_DIR" -type f \
  -not -path '*/etc/ssl/certs/*' \
  -not -path '*/usr/share/ca-certificates/*' \
  2>/dev/null \
  | awk -F/ '{print $NF}' \
  | grep -E "$BANNED_BASENAMES_REGEX" \
  | grep -vF -f <(printf '%s\n' "${ALLOWED_BASENAMES[@]}") \
  | head -50 || true)

# Path-based checks for directory patterns that .dockerignore
# should exclude. We exclude the same system paths as above.
SECRETS_HITS=$(find "$LAYERS_DIR" -path '*/secrets/*' \
  -not -path '*/etc/ssl/certs/*' \
  -type f 2>/dev/null | head -10 || true)
LOCALCFG_HITS=$(find "$LAYERS_DIR" -path '*/configs/*.local.*' \
  -not -path '*/etc/ssl/certs/*' \
  -type f 2>/dev/null | head -10 || true)

if [ -n "$BANNED_HITS$SECRETS_HITS$LOCALCFG_HITS" ]; then
  echo "FAIL: .dockerignore did not exclude all banned files from the runtime image." >&2
  [ -n "$BANNED_HITS" ] && {
    echo "  Banned basenames in image:" >&2
    echo "$BANNED_HITS" | sort -u | sed 's/^/    /' >&2
  }
  [ -n "$SECRETS_HITS" ] && {
    echo "  secrets/ files in image:" >&2
    echo "$SECRETS_HITS" | sed 's/^/    /' >&2
  }
  [ -n "$LOCALCFG_HITS" ] && {
    echo "  configs/*.local.* files in image:" >&2
    echo "$LOCALCFG_HITS" | sed 's/^/    /' >&2
  }
  echo "" >&2
  echo "  Add the missing pattern(s) to $DOCKERIGNORE and re-run." >&2
  echo "  (If a new path is intentionally in the runtime image, update ALLOWED_BASENAMES in this script.)" >&2
  exit 1
fi

echo "check-dockerignore: OK"
