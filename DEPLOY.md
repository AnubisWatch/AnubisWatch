# AnubisWatch Deployment Guide

## Quick Start

### 1. Binary İndirme

```bash
# Linux/Mac
curl -sSL https://github.com/AnubisWatch/anubiswatch/releases/latest/download/anubis-linux-amd64 -o anubis
chmod +x anubis

# Windows (PowerShell)
Invoke-WebRequest -Uri "https://github.com/AnubisWatch/anubiswatch/releases/latest/download/anubis-windows-amd64.exe" -OutFile "anubis.exe"
```

### 2. Config Oluşturma

```bash
# Interactive wizard
./anubis init

# Or simple config
./anubis init --output anubis.json
```

### 3. Sunucuyu Başlatma

```bash
./anubis serve --config anubis.json
```

**Dashboard**: http://localhost:8080  
**Login**: admin@anubis.watch / admin

---

## Production Deployment

### Docker

```bash
docker run -d \
  --name anubiswatch \
  -p 8080:8080 \
  -v $(pwd)/data:/data \
  -v $(pwd)/anubis.json:/config/anubis.json \
  anubiswatch/anubis:latest
```

### Docker Compose

```yaml
version: '3.8'

services:
  anubis:
    image: anubiswatch/anubis:latest
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
      - ./anubis.json:/config/anubis.json
    environment:
      - ANUBIS_CONFIG=/config/anubis.json
    restart: unless-stopped
```

### Systemd (Linux)

```ini
# /etc/systemd/system/anubiswatch.service
[Unit]
Description=AnubisWatch Monitoring
After=network.target

[Service]
Type=simple
User=anubis
Group=anubis
ExecStart=/usr/local/bin/anubis serve --config /etc/anubiswatch/anubis.json
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable anubiswatch
sudo systemctl start anubiswatch
```

---

## Cluster Mode

### Node 1 (Bootstrap)

```json
{
  "necropolis": {
    "enabled": true,
    "node_name": "jackal-1",
    "region": "us-east",
    "raft": {
      "bootstrap": true,
      "bind_addr": "0.0.0.0:7946"
    }
  }
}
```

### Node 2 (Join)

```json
{
  "necropolis": {
    "enabled": true,
    "node_name": "jackal-2",
    "region": "us-west",
    "raft": {
      "bootstrap": false,
      "bind_addr": "0.0.0.0:7946"
    },
    "peers": [
      {
        "id": "jackal-1",
        "address": "10.0.0.1:7946",
        "region": "us-east"
      }
    ]
  }
}
```

---

## TLS/HTTPS

### Let's Encrypt (Auto)

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 443,
    "tls": {
      "enabled": true,
      "auto_cert": true,
      "acme_email": "admin@example.com"
    }
  }
}
```

### Custom Certificate

```json
{
  "server": {
    "tls": {
      "enabled": true,
      "cert": "/path/to/cert.pem",
      "key": "/path/to/key.pem"
    }
  }
}
```

---

## API Quick Reference

### Authentication

```bash
# Login
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@anubis.watch","password":"admin"}'

# Use token
curl http://localhost:8080/api/v1/souls \
  -H "Authorization: Bearer <token>"
```

### Create Soul (Monitor)

```bash
curl -X POST http://localhost:8080/api/v1/souls \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "My API",
    "type": "http",
    "target": "https://api.example.com/health",
    "weight": "30s"
  }'
```

### WebSocket Realtime

```javascript
const ws = new WebSocket('ws://localhost:8080/ws?workspace=default');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log(data.type, data.payload);
};

// Subscribe to events
ws.send(JSON.stringify({
  type: 'subscribe',
  events: ['judgment', 'alert']
}));
```

---

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `ANUBIS_CONFIG` | Config file path | `./anubis.json` |
| `ANUBIS_DATA_DIR` | Data directory | `./data` |
| `ANUBIS_LOG_LEVEL` | Log level (debug/info/warn/error) | `info` |
| `ANUBIS_LOG_FORMAT` | Log format (json/text) | `json` |

---

## Troubleshooting

### Port Already in Use

```bash
# Find process using port 8080
lsof -i :8080
# or
netstat -tulpn | grep 8080

# Kill process or change port in config
```

### Permission Denied (Data Directory)

```bash
sudo chown -R $(whoami):$(whoami) ./data
chmod 755 ./data
```

### Cluster Join Failed

```bash
# Check firewall rules
sudo ufw allow 7946/tcp
sudo ufw allow 7947/udp  # mDNS

# Verify node connectivity
nc -zv <peer-ip> 7946
```

---

## Monitoring

### Health Check

```bash
curl http://localhost:8080/health
curl http://localhost:8080/ready
```

### Metrics (Prometheus - TODO)

```bash
curl http://localhost:8080/metrics
```

### Cluster Status

```bash
curl http://localhost:8080/api/v1/cluster/status \
  -H "Authorization: Bearer <token>"
```

---

## Backup & Restore

### Backup

```bash
# Stop server
sudo systemctl stop anubiswatch

# Backup data directory
tar -czf anubis-backup-$(date +%Y%m%d).tar.gz ./data

# Start server
sudo systemctl start anubiswatch
```

### Restore

```bash
# Stop server
sudo systemctl stop anubiswatch

# Restore data
rm -rf ./data
tar -xzf anubis-backup-20260101.tar.gz

# Start server
sudo systemctl start anubiswatch
```

---

## Support

- **Docs**: https://docs.anubis.watch
- **Issues**: https://github.com/AnubisWatch/anubiswatch/issues
- **Discord**: https://discord.gg/anubiswatch
