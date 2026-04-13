# AGENTS.md

> **Note:** `CLAUDE.md` is a symlink to this file. Only `AGENTS.md` needs to be edited; changes are automatically reflected in `CLAUDE.md`.

This file provides essential information for AI agents working on the LFX MCP Server codebase. It focuses on development workflows, architecture understanding, and build processes needed for making code changes.

## Repository Overview

The LFX MCP Server is a Model Context Protocol (MCP) implementation that provides tools and resources for interacting with the Linux Foundation's LFX platform. It's built using the official Go SDK for MCP and follows a clean, extensible architecture.

### Key Technologies

- **Language**: Go 1.26.0+
- **Protocol**: Model Context Protocol (MCP) 2024-11-05
- **SDK**: Official MCP Go SDK v1.5.0+
- **Transport**: JSON-RPC 2.0 over stdio and Streamable HTTP
- **Schema**: Automatic JSON schema generation via struct tags

## Architecture Overview

The service follows a simple, clean architecture pattern optimized for MCP tool development:

```text
lfx-mcp/
├── cmd/
│   └── lfx-mcp-server/     # Main application entry point
├── internal/
│   └── tools/              # MCP tool implementations
├── scripts/                # Test and utility scripts
├── bin/                    # Built binaries (gitignored)
├── go.mod                  # Go module definition
├── Makefile               # Build automation
├── README.md              # User documentation
└── AGENTS.md              # This file (AI agent guidelines)
```

### Data Flow
```text
Client (Claude, etc.) → JSON-RPC 2.0 → stdio transport → MCP Server → Tool Handler → Response
```

### Key Design Principles

1. **Simplicity**: Minimal abstraction layers using the official MCP Go SDK
2. **Extensibility**: Easy to add new tools through the `AddToolWithScopes` pattern
3. **Type Safety**: Strong typing with automatic schema generation
4. **Testability**: Simple stdio testing via JSON-RPC messages
5. **Observability**: Structured JSON logging with optional debug mode

### Multi-Pod Scaling (Stateless Architecture)

The HTTP server is designed to run across multiple pods without coordination:

- **`Stateless: true`** is set on `StreamableHTTPHandler` (`main.go`). This instructs the SDK to skip session-ID validation and use a temporary session per request, so any pod can handle any request.
- **Per-request server factory**: `newServer()` is called for each incoming HTTP request, so no MCP-level state accumulates across requests.
- **`SchemaCache`**: A package-level `schemaCache` is shared across per-request server instances. This avoids re-running reflection-based JSON schema generation for every request — schemas are computed on first use and then reused across subsequent requests in the same pod.
- **Streamable HTTP vs. old SSE transport**: Responses may use SSE framing (`text/event-stream`) within a single request/response cycle. This is not the deprecated long-lived SSE transport, so no connection affinity is needed between requests.
- **No sticky sessions required**: Because all state is either per-request or independently cached per pod, round-robin load balancing works without Kubernetes session affinity.
- **In-memory caches are all safe**: Token exchange, slug resolver, client credentials, and JWKS caches are performance-only; each pod warms independently and misses trigger upstream re-fetches.

**Stateless mode limitation**: The server cannot make client callbacks (e.g., `ListRoots`, `CreateMessage`, `Elicit`) in stateless mode. We do not use any of these currently. If sampling or elicitation features are ever needed, stateless mode would need to be reconsidered.

