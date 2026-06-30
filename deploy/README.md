# AnubisWatch Deployment

Production deployment configurations for AnubisWatch.

## Directory Structure

```
deploy/
├── helm/              # Helm charts for Kubernetes
│   └── anubiswatch/
├── k8s/               # Raw Kubernetes manifests
│   └── base.yaml
└── docker/            # Docker Compose configurations
    └── docker-compose.yml
```

## Quick Start

### Docker Compose (Development)

```bash
cd deploy/docker
docker-compose up -d
```

Access the UI at http://localhost:8080

### Kubernetes (Production)

```bash
# Using raw manifests
kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/configmap.yaml
kubectl create secret generic anubiswatch-secrets \
  --namespace anubiswatch \
  --from-literal=admin-password="$ANUBIS_ADMIN_PASSWORD" \
  --from-literal=encryption-key="$ANUBIS_ENCRYPTION_KEY" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f deploy/k8s/pvc.yaml -f deploy/k8s/service.yaml -f deploy/k8s/deployment.yaml

# Using Helm
helm install anubiswatch deploy/helm/anubiswatch \
  --namespace anubiswatch \
  --create-namespace \
  --set secrets.adminPassword="$ANUBIS_ADMIN_PASSWORD"
```

For production promotion, rollback, and smoke-test steps, use
[`docs/deployment/production-runbook.md`](../docs/deployment/production-runbook.md).

## Scaling

### Docker Compose

Edit `docker-compose.yml` and add more nodes:

```yaml
anubis-4:
  extends:
    file: docker-compose.yml
    service: anubis-2
  container_name: anubis-4
  ports:
    - "8083:8080"
```

### Kubernetes

Scale the StatefulSet:

```bash
kubectl scale statefulset anubiswatch --replicas=5 -n anubiswatch
```

Or update Helm values:

```yaml
statefulSet:
  replicas: 5
```

## Storage

### Docker Compose

Data is stored in named volumes:
- `anubis-data-1`
- `anubis-data-2`
- `anubis-data-3`

### Kubernetes

Uses PersistentVolumeClaims with StatefulSet. Each pod gets its own storage.

## Networking

### Ports

| Port | Protocol | Description |
|------|----------|-------------|
| 8080 | HTTP | Web UI and API |
| 7946 | TCP | Raft cluster communication |

### Service Discovery

- Docker Compose: Service names resolve to container IPs
- Kubernetes: Headless service for pod-to-pod communication

## Monitoring

Enable Prometheus metrics:

```bash
kubectl apply -f deploy/k8s/monitoring.yaml
```

Metrics endpoint: `/metrics`

## Backup

### Data Backup

```bash
# Kubernetes
kubectl exec -it anubiswatch-0 -n anubiswatch -- /bin/anubis backup > backup.db

# Docker
docker exec anubis-1 /bin/anubis backup > backup.db
```

### Restore

```bash
# Kubernetes
kubectl cp backup.db anubiswatch-0:/data/anubis.db -n anubiswatch

# Docker
docker cp backup.db anubis-1:/data/anubis.db
```

## Troubleshooting

### Check cluster status

```bash
# Kubernetes
kubectl exec -it anubiswatch-0 -n anubiswatch -- /bin/anubis necropolis

# Docker
docker exec anubis-1 /bin/anubis necropolis
```

### View logs

```bash
# Kubernetes
kubectl logs -f anubiswatch-0 -n anubiswatch

# Docker
docker logs -f anubis-1
```

### Health check

```bash
# All nodes
curl http://localhost:8080/health
curl http://localhost:8081/health
curl http://localhost:8082/health
```
