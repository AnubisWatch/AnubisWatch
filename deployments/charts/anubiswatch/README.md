# AnubisWatch Helm Chart

[Helm](https://helm.sh) chart for deploying [AnubisWatch](https://github.com/AnubisWatch/anubiswatch) on Kubernetes.

## Repository

```bash
helm repo add anubiswatch https://anubiswatch.github.io/helm-charts
helm repo update
```

## Installation

### Basic Install

```bash
helm install anubis anubiswatch/anubiswatch \
  --namespace monitoring \
  --create-namespace \
  --set secrets.adminPassword='REPLACE_WITH_A_STRONG_PASSWORD_1!'
```

### With Custom Values

```bash
helm install anubis anubiswatch/anubiswatch \
  --namespace monitoring \
  --create-namespace \
  -f values.yaml
```

### With Ingress

```bash
helm install anubis anubiswatch/anubiswatch \
  --namespace monitoring \
  --create-namespace \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=anubis.example.com \
  --set ingress.tls[0].secretName=anubiswatch-tls \
  --set ingress.tls[0].hosts[0]=anubis.example.com
```

## Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `1` |
| `image.repository` | Container image repository | `ghcr.io/anubiswatch/anubiswatch` |
| `image.tag` | Container image tag | `latest` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `service.type` | Kubernetes service type | `ClusterIP` |
| `service.httpPort` | HTTP service port | `8443` |
| `service.raftPort` | Raft service port | `7946` |
| `ingress.enabled` | Enable ingress | `false` |
| `ingress.className` | Ingress class name | `nginx` |
| `persistence.enabled` | Enable persistent storage | `true` |
| `persistence.size` | Persistent volume size | `10Gi` |
| `config.server.port` | Server bind port | `8443` |
| `config.logging.level` | Log level | `info` |
| `config.necropolis.enabled` | Enable cluster mode | `false` |
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `512Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `256Mi` |

## Cluster Mode

Enable Raft cluster mode with multiple replicas:

```yaml
# values.yaml
replicaCount: 3

config:
  necropolis:
    enabled: true
    region: "default"

secrets:
  adminPassword: "REPLACE_WITH_A_STRONG_PASSWORD_1!"
  clusterSecret: "REPLACE_WITH_A_CLUSTER_SECRET_1!"
```

```bash
helm install anubis anubiswatch/anubiswatch -f values.yaml
```

## Persistence

AnubisWatch uses CobaltDB for embedded storage. Enable persistence for data durability:

```yaml
persistence:
  enabled: true
  storageClass: "gp3"
  size: 10Gi
```

## Security

The chart follows security best practices:

- Runs as non-root (UID 65534)
- Read-only root filesystem
- Drops all capabilities
- No privilege escalation

## Monitoring

### Prometheus Metrics

AnubisWatch exposes Prometheus metrics at `/metrics`:

```yaml
# ServiceMonitor for Prometheus Operator
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: anubiswatch
  namespace: monitoring
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: anubiswatch
  endpoints:
    - port: http
      path: /metrics
      interval: 30s
```

### Grafana Dashboard

Import the provided Grafana dashboard or create custom dashboards using the exposed metrics.

## Upgrading

```bash
helm upgrade anubis anubiswatch/anubiswatch \
  --namespace monitoring \
  -f values.yaml
```

## Uninstalling

```bash
helm uninstall anubis --namespace monitoring
```

**Warning:** This will delete all data. Backup your data first.

## Troubleshooting

### Pod Not Starting

```bash
kubectl get pods -n monitoring
kubectl describe pod anubis-0 -n monitoring
kubectl logs anubis-0 -n monitoring
```

### Cluster Not Forming

Ensure all pods can communicate on ports 7946 (TCP/UDP):

```bash
kubectl get endpoints anubiswatch-headless -n monitoring
kubectl run test --rm -it --image=busybox --restart=Never -- nc anubis-0.anubiswatch-headless 7946
```

## Support

- **Documentation:** https://github.com/AnubisWatch/anubiswatch
- **Issues:** https://github.com/AnubisWatch/anubiswatch/issues
- **GHCR:** https://github.com/AnubisWatch/anubiswatch/pkgs/container/anubiswatch

---

**⚖️ The Judgment Never Sleeps**
