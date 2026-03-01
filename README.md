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
./bin/lfx-mcp-server -http
```

The server will start an HTTP endpoint at `http://localhost:8080/mcp` that accepts MCP requests over streamable HTTP (Server-Sent Events). This is useful for web-based MCP clients.

**Options:**
- `-http`: Enable HTTP transport (default: stdio)
- `-port`: Port to listen on for HTTP transport (default: 8080)
- `-host`: Host to bind to for HTTP transport (default: 127.0.0.1)

**Example with custom port:**
```bash
./bin/lfx-mcp-server -http -port 9090
```

**Example binding to all interfaces:**
```bash
./bin/lfx-mcp-server -http -host 0.0.0.0 -port 8080
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
 sleep 1) | ./bin/lfx-mcp-server stdio

# Test calling the hello_world tool
(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}'; 
 echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hello_world","arguments":{"name":"LFX User","message":"Welcome"}}}'; 
 sleep 1) | ./bin/lfx-mcp-server
```

#### Testing HTTP Transport

Start the HTTP server and send requests using curl:

```bash
# Start the server in the background
./bin/lfx-mcp-server -http -port 8080 &

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
