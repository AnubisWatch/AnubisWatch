# AnubisWatch Helm Chart

Single-binary uptime and synthetic monitoring platform that needs no external runtime services.

## Prerequisites

- Kubernetes 1.24+
- Helm 3.8+

## Installation

### Add the repository (when published)

```bash
helm repo add anubiswatch https://charts.anubis.watch
helm repo update
```

### Install the chart

```bash
helm install anubiswatch anubiswatch/anubiswatch \
  --namespace anubiswatch \
  --create-namespace \
  --set secrets.adminPassword="$ANUBIS_ADMIN_PASSWORD"
```

### Install with custom values

```bash
helm install anubiswatch anubiswatch/anubiswatch \
  --namespace anubiswatch \
  --create-namespace \
  -f values-production.yaml
```

## Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Image repository | `ghcr.io/anubiswatch/anubiswatch` |
| `image.tag` | Image tag; clustered mode requires an explicit reviewed non-latest tag or digest | `""` |
| `image.digest` | Optional immutable sha256 registry digest, preferred over tag | `""` |
| `replicaCount` | Standalone replica count; must remain `1` | `1` |
| `statefulSet.replicas` | Fixed cluster voter count; must be `3` | `3` |
| `config.logging.level` | Log level (debug/info/warn/error) | `info` |
| `config.storage.path` | Data directory mounted in the container | `/data` |
| `config.necropolis.enabled` | Opt in to the fixed three-voter StatefulSet | `false` |
| `cluster.domain` | Kubernetes DNS cluster suffix | `cluster.local` |
| `cluster.raftTLSSecret` | Existing peer mTLS Secret with `tls.crt`, `tls.key`, `ca.crt` | `""` |
| `config.server.metricsAuth` | Require a workspace token for monitor metrics | `true` |
| `serviceMonitor.bearerTokenSecret` | Existing Secret with a `token` key used to authenticate the ServiceMonitor scrape; required for a working scrape while `metricsAuth` is enabled | `""` |
| `service.type` | Service type | `ClusterIP` |
| `service.httpPort` | HTTP service port | `8080` |
| `service.grpcPort` | Management gRPC service port; `0` disables the listener and omits it from Pods/Services | `0` |
| `service.clusterPort` | Raft service port | `7946` |
| `ingress.enabled` | Enable ingress | `false` |
| `persistence.enabled` | Enable persistent storage | `true` |
| `persistence.size` | Storage size | `10Gi` |

## Production Values

Copy `values-production.example.yaml` to an environment-specific
`values-production.yaml`, keep that filled file out of version control, and
source secret values from your deployment secret store. A production-shaped
example is included with the chart:

```yaml
replicaCount: 1

resources:
  limits:
    cpu: 2000m
    memory: 2Gi
  requests:
    cpu: 500m
    memory: 512Mi

persistence:
  size: 50Gi
  storageClass: fast-ssd

config:
  server:
    allowedOrigins:
      - https://anubiswatch.example.com
  logging:
    level: warn
  storage:
    path: /data
  necropolis:
    enabled: false

secrets:
  # Fill from your deployment secret store before running preflight or deploy.
  adminPassword: ""
  encryptionKey: ""

monitoring:
  enabled: true
  serviceMonitor:
    enabled: true

pdb:
  # A single replica cannot be evicted with minAvailable: 1.
  enabled: false
  minAvailable: 1
```

## Upgrading

ConfigMap and chart-managed Secret changes alter the pod-template checksums,
so Helm upgrades restart the process to apply startup settings. Standalone uses
`Recreate` and therefore has a maintenance interruption; HA uses rolling updates.
The external peer TLS Secret is not owned by Helm: renew it using the staged
trust procedure below and explicitly restart voters. Changing an initial admin
password value does not rotate an existing persisted account; use the password
change/reset operation. Storage encryption-key rotation requires a verified
re-encryption/recovery procedure before changing the runtime secret.

```bash
helm upgrade anubiswatch anubiswatch/anubiswatch \
  --namespace anubiswatch \
  -f values.yaml
```

## Uninstalling

```bash
helm uninstall anubiswatch -n anubiswatch
```

## Supported Topologies

The default is one standalone replica with one PVC. `replicaCount` must remain
`1` and autoscaling must remain disabled because the embedded store cannot be
mutated by multiple standalone processes. The Deployment uses `Recreate` so
upgrades do not overlap two writers.

Cluster mode is opt-in and targets exactly three static Raft voters with one PVC
each. Use a reviewed image containing the replicated cluster-authentication
implementation; an old chart `appVersion` is not evidence of that capability.
Clustered mode therefore requires an explicit non-`latest` `image.tag` or a
sha256 `image.digest`. A registry digest is preferred for production promotion.
The namespace and release fullname are part of the persisted peer addresses;
do not rename or scale a running cluster.

