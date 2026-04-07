# AnubisWatch Cluster Mode (Necropolis) Setup Guide

## Overview

Necropolis is AnubisWatch's distributed cluster mode using Raft consensus. When enabled, multiple Jackal nodes form a cluster for high availability.

## Current Status: Standalone Mode

The server is currently running in **standalone mode**:
- `necropolis.enabled: false`
- Single node: `jackal-01`
- All data stored locally in CobaltDB
- 1,110+ judgments recorded

## Enabling Cluster Mode

### Step 1: Update Configuration

```json
{
  "necropolis": {
    "enabled": true,
    "node_name": "jackal-01",
    "bind_addr": "0.0.0.0:7946",
    "advertise_addr": "192.168.1.101:7946",
    "peers": ["192.168.1.102:7946", "192.168.1.103:7946"]
  }
}
```

### Step 2: Start Multiple Nodes

Node 1 (Bootstrap):
```bash
./anubis -config node1.json -node-id jackal-01
```

Node 2:
```bash
./anubis -config node2.json -node-id jackal-02
```

Node 3:
```bash
./anubis -config node3.json -node-id jackal-03
```

### Step 3: Verify Cluster

```bash
# Check cluster status
curl http://localhost:9191/api/v1/necropolis/status \
  -H "Authorization: Bearer $TOKEN"
```

Expected response:
```json
{
  "node_id": "jackal-01",
  "state": "leader",
  "term": 5,
  "peers": ["jackal-02", "jackal-03"],
  "last_log_index": 1110,
  "commit_index": 1110
}
```

## Current System Proof

### Standalone Mode Evidence

```
Server Process:     Running (PID 101528)
Port:               9191 (LISTENING)
Storage:            CobaltDB (1.5MB WAL)
Judgments:          1,110 recorded
Souls:              2 monitored (HTTP Bin, Google DNS)
Auth:               JWT tokens working
Dashboard:          Ancient Egypt theme active
Health:             {"status":"healthy"}
```

### API Endpoints Working

| Endpoint | Status | Proof |
|----------|--------|-------|
| POST /auth/login | ✅ | Token received |
| GET /users/me | ✅ | User profile |
| GET /souls | ✅ | 2 souls listed |
| GET /souls/{id}/judgments | ✅ | 4+ judgments |
| GET /alerts/channels | ✅ | Empty list |
| GET /alerts/rules | ✅ | Empty list |
| GET /journeys | ✅ | Empty list |
| GET /mcp/tools | ✅ | 2 tools listed |
| GET /health | ✅ | {"status":"healthy"} |
| GET / | ✅ | Egyptian dashboard HTML |

### Judgment Sample

```json
{
  "id": "06epefx4z7nj9x2jkj68tzdnj0",
  "soul_id": "06epefsdttgx0cx1hmthwfqzvm",
  "jackal_id": "jackal-01",
  "timestamp": "2026-04-07T09:21:57.4971624Z",
  "duration": 495832800,
  "status": "alive",
  "status_code": 200,
  "message": "HTTP 200 in 496ms",
  "tls_info": {
    "protocol": "TLS1.2",
    "cipher_suite": "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
    "issuer": "Amazon RSA 2048 M03",
    "subject": "httpbin.org",
    "days_until_expiry": 132
  }
}
```

## Switching to Cluster Mode

To run the system in cluster mode now:

1. Stop current server: `kill 101528`
2. Update `anubis.json` with `necropolis.enabled: true`
3. Restart server
4. Cluster endpoints will become active

Would you like me to switch to cluster mode now, or is standalone mode sufficient for your current needs?
