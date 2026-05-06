# WebSocket Protocol (Duat)

AnubisWatch uses WebSocket connections for real-time updates between the server and dashboard.

## Connection

**Endpoint**: `ws://localhost:8080/ws`

**Authentication**: Same as REST API - include Bearer token in the connection handshake:
```
Authorization: Bearer <token>
```

## Protocol

Messages are JSON-encoded with the following structure:

```json
{
  "type": "message_type",
  "timestamp": "2026-04-09T12:00:00Z",
  "payload": {}
}
```

## Message Types

### Client → Server

#### `subscribe`
Subscribe to real-time updates for a soul or workspace.

```json
{
  "type": "subscribe",
  "payload": {
    "channel": "soul:soul-001",
    "workspace": "ws-default"
  }
}
```

#### `unsubscribe`
Unsubscribe from updates.

```json
{
  "type": "unsubscribe",
  "payload": {
    "channel": "soul:soul-001"
  }
}
```

#### `ping`
Keep connection alive.

```json
{
  "type": "ping",
  "timestamp": "2026-04-09T12:00:00Z"
}
```

### Server → Client

#### `judgment`
New judgment received.

```json
{
  "type": "judgment",
  "timestamp": "2026-04-09T12:00:00Z",
  "payload": {
    "id": "judg-001",
    "soul_id": "soul-001",
    "status": "alive",
    "duration": 150,
    "timestamp": "2026-04-09T12:00:00Z",
    "details": {
      "status_code": 200,
      "response_time": "150ms"
    }
  }
}
```

#### `status_change`
Soul status changed.

```json
{
  "type": "status_change",
  "timestamp": "2026-04-09T12:00:00Z",
  "payload": {
    "soul_id": "soul-001",
    "old_status": "alive",
    "new_status": "dead",
    "last_judgment": {
      "id": "judg-002",
      "status": "dead",
      "error": "Connection refused"
    }
  }
}
```

#### `alert`
Alert triggered.

```json
{
  "type": "alert",
  "timestamp": "2026-04-09T12:00:00Z",
  "payload": {
    "id": "alert-001",
    "rule_id": "rule-high-latency",
    "soul_id": "soul-001",
    "severity": "warning",
    "message": "High latency detected: 2500ms",
    "channels": ["slack-ops"]
  }
}
```

#### `cluster_event`
Cluster membership changes.

```json
{
  "type": "cluster_event",
  "timestamp": "2026-04-09T12:00:00Z",
  "payload": {
    "event": "node_joined",
    "node": {
      "id": "jackal-02",
      "address": "10.0.0.2:7946",
      "region": "us-east",
      "role": "follower"
    }
  }
}
```

#### `pong`
Response to ping.

```json
{
  "type": "pong",
  "timestamp": "2026-04-09T12:00:00Z",
  "payload": {
    "server_time": "2026-04-09T12:00:00Z"
  }
}
```

## Connection Rooms

Clients can join specific rooms for targeted updates:

- `workspace:{id}` - All updates for a workspace
- `soul:{id}` - Updates for a specific soul
- `cluster` - Cluster membership changes
- `alerts` - Alert notifications
- `system` - System events

## Error Handling

Connection errors return a JSON error message:

```json
{
  "type": "error",
  "timestamp": "2026-04-09T12:00:00Z",
  "payload": {
    "code": "AUTH_FAILED",
    "message": "Invalid or expired token"
  }
}
```

## JavaScript Example

```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onopen = () => {
  // Subscribe to soul updates
  ws.send(JSON.stringify({
    type: 'subscribe',
    payload: { channel: 'soul:soul-001' }
  }));
};

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  
  switch (msg.type) {
    case 'judgment':
      console.log('New judgment:', msg.payload);
      break;
    case 'status_change':
      console.log('Status changed:', msg.payload);
      break;
  }
};

// Keep alive
setInterval(() => {
  ws.send(JSON.stringify({
    type: 'ping',
    timestamp: new Date().toISOString()
  }));
}, 30000);
```

## Reconnection

The client should implement exponential backoff for reconnection:

1. Attempt immediate reconnection on unexpected close
2. If failed, wait 1 second, then retry
3. Increase wait time: 1s, 2s, 5s, 10s, 30s
4. Maximum backoff: 30 seconds
5. Maximum retry attempts: unlimited (keep trying)

## Rate Limiting

- Maximum 100 connections per IP
- Maximum 100 messages per second per connection
- Pings are exempt from rate limiting
