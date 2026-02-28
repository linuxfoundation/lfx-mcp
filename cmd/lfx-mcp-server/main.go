// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: Apache-2.0

// Package main provides the LFX MCP server binary with support for stdio and HTTP transports.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/linuxfoundation/lfx-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	httpMode = flag.Bool("http", false, "Enable HTTP transport (default: stdio)")
	port     = flag.Int("port", 8080, "Port to listen on for HTTP transport")
)

func main() {
	flag.Parse()

	if *httpMode {
		runHTTPServer()
	} else {
		runStdioServer()
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

func runHTTPServer() {
	// Create server factory function for stateless mode.
	createServer := func(_ *http.Request) *mcp.Server {
		server := mcp.NewServer(&mcp.Implementation{
			Name:    "lfx-mcp-server",
			Version: "0.1.0",
		}, nil)

		// Register tools.
		tools.RegisterHelloWorld(server)

		return server
	}

	// Create streamable HTTP handler with stateless mode.
	handler := mcp.NewStreamableHTTPHandler(createServer, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})

	// Setup HTTP server with handler mounted on /mcp.
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	addr := fmt.Sprintf(":%d", *port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Setup graceful shutdown.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Start server in goroutine.
	go func() {
		log.Printf("Starting HTTP server on %s", addr)
		log.Printf("MCP endpoint available at http://localhost%s/mcp", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Wait for shutdown signal.
	<-shutdown
	log.Println("Shutting down HTTP server...")

	// Create shutdown context with timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown.
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}

	log.Println("HTTP server stopped")
}
