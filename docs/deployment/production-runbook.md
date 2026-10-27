# Production Deployment Runbook

This runbook is the operator checklist for promoting a tested AnubisWatch build
to a production or staging Kubernetes environment.

## Preconditions

- Target image digest or tag is known and has passed GitHub CI.
- Kubernetes context points at the target cluster.
- `helm`, `kubectl`, and `curl` are installed locally.
- `secrets.adminPassword` is set from the deployment secret store.
- `secrets.clusterSecret` is set when `config.necropolis.enabled=true`.
- `secrets.encryptionKey` is set when storage encryption is enabled.
- TLS is handled by ingress/cert-manager or by explicitly mounted `cert` and
  `key` files. Built-in certificate automation is not provided.
- A rollback revision is available from `helm history`.

## Preflight

```bash
set -euo pipefail
umask 077
: "${KUBECONFIG:?Set KUBECONFIG to the approved target kubeconfig file}"
: "${CONTEXT:?Set CONTEXT to the approved target context name}"
ARTIFACT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/anubiswatch-deploy.XXXXXX")"

# Constrain Helm, kubectl and the helper scripts to a private copy of the
# explicitly selected context. The original kubeconfig remains unchanged.
kubectl --kubeconfig "$KUBECONFIG" --context "$CONTEXT" \
  config view --minify --flatten --raw >"$ARTIFACT_DIR/kubeconfig"
export KUBECONFIG="$ARTIFACT_DIR/kubeconfig"
kubectl config use-context "$CONTEXT"

export NAMESPACE=anubiswatch
export RELEASE=anubiswatch
export CHART=deploy/helm/anubiswatch
export VALUES=values-production.yaml

kubectl config current-context
kubectl get namespace "$NAMESPACE" || kubectl create namespace "$NAMESPACE"
helm lint "$CHART"
helm template "$RELEASE" "$CHART" \
  --namespace "$NAMESPACE" \
  -f "$VALUES" >"$ARTIFACT_DIR/rendered.yaml"
kubectl apply --dry-run=server -f "$ARTIFACT_DIR/rendered.yaml"
```

Keep this shell's private `KUBECONFIG` for the deploy, smoke, rollback and
evidence commands below. `ARTIFACT_DIR` contains credentials and rendered
Secrets: retain it privately only as required, exclude it from uploads and
version control, and remove it after the operation's retention period. The
selected context is copied offline; this preparation does not establish that
the target is authorized or that CI/staging acceptance passed.

Start from
`deploy/helm/anubiswatch/values-production.example.yaml`, keep the filled
`values-production.yaml` out of version control, and source secret values from
the deployment secret store.

Review the rendered manifest before applying if the change touches storage,
ingress, secrets, RBAC, or replica counts.

The same checks can be run with the preflight helper:

```bash
VALUES="$VALUES" \
NAMESPACE="$NAMESPACE" \
RELEASE="$RELEASE" \
CHART="$CHART" \
scripts/production-preflight.sh
```

Use `ANUBIS_PREFLIGHT_CREATE_NAMESPACE=true` when the namespace should be
created by the operator command. Use `ANUBIS_PREFLIGHT_SKIP_CLUSTER=true` only
for offline Helm rendering checks where cluster access is intentionally absent.

## Deploy

```bash
helm upgrade --install "$RELEASE" "$CHART" \
  --namespace "$NAMESPACE" \
  --create-namespace \
  -f "$VALUES" \
  --atomic \
  --timeout 10m
```

Wait for the selected workload to become ready:

```bash
kubectl -n "$NAMESPACE" rollout status deployment/anubiswatch --timeout=10m
kubectl -n "$NAMESPACE" get pods,svc,ingress
```

The default chart deploys one standalone replica. Do not scale this Deployment.
The opt-in HA topology uses a separate three-voter StatefulSet; follow the HA
procedure below rather than changing an existing release's topology in place.

## Smoke Test

Run public checks after DNS and ingress have converged:

```bash
scripts/production-smoke.sh https://anubiswatch.example.com
```

Run authenticated checks when admin credentials are available:

```bash
ANUBIS_SMOKE_EMAIL=admin@example.com \
ANUBIS_SMOKE_PASSWORD="$ANUBIS_ADMIN_PASSWORD" \
scripts/production-smoke.sh https://anubiswatch.example.com
```

Include rollout validation with the same command when running from an operator
machine that has cluster access:

```bash
ANUBIS_SMOKE_NAMESPACE=anubiswatch \
ANUBIS_SMOKE_WORKLOAD=deployment/anubiswatch \
ANUBIS_SMOKE_EMAIL=admin@example.com \
ANUBIS_SMOKE_PASSWORD="$ANUBIS_ADMIN_PASSWORD" \
scripts/production-smoke.sh https://anubiswatch.example.com
```

