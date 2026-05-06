# Model Context Protocol (MCP) Server

AnubisWatch includes a built-in MCP server for AI agent integration, enabling AI assistants to monitor and manage your infrastructure.

## Overview

MCP (Model Context Protocol) is an open protocol for AI agent integration. AnubisWatch implements MCP to allow AI assistants to:

- Query service health status
- List monitored targets (souls)
- Trigger manual health checks
- View recent judgments
- Manage alert channels

## Connection

**Endpoint**: `http://localhost:8080/mcp`

**Protocol**: MCP Protocol version 2024-11-05

**Authentication**: Bearer token required

```
Authorization: Bearer <token>
```

## Tools

### anubis_list_souls

List all monitored souls (targets).

**Parameters:**
```json
{
  "workspace": "string, optional - Filter by workspace ID",
  "status": "string, optional - Filter by status (alive, dead, degraded)",
  "limit": "number, optional - Maximum results (default: 50)"
}
```

**Returns:**
```json
{
  "souls": [
    {
      "id": "soul-001",
      "name": "Production API",
      "type": "http",
      "target": "https://api.example.com/health",
      "status": "alive",
      "last_check": "2026-04-09T12:00:00Z",
      "latency_ms": 150
    }
  ],
  "total": 42
}
```

### anubis_get_soul_status

Get detailed status of a specific soul.

**Parameters:**
```json
{
  "soul_id": "string, required - Soul identifier"
}
```

**Returns:**
```json
{
  "id": "soul-001",
  "name": "Production API",
  "status": "alive",
  "last_judgment": {
    "id": "judg-123",
    "status": "alive",
    "duration": 150,
    "timestamp": "2026-04-09T12:00:00Z"
  },
  "uptime_24h": 99.9,
  "avg_latency_24h": 145,
  "checks_last_24h": 2880,
  "failures_last_24h": 3
}
```

### anubis_trigger_judgment

Manually trigger a health check.

**Parameters:**
```json
{
  "soul_id": "string, required - Soul to check"
}
```

**Returns:**
```json
{
  "judgment_id": "judg-456",
  "status": "pending",
  "estimated_duration": "5s"
}
```

### anubis_list_judgments

Get recent judgments for a soul.

**Parameters:**
```json
{
  "soul_id": "string, required - Soul identifier",
  "limit": "number, optional - Maximum results (default: 10)",
  "since": "string, optional - ISO timestamp for time range"
}
```

**Returns:**
```json
{
  "judgments": [
    {
      "id": "judg-123",
      "status": "alive",
      "duration": 150,
      "timestamp": "2026-04-09T12:00:00Z",
      "details": {
        "status_code": 200,
        "response_size": 1024
      }
    }
  ]
}
```

### anubis_list_alerts

List recent alerts.

**Parameters:**
```json
{
  "status": "string, optional - Filter by status (open, resolved, acknowledged)",
  "severity": "string, optional - Filter by severity (critical, warning, info)",
  "limit": "number, optional - Maximum results (default: 20)"
}
```

**Returns:**
```json
{
  "alerts": [
    {
      "id": "alert-001",
      "soul_id": "soul-001",
      "severity": "warning",
      "message": "High latency detected: 2500ms",
      "status": "open",
      "created_at": "2026-04-09T11:30:00Z"
    }
  ]
}
```

### anubis_get_metrics

Get system metrics.

**Parameters:**
```json
{
  "metric_type": "string, optional - Type of metrics (souls, system, cluster)"
}
```

**Returns:**
```json
{
  "total_souls": 42,
  "healthy_souls": 40,
  "degraded_souls": 1,
  "failed_souls": 1,
  "avg_latency": 145,
  "system": {
    "cpu_usage": 15.5,
    "memory_usage": 2048,
    "disk_usage": 45.2,
    "goroutines": 47
  }
}
```

### anubis_create_soul

Create a new monitored soul.

**Parameters:**
```json
{
  "name": "string, required - Display name",
  "type": "string, required - Protocol type (http, tcp, dns, etc.)",
  "target": "string, required - Target URL/address",
  "interval": "string, optional - Check interval (default: 60s)",
  "timeout": "string, optional - Request timeout (default: 10s)",
  "config": "object, optional - Protocol-specific config"
}
```

**Returns:**
```json
{
  "id": "soul-new-001",
  "status": "created",
  "first_check": "pending"
}
```

### anubis_acknowledge_alert

Acknowledge an alert.

**Parameters:**
```json
{
  "alert_id": "string, required - Alert to acknowledge",
  "note": "string, optional - Acknowledgment note"
}
```

**Returns:**
```json
{
  "id": "alert-001",
  "status": "acknowledged",
  "acknowledged_at": "2026-04-09T12:05:00Z"
}
```

## Resources

MCP resources provide read-only access to data:

### soul://{id}

Get soul configuration and current status.

### judgments://{soul_id}

Get recent judgments for a soul.

### metrics://system

Get current system metrics.

### config://anubis

Get current AnubisWatch configuration.

## Prompts

MCP prompts provide templated queries:

### anubis_analyze_outage

Analyze a recent service outage.

**Arguments:**
- `soul_id`: Soul that experienced outage
- `start_time`: When the outage started
- `end_time`: When the outage ended

**Template:**
```
Please analyze the outage for {soul_id} from {start_time} to {end_time}.
Include:
1. Root cause analysis from judgment logs
2. Duration and impact
3. Recovery time
4. Recommendations for prevention
```

### anubis_health_report

Generate a health report for a workspace.

**Arguments:**
- `workspace_id`: Target workspace
- `period`: Report period (24h, 7d, 30d)

**Template:**
```
Generate a health report for workspace {workspace_id} over the past {period}.
Include:
1. Overall service health
2. Top 5 slowest services
3. Services with most failures
4. Alert summary
5. Recommendations
```

## Example Usage

### With Claude Code

Configure `.claude/mcp.json`:

```json
{
  "mcpServers": {
    "anubis": {
      "url": "http://localhost:8080/mcp",
      "auth": {
        "type": "bearer",
        "token": "${ANUBIS_API_TOKEN}"
      }
    }
  }
}
```

Then use natural language:

```
"Check the status of all production services"
"What's the current latency of the API?"
"Show me recent alerts for the database"
"Create a new monitor for https://new-service.example.com"
```

### Direct API Calls

```bash
# List souls
curl -X POST http://localhost:8080/mcp/tools/anubis_list_souls \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}'

# Get soul status
curl -X POST http://localhost:8080/mcp/tools/anubis_get_soul_status \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"soul_id": "soul-001"}'

# Trigger judgment
curl -X POST http://localhost:8080/mcp/tools/anubis_trigger_judgment \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"soul_id": "soul-001"}'
```

## Security

- All MCP requests require valid authentication
- Rate limited to 100 requests per minute per token
- Sensitive operations (create, delete) require admin role
- All actions are logged to audit log

## Error Handling

MCP errors follow the standard MCP error format:

```json
{
  "error": {
    "code": "SOUL_NOT_FOUND",
    "message": "Soul with ID 'soul-999' not found",
    "details": {
      "soul_id": "soul-999"
    }
  }
}
```

Common error codes:
- `AUTH_FAILED` - Authentication error
- `SOUL_NOT_FOUND` - Soul doesn't exist
- `INVALID_PARAMS` - Missing or invalid parameters
- `RATE_LIMITED` - Too many requests
- `INTERNAL_ERROR` - Server error
