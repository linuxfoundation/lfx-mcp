# LFX MCP Server

A Model Context Protocol (MCP) server for the Linux Foundation's LFX platform, providing tools and resources for interacting with LFX services.

## Overview

This project implements an MCP server that exposes LFX platform functionality through standardized tools and resources. It's built using the [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) and follows the Model Context Protocol specification.

## Features

- **Hello World Tool**: A simple demonstration tool for testing MCP connectivity
- **Stdio Transport**: Communication via standard input/output streams
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

To start the MCP server with stdio transport:

```bash
./bin/lfx-mcp-server stdio
```

The server will listen for MCP messages on stdin and respond on stdout.

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

```
lfx-mcp/
├── cmd/
│   └── lfx-mcp-server/     # Main application entry point
├── bin/                    # Built binaries (created by make build)
├── go.mod                  # Go module definition
├── Makefile               # Build automation
└── README.md              # This file
```

### Adding New Tools

1. Define your tool's input struct with JSON schema tags:
   ```go
   type MyToolArgs struct {
       Param1 string `json:"param1" jsonschema:"Description of parameter 1"`
       Param2 int    `json:"param2,omitempty" jsonschema:"Optional parameter 2"`
   }
   ```

2. Use `mcp.AddTool` to register your tool:
   ```go
   mcp.AddTool(server, &mcp.Tool{
       Name:        "my_tool",
       Description: "Description of what the tool does",
   }, func(ctx context.Context, req *mcp.CallToolRequest, args MyToolArgs) (*mcp.CallToolResult, any, error) {
       // Your tool implementation here
       return &mcp.CallToolResult{
           Content: []mcp.Content{
               &mcp.TextContent{Text: "Tool result"},
           },
       }, nil, nil
   })
   ```

### Testing

To test the server manually, you can send JSON-RPC messages via stdio:

```bash
# Test server initialization and tool listing
(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}'; 
 echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'; 
 sleep 1) | ./bin/lfx-mcp-server stdio

# Test calling the hello_world tool
(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}'; 
 echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hello_world","arguments":{"name":"LFX User","message":"Welcome"}}}'; 
 sleep 1) | ./bin/lfx-mcp-server stdio
```

## License

Copyright The Linux Foundation and each contributor to LFX.

This project’s source code is licensed under the MIT License. A copy of the
license is available in LICENSE.

This project’s documentation is licensed under the Creative Commons Attribution
4.0 International License \(CC-BY-4.0\). A copy of the license is available in
LICENSE-docs.
