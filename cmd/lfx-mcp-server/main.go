// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: Apache-2.0

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

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "stdio":
		runStdioServer()
	case "http":
		runHTTPServer()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s <command> [options]\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  stdio    Start MCP server on stdio transport\n")
	fmt.Fprintf(os.Stderr, "  http     Start MCP server on HTTP transport\n\n")
	fmt.Fprintf(os.Stderr, "HTTP Options:\n")
	fmt.Fprintf(os.Stderr, "  --port   Port to listen on (default: 8080)\n")
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
	// Parse HTTP-specific flags.
	httpFlags := flag.NewFlagSet("http", flag.ExitOnError)
	port := httpFlags.Int("port", 8080, "Port to listen on")
	if err := httpFlags.Parse(os.Args[2:]); err != nil {
		log.Fatalf("Failed to parse flags: %v", err)
	}

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
