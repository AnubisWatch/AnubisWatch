#!/usr/bin/env bash
# Shared helpers for the AnubisWatch Kubernetes acceptance drill.
#
# This file is sourced by scripts/k8s-drill/k8s-drill.sh; it is not meant to be
# executed on its own. Style follows the other scripts in this repository:
# explicit inputs, no ambient cluster state, and every secret handled out of
# stdout.

# --------------------------------------------------------------------------
# Logging
# --------------------------------------------------------------------------

drill_log() {
    printf '[drill] %s\n' "$*"
}

drill_warn() {
    printf '[drill] WARN: %s\n' "$*" >&2
}

drill_fail() {
    printf '[drill] FAIL: %s\n' "$*" >&2
    exit 1
}

drill_section() {
    printf '\n[drill] === %s ===\n' "$*"
}

# --------------------------------------------------------------------------
# Small utilities
# --------------------------------------------------------------------------

drill_require_cmd() {
    command -v "$1" >/dev/null 2>&1 || drill_fail "$1 is required but was not found on PATH"
}

drill_have_cmd() {
    command -v "$1" >/dev/null 2>&1
}

# Globals shared with the runner. They are set here so the helpers below
# never depend on a variable the caller forgot to initialise.
LOGIN_ERROR=""
SOUL_ID=""
CLUSTER_STATUS_BODY=""
CLUSTER_STATUS_CODE=""

# json_escape escapes a string for embedding in a JSON document.
json_escape() {
    local value="$1"
    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    value="${value//$'\n'/\\n}"
    value="${value//$'\r'/\\r}"
    value="${value//$'\t'/\\t}"
    printf '%s' "$value"
}

# drill_lower lowercases without bash 4-only syntax.
drill_lower() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

# drill_now_utc prints an ISO-8601 UTC timestamp.
drill_now_utc() {
    date -u +%Y-%m-%dT%H:%M:%SZ
}

# drill_epoch prints seconds since the epoch.
drill_epoch() {
    date -u +%s
}

# --------------------------------------------------------------------------
# Cluster access. Every call names the kubeconfig and the context explicitly;
# there is no fallback to the ambient current-context.
# --------------------------------------------------------------------------

kube() {
    kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" "$@"
}

kube_ns() {
    kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" --namespace "$NAMESPACE" "$@"
}

helm_cmd() {
    # helm has no --context flag; it uses the current context of the
    # kubeconfig file. Materialise a minified single-context kubeconfig so
    # helm still only ever talks to the explicitly selected context.
    if [[ -z "${HELM_KUBECONFIG_FILE:-}" ]]; then
        if [[ -n "$STATE_DIR" ]]; then
            HELM_KUBECONFIG_FILE="$STATE_DIR/kubeconfig-helm.yaml"
        else
            HELM_KUBECONFIG_FILE="$(mktemp "${TMPDIR:-/tmp}/drill-kubeconfig.XXXXXX")"
        fi
        kubectl --kubeconfig "$KUBECONFIG" config view --context "$CONTEXT" --minify --flatten > "$HELM_KUBECONFIG_FILE"
        chmod 600 "$HELM_KUBECONFIG_FILE"
    fi
    helm --kubeconfig "$HELM_KUBECONFIG_FILE" "$@"
}

# drill_chart_name returns the chart name from Chart.yaml.
drill_chart_name() {
    sed -n 's/^name:[[:space:]]*//p' "$CHART/Chart.yaml" | head -1 | tr -d '"'"'"' \r'
}

# drill_fullname mirrors the chart's fullname helper so the runner can predict
# the StatefulSet, Service and PVC names it is about to create.
drill_fullname() {
    local release="$1" chart_name="$2" fullname
    case "$release" in
        *"$chart_name"*) fullname="$release" ;;
        *) fullname="$release-$chart_name" ;;
    esac
    printf '%s' "${fullname}" | cut -c1-63 | sed 's/-$//'
}

# --------------------------------------------------------------------------
# Resource ledger. The runner only ever deletes what it recorded here.
# --------------------------------------------------------------------------

ledger_add() {
    printf '%s\n' "$*" >>"$LEDGER_FILE"
}

