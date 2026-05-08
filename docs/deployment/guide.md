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

# Start server
./anubis serve --single
```

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
  --set secrets.adminPassword='REPLACE_WITH_A_STRONG_PASSWORD_1!'

# Install with custom values
helm install anubiswatch anubiswatch/anubiswatch \
  --namespace anubiswatch \
  --create-namespace \
  -f values-production.yaml
```

### Using Raw Manifests

```bash
# First edit deploy/k8s/secret.yaml and replace admin-password: "CHANGE_ME"
# with a strong password that satisfies the local auth policy.
kubectl apply -f deploy/k8s/base.yaml
```

### Production Helm Values

```yaml
# values-production.yaml
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
    path: /var/lib/anubis/data
  necropolis:
    enabled: true
  channels:
    - name: smtp
      type: email
      email:
        smtp_host: smtp.gmail.com
        smtp_port: 587
        from: alerts@example.com
        to: [ops@example.com]

monitoring:
  enabled: true
  serviceMonitor:
    enabled: true

secrets:
  adminPassword: "REPLACE_WITH_A_STRONG_PASSWORD_1!"
  clusterSecret: "REPLACE_WITH_A_CLUSTER_SECRET_1!"

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

### Scaling

```bash
# Scale StatefulSet when config.necropolis.enabled=true
kubectl scale statefulset anubiswatch --replicas=5 -n anubiswatch

# Or via Helm
helm upgrade anubiswatch anubiswatch/anubiswatch \
  --namespace anubiswatch \
  --set config.necropolis.enabled=true \
  --set statefulSet.replicas=5 \
  --set secrets.adminPassword='REPLACE_WITH_A_STRONG_PASSWORD_1!' \
  --set secrets.clusterSecret='REPLACE_WITH_A_CLUSTER_SECRET_1!'
```

## Production Checklist

### Security

- [ ] Enable TLS for all communications
- [ ] Use strong authentication (bcrypt + JWT)
- [ ] Configure rate limiting
- [ ] Set up firewall rules
- [ ] Enable audit logging
- [ ] Rotate secrets regularly
- [ ] Use RBAC for access control

### Reliability

- [ ] Deploy at least 3 nodes for HA
- [ ] Configure PodDisruptionBudget
- [ ] Set up health checks
- [ ] Configure backup strategy
- [ ] Test failover scenarios
- [ ] Set up monitoring and alerting

### Performance

- [ ] Use SSD storage
- [ ] Allocate sufficient CPU/memory
- [ ] Configure appropriate retention
- [ ] Enable connection pooling
- [ ] Tune Raft timeouts for network latency
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
# Create backup
kubectl exec -it anubiswatch-0 -n anubiswatch -- /bin/anubis backup > backup.db

# Restore backup
kubectl cp backup.db anubiswatch-0:/data/anubis.db -n anubiswatch
```

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
