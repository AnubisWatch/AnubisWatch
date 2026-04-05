# AnubisWatch Backup and Disaster Recovery Guide

## Overview

This guide covers backup strategies, recovery procedures, and disaster recovery planning for AnubisWatch deployments.

---

## What to Backup

### Critical Data

| Data Type | Location | Priority | RPO |
|-----------|----------|----------|-----|
| Soul configurations | `data/*/souls/` | Critical | 24h |
| Alert channels | `data/*/channels/` | Critical | 24h |
| Alert rules | `data/*/rules/` | Critical | 24h |
| Workspaces | `data/*/workspaces/` | Critical | 24h |
| Status pages | `data/*/status-pages/` | High | 24h |
| Judgment history | `data/*/judgments/` | Medium | 7d |
| Time-series data | `data/*/timeseries/` | Low | 7d |

### Configuration Files

| File | Location | Priority |
|------|----------|----------|
| Main config | `/etc/anubis/anubis.yaml` or `./anubis.yaml` | Critical |
| TLS certificates | `data/acme/` or `/etc/ssl/anubis/` | Critical |
| Session data | `data/sessions.json` | High |
| Cluster state | `data/raft/` | Critical (clustered only) |

---

## Backup Strategies

### Strategy 1: Simple File Copy (Single Node)

For single-node deployments:

```bash
#!/bin/bash
# backup-anubis.sh

BACKUP_DIR="/backup/anubis"
DATA_DIR="/var/lib/anubis"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

# Create backup directory
mkdir -p "$BACKUP_DIR/$TIMESTAMP"

# Stop AnubisWatch (optional, for consistent backup)
systemctl stop anubis

# Copy data directory
cp -r "$DATA_DIR" "$BACKUP_DIR/$TIMESTAMP/data"

# Copy config
cp /etc/anubis/anubis.yaml "$BACKUP_DIR/$TIMESTAMP/"

# Restart service
systemctl start anubis

# Compress backup
tar -czf "$BACKUP_DIR/anubis-backup-$TIMESTAMP.tar.gz" \
    -C "$BACKUP_DIR/$TIMESTAMP" .

# Remove uncompressed copy
rm -rf "$BACKUP_DIR/$TIMESTAMP"

# Clean old backups (keep 30 days)
find "$BACKUP_DIR" -name "anubis-backup-*.tar.gz" -mtime +30 -delete

echo "Backup completed: $BACKUP_DIR/anubis-backup-$TIMESTAMP.tar.gz"
```

### Strategy 2: Hot Backup with rsync

For minimal downtime:

```bash
#!/bin/bash
# hot-backup-anubis.sh

BACKUP_DIR="/backup/anubis"
DATA_DIR="/var/lib/anubis"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

# Initial sync (while running)
rsync -av --delete "$DATA_DIR/" "$BACKUP_DIR/$TIMESTAMP/"

# Quick pause for consistency (optional)
# Stop writes for 1-2 seconds
systemctl stop anubis
rsync -av "$DATA_DIR/" "$BACKUP_DIR/$TIMESTAMP/"
systemctl start anubis

# Compress and cleanup
tar -czf "$BACKUP_DIR/anubis-backup-$TIMESTAMP.tar.gz" \
    -C "$BACKUP_DIR/$TIMESTAMP" .
rm -rf "$BACKUP_DIR/$TIMESTAMP"
```

### Strategy 3: Cluster Backup (Multi-Node)

For clustered deployments, backup from the leader:

```bash
#!/bin/bash
# cluster-backup-anubis.sh

LEADER_API="http://$(anubis cluster-status | jq -r '.leader'):8443"
BACKUP_DIR="/backup/anubis-cluster"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

# Get cluster state via API
curl -s -H "Authorization: Bearer $API_TOKEN" \
    "$LEADER_API/api/v1/cluster/status" > \
    "$BACKUP_DIR/cluster-status-$TIMESTAMP.json"

# Backup all nodes
for node in $(anubis necropolis | jq -r '.nodes[].address'); do
    NODE_DIR="$BACKUP_DIR/node-${node%:*}"
    mkdir -p "$NODE_DIR"
    
    # SSH to node and rsync data
    ssh "anubis@$node" "tar -cf - /var/lib/anubis" | \
        tar -xf - -C "$NODE_DIR"
done

# Compress
tar -czf "$BACKUP_DIR/cluster-backup-$TIMESTAMP.tar.gz" \
    "$BACKUP_DIR"
```

---

## Scheduled Backups

### systemd Timer