ledger_entries() {
    local kind="$1" ledger="${2:-$LEDGER_FILE}"
    [[ -f "$ledger" ]] || return 1
    awk -v kind="$kind" '$1 == kind { sub(/^[^ ]+ /, ""); print }' "$ledger"
}

# Callers must check the result before consuming the snapshot: a partial
# output from a failed read/validation never grants resource ownership.
ledger_snapshot() {
    local destination="$1"
    [[ -f "$LEDGER_FILE" ]] || return 1
    awk '
        NF == 0 { next }
        $1 == "helm" && NF == 3 { print; next }
        ($1 == "namespace" || $1 == "secret" || $1 == "cordon" || $1 == "portforward") && NF == 2 { print; next }
        { exit 1 }
    ' "$LEDGER_FILE" >"$destination"
}

# --------------------------------------------------------------------------
# HTTP helpers
# --------------------------------------------------------------------------

# api_call METHOD PATH [JSON_BODY]
#
# Sets API_STATUS and API_BODY. Retries on 429 and 503 because the API applies
# per-IP rate limits and the drill talks to several replicas through the same
# tunnel.
api_call() {
    local method="$1" path="$2" body="${3:-}"
    local out="$TMP_DIR/api-body" errf="$TMP_DIR/api-error"
    local code attempt=1
    local args

    mkdir -p "$TMP_DIR" 2>/dev/null || true

    args=(-sS --connect-timeout 5 --max-time 30 -o "$out" -w '%{http_code}' -X "$method")
    args+=(-H 'Accept: application/json')
    # The response headers are captured on every call: the session cookie is
    # only ever visible there, and a login must read the headers of its own
    # response instead of a leftover file from an earlier login.
    args+=(-D "$TMP_DIR/api-headers")
    if [[ -n "${TOKEN:-}" ]]; then
        args+=(-H "Authorization: Bearer $TOKEN")
    fi
    if [[ -n "$body" ]]; then
        printf '%s' "$body" >"$TMP_DIR/api-request.json"
        args+=(-H 'Content-Type: application/json' --data-binary "@$TMP_DIR/api-request.json")
    fi

    while :; do
        # Bounded requests: an unbounded curl against a broken tunnel is how
        # a scenario silently stalls (finding F2). Retry logic below owns
        # retries; each attempt must fail fast instead.
        code="$(curl --connect-timeout 5 --max-time 20 "${args[@]}" "$API_BASE$path" 2>"$errf" || true)"
        if [[ -z "$code" ]]; then
            code=000
        fi
        if [[ "$code" == "429" || "$code" == "503" ]]; then
            if [[ $attempt -ge 5 ]]; then
                break
            fi
            sleep $((attempt * 5))
            attempt=$((attempt + 1))
            continue
        fi
        break
    done

    API_STATUS="$code"
    API_BODY="$(cat "$out" 2>/dev/null || true)"
    return 0
}

# api_expect METHOD PATH EXPECTED_STATUS [JSON_BODY] LABEL
api_expect() {
    local method="$1" path="$2" expected="$3" body="${4:-}" label="${5:-$1 $2}"
    api_call "$method" "$path" "$body"
    if [[ "$API_STATUS" != "$expected" ]]; then
        drill_fail "$label returned HTTP $API_STATUS, expected $expected: ${API_BODY:0:300}"
    fi
    drill_log "ok: $label ($method $path -> $API_STATUS)"
}

# api_json FIELD  reads a top-level string field from the last API_BODY.
api_json() {
    printf '%s' "$API_BODY" | jq -r "$1 // empty" 2>/dev/null || true
}

# api_login EMAIL PASSWORD authenticates against the current API_BASE and
# stores the session token in TOKEN.
#
# It returns 1 when the login is refused. The token is read from the headers
# of this response, so it always belongs to the session that was just
# created.
api_login() {
    local email="$1" password="$2" body
    body="$(printf '{"email":"%s","password":"%s"}' "$(json_escape "$email")" "$(json_escape "$password")")"
    api_call POST /api/v1/auth/login "$body"
    if [[ "$API_STATUS" != "200" ]]; then
        TOKEN=""
        return 1
    fi
    # The API issues the session as an httpOnly cookie; the token is not part
    # of the JSON body.
    TOKEN="$(sed -n 's/.*auth_token=\([^;[:space:]]*\).*/\1/p' "$TMP_DIR/api-headers" 2>/dev/null | head -1)"
    if [[ -z "$TOKEN" ]]; then
        return 1
    fi
    return 0
}

