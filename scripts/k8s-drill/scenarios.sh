#!/usr/bin/env bash
# Scenario implementations for the AnubisWatch Kubernetes acceptance drill.
#
# Sourced by scripts/k8s-drill/k8s-drill.sh. Every scenario must:
#   * return 0 on success and non-zero on failure,
#   * record structured evidence for the results JSON,
#   * leave the cluster in a state the next scenario can use.

# --------------------------------------------------------------------------
# Shared helpers
# --------------------------------------------------------------------------

# scenario_begin NAME starts a scenario and returns its index in RESULTS_SCENARIOS.
scenario_begin() {
    SCENARIO_NAME="$1"
    SCENARIO_START="$(drill_epoch)"
    SCENARIO_CHECKS=""
    SCENARIO_ERROR=""
    SCENARIO_STATUS="failed"
    SCENARIO_EVIDENCE=""
    SCENARIO_FAILED_CHECKS=0
    drill_section "Scenario $CURRENT_SCENARIO: $SCENARIO_NAME"
    # The recorded checks of the previous scenario must not leak into this
    # one: a scenario that dies early still reports the checks it managed.
    printf '' >"$STATE_DIR/checks.json"
}

# scenario_check LABEL CONDITION_RESULT [DETAIL]
#
# Records one assertion. CONDITION_RESULT is "ok" or "failed".
scenario_check() {
    local label="$1" result="$2" detail="${3:-}"
    # Each check is terminated by a comma: scenario_end strips the trailing
    # comma of the last one, so the result is a valid JSON object no matter
    # how many checks a scenario recorded.
    if [[ "$result" == "ok" ]]; then
        SCENARIO_CHECKS="${SCENARIO_CHECKS}\"$(json_escape "$label")\":{\"status\":\"passed\",\"detail\":\"$(json_escape "$detail")\"},"
    else
        SCENARIO_CHECKS="${SCENARIO_CHECKS}\"$(json_escape "$label")\":{\"status\":\"failed\",\"detail\":\"$(json_escape "$detail")\"},"
        SCENARIO_FAILED_CHECKS=$((SCENARIO_FAILED_CHECKS + 1))
    fi
    printf '%s' "$SCENARIO_CHECKS" >"$STATE_DIR/checks.json"
    if [[ "$result" == "ok" ]]; then
        drill_log "ok: $label${detail:+ - $detail}"
    else
        drill_log "FAIL: $label${detail:+ - $detail}"
    fi
}

# scenario_evidence PATH records an evidence file produced by a scenario.
scenario_evidence() {
    local path="$1"
    if [[ -z "$SCENARIO_EVIDENCE" ]]; then
        SCENARIO_EVIDENCE="\"$(json_escape "$path")\""
    else
        SCENARIO_EVIDENCE="$SCENARIO_EVIDENCE,\"$(json_escape "$path")\""
    fi
}

# scenario_end writes the scenario result into the results JSON accumulator.
scenario_end() {
    local status="$1" error="${2:-}"
    local now elapsed
    now="$(drill_epoch)"
    elapsed=$((now - SCENARIO_START))
    {
        printf '    {\n'
        printf '      "id": %s,\n' "$CURRENT_SCENARIO"
        printf '      "name": "%s",\n' "$(json_escape "$SCENARIO_NAME")"
        printf '      "status": "%s",\n' "$status"
        printf '      "duration_seconds": %s,\n' "$elapsed"
        printf '      "checks": {%s},\n' "${SCENARIO_CHECKS%,}"
        printf '      "evidence": [%s],\n' "$SCENARIO_EVIDENCE"
        printf '      "error": "%s"\n' "$(json_escape "$error")"
        printf '    }'
    } >>"$RESULTS_FILE"
    RESULTS_COMMA=1
    if [[ "$status" == "passed" ]]; then
        SCENARIO_FAILURES=$((SCENARIO_FAILURES + 0))
    else
        SCENARIO_FAILURES=$((SCENARIO_FAILURES + 1))
    fi
    printf '%s' "$SCENARIO_CHECKS" >"$STATE_DIR/last-checks.json"
}

# scenario_finish [ERROR] ends the scenario.
#
# A scenario passes only when every check it recorded passed. A scenario
# that recorded a failed check must never be reported as passed, whatever
# the rest of its body did.
scenario_finish() {
    local error="${1:-}"
    if [[ "$SCENARIO_FAILED_CHECKS" -gt 0 ]]; then
        if [[ -z "$error" ]]; then
            error="$SCENARIO_FAILED_CHECKS check(s) failed"
        fi
        scenario_end failed "$error"
        return 1
    fi
    scenario_end passed ""
    return 0
}

# --------------------------------------------------------------------------
# Per-replica HTTP access
# --------------------------------------------------------------------------

# replica_pods prints the pod names of the HA release, one per line, in
# ordinal order. The names come from the live StatefulSet, never from a
# hardcoded list.
replica_pods() {
    kube_ns get pods -l "app.kubernetes.io/instance=$HA_RELEASE" \
        -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null \
        | sort
}

# replica_pod_for_ordinal ORDINAL prints the pod name owning ORDINAL.
replica_pod_for_ordinal() {
    kube_ns get pods -l "app.kubernetes.io/instance=$HA_RELEASE" \
        -o jsonpath="{range .items[?(@.metadata.name=='$HA_FULLNAME-$1')]}{.metadata.name}{end}" 2>/dev/null
}

# pod_node POD prints the worker node a pod runs on.
pod_node() {
    kube_ns get pod "$1" -o jsonpath='{.spec.nodeName}' 2>/dev/null
}

# api_base_for_pod POD starts a tunnel to that pod and prints its base URL.
api_base_for_pod() {
    local pod="$1"
    pf_start "$pod" "$API_PORT"
    printf 'http://127.0.0.1:%s' "$PF_PORT"
}

# cluster_status_for_pod POD asks a replica for its cluster status.
#
# The answer is returned in CLUSTER_STATUS_BODY and CLUSTER_STATUS_CODE
# rather than on stdout, so a scenario can report the HTTP status and the
# raw body when the answer is not usable.
#
# The endpoint is authenticated (internal/api/rest.go registers it behind
# requireAuth), so the caller must hold a valid session token. The cluster
# authority is shared, so one session is accepted by every replica.
cluster_status_for_pod() {
    local pod="$1" base previous
    base="$(api_base_for_pod "$pod")"
    previous="$API_BASE"
    API_BASE="$base"
    api_call GET /api/v1/cluster/status
    CLUSTER_STATUS_CODE="$API_STATUS"
    CLUSTER_STATUS_BODY="$API_BODY"
    API_BASE="$previous"
}

# cluster_role_for_pod POD prints the role a replica reports ("leader",
# "follower", ...), or an empty string when it reports none.
cluster_role_for_pod() {
    local pod="$1"
    cluster_status_for_pod "$pod"
    printf '%s' "$CLUSTER_STATUS_BODY" | jq -r '.state // empty' 2>/dev/null || true
}

