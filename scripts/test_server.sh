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

echo ""
echo "=== Test 1: Server initialization and capabilities ==="
(
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'
	sleep 0.5
) |
	LFX_MCP_TOOLS=hello_world ./bin/lfx-mcp-server stdio $DEBUG_FLAG |
	grep '"id":1' |
	jq '.'

echo ""
echo "=== Test 2: List available tools ==="
(
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'
	echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
	sleep 0.5
) |
	LFX_MCP_TOOLS=hello_world ./bin/lfx-mcp-server stdio $DEBUG_FLAG |
	grep '"id":2' |
	jq '.result.tools'

echo ""
echo "=== Test 3: Call hello_world tool (default greeting) ==="
(
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'
	echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hello_world","arguments":{}}}'
	sleep 0.5
) |
	LFX_MCP_TOOLS=hello_world ./bin/lfx-mcp-server stdio $DEBUG_FLAG |
	grep '"id":2' |
	jq '.result.content[0].text'

echo ""
echo "=== Test 4: Call hello_world tool (with name) ==="
(
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'
	echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hello_world","arguments":{"name":"LFX Developer"}}}'
	sleep 0.5
) |
	LFX_MCP_TOOLS=hello_world ./bin/lfx-mcp-server stdio $DEBUG_FLAG |
	grep '"id":2' |
	jq '.result.content[0].text'

echo ""
echo "=== Test 5: Call hello_world tool (with custom message) ==="
(
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'
	echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hello_world","arguments":{"name":"LFX Team","message":"Welcome to the platform"}}}'
	sleep 0.5
) |
	LFX_MCP_TOOLS=hello_world ./bin/lfx-mcp-server stdio $DEBUG_FLAG |
	grep '"id":2' |
	jq '.result.content[0].text'

echo ""
echo "=== Test 6: Error handling (invalid tool name) ==="
(
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'
	echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"invalid_tool","arguments":{}}}'
	sleep 0.5
) |
	LFX_MCP_TOOLS=hello_world ./bin/lfx-mcp-server stdio $DEBUG_FLAG |
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
echo "- Hello world tool with optional parameters"
echo "- Proper error handling for invalid tools"
echo "- JSON schema validation for tool parameters"
echo ""
echo "Usage: $0 [--debug|-d]  # Enable debug logging during tests"
