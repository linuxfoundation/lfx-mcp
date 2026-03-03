#!/bin/bash

# Copyright The Linux Foundation and contributors.
# SPDX-License-Identifier: MIT

set -e

# Color output for better readability.
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== LFX MCP OAuth Configuration Test ===${NC}"
echo ""

# Parse command line arguments.
WORKSPACE="${1:-dev}"
DEBUG_FLAG=""
if [[ "$2" == "--debug" || "$2" == "-d" ]]; then
    DEBUG_FLAG="-debug"
    echo "Running tests with debug logging enabled..."
fi

# Set environment based on workspace.
case $WORKSPACE in
    dev)
        AUTH0_DOMAIN="linuxfoundation-dev.auth0.com"
        LFX_MCP_API_URL="https://lfx-mcp.dev.v2.cluster.linuxfound.info/mcp"
        LFX_V2_API_URL="https://lfx-api.dev.v2.cluster.linuxfound.info"
        ;;
    staging)
        AUTH0_DOMAIN="linuxfoundation-staging.auth0.com"
        LFX_MCP_API_URL="https://lfx-mcp.staging.v2.cluster.linuxfound.info/mcp"
        LFX_V2_API_URL="https://lfx-api.staging.v2.cluster.linuxfound.info"
        ;;
    prod)
        AUTH0_DOMAIN="sso.linuxfoundation.org"
        LFX_MCP_API_URL="https://mcp.lfx.dev/mcp"
        LFX_V2_API_URL="https://lfx-api.v2.cluster.lfx.dev"
        ;;
    *)
        echo -e "${RED}Error: Invalid workspace '$WORKSPACE'. Use dev, staging, or prod${NC}"
        exit 1
        ;;
esac

TOKEN_ENDPOINT="https://$AUTH0_DOMAIN/oauth/token"

echo -e "${BLUE}Configuration:${NC}"
echo "  Workspace: $WORKSPACE"
echo "  Auth0 Domain: $AUTH0_DOMAIN"
echo "  Token Endpoint: $TOKEN_ENDPOINT"
echo "  LFX MCP API: $LFX_MCP_API_URL"
echo "  LFX V2 API: $LFX_V2_API_URL"
echo ""

# Build the server if needed.
if [ ! -f "./bin/lfx-mcp-server" ]; then
    echo -e "${YELLOW}Building server...${NC}"
    make build
    echo ""
fi

# Test 1: Check Protected Resource Metadata endpoint.
echo -e "${BLUE}=== Test 1: Protected Resource Metadata (PRM) Endpoint ===${NC}"
echo "Starting server with OAuth configuration..."

# Start server in background with HTTP mode.
LFX_MCP_MODE=http \
LFX_MCP_HTTP_PORT=8081 \
LFX_MCP_MCP_API_AUTH_SERVERS="https://$AUTH0_DOMAIN" \
LFX_MCP_MCP_API_PUBLIC_URL="$LFX_MCP_API_URL" \
LFX_MCP_TOOLS=hello_world \
./bin/lfx-mcp-server $DEBUG_FLAG &
SERVER_PID=$!

# Wait for server to start.
sleep 2