**Reference**: [SDK distributed example](https://github.com/modelcontextprotocol/go-sdk/blob/v1.5.0/examples/server/distributed/main.go) — uses the same `Stateless: true` + per-request factory + round-robin pattern.

## Development Workflow

### Prerequisites

```bash
# Ensure Go 1.26.0+ is installed
go version  # Should show go version go1.26.0 or later
```

### Common Development Tasks

#### 1. Build the Server

```bash
make build
# or directly: go build -ldflags="-s -w" -o bin/lfx-mcp-server ./cmd/lfx-mcp-server
```

#### 2. Run Tests

```bash
# Integration tests
./scripts/test_server.sh

# Integration tests with debug logging
./scripts/test_server.sh --debug

# Manual testing
make run  # Starts server in stdio mode

# Manual testing with debug logging
./bin/lfx-mcp-server -debug
```

#### 3. Code Quality Checks

```bash
make fmt     # Format code
make vet     # Run go vet
make lint    # Run golangci-lint (if installed)
make check   # Run all checks
```

#### 4. Clean Build Artifacts

```bash
make clean
```

## Logging

The server has two separate logging systems:

### 1. Server-Side Logging (for operators)

Uses Go's standard `slog` package for operational logs. These logs are written to stdout (HTTP mode) or stderr (stdio mode).

**Configuration:**

- **Format**: JSON (always)
- **Output**: stdout (HTTP mode) or stderr (stdio mode)
- **Default Level**: INFO
- **Debug Mode**: Enabled via `-debug` flag or `LFXMCP_DEBUG=true` environment variable

**Enable debug logging:**

```bash
# Via command-line flag
./bin/lfx-mcp-server -debug

# Via environment variable
LFXMCP_DEBUG=true ./bin/lfx-mcp-server

# Both work in HTTP mode too
./bin/lfx-mcp-server -mode=http -debug
```

### 2. MCP Client Logging (for tool developers)

Tools can send logs to the MCP client using `mcp.NewLoggingHandler`. These logs appear in the client's UI (e.g., Claude Desktop logs) and are controlled by the client's log level.

**Usage in tools:**

```go
func handleMyTool(ctx context.Context, req *mcp.CallToolRequest, args MyToolArgs) (*mcp.CallToolResult, any, error) {
    // Create MCP logger that sends logs to the client.
    logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))
    
    logger.Info("processing started", "param", args.Param)
    logger.Debug("detailed info", "value", someValue)
    logger.Warn("potential issue", "reason", "something unexpected")
    
    // ... tool implementation ...
}
```

**How it works:**

- The **client** controls the log level via the `SetLoggingLevel` MCP notification
- Only logs at or above the client's level are sent over the protocol
- Logs appear in the client's logging UI (not in server logs)
- Log levels: debug, info, notice, warning, error, critical, alert, emergency

**Key differences:**

| Feature  | Server Logging   | MCP Client Logging         |
|----------|------------------|----------------------------|
| Audience | Server operators | Client users/developers    |
| Output   | stdout/stderr    | MCP protocol notifications |
| Control  | `-debug` flag    | Client's `SetLoggingLevel` |
| Format   | JSON to files    | JSON over protocol         |
| Use case | Debugging server | Debugging tool execution   |

### Server Log Structure

Server-side logs are emitted as JSON objects:

```json
{"time":"2024-01-15T10:30:45.123Z","level":"INFO","msg":"Starting HTTP server","addr":"127.0.0.1:8080"}
{"time":"2024-01-15T10:30:45.456Z","level":"ERROR","msg":"server failed","error":"connection refused"}
```

With debug logging enabled, source information is included:

```json
{"time":"2024-01-15T10:30:45.789Z","level":"DEBUG","source":{"file":"main.go","line":150},"msg":"processing request"}
```

### Using Server Logger

The server logger is initialized in `main.go` and set as the default slog logger. Use it for operational logging:

```go
import "log/slog"

// Info level
slog.Info("operation completed", "key", "value")

// Error level with structured fields
slog.Error("operation failed", "error", err, "context", "additional info")

// Debug level (only shown when debug mode is enabled)
slog.Debug("detailed diagnostic", "request_id", reqID)

// Using logger with context
logger.With("component", "tool_handler").Info("processing tool call")
```

### Error Logging Convention

```go
const errKey = "error"

// Server-side error logging
logger.With(errKey, err).Error("operation failed")

// MCP client error logging (in tools)
mcpLogger.Error("tool operation failed", "error", err)
```

**Recommendation**: Use MCP client logging in tools for visibility to end users, and server-side logging for operational concerns.

## Adding New Tools

The MCP Go SDK provides a simple pattern for adding tools. Tools are implemented in the `internal/tools` package and registered with the server. Every tool **must** be registered via `AddToolWithScopes` (defined in `internal/tools/scopes.go`) so that scope enforcement is applied automatically in HTTP mode. In stdio mode (no auth), the scope check is skipped.

### Scope Enforcement

Two scope constants are defined in `internal/tools/scopes.go`:

| Constant      | Value        | Used for                                            |
|---------------|--------------|-----------------------------------------------------|
| `ScopeRead`   | `read:all`   | Tools with `ReadOnlyHint: true`                     |
| `ScopeManage` | `manage:all` | Tools where `ReadOnlyHint` is `false` (the default) |

Use the helper functions `ReadScopes()` and `WriteScopes()` when registering tools.

When a caller's JWT token lacks the required scope, the tool returns a structured `IsError` result (not a JSON-RPC error), keeping the failure inside the MCP tool-call protocol.

### Tool Implementation Steps

1. **Create a new file** in `internal/tools/` (e.g., `my_tool.go`)
2. **Define the input struct** with JSON schema tags
3. **Implement the handler function** with tool logic
4. **Create a registration function** using `AddToolWithScopes` with the correct scope
5. **Call the registration function** in `main.go`

### Example Tool Implementation

**File: `internal/tools/my_tool.go`** (read-only tool)

```go
// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

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
    AddToolWithScopes(server, &mcp.Tool{
        Name:        "my_tool",
        Description: "Brief description of what the tool does",
        Annotations: &mcp.ToolAnnotations{
            Title:        "My Tool",
            ReadOnlyHint: true,
        },
    }, ReadScopes(), handleMyTool)
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

For a **write tool** (where `ReadOnlyHint` is `false` / unset), use `WriteScopes()` instead:

```go
func RegisterMyWriteTool(server *mcp.Server) {
    AddToolWithScopes(server, &mcp.Tool{
        Name:        "create_thing",
        Description: "Create a new thing",
        Annotations: &mcp.ToolAnnotations{
            Title:           "Create Thing",
            DestructiveHint: boolPtr(false),
        },
    }, WriteScopes(), handleCreateThing)
}
```

**Register in `cmd/lfx-mcp-server/main.go`:**

```go
import (
    "github.com/linuxfoundation/lfx-mcp/internal/tools"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

func runStdioServer() {
    // ... server setup ...
    
    // Register tools.
    tools.RegisterHelloWorld(server)
    tools.RegisterMyTool(server)  // Add your new tool
    
    // ... run server ...
}
```

### JSON Schema Tags

The MCP Go SDK uses `jsonschema` struct tags for automatic schema generation:

```go
type ToolArgs struct {
    Required   string  `json:"required" jsonschema:"This parameter is required"`
    Optional   *string `json:"optional,omitempty" jsonschema:"This parameter is optional"`
    Number     int     `json:"number" jsonschema:"A numeric parameter"`
    WithEnum   string  `json:"status" jsonschema:"enum=active,inactive,pending"`
}
```

### Content Types

MCP supports various content types in tool responses:

```go
// Text content
&mcp.TextContent{Text: "Plain text response"}

// Multiple content items
return &mcp.CallToolResult{
    Content: []mcp.Content{
        &mcp.TextContent{Text: "First part"},
        &mcp.TextContent{Text: "Second part"},
    },
}, nil, nil
```

### Tool Annotations

All tools should include a `mcp.ToolAnnotations` struct to provide metadata hints to MCP clients (e.g., Claude). Annotations help clients decide how to present tools and whether to confirm before calling them.

```go
boolPtr := func(v bool) *bool { return &v }

Annotations: &mcp.ToolAnnotations{
    Title:        "Human Readable Title",
    ReadOnlyHint: true,                // True if the tool makes no mutations.
    // DestructiveHint: boolPtr(false), // Set when ReadOnlyHint is false and the tool is non-destructive.
    // OpenWorldHint:  boolPtr(false),  // Override only for truly closed-world tools (see below).
},
```

**`ReadOnlyHint`** (bool, default `false`) is the most impactful annotation — clients use it to decide whether to auto-confirm tool calls. Set it to `true` for any tool that only reads data and has no side effects.

**`DestructiveHint`** (`*bool`, default `true`) is only meaningful when `ReadOnlyHint` is `false`. Set it to `false` for write tools that are additive or non-destructive (e.g., creating a new resource vs. deleting one).

**`OpenWorldHint`** (`*bool`, default `true`) signals whether the tool interacts with an external, stateful system. The key distinction is not whether you own the API — it's whether the environment is fully controlled and deterministic:

- **Set to `true` (or omit, since it's the default)** for any tool that calls an external API, including LFX's own services. Even for APIs we own, results can change between calls as data mutates on the server, network failures are possible, and write operations have real-world side effects. The tool doesn't fully control what it's reading or modifying, so the world is open.

- **Set to `false` only** for tools that are genuinely closed-world: pure in-process computations, static config lookups that never change, or in-memory operations with no network calls. These are the exception, not the rule.

Focus annotation effort on `ReadOnlyHint` and `DestructiveHint` — those have the most impact on client behavior. For `OpenWorldHint`, the default of `true` is correct for virtually all LFX API tools; only override it when you are certain the tool has zero external interaction.

## Testing Patterns

### Manual Testing via stdio

Test the server by sending JSON-RPC messages:

```bash
# Initialize and call tool
(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}';
 echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hello_world","arguments":{"name":"Test"}}}';
 sleep 0.5) | ./bin/lfx-mcp-server stdio
```

### Integration Test Script

The `scripts/test_server.sh` script provides comprehensive testing:

```bash
./scripts/test_server.sh
```

This tests:
- Server initialization
- Tool discovery (`tools/list`)
- Tool execution with various parameters
- Error handling

### Expected JSON-RPC Messages

#### Tool List Request/Response
```json
// Request
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}

