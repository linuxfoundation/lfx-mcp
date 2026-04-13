# LFX MCP Server — Developer Guide

This guide covers building, running, testing, and extending the LFX MCP Server. For end-user integration instructions, see [README.md](README.md).

## Prerequisites

- Go 1.26.0 or later

## Build

```bash
make build
# Or directly:
go build -ldflags="-s -w" -o bin/lfx-mcp-server ./cmd/lfx-mcp-server
```

## Running Locally

> **Note:** The stdio transport only exposes the `hello_world` tool. All LFX data tools require OAuth authentication, which is only supported via the HTTP transport. There is currently no personal access token (PAT) capability in LFX, so running the full server locally for end-to-end use is not practical without a complete OAuth setup.

**Stdio transport:**

```bash
./bin/lfx-mcp-server
# With debug logging:
./bin/lfx-mcp-server -debug
```

**HTTP transport:**

```bash
./bin/lfx-mcp-server -mode=http
# With debug logging:
./bin/lfx-mcp-server -mode=http -debug
```

The HTTP server listens at `http://localhost:8080/mcp` by default.

## Configuration

Environment variables use the `LFXMCP_` prefix and **override** their corresponding flags.

| Flag                            | Env Var                               | Default     | Description                                                 |
|---------------------------------|---------------------------------------|-------------|-------------------------------------------------------------|
| `-mode`                         | `LFXMCP_MODE`                         | `stdio`     | Transport mode: `stdio` or `http`                           |
| `-http.host`                    | `LFXMCP_HTTP_HOST`                    | `127.0.0.1` | HTTP server bind address                                    |
| `-http.port`                    | `LFXMCP_HTTP_PORT`                    | `8080`      | HTTP server port                                            |
| `-http.public_url`              | `LFXMCP_HTTP_PUBLIC_URL`              | —           | Public URL for HTTP transport (reverse proxies)             |
| `-debug`                        | `LFXMCP_DEBUG`                        | `false`     | Enable debug logging with source locations                  |
| `-debug_traffic`                | `LFXMCP_DEBUG_TRAFFIC`                | `false`     | Log outbound LFX API request/response bodies                |
| `-tools`                        | `LFXMCP_TOOLS`                        | —           | Comma-separated list of tools to enable                     |
| `-mcp_api.auth_servers`         | `LFXMCP_MCP_API_AUTH_SERVERS`         | —           | OAuth authorization server URLs (comma-separated)           |
| `-mcp_api.public_url`           | `LFXMCP_MCP_API_PUBLIC_URL`           | —           | Public URL for MCP API (OAuth PRM)                          |
| `-mcp_api.scopes`               | `LFXMCP_MCP_API_SCOPES`               | —           | OAuth scopes (comma-separated)                              |
| `-client_id`                    | `LFXMCP_CLIENT_ID`                    | —           | OAuth client ID for token exchange                          |
| `-client_secret`                | `LFXMCP_CLIENT_SECRET`                | —           | OAuth client secret                                         |
| `-client_assertion_signing_key` | `LFXMCP_CLIENT_ASSERTION_SIGNING_KEY` | —           | PEM-encoded RSA private key for client assertion (RFC 7523) |
| `-token_endpoint`               | `LFXMCP_TOKEN_ENDPOINT`               | —           | OAuth2 token endpoint URL (RFC 8693)                        |
| `-lfx_api_url`                  | `LFXMCP_LFX_API_URL`                  | —           | LFX API base URL (token exchange audience)                  |

## Code Quality

```bash
make fmt     # Format code.
make vet     # Run go vet.
make lint    # Run golangci-lint (install if needed).
make check   # Run fmt, vet, and lint together.
```

## Testing

### Automated tests

```bash
make test           # Run all Go tests.
make test-coverage  # Run tests with coverage report.
```

### Integration test script

```bash
./scripts/test_server.sh          # Run integration tests.
./scripts/test_server.sh --debug  # With debug logging.
```

