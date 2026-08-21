# Deployment Guide

## Table of Contents

1. [Quick Start](#quick-start)
2. [Single Node Deployment](#single-node-deployment)
3. [Multi-Node Cluster](#multi-node-cluster)
4. [Docker Deployment](#docker-deployment)
5. [Kubernetes Deployment](#kubernetes-deployment)
6. [Production Checklist](#production-checklist)

## Quick Start

### Binary Installation

```bash
# Download latest release
curl -L https://github.com/AnubisWatch/AnubisWatch/releases/latest/download/anubis-linux-amd64 -o anubis
chmod +x anubis

# Initialize configuration
./anubis init

# Start server (single-node mode — no cluster peers, no leader election)
./anubis serve --single
```

> `--single` runs the node standalone: it self-elects as leader, gossip is
> disabled, and `ANUBIS_CLUSTER_SECRET` is not required. Set the secret
> (and remove `--single`) only when you scale to multiple nodes — the
> Helm chart enforces this in `deploy/helm/anubiswatch/templates/secret.yaml`
> by failing the install when `config.necropolis.enabled=true` and the
> secret is empty.

Access the dashboard at `http://localhost:8080`

## Single Node Deployment

### Configuration

Create `anubis.yaml`:

```yaml
server:
  host: 0.0.0.0
  port: 8080
  tls:
    enabled: true
    cert: /etc/anubis/server.crt
    key: /etc/anubis/server.key

storage:
  path: /var/lib/anubis/data

logging:
  level: info
  format: json

channels:
  - name: smtp
    type: email
    email:
      smtp_host: smtp.gmail.com
      smtp_port: 587
      username: alerts@example.com
      password: ${SMTP_PASSWORD}
      from: alerts@example.com
      to: [ops@example.com]
```

### Systemd Service

Create `/etc/systemd/system/anubis.service`:

```ini
[Unit]
Description=AnubisWatch Monitoring Platform
After=network.target

[Service]
Type=simple
User=anubis
Group=anubis
ExecStart=/usr/local/bin/anubis serve --config /etc/anubis/anubis.yaml
Restart=always
RestartSec=5
Environment="SMTP_PASSWORD=secret"

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl enable anubis
sudo systemctl start anubis
```

## Multi-Node Cluster

### Bootstrap First Node

```bash
# On node-1
anubis serve \
  --bootstrap \
  --node-id node-1 \
  --bind-addr 0.0.0.0:7946 \
  --advertise-addr 10.0.0.1:7946
```

### Join Additional Nodes

```bash
# On node-2
anubis serve \
  --join 10.0.0.1:7946 \
  --node-id node-2 \
  --bind-addr 0.0.0.0:7946 \
  --advertise-addr 10.0.0.2:7946

# On node-3
anubis serve \
  --join 10.0.0.1:7946 \
  --node-id node-3 \
  --bind-addr 0.0.0.0:7946 \
  --advertise-addr 10.0.0.3:7946
```

### Verify Cluster

```bash
# Check cluster status
anubis necropolis

# Output:
# Necropolis Status
# ================
# Node ID: node-1
# State: Leader
# Term: 5
# Leader: node-1
# Peers: 3
```

## Docker Deployment

### Single Node

```bash
docker run -d \
  --name anubis \
  -p 8080:8080 \
  -v anubis-data:/data \
  ghcr.io/anubiswatch/anubiswatch:latest \
  serve --single
```

No `ANUBIS_CLUSTER_SECRET` is required in single-node mode. The
binary self-elects as leader and the gossip/join path is disabled.
Set the secret only when scaling to multiple nodes (see "Join
Additional Nodes" above).

### Multi-Node with Docker Compose

See `deploy/docker/docker-compose.yml`:

```bash
cd deploy/docker
docker-compose up -d
```

This creates a 3-node cluster with:
- Node 1: Bootstrap node (accessible on port 8080)
- Node 2: Joins node 1 (port 8081)
- Node 3: Joins node 1 (port 8082)

## Kubernetes Deployment

### Using Helm

```bash
# Add repository (when published)
helm repo add anubiswatch https://charts.anubis.watch
helm repo update

# Install with default values
helm install anubiswatch anubiswatch/anubiswatch \
  --namespace anubiswatch \
  --create-namespace \
  --set secrets.adminPassword="$ANUBIS_ADMIN_PASSWORD"

# Install with custom values
helm install anubiswatch anubiswatch/anubiswatch \
  --namespace anubiswatch \
  --create-namespace \
  -f values-production.yaml
```

### Using Raw Manifests

The raw manifests support one standalone replica. Create the credentials from a
secret manager before applying the workload; do not edit or commit plaintext
secrets.

```bash
kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/configmap.yaml
kubectl create secret generic anubiswatch-secrets \
  --namespace anubiswatch \
  --from-literal=admin-password="$ANUBIS_ADMIN_PASSWORD" \
  --from-literal=encryption-key="$ANUBIS_ENCRYPTION_KEY" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f deploy/k8s/pvc.yaml \
  -f deploy/k8s/service.yaml \
  -f deploy/k8s/deployment.yaml
```

`deploy/k8s/base.yaml` is an equivalent aggregate bundle, but its intentionally
empty Secret must be populated before the Deployment can start.

### Production Helm Values

Start from `deploy/helm/anubiswatch/values-production.example.yaml`, copy it
to `values-production.yaml`, and keep the filled file out of version control.
Populate `secrets.*` from your deployment secret store before running preflight
or deploy. The supported chart topology is one standalone replica:

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
  logging:
    level: warn
  storage:
    path: /data
    encryption:
      enabled: true
  necropolis:
    enabled: false

secrets:
  adminPassword: ""
  encryptionKey: ""

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

### Scaling Boundary

Do not scale the standalone Deployment above one replica and do not enable its
HPA: multiple processes cannot safely mutate the same embedded database/PVC.
The chart enforces this invariant and currently rejects
`config.necropolis.enabled=true` because deterministic ordinal bootstrap/join
and Raft mTLS are not yet supplied by the chart. Use a separately reviewed
cluster manifest only after target-environment formation, failover,
backup/restore, and peer-security validation.

## Production Checklist

For an operator-focused deployment, smoke-test, and rollback flow, use the
[Production Deployment Runbook](production-runbook.md).

### Security

- [ ] Enable TLS for all communications
- [ ] Use strong authentication (bcrypt + JWT)
- [ ] Configure rate limiting
- [ ] Set up firewall rules
- [ ] Enable audit logging
- [ ] Rotate secrets regularly
- [ ] Use RBAC for access control
- [ ] Configure CORS origins via `ANUBIS_CORS_ORIGINS` env var or `server.allowed_origins` config

### Reliability

- [ ] Keep the supported standalone Deployment at exactly one replica
- [ ] Snapshot and restore-test the `/data` PVC and encryption key together
- [ ] Verify startup, readiness, liveness, and graceful SIGTERM behavior
- [ ] Rehearse Helm rollback with the production smoke script
- [ ] Capacity-test WAL replay time and steady-state load against the target PVC
- [ ] Set up monitoring and alerting for readiness, restarts, disk, and latency

### Performance

- [ ] Use SSD storage
- [ ] Allocate sufficient CPU/memory
- [ ] Configure appropriate retention
- [ ] Validate outbound connection limits for the configured probe mix
- [ ] Measure startup WAL replay against the expected retained dataset
- [ ] Load test with expected traffic

### Monitoring

- [ ] Configure Prometheus metrics
- [ ] Set up Grafana dashboards
- [ ] Enable structured logging
- [ ] Configure log aggregation
- [ ] Set up alerting rules
- [ ] Monitor cluster health

### Backup & Recovery

```bash
# Create encrypted backup
kubectl exec -it anubiswatch-0 -n anubiswatch -- /bin/anubis backup --encrypt

# Restore backup (key stored at backups/.backup_key)
kubectl exec -it anubiswatch-0 -n anubiswatch -- /bin/anubis restore --input backup.db

# Set backup encryption key (via env var)
ANUBIS_BACKUP_ENCRYPTION_KEY=your-32-byte-key
```

### CORS Configuration

If the dashboard is served from a different origin than the API, configure allowed origins:

```bash
# Via environment variable
ANUBIS_CORS_ORIGINS=https://dashboard.example.com

# Via config file
server:
  allowed_origins:
    - https://dashboard.example.com
```

If not configured, CORS defaults to deny all (production-safe default).

### Log Rotation

AnubisWatch outputs JSON logs to stdout. For production, configure log rotation:

**Via Docker:**
```bash
docker run ... -v /var/lib/anubis/logs:/var/log/anubis \
  anubis serve --single 2>&1 | rotatelogs -f -p 7D /var/log/anubis/anubis.%Y%m%d.log
```

**Via logrotate:**
```bash
# /etc/logrotate.d/anubis
/var/log/anubis/*.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
}
```

**In Kubernetes:** Use a sidecar or log collector (Fluentd, Fluent Bit, Vector).

### Troubleshooting

```bash
# Check node status
anubis necropolis

# View logs
kubectl logs -f anubiswatch-0 -n anubiswatch

# Check metrics
curl http://localhost:8080/metrics

# Health check
curl http://localhost:8080/health
curl http://localhost:8080/ready
```
