#!/bin/bash
# Test Protected Resource Metadata endpoint

set -e

# Start server in background with MCP API configured
./bin/lfx-mcp-server \
  -mode=http \
  -http.port=8081 \
  -mcp_api.auth_servers=https://dev-lfx.us.auth0.com \
  -mcp_api.public_url=https://api-dev.lfx.linuxfoundation.org/mcp &

SERVER_PID=$!
echo "Started server with PID $SERVER_PID"

# Wait for server to start
sleep 2

# Test PRM endpoint
echo "Testing PRM endpoint..."
curl -s http://127.0.0.1:8081/.well-known/oauth-protected-resource | jq .

# Cleanup
kill $SERVER_PID 2>/dev/null || true
echo "Test complete"
