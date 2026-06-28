#!/bin/bash
# AnubisWatch Node 2 - Follower

set -e

DATA_DIR="${DATA_DIR:-/tmp/anubis2}"
PORT="${PORT:-8889}"
CLUSTER_PORT="${CLUSTER_PORT:-7947}"
NODE_ID="${NODE_ID:-jackal-02}"
CONFIG_FILE="/tmp/anubis_node2.json"

echo "Starting AnubisWatch Node 2..."
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
      "bootstrap": false
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