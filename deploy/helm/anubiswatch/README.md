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
  --create-namespace
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
| `image.repository` | Image repository | `anubiswatch/anubis` |
| `image.tag` | Image tag | `3.0.0` |
| `statefulSet.replicas` | Number of cluster nodes | `3` |
| `config.logging.level` | Log level (debug/info/warn/error) | `info` |
| `config.storage.path` | Data directory mounted in the container | `/data` |
| `config.necropolis.enabled` | Enable cluster mode | `false` |
| `service.type` | Service type | `ClusterIP` |
| `service.httpPort` | HTTP service port | `8080` |
| `service.grpcPort` | gRPC service port | `9090` |
| `service.clusterPort` | Raft service port | `7946` |
| `ingress.enabled` | Enable ingress | `false` |
| `persistence.enabled` | Enable persistent storage | `true` |
| `persistence.size` | Storage size | `10Gi` |

## Production Values

Create a `values-production.yaml`:

```yaml
statefulSet:
  replicas: 5

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
  logging:
    level: warn
  storage:
    path: /data
  necropolis:
    enabled: true

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

## Cluster Formation

AnubisWatch uses Raft consensus for clustering. When deploying with multiple replicas:

1. The first pod (anubiswatch-0) bootstraps the cluster
2. Subsequent pods join automatically
3. Cluster requires majority (n/2+1) nodes for consensus

## Storage

StatefulSet uses persistent volumes for data. Each pod gets its own PVC:
- `anubiswatch-data-anubiswatch-0`
- `anubiswatch-data-anubiswatch-1`
- `anubiswatch-data-anubiswatch-2`

## Monitoring

Enable Prometheus ServiceMonitor:

```yaml
monitoring:
  enabled: true
  serviceMonitor:
    enabled: true
```

Metrics available at `/metrics` endpoint.

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
