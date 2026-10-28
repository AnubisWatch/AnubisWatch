#!/usr/bin/env bash
# Checkout provenance, separate from the requested container image reference.
# Hash tracked and nonignored untracked content; never publish paths or bytes.
source_worktree_manifest() (
    local root="$1" output="$2" path hash kind state
    local -a paths=(.)
    state="$(cd "$STATE_DIR" && pwd -P)" || return 1
    if [[ "$output" == "$root/"* ]]; then
        paths+=(":(top,exclude,literal)${output#"$root/"}")
    fi
    cd "$root" || return 1
    git ls-files --cached --others --exclude-standard -z -- "${paths[@]}" |
        LC_ALL=C sort -zu >"$state/source-files" || return 1
    : >"$state/source-regular" || return 1
    while IFS= read -r -d '' path; do
        hash=""
        if [[ -L "$path" ]]; then
            kind=symlink
            hash="$(readlink -n -- "$path" | sha256sum)" || return 1
        elif [[ -f "$path" ]]; then
            kind=file
            [[ ! -x "$path" ]] || kind=executable
            printf '%s\0' "$path" >>"$state/source-regular" || return 1
        elif [[ ! -e "$path" ]]; then
            kind=missing
        else
            # Submodules/unsupported files cannot imply complete identity.
            return 1
        fi
        printf '%s\0%s\0%s\0' "$path" "$kind" "${hash%% *}" || return 1
    done <"$state/source-files"
    printf 'contents\0' || return 1
    if [[ -s "$state/source-regular" ]]; then
        # Batch hashing avoids one process per source file. NUL records keep
        # spaces/newlines unambiguous; paths stay relative for reproducibility.
        xargs -0 sha256sum --zero -- <"$state/source-regular" || return 1
    fi
)

capture_source_identity() {
    local identity="$STATE_DIR/source-identity.json"
    local root output commit dirty hash
    local -a paths=(.)
    # Unavailable provenance is explicit, including archives without .git.
    printf '{"identity_status":"unavailable","commit":"unknown","dirty":null,"worktree_sha256":null}\n' >"$identity"
    command -v git >/dev/null 2>&1 || return 0
    command -v sha256sum >/dev/null 2>&1 || return 0
    root="$(git -C "$REPO_ROOT" rev-parse --show-toplevel 2>/dev/null)" || return 0
    root="$(cd "$root" && pwd -P)" || return 0
    output="$(cd "$OUTPUT_DIR" && pwd -P)" || return 0
    commit="$(git -C "$root" rev-parse --verify 'HEAD^{commit}' 2>/dev/null)" || return 0
    [[ "$commit" =~ ^[a-f0-9]{40,64}$ ]] || return 0
    if [[ "$output" == "$root/"* ]]; then
        paths+=(":(top,exclude,literal)${output#"$root/"}")
    fi
    git -C "$root" status --porcelain=v1 --untracked-files=all -- "${paths[@]}" >"$STATE_DIR/source-status" 2>/dev/null || return 0
    dirty=false
    [[ ! -s "$STATE_DIR/source-status" ]] || dirty=true
    hash="$(source_worktree_manifest "$root" "$output" | sha256sum)" || return 0
    hash="${hash%% *}"
    [[ "$hash" =~ ^[a-f0-9]{64}$ ]] || return 0
    printf '{"identity_status":"available","commit":"%s","dirty":%s,"worktree_sha256":"%s"}\n' \
        "$commit" "$dirty" "$hash" >"$identity"
}
