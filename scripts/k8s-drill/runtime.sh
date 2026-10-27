#!/usr/bin/env bash
# Observe only installed releases recorded by this run. Publish no pod env,
# command, mounted secret or annotation data from the Kubernetes response.
capture_runtime_images() {
    local entry ns release response inventory entries failed=0 count=0
    local fragments="$STATE_DIR/runtime-images.frag"
    local report="$STATE_DIR/runtime-images.json"
    printf '{"status":"unavailable","pods":[]}\n' >"$report" || return 1
    ledger_snapshot "$STATE_DIR/runtime-ledger.txt" || return 1
    entries="$(ledger_entries helm "$STATE_DIR/runtime-ledger.txt")" || return 1
    : >"$fragments" || return 1
    while IFS= read -r entry; do
        [[ -n "$entry" ]] || continue
        ns="${entry%% *}"
        release="${entry#* }"
        count=$((count + 1))
        if ! response="$(kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" \
            --namespace "$ns" --request-timeout=15s get pods -l "app.kubernetes.io/instance=$release" -o json \
            2>>"$STATE_DIR/runtime-images-errors.log")"; then
            failed=1
            continue
        fi
        if ! inventory="$(jq -ce --arg namespace "$ns" --arg release "$release" '
            def nonempty: type == "string" and length > 0;
            [.items[] | select(.metadata.deletionTimestamp == null)] |
            if length == 0 then error("no active pods") else . end |
            map(. as $pod | {
                namespace: $namespace, release: $release,
                pod: .metadata.name, uid: .metadata.uid, node: .spec.nodeName,
                containers: [.spec.containers[] as $container |
                    $pod.status.containerStatuses[]? |
                    select(.name == $container.name) |
                    {name: .name, image: $container.image, image_id: .imageID}]
            } | if (.pod | nonempty) and (.uid | nonempty) and
                    (.containers | length) == ($pod.spec.containers | length) and
                    (.containers | length) > 0 and
                    all(.containers[]; (.name | nonempty) and (.image | nonempty) and (.image_id | nonempty))
                then . else error("incomplete runtime identity") end)
            ' <<<"$response" 2>>"$STATE_DIR/runtime-images-errors.log")"; then
            failed=1
            continue
        fi
        printf '%s\n' "$inventory" >>"$fragments" || return 1
    done <<<"$entries"
    if [[ "$count" == 0 ]]; then
        printf '{"status":"not_observed","pods":[]}\n' >"$report"
        return $?
    fi
    local status=available
    [[ "$failed" == 0 ]] || status=unavailable
    jq -s --arg status "$status" --arg sampled_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '{status: $status, sampled_at: $sampled_at, pods: (add // [])}' \
        "$fragments" >"$report.tmp" || return 1
    mv -f -- "$report.tmp" "$report" || return 1
    return "$failed"
}
