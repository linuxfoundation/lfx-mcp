# AGENTS.md

This file provides essential information for AI agents working on the LFX MCP Server codebase. It focuses on development workflows, architecture understanding, and build processes needed for making code changes.

## Repository Overview

The LFX MCP Server is a Model Context Protocol (MCP) implementation that provides tools and resources for interacting with the Linux Foundation's LFX platform. It's built using the official Go SDK for MCP and follows a clean, extensible architecture.

### Key Technologies

- **Language**: Go 1.26.0+
- **Protocol**: Model Context Protocol (MCP) 2024-11-05
- **SDK**: Official MCP Go SDK v1.4.0+
- **Transport**: JSON-RPC 2.0 over stdio
- **Schema**: Automatic JSON schema generation via struct tags

## Architecture Overview

The service follows a simple, clean architecture pattern optimized for MCP tool development:

```text
lfx-mcp/
├── cmd/
│   └── lfx-mcp-server/     # Main application entry point
├── internal/
│   └── tools/              # MCP tool implementations
├── bin/                    # Built binaries (gitignored)
├── go.mod                  # Go module definition
├── Makefile               # Build automation
├── test_server.sh         # Integration test script
├── README.md              # User documentation
└── AGENTS.md              # This file (AI agent guidelines)
```

### Data Flow
```text
Client (Claude, etc.) → JSON-RPC 2.0 → stdio transport → MCP Server → Tool Handler → Response
```

### Key Design Principles

1. **Simplicity**: Minimal abstraction layers using the official MCP Go SDK
2. **Extensibility**: Easy to add new tools through the `mcp.AddTool` pattern
3. **Type Safety**: Strong typing with automatic schema generation
4. **Testability**: Simple stdio testing via JSON-RPC messages
5. **Observability**: Structured JSON logging with optional debug mode

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
./test_server.sh

# Integration tests with debug logging
./test_server.sh --debug

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

The server uses Go's standard `slog` package for structured logging with the following characteristics:

### Log Configuration

- **Format**: JSON (always)
- **Output**: stdout
- **Default Level**: INFO
- **Debug Mode**: Enabled via `-debug` flag or `LFX_MCP_DEBUG=true` environment variable

### Debug Logging

When debug logging is enabled:
- Log level is set to DEBUG
- Source file and line numbers are included in each log entry
- Additional diagnostic information is emitted

**Enable debug logging:**

```bash
# Via command-line flag
./bin/lfx-mcp-server -debug

# Via environment variable
LFX_MCP_DEBUG=true ./bin/lfx-mcp-server

# Both work in HTTP mode too
./bin/lfx-mcp-server -mode=http -debug
```

### Log Structure

All logs are emitted as JSON objects to stdout:

```json
{"time":"2024-01-15T10:30:45.123Z","level":"INFO","msg":"Starting HTTP server","addr":"127.0.0.1:8080"}
{"time":"2024-01-15T10:30:45.456Z","level":"ERROR","msg":"server failed","error":"connection refused"}
```

With debug logging enabled, source information is included:

```json
{"time":"2024-01-15T10:30:45.789Z","level":"DEBUG","source":{"file":"main.go","line":150},"msg":"processing request"}
```

### Using the Logger

The logger is initialized in `main.go` and set as the default slog logger. Use it throughout the codebase:

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

// Log errors with the "error" key
logger.With(errKey, err).Error("operation failed")
```

## Adding New Tools

The MCP Go SDK provides a simple pattern for adding tools. Tools are implemented in the `internal/tools` package and registered with the server.

### Tool Implementation Steps

1. **Create a new file** in `internal/tools/` (e.g., `my_tool.go`)
2. **Define the input struct** with JSON schema tags
3. **Implement the handler function** with tool logic
4. **Create a registration function** to register with the server
5. **Call the registration function** in `main.go`

### Example Tool Implementation

**File: `internal/tools/my_tool.go`**

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
    mcp.AddTool(server, &mcp.Tool{
        Name:        "my_tool",
        Description: "Brief description of what the tool does",
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

The `test_server.sh` script provides comprehensive testing:

```bash
./test_server.sh
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

Currently, the server doesn't require environment variables, but future LFX integrations may add:

| Variable      | Description          | Default | Required |
|---------------|----------------------|---------|----------|
| `LFX_API_URL` | LFX API base URL     | -       | Future   |
| `DEBUG`       | Enable debug logging | false   | No       |

...and various other parameters needed to configure OAuth2 authentication.

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
3. **Registration Pattern**: Each tool should have a `Register<ToolName>(server)` function
4. **Schema Tags**: Always include descriptive `jsonschema` tags
5. **Testing**: Test new tools with the test script (`./test_server.sh`)
6. **Documentation**: Update README.md for user-facing changes
7. **Code Quality**: Run `make check` before commits

## Future Extensions

The skeleton is designed for easy extension with LFX-specific tools:

- **Project Management**: Create/search/update projects
- **Committee Management**: Committee and committee member management
- **Meeting Management**: Meetings scheduling and participant management
- **Mailing List Management**: Meetings creation and management
- **Membership operations**: Get project members and key contacts

Each new tool should follow the established patterns and maintain the clean, simple architecture.
