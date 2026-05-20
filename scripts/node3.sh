#!/bin/bash
# AnubisWatch Node 3 - Follower

set -e

DATA_DIR="${DATA_DIR:-/tmp/anubis3}"
PORT="${PORT:-8890}"
CLUSTER_PORT="${CLUSTER_PORT:-7948}"
NODE_ID="${NODE_ID:-jackal-03}"
CONFIG_FILE="/tmp/anubis_node3.json"

echo "Starting AnubisWatch Node 3..."
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
      "admin_password": "admin123"
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