// Response
{
  "jsonrpc":"2.0",
  "id":2,
  "result":{
    "tools":[
      {
        "name":"hello_world",
        "description":"A simple hello world tool...",
        "inputSchema":{
          "type":"object",
          "properties":{...}
        }
      }
    ]
  }
}
```

#### Tool Call Request/Response
```json
// Request
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"hello_world","arguments":{"name":"LFX"}}}

// Response
{
  "jsonrpc":"2.0",
  "id":3,
  "result":{
    "content":[
      {"type":"text","text":"Hello, LFX!"}
    ]
  }
}
```

## Build System (Makefile)

### Available Targets

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

### Build Flags

The Makefile uses optimized build flags:
- `-ldflags="-s -w"` for smaller binaries
- Output to `./bin/lfx-mcp-server`

## Environment Variables

The server supports configuration via environment variables with the `LFXMCP_` prefix. Environment variable names use underscores, which are automatically transformed to dots for nested configuration keys (e.g., `LFXMCP_HTTP_PORT` becomes `http.port`).

**Configuration Precedence:** Environment variables **override** command-line flags. This allows command-line flags to provide defaults while environment variables can override them in containerized deployments.

| Variable                              | Description                                                          | Default   | Required |
|---------------------------------------|----------------------------------------------------------------------|-----------|----------|
| `LFXMCP_MODE`                         | Transport mode (`stdio` or `http`)                                   | stdio     | No       |
| `LFXMCP_HTTP_HOST`                    | HTTP server host                                                     | 127.0.0.1 | No       |
| `LFXMCP_HTTP_PORT`                    | HTTP server port                                                     | 8080      | No       |
| `LFXMCP_HTTP_PUBLIC_URL`              | Public URL for HTTP transport                                        | -         | No       |
| `LFXMCP_DEBUG`                        | Enable debug logging                                                 | false     | No       |
| `LFXMCP_DEBUG_TRAFFIC`                | Enable HTTP request/response wire logging for outbound LFX API calls | false     | No       |
| `LFXMCP_TOOLS`                        | Comma-separated list of tools to enable                              | -         | No       |
| `LFXMCP_MCP_API_AUTH_SERVERS`         | Comma-separated list of authorization server URLs                    | -         | No       |
| `LFXMCP_MCP_API_PUBLIC_URL`           | Public URL for MCP API (for OAuth PRM)                               | -         | No       |
| `LFXMCP_MCP_API_SCOPES`               | OAuth scopes as comma-separated list                                 | -         | No       |
| `LFXMCP_CLIENT_ID`                    | OAuth client ID for authentication                                   | -         | No       |
| `LFXMCP_CLIENT_SECRET`                | OAuth client secret                                                  | -         | No       |
| `LFXMCP_CLIENT_ASSERTION_SIGNING_KEY` | PEM-encoded RSA private key for client assertion                     | -         | No       |
| `LFXMCP_TOKEN_ENDPOINT`               | OAuth2 token endpoint URL for token exchange                         | -         | No       |
| `LFXMCP_LFX_API_URL`                  | LFX API URL (used as token exchange audience)                        | -         | No       |
| `LFXMCP_ONBOARDING_API_URL`           | Base URL of the member onboarding service                            | -         | No       |
| `LFXMCP_ONBOARDING_API_AUDIENCE`      | Auth0 resource server audience for the member onboarding API         | -         | No       |
| `LFXMCP_LENS_API_URL`                 | Base URL of the LFX Lens service                                     | -         | No       |
| `LFXMCP_LENS_API_AUDIENCE`            | Auth0 resource server audience for the LFX Lens API                  | -         | No       |

**Example:**

```bash
export LFXMCP_MODE=http
export LFXMCP_HTTP_PORT=8080
export LFXMCP_DEBUG=true
export LFXMCP_TOOLS=hello_world,user_info
export LFXMCP_MCP_API_AUTH_SERVERS=https://linuxfoundation-dev.auth0.com
export LFXMCP_CLIENT_ID=your_client_id
export LFXMCP_TOKEN_ENDPOINT=https://linuxfoundation-dev.auth0.com/oauth/token
export LFXMCP_LFX_API_URL=https://lfx-api.dev.v2.cluster.linuxfound.info/