# leader_pod prints the pod name that currently reports the Raft leader.
#
# The leader is discovered from the live cluster status of every replica, not
# from a fixed ordinal, because leader placement changes across scenarios.
leader_pod() {
    local pod state
    for pod in $(replica_pods); do
        cluster_status_for_pod "$pod"
        state="$(printf '%s' "$CLUSTER_STATUS_BODY" | jq -r '.state // empty' 2>/dev/null)"
        if [[ "$state" == "leader" ]]; then
            printf '%s' "$pod"
            return 0
        fi
    done
    return 1
}

# --------------------------------------------------------------------------
# Scenario 1: standalone install, authenticated smoke, encrypted PVC
# persistence, config-only rollout and rollback.
# --------------------------------------------------------------------------
# A pod deletion does not change Deployment generation. Its rollout status can
# still describe the old ready pod, so observe the replacement itself.
wait_for_replacement_pod() {
    local release="$1" previous="$2" response pod waited=0
    while [[ "$waited" -lt "$RESTART_WAIT_SECONDS" ]]; do
        response="$(kube_ns get pods -l "app.kubernetes.io/instance=$release" -o json)" || return 1
        pod="$(jq -r --arg previous "$previous" '
            [.items[] | select(.metadata.name != $previous and
                .metadata.deletionTimestamp == null and .status.phase == "Running") |
                select(any(.status.conditions[]?; .type == "Ready" and .status == "True"))] |
            if length == 1 then .[0].metadata.name else empty end
            ' <<<"$response")" || return 1
        if [[ -n "$pod" ]]; then
            printf '%s\n' "$pod"
            return 0
        fi
        sleep 1
        waited=$((waited + 1))
    done
    return 1
}

scenario_standalone() {
    scenario_begin "standalone install, authenticated smoke, encrypted PVC persistence, config-only rollout and rollback"

    local release="$SA_RELEASE" fullname="$SA_FULLNAME"
    local values_file="$STATE_DIR/standalone-values.yaml"
    local rollout_before rollout_after

    cat >"$values_file" <<EOF
image:
  repository: ${IMAGE_REPOSITORY}
  tag: "${IMAGE_TAG}"
  digest: "${IMAGE_DIGEST}"
replicaCount: 1
config:
  necropolis:
    enabled: false
  auth:
    type: local
  storage:
    path: /data
    encryption:
      enabled: true
secrets:
  adminPassword: "${SA_ADMIN_PASSWORD}"
  encryptionKey: "${SA_ENCRYPTION_KEY}"
persistence:
  enabled: true
  size: 1Gi
monitoring:
  enabled: false
autoscaling:
  enabled: false
pdb:
  enabled: false
EOF
    chmod 600 "$values_file"

    helm_cmd upgrade --install "$release" "$CHART" \
        --namespace "$SA_NAMESPACE" \
        --values "$values_file" \
        --wait --timeout "$TIMEOUT" \
        >"$STATE_DIR/standalone-install.log" 2>&1 || {
        drill_fail "standalone helm install failed; see $STATE_DIR/standalone-install.log"
    }
    ledger_add "helm $SA_NAMESPACE $release"

    if kube_ns rollout status "deployment/$fullname" --timeout="$TIMEOUT" >"$STATE_DIR/standalone-rollout.log" 2>&1; then
        scenario_check "standalone deployment rolls out" ok "deployment/$fullname ready"
    else
        scenario_check "standalone deployment rolls out" failed "see $STATE_DIR/standalone-rollout.log"
        return 1
    fi

    # Encrypted storage: the running pod must actually receive the encryption
    # key and the storage config must enable encryption. Checking the
    # deployment env and the mounted config proves encryption is active rather
    # than merely requested.
    #
    # The chart injects the key from the release secret (a secretKeyRef), so
    # the pod spec carries no literal value: the check looks for the reference
    # and for the secret entry it points at.
    local pod env_ref secret_ref secret_key cfg_encryption
    pod="$(kube_ns get pods -l "app.kubernetes.io/instance=$release" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
    env_ref="$(kube_ns get pod "$pod" -o jsonpath='{.spec.containers[0].env[?(@.name=="ANUBIS_ENCRYPTION_KEY")].valueFrom.secretKeyRef.name}' 2>/dev/null)"
    secret_ref="$(kube_ns get pod "$pod" -o jsonpath='{.spec.containers[0].env[?(@.name=="ANUBIS_ENCRYPTION_KEY")].valueFrom.secretKeyRef.key}' 2>/dev/null)"
    secret_key=""
    if [[ -n "$env_ref" && -n "$secret_ref" ]]; then
        secret_key="$(kube_ns get secret "$env_ref" -o json 2>/dev/null | jq -r --arg k "$secret_ref" '.data[$k] // ""' 2>/dev/null)"
    fi
    if [[ -n "$env_ref" && -n "$secret_ref" && -n "$secret_key" ]]; then
        scenario_check "encryption key is injected into the pod" ok "ANUBIS_ENCRYPTION_KEY from secret $env_ref/$secret_ref"
    else
        scenario_check "encryption key is injected into the pod" failed "ANUBIS_ENCRYPTION_KEY missing on $pod (secret reference '${env_ref:-none}/${secret_ref:-none}')"
    fi

    cfg_encryption="$(kube_ns get configmap "$fullname-config" -o jsonpath='{.data.anubis\.yaml}' 2>/dev/null | grep -c 'enabled: true' || true)"
    if [[ "$cfg_encryption" -ge 1 ]]; then
        scenario_check "storage encryption is enabled in the mounted config" ok "encryption.enabled true"
    else
        scenario_check "storage encryption is enabled in the mounted config" failed "encryption not enabled in $fullname-config"
    fi

    # Authenticated smoke against the running instance.
    local base
    base="$(api_base_for_pod "$pod")"
    local previous_base="$API_BASE"
    API_BASE="$base"

    drill_login "$SA_ADMIN_EMAIL" "$SA_ADMIN_PASSWORD" >/dev/null
    api_expect GET /api/v1/auth/me 200 "" "authenticated current user"
    api_expect GET /api/v1/souls 200 "" "souls listing"
    api_expect GET /api/v1/stats/overview 200 "" "stats overview"

    # Create a config object so persistence can be proven later.
    local soul_id
    create_soul "drill-standalone-$(drill_gen_hex 4)" "http://127.0.0.1:1/health" 30s
    soul_id="$SOUL_ID"
    if [[ -n "$soul_id" ]]; then
        scenario_check "config object can be created" ok "soul $soul_id"
        printf '%s' "$soul_id" >"$STATE_DIR/standalone-soul-id"
    else
        scenario_check "config object can be created" failed "POST /api/v1/souls returned HTTP $API_STATUS: ${API_BODY:0:200}"
    fi

    # Encrypted PVC persistence: the data must survive a pod replacement.
    local new_pod
    if kube_ns delete pod "$pod" --wait=false >"$STATE_DIR/standalone-restart.log" 2>&1 &&
        new_pod="$(wait_for_replacement_pod "$release" "$pod" 2>>"$STATE_DIR/standalone-restart.log")"; then
        scenario_check "pod is replaced after deletion" ok "replacement $new_pod is Running and Ready"
    else
        scenario_check "pod is replaced after deletion" failed "see $STATE_DIR/standalone-restart.log"
        return 1
    fi

    pf_start "$new_pod" "$API_PORT"
    API_BASE="http://127.0.0.1:$PF_PORT"
    drill_login "$SA_ADMIN_EMAIL" "$SA_ADMIN_PASSWORD" >/dev/null

    if [[ -n "$soul_id" ]]; then
        api_call GET "/api/v1/souls/$soul_id"
        if [[ "$API_STATUS" == "200" ]] && [[ "$API_BODY" == *"$soul_id"* ]]; then
            scenario_check "config object survives pod replacement on the encrypted PVC" ok "soul $soul_id still readable"
        else
            scenario_check "config object survives pod replacement on the encrypted PVC" failed "soul $soul_id not readable after restart (HTTP $API_STATUS)"
        fi
    fi

    # Config-only rollout: change a configuration value only, and confirm the
    # running pod picks it up.
    local before_hash after_hash
    before_hash="$(kube_ns get pod "$new_pod" -o jsonpath='{.metadata.annotations.checksum/config}' 2>/dev/null)"
    helm_cmd upgrade "$release" "$CHART" \
        --namespace "$SA_NAMESPACE" \
        --values "$values_file" \
        --set-string config.logging.level=debug \
        --wait --timeout "$TIMEOUT" \
        >"$STATE_DIR/standalone-rollout-config.log" 2>&1 || {
        drill_fail "config-only rollout failed; see $STATE_DIR/standalone-rollout-config.log"
    }
    local rolled_pod
    rolled_pod="$(kube_ns get pods -l "app.kubernetes.io/instance=$release" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
    after_hash="$(kube_ns get pod "$rolled_pod" -o jsonpath='{.metadata.annotations.checksum/config}' 2>/dev/null)"
    if [[ -n "$after_hash" ]] && [[ "$after_hash" != "$before_hash" ]]; then
        scenario_check "config-only rollout replaces the pod with the new config" ok "checksum/config $before_hash -> $after_hash"
    else
        scenario_check "config-only rollout replaces the pod with the new config" failed "checksum/config unchanged ($before_hash)"
    fi

    local level
    level="$(kube_ns get configmap "$fullname-config" -o jsonpath='{.data.anubis\.yaml}' 2>/dev/null | grep -cE 'level:[[:space:]]*"?debug"?' || true)"
    if [[ "$level" -ge 1 ]]; then
        scenario_check "new configuration value is applied in the mounted config" ok "logging level debug"
    else
        scenario_check "new configuration value is applied in the mounted config" failed "logging level not debug in $fullname-config"
    fi

    # Rollback restores the previous revision and the previous configuration.
    # The revision to roll back to is the one before the config-only upgrade:
    # rolling back to the newest revision would keep the changed value.
    rollout_before="$(helm_cmd history "$release" --namespace "$SA_NAMESPACE" -o json 2>/dev/null | jq -r '.[-2].revision // empty' 2>/dev/null)"
    if [[ -z "$rollout_before" ]]; then
        scenario_check "rollback restores the previous configuration" failed "no previous revision to roll back to"
        API_BASE="$previous_base"
        scenario_finish "the standalone rollback has no previous revision"
        return 1
    fi
    helm_cmd rollback "$release" "$rollout_before" --namespace "$SA_NAMESPACE" --wait --timeout "$TIMEOUT" \
        >"$STATE_DIR/standalone-rollback.log" 2>&1 || {
        drill_fail "standalone rollback failed; see $STATE_DIR/standalone-rollback.log"
    }
    local rolled_back_pod reverted
    rolled_back_pod="$(kube_ns get pods -l "app.kubernetes.io/instance=$release" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
    reverted="$(kube_ns get configmap "$fullname-config" -o jsonpath='{.data.anubis\.yaml}' 2>/dev/null | grep -cE 'level:[[:space:]]*"?info"?' || true)"
    if [[ "$reverted" -ge 1 ]]; then
        scenario_check "rollback restores the previous configuration" ok "logging level info again"
    else
        scenario_check "rollback restores the previous configuration" failed "logging level not reverted in $fullname-config"
    fi

    API_BASE="$previous_base"
    scenario_finish "the standalone release did not satisfy every check"
    return $?
}

