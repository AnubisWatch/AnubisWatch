# AnubisWatch API Reference

**Version:** 1.0  
**Base URL:** `http://localhost:8443/api/v1`

## Authentication

All API endpoints (except health checks) require authentication using a Bearer token:

```bash
curl -H "Authorization: Bearer <token>" https://localhost:8443/api/v1/souls
```

### Login

```http
POST /api/v1/auth/login
Content-Type: application/json

{
  "email": "admin@example.com",
  "password": "your-password"
}
```

**Response:**
```json
{
  "user": {
    "id": "user_123",
    "email": "admin@example.com",
    "workspace": "default"
  },
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

### Logout

```http
POST /api/v1/auth/logout
Authorization: Bearer <token>
```

### Get Current User

```http
GET /api/v1/auth/me
Authorization: Bearer <token>
```

---

## Souls (Monitors)

Souls are the core monitoring units in AnubisWatch. Each soul represents a target to be monitored.

### List Souls

```http
GET /api/v1/souls?offset=0&limit=20
Authorization: Bearer <token>
```

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| offset | int | 0 | Pagination offset |
| limit | int | 20 | Max results (max: 100) |
| workspace | string | user's workspace | Filter by workspace |

**Response:**
```json
{
  "data": [
    {
      "id": "soul_abc123",
      "workspace_id": "default",
      "name": "Example API",
      "type": "http",
      "target": "https://api.example.com/health",
      "weight": 60000000000,
      "timeout": 10000000000,
      "enabled": true,
      "tags": ["production", "api"],
      "regions": ["us-east-1"],
      "created_at": "2026-04-05T10:00:00Z"
    }
  ],
  "pagination": {
    "total": 1,
    "offset": 0,
    "limit": 20,
    "has_more": false
  }
}
```

### Create Soul

```http
POST /api/v1/souls
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "My API Monitor",
  "type": "http",
  "target": "https://api.example.com/health",
  "weight": "60s",
  "timeout": "10s",
  "enabled": true,
  "tags": ["production"],
  "http": {
    "method": "GET",
    "valid_status": [200, 201],
    "expect_body_contains": "\"status\":\"ok\""
  }
}
```

**Soul Types:**
- `http` - HTTP/HTTPS endpoints
- `tcp` - TCP port connectivity
- `udp` - UDP port connectivity
- `dns` - DNS record resolution
- `tls` - TLS certificate validation
- `smtp` - SMTP server connectivity
- `grpc` - gRPC service health
- `websocket` - WebSocket connection
- `icmp` - ICMP ping (requires privileges)
- `mysql` - MySQL database connectivity
- `redis` - Redis database connectivity

### Get Soul

```http
GET /api/v1/souls/:id
Authorization: Bearer <token>
```

### Update Soul

```http
PUT /api/v1/souls/:id
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "Updated Name",
  "enabled": false
}
```

### Delete Soul

```http
DELETE /api/v1/souls/:id
Authorization: Bearer <token>
```

### Force Check

```http
POST /api/v1/souls/:id/check
Authorization: Bearer <token>
```

**Response:**
```json
{
  "id": "judgment_xyz789",
  "soul_id": "soul_abc123",
  "status": "alive",
  "statusCode": 200,
  "duration": 45000000,
  "timestamp": "2026-04-05T10:30:00Z",
  "message": "OK"
}
```

### List Judgments

```http
GET /api/v1/souls/:id/judgments?start=2026-04-01&end=2026-04-05&limit=100
Authorization: Bearer <token>
```

---

## Alert Channels

Alert channels define where notifications are sent when souls change status.

### List Channels

```http
GET /api/v1/channels
Authorization: Bearer <token>
```

### Create Channel

```http
POST /api/v1/channels
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "Ops Slack",
  "type": "slack",
  "enabled": true,
  "config": {
    "webhook_url": "https://hooks.slack.com/services/xxx/yyy/zzz"
  },
  "rate_limit": {
    "enabled": true,
    "max_alerts": 10,
    "window": "1h",
    "grouping_key": "soul_id"
  }
}
```

**Channel Types:**
- `slack` - Slack webhooks
- `discord` - Discord webhooks
- `telegram` - Telegram bot
- `email` - SMTP email
- `pagerduty` - PagerDuty integration
- `opsgenie` - OpsGenie integration
- `sms` - SMS via Twilio/Vonage
- `ntfy` - Ntfy.sh notifications
- `webhook` - Generic HTTP webhook

### Update Channel

```http
PUT /api/v1/channels/:id
Authorization: Bearer <token>
Content-Type: application/json
```

### Delete Channel

```http
DELETE /api/v1/channels/:id
Authorization: Bearer <token>
```

### Test Channel

```http
POST /api/v1/channels/:id/test
Authorization: Bearer <token>
```

---

## Alert Rules (Verdicts)

Rules define when alerts should be triggered.

### List Rules

```http
GET /api/v1/rules
Authorization: Bearer <token>
```

### Create Rule

```http
POST /api/v1/rules
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "Service Down Alert",
  "enabled": true,
  "scope": {
    "type": "tag",
    "tags": ["production"]
  },
  "conditions": [
    {
      "type": "consecutive_failures",
      "threshold": 3
    }
  ],
  "channels": ["channel_abc123"],
  "cooldown": "5m",
  "auto_resolve": true,
  "escalation": {
    "stages": [
      {
        "wait": "15m",
        "channels": ["slack-ops"]
      },
      {
        "wait": "30m",
        "channels": ["pagerduty"]
      }
    ]
  }
}
```

**Condition Types:**
- `consecutive_failures` - N consecutive failed checks
- `status_change` - Transition from one status to another
- `status_for` - Status persists for duration
- `failure_rate` - Failure rate exceeds threshold
- `degraded` - Service is degraded
- `recovery` - Service recovers from failure

### Update Rule

```http
PUT /api/v1/rules/:id
Authorization: Bearer <token>
```

### Delete Rule

```http
DELETE /api/v1/rules/:id
Authorization: Bearer <token>
```

---

## Incidents

Incidents represent active or resolved alert situations.

### List Incidents

```http
GET /api/v1/incidents?status=open
Authorization: Bearer <token>
```

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| status | string | Filter: open, acked, resolved |
| severity | string | Filter: critical, warning, info |

### Acknowledge Incident

```http
POST /api/v1/incidents/:id/acknowledge
Authorization: Bearer <token>
```

### Resolve Incident

```http
POST /api/v1/incidents/:id/resolve
Authorization: Bearer <token>
```

---

## Status Pages

Public status pages for sharing service health externally.

### List Status Pages

```http
GET /api/v1/status-pages
Authorization: Bearer <token>
```

### Create Status Page

```http
POST /api/v1/status-pages
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "Public Status",
  "slug": "status",
  "description": "Real-time service status",
  "custom_domain": "status.example.com",
  "visibility": "public",
  "theme": {
    "primary_color": "#7c3aed",
    "background_color": "#0f0f12",
    "text_color": "#ffffff",
    "font_family": "system-ui"
  },
  "souls": ["soul_abc123", "soul_def456"],
  "uptime_days": 90
}
```

**Visibility Options:**
- `public` - Anyone can view
- `protected` - Password required
- `private` - Authentication required

### Get Status Page

```http
GET /api/v1/status-pages/:id
Authorization: Bearer <token>
```

### Update Status Page

```http
PUT /api/v1/status-pages/:id
Authorization: Bearer <token>
```

### Delete Status Page

```http
DELETE /api/v1/status-pages/:id
Authorization: Bearer <token>
```

---

## Workspaces

Multi-tenant workspace management.

### List Workspaces

```http
GET /api/v1/workspaces
Authorization: Bearer <token>
```

### Create Workspace

```http
POST /api/v1/workspaces
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "Acme Corp",
  "slug": "acme",
  "description": "Acme Corporation workspace"
}
```

### Get Workspace

```http
GET /api/v1/workspaces/:id
Authorization: Bearer <token>
```

### Update Workspace

```http
PUT /api/v1/workspaces/:id
Authorization: Bearer <token>
```

### Delete Workspace

```http
DELETE /api/v1/workspaces/:id
Authorization: Bearer <token>
```

---

## Stats & Metrics

### Get Stats

```http
GET /api/v1/stats?start=2026-04-01&end=2026-04-05
Authorization: Bearer <token>
```

### Get Stats Overview

```http
GET /api/v1/stats/overview
Authorization: Bearer <token>
```

---

## Cluster (Raft)

For clustered deployments.

### Cluster Status

```http
GET /api/v1/cluster/status
Authorization: Bearer <token>
```

**Response:**
```json
{
  "is_clustered": true,
  "node_id": "jackal-1",
  "state": "leader",
  "leader": "jackal-1",
  "term": 5,
  "peer_count": 2
}
```

### Cluster Peers

```http
GET /api/v1/cluster/peers
Authorization: Bearer <token>
```

---

## Health Checks

### Health Endpoint

```http
GET /health
```

**Response:**
```json
{
  "status": "healthy"
}
```

### Ready Endpoint

```http
GET /ready
```

---

## Error Responses

All errors follow this format:

```json
{
  "error": "Error message describing what went wrong"
}
```

**HTTP Status Codes:**
| Code | Meaning |
|------|---------|
| 200 | Success |
| 201 | Created |
| 204 | No Content |
| 400 | Bad Request - Invalid input |
| 401 | Unauthorized - Missing or invalid token |
| 403 | Forbidden - Insufficient permissions |
| 404 | Not Found |
| 409 | Conflict - Resource already exists |
| 429 | Too Many Requests - Rate limited |
| 500 | Internal Server Error |

---

## Rate Limiting

API requests are rate limited to 100 requests per minute per IP address.

**Rate Limit Headers:**
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1712312400
```

When rate limited:
```http
HTTP/1.1 429 Too Many Requests
Retry-After: 60

{"error": "Rate limit exceeded. Try again in 60 seconds."}
```

---

## MCP (Model Context Protocol)

For AI agent integration via Claude Code.

```http
POST /api/v1/mcp
Authorization: Bearer <token>
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/list"
}
```

**Available MCP Tools:**
- `list_souls` - List all monitors
- `get_soul` - Get monitor details
- `force_check` - Trigger immediate check
- `get_judgments` - Get check history
- `list_incidents` - List active incidents
- `get_stats` - Get system statistics
- `acknowledge_incident` - Acknowledge an incident
- `create_soul` - Create new monitor

See `docs/MCP.md` for detailed MCP protocol documentation.

---

## Webhooks

### Subscription Webhooks

Status pages can send webhook notifications:

```json
{
  "event": "soul.status_change",
  "page_id": "page_xyz",
  "soul_id": "soul_abc",
  "old_status": "alive",
  "new_status": "dead",
  "timestamp": "2026-04-05T10:30:00Z"
}
```

---

*Generated for AnubisWatch v1.0 - The Judgment Never Sleeps*
