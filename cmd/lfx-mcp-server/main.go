// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "stdio" {
		runStdioServer()
	} else {
		fmt.Fprintf(os.Stderr, "Usage: %s stdio\n", os.Args[0])
		os.Exit(1)
	}
}

func runStdioServer() {
	ctx := context.Background()

	// Create the MCP server.
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "lfx-mcp-server",
		Version: "0.1.0",
	}, nil)

	// Add hello world tool.
	type HelloWorldArgs struct {
		Name    string `json:"name,omitempty" jsonschema:"Name to greet (optional, defaults to 'World')"`
		Message string `json:"message,omitempty" jsonschema:"Custom greeting message (optional)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "hello_world",
		Description: "A simple hello world tool that greets the user with an optional custom message",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args HelloWorldArgs) (*mcp.CallToolResult, any, error) {
		// Extract name parameter with default.
		name := "World"
		if args.Name != "" {
			name = args.Name
		}

		// Generate greeting.
		var greeting string
		if args.Message != "" {
			greeting = fmt.Sprintf("%s, %s!", args.Message, name)
		} else {
			greeting = fmt.Sprintf("Hello, %s!", name)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: greeting},
			},
		}, nil, nil
	})

	// Run the server on stdio transport.
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
