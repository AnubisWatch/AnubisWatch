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

## Supported Topology and Scaling

The production Kubernetes artifacts support one standalone replica backed by
one ReadWriteOnce PVC. Do not scale the Deployment or enable HPA: concurrent
standalone writers cannot safely share the embedded store. The Helm chart
rejects unsafe replica counts and cluster mode until deterministic bootstrap /
join wiring and Raft mTLS are supplied and validated.

Docker Compose includes a cluster profile for controlled development and
failure testing. Do not promote that profile to production without a separate
formation, failover, peer-security, backup, and restore review.

## Storage

Docker Compose uses named volumes. The supported Kubernetes Deployment uses
`anubiswatch-data`, mounted at `/data`. Back up the data volume and the
corresponding encryption key together; neither is useful without the other.

## Networking

### Ports

| Port | Protocol | Description |
|------|----------|-------------|
| 8080 | HTTP | Web UI and API |
| 7946 | TCP | Raft cluster communication (development cluster profile only) |

### Service Discovery

- Docker Compose cluster profile: service names resolve to container IPs.
- Supported Kubernetes deployment: no peer discovery is required.

## Monitoring

Enable Prometheus metrics:

```bash
kubectl apply -f deploy/k8s/monitoring.yaml
```

Metrics endpoint: `/metrics`

## Backup

### Data Backup

Run backup/restore with the server stopped so the CLI can open the embedded
store exclusively. For Kubernetes, scale the standalone Deployment to zero,
mount its PVC in a one-shot maintenance Pod that uses the same image and secret,
and run:

```bash
/bin/anubis backup create --include-history --output /data/backups/anubis.json.gz
/bin/anubis backup info /data/backups/anubis.json.gz
```

Copy the verified backup off-cluster, then restart the Deployment. Keep the
matching encryption key in the secret manager.

### Restore

With the server still stopped and the same PVC/key mounted in a maintenance
Pod, restore and verify before starting traffic:

```bash
/bin/anubis restore /data/backups/anubis.json.gz --force
/bin/anubis backup info /data/backups/anubis.json.gz
```

Rehearse this procedure in staging and record the backup checksum, restore
result, startup readiness time, and post-restore smoke result.

## Troubleshooting

### Check runtime status

```bash
kubectl -n anubiswatch get deploy,pod,svc,pvc
kubectl -n anubiswatch port-forward svc/anubiswatch 8080:8080
curl -fsS http://127.0.0.1:8080/ready
```

### View logs

```bash
kubectl logs -f deployment/anubiswatch -n anubiswatch

# Development cluster profile only
docker logs -f anubis-1
```

### Health check

```bash
curl -fsS http://localhost:8080/health
curl -fsS http://localhost:8080/ready
```
