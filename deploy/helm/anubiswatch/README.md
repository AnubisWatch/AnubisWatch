# AnubisWatch Helm Chart

Zero-dependency, single-binary uptime and synthetic monitoring platform.

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
| `image.tag` | Image tag (defaults to chart `appVersion`) | `""` |
| `replicaCount` | Standalone replica count; must remain `1` | `1` |
| `statefulSet.replicas` | Reserved for a separately validated cluster manifest | `3` |
| `config.logging.level` | Log level (debug/info/warn/error) | `info` |
| `config.storage.path` | Data directory mounted in the container | `/data` |
| `config.necropolis.enabled` | Enable cluster mode | `false` |
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
  enabled: true
  minAvailable: 3
```

## Upgrading

```bash
helm upgrade anubiswatch anubiswatch/anubiswatch \
  --namespace anubiswatch \
  -f values.yaml
```

## Uninstalling

```bash
helm uninstall anubiswatch -n anubiswatch
```

## Supported Topology

The production chart supports one standalone AnubisWatch replica with one PVC.
`replicaCount` must remain `1` and autoscaling must remain disabled because the
embedded store cannot be mounted and mutated by multiple standalone processes.
The Deployment uses `Recreate` so upgrades do not overlap two writers.

The codebase contains Raft clustering primitives, but this chart intentionally
refuses `config.necropolis.enabled=true`: it does not yet provide deterministic
ordinal bootstrap/join wiring or Raft mTLS material. Use a separately reviewed
cluster manifest only after validating formation, failover, backup/restore, and
peer transport security in the target environment.

## Storage

The supported Deployment mounts one persistent volume at `/data`. Retain and
back up that PVC; deleting it deletes the embedded database.

## Monitoring

Enable Prometheus ServiceMonitor:

```yaml
monitoring:
  enabled: true
  serviceMonitor:
    enabled: true
```

Metrics available at `/metrics` endpoint.

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