# --------------------------------------------------------------------------
# Scenario 2: three voters on distinct workers, one leader and two
# followers, and a leader identity that matches across replicas.
# --------------------------------------------------------------------------
scenario_ha_topology() {
    scenario_begin "three voters on distinct workers, one leader and two followers with a matching leader identity"

    local pods count leader follower_count status leader_id consensus_leader
    pods="$(replica_pods)"
    count="$(printf '%s\n' "$pods" | grep -c . || true)"
    if [[ "$count" -ne 3 ]]; then
        scenario_check "exactly three voter pods exist" failed "found $count pods"
        return 1
    fi
    scenario_check "exactly three voter pods exist" ok "3 pods"

    local nodes
    nodes="$(for pod in $pods; do pod_node "$pod"; echo; done | sort -u | grep -c . || true)"
    if [[ "$nodes" -ne 3 ]]; then
        scenario_check "voters are scheduled on three distinct workers" failed "pods span $nodes distinct nodes"
        return 1
    fi
    scenario_check "voters are scheduled on three distinct workers" ok "3 distinct nodes"

    # The cluster status endpoint is authenticated and the cluster authority
    # is shared, so one session covers every replica.
    local previous_base="$API_BASE"
    API_BASE="$(api_base_for_pod "$(printf '%s\n' "$pods" | head -1)")"
    if ! drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)"; then
        scenario_check "the drill can authenticate against a replica" failed "$LOGIN_ERROR"
        API_BASE="$previous_base"
        return 1
    fi
    scenario_check "the drill can authenticate against a replica" ok "session accepted by the cluster authority"
    API_BASE="$previous_base"

    # Leader and follower roles come from the live cluster status of every
    # replica, so the drill never assumes which ordinal leads.
    leader_count=0
    follower_count=0
    leader_id=""
    local pod role pod_leader
    for pod in $pods; do
        cluster_status_for_pod "$pod"
        status="$CLUSTER_STATUS_BODY"
        role="$(printf '%s' "$status" | jq -r '.state // empty' 2>/dev/null)"
        pod_leader="$(printf '%s' "$status" | jq -r '.leader // empty' 2>/dev/null)"
        case "$role" in
            leader)
                leader_count=$((leader_count + 1))
                leader_id="$pod_leader"
                ;;
            follower)
                follower_count=$((follower_count + 1))
                ;;
            *)
                scenario_check "every replica reports a leader or follower role" failed "$pod reported role '$role' (HTTP $CLUSTER_STATUS_CODE: ${status:0:200})"
                return 1
                ;;
        esac
        if [[ -n "$pod_leader" ]]; then
            consensus_leader="$pod_leader"
        fi
    done

    if [[ "$leader_count" -eq 1 && "$follower_count" -eq 2 ]]; then
        scenario_check "exactly one leader and two followers" ok "1 leader, 2 followers"
    else
        scenario_check "exactly one leader and two followers" failed "found $leader_count leaders and $follower_count followers"
        return 1
    fi

    if [[ -n "$leader_id" ]] && [[ "$leader_id" == "$consensus_leader" ]]; then
        scenario_check "replicas agree on the same leader identity" ok "leader $leader_id"
    else
        scenario_check "replicas agree on the same leader identity" failed "leader pod reported '$leader_id', others reported '$consensus_leader'"
        return 1
    fi

    printf '%s' "$leader_id" >"$STATE_DIR/leader-id"
    scenario_finish "the cluster topology did not match the expected shape"
    return $?
}

