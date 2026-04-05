# AnubisWatch Deployment Troubleshooting Guide

## Table of Contents

1. [Quick Start Issues](#quick-start-issues)
2. [Configuration Problems](#configuration-problems)
3. [Network & Connectivity](#network--connectivity)
4. [Storage Issues](#storage-issues)
5. [Cluster Problems](#cluster-problems)
6. [Alert Delivery Failures](#alert-delivery-failures)
7. [Performance Issues](#performance-issues)
8. [Common Error Messages](#common-error-messages)

---

## Quick Start Issues

### Server Won't Start

**Symptom:** `anubis serve` exits immediately

**Check:**
```bash
# Check if port is already in use
# Windows
netstat -ano | findstr :8443

# Linux/macOS
lsof -i :8443
```

**Solution:**
```bash
# Use a different port
export ANUBIS_PORT=8444
./anubis serve

# Or kill the process using the port
kill <PID>
```

### Config File Not Found

**Symptom:** `failed to load config: open anubis.yaml: no such file or directory`

**Solution:**
```bash
# Generate default config
./anubis init

# Or specify config path
export ANUBIS_CONFIG=/path/to/config.yaml
./anubis serve
```

### Permission Denied

**Symptom:** `permission denied` when writing to data directory

**Solution:**
```bash
# Create data directory with correct permissions
mkdir -p /var/lib/anubis
chown $(whoami):$(whoami) /var/lib/anubis

# Or use a different data directory
export ANUBIS_DATA_DIR=$HOME/.anubis
./anubis serve
```

---

## Configuration Problems

### Invalid YAML Syntax

**Symptom:** `yaml: line X: did not find expected key`

**Solution:**
```bash
# Validate YAML syntax
python3 -c "import yaml; yaml.safe_load(open('anubis.yaml'))"

# Check for common issues:
# - Tabs vs spaces (use spaces)
# - Missing colons
# - Unclosed quotes
```

### Missing Required Fields

**Symptom:** `validation failed: field 'target' is required`

**Solution:**
```yaml
# Ensure all required fields are present
souls:
  - name: "My Monitor"
    type: "http"        # Required
    target: "https://example.com"  # Required
    weight: "60s"       # Required
    timeout: "10s"      # Required
```

### Environment Variables Not Applied

**Symptom:** Config shows empty strings for expected values

**Solution:**
```bash
# Check environment variables are set
echo $ANUBIS_ENCRYPTION_KEY

# Restart after setting
export ANUBIS_ENCRYPTION_KEY="your-secret-key-here"
./anubis serve
```

---

## Network & Connectivity

### Health Checks Failing

**Symptom:** All souls showing as `dead` or `degraded`

**Diagnosis:**
```bash
# Check network connectivity from the server
curl -v https://your-target.com

# Check DNS resolution
nslookup your-target.com

# Check firewall rules
# Windows
netsh advfirewall show allprofiles

# Linux
iptables -L -n
```

**Solutions:**
1. **DNS Issues**: Use IP address instead of hostname
2. **Firewall**: Allow outbound connections on required ports
3. **Proxy**: Configure proxy in soul config:
   ```yaml
   http:
     proxy: "http://proxy.example.com:8080"
   ```

### TLS Certificate Errors

**Symptom:** `x509: certificate signed by unknown authority`

**Solutions:**
```yaml
# Option 1: Skip verification (not recommended for production)
tls:
  insecure_skip_verify: true

# Option 2: Add custom CA
tls:
  ca_cert_file: "/path/to/ca.pem"

# Option 3: Use system certificates
tls:
  use_system_certs: true
```

### Timeout Issues

**Symptom:** `context deadline exceeded`

**Solutions:**
```yaml
# Increase timeout for slow endpoints
souls:
  - name: "Slow API"
    type: "http"
    target: "https://slow-api.com"
    timeout: "30s"  # Default is 10s
```

---

## Storage Issues

### Database Locked

**Symptom:** `database is locked` or `file in use`

**Diagnosis:**
```bash
# Check for running instances
ps aux | grep anubis

# Check file locks (Linux)
lsof /path/to/data/
```

**Solution:**
```bash
# Stop all anubis processes
pkill anubis

# Remove lock files
rm -f /path/to/data/*.lock

# Restart
./anubis serve
```

### Disk Space Full

**Symptom:** `no space left on device`

**Diagnosis:**
```bash
# Check disk usage
df -h

# Check AnubisWatch data size
du -sh /path/to/data/*
```

**Solution:**
```yaml
# Configure retention policy
storage:
  retention_days: 30  # Keep 30 days of data
  
# Enable log compaction
storage:
  compaction_interval: "1h"
  compaction_threshold: 10000
```

### Data Corruption

**Symptom:** `failed to decode JSON` or unexpected errors

**Solution:**
```bash
# Backup current data
cp -r /path/to/data /path/to/data.backup

# List souls to identify corrupted files
./anubis judge

# Remove corrupted soul files
rm /path/to/data/default/souls/<corrupted-id>.json

# Restart
./anubis serve
```

---

## Cluster Problems

### Node Can't Join Cluster

**Symptom:** `failed to join cluster: connection refused`

**Diagnosis:**
```bash
# Check cluster secret matches
echo $ANUBIS_CLUSTER_SECRET

# Verify network connectivity between nodes
telnet <leader-ip> 7946

# Check Raft bind address
cat anubis.yaml | grep -A5 raft:
```

**Solution:**
```yaml
necropolis:
  cluster_secret: "same-secret-on-all-nodes"
  raft:
    bind_addr: "0.0.0.0:7946"
    advertise_addr: "<public-ip>:7946"
    bootstrap: false  # Only true on initial leader
```

### Leader Election Loops

**Symptom:** Constant leader changes, logs show repeated elections

**Diagnosis:**
```bash
# Check network stability between nodes
ping -c 10 <peer-ip>

# Check Raft logs
grep "election" /path/to/data/logs/*.log
```

**Solution:**
```yaml
# Increase election timeout for unstable networks
necropolis:
  raft:
    election_timeout: "3s"     # Default: 1s
    heartbeat_timeout: "1s"    # Default: 500ms
```

### Split Brain

**Symptom:** Two nodes both think they're leader

**Solution:**
```bash
# Identify healthy nodes
./anubis necropolis

# On minority partition nodes, stop and rejoin
./anubis banish <old-leader-id>

# Restart cluster with majority quorum
./anubis serve
```

---

## Alert Delivery Failures

### Slack Not Receiving Alerts

**Symptom:** Alerts logged but not appearing in Slack

**Diagnosis:**
```bash
# Test webhook URL directly
curl -X POST -H 'Content-type: application/json' \
  --data '{"text":"Test"}' \
  https://hooks.slack.com/services/YOUR/WEBHOOK/URL
```

**Solution:**
1. Verify webhook URL is correct
2. Check Slack app permissions
3. Ensure channel exists and bot is invited

### Email Not Sending

**Symptom:** `failed to send email: connection timeout`

**Solution:**
```yaml
channels:
  - name: "Email Alerts"
    type: "email"
    email:
      smtp_host: "smtp.example.com"
      smtp_port: 587
      starttls: true  # Required for port 587
      username: "alerts@example.com"
      password: "${SMTP_PASSWORD}"
```

### PagerDuty Integration Issues

**Symptom:** Incidents not appearing in PagerDuty

**Diagnosis:**
```bash
# Test integration key
curl -X POST https://events.pagerduty.com/v2/enqueue \
  -H "Content-Type: application/json" \
  -d '{"routing_key":"YOUR_KEY","event_action":"trigger","payload":{...}}'
```

---

## Performance Issues

### High Memory Usage

**Symptom:** Process using >1GB RAM

**Diagnosis:**
```bash
# Check memory profile
go tool pprof http://localhost:8444/debug/pprof/heap

# Check number of souls
./anubis judge | wc -l
```

**Solution:**
```yaml
# Reduce judgment history
storage:
  max_judgments_per_soul: 1000

# Enable downsampling
storage:
  downsampling:
    enabled: true
    raw_retention: "24h"
    hourly_retention: "7d"
    daily_retention: "90d"
```

### Slow API Responses

**Symptom:** API calls taking >1 second

**Diagnosis:**
```bash
# Check API latency
time curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8443/api/v1/souls
```

**Solution:**
```yaml
# Enable pagination
# Use ?limit=50 instead of fetching all

# Add indexes for frequently queried fields
storage:
  indexes:
    - field: "status"
    - field: "workspace_id"
```

### Check Latency Spikes

**Symptom:** Judgments taking longer than expected

**Solution:**
```yaml
# Limit concurrent checks
probe:
  max_concurrent_checks: 50  # Default: 100

# Increase check interval for non-critical souls
souls:
  - name: "Non-critical"
    weight: "300s"  # Check every 5 minutes
```

---

## Common Error Messages

### `context deadline exceeded`

**Cause:** Check or operation took longer than timeout

**Fix:**
```yaml
# Increase timeout
timeout: "30s"
```

### `connection refused`

**Cause:** Target service not reachable

**Fix:** Check network, firewall, target service status

### `certificate verify failed`

**Cause:** TLS certificate issue

**Fix:** See [TLS Certificate Errors](#tls-certificate-errors)

### `rate limit exceeded`

**Cause:** Too many API requests

**Fix:** Wait and retry, or increase rate limit:
```yaml
api:
  rate_limit: 200  # requests per minute
```

### `no such file or directory`

**Cause:** Data directory or config file missing

**Fix:** Create directory or specify correct path

---

## Debug Mode

Enable debug logging for detailed troubleshooting:

```bash
export ANUBIS_LOG_LEVEL=debug
./anubis serve
```

Debug logs show:
- Every health check execution
- Alert routing decisions
- Raft consensus messages
- Storage operations

---

## Getting Help

If issues persist:

1. **Check logs**: `/path/to/data/logs/`
2. **GitHub Issues**: https://github.com/AnubisWatch/anubiswatch/issues
3. **Documentation**: `docs/` directory
4. **Health endpoint**: `curl http://localhost:8443/health`

---

*Last updated: 2026-04-05*