# drill_login_soft EMAIL PASSWORD authenticates and stores the session token
# in TOKEN.
#
# It returns 1 instead of aborting the drill when the login fails, so a
# scenario can report the failure as a check. LOGIN_ERROR holds the reason.
#
# The session cookie is read from the headers of THIS response. An earlier
# login's token must never be reused: the standalone release and the cluster
# release are different authorities, so a stale token is rejected by every
# cluster replica with "invalid token".
drill_login_soft() {
    local email="$1" password="$2"
    if api_login "$email" "$password"; then
        return 0
    fi
    if [[ "$API_STATUS" != "200" ]]; then
        LOGIN_ERROR="login as $email returned HTTP $API_STATUS: ${API_BODY:0:200}"
    else
        LOGIN_ERROR="login as $email did not return an auth_token cookie (HTTP $API_STATUS: ${API_BODY:0:200})"
    fi
    TOKEN=""
    return 1
}

# drill_login authenticates and stores the session token in TOKEN. A failed
# login ends the drill: without a session nothing else can be checked.
drill_login() {
    local email="$1" password="$2"
    if ! drill_login_soft "$email" "$password"; then
        drill_fail "$LOGIN_ERROR"
    fi
    drill_log "ok: authenticated as $email"
}

# --------------------------------------------------------------------------
# Port forwarding. Each replica needs its own tunnel so that per-replica
# reads and writes really address that replica.
# --------------------------------------------------------------------------

# pf_start POD REMOTE_PORT sets PF_PORT to the local port of a new tunnel.
pf_start() {
    local pod="$1" remote="$2"
    local key="$pod.$remote"
    local portfile="$STATE_DIR/pf/$key.port" pidfile="$STATE_DIR/pf/$key.pid" logf="$STATE_DIR/pf/$key.log"
    local pid port i

    mkdir -p "$STATE_DIR/pf"
    if [[ -f "$portfile" && -f "$pidfile" ]]; then
        pid="$(cat "$pidfile" 2>/dev/null || true)"
        if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
            PF_PORT="$(cat "$portfile")"
            return 0
        fi
    fi

    kube_ns port-forward "pod/$pod" ":$remote" --address 127.0.0.1 >"$logf" 2>&1 &
    pid=$!
    printf '%s\n' "$pid" >"$pidfile"
    ledger_add "portforward $pod:$remote"

    port=""
    i=0
    while [[ $i -lt 60 ]]; do
        port="$(sed -n 's/.*Forwarding from 127\.0\.0\.1:\([0-9][0-9]*\).*/\1/p' "$logf" | head -1)"
        if [[ -n "$port" ]]; then
            break
        fi
        if ! kill -0 "$pid" 2>/dev/null; then
            break
        fi
        sleep 0.5
        i=$((i + 1))
    done

    if [[ -z "$port" ]]; then
        drill_fail "port-forward for $pod:$remote did not start; see $logf"
    fi
    printf '%s\n' "$port" >"$portfile"
    PF_PORT="$port"
}

