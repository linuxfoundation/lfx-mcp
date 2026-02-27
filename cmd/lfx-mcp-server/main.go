// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/linuxfoundation/lfx-mcp/internal/tools"
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

	// Register tools.
	tools.RegisterHelloWorld(server)

	// Run the server on stdio transport.
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
