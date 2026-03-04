# LFX MCP Server

A Model Context Protocol (MCP) server for the Linux Foundation's LFX platform, providing tools and resources for interacting with LFX services.

## Overview

This project implements an MCP server that exposes LFX platform functionality through standardized tools and resources. It's built using the [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) and follows the Model Context Protocol specification.

## Features

- **Hello World Tool**: A simple demonstration tool for testing MCP connectivity
- **Stdio Transport**: Communication via standard input/output streams
- **HTTP Transport**: Streamable HTTP endpoint for web-based MCP clients
- **Extensible Architecture**: Clean structure for adding new tools and resources

## Quick Start

### Prerequisites

- Go 1.26.0 or later
- Git

### Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/linuxfoundation/lfx-mcp.git
   cd lfx-mcp
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Build the server:
   ```bash
   go build -o bin/lfx-mcp-server ./cmd/lfx-mcp-server
   ```

### Running the Server

#### Stdio Transport (Default)

To start the MCP server with stdio transport (default behavior):

```bash
./bin/lfx-mcp-server
```

The server will listen for MCP messages on stdin and respond on stdout.

#### HTTP Transport

To start the MCP server with HTTP transport:

```bash
./bin/lfx-mcp-server -mode=http
```

The server will start an HTTP endpoint at `http://localhost:8080/mcp` that accepts MCP requests over streamable HTTP (Server-Sent Events). This is useful for web-based MCP clients.

### Configuration

The server supports configuration via both command-line flags and environment variables. Environment variables **override** command-line flags, allowing flags to provide defaults while environment variables can override them in containerized deployments.

**Command-line Flags:**
- `-mode`: Transport mode: `stdio` or `http` (default: `stdio`)
- `-http.port`: Port to listen on for HTTP transport (default: `8080`)
- `-http.host`: Host to bind to for HTTP transport (default: `127.0.0.1`)
- `-http.public_url`: Public URL for HTTP transport (for reverse proxies, e.g., `https://example.com/mcp`)
- `-debug`: Enable debug logging with source location tracking (default: `false`)
- `-tools`: Comma-separated list of tools to enable (default: none)
- `-oauth.domain`: Issuer domain for IdP
- `-oauth.resource_url`: LFX API domain for OAuth audience
- `-oauth.scopes`: OAuth scopes as comma-separated list (default: `openid,profile`)
- `-token_exchange.token_endpoint`: OAuth2 token endpoint URL for RFC 8693 token exchange (e.g., `https://example.auth0.com/oauth/token`)
- `-token_exchange.client_id`: M2M client ID for token exchange
- `-token_exchange.client_secret`: M2M client secret for token exchange (ignored if `client_assertion_signing_key` is set)
- `-token_exchange.client_assertion_signing_key`: PEM-encoded RSA private key for client assertion (RFC 7523). Takes precedence over `client_secret` if both are provided
- `-token_exchange.subject_token_type`: Subject token type for RFC 8693 (e.g., LFX MCP API identifier)
- `-token_exchange.audience`: Target audience for exchanged token (e.g., LFX V2 API identifier)

**Environment Variables:**

All environment variables use the `LFXMCP_` prefix. Variable names use underscores, which are automatically transformed to dots for nested configuration keys (e.g., `LFXMCP_HTTP_PORT` becomes `http.port`):
- `LFXMCP_MODE`: Transport mode (`stdio` or `http`)
- `LFXMCP_HTTP_HOST`: HTTP server host
- `LFXMCP_HTTP_PORT`: HTTP server port
- `LFXMCP_DEBUG`: Enable debug logging (`true` or `false`)
- `LFXMCP_TOOLS`: Comma-separated list of tools to enable
- `LFXMCP_MCP_API_AUTH_SERVERS`: Comma-separated list of authorization server URLs
- `LFXMCP_MCP_API_PUBLIC_URL`: Public URL for MCP API (for OAuth PRM)
- `LFXMCP_MCP_API_SCOPES`: OAuth scopes as comma-separated list (default: `openid,profile`)
- `LFXMCP_CLIENT_ID`: OAuth client ID for authentication
- `LFXMCP_CLIENT_SECRET`: OAuth client secret
- `LFXMCP_CLIENT_ASSERTION_SIGNING_KEY`: PEM-encoded RSA private key for client assertion
- `LFXMCP_TOKEN_ENDPOINT`: OAuth2 token endpoint URL for token exchange
- `LFXMCP_LFX_API_URL`: LFX API URL (used as token exchange audience)

**Examples:**

With command-line flags:
```bash
# Custom HTTP port
./bin/lfx-mcp-server -mode=http -http.port=9090

# Bind to all interfaces
./bin/lfx-mcp-server -mode=http -http.host=0.0.0.0 -http.port=8080
```

With environment variables:
```bash
# Start in HTTP mode on custom port
LFXMCP_MODE=http LFXMCP_HTTP_PORT=9090 ./bin/lfx-mcp-server

# Enable debug logging (env var overrides flag)
LFXMCP_DEBUG=true ./bin/lfx-mcp-server

# Environment variable overrides flag
LFXMCP_HTTP_PORT=9090 ./bin/lfx-mcp-server -mode=http -http.port=8080
# Result: Server runs on port 9090 (env var wins)
```

