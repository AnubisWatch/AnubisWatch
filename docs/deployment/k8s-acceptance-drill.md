# Kubernetes acceptance drill

The Kubernetes acceptance drill exercises a real AnubisWatch deployment on a
real cluster: it installs a three-voter cluster and a standalone release,
drives the API through every replica, breaks the cluster on purpose and checks
that the data, the sessions and the verdicts survive.

The drill is a **verification harness**, not a deployment guide. It creates
disposable objects with disposable credentials, proves a set of acceptance
claims, and then deletes exactly what it created.

## Status: locally validated — 8/8 scenarios passed on a disposable kind cluster

The runner has been executed repeatedly against a real 3-worker kind cluster
(runs 2026-10-24; final green run: run18, evidence in
`.temp_files/k8s-drill-live/results.json` and
`.temp_files/k8s-drill-live/DRILL-EVIDENCE.md`). All eight scenarios pass,
including verified server identity, a successful trusted-client Raft RPC,
and rejection without a client certificate (checked at the protocol level:
a cert-less peer gets no Identify answer — in TLS 1.3 the client-side
handshake can complete while the server has already refused, so exit codes
are not evidence). Runtime enforcement is proven by startup logs:
`client_auth: RequireAndVerifyClientCert` at both config build and
transport start.

Consequences you must plan for:

- The earlier "product mTLS gap" reading of the first failing runs was a
  measurement artifact of the old check (client exit code under TLS 1.3),
  not a product defect — see `DRILL-EVIDENCE.md` for the three-line proof.
- Scenario outcomes are evidence; the run18 results should be reviewed by a
  human who knows the expected cluster behaviour before a release claim.