# pf_reset POD PORT discards a cached tunnel for one target: a kubectl
# port-forward process can stay alive while its upstream pod is gone (e.g.
# after a StatefulSet pod is recreated), leaving a tunnel that refuses or
# resets connections. Resetting forces pf_start to open a fresh tunnel.
pf_reset() {
    local key="$1.$2"
    local pidfile="$STATE_DIR/pf/$key.pid"
    if [[ -f "$pidfile" ]]; then
        local pid
        pid="$(cat "$pidfile" 2>/dev/null || true)"
        [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
        rm -f "$pidfile" "$STATE_DIR/pf/$key.port"
    fi
}

# pf_stop_all terminates every tunnel this run started.
pf_stop_all() {
    local pidfile pid
    if [[ -d "$STATE_DIR/pf" ]]; then
        for pidfile in "$STATE_DIR"/pf/*.pid; do
            [[ -f "$pidfile" ]] || continue
            pid="$(cat "$pidfile" 2>/dev/null || true)"
            if [[ -n "$pid" ]]; then
                kill "$pid" 2>/dev/null || true
                wait "$pid" 2>/dev/null || true
            fi
        done
    fi
}

# --------------------------------------------------------------------------
# Disposable credentials and certificates
# --------------------------------------------------------------------------

# drill_gen_password prints a password that satisfies both the chart policy
# (>= 12 characters, three of four character classes, not a placeholder) and
# the server-side policy.
drill_gen_password() {
    printf 'Drill%s!A9' "$(openssl rand -hex 10)"
}

# drill_gen_hex BYTES prints random hex characters.
drill_gen_hex() {
    openssl rand -hex "$1"
}

# drill_gen_certs OUTDIR SAN...
#
# Creates a disposable CA and one leaf certificate carrying both serverAuth
# and clientAuth usage, plus the requested subject alternative names.
drill_gen_certs() {
    local outdir="$1"
    shift
    local san="" name

    for name in "$@"; do
        if [[ -z "$san" ]]; then
            san="DNS:$name"
        else
            san="$san,DNS:$name"
        fi
    done

    mkdir -p "$outdir"
    umask 077

    openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 2 \
        -keyout "$outdir/ca.key" -out "$outdir/ca.crt" \
        -subj "/CN=anubiswatch-drill-ca" \
        -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
        -addext "keyUsage=critical,digitalSignature,keyCertSign,cRLSign" \
        >/dev/null 2>&1 || drill_fail "failed to generate the drill CA"

    openssl req -new -newkey rsa:2048 -nodes -sha256 \
        -keyout "$outdir/leaf.key" -out "$outdir/leaf.csr" \
        -subj "/CN=anubiswatch-drill-peer" \
        >/dev/null 2>&1 || drill_fail "failed to generate the drill leaf key"

    cat >"$outdir/leaf.ext" <<EOF
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth,clientAuth
subjectAltName=$san
EOF

    openssl x509 -req -in "$outdir/leaf.csr" -CA "$outdir/ca.crt" -CAkey "$outdir/ca.key" \
        -CAcreateserial -sha256 -days 2 -extfile "$outdir/leaf.ext" \
        -out "$outdir/leaf.crt" \
        >/dev/null 2>&1 || drill_fail "failed to sign the drill leaf certificate"

    rm -f "$outdir/leaf.csr" "$outdir/leaf.ext" "$outdir/ca.srl"
}

# --------------------------------------------------------------------------
# Diagnostics capture
# --------------------------------------------------------------------------

# drill_capture_diagnostics LABEL saves pod logs, descriptions and events so a
# failed scenario keeps its raw evidence.
drill_capture_diagnostics() {
    local label="$1"
    local dir="$OUTPUT_DIR/evidence/failures/$label"
    local pod

    mkdir -p "$dir"
    kube_ns get pods -o wide >"$dir/pods.txt" 2>&1 || true
    kube_ns get events --sort-by=.lastTimestamp >"$dir/events.txt" 2>&1 || true
    kube_ns describe pods >"$dir/pods-describe.txt" 2>&1 || true
    kube_ns get configmap,secret -o yaml >"$dir/workload.yaml" 2>&1 || true
    kube_ns logs --all-containers --tail=500 --prefix >"$dir/pod-logs.txt" 2>&1 || true
    for pod in $(kube_ns get pods -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true); do
        kube_ns logs "$pod" --all-containers --tail=500 --prefix >"$dir/log-$pod.txt" 2>&1 || true
        kube_ns logs "$pod" --all-containers --previous --tail=200 --prefix >"$dir/log-$pod-previous.txt" 2>&1 || true
    done
    drill_log "diagnostics for $label saved to $dir"
}

# drill_error_logs prints the ERROR-level log lines of the given pods.
drill_error_logs() {
    local pod
    for pod in "$@"; do
        kube_ns logs "$pod" --all-containers --tail=2000 --prefix 2>/dev/null \
            | grep -E '"level":"ERROR"|level=ERROR' || true
    done
}

# --------------------------------------------------------------------------
# Drill API helpers. These are the calls the scenarios make against the
# running instances; they live here so every scenario uses one shape.
# --------------------------------------------------------------------------

# create_soul NAME TARGET [INTERVAL] creates a monitor and stores its id in
# SOUL_ID.
#
# The target is stored but not contacted, so a deliberately unreachable
# address is fine: the drill wants durable objects, not live probes.
#
# The function must be called directly, never in a command substitution: the
# HTTP status and the response body stay in the caller's shell, so a failed
# create can report what the API actually answered instead of a stale status
# from an earlier call.
create_soul() {
    local name="$1" target="$2" interval="${3:-30s}"
    local body
    body="$(printf '{"name":"%s","type":"http","target":"%s","weight":"%s","enabled":true}' \
        "$(json_escape "$name")" "$(json_escape "$target")" "$(json_escape "$interval")")"
    api_call POST /api/v1/souls "$body"
    if [[ "$API_STATUS" != "201" && "$API_STATUS" != "200" ]]; then
        SOUL_ID=""
        return 1
    fi
    SOUL_ID="$(api_json '.id')"
    if [[ -z "$SOUL_ID" ]]; then
        return 1
    fi
    return 0
}

# list_judgment_ids SOUL_ID prints the judgment IDs of a soul, newest first,
# one per line.
list_judgment_ids() {
    local soul_id="$1"
    api_call GET "/api/v1/souls/$soul_id/judgments?limit=100"
    if [[ "$API_STATUS" != "200" ]]; then
        return 1
    fi
    printf '%s' "$API_BODY" | jq -r '(.data // [])[] | .id' 2>/dev/null || true
}

# current_ha_password prints the password the HA admin currently authenticates
# with. Scenario 4 rotates it, so later scenarios must read the rotated value
# rather than the one the runner generated.
current_ha_password() {
    if [[ -f "$STATE_DIR/ha-admin-password" ]]; then
        cat "$STATE_DIR/ha-admin-password"
        return 0
    fi
    printf '%s' "$HA_ADMIN_PASSWORD"
}

# --------------------------------------------------------------------------
# Raft transport framing
# --------------------------------------------------------------------------

# build_identify_frame prints a signed Raft Identify frame.
#
# The cluster runs with an authenticated transport, so every frame carries an
# auth line between the method and the length:
#
#     Identify\n
#     AUTH <origin> <epoch> <seq> <unix-nanos> <hex hmac>\n
#     <length>\n
#     <payload>
#
# The MAC is HMAC-SHA256 under the cluster secret over the domain, the method,
# every auth field and the payload. The Identify method is exempt from the
# membership check, so an external client that knows the secret may ask a
# voter for its identity.
build_identify_frame() {
    local method="Identify"
    local payload='{}'
    local origin="${RAFT_CLIENT_ORIGIN:-drill-client}"
    local epoch seq ts mac
    local input="$TMP_DIR/raft-mac-input"

    epoch=$(( $(drill_epoch) * 1000000 + (RANDOM % 1000000) ))
    seq=1
    ts="$(date +%s%N)"

    {
        printf '%s\n' 'anubis-raft-rpc-v1'
        printf '%s\n' "$method"
        printf '%s\n' "$origin"
        printf '%s\n' "$epoch"
        printf '%s\n' "$seq"
        printf '%s\n' "$ts"
        printf '%s' "$payload"
    } >"$input"

    mac="$(openssl dgst -sha256 -hmac "$HA_CLUSTER_SECRET" -hex "$input" 2>/dev/null | awk '{print $NF}')"
    if [[ -z "$mac" ]]; then
        drill_fail "could not sign the Raft Identify frame"
    fi

    # The auth line carries the MAC between the timestamp and the length
    # line, and the payload follows the length line:
    #
    #     Identify\n
    #     AUTH <origin> <epoch> <seq> <unix-nanos> <hex hmac>\n
    #     <length>\n
    #     <payload>
    printf '%s\n%s %s %s %s %s %s\n%d\n%s' \
        "$method" "AUTH" "$origin" "$epoch" "$seq" "$ts" "$mac" \
        "${#payload}" "$payload"
}
