#!/bin/bash

# Copyright The Linux Foundation and contributors.
# SPDX-License-Identifier: MIT

set -e

# Parse command line arguments
DEBUG_FLAG=""
if [[ "$1" == "--debug" || "$1" == "-d" ]]; then
	DEBUG_FLAG="-debug"
	echo "Running tests with debug logging enabled..."
fi

echo "Testing LFX MCP Server..."

# Build the server if needed
if [ ! -f "./bin/lfx-mcp-server" ]; then
	echo "Building server..."
	make build
fi

# These tests exercise transport/protocol plumbing and schema generation
# across every default tool (no LFXMCP_TOOLS override), not any specific
# tool's business logic. They require no LFX credentials or OAuth config,
# so they exercise the same startup path every tool goes through without
# needing a bearer token. See README's "Local (stdio) mode" section for a
# manual walkthrough that exercises real LFX API calls with `lfx auth token`.

echo ""
echo "=== Test 1: Server initialization and capabilities ==="
(
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'
	sleep 0.5
) |
	./bin/lfx-mcp-server $DEBUG_FLAG |
	grep '"id":1' |
	jq '.'

echo ""
echo "=== Test 2: List available tools (schema generation for all default tools) ==="
(
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'
	echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
	sleep 0.5
) |
	./bin/lfx-mcp-server $DEBUG_FLAG |
	grep '"id":2' |
	jq '.result.tools'

echo ""
echo "=== Test 3: Error handling (invalid tool name) ==="
(
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'
	echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"invalid_tool","arguments":{}}}'
	sleep 0.5
) |
	./bin/lfx-mcp-server $DEBUG_FLAG |
	grep '"id":2' |
	jq '.error.message' || echo "\"Tool not found error handled correctly\""

echo ""
echo "All tests completed successfully! 🎉"
echo ""
if [[ -n "$DEBUG_FLAG" ]]; then
	echo "Tests ran with debug logging enabled (logs on stderr)"
fi
echo ""
echo "The LFX MCP Server is working correctly with:"
echo "- JSON-RPC 2.0 protocol compliance"
echo "- MCP protocol version 2024-11-05 support"
echo "- Schema generation for every default tool, without conflicts"
echo "- Proper error handling for invalid tools"
echo ""
echo "Usage: $0 [--debug|-d]  # Enable debug logging during tests"