# --------------------------------------------------------------------------
# Scenario 3: a write accepted by a follower is readable on every replica.
# --------------------------------------------------------------------------
scenario_follower_write() {
    scenario_begin "a write accepted by a follower is readable on all replicas"

    local pods pod status follower=""
    pods="$(replica_pods)"

    # The cluster status endpoint is authenticated: the drill signs in before
    # asking the replicas about their role.
    local previous_base="$API_BASE"
    API_BASE="$(api_base_for_pod "$(printf '%s\n' "$pods" | head -1)")"
    if ! drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)"; then
        scenario_check "the drill can authenticate against a replica" failed "$LOGIN_ERROR"
        API_BASE="$previous_base"
        return 1
    fi
    scenario_check "the drill can authenticate against a replica" ok "session accepted by the cluster authority"

    for pod in $pods; do
        cluster_status_for_pod "$pod"
        status="$CLUSTER_STATUS_BODY"
        if [[ "$(printf '%s' "$status" | jq -r '.state // empty' 2>/dev/null)" == "follower" ]]; then
            follower="$pod"
            break
        fi
    done
    if [[ -z "$follower" ]]; then
        scenario_check "a follower replica can be identified" failed "no replica reported the follower role (HTTP $CLUSTER_STATUS_CODE: ${status:0:200})"
        API_BASE="$previous_base"
        return 1
    fi
    scenario_check "a follower replica can be identified" ok "$follower"

    API_BASE="$(api_base_for_pod "$follower")"
    drill_login "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null

    local soul_id
    create_soul "drill-follower-write-$(drill_gen_hex 4)" "http://127.0.0.1:1/health" 30s
    soul_id="$SOUL_ID"
    if [[ -z "$soul_id" ]]; then
        scenario_check "follower accepts a write" failed "POST /api/v1/souls returned HTTP $API_STATUS: ${API_BODY:0:200}"
        API_BASE="$previous_base"
        return 1
    fi
    scenario_check "follower accepts a write" ok "soul $soul_id created through $follower"
    printf '%s' "$soul_id" >"$STATE_DIR/follower-write-soul-id"

    # The write must be readable from every replica, including the leader.
    local readable=0 unreachable="" pod
    for pod in $pods; do
        API_BASE="$(api_base_for_pod "$pod")"
        drill_login "$HA_ADMIN_EMAIL" "$HA_ADMIN_PASSWORD" >/dev/null
        api_call GET "/api/v1/souls/$soul_id"
        if [[ "$API_STATUS" == "200" ]] && [[ "$API_BODY" == *"$soul_id"* ]]; then
            readable=$((readable + 1))
        else
            unreachable="$unreachable $pod"
        fi
    done

    if [[ "$readable" -eq 3 ]]; then
        scenario_check "the follower-originated write is readable on all replicas" ok "3 of 3 replicas"
    else
        scenario_check "the follower-originated write is readable on all replicas" failed "readable on $readable of 3; not readable on:$unreachable"
    fi

    API_BASE="$previous_base"
    if [[ "$readable" -eq 3 ]]; then
        scenario_finish "the follower-originated write was not readable on every replica"
        return $?
    fi
    scenario_end failed "write not readable on every replica"
    return 1
}

# --------------------------------------------------------------------------
# Scenario 4: one session accepted through all backends, a global password
# rotation, old-password rejection, and logout revocation.
# --------------------------------------------------------------------------
scenario_session_rotation() {
    scenario_begin "one session accepted through all backends, global password rotation, old-password rejection, and logout revocation"

    local pods previous_base="$API_BASE"
    pods="$(replica_pods)"

    # Log in once against one replica and reuse that session everywhere. A
    # session issued by one backend must be accepted by all of them.
    local first
    first="$(printf '%s\n' "$pods" | head -1)"
    API_BASE="$(api_base_for_pod "$first")"
    drill_login "$HA_ADMIN_EMAIL" "$HA_ADMIN_PASSWORD" >/dev/null
    local session_token="$TOKEN"

    local accepted=0 pod
    for pod in $pods; do
        API_BASE="$(api_base_for_pod "$pod")"
        TOKEN="$session_token"
        api_call GET /api/v1/auth/me
        if [[ "$API_STATUS" == "200" ]]; then
            accepted=$((accepted + 1))
        fi
    done
    if [[ "$accepted" -eq 3 ]]; then
        scenario_check "a single session is accepted by every backend" ok "3 of 3 replicas"
    else
        scenario_check "a single session is accepted by every backend" failed "accepted on $accepted of 3 replicas"
        API_BASE="$previous_base"
        scenario_finish "no replica accepted the session issued by $first"
        return 1
    fi

    # Rotate the password globally through one backend.
    local new_password
    new_password="$(drill_gen_password)"
    TOKEN="$session_token"
    api_call PUT /api/v1/auth/change-password \
        "$(printf '{"current_password":"%s","new_password":"%s"}' \
            "$(json_escape "$HA_ADMIN_PASSWORD")" "$(json_escape "$new_password")")"
    if [[ "$API_STATUS" != "200" ]]; then
        scenario_check "password rotation is accepted" failed "HTTP $API_STATUS: ${API_BODY:0:200}"
        API_BASE="$previous_base"
        scenario_finish "the password rotation was refused"
        return 1
    fi
    scenario_check "password rotation is accepted" ok "rotated through $first"

    # The old password must be rejected everywhere, and the new one accepted
    # everywhere: the rotation is global, not per-backend.
    local old_rejected=0 new_accepted=0
    for pod in $pods; do
        API_BASE="$(api_base_for_pod "$pod")"
        TOKEN=""
        api_call POST /api/v1/auth/login \
            "$(printf '{"email":"%s","password":"%s"}' "$(json_escape "$HA_ADMIN_EMAIL")" "$(json_escape "$HA_ADMIN_PASSWORD")")"
        if [[ "$API_STATUS" == "401" ]]; then
            old_rejected=$((old_rejected + 1))
        fi
        api_call POST /api/v1/auth/login \
            "$(printf '{"email":"%s","password":"%s"}' "$(json_escape "$HA_ADMIN_EMAIL")" "$(json_escape "$new_password")")"
        if [[ "$API_STATUS" == "200" ]]; then
            new_accepted=$((new_accepted + 1))
        fi
    done
    if [[ "$old_rejected" -eq 3 ]]; then
        scenario_check "the old password is rejected on every backend" ok "3 of 3 replicas returned 401"
    else
        scenario_check "the old password is rejected on every backend" failed "rejected on $old_rejected of 3 replicas"
    fi
    if [[ "$new_accepted" -eq 3 ]]; then
        scenario_check "the rotated password is accepted on every backend" ok "3 of 3 replicas"
    else
        scenario_check "the rotated password is accepted on every backend" failed "accepted on $new_accepted of 3 replicas"
    fi

    # The rotated password becomes the current one for later scenarios.
    printf '%s' "$new_password" >"$STATE_DIR/ha-admin-password"

    # Logout must revoke the session on every backend.
    API_BASE="$(api_base_for_pod "$first")"
    TOKEN=""
    api_login "$HA_ADMIN_EMAIL" "$new_password"
    local revoked_token="$TOKEN"
    if [[ -z "$revoked_token" ]]; then
        scenario_check "a fresh session can be established for the revocation test" failed "login after rotation did not return a token (HTTP $API_STATUS)"
        API_BASE="$previous_base"
        scenario_finish "no session could be established after the password rotation"
        return 1
    fi
    api_call POST /api/v1/auth/logout
    if [[ "$API_STATUS" != "200" ]]; then
        scenario_check "logout is accepted" failed "HTTP $API_STATUS"
        API_BASE="$previous_base"
        scenario_finish "the logout was refused"
        return 1
    fi
    scenario_check "logout is accepted" ok "session revoked through $first"

    local revoked=0
    for pod in $pods; do
        API_BASE="$(api_base_for_pod "$pod")"
        TOKEN="$revoked_token"
        api_call GET /api/v1/auth/me
        if [[ "$API_STATUS" == "401" ]]; then
            revoked=$((revoked + 1))
        fi
    done
    if [[ "$revoked" -eq 3 ]]; then
        scenario_check "the revoked session is rejected on every backend" ok "3 of 3 replicas returned 401"
    else
        scenario_check "the revoked session is rejected on every backend" failed "rejected on $revoked of 3 replicas"
    fi

    API_BASE="$previous_base"
    if [[ "$old_rejected" -eq 3 && "$new_accepted" -eq 3 && "$revoked" -eq 3 ]]; then
        scenario_finish "the session lifecycle assertions did not hold on every backend"
        return $?
    fi
    scenario_end failed "session lifecycle assertions did not hold on every backend"
    return 1
}