### Manual stdio testing

Send raw JSON-RPC messages to the server:

```bash
# List available tools.
(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}';
 echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}';
 sleep 0.5) | ./bin/lfx-mcp-server stdio

# Call the hello_world tool.
(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}';
 echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hello_world","arguments":{"name":"LFX User"}}}';
 sleep 0.5) | ./bin/lfx-mcp-server stdio
```

### Manual HTTP testing

```bash
# Start the server.
./bin/lfx-mcp-server -mode=http &

# List tools.
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

Responses are returned as Server-Sent Events (SSE) with `event: message` and `data:` fields.

## Logging

The server uses structured JSON logging via Go's `slog` package. Logs go to stderr in stdio mode, or stdout in HTTP mode.

Enable debug logging via flag or environment variable:

```bash
./bin/lfx-mcp-server -debug
# Or:
LFXMCP_DEBUG=true ./bin/lfx-mcp-server
```

To log full request/response bodies for outbound LFX API calls:

```bash
./bin/lfx-mcp-server -debug_traffic
```

## Project Structure

```text
lfx-mcp/
├── cmd/
│   └── lfx-mcp-server/     # Main application entry point
├── internal/
│   └── tools/              # MCP tool implementations
├── bin/                    # Built binaries (created by make build)
├── charts/                 # Helm chart for Kubernetes deployment
├── go.mod                  # Go module definition
├── Makefile                # Build automation
├── README.md               # End-user documentation
├── DEVELOPER.md            # This file
└── AGENTS.md               # AI agent / architecture guide
```

## Adding New Tools

Tools live in `internal/tools/`, one file per tool. Each file defines an args struct, a handler function, and a `Register<ToolName>` function.

1. **Create** `internal/tools/my_tool.go`:

```go
// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools contains MCP tool implementations.
package tools

import (
    "context"
    "fmt"

    "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MyToolArgs defines the input parameters for the my_tool tool.
type MyToolArgs struct {
    Param1 string `json:"param1" jsonschema:"Description of parameter 1"`
    Param2 int    `json:"param2,omitempty" jsonschema:"Optional parameter 2"`
}

// RegisterMyTool registers the my_tool tool with the MCP server.
func RegisterMyTool(server *mcp.Server) {
    mcp.AddTool(server, &mcp.Tool{
        Name:        "my_tool",
        Description: "Brief description of what the tool does.",
        Annotations: &mcp.ToolAnnotations{
            Title:        "My Tool",
            ReadOnlyHint: true,
        },
    }, handleMyTool)
}

// handleMyTool implements the my_tool tool logic.
func handleMyTool(ctx context.Context, req *mcp.CallToolRequest, args MyToolArgs) (*mcp.CallToolResult, any, error) {
    result := fmt.Sprintf("Processed: %s with value %d", args.Param1, args.Param2)

    return &mcp.CallToolResult{
        Content: []mcp.Content{
            &mcp.TextContent{Text: result},
        },
    }, nil, nil
}
```

2. **Register** the tool in `cmd/lfx-mcp-server/main.go`:

```go
tools.RegisterMyTool(server)
```

For more detail on tool annotations, JSON schema tags, content types, and the MCP Go SDK patterns used throughout this codebase, see [AGENTS.md](AGENTS.md).

## Build System

| Target          | Description                       |
|-----------------|-----------------------------------|
| `all`           | Clean, check, and build (default) |
| `build`         | Compile the binary                |
| `clean`         | Remove build artifacts            |
| `fmt`           | Format Go code                    |
| `vet`           | Run go vet                        |
| `lint`          | Run golangci-lint                 |
| `check`         | Run fmt, vet, and lint            |
| `run`           | Build and run in stdio mode       |
| `test`          | Run Go tests                      |
| `test-coverage` | Run tests with coverage           |
| `deps`          | Download and tidy dependencies    |
| `install-tools` | Install development tools         |