# AnubisWatch API Reference

**Version:** 1.0  
**Base URL:** `http://localhost:8080/api/v1`

## Authentication

All API endpoints (except health checks) require authentication using a Bearer token:

```bash
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/souls
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

### Switch Workspace

```http
POST /api/v1/auth/workspace
Authorization: Bearer <token>
Content-Type: application/json

{
  "workspace": "tenant-a"
}
```

Switches the current session to a workspace the authenticated user can access.
The API returns `403 Forbidden` when the workspace exists but is not visible to
the user.

### Get Current User

```http
GET /api/v1/auth/me
Authorization: Bearer <token>
```

---

## API Keys

API keys are long-lived bearer credentials for scripts, CI and integrations.
A key looks like `anb_<id>_<secret>` (24 hex characters of id, 32 random
bytes of secret) and is sent exactly like a session token:

```bash
curl -H "Authorization: Bearer anb_..." http://localhost:8080/api/v1/souls
ANUBIS_API_TOKEN=anb_... anubis judge      # CLI client commands
```

The same header works for gRPC (`authorization` metadata), WebSocket `/ws`,
SSE `/api/v1/events` and MCP `/api/v1/mcp`. Open WebSocket/SSE streams close
within 30 s of the key being revoked or expiring.

- **Storage:** only the SHA-256 of the key is stored, under
  `{workspace}/apikeys/{id}`; the plaintext is returned once, at creation, and
  is never logged. In a cluster the record is written through Raft like every
  other workspace resource, so every node accepts and revokes the same keys.
  `last_used_at` is updated at most every 5 minutes per key.
- **Principal:** a key acts as `apikey:<id>` with its own role in the
  workspace it was created in. The audit log's `user_id` names the key on
  every request; `GET /auth/me` returns `api_key_id`.
- **Role:** defaults to the creator's role and may not rank above it
  (owner > admin > editor > api > viewer). The role is fixed at creation.
- **Expiry:** optional, at most 366 days. Expired and revoked keys answer 401.
- **Interactive-only endpoints:** a key can never list, create or revoke keys,
  change a password (`PUT /auth/change-password`) or switch workspace
  (`POST /auth/workspace`); those answer 403.
- **Private status pages:** send a key of the page's workspace in
  `X-API-Key: anb_...` (any role); a browser session cookie works too.
- **Permission:** managing keys needs `apikeys:*`, held by `admin` and `owner`.
- With `auth.enabled: false` nothing changes: requests stay anonymous and
  read-only, and the key endpoints answer 403.

### List API Keys

```http
GET /api/v1/api-keys
Authorization: Bearer <session token>
```

Returns the workspace's keys (active, expired and revoked) with `id`, `name`,
`prefix`, `role`, `status`, `created_at`, `expires_at`, `last_used_at`,
`revoked_at`. Neither the secret nor its hash is ever returned.

### Create API Key

```http
POST /api/v1/api-keys
Authorization: Bearer <session token>
Content-Type: application/json

{ "name": "deploy pipeline", "role": "editor", "expires_in_days": 90 }
```

`expires_at` (RFC 3339) may be sent instead of `expires_in_days`; omit both for
a key that does not expire. The `201` response carries the plaintext in `key`
— store it now, it cannot be shown again.

### Revoke API Key

```http
DELETE /api/v1/api-keys/{id}
Authorization: Bearer <session token>
```

Revocation is immediate and permanent. From the CLI (after `anubis login`):

```bash
anubis apikey create --name deploy --role editor --expires-in-days 90
anubis apikey list
anubis apikey revoke <id>
```

## Members and invitations

Local accounts, their per-workspace roles and invitations. Every endpoint
except accepting an invitation needs an interactive session with `members:*`
(`admin`, `owner`); API keys are refused with 403. Nobody can grant or manage a
role above their own (`owner > admin > editor > api > viewer`), and a workspace
must keep an enabled owner (409 otherwise). Disabling, removing, deleting or
downgrading an account ends its sessions and live WebSocket/SSE streams
immediately; its API keys stop authenticating while it lacks the key's role.

### List Members

```http
GET /api/v1/members
```

Returns `id`, `email`, `name`, `role`, `workspace`, `source` (`local`, `oidc`,
`ldap`), `disabled`, `must_change_password`, `configured_admin`, `created_at`,
`last_login_at` for each member of the current workspace.

### Create Member

```http
POST /api/v1/members
Content-Type: application/json