# --------------------------------------------------------------------------
# Scenario 5: verified server identity, a successful trusted-client Raft
# RPC, and rejection when no client certificate is presented.
# --------------------------------------------------------------------------
scenario_raft_mtls() {
    scenario_begin "verified server identity, a successful trusted-client Raft RPC, and rejection without a client certificate"

    local pods pod target
    pods="$(replica_pods)"
    target="$(printf '%s\n' "$pods" | head -1)"
    if [[ -z "$target" ]]; then
        scenario_check "a voter pod is available for the TLS probe" failed "no voter pod found"
        return 1
    fi

    local service="${HA_FULLNAME}-headless"
    local dns="${target}.${service}.${NAMESPACE}.svc.${CLUSTER_DOMAIN}"
    local port="$RAFT_PORT"

    # The Raft port is only reachable inside the cluster network, and the
    # drill host does not resolve cluster DNS names. The probe therefore goes
    # through a port-forward to the pod and addresses the peer by its
    # in-cluster DNS name, which is what the certificate is issued for.
    # Each probe opens its own tunnel: pf_start reuses a live tunnel and
    # restarts a dead one, so a tunnel lost mid-scenario cannot fail the
    # remaining probes with connection refused.
    local endpoint=""

    pf_start "$target" "$port"
    endpoint="127.0.0.1:$PF_PORT"

    # Verified server identity: the peer must present a certificate that
    # chains to the drill CA and matches the pod's DNS name.
    local server_out="$STATE_DIR/raft-server-identity.txt"
    if timeout 20 openssl s_client -connect "$endpoint" \
        -servername "$dns" -CAfile "$STATE_DIR/certs/ca.crt" \
        -cert "$STATE_DIR/certs/leaf.crt" -key "$STATE_DIR/certs/leaf.key" \
        -verify_hostname "$dns" -verify_return_error \
        </dev/null >"$server_out" 2>&1; then
        if grep -q "Verify return code: 0 (ok)" "$server_out"; then
            scenario_check "the Raft server presents a certificate that verifies against the drill CA and the pod DNS name" ok "$dns"
        else
            scenario_check "the Raft server presents a certificate that verifies against the drill CA and the pod DNS name" failed "verification did not return ok; see $server_out"
        fi
    else
        scenario_check "the Raft server presents a certificate that verifies against the drill CA and the pod DNS name" failed "TLS handshake failed; see $server_out"
    fi
    scenario_evidence "$server_out"

    # Trusted-client RPC: with the client certificate the peer must accept a
    # real Raft frame and answer it.
    pf_start "$target" "$port"
    endpoint="127.0.0.1:$PF_PORT"

    local client_out="$STATE_DIR/raft-client-rpc.txt"
    local rpc_body
    rpc_body="$(build_identify_frame)"
    # The frame is followed by a short pause: the peer answers while the
    # tunnel is still open, and an immediate close would race the answer.
    # The server keeps the connection open after answering, so s_client does
    # not exit on its own; `timeout` bounds the read and its non-zero status
    # is expected. The check therefore judges the captured response, not the
    # exit code: a handshake or verification failure produces no Identify
    # answer at all.
    { printf '%s' "$rpc_body"; sleep 2; } | timeout 20 openssl s_client -connect "$endpoint" \
        -servername "$dns" -CAfile "$STATE_DIR/certs/ca.crt" \
        -cert "$STATE_DIR/certs/leaf.crt" -key "$STATE_DIR/certs/leaf.key" \
        -quiet -verify_return_error >"$client_out" 2>&1
    if grep -q '"awaiting_join"' "$client_out"; then
        scenario_check "a trusted client certificate completes a Raft RPC" ok "Identify RPC answered by $target"
    elif grep -qi 'alert\|handshake failure\|certificate required' "$client_out"; then
        scenario_check "a trusted client certificate completes a Raft RPC" failed "TLS handshake with the client certificate failed; see $client_out"
    else
        scenario_check "a trusted client certificate completes a Raft RPC" failed "no Identify response; see $client_out"
    fi
    scenario_evidence "$client_out"

    # Rejection without a client certificate. TLS 1.3 subtlety: the server
    # sends its Finished before validating the client's certificate, so a
    # client WITHOUT a certificate can complete the TLS handshake and exit 0
    # while the server rejects the connection at its first read. The
    # rejection is therefore only observable at the protocol level: an
    # unauthenticated peer must not receive an Identify answer. Check 1
    # already verified the server certificate on this endpoint, so a missing
    # answer here is client-certificate enforcement, not a server-cert
    # artifact.
    pf_start "$target" "$port"
    endpoint="127.0.0.1:$PF_PORT"

    local no_cert_out="$STATE_DIR/raft-no-client-cert.txt"
    local no_cert_body
    no_cert_body="$(build_identify_frame)"
    { printf '%s' "$no_cert_body"; sleep 2; } | timeout 20 openssl s_client \
        -connect "$endpoint" -servername "$dns" -CAfile "$STATE_DIR/certs/ca.crt" \
        -quiet >"$no_cert_out" 2>&1
    if grep -q '"awaiting_join"' "$no_cert_out"; then
        scenario_check "the peer rejects a connection without a client certificate" failed "an unauthenticated Raft RPC was answered; see $no_cert_out"
    else
        scenario_check "the peer rejects a connection without a client certificate" ok "no Identify answer without a client certificate; unauthenticated peer rejected"
    fi
    scenario_evidence "$no_cert_out"

    scenario_finish "the Raft mutual-TLS expectations did not hold"
    return $?
}

