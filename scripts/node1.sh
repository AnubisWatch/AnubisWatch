#!/bin/bash
# AnubisWatch Node 1 - Leader/Initial Node

set -e

DATA_DIR="${DATA_DIR:-/tmp/anubis1}"
PORT="${PORT:-8888}"
CLUSTER_PORT="${CLUSTER_PORT:-7946}"
NODE_ID="${NODE_ID:-jackal-01}"
CONFIG_FILE="/tmp/anubis_node1.json"

echo "Starting AnubisWatch Node 1..."
echo "  Data Dir: $DATA_DIR"
echo "  REST Port: $PORT"
echo "  Cluster Port: $CLUSTER_PORT"
echo "  Node ID: $NODE_ID"

mkdir -p "$DATA_DIR"

# Create node-specific config
cat > "$CONFIG_FILE" << EOF
{
  "server": {
    "host": "0.0.0.0",
    "port": $PORT,
    "tls": {"enabled": false}
  },
  "storage": {
    "path": "$DATA_DIR",
    "retention_days": 90,
    "encryption": {"enabled": false}
  },
  "auth": {
    "enabled": true,
    "type": "local",
    "local": {
      "admin_email": "admin@anubis.watch",
      "admin_password": "DemoPass123!"
    }
  },
  "necropolis": {
    "enabled": true,
    "raft": {
      "node_id": "$NODE_ID",
      "bind_addr": "127.0.0.1:$CLUSTER_PORT",
      "bootstrap": true
    }
  },
  "dashboard": {
    "enabled": true,
    "theme": "dark"
  },
  "logging": {
    "level": "info",
    "format": "json"
  }
}
EOF

ANUBIS_CONFIG="$CONFIG_FILE" ANUBIS_DATA_DIR="$DATA_DIR" ./bin/anubis serve