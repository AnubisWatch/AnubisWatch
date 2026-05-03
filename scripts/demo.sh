#!/bin/bash
# AnubisWatch Demo Script

set -e

echo "═══════════════════════════════════════════════════════════"
echo "⚖️  AnubisWatch Production Demo"
echo "═══════════════════════════════════════════════════════════"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "Cleaning up..."
    if [ -n "$SERVER_PID" ]; then
        kill $SERVER_PID 2>/dev/null || true
    fi
    rm -rf /tmp/anubis-demo-data
}
trap cleanup EXIT

# Prepare demo config
mkdir -p /tmp/anubis-demo-data
cat > /tmp/anubis-demo.json << 'EOF'
{
  "server": {
    "host": "0.0.0.0",
    "port": 18080,
    "tls": { "enabled": false }
  },
  "storage": { "path": "/tmp/anubis-demo-data" },
  "auth": {
    "enabled": true,
    "type": "local",
    "local": {
      "admin_email": "admin@demo.local",
      "admin_password": "DemoPass123!"
    }
  },
  "dashboard": { "enabled": true },
  "logging": { "level": "info", "format": "json" },
  "souls": []
}
EOF

echo "1. Starting AnubisWatch Server..."
./bin/anubis serve --config /tmp/anubis-demo.json --single &
SERVER_PID=$!
sleep 3

echo ""
echo "2. Testing Health Endpoint..."
curl -fsS http://localhost:18080/health | grep -q '"status"' && echo "   ✓ Server responding"

echo ""
echo "3. Authenticating..."
AUTH_RESPONSE=$(curl -s -X POST http://localhost:18080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@demo.local","password":"DemoPass123!"}')
TOKEN=$(echo "$AUTH_RESPONSE" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
echo "   ✓ Token received: ${TOKEN:0:20}..."

echo ""
echo "4. Creating Test Souls..."

# Create HTTP soul
curl -s -X POST http://localhost:18080/api/v1/souls \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Google DNS",
    "type": "dns",
    "target": "dns.google",
    "weight": "30s",
    "timeout": "5s",
    "enabled": true,
    "dns": {
      "record_type": "A"
    }
  }' > /dev/null
echo "   ✓ DNS soul created"

# Create another HTTP soul  
curl -s -X POST http://localhost:18080/api/v1/souls \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "HTTPBin API",
    "type": "http",
    "target": "https://httpbin.org/get",
    "weight": "60s",
    "timeout": "10s",
    "enabled": true,
    "http": {
      "method": "GET",
      "valid_status": [200],
      "headers": {}
    }
  }' > /dev/null
echo "   ✓ HTTP soul created"

echo ""
echo "5. Listing Souls..."
SOULS=$(curl -s -H "Authorization: Bearer $TOKEN" http://localhost:18080/api/v1/souls)
SOUL_COUNT=$(echo "$SOULS" | grep -o '"id"' | wc -l)
echo "   ✓ $SOUL_COUNT soul(s) registered"

echo ""
echo "6. Checking Status Page (Public)..."
STATUS=$(curl -s http://localhost:18080/public/status)
echo "   ✓ Status: $(echo "$STATUS" | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4)"

echo ""
echo "7. Waiting for first judgments (5s)..."
sleep 5
JUDGMENTS=$(curl -s -H "Authorization: Bearer $TOKEN" http://localhost:18080/api/v1/judgments | grep -o '"id"' | wc -l)
echo "   ✓ $JUDGMENTS judgment(s) recorded"

echo ""
echo "8. Dashboard Check..."
DASHBOARD_STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:18080/)
if [ "$DASHBOARD_STATUS" = "200" ]; then
    echo "   ✓ Dashboard accessible (HTTP 200)"
else
    echo "   ✗ Dashboard returned HTTP $DASHBOARD_STATUS"
fi

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "✅ DEMO COMPLETE"
echo "═══════════════════════════════════════════════════════════"
echo ""
echo "Dashboard: http://localhost:18080/"
echo "Login:     admin@demo.local / DemoPass123!"
echo "API:       http://localhost:18080/api/v1/"
echo ""

# Keep server running for manual testing
echo "Server running. Press Ctrl+C to stop..."
wait $SERVER_PID