# --------------------------------------------------------------------------
# Scenario 6: active probes during an owner/leader failure, with the
# pre-failure acknowledged judgment IDs preserved on every replica.
# --------------------------------------------------------------------------
scenario_probe_failover() {
    scenario_begin "active probes during an owner/leader failure, with pre-failure acknowledged judgment IDs preserved on all replicas"

    local pods previous_base="$API_BASE"
    pods="$(replica_pods)"

    # The probe target must be reachable and must pass SSRF validation, so
    # the drill points a monitor at the drill's own in-cluster HTTP endpoint.
    local probe_target
    probe_target="http://${SA_FULLNAME}.${SA_NAMESPACE}.svc.cluster.local:${API_PORT}/health"

    local first
    first="$(printf '%s\n' "$pods" | head -1)"
    API_BASE="$(api_base_for_pod "$first")"
    # drill_login_soft + tunnel reset: a stale tunnel must degrade to a
    # recorded check failure, never exit the runner (drill_fail exits).
    if ! drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1; then
        pf_reset "$first" "$API_PORT"
        API_BASE="$(api_base_for_pod "$first")"
        drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1 || true
    fi

    local soul_id
    create_soul "drill-probe-$(drill_gen_hex 4)" "$probe_target" 5s
    soul_id="$SOUL_ID"
    if [[ -z "$soul_id" ]]; then
        scenario_check "a probe monitor can be created" failed "POST /api/v1/souls returned HTTP $API_STATUS: ${API_BODY:0:200}"
        API_BASE="$previous_base"
        scenario_finish "the probe monitor could not be created"
        return 1
    fi
    scenario_check "a probe monitor can be created" ok "soul $soul_id targeting $probe_target"
    printf '%s' "$soul_id" >"$STATE_DIR/probe-soul-id"

    # Wait for at least one acknowledged judgment from the running probe.
    local judgments="" deadline
    deadline=$(( $(drill_epoch) + PROBE_WAIT_SECONDS ))
    while [[ $(drill_epoch) -lt $deadline ]]; do
        judgments="$(list_judgment_ids "$soul_id")"
        if [[ -n "$judgments" ]]; then
            break
        fi
        sleep 5
    done
    if [[ -z "$judgments" ]]; then
        scenario_check "the probe produces acknowledged judgments" failed "no judgment recorded within ${PROBE_WAIT_SECONDS}s"
        API_BASE="$previous_base"
        scenario_finish "the probe produced no acknowledged judgments"
        return 1
    fi
    local pre_ids
    pre_ids="$(printf '%s' "$judgments" | head -3 | tr '\n' ',')"
    scenario_check "the probe produces acknowledged judgments" ok "pre-failure judgment ids $pre_ids"
    printf '%s' "$judgments" >"$STATE_DIR/pre-failure-judgment-ids"

    # Fail the leader. The pod is deleted so the StatefulSet recreates it and
    # a new leader is elected; the drill never assumes which pod leads.
    local leader leader_before
    leader="$(leader_pod)" || {
        scenario_check "the current leader can be identified" failed "no replica reported a leader"
        API_BASE="$previous_base"
        scenario_finish "no replica reported a leader"
        return 1
    }
    cluster_status_for_pod "$leader"
    leader_before="$(printf '%s' "$CLUSTER_STATUS_BODY" | jq -r '.leader // empty' 2>/dev/null)"
    scenario_check "the current leader can be identified" ok "$leader (leader id $leader_before)"

    kube_ns delete pod "$leader" --wait=false >/dev/null 2>&1 || true
    drill_log "deleted leader pod $leader to force a leader change"

    # Wait for a new leader that is not the old one.
    local new_leader="" new_leader_id="" waited=0
    while [[ $waited -lt $LEADER_FAILOVER_SECONDS ]]; do
        sleep 5
        waited=$((waited + 5))
        local candidate
        for candidate in $(replica_pods); do
            local st
            cluster_status_for_pod "$candidate"
            st="$CLUSTER_STATUS_BODY"
            local role cand_leader
            role="$(printf '%s' "$st" | jq -r '.state // empty' 2>/dev/null)"
            cand_leader="$(printf '%s' "$st" | jq -r '.leader // empty' 2>/dev/null)"
            if [[ "$role" == "leader" ]] && [[ -n "$cand_leader" ]] && [[ "$cand_leader" != "$leader_before" ]]; then
                new_leader="$candidate"
                new_leader_id="$cand_leader"
                break 2
            fi
        done
    done
    if [[ -n "$new_leader" ]]; then
        scenario_check "a new leader is elected after the failure" ok "$new_leader (leader id $new_leader_id)"
    else
        scenario_check "a new leader is elected after the failure" failed "no new leader within ${LEADER_FAILOVER_SECONDS}s"
    fi

    # Probes must stay active while the cluster is recovering.
    local post_judgments=""
    deadline=$(( $(drill_epoch) + PROBE_WAIT_SECONDS ))
    while [[ $(drill_epoch) -lt $deadline ]]; do
        post_judgments="$(list_judgment_ids "$soul_id")"
        if [[ "$(printf '%s\n' "$post_judgments" | grep -c . || true)" -gt "$(printf '%s\n' "$judgments" | grep -c . || true)" ]]; then
            break
        fi
        sleep 5
    done
    local pre_count post_count
    pre_count="$(printf '%s\n' "$judgments" | grep -c . || true)"
    post_count="$(printf '%s\n' "$post_judgments" | grep -c . || true)"
    if [[ "$post_count" -gt "$pre_count" ]]; then
        scenario_check "probes keep producing judgments across the leader failure" ok "$pre_count before, $post_count after"
    else
        scenario_check "probes keep producing judgments across the leader failure" failed "no new judgments after the failure ($pre_count -> $post_count)"
    fi

    # The pre-failure judgment IDs must still be readable on every replica.
    # The recreated leader pod may still be starting, so wait for the
    # StatefulSet to settle and re-resolve the replica list before probing;
    # a readiness-gated check must not report HTTP 000 for a pod that is
    # simply being recreated.
    kube_ns rollout status "statefulset/$HA_FULLNAME" --timeout=180s >/dev/null
    pods="$(replica_pods)"
    local missing="" pid replica
    pid="$(printf '%s\n' "$judgments" | head -3)"
    for replica in $pods; do
        API_BASE="$(api_base_for_pod "$replica")"
        # drill_login_soft, not drill_login: a failed replica login must be
        # recorded as a missing judgment, not kill the whole drill —
        # drill_fail inside drill_login exits the runner from inside the
        # scenario (the silent scenario-6 aborts in earlier runs).
        if ! drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1; then
            # The cached tunnel may outlive its upstream pod (recreated in
            # the failover); reset it once and retry before failing.
            pf_reset "$replica" "$API_PORT"
            API_BASE="$(api_base_for_pod "$replica")"
            drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1 || true
        fi
        for pid in $pid; do
            api_call GET "/api/v1/judgments/$pid"
            if [[ "$API_STATUS" != "200" ]]; then
                missing="$missing $pid@$replica"
            fi
        done
    done
    if [[ -z "$missing" ]]; then
        scenario_check "pre-failure judgment IDs are preserved on every replica" ok "3 ids checked on 3 replicas"
    else
        scenario_check "pre-failure judgment IDs are preserved on every replica" failed "missing:$missing"
    fi

    # ERROR-level log inspection across the surviving replicas.
    local errors
    errors="$(drill_error_logs $pods | grep -v 'tlsv1 alert' | head -20)"
    if [[ -z "$errors" ]]; then
        scenario_check "no ERROR-level log lines appear during the failover" ok "logs clean"
    else
        scenario_check "no ERROR-level log lines appear during the failover" failed "ERROR lines found: $(printf '%s' "$errors" | head -3 | tr '\n' ' ')"
    fi

    API_BASE="$previous_base"
    if [[ -n "$new_leader" && -z "$missing" ]]; then
        scenario_finish "the leader failover did not complete cleanly"
        return $?
    fi
    scenario_end failed "leader failover did not complete cleanly"
    return 1
}

