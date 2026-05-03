# AnubisWatch User Manual

## Table of Contents

1. [Getting Started](#getting-started)
2. [Dashboard Overview](#dashboard-overview)
3. [Managing Souls](#managing-souls)
4. [Alerting](#alerting)
5. [Synthetic Monitoring](#synthetic-monitoring)
6. [Cluster Management](#cluster-management)
7. [Status Pages](#status-pages)
8. [Troubleshooting](#troubleshooting)

## Getting Started

### First Login

1. Open your browser and navigate to `http://localhost:8080`
2. Login with default credentials:
   - Email: `admin@anubis.watch`
   - Password: `SecurePass123!`
3. **Important:** Change the default password immediately!

### Dashboard Overview

The AnubisWatch dashboard ("Hall of Ma'at") provides a complete overview of your monitoring infrastructure:

```
┌─────────────────────────────────────────────────────────────┐
│  AnubisWatch                    [Search] [Alerts] [Profile] │
├──────────┬──────────────────────────────────────────────────┤
│          │                                                  │
│ Dashboard│  ┌─────────────┐  ┌─────────────┐  ┌──────────┐  │
│ Souls    │  │   Total     │  │   Healthy   │  │  Failed  │  │
│ Judgments│  │    42       │  │     40      │  │    2     │  │
│ Alerts   │  └─────────────┘  └─────────────┘  └──────────┘  │
│ Journeys │                                                  │
│ Cluster  │  [Status Chart]                                  │
│ Settings │                                                  │
│          │  Recent Judgments                                │
│          │  ┌──────────────────────────────────────────┐   │
│          │  │ [Green] api.example.com      23ms  ✓     │   │
│          │  │ [Red]   db.internal          timeout ✗   │   │
│          │  │ [Green] cdn.site.com         45ms  ✓     │   │
│          │  └──────────────────────────────────────────┘   │
└──────────┴──────────────────────────────────────────────────┘
```

## Managing Souls

### What is a Soul?

A Soul represents a monitored target - it could be a website, API endpoint, database, or any service you want to monitor.

### Creating a Soul

1. Navigate to **Souls** in the sidebar
2. Click **Add Soul** button
3. Configure the soul:
   - **Name**: Human-readable name
   - **Type**: HTTP, TCP, DNS, ICMP, etc.
   - **Target**: URL, IP address, or hostname
   - **Interval**: How often to check (default: 60s)
   - **Timeout**: Maximum wait time (default: 10s)

#### HTTP Soul Example

```yaml
Name: API Health Check
Type: HTTP
Target: https://api.example.com/health
Interval: 30 seconds
Timeout: 5 seconds
HTTP Config:
  Method: GET
  Headers:
    Authorization: Bearer ${API_TOKEN}
  Valid Status: [200, 204]
  Follow Redirects: true
  Verify TLS: true
```

#### TCP Soul Example

```yaml
Name: Database Connection
Type: TCP
Target: db.internal:5432
Interval: 60 seconds
Timeout: 3 seconds
```

### Soul Status

| Status | Icon | Meaning |
|--------|------|---------|
| Alive | 🟢 | Service is healthy |
| Dead | 🔴 | Service is failing |
| Degraded | 🟡 | Service has issues |
| Unknown | ⚪ | Not yet checked |
| Embalmed | ⚫ | Maintenance mode |

### Bulk Operations

```bash
# Import souls from JSON
anubis import souls.json

# Export souls to JSON
anubis export souls > backup.json

# Quick-add from CLI
anubis watch https://example.com --name "Example Site"
```

## Alerting

### Alert Channels

AnubisWatch supports multiple notification channels:

#### Email

```yaml
Name: Engineering Team
Type: Email
Config:
  Recipients:
    - oncall@example.com
    - alerts@example.com
  SMTP: Default
```

#### Slack

```yaml
Name: #alerts Slack
Type: Slack
Config:
  Webhook URL: https://hooks.slack.com/services/...
  Channel: #alerts
  Username: AnubisWatch
```

#### Webhook

```yaml
Name: PagerDuty
Type: Webhook
Config:
  URL: https://events.pagerduty.com/v2/enqueue
  Method: POST
  Headers:
    Authorization: Token token=${PD_TOKEN}
```

### Alert Rules

Create rules to trigger alerts:

```yaml
Name: Critical Service Down
Condition: soul.status == 'dead' && soul.type == 'http'
Channels:
  - Engineering Team
  - #alerts Slack
Cooldown: 5 minutes
Auto-resolve: true

Name: High Latency
Condition: judgment.latency > 2000 && soul.name contains 'api'
Channels:
  - #alerts Slack
Cooldown: 15 minutes
```

### Condition Syntax

```javascript
// Basic conditions
soul.status == 'dead'
soul.status != 'alive'

// Numeric comparisons
judgment.latency > 1000
judgment.latency >= 500

// String matching
soul.name contains 'api'
soul.target startswith 'https'

// Logical operators
soul.status == 'dead' && soul.type == 'http'
soul.status == 'dead' || judgment.latency > 2000

// Time-based
judgment.timestamp > now() - 5m
```

### Incident Management

When an alert triggers, an incident is created:

1. **Acknowledge**: "I'm working on this"
2. **Resolve**: "Issue is fixed"
3. **Snooze**: "Remind me in X minutes"

## Synthetic Monitoring

### What are Journeys?

Journeys are multi-step synthetic tests that simulate user workflows.

### Creating a Journey

1. Navigate to **Journeys** in the sidebar
2. Click **Create Journey**
3. Define steps:

```yaml
Name: User Login Flow
Description: Test the complete login process

Steps:
  1. Load Login Page
     - URL: https://app.example.com/login
     - Assert: Status 200
     - Assert: Body contains "Sign In"

  2. Submit Credentials
     - URL: https://app.example.com/api/login
     - Method: POST
     - Body: {"email": "test@example.com", "password": "test123"}
     - Assert: Status 200
     - Assert: Response has "token"

  3. Access Dashboard
     - URL: https://app.example.com/dashboard
     - Headers:
         Authorization: Bearer ${step2.token}
     - Assert: Status 200
     - Assert: Body contains "Welcome"
```

### Step Types

| Type | Description |
|------|-------------|
| HTTP Request | Make HTTP call and validate response |
| Wait | Pause for specified duration |
| Condition | Conditional execution based on previous step |
| Extract | Extract data from response for later use |

### Assertions

```yaml
# Status code
Assert: Status 200
Assert: Status >= 200 && Status < 300

# Response body
Assert: Body contains "success"
Assert: Body matches "^Hello.*"

# JSON path
Assert: JSON $.status == "ok"
Assert: JSON $.data.items length > 0

# Headers
Assert: Header content-type contains "json"
Assert: Header x-request-id exists

# Response time
Assert: Latency < 500
```

## Cluster Management

### Viewing Cluster Status

Navigate to **Necropolis** in the sidebar to see:

- Current leader
- Node status
- Term number
- Peer count
- Network latency

### Adding a Node

```bash
# On new node
anubis serve --join <leader-ip>:7946 --node-id node-4

# Or via API (leader only)
curl -X POST http://leader:8080/api/v1/cluster/peers \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"id": "node-4", "address": "10.0.0.4:7946"}'
```

### Removing a Node

```bash
# Via CLI
anubis banish node-4

# Via API
curl -X DELETE http://leader:8080/api/v1/cluster/peers/node-4 \
  -H "Authorization: Bearer $TOKEN"
```

### Work Distribution

The cluster automatically distributes souls across nodes:

- **Round Robin**: Even distribution
- **Region Aware**: Geographically optimal
- **Redundant**: Multiple nodes per soul

View current distribution in the Necropolis dashboard.

## Status Pages

### Creating a Status Page

1. Navigate to **Status Pages** in the sidebar
2. Click **Create Status Page**
3. Configure:

```yaml
Name: Example Systems Status
Slug: example-status
Description: Real-time status of Example Inc services
Logo: https://example.com/logo.png
Favicon: https://example.com/favicon.ico
Theme: Dark

Souls to Display:
  - API (api.example.com)
  - Website (www.example.com)
  - CDN (cdn.example.com)

Customize:
  - Header Color: #D4AF37
  - Show Uptime: true
  - Show Latency: true
  - History Days: 90
```

### Public Access

Status pages are publicly accessible:

```
https://your-anubiswatch.com/status/example-status
```

### Subscription

Visitors can subscribe to updates:
- Email notifications
- RSS feed
- Webhook
- Slack integration

## Troubleshooting

### Common Issues

#### Service Won't Start

```bash
# Check logs
journalctl -u anubis -f

# Verify configuration
anubis init --dry-run

# Check port availability
netstat -tlnp | grep 8080
netstat -tlnp | grep 7946
```

#### Node Can't Join Cluster

1. Verify network connectivity:
   ```bash
   telnet <leader-ip> 7946
   ```

2. Check firewall rules
3. Verify advertise address is correct
4. Check logs for "connection refused" errors

#### High Memory Usage

1. Reduce check frequency
2. Decrease retention period
3. Enable sampling for high-volume souls
4. Check for goroutine leaks:
   ```bash
   curl http://localhost:8080/debug/pprof/goroutine
   ```

#### Alerts Not Firing

1. Verify alert channel configuration
2. Check rule syntax
3. Ensure cooldown period has passed
4. Review alert logs:
   ```bash
   grep "alert" /var/log/anubis.log
   ```

### Debug Commands

```bash
# System status
anubis judge

# Cluster status
anubis necropolis

# Export soul definitions
anubis souls export --format json

# Current judgments
anubis judge

# Force check
anubis check <soul-id>

# Export metrics
curl http://localhost:8080/metrics

# Health checks
curl http://localhost:8080/health
curl http://localhost:8080/ready
```

### Getting Help

- **Documentation**: https://docs.anubis.watch
- **GitHub Issues**: https://github.com/AnubisWatch/AnubisWatch/issues
- **Community**: https://discord.gg/anubiswatch
- **Email**: support@anubis.watch