Create `/etc/systemd/system/anubis-backup.service`:

```ini
[Unit]
Description=AnubisWatch Backup
After=anubis.service

[Service]
Type=oneshot
ExecStart=/opt/scripts/backup-anubis.sh
User=root
```

Create `/etc/systemd/system/anubis-backup.timer`:

```ini
[Unit]
Description=Run AnubisWatch backup daily
Requires=anubis-backup.service

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
```

Enable the timer:
```bash
systemctl enable anubis-backup.timer
systemctl start anubis-backup.timer
```

### Cron Job

```bash
# /etc/cron.d/anubis-backup
# Daily backup at 2:00 AM
0 2 * * * root /opt/scripts/backup-anubis.sh >> /var/log/anubis-backup.log 2>&1
```

---

## Recovery Procedures

### Full Recovery (Single Node)

1. **Stop the service:**
   ```bash
   systemctl stop anubis
   ```

2. **Remove corrupted data:**
   ```bash
   rm -rf /var/lib/anubis/*
   ```

3. **Restore from backup:**
   ```bash
   tar -xzf /backup/anubis/anubis-backup-20260405-020000.tar.gz \
       -C /var/lib/anubis/
   ```

4. **Restore config:**
   ```bash
   cp /backup/anubis/anubis-backup-20260405-020000/anubis.yaml \
       /etc/anubis/
   ```

5. **Set permissions:**
   ```bash
   chown -R anubis:anubis /var/lib/anubis
   chmod -R 750 /var/lib/anubis
   ```

6. **Start the service:**
   ```bash
   systemctl start anubis
   ```

### Partial Recovery (Specific Soul)

To restore a single soul:

```bash
# Stop AnubisWatch
systemctl stop anubis

# Restore specific soul file
cp /backup/anubis/latest/data/default/souls/soul_abc123.json \
   /var/lib/anubis/default/souls/

# Restart
systemctl start anubis
```

### Cluster Recovery

**Scenario: Leader node failure**

1. **Promote a follower:**
   ```bash
   # On a follower node
   anubis banish <failed-leader-id>
   ```

2. **Restore the failed node:**
   ```bash
   # After hardware fix
   anubis serve --bootstrap=false
   ```

3. **Rejoin cluster:**
   ```bash
   # From healthy node
   anubis summon <restored-node-address>
   ```

**Scenario: Complete cluster failure**

1. **Identify node with most recent data:**
   ```bash
   # Check raft logs on each node
   cat /var/lib/anubis/raft/raft.log | tail -20
   ```

2. **Restore that node first** (see Full Recovery)

3. **Start as bootstrap leader:**
   ```bash
   anubis serve --bootstrap
   ```

4. **Re-add other nodes one at a time**

---

## Disaster Recovery Plan

### Recovery Time Objectives (RTO)

| Scenario | Target RTO |
|----------|------------|
| Single node failure | < 5 minutes |
| Full cluster failure | < 30 minutes |
| Data center loss | < 4 hours |
| Complete rebuild | < 24 hours |

### Recovery Point Objectives (RPO)

| Data Type | Target RPO |
|-----------|------------|
| Configuration | 24 hours |
| Alert history | 24 hours |
| Metrics data | 7 days (acceptable loss) |

### DR Runbook

#### Immediate Response (0-15 minutes)

1. **Assess the damage**
   - Check service status: `systemctl status anubis`
   - Check logs: `journalctl -u anubis -f`
   - Identify failure type (hardware, software, network)

2. **Failover (if clustered)**
   - Verify leader election: `anubis necropolis`
   - Update DNS/load balancer if needed

3. **Notify stakeholders**
   - Send incident notification
   - Create incident ticket

#### Short-term Recovery (15-60 minutes)

1. **Restore service**
   - Apply appropriate recovery procedure
   - Verify functionality

2. **Validate data integrity**
   - Check souls: `anubis judge`
   - Verify alerts are firing
   - Test API endpoints

3. **Document incident**
   - Record timeline
   - Note any data loss

#### Long-term Recovery (1-24 hours)

1. **Root cause analysis**
   - Analyze logs
   - Identify prevention measures

2. **Implement fixes**
   - Apply patches
   - Update configuration

3. **Update DR plan**
   - Document lessons learned
   - Adjust RTO/RPO if needed

---

## Testing Backups

### Monthly Backup Verification