Start from `values-production.example.yaml` and layer `values-ha.example.yaml`
after filling environment-specific values and secrets. HA mode additionally
requires:

- `config.auth.type: local`, exactly three voters, `Parallel` pod creation,
  persistent storage, and autoscaling disabled.
- Three schedulable Kubernetes nodes. The default required pod anti-affinity
  places voters on different hostnames. An explicit `affinity` override changes
  that failure-domain policy and requires independent availability validation.
- An existing `cluster.raftTLSSecret` in the release namespace containing
  `tls.crt`, `tls.key`, and `ca.crt`. The leaf certificate must be trusted by that
  CA, allow both server and client authentication, and cover
  `*.FULLNAME-headless.NAMESPACE.svc.DOMAIN` using the actual release fullname,
  namespace, and `cluster.domain` (default `cluster.local`).
- The same strong `secrets.clusterSecret` on every voter. HMAC membership
  authentication and verified client/server peer TLS are both required; missing
  certificate material fails pod startup instead of falling back to plaintext.

Each ordinal receives its own static configuration with exactly the other two
voters. All configurations set `bootstrap: false` and disable gossip. This forms
a quorum from the predefined peer set and avoids an empty replacement PVC
bootstrapping a competing one-node cluster. The headless Service publishes
not-yet-ready addresses so initial peers can find each other. `Parallel` startup
is required because waiting for one ready voter before creating the next would
prevent initial quorum formation.

Do not toggle standalone/cluster on an existing release. Real Helm upgrades
reject an opposite workload with the same release name; install a separate
release and perform a verified offline backup/restore migration. Offline
`helm template` cannot detect cluster resources unless server lookup is enabled.
Existing standalone or legacy per-node authentication files are not merged into
one shared authority automatically. Start a fresh HA release or perform an
explicit, reviewed migration that selects the authoritative user and credential
state; verify cluster-wide password rotation and session revocation before use.
HA-to-HA upgrades retain existing PVCs and use one-at-a-time StatefulSet rolling
updates. A recommended cluster disruption budget is `minAvailable: 2`; a
standalone PDB with `minAvailable: 1` blocks voluntary maintenance.

Static voter membership is intentionally fixed. Do not use an HPA, edit the
StatefulSet replica count, or call membership add/remove operations on these
managed voters without a separately designed migration. Retain a pre-upgrade
backup and validate pod loss, leader loss, restart, quorum loss/recovery, and
cross-pod session behavior with the exact image before production promotion.
Local cluster validation is not a guarantee about cloud storage or zone failure.

Peer TLS material is managed outside this chart. Renew leaf certificates with a
compatible trust bundle, then restart voters one at a time. A CA replacement
needs a staged old+new trust bundle before switching leaf certificates. Shared
HMAC secret rotation is a coordinated maintenance operation; mixed old/new keys
cannot communicate, so do not treat it as a zero-downtime ordinary rollout.

## Storage

The standalone Deployment mounts one persistent volume at `/data`. Clustered
voters each mount their own claim at `config.storage.path`. Retain and back up
these PVCs; deleting them deletes the embedded databases. A cluster is not a
replacement for offsite backup or a recovery drill.

## Monitoring

Enable Prometheus ServiceMonitor:

```yaml
monitoring:
  enabled: true
  serviceMonitor:
    enabled: true
```

Metrics are protected by default (`config.server.metricsAuth: true`). Each
scrape returns only the token's workspace monitor data. Supply an existing
Secret to the ServiceMonitor:

```yaml
monitoring:
  enabled: true
  serviceMonitor:
    enabled: true
    bearerTokenSecret:
      name: anubis-metrics-token
      key: token
```

Manage the workspace token and its rotation in your secret store. Setting
`metricsAuth: false` explicitly exposes global metrics; limit that endpoint to a
trusted monitoring network and keep it off the public ingress.

## Management gRPC

The management gRPC API is disabled by default because it authenticates with
bearer tokens. `service.grpcPort: 0` omits the listener and the corresponding
Pod/Service ports. To enable it, set a positive port and either configure
`config.server.tls` with certificate files available inside the container or
bind `config.server.host` to a literal loopback IP for a local-only tunnel.
TLS termination on the chart's HTTP ingress does **not** protect the separate
gRPC listener.

## TLS

Enable TLS via ingress:

```yaml
ingress:
  enabled: true
  hosts:
    - host: anubiswatch.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - hosts:
        - anubiswatch.example.com
      secretName: anubiswatch-tls
```