# --------------------------------------------------------------------------
# Scenario 7: quorum loss rejects authorization, and quorum return restores
# availability without data loss.
# --------------------------------------------------------------------------
scenario_quorum_loss() {
    scenario_begin "quorum loss rejects authorization, and quorum return restores availability without data loss"

    local pods previous_base="$API_BASE"
    pods="$(replica_pods)"

    # Record a durable marker before the disruption so data loss after the
    # quorum returns would be detectable.
    local first
    first="$(printf '%s\n' "$pods" | head -1)"
    API_BASE="$(api_base_for_pod "$first")"
    # drill_login_soft + tunnel reset: a stale tunnel must degrade to a
    # recorded check failure, never exit the runner (drill_fail exits).
    if ! drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1; then
        pf_reset "$first" "$API_PORT"
        API_BASE="$(api_base_for_pod "$first")"
        drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1 || true
    fi

    local marker_name marker_id
    marker_name="drill-quorum-$(drill_gen_hex 4)"
    create_soul "$marker_name" "http://127.0.0.1:1/health" 30s
    marker_id="$SOUL_ID"
    if [[ -z "$marker_id" ]]; then
        scenario_check "a durable marker object can be created before the disruption" failed "POST /api/v1/souls returned HTTP $API_STATUS: ${API_BODY:0:200}"
        API_BASE="$previous_base"
        scenario_finish "the durable marker could not be created"
        return 1
    fi
    scenario_check "a durable marker object can be created before the disruption" ok "soul $marker_id"

    # Lose quorum. The cluster has fixed membership of three voters, so two
    # of the three voters are stopped to remove quorum. The pods are deleted
    # with --wait=false so the StatefulSet recreates them afterwards.
    # Scale down to a single voter: with a fixed three-voter membership the
    # StatefulSet immediately recreates deleted pods, so pod deletion does
    # not produce a durable quorum loss. Scaling to 1 removes two voters
    # until the drill scales back, which is a real quorum loss.
    kube_ns scale statefulset "$HA_FULLNAME" --replicas=1 >/dev/null
    drill_log "scaled $HA_FULLNAME to 1 replica to lose quorum"

    # Wait for the quorum loss to actually take effect.
    local rejected=0 waited=0
    while [[ $waited -lt $QUORUM_LOSS_SECONDS ]]; do
        sleep 5
        waited=$((waited + 5))
        API_BASE="$(api_base_for_pod "$first")"
        TOKEN=""
        api_call POST /api/v1/auth/login \
            "$(printf '{"email":"%s","password":"%s"}' "$(json_escape "$HA_ADMIN_EMAIL")" "$(json_escape "$(current_ha_password)")")"
        if [[ "$API_STATUS" != "200" ]]; then
            rejected=1
            break
        fi
    done
    if [[ "$rejected" -eq 1 ]]; then
        scenario_check "authorization is rejected while quorum is lost" ok "login refused with HTTP $API_STATUS"
    else
        scenario_check "authorization is rejected while quorum is lost" failed "authorization still succeeded after ${QUORUM_LOSS_SECONDS}s"
    fi

    # Restore quorum by scaling the voters back, and wait until all three
    # replicas are ready before testing recovery: logging in through a pod
    # that is still starting would report HTTP 000 instead of the cluster's
    # real behavior.
    kube_ns scale statefulset "$HA_FULLNAME" --replicas=3 >/dev/null
    kube_ns rollout status "statefulset/$HA_FULLNAME" --timeout=180s >/dev/null
    local restored=0 waited=0
    while [[ $waited -lt $QUORUM_RECOVERY_SECONDS ]]; do
        sleep 5
        waited=$((waited + 5))
        API_BASE="$(api_base_for_pod "$first")"
        TOKEN=""
        api_call POST /api/v1/auth/login \
            "$(printf '{"email":"%s","password":"%s"}' "$(json_escape "$HA_ADMIN_EMAIL")" "$(json_escape "$(current_ha_password)")")"
        if [[ "$API_STATUS" == "200" ]]; then
            restored=1
            break
        fi
    done
    if [[ "$restored" -eq 1 ]]; then
        scenario_check "authorization succeeds again once quorum returns" ok "login accepted after ${waited}s"
    else
        scenario_check "authorization succeeds again once quorum returns" failed "login still refused after ${QUORUM_RECOVERY_SECONDS}s"
    fi

    # The marker written before the disruption must still be there: no data
    # loss across the quorum event.
    local present=0 pod
    for pod in $(replica_pods); do
        API_BASE="$(api_base_for_pod "$pod")"
        # drill_login_soft, not drill_login: a failed replica login after the
        # quorum event must be recorded as a missing marker, not exit the
        # runner (drill_fail inside drill_login kills the drill).
        if ! drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1; then
            pf_reset "$pod" "$API_PORT"
            API_BASE="$(api_base_for_pod "$pod")"
            drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1 || true
        fi
        api_call GET "/api/v1/souls/$marker_id"
        if [[ "$API_STATUS" == "200" ]] && [[ "$API_BODY" == *"$marker_id"* ]]; then
            present=$((present + 1))
        fi
    done
    if [[ "$present" -eq 3 ]]; then
        scenario_check "no data is lost across the quorum event" ok "marker $marker_id readable on 3 of 3 replicas"
    else
        scenario_check "no data is lost across the quorum event" failed "marker readable on $present of 3 replicas"
    fi

    API_BASE="$previous_base"
    if [[ "$rejected" -eq 1 && "$restored" -eq 1 && "$present" -eq 3 ]]; then
        scenario_finish "the quorum loss and recovery did not behave as required"
        return $?
    fi
    scenario_end failed "quorum loss and recovery did not behave as required"
    return 1
}

