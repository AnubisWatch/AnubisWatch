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
- [Dashboard](#dashboard)
- [Souls (Monitored Targets)](#souls-monitored-targets)
- [Alert Channels](#alert-channels)
- [Alert Rules (Verdicts)](#alert-rules-verdicts)
- [Synthetic Monitoring (Journeys)](#synthetic-monitoring-journeys)
- [Logging](#logging)
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
  port: 8443

storage:
  path: /var/lib/anubis/data
```

---

## Server Configuration

### Root `server` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `host` | string | `"0.0.0.0"` | No | Bind address for HTTP server |
| `port` | integer | `8443` | No | HTTPS port |
| `tls` | object | - | No | TLS configuration |

### `server.tls` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `enabled` | boolean | `true` | No | Enable HTTPS |
| `cert` | string | - | Conditional | Path to TLS certificate (PEM) |
| `key` | string | - | Conditional | Path to TLS private key (PEM) |
| `auto_cert` | boolean | `false` | No | Auto-provision Let's Encrypt cert |
| `acme_email` | string | - | Conditional | Email for ACME registration |
| `acme_domains` | array | - | Conditional | Domains for certificate |

### Examples

#### Basic HTTP (No TLS)

```yaml
server:
  host: "0.0.0.0"
  port: 8443
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
```

#### Automatic Let's Encrypt

```yaml
server:
  host: "0.0.0.0"
  port: 443
  tls:
    enabled: true
    auto_cert: true
    acme_email: "admin@example.com"
    acme_domains:
      - "anubis.example.com"
      - "status.example.com"
```

---

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
| `key` | string | - | Conditional | 32-byte encryption key (use env var) |

### `storage.timeseries.compaction` Block

CobaltDB automatically compacts time-series data:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `raw_to_minute` | duration | `48h` | Raw samples → 1-minute aggregates |
| `minute_to_five` | duration | `7d` | 1-minute → 5-minute aggregates |
| `five_to_hour` | duration | `30d` | 5-minute → hourly aggregates |
| `hour_to_day` | duration | `365d` | Hourly → daily aggregates |

### `storage.retention` Block

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `raw` | duration | `48h` | Keep raw judgment samples |
| `minute` | duration | `30d` | Keep minute-level aggregates |
| `five` | duration | `90d` | Keep 5-minute aggregates |
| `hour` | duration | `365d` | Keep hourly aggregates |
| `day` | duration | `unlimited` | Keep daily aggregates |

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
      minute_to_five: 7d
      five_to_hour: 30d
      hour_to_day: 365d
    retention:
      raw: 48h
      minute: 30d
      five: 90d
      hour: 365d
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
| `cluster_secret` | string | - | Conditional | Shared secret for cluster auth |
| `discovery` | object | - | No | Node discovery configuration |
| `raft` | object | - | No | Raft consensus tuning |

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

### `auth.oidc` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `issuer` | string | - | **Yes** | OIDC issuer URL |
| `client_id` | string | - | **Yes** | OAuth client ID |
| `client_secret` | string | - | **Yes** | OAuth client secret |
| `redirect_uri` | string | Auto-generated | No | Callback URL |
| `scopes` | array | `["openid", "email", "profile"]` | No | OAuth scopes |

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
| `title` | string | `"AnubisWatch"` | No | Browser title |
| `logo` | string | - | No | Custom logo URL |
| `theme` | string | `"auto"` | No | `auto`, `dark`, or `light` |

### Example

```yaml
dashboard:
  enabled: true
  branding:
    title: "My Company Monitoring"
    logo: "/assets/logo.png"
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
| `target` | string | - | **Yes** | Target URL/host |
| `weight` | duration | `60s` | No | Check interval |
| `timeout` | duration | `10s` | No | Check timeout |
| `enabled` | boolean | `true` | No | Enable/disable check |
| `tags` | array | - | No | Tags for filtering/alerting |
| `regions` | array | - | No | Regions to run checks from |
| `<type>` | object | - | Conditional | Protocol-specific config |

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

## Alert Rules (Verdicts)

### Root `verdicts` Block

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `rules` | array | - | No | Alert rules list |
| `escalation` | array | - | No | Escalation policies |

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

```yaml
verdicts:
  escalation:
    - name: "production-escalation"
      stages:
        - wait: 0s
          channels: ["ops-slack", "ntfy-push"]
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
  cluster_secret: "${ANUBIS_CLUSTER_SECRET:-default-secret}"

server:
  tls:
    cert: "${ANUBIS_CERT:-/etc/anubis/tls/cert.pem}"
```

---

## Configuration Validation

Validate your configuration file:

```bash
# Validate config syntax
anubis validate --config anubis.yaml

# Dry-run with config (no actual checks)
anubis serve --config anubis.yaml --dry-run
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
  port: 8443

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
    auto_cert: true
    acme_email: "admin@example.com"
    acme_domains:
      - "anubis.example.com"

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

See [DEPLOYMENT.md](../DEPLOYMENT.md#kubernetes-cluster) for cluster configuration examples.

---

## See Also

- [DEPLOYMENT.md](../DEPLOYMENT.md) - Deployment guides
- [openapi.yaml](openapi.yaml) - REST API specification
- [GHCR.md](../GHCR.md) - Container image documentation

---

**⚖️ The Judgment Never Sleeps**