The smoke script verifies:

- Kubernetes rollout status when requested.
- `/health`, `/ready`, `/metrics`, `/api/openapi.json`, and the dashboard shell.
- Optional authenticated login, current-user lookup, soul listing, and stats
  overview.
- TLS certificate validation for HTTPS URLs unless `ANUBIS_SMOKE_INSECURE=true`
  is explicitly set.

## Rollback

Use rollback when rollout, health, or authenticated checks fail and the issue is
not an external dependency problem.

```bash
helm history "$RELEASE" --namespace "$NAMESPACE"
helm rollback "$RELEASE" <REVISION> --namespace "$NAMESPACE" --wait --timeout 10m
kubectl -n "$NAMESPACE" rollout status deployment/anubiswatch --timeout=10m
scripts/production-smoke.sh https://anubiswatch.example.com
```

After rollback, capture the failed release revision, image digest, Helm values
diff, failing check output, and pod logs.

## Evidence To Record

- Git commit SHA and container image digest.
- Helm release revision and values file checksum.
- `kubectl get pods,svc,ingress` output.
- Smoke test command and result.
- Any skipped checks and why they were skipped.

Use the evidence capture helper after deploy or rollback to create a timestamped
artifact directory:

```bash
ANUBIS_EVIDENCE_VALUES="$VALUES" \
ANUBIS_EVIDENCE_IMAGE="ghcr.io/anubiswatch/anubiswatch@sha256:..." \
ANUBIS_EVIDENCE_BASE_URL=https://anubiswatch.example.com \
ANUBIS_EVIDENCE_RUN_SMOKE=true \
scripts/capture-deployment-evidence.sh
```

Or through `make`:

```bash
make capture-production-evidence OUTPUT_DIR=evidence/prod-$(date -u +%Y%m%dT%H%M%SZ)
```

The helper records command output, rollout status, Helm status/history, workload
YAML, events, and optional smoke output. It records only the checksum of the
values file, not the values file content.

## Known Gaps

- This repository does not contain live cluster credentials, production secret
  values, DNS ownership, or certificate issuer configuration.
- Built-in ACME/autocert support has been removed; certificate lifecycle must be
  owned by ingress/cert-manager or another platform-level TLS process.
- The runbook validates deployment health, not production traffic volume or
  long-term SLO conformance. Run load tests separately for capacity changes.

## Offline backup and recovery drill

For the supported single-node deployment, schedule a maintenance window: the
backup/restore CLI opens the embedded data directory exclusively, so stop the
server before either operation. Never run a drill against the production PVC.
Keep the backup, matching configuration, and storage encryption keys in
protected recovery storage. Creating a logical backup requires the key that
opens the encrypted source database. Physical data-file recovery also needs
the original storage key. The CLI's `.json.gz` archive contains decrypted
logical records and account/password hashes: database encryption does not
encrypt that archive. Protect it as secret material. A logical restore can
write to a new empty database using a newly configured destination storage key.

1. Record the binary version, image digest, configuration checksum, and the
   IDs/counts of the monitors and history that must survive recovery.
2. Stop the source server cleanly. Using the same `ANUBIS_CONFIG`,
   `ANUBIS_DATA_DIR`, and encryption key as the server, create an archive:

   ```bash
   anubis backup create --include-history --output /secure-backups/anubis-drill.json.gz
   ```

3. Point `ANUBIS_CONFIG` and `ANUBIS_DATA_DIR` at a separate, empty recovery
   instance. Supply the appropriate encryption key, then restore:

   ```bash
   anubis restore /secure-backups/anubis-drill.json.gz --force
   ```

   Check the exit status. `--force` skips the prompt and continues attempting
   records after an error; any failed records still produce a nonzero result.
   Do not promote a partial restore.
4. Start the recovery instance on a separate address with probe/notification
   egress isolated. Log in again (restore invalidates saved sessions), compare
   monitor IDs, alert rules, journeys, and included history with the inventory,
   and run authenticated smoke checks. History inclusion defaults to the last
   seven days; this is not an archive of all retained history.
   For standalone local authentication, retain the source admin email and
   configured bootstrap password to preserve a password changed at runtime.
   Changing that configured password intentionally overrides the restored
   runtime password on startup. Decide and record that credential policy before
   starting recovery; HA credential authority has separate semantics.
5. Record backup duration, restore-to-ready duration, data timestamps, failures,
   and the resulting measured RPO/RTO. Confirm that these satisfy the service's
   recovery objectives before promotion. Resume the source service if it is
   still the production instance.

New Raft snapshots use an atomically published chunk manifest so images can
exceed one WAL record. This version can read legacy snapshots. Older binaries
cannot read the new snapshot format: keep a pre-upgrade backup and restore it
to a separate data directory when rolling back across this change.

