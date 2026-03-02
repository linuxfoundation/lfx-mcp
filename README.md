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

The server supports configuration via both command-line flags and environment variables. Command-line flags take precedence over environment variables.

**Command-line Flags:**
- `-mode`: Transport mode: `stdio` or `http` (default: `stdio`)
- `-http.port`: Port to listen on for HTTP transport (default: `8080`)
- `-http.host`: Host to bind to for HTTP transport (default: `127.0.0.1`)
- `-debug`: Enable debug logging with source location tracking (default: `false`)
- `-tools`: Comma-separated list of tools to enable (default: none)
- `-oauth.domain`: Issuer domain for IdP
- `-oauth.resource_url`: LFX API domain for OAuth audience
- `-oauth.scopes`: OAuth scopes as comma-separated list (default: `openid,profile`)

**Environment Variables:**

All environment variables use the `LFX_MCP_` prefix with underscore separators converted to dots:
- `LFX_MCP_MODE`: Transport mode (`stdio` or `http`)
- `LFX_MCP_HTTP_PORT`: HTTP server port
- `LFX_MCP_HTTP_HOST`: HTTP server host
- `LFX_MCP_DEBUG`: Enable debug logging (`true` or `false`)
- `LFX_MCP_TOOLS`: Comma-separated list of tools to enable
- `LFX_MCP_OAUTH_DOMAIN`: Issuer domain for IdP
- `LFX_MCP_OAUTH_RESOURCE_URL`: LFX API domain
- `LFX_MCP_OAUTH_SCOPES`: OAuth scopes as comma-separated list (default: `openid,profile`)

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
LFX_MCP_MODE=http LFX_MCP_HTTP_PORT=9090 ./bin/lfx-mcp-server

# Enable debug logging
LFX_MCP_DEBUG=true ./bin/lfx-mcp-server -mode=stdio

# Override environment with command-line flag
LFX_MCP_HTTP_PORT=9090 ./bin/lfx-mcp-server -mode=http -http.port=8888
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
LFX_MCP_DEBUG=true ./bin/lfx-mcp-server
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
