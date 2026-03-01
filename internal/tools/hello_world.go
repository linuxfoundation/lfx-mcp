// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: Apache-2.0

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HelloWorldArgs defines the input parameters for the hello_world tool.
type HelloWorldArgs struct {
	Name    string `json:"name,omitempty" jsonschema:"Name to greet (optional, defaults to 'World')"`
	Message string `json:"message,omitempty" jsonschema:"Custom greeting message (optional)"`
}

// RegisterHelloWorld registers the hello_world tool with the MCP server.
func RegisterHelloWorld(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "hello_world",
		Description: "A simple hello world tool that greets the user with an optional custom message",
	}, handleHelloWorld)
}

// handleHelloWorld implements the hello_world tool logic.
func handleHelloWorld(_ context.Context, _ *mcp.CallToolRequest, args HelloWorldArgs) (*mcp.CallToolResult, any, error) {
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
}