```bash
#!/bin/bash
# test-backup.sh

BACKUP_FILE="$1"
TEST_DIR="/tmp/anubis-backup-test-$$"

# Create test directory
mkdir -p "$TEST_DIR"

# Extract backup
tar -xzf "$BACKUP_FILE" -C "$TEST_DIR"

# Verify structure
for dir in souls channels rules workspaces; do
    if [ ! -d "$TEST_DIR/data/default/$dir" ]; then
        echo "ERROR: Missing $dir directory"
        exit 1
    fi
done

# Verify config
if [ ! -f "$TEST_DIR/anubis.yaml" ]; then
    echo "ERROR: Missing config file"
    exit 1
fi

# Validate JSON files
for f in $(find "$TEST_DIR" -name "*.json"); do
    if ! jq empty "$f" 2>/dev/null; then
        echo "ERROR: Invalid JSON: $f"
        exit 1
    fi
done

# Cleanup
rm -rf "$TEST_DIR"

echo "Backup verification passed: $BACKUP_FILE"
```

### Annual DR Drill

1. **Schedule maintenance window**
2. **Simulate complete failure** (stop all nodes)
3. **Execute DR runbook**
4. **Measure actual RTO vs target**
5. **Document gaps**
6. **Update procedures**

---

## Cloud-Specific Considerations

### AWS

```bash
# EBS Snapshot
aws ec2 create-snapshot \
    --volume-id vol-xxxxxxxx \
    --description "AnubisWatch backup $(date +%Y%m%d)"

# S3 Backup
aws s3 cp /backup/anubis/ \
    s3://my-anubis-backups/ \
    --recursive
```

### Azure

```bash
# Managed Disk Snapshot
az snapshot create \
    --resource-group my-rg \
    --name anubis-backup-$(date +%Y%m%d) \
    --source /subscriptions/xxx/resourceGroups/my-rg/providers/Microsoft.Compute/disks/anubis-data

# Blob Storage Backup
az storage blob upload-batch \
    --destination anubis-backups \
    --source /backup/anubis/
```

### GCP

```bash
# Persistent Disk Snapshot
gcloud compute disks snapshot anubis-data \
    --snapshot-names=anubis-backup-$(date +%Y%m%d) \
    --zone=us-central1-a

# GCS Backup
gsutil -m cp -r /backup/anubis/ gs://my-anubis-backups/
```

---

## Encryption

### Backup Encryption with GPG

```bash
# Encrypt backup
gpg --encrypt \
    --recipient backup-key@company.com \
    --trust-model always \
    --output anubis-backup-$(date +%Y%m%d).tar.gz.gpg \
    anubis-backup-$(date +%Y%m%d).tar.gz

# Decrypt for restore
gpg --decrypt \
    --output anubis-backup-20260405.tar.gz \
    anubis-backup-20260405.tar.gz.gpg
```

### Encryption at Rest

For sensitive data, enable storage encryption:

```yaml
# anubis.yaml
storage:
  path: "/var/lib/anubis"
  encryption_key: "${ANUBIS_ENCRYPTION_KEY}"  # 32-byte key
  encryption_enabled: true
```

---

## Monitoring Backup Health

### Backup Status Endpoint

Add to your monitoring:

```bash
#!/bin/bash
# check-backup-status.sh

BACKUP_DIR="/backup/anubis"
MAX_AGE_HOURS=25

# Find latest backup
LATEST=$(ls -t "$BACKUP_DIR"/anubis-backup-*.tar.gz 2>/dev/null | head -1)

if [ -z "$LATEST" ]; then
    echo "CRITICAL: No backups found"
    exit 2
fi

# Check age
BACKUP_AGE=$(( ($(date +%s) - $(stat -c %Y "$LATEST")) / 3600 ))

if [ "$BACKUP_AGE" -gt "$MAX_AGE_HOURS" ]; then
    echo "WARNING: Last backup is $BACKUP_AGE hours old"
    exit 1
fi

echo "OK: Last backup is $BACKUP_AGE hours old"
exit 0
```

---

## Checklist

### Pre-Deployment

- [ ] Backup script created and tested
- [ ] Backup storage configured
- [ ] Retention policy defined
- [ ] Encryption keys generated (if needed)
- [ ] Monitoring alerts configured

### Ongoing Operations

- [ ] Daily backups running successfully
- [ ] Monthly backup verification completed
- [ ] Quarterly DR drill conducted
- [ ] Annual DR drill with full failover

### Post-Incident

- [ ] Backup integrity verified
- [ ] Recovery procedure documented
- [ ] RTO/RPO metrics recorded
- [ ] DR plan updated

---

*Last updated: 2026-04-05*
