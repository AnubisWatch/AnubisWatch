# AnubisWatch Configuration Reference

> **Complete guide to `anubis.yaml` configuration options**
> **Version:** 1.0.0

---

## Table of Contents

- [Quick Start](#quick-start)
- [Server Configuration](#server-configuration)
- [Storage Configuration](#storage-configuration)
- [Cluster Configuration (Necropolis)](#cluster-configuration-necropolis)
- [Authentication](#authentication)
- [Outgoing Mail](#outgoing-mail)
- [Dashboard](#dashboard)
- [Souls (Monitored Targets)](#souls-monitored-targets)
- [Alert Channels](#alert-channels)
- [Alert Rules (Verdicts)](#alert-rules-verdicts)
- [Synthetic Monitoring (Journeys)](#synthetic-monitoring-journeys)
- [Logging](#logging)
- [Security](#security)
- [Environment Variable Expansion](#environment-variable-expansion)
- [Configuration Validation](#configuration-validation)
- [Example Configurations](#example-configurations)

---

## Quick Start

```bash
# Generate default configuration
anubis init

# Edit configuration
vim anubis.yaml

# Start with custom config
anubis serve --config /path/to/anubis.yaml
```

### Minimum Valid Configuration

```yaml
# anubis.yaml - Minimal configuration
server:
  port: 8080

storage:
  path: /var/lib/anubis/data
```

---

## Server Configuration

### Root `server` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `host` | string | `"0.0.0.0"` | No | Bind address for HTTP server |
| `port` | integer | `8080` from `anubis init` | No | HTTP server port; use `443` or another HTTPS port when TLS is enabled |
| `grpc_port` | integer | `0` | No | Management gRPC port. `0` disables gRPC; a positive value explicitly enables it |
| `tls` | object | - | No | TLS configuration |
| `allowed_origins` | string[] | `[]` | No | Origins permitted to open WebSocket/CORS connections. Empty = none allowed |
| `trusted_proxies` | string[] | `[]` | No | Proxy IPs whose `X-Forwarded-For` is trusted for the real client IP (REST + WebSocket rate limiting). Empty = trust none (use `RemoteAddr`) |
| `grpc_reflection` | boolean | `false` | No | Enable gRPC server reflection (leave off in production) |
| `metrics_auth` | boolean | `false` | No | Require a bearer token on `/metrics`. Default is open for Prometheus scraping; enable in multi-tenant or internet-exposed deployments, since `/metrics` exposes monitor names/latencies across all workspaces |
| `public_url` | string | `""` | With `mail` | Externally reachable base URL (e.g. `https://status.example.com`) used for links in emails: subscription confirm/unsubscribe and password reset. Absolute http(s), no query or fragment |

> **Note on `environment`:** setting the top-level `environment: "production"` rejects any config with TLS disabled. When HTTP TLS is terminated upstream (ingress/reverse proxy), use `environment: "production-proxied"` so the instance may serve plain HTTP behind the proxy.
>
> **Management gRPC security:** `grpc_port: 0` is the secure default. In `production` and `production-proxied`, enabling gRPC requires process TLS because the API carries bearer tokens, unless `server.host` is an explicit loopback IP (`127.0.0.1` or `::1`) for a local-only tunnel. An HTTP ingress does not automatically protect the separate gRPC listener. gRPC binds to the same configured `server.host`, and unreadable/invalid TLS certificate files abort startup rather than falling back to plaintext.

### `server.tls` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `enabled` | boolean | `true` | No | Enable HTTPS |
| `cert` | string | - | Conditional | Path to TLS certificate (PEM) |
| `key` | string | - | Conditional | Path to TLS private key (PEM) |

### Examples

#### Basic HTTP (No TLS)

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  tls:
    enabled: false
```

#### Bring Your Own Certificates

```yaml
server:
  host: "0.0.0.0"
  port: 8443
  tls:
    enabled: true
    cert: "/etc/ssl/certs/anubis.crt"
    key: "/etc/ssl/private/anubis.key"
    min_version: 3
```

#### `server.tls` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Terminate TLS on the HTTP and gRPC listeners |
| `cert` | string | - | PEM certificate path. Required when `enabled: true` |
| `key` | string | - | PEM private key path. Required when `enabled: true` |
| `min_version` | int | `0` | Minimum TLS version: `0` (unset — TLS 1.2), `3` (TLS 1.2), `4` (TLS 1.3) |

`min_version` applies to both the REST and gRPC listeners. `1` (TLS 1.0) and `2`
(TLS 1.1) are **rejected at config load** rather than honoured — both protocols
are deprecated, and accepting them would lower the server below its TLS 1.2
floor. Any other value is also rejected, so a typo fails fast instead of
silently falling back.

> **Removed:** `server.tls.prefer_server` is accepted for backward compatibility
> but does nothing. It mapped onto `tls.Config.PreferServerCipherSuites`, which
> Go has ignored since 1.17 — the runtime chooses cipher preference. Delete it
> from your config.

## Storage Configuration

### Root `storage` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `path` | string | - | **Yes** | Path to CobaltDB data directory |
| `encryption` | object | - | No | Disk encryption settings |
| `timeseries` | object | - | No | Time-series compaction rules |
| `retention` | object | - | No | Data retention policies |

### `storage.encryption` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `enabled` | boolean | `false` | No | Enable AES-256 encryption |
| `key` | string | - | Conditional | Encryption key (use env var). Used verbatim as key material (hex is not decoded); at least 16 bytes, 32+ recommended, e.g. `openssl rand -hex 32`. A wrong key or a missing key for an encrypted data directory stops startup (`encryption.keycheck`). |

### `storage.timeseries.compaction` Block

Every check result is folded into a 1-minute summary as it is saved. A
background job then rolls summaries up into coarser ones every 5 minutes. A
bucket is rolled up once its whole span is older than the threshold, and is
built only once.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `raw_to_minute` | duration | `48h` | Unused: 1-minute summaries are written as results arrive |
| `minute_to_five` | duration | `168h` | 1-minute → 5-minute summaries |
| `five_to_hour` | duration | `720h` | 5-minute → hourly summaries |
| `hour_to_day` | duration | `720h` | Hourly → daily summaries |

### `storage.timeseries.retention` Block

Data older than these periods is deleted once an hour, starting at server
start.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `raw` | duration | `2160h` | Keep raw check results (judgments) |
| `minute` | duration | `720h` | Keep 1-minute summaries |
| `five` | duration | `2160h` | Keep 5-minute summaries |
| `hour` | duration | `8760h` | Keep hourly summaries |
| `day` | duration | `unlimited` | Keep daily summaries |

Status pages (90 days of uptime bars), soul logs and dashboard queries read raw
check results, so a `raw` period shorter than 90 days leaves gaps in them.

Keep each summary level longer than its compaction threshold plus one bucket of
the next level (for example `hour` > `hour_to_day` + 24h). Otherwise its oldest
buckets are deleted before they are rolled up, and the server logs a warning at
startup.

### Example

```yaml
storage:
  path: "/var/lib/anubis/data"
  encryption:
    enabled: true
    key: "${ANUBIS_ENCRYPTION_KEY}"
  timeseries:
    compaction:
      raw_to_minute: 48h
      minute_to_five: 168h
      five_to_hour: 720h
      hour_to_day: 720h
    retention:
      raw: 2160h
      minute: 720h
      five: 2160h
      hour: 8760h
      day: unlimited
```

---

## Cluster Configuration (Necropolis)

### Root `necropolis` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `enabled` | boolean | `false` | No | Enable cluster mode |
| `node_name` | string | Auto-generated | No | Human-readable node name |
| `region` | string | `"default"` | No | Geographic region for routing |
| `bind_addr` | string | `"0.0.0.0:7946"` | No | Address to bind Raft/gossip |
| `advertise_addr` | string | Auto-detected | No | Address advertised to peers |
| `cluster_secret` | string | - | Conditional | Shared HMAC secret. It authenticates gossip and **every Raft RPC** (votes, pre-votes, AppendEntries, heartbeats, snapshots, forwarded writes, joins); see *Cluster traffic authentication* below. Must be identical on every node. Without it cluster traffic is unauthenticated and the node logs a warning at startup. Leave empty in single-node mode (`anubis serve --single`); the Helm chart enforces presence when `config.necropolis.enabled=true` |
| `discovery` | object | - | No | Node discovery configuration |
| `raft` | object | - | No | Raft consensus tuning |
| `distribution` | object | - | No | How souls are shared between nodes |

### `necropolis.distribution` Block

In cluster mode every soul is owned by exactly one live node, which is the
only node that probes it and evaluates its alert rules. Judgments and
incidents replicate through Raft, so every node shows every soul's history.
Ownership is recomputed when souls change or a node joins, leaves or stops
answering the leader (after five election timeouts), without a restart.

Journeys are shared the same way: each journey (configured or created through
the API) runs on exactly one live node, chosen by the same strategy over the
journey ID, and its runs replicate so every node lists them. A standalone node
runs its configured journeys as before.

When a soul moves to a new owner, alerting continues where the old owner left
off: the record of when each rule last notified about the soul (its cooldown)
is replicated, and the new owner seeds the `consecutive_failures` streak and
the `status_for` clock from the soul's replicated judgments, so a failover
does not re-send an alert inside the rule's cooldown. Per-channel rate-limit
counters remain per node.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `strategy` | string | `"hash"` | `hash` (rendezvous hashing: a membership change moves only the affected node's souls), `round-robin` (exactly even counts, but adding or removing a soul can move others), `region-aware` (prefer nodes in the soul's `region`, balance by count), `load-based` (same as round-robin today). Unrecognised values fall back to `hash` with a warning. A soul's `regions` restriction is always honoured. |

`redundancy` and `rebalance_interval` are parsed but not used.

Writes may be sent to any node: a follower forwards them to the leader and
answers once it has applied them itself. When no leader is reachable a write
is refused with `503 Service Unavailable` and a `Retry-After` header.

Nodes can be added and removed at runtime. Each change is a replicated
joint-consensus configuration change, one at a time.

**Joining by address.** Start the new node with one or more existing
members' Raft addresses and no peers:

```bash
anubis serve --join 10.0.1.10:7946          # or necropolis.raft.join: ["10.0.1.10:7946"]
```

A node with `raft.join`, no `raft.peers` and `bootstrap: false` never elects
itself; it owns no souls or journeys and asks the join addresses to add it
under its own `raft.node_id`. When it is added it learns every member's real
node ID and address from the answer and becomes an ordinary voter (the
membership is persisted, so later restarts do not need `--join`).

- With a `cluster_secret`, the join request is authenticated and the new node
  is admitted automatically.
- Without a secret, joining is not automatic (anyone who can reach the Raft
  port could otherwise add themselves). Add the waiting node from any member
  with `anubis summon <new-node-raft-addr>`: the server asks the node for its
  node ID and adds it under that ID. `--name <node-id>` is still accepted to
  name it explicitly.

`anubis banish <node-id>` removes a member. The REST equivalents are
`POST /api/v1/cluster/nodes` (`node_id` optional: omitted, it is learned from
the node at `address`) and `DELETE /api/v1/cluster/nodes/{id}`. Statically
configured `raft.peers` keep working as before.

### Cluster traffic authentication

With `cluster_secret` set, every frame on the Raft transport carries an
HMAC-SHA256 signature over the RPC method, the sender's node ID, a timestamp,
a per-process epoch and sequence number, and the payload; responses are signed
and bound to their request. A receiver rejects a frame that is unsigned,
signed with another secret, altered, timestamped more than 60 seconds from its
own clock, replayed, or sent by a node that is not a current member (a
removed node holding the secret can no longer vote or replicate). Replay
protection is a fixed-size sliding window per sender, so it adds no
per-message allocation to the heartbeat path. Keep node clocks within 60
seconds of each other (NTP). Every node must use the same secret: a node with
a different or missing secret cannot talk to the cluster. TLS
(`raft.tls`) can be layered on top for confidentiality.

### `necropolis.discovery` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `mode` | string | `"gossip"` | No | `mdns`, `gossip`, or `manual` |
| `seeds` | array | - | Conditional | Seed nodes for gossip/manual |

### `necropolis.raft` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `election_timeout` | duration | `1000ms` | Follower → candidate timeout |
| `heartbeat_timeout` | duration | `300ms` | Leader heartbeat interval |
| `join` | array | - | Raft addresses of existing members to ask for admission (see *Joining by address*); used only while the node has no peers and is not bootstrapping. `anubis serve --join <addr>` appends to it |

### Example: Single Node

```yaml
necropolis:
  enabled: false
```

### Example: 3-Node Cluster with Gossip

```yaml
necropolis:
  enabled: true
  node_name: "jackal-eu-01"
  region: "eu-west"
  bind_addr: "0.0.0.0:7946"
  advertise_addr: "203.0.113.10:7946"
  cluster_secret: "${ANUBIS_CLUSTER_SECRET}"
  discovery:
    mode: "gossip"
    seeds:
      - "jackal-us-01.example.com:7946"
      - "jackal-apac-01.example.com:7946"
  raft:
    election_timeout: 1000ms
    heartbeat_timeout: 300ms
```

### Example: Manual Discovery

```yaml
necropolis:
  enabled: true
  node_name: "jackal-us-01"
  region: "us-east"
  cluster_secret: "${ANUBIS_CLUSTER_SECRET}"
  discovery:
    mode: "manual"
    seeds:
      - "10.0.1.10:7946"
      - "10.0.1.11:7946"
```

---

## Outgoing Mail

The root `mail` block configures one SMTP relay used by the server itself:

- **Status page subscriptions.** Visitors subscribe by email from a public
  status page. Double opt-in: the confirmation link is single use and expires
  after 48 hours; nothing else is sent until it is confirmed. Confirmed
  subscribers are emailed when an alert incident for one of the page's souls
  opens, is acknowledged or resolves, and when an operator posts, updates or
  resolves an incident on the page. Every email carries an unsubscribe link and
  `List-Unsubscribe` / `List-Unsubscribe-Post` (RFC 8058 one-click) headers.
  The subscribe endpoint is rate limited (10 requests per 10 minutes per client
  IP, 3 confirmation emails per address per day) and answers identically
  whether an address is new or already subscribed. In a cluster only the
  leader sends incident notifications.
- **Password reset.** `POST /api/v1/auth/reset-password` emails a link to
  `<public_url>/reset-password?token=...` (valid one hour, at most 3 emails per
  address per hour), and the login page offers "Forgot password?".

Without `mail.host` both stay off: `POST /status/subscribe` answers `503
subscriptions unavailable`, pages show no form (`subscriptions_available:
false` in the page JSON), and password recovery is `anubis admin
reset-password`. Alert email channels keep their own per-channel SMTP settings.

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `host` | string | `""` | Yes (to enable) | SMTP relay host. Dialed through the SSRF policy like probes and alert channels: a private or loopback relay needs `security.allow_private_targets` or `security.allowed_networks` |
| `port` | integer | `587` (`465` for `implicit`) | No | SMTP port |
| `username` | string | `""` | No | AUTH PLAIN user. Credentials are only sent over TLS or to a localhost relay |
| `password` | string | `""` | No | AUTH PLAIN password. Use `${ANUBIS_MAIL_PASSWORD}`; masked by `anubis config show` |
| `from` | string | - | Yes | Sender, e.g. `AnubisWatch <status@example.com>` |
| `tls` | string | `starttls` | No | `starttls` (required), `implicit` (TLS from connect, port 465) or `none` |
| `timeout` | duration | `30s` | No | Per-delivery timeout (1s to 5m). Failed deliveries are retried after 30s, 2m and 10m |

`server.public_url` is required when `mail.host` is set. The slugs `subscribe`
and `subscriptions` are reserved for the subscription endpoints
(`/status/subscribe`, `/status/subscriptions/confirm`,
`/status/subscriptions/unsubscribe`).

```yaml
server:
  public_url: "https://status.example.com"
mail:
  host: "smtp.example.com"
  port: 587
  username: "anubis@example.com"
  password: "${ANUBIS_MAIL_PASSWORD}"
  from: "AnubisWatch <status@example.com>"
  tls: "starttls"
```

---

## Authentication

### Root `auth` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `type` | string | `"local"` | No | `local`, `oidc`, or `ldap` |
| `local` | object | - | Conditional | Local auth settings |
| `oidc` | object | - | Conditional | OIDC provider settings |
| `ldap` | object | - | Conditional | LDAP settings |

### `auth.local` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `admin_email` | string | - | Conditional | Admin user email |
| `admin_password` | string | - | Conditional | Admin password (use env var) |

The configured `admin_password` seeds the account on first start. A password
changed later (dashboard **Settings → Change Password** or the API) is stored
hashed in `<storage.path>/sessions.json` and takes precedence over the config
file after a restart. To recover a lost password, stop the server and run:

```bash
anubis admin reset-password            # prompts; or --password-stdin
```

This revokes all sessions. With [outgoing mail](#outgoing-mail) configured,
`POST /api/v1/auth/reset-password` emails a reset link to any enabled local
account.

#### Members and invitations

Local auth is multi-user. The configured admin is the owner of the `default`
workspace (and cannot be disabled or deleted); further accounts are added from
**Settings → Members** or the [members API](API.md#members-and-invitations):

- **Invite** by email and role. With `mail.*` and `server.public_url` set, the
  single-use `/accept-invite` link is emailed; without mail the API returns it
  once for you to share. Links expire after 7 days; only a SHA-256 of the
  token is stored.
- **Create** an account with an initial password; the user must change it at
  first sign-in.
- Roles are per workspace: `owner`, `admin`, `editor`, `api`, `viewer`. Nobody
  grants or manages a role above their own, and a workspace always keeps an
  enabled owner.

Accounts live in `<storage.path>/sessions.json` next to sessions (in a cluster,
in the replicated authentication state, which is one Raft entry of at most
192 KiB; hence at most 150 accounts, 25 pending invitations, 10 sessions per
account and 200 in total (the oldest is signed out), and 20 reset tokens, one
per account).
Account emails are ASCII-only and compared with ASCII case folding only.
OIDC and LDAP users are added to the same directory at their first sign-in as
viewers of `default`; roles changed in the members list are applied on their
next request. `tenants.default_quotas.max_team_members` caps members plus
pending invitations per workspace when `tenants.enabled` is set.

### `auth.oidc` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `issuer` | string | - | **Yes** | OIDC issuer URL |
| `client_id` | string | - | **Yes** | OAuth client ID |
| `client_secret` | string | - | **Yes** | OAuth client secret |
| `redirect_uri` | string | Auto-generated | No | Callback URL |
| `scopes` | array | `["openid", "email", "profile"]` | No | OAuth scopes |
| `allow_unverified_email` | boolean | `false` | No | Accept identities without `email_verified: true`. Accounts are matched by email, so leave this off unless the provider never sends the claim and verifies addresses itself |

### `auth.ldap` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `host` | string | - | **Yes** | LDAP server hostname |
| `port` | integer | `389` | No | LDAP port (389 or 636 for LDAPS) |
| `use_tls` | boolean | `false` | No | Use LDAPS |
| `base_dn` | string | - | **Yes** | Base DN for searches |
| `bind_dn` | string | - | Conditional | Service account DN |
| `bind_password` | string | - | Conditional | Service account password |
| `user_filter` | string | `(uid=%s)` | No | LDAP filter for user lookup |
| `group_dn` | string | - | No | Required group DN |

### Example: Local Auth

```yaml
auth:
  type: "local"
  local:
    admin_email: "admin@example.com"
    admin_password: "${ANUBIS_ADMIN_PASSWORD}"
```

### Example: OIDC (Google Workspace)

```yaml
auth:
  type: "oidc"
  oidc:
    issuer: "https://accounts.google.com"
    client_id: "${GOOGLE_OAUTH_CLIENT_ID}"
    client_secret: "${GOOGLE_OAUTH_CLIENT_SECRET}"
    redirect_uri: "https://anubis.example.com/oauth/callback"
    scopes:
      - "openid"
      - "email"
      - "profile"
```

### Example: LDAP (Active Directory)

```yaml
auth:
  type: "ldap"
  ldap:
    host: "dc01.example.com"
    port: 636
    use_tls: true
    base_dn: "DC=example,DC=com"
    bind_dn: "CN=anubis,CN=Users,DC=example,DC=com"
    bind_password: "${LDAP_BIND_PASSWORD}"
    user_filter: "(sAMAccountName=%s)"
    group_dn: "CN=AnubisAdmins,CN=Users,DC=example,DC=com"
```

---

## Dashboard

### Root `dashboard` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `enabled` | boolean | `true` | No | Enable embedded dashboard |
| `branding` | object | - | No | Custom branding options |

### `dashboard.branding` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `title` | string | `"AnubisWatch"` | No | Dashboard, login page and browser title (at most 64 characters) |
| `logo` | string | - | No | Logo image: an `https://` URL or a same-origin path starting with `/` |
| `theme` | string | `"auto"` | No | `dark` or `light` sets the theme for viewers who have not picked one; `auto` keeps the dashboard's default |

Branding is public — the login page shows it — and is served by
`GET /api/v1/branding`. Changes apply after a restart.

### Example

```yaml
dashboard:
  enabled: true
  branding:
    title: "My Company Monitoring"
    logo: "https://cdn.example.com/logo.svg"
    theme: "dark"
```

---

## Souls (Monitored Targets)

### Root `souls` Array

Each soul represents a monitored target.

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `name` | string | - | **Yes** | Human-readable name |
| `type` | string | - | **Yes** | Check type (see below) |
| `target` | string | - | **Yes** (not for `heartbeat`) | Target URL/host |
| `weight` | duration | `60s` | No | Check interval (`heartbeat`: expected ping period) |
| `timeout` | duration | `10s` | No | Check timeout |
| `enabled` | boolean | `true` | No | Enable/disable check |
| `tags` | array | - | No | Tags for filtering/alerting |
| `regions` | array | - | No | Regions to run checks from |
| `retries` | integer | `0` | No | Confirmation retries (0–5) before a failure is recorded |
| `retry_interval` | duration | `5s` | No | Pause between retries (≥ 100ms, shorter than `weight`) |
| `flap_detection` | object | inherit | No | Per-soul override of `probe.flap_detection` |
| `<type>` | object | - | Conditional | Protocol-specific config |

### Confirmation Retries and Flap Detection

**Retries.** With `retries: N`, a failed check (error or `dead`) is re-run up
to N more times, `retry_interval` apart, before the soul is recorded `dead`.
Exactly one judgment is stored per scheduled check — the final outcome — with
the number of attempts in `details.attempts`, so history and uptime count one
check, not N. The concurrency slot (`probe.max_concurrent_checks`) is released
while a retry waits; a retry that would start after the next scheduled check
is skipped; cancelling the assignment (soul removed, ownership moved) stops
the retries without recording anything. `retries: 0` (default) is the old
behaviour. Heartbeat souls ignore retries, and manual "check now" runs a
single attempt.

**Flap detection.** A soul whose status changes at least `threshold` times
within `window` is *flapping*. Its judgments keep being recorded with
`details.flapping: true`, and soul payloads (REST, WebSocket `soul_update`,
MCP) carry `"flapping": true` beside the unchanged `status`. While flapping,
alert rules do not fire or auto-resolve for the soul; instead each enabled
rule that covers it sends one `flapping_started` notice (warning). Flapping
ends once fewer than half the threshold (rounded up) changes remain in the
window, or as soon as the status has held for `threshold` consecutive checks
(so a soul that goes hard down while flagged is not muted for a whole
window). Each rule then sends one `flapping_stopped` notice (info) and is
evaluated against the status the soul had **before** flapping started: a soul
that was up and stabilizes down fires the normal down alert and opens an
incident; one that was down and stabilizes up resolves (auto-resolve) and
fires `recovery` rules. The notices use the rule's channels and its cooldown
as dedup window (separate from status alerts) and do not open incidents. Flap
state is evaluated by the node that owns the soul; a new runner (interval
change, ownership move, restart) restores it from the soul's latest stored
judgment (`details.flapping`, `details.flap_baseline`), so the flag and the
single "stopped" notice survive the handover. The lookup covers the flap
`window`; a soul whose last judgment is older than that (an outage longer than
the window) starts unflagged and gets no "stopped" notice.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `false` | Turn flap detection on |
| `threshold` | integer | `5` | Status changes that start flapping (2–50; 0 = inherit) |
| `window` | duration | `10m` | Sliding window (10s–24h; unset = inherit) |

Set the default for every soul under `probe`, and override it per soul
(a soul block replaces `enabled`; zero `threshold`/`window` inherit):

```yaml
probe:
  flap_detection:
    enabled: true
    threshold: 5
    window: 10m

souls:
  - name: Flaky API
    type: http
    target: https://api.example.com/health
    weight: 30s
    retries: 2
    retry_interval: 5s
    flap_detection:
      enabled: true
      threshold: 4
      window: 15m
```

`anubis watch <target> --retries 2 --retry-interval 5s` sets the retry fields
from the CLI.

### Supported Types

| Type | Protocol | Config Block |
|------|----------|--------------|
| `http` | HTTP/HTTPS | `http` |
| `tcp` | TCP | `tcp` |
| `udp` | UDP | `udp` |
| `dns` | DNS | `dns` |
| `smtp` | SMTP | `smtp` |
| `imap` | IMAP | `imap` |
| `icmp` | ICMP Ping | `icmp` |
| `grpc` | gRPC | `grpc` |
| `websocket` | WebSocket | `websocket` |
| `tls` | TLS Cert | `tls` |
| `heartbeat` | Push / cron (inbound pings) | `heartbeat` |

### `http` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `method` | string | `GET` | HTTP method |
| `headers` | object | - | Custom headers |
| `body` | string | - | Request body |
| `valid_status` | array | `[200-299]` | Accepted status codes |
| `body_contains` | string | - | Required body substring |
| `body_regex` | string | - | Body regex pattern |
| `json_path` | object | - | JSON path assertions |
| `json_schema` | string | - | JSON Schema (draft 2020-12) |
| `feather` | duration | - | Max response time budget |
| `follow_redirects` | boolean | `true` | Follow redirects |
| `max_redirects` | integer | `5` | Max redirect count |

#### HTTP Example

```yaml
souls:
  - name: "Production API"
    type: http
    target: "https://api.example.com/health"
    weight: 30s
    timeout: 10s
    tags: ["production", "api", "critical"]
    http:
      method: GET
      headers:
        Authorization: "Bearer ${API_TOKEN}"
      valid_status: [200, 201]
      body_contains: '"status":"ok"'
      json_path:
        "$.status": "ok"
        "$.services.db": "connected"
      feather: 500ms
```

### `tcp` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `banner_match` | string | - | Expected banner regex |
| `send` | string | - | Data to send (hex or text) |
| `expect` | string | - | Expected response |

#### TCP Example

```yaml
souls:
  - name: "PostgreSQL Primary"
    type: tcp
    target: "db-primary.example.com:5432"
    weight: 15s
    timeout: 5s
    tags: ["database", "critical"]
    tcp:
      banner_match: "PostgreSQL"
```

### `dns` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `record_type` | string | `A` | DNS record type |
| `expected` | array | - | Expected values |
| `nameservers` | array | System DNS | Nameservers to query |
| `propagation_check` | boolean | `false` | Check propagation |
| `propagation_threshold` | integer | `100` | Required agreement % |
| `dnssec_validate` | boolean | `false` | Validate DNSSEC |

#### DNS Example

```yaml
souls:
  - name: "Primary DNS"
    type: dns
    target: "example.com"
    weight: 60s
    timeout: 10s
    tags: ["dns", "critical"]
    dns:
      record_type: A
      expected: ["93.184.216.34"]
      nameservers:
        - "8.8.8.8:53"
        - "1.1.1.1:53"
      propagation_check: true
      propagation_threshold: 100
```

### `tls` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `expiry_warn_days` | integer | `30` | Days until warning |
| `expiry_critical_days` | integer | `7` | Days until critical |
| `min_protocol` | string | `TLS1.2` | Minimum TLS version |
| `expected_issuer` | string | - | Expected CA issuer |
| `check_ocsp` | boolean | `false` | Check OCSP stapling |
| `min_key_bits` | integer | `2048` | Minimum key size |

#### TLS Example

```yaml
souls:
  - name: "API Certificate"
    type: tls
    target: "api.example.com:443"
    weight: 3600s
    timeout: 10s
    tags: ["security", "certificate"]
    tls:
      expiry_warn_days: 30
      expiry_critical_days: 7
      min_protocol: "TLS1.2"
      expected_issuer: "Let's Encrypt"
      check_ocsp: true
      min_key_bits: 2048
```

### `grpc` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `service` | string | - | gRPC service name |
| `tls` | boolean | `false` | Use TLS |
| `metadata` | object | - | Request metadata |
| `feather` | duration | - | Response time budget |

#### gRPC Example

```yaml
souls:
  - name: "Payment Service gRPC"
    type: grpc
    target: "grpc.example.com:9090"
    weight: 30s
    timeout: 5s
    tags: ["production", "grpc", "payments"]
    grpc:
      service: "payment.PaymentService"
      tls: true
      metadata:
        x-api-key: "${GRPC_API_KEY}"
      feather: 200ms
```

### `websocket` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `headers` | object | - | Upgrade headers |
| `subprotocols` | array | - | WebSocket subprotocols |
| `send` | string | - | Message to send |
| `expect_contains` | string | - | Expected response substring |
| `ping_check` | boolean | `false` | Validate ping/pong |

#### WebSocket Example

```yaml
souls:
  - name: "Realtime Feed"
    type: websocket
    target: "wss://ws.example.com/feed"
    weight: 60s
    timeout: 10s
    tags: ["websocket", "realtime"]
    websocket:
      headers:
        Authorization: "Bearer ${WS_TOKEN}"
      subprotocols: ["graphql-ws"]
      send: '{"type":"connection_init"}'
      expect_contains: "connection_ack"
      ping_check: true
```

### `icmp` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `count` | integer | `4` | Number of pings |
| `interval` | duration | `200ms` | Between pings |
| `max_loss_percent` | integer | `0` | Max packet loss % |
| `feather` | duration | - | Latency budget |
| `privileged` | boolean | `true` | Use raw sockets |

#### ICMP Example

```yaml
souls:
  - name: "Gateway Router"
    type: icmp
    target: "10.0.0.1"
    weight: 15s
    timeout: 5s
    tags: ["network", "infrastructure"]
    icmp:
      count: 5
      interval: 200ms
      max_loss_percent: 20
      feather: 100ms
      privileged: true
```

### `heartbeat` Block

A heartbeat soul is a push monitor for cron jobs, batch workers and backups:
nothing is probed, the job calls AnubisWatch. It needs no `target`; `weight`
is how often the job is expected to ping (at most 31 days).

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `grace` | duration | half of `weight` | How late a ping may be before the soul is judged dead (1s to 7d) |
| `token` | string | generated | Secret ping token, 32-128 characters of `A-Z a-z 0-9 - _`. Souls created through the API, dashboard, MCP, gRPC or `anubis watch --type heartbeat` get a generated 256-bit token (ignored if supplied); a soul in the config file must set one, e.g. `openssl rand -hex 32` |

Ping endpoints (no session or API key; the token is the credential, so keep
the URL secret and rotate it from the soul's page if it leaks):

| Request | Effect |
|---------|--------|
| `GET\|POST /api/v1/heartbeat/<token>` | Success: an `alive` judgment (duration = time since the last `/start`) |
| `GET\|POST /api/v1/heartbeat/<token>/start` | Run started; no judgment, used to measure the run time |
| `GET\|POST /api/v1/heartbeat/<token>/fail` | Failure: a `dead` judgment; the POST body (any content type) or `?msg=` is the message, first 1KB kept |

Each answers a plain-text `OK`, `404` for an unknown token, `429` past 30
pings per token per minute (on top of the per-IP API limit), `503` while a
cluster has no leader. When no ping arrives within `weight + grace` the soul
is judged `dead`, and again every `weight` until a ping arrives, so uptime and
alert rules work exactly as for pulled checks; maintenance windows suppress
its alerts and show it embalmed like any soul. "Test now" does not apply
(`POST /souls/{id}/check` answers 409). The ping URL is shown on the soul's
page in the dashboard and by `GET /api/v1/souls/{id}/heartbeat` (editors and
above); soul list/get responses return the token as `[REDACTED]`, while
backups and `anubis souls export` keep it, like other soul credentials.

In a cluster a ping may hit any node: it is stored in the soul's replicated
ping record (`{workspace}/heartbeats/{soul}`) through Raft, and only the
soul's owner turns it into judgments and evaluates the deadline, so each ping
or missed deadline yields one judgment cluster-wide.

#### Heartbeat Example

```yaml
souls:
  - name: "Nightly backup"
    type: heartbeat
    weight: 24h
    heartbeat:
      grace: 1h
      token: "${BACKUP_HEARTBEAT_TOKEN}"
```

```bash
# crontab: ping on success only (curl -f: no ping when the job fails)
0 3 * * * /usr/local/bin/backup.sh && curl -fsS -m 10 --retry 3 https://anubis.example.com/api/v1/heartbeat/$TOKEN

# measure run time and report failures with the job's output
curl -fsS -m 10 https://anubis.example.com/api/v1/heartbeat/$TOKEN/start
if /usr/local/bin/backup.sh > /tmp/backup.log 2>&1; then
  curl -fsS -m 10 --retry 3 https://anubis.example.com/api/v1/heartbeat/$TOKEN
else
  curl -fsS -m 10 --retry 3 --data-binary @/tmp/backup.log https://anubis.example.com/api/v1/heartbeat/$TOKEN/fail
fi
```

---

## Alert Channels

### Root `channels` Array

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `name` | string | - | **Yes** | Channel name |
| `type` | string | - | **Yes** | Channel type |
| `<type>` | object | - | **Yes** | Type-specific config |

### Supported Channel Types

| Type | Description | Config Block |
|------|-------------|--------------|
| `slack` | Slack webhook | `slack` |
| `discord` | Discord webhook | `discord` |
| `telegram` | Telegram bot | `telegram` |
| `email` | SMTP email | `email` |
| `pagerduty` | PagerDuty | `pagerduty` |
| `opsgenie` | Atlassian Opsgenie | `opsgenie` |
| `sms` | Twilio SMS | `sms` |
| `ntfy` | Ntfy.sh push | `ntfy` |
| `webhook` | Generic webhook | `webhook` |

### `slack` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `webhook_url` | string | - | Slack incoming webhook |
| `channel` | string | - | Override channel |
| `username` | string | `AnubisWatch` | Bot username |
| `icon_emoji` | string | `:anubis:` | Bot emoji icon |
| `mention_on_critical` | array | - | Users/roles to mention |

#### Slack Example

```yaml
channels:
  - name: "ops-slack"
    type: slack
    slack:
      webhook_url: "${SLACK_WEBHOOK_URL}"
      channel: "#ops-alerts"
      username: "AnubisWatch"
      icon_emoji: ":anubis:"
      mention_on_critical: ["@oncall-team"]
```

### `discord` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `webhook_url` | string | - | Discord webhook URL |
| `username` | string | `AnubisWatch` | Bot username |
| `avatar_url` | string | - | Bot avatar URL |

#### Discord Example

```yaml
channels:
  - name: "dev-discord"
    type: discord
    discord:
      webhook_url: "${DISCORD_WEBHOOK_URL}"
      username: "AnubisWatch"
```

### `email` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `smtp_host` | string | - | SMTP server |
| `smtp_port` | integer | `587` | SMTP port |
| `starttls` | boolean | `true` | Use STARTTLS |
| `username` | string | - | SMTP username |
| `password` | string | - | SMTP password |
| `from` | string | - | From address |
| `to` | array | - | Recipients |
| `subject_template` | string | - | Go template for subject |

#### Email Example

```yaml
channels:
  - name: "ops-email"
    type: email
    email:
      smtp_host: "smtp.example.com"
      smtp_port: 587
      starttls: true
      username: "${SMTP_USER}"
      password: "${SMTP_PASS}"
      from: "anubis@example.com"
      to:
        - "ops@example.com"
        - "cto@example.com"
      subject_template: "[{{ .Severity }}] {{ .Soul.Name }} — {{ .Judgment.Status }}"
```

### `pagerduty` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `integration_key` | string | - | PagerDuty integration key |
| `severity_map` | object | - | Custom severity mapping |
| `auto_resolve` | boolean | `true` | Auto-resolve on recovery |

#### PagerDuty Example

```yaml
channels:
  - name: "pagerduty-oncall"
    type: pagerduty
    pagerduty:
      integration_key: "${PAGERDUTY_INTEGRATION_KEY}"
      severity_map:
        critical: "critical"
        warning: "warning"
        info: "info"
      auto_resolve: true
```

### `ntfy` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `server` | string | `https://ntfy.sh` | Ntfy server URL |
| `topic` | string | - | Topic name |
| `priority_map` | object | - | Priority mapping |

#### Ntfy Example

```yaml
channels:
  - name: "ntfy-push"
    type: ntfy
    ntfy:
      server: "https://ntfy.sh"
      topic: "anubis-alerts"
      priority_map:
        critical: "urgent"
        warning: "high"
        info: "default"
```

### `webhook` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | - | Webhook URL |
| `method` | string | `POST` | HTTP method |
| `headers` | object | - | Custom headers |
| `body_template` | string | - | Go template for body |
| `timeout` | duration | `10s` | Request timeout |

#### Generic Webhook Example

```yaml
channels:
  - name: "custom-webhook"
    type: webhook
    webhook:
      url: "https://hooks.example.com/alerts"
      method: POST
      headers:
        Authorization: "Bearer ${WEBHOOK_TOKEN}"
        Content-Type: "application/json"
      body_template: |
        {
          "severity": "{{ .Severity }}",
          "soul": "{{ .Soul.Name }}",
          "status": "{{ .Judgment.Status }}",
          "timestamp": "{{ .Judgment.Timestamp }}"
        }
```

---

### `alerting` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `proxy` | string | `""` | `http://` or `https://` proxy for HTTP alert channels (webhook, Slack, Discord, Teams, PagerDuty, OpsGenie, ntfy, Telegram, Twilio). Email is unaffected. |

Alert channels never read `HTTP_PROXY`/`HTTPS_PROXY`: an implicit proxy connects
to the target itself and would bypass the SSRF policy. With `alerting.proxy` set,
targets are still checked against the policy before each send, but the proxy
resolves names itself, so DNS-rebinding protection is the proxy's responsibility.

## Alert Rules (Verdicts)

### Root `verdicts` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `rules` | array | - | No | Alert rules list |
| `escalation` | array | - | No | Shared escalation policies that rules name |

### `verdicts.rules[]` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `name` | string | - | **Yes** | Rule name |
| `scope` | string | `"all"` | No | Target scope |
| `condition` | object | - | **Yes** | Trigger condition |
| `severity` | string | `"warning"` | No | Alert severity |
| `channels` | array | - | No | Channels to notify |
| `cooldown` | duration | `5m` | No | Min time between alerts |

### Scope Formats

| Format | Description |
|--------|-------------|
| `all` | All souls |
| `tag:<tag>` | Souls with specific tag |
| `type:<type>` | Souls of specific type |
| `soul:<name>` | Specific soul by name |
| `region:<region>` | Souls in specific region |

### Condition Types

#### `consecutive_failures`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | - | `consecutive_failures` |
| `threshold` | integer | - | Number of failures |

```yaml
condition:
  type: consecutive_failures
  threshold: 3
```

#### `threshold`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | - | `threshold` |
| `metric` | string | - | Metric name |
| `operator` | string | - | `>`, `<`, `>=`, `<=`, `==` |
| `value` | string | - | Threshold value |
| `window` | duration | - | Evaluation window |

```yaml
condition:
  type: threshold
  metric: latency_p95
  operator: ">"
  value: "500ms"
  window: 5m
```

#### `percentage`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | - | `percentage` |
| `metric` | string | - | Metric name |
| `operator` | string | - | Comparison operator |
| `threshold` | integer | - | Percentage threshold |
| `window` | duration | - | Evaluation window |

```yaml
condition:
  type: percentage
  metric: failure_rate
  operator: ">"
  threshold: 10
  window: 5m
```

### Available Metrics

| Metric | Description |
|--------|-------------|
| `latency_p50` | 50th percentile latency |
| `latency_p95` | 95th percentile latency |
| `latency_p99` | 99th percentile latency |
| `latency_max` | Maximum latency |
| `failure_rate` | Failure percentage |
| `uptime_percent` | Uptime percentage |
| `tls_days_until_expiry` | Days until cert expiry |
| `propagation_percent` | DNS propagation % |
| `packet_loss_percent` | ICMP packet loss % |

### Example Rules

```yaml
verdicts:
  rules:
    - name: "Service Down - Critical"
      scope: "all"
      condition:
        type: consecutive_failures
        threshold: 3
      severity: critical
      channels: ["ops-slack", "pagerduty-oncall"]
      cooldown: 5m

    - name: "High Latency"
      scope: "tag:production"
      condition:
        type: threshold
        metric: latency_p95
        operator: ">"
        value: "1s"
        window: 5m
      severity: warning
      channels: ["ops-slack"]
      cooldown: 15m

    - name: "Certificate Expiring Soon"
      scope: "tag:security"
      condition:
        type: threshold
        metric: tls_days_until_expiry
        operator: "<"
        value: 14
      severity: warning
      channels: ["ops-email"]
      cooldown: 24h
```

### Escalation Policies

A rule escalates when it has an `escalation` block: either its own `stages`,
or the `name` of a shared policy declared under `verdicts.escalation`. Stage
channels are channel names, like a rule's `channels`. Naming a policy that
does not exist fails config validation. The rule's own `channels` are
notified when the incident opens; each stage then fires once, `wait` after
the previous stage (the first one: after the incident opened), as long as
its `condition` holds: `not_acknowledged` (the default) stops at
acknowledgement, `not_resolved` continues until the incident resolves. An
omitted or zero `wait` means 15 minutes.

```yaml
verdicts:
  rules:
    - name: "Service Down"
      # ...conditions and channels...
      escalation:
        name: "production-escalation"
  escalation:
    - name: "production-escalation"
      stages:
        - wait: 5m
          channels: ["pagerduty-oncall"]
          condition: "not_acknowledged"
        - wait: 15m
          channels: ["ops-email", "sms-oncall"]
          condition: "not_resolved"
```

---

## Synthetic Monitoring (Journeys)

### Root `journeys` Array

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `name` | string | - | **Yes** | Journey name |
| `weight` | duration | `300s` | No | Run interval |
| `timeout` | duration | `30s` | No | Total timeout |
| `continue_on_failure` | boolean | `false` | No | Continue on step failure |
| `variables` | object | - | No | Journey variables |
| `steps` | array | - | **Yes** | Journey steps |

### `steps[]` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `name` | string | - | **Yes** | Step name |
| `type` | string | `http` | No | Step type |
| `target` | string | - | **Yes** | Target URL |
| `http` | object | - | No | HTTP config |
| `extract` | object | - | No | Variable extraction |

### `extract` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `<var_name>` | object | - | Extraction rule |

### Extraction Rule

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `from` | string | - | `body`, `header`, `cookie` |
| `json_path` | string | - | JSON path (for body) |
| `header_name` | string | - | Header name |
| `cookie_name` | string | - | Cookie name |
| `regex` | string | - | Regex pattern |

### Journey Example

```yaml
journeys:
  - name: "User Login Flow"
    weight: 300s
    timeout: 30s
    continue_on_failure: false
    variables:
      base_url: "https://app.example.com"
      test_email: "${TEST_EMAIL}"
      test_password: "${TEST_PASSWORD}"
    steps:
      - name: "Get CSRF Token"
        type: http
        target: "${base_url}/login"
        http:
          method: GET
          valid_status: [200]
        extract:
          csrf_token:
            from: body
            json_path: "$.csrf_token"

      - name: "Perform Login"
        type: http
        target: "${base_url}/api/auth/login"
        http:
          method: POST
          headers:
            Content-Type: "application/json"
            X-CSRF-Token: "${csrf_token}"
          body: |
            {"email": "${test_email}", "password": "${test_password}"}
          valid_status: [200]
        extract:
          auth_token:
            from: body
            json_path: "$.token"

      - name: "Fetch Dashboard"
        type: http
        target: "${base_url}/api/dashboard"
        http:
          method: GET
          headers:
            Authorization: "Bearer ${auth_token}"
          valid_status: [200]
          feather: 2s
```

---

## Logging

### Root `logging` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `level` | string | `"info"` | No | Log level |
| `format` | string | `"json"` | No | Log format |
| `output` | string | `"stdout"` | No | Log output |
| `file` | string | - | Conditional | Log file path |

### `level` Options

- `debug` - All logs
- `info` - Informational and above
- `warn` - Warnings and errors
- `error` - Errors only

### `format` Options

- `json` - Structured JSON
- `text` - Human-readable text

### `output` Options

- `stdout` - Standard output
- `file` - File (requires `file` field)

### Example

```yaml
logging:
  level: "info"
  format: "json"
  output: "file"
  file: "/var/log/anubis/anubis.log"
```

---

## Security

```yaml
security:
  # Probes and alert channels refuse private/loopback targets by default
  # (SSRF protection). Enable to monitor services on your LAN.
  allow_private_targets: false
  # Narrower alternative: only these private ranges are reachable.
  allowed_networks:
    - "10.20.0.0/16"
  # Honour per-soul insecure_skip_verify (MITM risk; dev/test only).
  allow_insecure_skip_verify: false
```

`allow_private_targets` covers 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16,
127.0.0.0/8, fc00::/7 and ::1. Link-local and cloud metadata addresses
(169.254.0.0/16, 100.100.100.200, …) stay blocked regardless.
`ANUBIS_SSRF_ALLOW_PRIVATE=1` has the same effect as `allow_private_targets: true`.

A soul whose target is blocked is recorded as `dead` with the reason in the
judgment message, so it shows up in the dashboard and triggers alert rules.

---

## Environment Variable Expansion

AnubisWatch supports environment variable expansion in configuration values:

### Syntax

| Syntax | Description |
|--------|-------------|
| `${VAR}` | Replace with `VAR` value (empty if unset) |
| `${VAR:-default}` | Use `VAR` or `default` if unset |
| `${VAR:+alternate}` | Use `alternate` if `VAR` is set, empty otherwise |

### Examples

```yaml
storage:
  encryption:
    key: "${ANUBIS_ENCRYPTION_KEY}"  # Required, fails if unset

necropolis:
  cluster_secret: "${ANUBIS_CLUSTER_SECRET}"  # Required when clustering is enabled

server:
  tls:
    cert: "${ANUBIS_CERT:-/etc/anubis/tls/cert.pem}"
```

---

## Configuration Validation

Validate your configuration file:

```bash
# Validate config syntax
anubis config validate --config anubis.yaml

# Show the resolved config path
anubis config path --config anubis.yaml
```

### Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| `missing required field 'path'` | Storage path not set | Add `storage.path` |
| `invalid duration format` | Bad duration syntax | Use `1h`, `30m`, `60s`, `500ms` |
| `unknown protocol 'htp'` | Typo in type | Use `http`, `tcp`, etc. |
| `invalid JSON path` | Malformed JSON path | Use `$.key.subkey` format |
| `TLS cert not found` | Missing certificate file | Check `server.tls.cert` path |

---

## Example Configurations

### Minimal Single-Node

```yaml
# anubis.yaml - Minimal
server:
  port: 8080

storage:
  path: /var/lib/anubis/data

souls:
  - name: "Google DNS"
    type: dns
    target: "google.com"
    weight: 60s
    dns:
      record_type: A
```

### Production-Ready Single Node

```yaml
server:
  host: "0.0.0.0"
  port: 8443
  tls:
    enabled: true
    cert: "/etc/ssl/certs/anubis.crt"
    key: "/etc/ssl/private/anubis.key"

storage:
  path: /var/lib/anubis/data
  encryption:
    enabled: true
    key: "${ANUBIS_ENCRYPTION_KEY}"

auth:
  type: "local"
  local:
    admin_email: "admin@example.com"
    admin_password: "${ANUBIS_ADMIN_PASSWORD}"

souls:
  - name: "Production API"
    type: http
    target: "https://api.example.com/health"
    weight: 30s
    tags: ["production", "api", "critical"]
    http:
      valid_status: [200]
      body_contains: '"status":"ok"'

channels:
  - name: "ops-slack"
    type: slack
    slack:
      webhook_url: "${SLACK_WEBHOOK_URL}"

verdicts:
  rules:
    - name: "Service Down"
      scope: "tag:production"
      condition:
        type: consecutive_failures
        threshold: 3
      severity: critical
      channels: ["ops-slack"]
      cooldown: 5m
```

### 3-Node Cluster

See [deployment/guide.md](deployment/guide.md) for cluster configuration examples.

---

## See Also

- [deployment/guide.md](deployment/guide.md) - Deployment guides
- [api/README.md](api/README.md) - REST API specification
- [deploy/README.md](../deploy/README.md) - Container image and deployment assets

---

**⚖️ The Judgment Never Sleeps**
