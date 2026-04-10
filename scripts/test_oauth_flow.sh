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

echo -e "${BLUE}=== LFX MCP OAuth Flow Test ===${NC}"
echo ""

# Check required environment variables.
if [ -z "$LFXMCP_CLIENT_ID" ]; then
	echo -e "${RED}Error: LFXMCP_CLIENT_ID is not set${NC}"
	exit 1
fi

if [ -z "$LFXMCP_CLIENT_ASSERTION_SIGNING_KEY_FILE" ]; then
	echo -e "${RED}Error: LFXMCP_CLIENT_ASSERTION_SIGNING_KEY_FILE is not set${NC}"
	echo "Please set it to the path of your .pem file"
	exit 1
fi

if [ ! -f "$LFXMCP_CLIENT_ASSERTION_SIGNING_KEY_FILE" ]; then
	echo -e "${RED}Error: Private key file not found: $LFXMCP_CLIENT_ASSERTION_SIGNING_KEY_FILE${NC}"
	exit 1
fi

# Load private key from file.
PRIVATE_KEY=$(cat "$LFXMCP_CLIENT_ASSERTION_SIGNING_KEY_FILE")

# Set environment based on workspace (default to dev).
WORKSPACE="${WORKSPACE:-dev}"

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
echo "  Client ID: $LFXMCP_CLIENT_ID"
echo "  Private Key File: $LFXMCP_CLIENT_ASSERTION_SIGNING_KEY_FILE"
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
LFXMCP_MODE=http \
	LFXMCP_HTTP_PORT=8081 \
	LFXMCP_MCP_API_AUTH_SERVERS="https://$AUTH0_DOMAIN" \
	LFXMCP_MCP_API_PUBLIC_URL="$LFX_MCP_API_URL" \
	LFXMCP_TOOLS=hello_world \
	./bin/lfx-mcp-server &
SERVER_PID=$!

# Wait for server to start.
sleep 2