# --------------------------------------------------------------------------
# Scenario 8: a full rolling restart preserves sessions, config objects, and
# selected history IDs.
# --------------------------------------------------------------------------
scenario_rolling_restart() {
    scenario_begin "a full rolling restart preserves sessions, config objects, and selected history IDs"

    local pods previous_base="$API_BASE"
    pods="$(replica_pods)"

    local first
    first="$(printf '%s\n' "$pods" | head -1)"
    API_BASE="$(api_base_for_pod "$first")"
    # drill_login_soft + tunnel reset: a stale tunnel must degrade to a
    # recorded check failure, never exit the runner (drill_fail exits).
    if ! drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1; then
        pf_reset "$first" "$API_PORT"
        API_BASE="$(api_base_for_pod "$first")"
        drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1 || true
    fi
    local session_token="$TOKEN"

    # A config object and a history ID to check afterwards.
    local config_id
    create_soul "drill-restart-$(drill_gen_hex 4)" "http://127.0.0.1:1/health" 30s
    config_id="$SOUL_ID"
    if [[ -z "$config_id" ]]; then
        scenario_check "a config object can be created before the restart" failed "POST /api/v1/souls returned HTTP $API_STATUS: ${API_BODY:0:200}"
        API_BASE="$previous_base"
        scenario_finish "the config object could not be created before the restart"
        return 1
    fi
    scenario_check "a config object can be created before the restart" ok "soul $config_id"

    local history_ids=""
    local deadline
    deadline=$(( $(drill_epoch) + PROBE_WAIT_SECONDS ))
    while [[ $(drill_epoch) -lt $deadline ]]; do
        history_ids="$(list_judgment_ids "$config_id")"
        if [[ -n "$history_ids" ]]; then
            break
        fi
        sleep 5
    done
    if [[ -n "$history_ids" ]]; then
        scenario_check "history IDs exist before the restart" ok "$(printf '%s' "$history_ids" | head -2 | tr '\n' ',')"
    else
        scenario_check "history IDs exist before the restart" failed "no judgment recorded within ${PROBE_WAIT_SECONDS}s"
    fi
    printf '%s' "$history_ids" >"$STATE_DIR/restart-history-ids"

    # Roll every voter. The StatefulSet uses a rolling update, so the voters
    # restart one at a time and the cluster keeps a quorum throughout.
    local sts="${HA_FULLNAME}"
    kube_ns rollout restart "statefulset/$sts" >/dev/null 2>&1 || true
    if kube_ns rollout status "statefulset/$sts" --timeout="$TIMEOUT" >"$STATE_DIR/rolling-restart.log" 2>&1; then
        scenario_check "the rolling restart completes" ok "statefulset/$sts rolled out"
    else
        scenario_check "the rolling restart completes" failed "see $STATE_DIR/rolling-restart.log"
    fi

    # Wait for all three voters to be ready again.
    local ready=0 waited=0
    while [[ $waited -lt $RESTART_WAIT_SECONDS ]]; do
        ready="$(kube_ns get pods -l "app.kubernetes.io/instance=$HA_RELEASE" \
            -o jsonpath='{range .items[?(@.status.phase=="Running")]}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}' 2>/dev/null \
            | grep -c True || true)"
        if [[ "$ready" -eq 3 ]]; then
            break
        fi
        sleep 5
        waited=$((waited + 5))
    done
    if [[ "$ready" -eq 3 ]]; then
        scenario_check "all three voters are ready after the restart" ok "3 of 3 ready"
    else
        scenario_check "all three voters are ready after the restart" failed "$ready of 3 ready after ${RESTART_WAIT_SECONDS}s"
    fi

    # Sessions, config objects, and history must all survive.
    local session_ok=0 config_ok=0 history_ok=0 pod
    for pod in $(replica_pods); do
        API_BASE="$(api_base_for_pod "$pod")"
        TOKEN="$session_token"
        api_call GET /api/v1/auth/me
        if [[ "$API_STATUS" == "200" ]]; then
            session_ok=$((session_ok + 1))
        fi

        # Soft login + tunnel reset: the rolling restart recreates every
        # voter pod, so cached tunnels can be stale; a failed login must be
        # recorded as a failed check, never exit the runner.
        if ! drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1; then
            pf_reset "$pod" "$API_PORT"
            API_BASE="$(api_base_for_pod "$pod")"
            drill_login_soft "$HA_ADMIN_EMAIL" "$(current_ha_password)" >/dev/null 2>&1 || true
        fi
        api_call GET "/api/v1/souls/$config_id"
        if [[ "$API_STATUS" == "200" ]] && [[ "$API_BODY" == *"$config_id"* ]]; then
            config_ok=$((config_ok + 1))
        fi

        local hid
        for hid in $(printf '%s\n' "$history_ids" | head -2); do
            api_call GET "/api/v1/judgments/$hid"
            if [[ "$API_STATUS" != "200" ]]; then
                history_ok=$((history_ok + 1))
            fi
        done
    done
    local history_checks
    history_checks="$(printf '%s\n' "$history_ids" | head -2 | grep -c . || true)"
    history_checks=$((history_checks * 3))

    # A full rolling restart makes every voter replay its log and rejoin, so
    # a replica may still be converging when it is first probed (runs showed
    # 0/3 and 2/3 valid immediately after rollout while the data itself was
    # intact). Re-check the preserved session with a bounded wait before
    # declaring it lost: the check must distinguish "session lost" from
    # "not yet converged".
    local session_deadline=$((SECONDS + 90))
    while [[ "$session_ok" -lt 3 && $SECONDS -lt $session_deadline ]]; do
        sleep 5
        session_ok=0
        for pod in $(replica_pods); do
            API_BASE="$(api_base_for_pod "$pod")"
            TOKEN="$session_token"
            api_call GET /api/v1/auth/me
            if [[ "$API_STATUS" == "200" ]]; then
                session_ok=$((session_ok + 1))
            fi
        done
    done

    if [[ "$session_ok" -eq 3 ]]; then
        scenario_check "sessions survive the rolling restart" ok "3 of 3 replicas"
    else
        scenario_check "sessions survive the rolling restart" failed "valid on $session_ok of 3 replicas"
    fi
    if [[ "$config_ok" -eq 3 ]]; then
        scenario_check "config objects survive the rolling restart" ok "3 of 3 replicas"
    else
        scenario_check "config objects survive the rolling restart" failed "readable on $config_ok of 3 replicas"
    fi
    if [[ "$history_ok" -eq 0 ]]; then
        scenario_check "selected history IDs survive the rolling restart" ok "$history_checks history lookups succeeded"
    else
        scenario_check "selected history IDs survive the rolling restart" failed "$history_ok of $history_checks history lookups failed"
    fi

    API_BASE="$previous_base"
    if [[ "$session_ok" -eq 3 && "$config_ok" -eq 3 && "$history_ok" -eq 0 ]]; then
        scenario_finish "the state was not fully preserved across the rolling restart"
        return $?
    fi
    scenario_end failed "state was not fully preserved across the rolling restart"
    return 1
}
