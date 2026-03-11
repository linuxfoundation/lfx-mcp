// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"log/slog"

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
		Annotations: &mcp.ToolAnnotations{
			Title:         "Hello World",
			ReadOnlyHint:  true,
			OpenWorldHint: boolPtr(false),
		},
	}, handleHelloWorld)
}

// handleHelloWorld implements the hello_world tool logic.
func handleHelloWorld(_ context.Context, req *mcp.CallToolRequest, args HelloWorldArgs) (*mcp.CallToolResult, any, error) {
	// Create MCP logger that sends logs to the client.
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	// Extract name parameter with default.
	name := "World"
	if args.Name != "" {
		name = args.Name
	}

	// Log tool execution with client-visible logs.
	logger.Info("hello_world tool called", "name", name)

	// Generate greeting.
	var greeting string
	if args.Message != "" {
		greeting = fmt.Sprintf("%s, %s!", args.Message, name)
		logger.Warn("custom message provided", "message", args.Message)
	} else {
		greeting = fmt.Sprintf("Hello, %s!", name)
	}

	logger.Debug("greeting generated", "greeting", greeting)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: greeting},
		},
	}, nil, nil
}