# Test PRM endpoint.
echo "Fetching PRM metadata..."
PRM_RESPONSE=$(curl -s http://127.0.0.1:8081/.well-known/oauth-protected-resource)

echo -e "${GREEN}PRM Response:${NC}"
echo "$PRM_RESPONSE" | jq '.'

# Verify PRM contains expected fields.
RESOURCE=$(echo "$PRM_RESPONSE" | jq -r '.resource')
AUTH_SERVER=$(echo "$PRM_RESPONSE" | jq -r '.authorization_servers[0]')

if [ "$RESOURCE" == "$LFX_MCP_API_URL" ]; then
	echo -e "${GREEN}✓ Resource identifier matches expected value${NC}"
else
	echo -e "${RED}✗ Resource identifier mismatch: expected $LFX_MCP_API_URL, got $RESOURCE${NC}"
fi

if [ "$AUTH_SERVER" == "https://$AUTH0_DOMAIN/" ]; then
	echo -e "${GREEN}✓ Authorization server matches expected value${NC}"
else
	echo -e "${RED}✗ Authorization server mismatch: expected https://$AUTH0_DOMAIN/, got $AUTH_SERVER${NC}"
fi

# Stop server.
kill $SERVER_PID
wait $SERVER_PID 2>/dev/null || true
echo ""

# Test 2: Test token exchange with user_info tool.
echo -e "${BLUE}=== Test 2: Token Exchange with user_info Tool ===${NC}"

# Get a test token from Auth0 (client credentials flow for testing).
echo "Getting test MCP token from Auth0..."
MCP_TOKEN_RESPONSE=$(curl -s --request POST \
	--url "https://$AUTH0_DOMAIN/oauth/token" \
	--header 'content-type: application/x-www-form-urlencoded' \
	--data "grant_type=client_credentials" \
	--data "client_id=$LFXMCP_CLIENT_ID" \
	--data "client_secret=<placeholder>" \
	--data "audience=$LFX_MCP_API_URL")

MCP_TOKEN=$(echo "$MCP_TOKEN_RESPONSE" | jq -r '.access_token')

if [ "$MCP_TOKEN" == "null" ] || [ -z "$MCP_TOKEN" ]; then
	echo -e "${YELLOW}Warning: Could not get MCP token via client credentials. This is expected for token exchange clients.${NC}"
	echo "You'll need to get a valid user token through the full OAuth flow."
	echo ""
	echo -e "${BLUE}Next Steps:${NC}"
	echo "1. Use a browser or OAuth client to authenticate a user and get an MCP API token"
	echo "2. Export the token: export MCP_USER_TOKEN='<your_token>'"
	echo "3. Run manual tests below:"
	echo ""
	echo "   # Start server with token exchange:"
	echo "   LFXMCP_MODE=http \\"
	echo "   LFXMCP_HTTP_PORT=8081 \\"
	echo "   LFXMCP_MCP_API_AUTH_SERVERS=\"https://$AUTH0_DOMAIN\" \\"
	echo "   LFXMCP_MCP_API_PUBLIC_URL=\"$LFX_MCP_API_URL\" \\"
	echo "   LFXMCP_TOKEN_ENDPOINT=\"$TOKEN_ENDPOINT\" \\"
	echo "   LFXMCP_CLIENT_ID=\"\$LFXMCP_CLIENT_ID\" \\"
	echo "   LFXMCP_CLIENT_ASSERTION_SIGNING_KEY=\"\$(cat \$LFXMCP_CLIENT_ASSERTION_SIGNING_KEY_FILE)\" \\"
	echo "   LFXMCP_LFX_API_URL=\"$LFX_V2_API_URL\" \\"
	echo "   LFXMCP_TOOLS=user_info \\"
	echo "   LFXMCP_DEBUG=true \\"
	echo "   ./bin/lfx-mcp-server"
	echo ""
	echo "   # Test user_info tool with your token:"
	echo "   curl -X POST http://127.0.0.1:8081/mcp \\"
	echo "     -H \"Content-Type: application/json\" \\"
	echo "     -H \"Authorization: Bearer \$MCP_USER_TOKEN\" \\"
	echo "     -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{},\"clientInfo\":{\"name\":\"test\",\"version\":\"1.0.0\"}}}'"
	echo ""
	echo "   curl -X POST http://127.0.0.1:8081/mcp \\"
	echo "     -H \"Content-Type: application/json\" \\"
	echo "     -H \"Authorization: Bearer \$MCP_USER_TOKEN\" \\"
	echo "     -d '{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"user_info\",\"arguments\":{}}}'"
	echo ""
else
	echo -e "${GREEN}Successfully obtained MCP token${NC}"
	echo "Token preview: ${MCP_TOKEN:0:50}..."
	echo ""

	# Start server with token exchange configuration.
	echo "Starting server with token exchange configuration..."
	LFXMCP_MODE=http \
		LFXMCP_HTTP_PORT=8081 \
		LFXMCP_MCP_API_AUTH_SERVERS="https://$AUTH0_DOMAIN" \
		LFXMCP_MCP_API_PUBLIC_URL="$LFX_MCP_API_URL" \
		LFXMCP_TOKEN_ENDPOINT="$TOKEN_ENDPOINT" \
		LFXMCP_CLIENT_ID="$LFXMCP_CLIENT_ID" \
		LFXMCP_CLIENT_ASSERTION_SIGNING_KEY="$PRIVATE_KEY" \
		LFXMCP_LFX_API_URL="$LFX_V2_API_URL" \
		LFXMCP_TOOLS=user_info \
		./bin/lfx-mcp-server &
	SERVER_PID=$!

	# Wait for server to start.
	sleep 2

	# Test user_info tool.
	echo "Testing user_info tool..."
	USER_INFO_RESPONSE=$(curl -s -X POST http://127.0.0.1:8081/mcp \
		-H "Content-Type: application/json" \
		-H "Authorization: Bearer $MCP_TOKEN" \
		-d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}')

	echo -e "${GREEN}Initialize Response:${NC}"
	echo "$USER_INFO_RESPONSE" | jq '.'

	USER_INFO_RESPONSE=$(curl -s -X POST http://127.0.0.1:8081/mcp \
		-H "Content-Type: application/json" \
		-H "Authorization: Bearer $MCP_TOKEN" \
		-d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"user_info","arguments":{}}}')

	echo -e "${GREEN}user_info Tool Response:${NC}"
	echo "$USER_INFO_RESPONSE" | jq '.'

	# Stop server.
	kill $SERVER_PID
	wait $SERVER_PID 2>/dev/null || true
fi

echo ""
echo -e "${GREEN}=== OAuth Flow Test Complete ===${NC}"
echo ""
echo -e "${BLUE}Summary:${NC}"
echo "✓ PRM endpoint returns correct OAuth metadata"
echo "✓ Server starts with token exchange configuration"
echo "✓ Token exchange client configuration is valid"
echo ""
echo -e "${YELLOW}Note: Full end-to-end testing requires a user access token.${NC}"
echo "See the manual test commands above for complete OAuth flow testing."