./bin/lfx-mcp-server
```

## Error Handling Patterns

### Tool Error Responses

```go
// Return error in tool result (not JSON-RPC error)
return &mcp.CallToolResult{
    Content: []mcp.Content{
        &mcp.TextContent{Text: "Error: " + err.Error()},
    },
    IsError: true,
}, nil, nil

// Return JSON-RPC error for invalid requests
return nil, nil, fmt.Errorf("invalid parameter: %s", param)
```

### MCP Protocol Errors

The SDK handles most protocol-level errors automatically. Tool implementation should focus on business logic errors.

## Debugging Tips

1. **Server Logs**: Use log statements in tool handlers
2. **JSON Validation**: Ensure JSON-RPC messages are properly formatted
3. **Schema Validation**: Check that input matches generated schema
4. **Manual Testing**: Use the test script for quick validation

### Debug Mode

Add debug logging to tools:

```go
func(ctx context.Context, req *mcp.CallToolRequest, args MyToolArgs) (*mcp.CallToolResult, any, error) {
    log.Printf("Tool called with args: %+v", args)
    // Tool implementation
}
```

## Dependencies

### Core Dependencies

- `github.com/modelcontextprotocol/go-sdk` - Official MCP Go SDK
- `github.com/google/jsonschema-go` - JSON schema generation (indirect)

### Development Tools

- `golangci-lint` - Code linting (optional)
- `jq` - JSON processing for tests

## Contributing Guidelines

1. **Add Tools**: Create new tools in `internal/tools/` following the established pattern
2. **Tool Organization**: One tool per file (e.g., `hello_world.go`, `my_tool.go`)
3. **Registration Pattern**: Each tool should have a `Register<ToolName>(server)` function that calls `AddToolWithScopes` with `ReadScopes()` or `WriteScopes()` — never call `mcp.AddTool` directly
4. **Schema Tags**: Always include descriptive `jsonschema` tags
5. **Testing**: Test new tools with the test script (`./scripts/test_server.sh`)
6. **Documentation**: Update README.md for user-facing changes
7. **Code Quality**: Run `make check` before commits
8. **Package Comments**: Every new `*.go` file must include a `// Package <name> ...` doc comment immediately above the `package` declaration (required by Megalinter's revive `package-comments` rule)

## Future Extensions

The skeleton is designed for easy extension with LFX-specific tools:

- **Project Management**: Create/search/update projects
- **Committee Management**: Committee and committee member management
- **Meeting Management**: Meetings scheduling and participant management
- **Mailing List Management**: Meetings creation and management
- **Membership operations**: Get project members and key contacts

Each new tool should follow the established patterns and maintain the clean, simple architecture.