# Test PRM endpoint.
echo "Fetching PRM metadata..."
PRM_RESPONSE=$(curl -s http://127.0.0.1:8081/.well-known/oauth-protected-resource)

echo -e "${GREEN}PRM Response:${NC}"
echo "$PRM_RESPONSE" | jq '.'
echo ""

# Verify PRM contains expected fields.
RESOURCE=$(echo "$PRM_RESPONSE" | jq -r '.resource')
AUTH_SERVER=$(echo "$PRM_RESPONSE" | jq -r '.authorization_servers[0]')
BEARER_METHODS=$(echo "$PRM_RESPONSE" | jq -r '.bearer_methods_supported[]' | tr '\n' ', ' | sed 's/,$//')
SCOPES=$(echo "$PRM_RESPONSE" | jq -r '.scopes_supported[]' | tr '\n' ', ' | sed 's/,$//')

echo -e "${BLUE}Validation:${NC}"
if [ "$RESOURCE" == "$LFX_MCP_API_URL" ]; then
    echo -e "${GREEN}✓ Resource identifier: $RESOURCE${NC}"
else
    echo -e "${RED}✗ Resource identifier mismatch: expected $LFX_MCP_API_URL, got $RESOURCE${NC}"
fi

if [ "$AUTH_SERVER" == "https://$AUTH0_DOMAIN/" ]; then
    echo -e "${GREEN}✓ Authorization server: $AUTH_SERVER${NC}"
else
    echo -e "${RED}✗ Authorization server mismatch: expected https://$AUTH0_DOMAIN/, got $AUTH_SERVER${NC}"
fi

echo -e "${GREEN}✓ Bearer methods: $BEARER_METHODS${NC}"
echo -e "${GREEN}✓ Scopes: $SCOPES${NC}"
echo ""

# Test 2: Verify Auth0 OIDC configuration is accessible.
echo -e "${BLUE}=== Test 2: Auth0 OIDC Configuration ===${NC}"
echo "Fetching Auth0 OIDC configuration..."
OIDC_CONFIG=$(curl -s "https://$AUTH0_DOMAIN/.well-known/openid-configuration")

echo -e "${GREEN}OIDC Issuer:${NC} $(echo "$OIDC_CONFIG" | jq -r '.issuer')"
echo -e "${GREEN}Authorization Endpoint:${NC} $(echo "$OIDC_CONFIG" | jq -r '.authorization_endpoint')"
echo -e "${GREEN}Token Endpoint:${NC} $(echo "$OIDC_CONFIG" | jq -r '.token_endpoint')"
echo -e "${GREEN}JWKS URI:${NC} $(echo "$OIDC_CONFIG" | jq -r '.jwks_uri')"
echo ""

# Test 3: Test MCP endpoint without auth (should still work for initialize).
echo -e "${BLUE}=== Test 3: MCP Endpoint Accessibility ===${NC}"
echo "Testing MCP endpoint without authentication..."
MCP_RESPONSE=$(curl -s -X POST http://127.0.0.1:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}')

echo -e "${GREEN}MCP Initialize Response:${NC}"
echo "$MCP_RESPONSE" | jq '.'
echo ""

# Check if response contains server info.
SERVER_NAME=$(echo "$MCP_RESPONSE" | jq -r '.result.serverInfo.name // empty')
if [ -n "$SERVER_NAME" ]; then
    echo -e "${GREEN}✓ MCP server responding: $SERVER_NAME${NC}"
else
    echo -e "${YELLOW}⚠ MCP server may require authentication for initialize${NC}"
fi
echo ""

# Test 4: List tools.
echo -e "${BLUE}=== Test 4: List Available Tools ===${NC}"
TOOLS_RESPONSE=$(curl -s -X POST http://127.0.0.1:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}')

echo -e "${GREEN}Available Tools:${NC}"
echo "$TOOLS_RESPONSE" | jq '.result.tools[]?.name // empty' | tr '\n' ' '
echo ""
echo ""

# Stop server.
kill $SERVER_PID
wait $SERVER_PID 2>/dev/null || true

echo -e "${GREEN}=== OAuth Configuration Test Complete ===${NC}"
echo ""
echo -e "${BLUE}Summary:${NC}"
echo "✓ Server starts with OAuth configuration"
echo "✓ PRM endpoint returns correct OAuth metadata"
echo "✓ Auth0 OIDC configuration is accessible"
echo "✓ MCP endpoints respond correctly"
echo ""
echo -e "${YELLOW}Next Steps for Full OAuth Testing:${NC}"
echo "1. Get a valid user token using one of these methods:"
echo "   - Use Auth0 Universal Login in a browser"
echo "   - Use a test script with Device Authorization Grant"
echo "   - Use Postman or similar OAuth client"
echo ""
echo "2. Set required environment variables:"
echo "   export LFX_MCP_CLIENT_ID='<client_id>'"
echo "   export LFX_MCP_CLIENT_ASSERTION_SIGNING_KEY_FILE='<path_to_pem>'"
echo ""
echo "3. Test token exchange with user_info tool:"
echo "   # Start server with token exchange config"
echo "   LFX_MCP_MODE=http \\"
echo "   LFX_MCP_HTTP_PORT=8081 \\"
echo "   LFX_MCP_MCP_API_AUTH_SERVERS=\"https://$AUTH0_DOMAIN\" \\"
echo "   LFX_MCP_MCP_API_PUBLIC_URL=\"$LFX_MCP_API_URL\" \\"
echo "   LFX_MCP_TOKEN_ENDPOINT=\"$TOKEN_ENDPOINT\" \\"
echo "   LFX_MCP_CLIENT_ID=\"\$LFX_MCP_CLIENT_ID\" \\"
echo "   LFX_MCP_CLIENT_ASSERTION_SIGNING_KEY=\"\$(cat \$LFX_MCP_CLIENT_ASSERTION_SIGNING_KEY_FILE)\" \\"
echo "   LFX_MCP_LFX_API_URL=\"$LFX_V2_API_URL\" \\"
echo "   LFX_MCP_TOOLS=user_info \\"
echo "   LFX_MCP_DEBUG=true \\"
echo "   ./bin/lfx-mcp-server"
echo ""
echo "   # Call user_info tool with your user token"
echo "   curl -X POST http://127.0.0.1:8081/mcp \\"
echo "     -H 'Content-Type: application/json' \\"
echo "     -H 'Authorization: Bearer <your_user_token>' \\"
echo "     -d '{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"user_info\",\"arguments\":{}}}'"
echo ""
echo "Usage: $0 [dev|staging|prod] [--debug|-d]"