{ "email": "ana@example.com", "name": "Ana", "password": "Initial-Passw0rd", "role": "editor" }
```

Creates a local account that must change its password at first sign-in
(until then every other endpoint answers 403 `password_change_required`).
An email that already has an account is refused with 409: an existing account
joins a workspace only by accepting an invitation with its own password.

### Update Member

```http
PATCH /api/v1/members/{id}
Content-Type: application/json

{ "role": "viewer" }          // or { "disabled": true } / { "name": "Ana B." }
```

### Remove Member / Delete Account

```http
DELETE /api/v1/members/{id}   # remove from this workspace (an account left with none is deleted)
DELETE /api/v1/users/{id}     # delete the account everywhere
```

### Invitations

```http
POST /api/v1/invitations
Content-Type: application/json

{ "email": "new@example.com", "role": "viewer" }
```

`201` returns the invitation with `email_sent`. When the link was not emailed
(no mail configured, or the queue refused it) `invite_url` carries the
`/accept-invite?token=…` link this one time. Invitations expire after 7 days;
re-inviting an address replaces its pending invitation. Pending invitations
count against `max_team_members` (409 `quota_exceeded`).

```http
GET    /api/v1/invitations        # pending invitations, never the token
DELETE /api/v1/invitations/{id}   # revoke
```

### Accept Invitation (public)

```http
POST /api/v1/auth/accept-invite
Content-Type: application/json

{ "token": "<from the link>", "name": "New Person", "password": "Chosen-Passw0rd" }
```

Creates the account (`201`); the token cannot be used again. When the address
already has a local account, send its current password (name is ignored) and
the membership is added to it. `400` for an unknown, used or expired token, a
token whose inviter has since been disabled, removed or demoted below the
invited role, or a password that fails the policy (or does not match the
existing account).

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

### Soul Uptime

```http
GET /api/v1/souls/:id/uptime?range=90d&tz_offset_minutes=180
```

Uptime over the last 24 hours, 7 and 30 days, plus equal buckets over
`range`: `1h` (5-minute buckets), `24h` (hours), `7d`, `30d`, `90d` or `1y`
(calendar days in the caller's offset). Long ranges are counted from hourly
rollups, not raw judgments.

### Export Judgments

```http
GET /api/v1/souls/:id/judgments/export?from=2026-10-01&to=2026-10-27T00:00:00Z&format=csv&limit=100000
Authorization: Bearer <token>
```

Streams the soul's judgments in `[from, to)`, oldest first, as `csv` (default)
or `ndjson`, one hour of storage at a time, so memory stays flat however long
the period. Requires `judgments:read`; a soul of another workspace answers 404.

- `from` / `to`: RFC3339 or `YYYY-MM-DD` (UTC). Defaults: the last 30 days;
  `to` is capped at now. `from` is clipped to raw-judgment retention
  (`storage.timeseries.retention.raw`); `X-Export-Clipped: true` says so and
  `X-Export-From` / `X-Export-To` give the effective bounds.
- `limit`: 1–1,000,000 rows (default 100,000). The response ends with the
  HTTP trailers `X-Export-Rows` and `X-Export-Truncated`; when truncated,
  continue with `from` set to the last row's timestamp.
- Columns: `timestamp,id,soul_id,status,duration_ms,status_code,region,jackal_id,message`
  (NDJSON objects carry the same fields). Protocol details such as response
  bodies are not exported.
- CSV cells whose text starts with `=`, `+`, `-`, `@`, tab or carriage return
  are prefixed with `'` so spreadsheets do not evaluate them as formulas.
- `Content-Disposition: attachment; filename="anubis-judgments-<soul>-<from>-<to>.csv"`.
- At most 2 exports per user and 8 per server run at once (429 with
  `Retry-After` beyond that). Each 32 KiB write must reach the client within
  30 s and a whole export finish within 15 min, or the stream is cut off.