### Logging

The server uses structured JSON logging via Go's `slog` package. All logs are written to stdout in JSON format for easy parsing and integration with log aggregation systems.

**Log Levels:**
- `INFO` (default): Standard operational messages
- `DEBUG`: Detailed diagnostic information with source location tracking

**Enabling Debug Logging:**

Debug logging can be enabled via command-line flag or environment variable:

```bash
# Via command-line flag
./bin/lfx-mcp-server -debug

# Via environment variable
LFXMCP_DEBUG=true ./bin/lfx-mcp-server
```

When debug logging is enabled, the following additional information is included:
- Source file locations for each log statement
- Detailed request/response information
- Internal state transitions

**Log Format:**

All logs are emitted as JSON objects with the following structure:

```json
{"time":"2024-01-15T10:30:45.123Z","level":"INFO","msg":"Starting HTTP server","addr":"127.0.0.1:8080"}
{"time":"2024-01-15T10:30:45.456Z","level":"ERROR","msg":"server failed","error":"connection refused"}
```

Debug logs include source information:

```json
{"time":"2024-01-15T10:30:45.789Z","level":"DEBUG","source":{"file":"main.go","line":150},"msg":"processing request"}
```

**Example HTTP request:**
```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"web-client","version":"1.0.0"}}}'
```

The HTTP transport uses stateless mode, creating a new MCP session for each request. This allows horizontal scaling without session management.

## Available Tools

### hello_world

A simple greeting tool that demonstrates the MCP tool interface.

**Parameters:**
- `name` (string, optional): Name to greet (defaults to "World")
- `message` (string, optional): Custom greeting message

**Example usage:**
```json
{
  "method": "tools/call",
  "params": {
    "name": "hello_world",
    "arguments": {
      "name": "LFX User",
      "message": "Welcome"
    }
  }
}
```

## Development

### Project Structure

```text
lfx-mcp/
├── cmd/
│   └── lfx-mcp-server/     # Main application entry point
├── internal/
│   └── tools/              # MCP tool implementations
├── bin/                    # Built binaries (created by make build)
├── go.mod                  # Go module definition
├── Makefile               # Build automation
└── README.md              # This file
```

### Adding New Tools

Tools are implemented in the `internal/tools` package for better organization and scalability.

1. Create a new file in `internal/tools/` (e.g., `my_tool.go`):
   ```go
   package tools

   import (
       "context"
       "github.com/modelcontextprotocol/go-sdk/mcp"
   )

   // MyToolArgs defines the input parameters.
   type MyToolArgs struct {
       Param1 string `json:"param1" jsonschema:"Description of parameter 1"`
       Param2 int    `json:"param2,omitempty" jsonschema:"Optional parameter 2"`
   }

   // RegisterMyTool registers the tool with the MCP server.
   func RegisterMyTool(server *mcp.Server) {
       mcp.AddTool(server, &mcp.Tool{
           Name:        "my_tool",
           Description: "Description of what the tool does",
       }, handleMyTool)
   }

   // handleMyTool implements the tool logic.
   func handleMyTool(ctx context.Context, req *mcp.CallToolRequest, args MyToolArgs) (*mcp.CallToolResult, any, error) {
       // Your tool implementation here
       return &mcp.CallToolResult{
           Content: []mcp.Content{
               &mcp.TextContent{Text: "Tool result"},
           },
       }, nil, nil
   }
   ```

2. Register your tool in `cmd/lfx-mcp-server/main.go`:
   ```go
   // Register tools.
   tools.RegisterHelloWorld(server)
   tools.RegisterMyTool(server)  // Add your new tool
   ```

### Testing

#### Testing Stdio Transport

To test the server manually, you can send JSON-RPC messages via stdio:

```bash
# Test server initialization and tool listing
(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}'; 
 echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'; 
 sleep 1) | ./bin/lfx-mcp-server

# With debug logging enabled
(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}'; 
 echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'; 
 sleep 1) | ./bin/lfx-mcp-server -debug

# Test calling the hello_world tool
(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}'; 
 echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hello_world","arguments":{"name":"LFX User","message":"Welcome"}}}'; 
 sleep 1) | ./bin/lfx-mcp-server
```

#### Testing HTTP Transport

Start the HTTP server and send requests using curl:

```bash
# Start the server in the background
./bin/lfx-mcp-server -mode=http -http.port=8080 &

# Or with debug logging
./bin/lfx-mcp-server -mode=http -http.port=8080 -debug &

# Test tools list
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'

# Test calling hello_world tool
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hello_world","arguments":{"name":"LFX","message":"Welcome"}}}'
```

Responses are returned as Server-Sent Events (SSE) with `event: message` and `data:` fields.

## License

Copyright The Linux Foundation and each contributor to LFX.

This project’s source code is licensed under the MIT License. A copy of the
license is available in LICENSE.

This project’s documentation is licensed under the Creative Commons Attribution
4.0 International License \(CC-BY-4.0\). A copy of the license is available in
LICENSE-docs.