## Protected monitoring endpoints

The shipped container, Kubernetes manifests, and Helm chart require
authentication for `/metrics`. Supply smoke credentials to validate the scrape;
without them the smoke script verifies that a protected endpoint returns 401
and explicitly reports the authenticated scrape as not run. Authenticated
monitor metrics are scoped to the token's workspace. For Prometheus Operator,
configure `monitoring.serviceMonitor.bearerTokenSecret` with an existing Secret
and rotate the token through your secret store.

Legacy `/status` and `/status.html` follow authentication/workspace rules when
auth is enabled. Publish selected monitors through an explicit public status
page instead. Password-protected pages accept a POST and issue a signed cookie;
passwords in query strings are not accepted.

## Three-voter HA deployment

Use a newly built candidate containing shared cluster authentication and
`deploy/helm/anubiswatch/values-ha.example.yaml` as the starting point. Historical
images predating this change do not share sessions or password revocation.
Supply an explicit immutable image digest, three schedulable worker nodes,
durable per-pod volumes, the same encryption key and cluster secret, and an
existing Raft TLS Secret containing `tls.crt`, `tls.key`, and `ca.crt`.

The certificate must cover each pod's DNS name:
`<fullname>-<ordinal>.<fullname>-headless.<namespace>.svc.<cluster.domain>`.
Include server and client authentication usages and manage renewal externally.
The chart requires peer verification and client certificates. Network policy
must permit Raft TCP traffic between all three voters and DNS resolution.
The process loads peer certificates at startup. After updating this external
Secret, roll the StatefulSet one voter at a time and verify convergence. For
CA changes, stage overlapping trust before replacing leaf certificates; do not
replace the only trusted CA on every voter at once.

Membership is fixed at three voters with per-ordinal peer lists. Leave bootstrap
disabled, persistence enabled, and autoscaling disabled. Keep the default
anti-affinity or provide equivalent placement across failure domains. A lost
PVC requires a recovery procedure; never bootstrap an empty replacement into a
new independent cluster. Do not remove multiple voters during maintenance.

Only `auth.type=local` is supported for this HA path. Sessions, password changes,
logout, reset tokens and lockouts use replicated state. Authentication requires
quorum and fails closed when quorum is unavailable. OIDC/LDAP cluster startup is
rejected until their session lifecycle supports the same authority. Initialize
a fresh HA release with one trusted credential seed. Once replicated auth state
exists, changing the Helm admin password seed does not rotate that authority;
use the authenticated password-change or reset workflow. Existing divergent
node-local auth files are not an automatic credential migration source.

Run `scripts/production-preflight.sh` with an explicit target kubeconfig and HA
values, install into a separate release, and wait for
`statefulset/<fullname>`. Then check `/api/v1/cluster/status` on every pod: one
leader, two followers, the same nonempty leader ID and two peers per node.
`/ready` alone does not prove cluster convergence. Verify that a follower write
is readable on every pod and that one login works across the Service. Revoke it
and confirm rejection on all replicas. Repeat after replacing the leader.

In-place standalone/HA topology toggles are rejected. Plan a separate-release
backup/restore migration, validate the restored inventory and credentials, and
switch ingress only after acceptance. A Helm rollback cannot reverse an
incompatible storage format or substitute for a backup.

## Repeatable bounded soak

Run the acceptance harness against a candidate binary on an isolated machine:

```bash
scripts/production-soak.sh --binary ./anubis --souls 1000 --duration 180s \
  --output .temp_files/production-soak
```

The harness creates its own encrypted store, injects a target failure and
recovery, checks persisted histories, and restarts the server to verify retained
judgments. It reports cadence, concurrency and memory against explicit bounds.
This short local workload does not establish multi-day stability or production
capacity; measure those with the deployment's actual checks and retention.

## Acceptance drill

The Kubernetes acceptance drill exercises a candidate release on a real
cluster: it installs a three-voter cluster and a standalone release, drives
the API through every replica, breaks the cluster on purpose and checks that
the data, the sessions and the verdicts survive. It disposes of everything it
creates.

```bash
scripts/k8s-drill/k8s-drill.sh \
    --kubeconfig <path-to-kubeconfig> \
    --context <context> \
    --image <repository>:<tag> \
    --output-dir .temp_files/k8s-drill
```

The kubeconfig argument is mandatory; the drill never falls back to an ambient
current-context. `--scenarios` selects a subset of the eight scenarios
(`standalone`, `ha-topology`, `follower-write`, `session-rotation`,
`raft-mtls`, `probe-failover`, `quorum-loss`, `rolling-restart`), and
`--keep-on-fail` leaves the drill-owned objects in place so a failure can be
inspected. The drill writes a structured `results.json` and a summary table
into the output directory, and exits non-zero if any scenario failed.