CLI: `anubis export judgments --soul ID --from 7d [--to T] [--format csv|ndjson] [--limit N] [-o FILE]`.

---

## Reports

### SLA Report

```http
GET /api/v1/reports/sla?from=2026-07-01&to=2026-10-01&target=99.9[&soul_id=ID]
Authorization: Bearer <token>
```

Per soul of the workspace (or only `soul_id`) and in total over `[from, to)`
(default: the last 30 days; at most 1,830 days). Requires `judgments:read`.

```json
{
  "from": "2026-07-01T00:00:00Z",
  "to": "2026-10-01T00:00:00Z",
  "generated_at": "2026-10-27T09:00:00Z",
  "target_percent": 99.9,
  "retention_horizon": null,
  "clipped": false,
  "summary": { "...": "same shape as a soul, soul_id omitted" },
  "souls": [{
    "soul_id": "api", "soul_name": "Payments API", "soul_type": "http",
    "period_seconds": 7948800, "excluded_seconds": 7200, "monitored_seconds": 7941600,
    "no_data_seconds": 0,
    "checks": 264960, "failed": 41, "availability_percent": 99.9845,
    "downtime_seconds": 1230,
    "incidents": 2, "mttr_seconds": 540,
    "latency": { "avg_ms": 82.1, "p50_ms": 74, "p95_ms": 160, "p99_ms": 240, "exact": false },
    "error_budget": {
      "target_percent": 99.9, "allowed_downtime_seconds": 7941.6,
      "consumed_downtime_seconds": 1230, "remaining_downtime_seconds": 6711.6,
      "remaining_percent": 84.51, "met": true
    },
    "sources": { "raw": 2, "hour": 2206, "day": 0 }
  }]
}
```

How the figures are computed:

- **Availability** = checks that found the soul `alive` / all checks
  (`degraded` and `dead` count as failed); `null` without checks.
- **Downtime** is estimated per bucket as *bucket length × failed / checks*.
  Time without any check is `no_data_seconds`, not downtime.
- **Data sources.** Whole UTC hours are read from the hourly rollups (counted
  live as results arrive) and, once hourly retention has purged them, whole
  UTC days from the daily rollups, so 90-day and 1-year reports never scan
  raw judgments. Raw judgments fill only hours cut by the period's edges or a
  maintenance window, and time before rollups started; once raw retention has
  purged those, that time is no data. `sources` counts the buckets of each
  kind. A whole hour gives the same figures from either source. The day
  hourly retention cuts through is its daily rollup minus the surviving
  hourly rollups.
- A report has a 60 s time budget (503 beyond it) and stops when the client
  disconnects.
- **Maintenance** (excluded time): enabled maintenance windows that apply to
  the soul (by soul ID, tag or workspace-wide), with daily/weekly/monthly
  recurrences expanded, are cut out of the period. Checks inside them and
  results with status `embalmed` are ignored; `excluded_seconds` reports the
  time removed and `monitored_seconds` what is left. Windows are taken as
  currently defined, so a deleted or disabled window no longer excludes its
  past time.
- **Incidents / MTTR**: alert incidents of the soul that *started* in the
  period; MTTR is the mean `resolved_at − started_at` over those resolved.
- **Latency** percentiles come from a fixed log-linear histogram (1,000
  buckets, γ = 1.02), so they are within 1% of the true value and cost the
  same however many checks the period has. `exact: false` means rollups were
  used, where each bucket's average stands in for its checks.
- **Error budget** (with `target`, 0 < target ≤ 100): allowed downtime =
  `monitored_seconds × (100 − target) / 100`; `remaining_percent` goes
  negative once overspent (null for a 100% target); `met` = downtime within
  the allowance.
- The workspace `summary` sums over souls, so its durations are
  soul-seconds.
- `from` earlier than the oldest data retention keeps is moved forward
  (`clipped: true`, `retention_horizon`); with unlimited daily retention
  (the default) there is no horizon.