- The gaps listed in [Not covered by the drill](#not-covered-by-the-drill) are
  open. They are not "known failures"; they are simply not exercised.

## Prerequisites

| Requirement | Notes |
|---|---|
| `kubectl` | Must be able to reach the target cluster with the given kubeconfig. |
| `helm` | The drill installs the chart with `helm upgrade --install`. |
| `jq` | Used by the scenarios to read cluster status and API payloads. |
| `curl` | Used for the API calls against the port-forwarded replicas. |
| `openssl` | Used to generate the disposable CA/leaf and to run the TLS probes. |
| A kubeconfig file | Passed with `--kubeconfig`. The drill never uses the ambient context. |
| A container image | The drill needs an explicit `REPO:TAG` or `REPO@sha256:DIGEST`. Cluster mode refuses `latest`. |
| Cluster with three schedulable workers | The HA scenario asserts the three voters land on three distinct nodes. |

`kind` is optional but convenient:

```bash
kind create cluster --name anubis-drill
kind load docker-image anubiswatch:v0.2.1 --name anubis-drill
```

A kind cluster with a single node cannot satisfy the "three distinct workers"
assertion of the `ha-topology` scenario; that check will fail on a
single-worker kind cluster by design.

The drill installs into a namespace it creates (`anubiswatch-drill` by
default). The target cluster must allow namespace creation and Helm installs
by the identity in the kubeconfig.

## Running the drill

```bash
# Full drill against a kind cluster.
scripts/k8s-drill/k8s-drill.sh \
    --kubeconfig ~/.kube/config \
    --context kind-anubis-drill \
    --image anubiswatch:v0.2.1 \
    --output-dir .temp_files/k8s-drill

# One scenario only.
scripts/k8s-drill/k8s-drill.sh \
    --kubeconfig ~/.kube/config \
    --context kind-anubis-drill \
    --image anubiswatch:v0.2.1 \
    --scenarios quorum-loss

# Keep the drill objects after a failure so they can be inspected in place.
scripts/k8s-drill/k8s-drill.sh \
    --kubeconfig ~/.kube/config \
    --context kind-anubis-drill \
    --image anubiswatch:v0.2.1 \
    --keep-on-fail
```

Options:

| Option | Default | Meaning |
|---|---|---|
| `--kubeconfig PATH` | *(required)* | Kubeconfig of the target cluster. The drill refuses to start without it and never falls back to an ambient context. |
| `--context NAME` | `current-context` of that file | Context inside the given kubeconfig. |
| `--image REF` | chart image block | `REPO:TAG`, `REPO:TAG@sha256:DIGEST` or `REPO@sha256:DIGEST`. |
| `--output-dir DIR` | `.temp_files/k8s-drill-<UTC timestamp>` | New evidence directory; must not already exist. Parent directories are created as needed. |
| `--scenarios LIST` | `all` | Comma separated scenario ids, or `all`. |
| `--namespace NAME` | `anubiswatch-drill` | Namespace for the drill objects. |
| `--standalone-namespace NAME` | same as `--namespace` | Namespace for the standalone release. |
| `--chart DIR` | `deploy/helm/anubiswatch` | Chart directory. |
| `--keep-on-fail` | off | Keep the drill-owned objects when the drill fails. |

Exit status is `0` when every selected scenario passed and non-zero otherwise.
A scenario that fails does not abort the run: the remaining scenarios still
execute and the failure diagnostics are captured, so one run yields a full
picture instead of the first broken step.

## Scenarios

The eight scenarios run in this order. The order matters: the standalone
release comes first because the probe scenario targets it, and the session
rotation comes before the failure scenarios because it changes the admin
password the later scenarios use.

| # | Id | What it asserts |
|---|---|---|
| 1 | `standalone` | A standalone release installs and becomes ready; the pod really receives the encryption key and the storage config really enables encryption; a Soul survives a pod restart on the encrypted PVC; a config-only change rolls out with a changed config checksum; a Helm rollback restores the previous config. |
| 2 | `ha-topology` | Exactly three voter pods exist, they are scheduled on three distinct workers, exactly one reports `leader` and two report `follower`, and every replica reports the same leader identity. |
| 3 | `follower-write` | A write accepted by a follower is readable on all three replicas, including the leader. |
| 4 | `session-rotation` | One session issued by one backend is accepted by all three; a password rotation is accepted; the old password is rejected and the new password accepted on every backend; a logout revokes the session on every backend. |
| 5 | `raft-mtls` | The Raft peer presents a certificate that chains to the drill CA and matches the pod DNS name; a trusted client certificate completes a real Raft Identify RPC; a connection without a client certificate is refused, and the refusal is recognised as client-certificate enforcement rather than a server-certificate validation error. |
| 6 | `probe-failover` | A probe monitor produces acknowledged judgments; deleting the leader pod elects a new leader; probes keep producing judgments across the failure; the pre-failure judgment ids stay readable on every replica; no `ERROR`-level log lines appear during the failover. |
| 7 | `quorum-loss` | Two of three voters are stopped; authorization is refused while quorum is missing; authorization succeeds again once quorum returns; a marker object written before the disruption is still readable on all three replicas afterwards. |
| 8 | `rolling-restart` | A rolling restart of the StatefulSet ends with three ready voters; sessions, config objects and selected history ids survive the restart. |

## Evidence layout

```
<output-dir>/
├── results.json                 # structured result document (see below)
├── evidence/
│   └── failures/<scenario>/     # captured only for failing scenarios
│       ├── pods.txt
│       ├── pods-describe.txt
│       ├── events.txt
│       ├── workload.yaml        # configmaps and secrets of the namespace
│       ├── pod-logs.txt
│       ├── log-<pod>.txt
│       └── log-<pod>-previous.txt
├── state/                       # drill working state
│   ├── ledger.txt               # every object the drill created
│   ├── scenario-results.frag    # raw per-scenario result fragments
│   ├── checks.json              # last recorded check set
│   ├── last-checks.json
│   ├── standalone-values.yaml   # generated Helm values
│   ├── ha-values.yaml
│   ├── certs/                   # disposable CA and leaf (0600)
│   └── pf/                      # port-forward pid/port/log files
└── tmp/                         # API request/response scratch files
```

`results.json` carries the checkout identity captured before cluster preflight,
the requested image reference, the target cluster identity, and per-scenario
checks. Checkout identity includes the full HEAD, a dirty flag and a SHA256
fingerprint of tracked and nonignored untracked file bytes, executable modes,
symlink targets and deleted paths. The run's output directory is excluded so
generated evidence cannot alter that identity. Ignored files, including build
outputs and local secrets, are excluded. Without Git or on a hashing failure,
`identity_status` is `unavailable`, with null dirty/fingerprint fields.

This records the checkout used by the drill, not an attestation that the
requested image was built from it. An immutable image digest and its build
provenance are still required to bind deployment results to a release.

Before cleanup, the runner queries the installed releases in its ownership
ledger and records `runtime_images`: active pod names/UIDs, namespaces,
releases, worker nodes and each container's configured image and Kubernetes
`imageID`. This is the final observed inventory, including sidecars; it is not
a history of all pods replaced during the drill. The response is reduced to
these fields so pod environment variables and other secret-bearing fields
are excluded. ImageID formats come from the container runtime and need not
equal the requested multi-platform registry digest.

Inventory status is `available` after successful observation, `not_observed`
when no installed release was recorded, or `unavailable` if a query fails or
an active pod lacks its UID or container imageID. Collection uses a bounded
API request. An unavailable installed-release inventory, malformed scenario
results or failed report publication makes the drill exit nonzero even if
all scenario checks passed. Cleanup still runs; raw fragments and private
error logs remain under `state/` for diagnosis. An aborted scenario retains
its already completed checks in a failed scenario result.

```json
{
  "drill": "anubiswatch-kubernetes-acceptance",
  "status": "failed",
  "source": {
    "identity_status": "available",
    "commit": "0123456789abcdef0123456789abcdef01234567",
    "dirty": false,
    "worktree_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "chart": "anubiswatch",
    "chart_version": "0.2.1",
    "image": "ghcr.io/anubiswatch/anubiswatch:v0.2.1"
  },
  "cluster": {
    "kubeconfig": "/home/user/.kube/config",
    "context": "kind-anubis-drill",
    "namespace": "anubiswatch-drill"
  },
  "scenarios": [
    {
      "id": 1,
      "name": "three voters on distinct workers, ...",
      "status": "passed",
      "duration_seconds": 42,
      "checks": {
        "exactly three voter pods exist": {
          "status": "passed",
          "detail": "3 pods"
        }
      },
      "evidence": [],
      "error": ""
    }
  ],
  "failures": 0
}
```

The drill prints a summary table of the same data at the end of the run.

## Cleanup behavior

The drill only ever deletes what it recorded in `state/ledger.txt`:

- the Helm releases it installed (`helm uninstall` in the drill namespace),
- the Raft TLS secret it created,
- the namespace, but only if the drill created it — a namespace that already
  existed is rejected before installing any release. Both namespace arguments
  must name fresh disposable namespaces; they may name the same namespace.

Cleanup runs on `EXIT`, `INT` and `TERM`. It first stops port-forward tunnels
and reads a validated ownership-ledger snapshot. An unreadable, missing or
malformed ledger is an error; destructive cleanup is refused rather than
guessing resource ownership. Runtime-image collection also reports such a
ledger as unavailable, rather than an empty release list.

Cleanup attempts all recorded operations even when one fails. Namespace
deletion waits for completion with a timeout; failed API/Helm operations are
retained in the private `state/cleanup-errors.log`. An incomplete cleanup
makes the final drill status failed and its exit code nonzero. The report's
`cleanup` object records `status` (`completed`, `failed`, or `kept`) and
`failed_operations`. Inspect the retained ledger and restore API access or
permissions before manually removing resources left by a failed cleanup.

The runner stages evidence privately before cleanup and publishes final
`results.json` afterward. A passing public report therefore requires both
scenario/evidence validation and successful cleanup. Captured runtime IDs
remain the pre-cleanup sample.

`--keep-on-fail` skips the object removal when the drill failed, so the
failure can be inspected on the cluster. The tunnels are still stopped and the
evidence directory is still written.

Secrets never reach stdout: the generated admin passwords, the encryption key
and the cluster secret only exist in the `0600` files under `state/` and in the
`0600` generated Helm values files.

## Not covered by the drill

These aspects are deliberately **not** exercised yet. They are gaps, not
known failures:

- **Certificate rotation.** The drill generates a disposable CA and a leaf
  certificate for the Raft mTLS path, but it never rotates them. There is no
  scenario that replaces the CA or the peer certificate while the cluster is
  running, so the reloading behaviour of the Raft TLS material is unproven.
- **CA and peer certificate renewal under load.** Same gap, seen from the
  peer side: no scenario re-signs a peer and checks that the running cluster
  accepts the new material.
- **Voter membership changes.** Membership is fixed at three voters. Joining a
  fourth voter, removing a voter and the recovery from a replaced member are
  not covered.
- **Storage failures.** The quorum scenario stops voters, but no scenario
  corrupts or detaches a PVC, fills the disk, or corrupts the WAL.
- **Ingress, TLS termination and the dashboard.** The drill talks to the API
  through port-forward tunnels on `127.0.0.1`; ingress, `config.server.tls`
  and the browser path are not covered.
- **Backups and restores.** The drill never snapshots or restores. The backup
  and restore procedure in `docs/deployment/production-runbook.md` is a
  separate, manual drill.
- **Multi-day stability and capacity.** A single drill run is a functional
  check, not a soak. The bounded soak harness
  (`scripts/production-soak.sh`) covers the local load side.

Two runner details are worth knowing before the first real run:

- The `raft-mtls` scenario addresses the voters by **pod name**
  (`<pod>.<service>.<namespace>.svc.<domain>`) on the Raft port. That name
  has to resolve from wherever the drill runs. On a workstation it usually
  does not, so run the drill from inside the cluster network (a CI pod, for
  example) or make the pod DNS names resolvable.
- The drill sets `ANUBIS_SSRF_ALLOW_PRIVATE=1` on the drill workloads, because
  the probe scenario targets the drill's own in-cluster endpoint. The chart
  exposes no value for this, so the variable is applied to the workload
  directly after the install.

## Files

| File | Role |
|---|---|
| `scripts/k8s-drill/k8s-drill.sh` | Entry point: options, preflight, setup, dispatch, cleanup, results. |
| `scripts/k8s-drill/lib.sh` | Shared helpers: cluster access, resource ledger, API calls, port-forwards, certificate generation. |
| `scripts/k8s-drill/scenarios.sh` | The scenario implementations and the structured check recorder. |