The full procedure, the evidence layout, the cleanup rules and the list of
uncovered aspects are documented in
[docs/deployment/k8s-acceptance-drill.md](k8s-acceptance-drill.md).

The drill has passed all eight scenarios on a disposable local kind cluster,
including runtime image identification and checked cleanup. Run it again for
the exact candidate and intended environment; local evidence does not replace
required CI or staging acceptance.

## WAL batch format and binary rollback

Judgment writes now persist their primary record, lookup index, live rollups
and first-result marker together as one checksummed WAL `BATCH` frame with one
fsync. A failed compound write returns an error before publishing any of those
records in memory. Recovery applies a complete valid frame or skips a torn or
checksum-damaged frame as a unit; malformed batch payloads fail startup.

The new binary continues to read legacy `PUT`/`DELETE` records. Older binaries
do not understand `BATCH`: they can silently skip its data and checkpoint the
result over the WAL. **Do not point an older binary at a data directory written
by this version.** A Helm rollback changes the binary, not its persisted data.
Keep a tested pre-upgrade backup and restore that backup into separate empty
directories/PVCs with the matching keys before starting an older binary. Keep
the upgraded source fenced and intact until the rollback inventory and auth
checks pass. A current-format physical backup requires the matching new reader;
it is not a substitute for a pre-upgrade rollback backup.

## HA cold recovery with the same voter identities

This procedure covers a fixed three-voter cluster restored from complete,
cleanly stopped member directories. It preserves the three voter IDs and peer
addresses and uses a known credential from the backup's authority. The local
rehearsal used encrypted storage, verified peer mTLS, retained Raft snapshots,
logs and hard state, and an isolated recovery network. Kubernetes CSI snapshots,
offsite transport and provider-specific PVC restoration need separate rehearsal.

1. Block management ingress and notification/probe egress. Record the three
   voter IDs, peer addresses, binary identity, configuration checksums and the
   acknowledged configuration/history inventory. Check that every replica can
   read the inventory, then stop **all three** servers. Keep the original cluster
   fenced throughout recovery; two clusters with the same identities and keys
   must not run against the same peer network.
2. Copy each member's **whole data directory** after every process has exited.
   Retain file ownership/permissions and a checksum manifest, including WAL,
   Raft logs, hard state, snapshots and shared auth state. Keep matching configs
   and versions of the storage key, cluster secret, CA and peer certificate/key
   material in protected recovery storage. Retain the known admin credential
   for this backup cut through the approved secret-management system.
   `anubis backup create` is a logical application archive that excludes
   node-local Raft records; it is not this three-member physical recovery set.
3. Restore the three cold copies into separate empty directories/PVCs, retaining
   each member-to-data mapping. Verify the manifest before starting anything.
   Physical encrypted data requires its matching storage key. Reestablish the
   same peer addresses/DNS identities and verified TLS trust. Start with the
   preserved membership; do not bootstrap a fresh authority or combine data
   from unrelated backups. Membership/address migrations require their own
   supported reconfiguration procedure.
4. Start all three recovery members on the isolated peer network. Through a
   controlled management connection, verify one leader, two followers and the
   same leader identity on every member. Use a credential valid at the backup
   cut, and compare configuration IDs, acknowledged history and user identity
   on each backend. Verify a new write can commit and replicate before promotion.
5. A physical backup can resurrect sessions and passwords revoked **after** its
   cut. Keep ingress closed until the recovered authority is secured. Rotate
   the admin password through the authenticated
   `PUT /api/v1/auth/change-password` workflow, log in again, and verify that
   backed-up sessions and the old password are rejected by **every** backend.
   The local-file `anubis admin reset-password` command refuses cluster
   configurations because it cannot mutate replicated auth state. Changing an
   HA bootstrap seed is also not a rotation of an existing shared authority.
   If no known backup credential remains, keep the deployment isolated and
   establish an approved authority-recovery path before promotion.
6. Record the backup cut, any expected post-cut data loss, restore-to-ready
   measurements, credential/session outcome and artifact identities. Open
   ingress and egress only after these checks and the deployment's RPO/RTO
   criteria pass. Retain the frozen backup and source fence for controlled
   rollback; a successful local rehearsal is not an offsite or production SLO.


## Judgment identity format and standalone installation

For the current binary/Docker installation target, use
[Binary and Docker installation](binary-and-docker.md). New Judgment keys append
`/{judgmentID}` after the timestamp to preserve collisions. Legacy records
remain readable. Old writers cannot safely deduplicate or look up all new
records; use a pre-upgrade backup in separate empty storage for downgrade,
as for the WAL `BATCH` boundary above.