### SLA Report as CSV

```http
GET /api/v1/reports/sla.csv?from=2026-07-01&to=2026-10-01&target=99.9
```

Same parameters; one row per soul, then a total row (`soul_id` empty,
`soul_name` `(total)`), as an attachment named `anubis-sla-<from>-<to>.csv`.
Text cells are protected against formula injection as above.

CLI: `anubis report sla [--from 90d] [--to T] [--target 99.9] [--soul ID] [--format table|json|csv]`.

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

### Incidents and Scheduled Maintenance

Operator-posted incidents and maintenance announcements have their own
endpoints, so two editors never overwrite each other (each write is a locked
read-modify-write of the page). Writes need `settings:write`; a page in another
workspace answers `404`. Title (200 chars) and message (5000 chars) are plain
text: stored verbatim, escaped everywhere they are rendered, and `http(s)` URLs
become links on the public page. Raw HTML is never interpreted.

```http
GET    /api/v1/status-pages/:id/incidents
POST   /api/v1/status-pages/:id/incidents                       # {title, message, impact, status, affected_souls}
PATCH  /api/v1/status-pages/:id/incidents/:incidentId           # {title?, impact?, affected_souls?}
DELETE /api/v1/status-pages/:id/incidents/:incidentId
POST   /api/v1/status-pages/:id/incidents/:incidentId/updates   # {message, status}
POST   /api/v1/status-pages/:id/incidents/:incidentId/resolve   # {message?}
GET    /api/v1/status-pages/:id/announcements
POST   /api/v1/status-pages/:id/announcements                   # {title, message, starts_at, ends_at, affected_souls, maintenance_window_id?}
PATCH  /api/v1/status-pages/:id/announcements/:announcementId
DELETE /api/v1/status-pages/:id/announcements/:announcementId
```

- `incidents`/`announcements` are read-only on the page create and PUT:
  they are ignored there and the stored values are kept.
- One page record is capped at 256 KiB serialized; a write that would grow it
  further gets `413`. Each write first drops resolved incidents and finished
  maintenance older than the 90-day public history.
- `impact`: `none` | `minor` (default) | `major` | `critical`. `status`:
  `investigating` (default) | `identified` | `monitoring` | `resolved`.
  `affected_souls` must be souls the page shows.
- The status changes only through a posted update, which appends to the
  timeline; `resolved` sets `resolved_at`, any other status reopens.
- Confirmed email subscribers get exactly one email when an incident is
  created (unless already resolved), per posted update, and per new
  announcement. Editing metadata or deleting sends nothing.
- `maintenance_window_id` links a one-off maintenance window in the same
  workspace: it supplies the schedule (and, if omitted, the title, message and
  the page's souls it covers); moving the window moves the announcement.
  Recurring windows cannot be linked.
- The public page shows an active-incident banner coloured by the worst
  impact, components at least as bad as the incidents naming them (minor:
  degraded, major/critical: down) and under maintenance during an
  in-progress announcement, upcoming/in-progress maintenance, and 90 days of
  incident history with timelines. Its JSON adds `incident_history`,
  `maintenance` (with `phase`: `upcoming`, `in_progress`, `completed`) and
  per-soul `incident_impact`; the RSS feed (`/status/:slug/feed.xml`) adds an
  item per incident (re-issued on every update) and per announcement.

---

## Workspaces

Multi-tenant workspace management.

Workspace reads and mutations are scoped to the authenticated user's current
workspace. Requests for another workspace return `403 Forbidden`.
Workspace listing returns the current workspace and workspaces owned by the
authenticated user.
Creating a workspace initializes the authenticated user's current workspace
record. The server assigns `id`, `owner_id`, quotas, feature flags, status, and
timestamps; client-supplied protected fields are ignored. If the current
workspace already exists, the API returns `409 Conflict`.

### List Workspaces

```http
GET /api/v1/workspaces
Authorization: Bearer <token>
```

Returns only workspaces visible to the authenticated user.

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

`slug` is optional when `name` can be converted into a valid slug. Reserved or
invalid slugs return `400 Bad Request`.

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
