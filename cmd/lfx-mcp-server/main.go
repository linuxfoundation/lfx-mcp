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
	"strings"
	"syscall"
	"time"

	"github.com/knadh/koanf/providers/basicflag"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/v2"
	"github.com/linuxfoundation/lfx-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Config holds all configuration for the LFX MCP server.
type Config struct {
	Mode string     `koanf:"mode"`
	HTTP HTTPConfig `koanf:"http"`
}

// HTTPConfig holds HTTP-specific configuration.
type HTTPConfig struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

func main() {
	k := koanf.New(".")

	// Define flags.
	f := flag.NewFlagSet("lfx-mcp-server", flag.ExitOnError)
	f.String("mode", "stdio", "Transport mode: stdio or http")
	f.String("http.host", "127.0.0.1", "Host to bind to for HTTP transport")
	f.Int("http.port", 8080, "Port to listen on for HTTP transport")

	if err := f.Parse(os.Args[1:]); err != nil {
		log.Fatalf("Failed to parse flags: %v", err)
	}

	// Load configuration from environment variables with LFX_MCP_ prefix.
	if err := k.Load(env.Provider(".", env.Opt{
		Prefix: "LFX_MCP_",
		TransformFunc: func(k, v string) (string, any) {
			key := strings.Replace(strings.ToLower(
				strings.TrimPrefix(k, "LFX_MCP_")), "_", ".", -1)
			return key, v
		},
	}), nil); err != nil {
		log.Fatalf("Failed to load environment variables: %v", err)
	}

	// Load configuration from flags (flags override environment variables).
	if err := k.Load(basicflag.Provider(f, "."), nil); err != nil {
		log.Fatalf("Failed to load flags: %v", err)
	}

	// Unmarshal configuration.
	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		log.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Run server based on mode.
	switch cfg.Mode {
	case "stdio":
		runStdioServer()
	case "http":
		runHTTPServer(cfg.HTTP)
	default:
		log.Fatalf("Invalid mode: %s (must be 'stdio' or 'http')", cfg.Mode)
	}
}

// newServer creates and configures a new MCP server with registered tools.
func newServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "lfx-mcp-server",
		Version: "0.1.0",
	}, nil)

	// Register tools.
	tools.RegisterHelloWorld(server)

	return server
}

func runStdioServer() {
	ctx := context.Background()

	// Create the MCP server.
	server := newServer()

	// Run the server on stdio transport.
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func runHTTPServer(cfg HTTPConfig) {
	// Create server factory function for stateless mode.
	createServer := func(_ *http.Request) *mcp.Server {
		return newServer()
	}

	// Create streamable HTTP handler with stateless mode.
	handler := mcp.NewStreamableHTTPHandler(createServer, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})

	// Setup HTTP server with handler mounted on /mcp.
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Setup graceful shutdown.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)

	// Start server in goroutine.
	go func() {
		log.Printf("Starting HTTP server on %s", addr)
		log.Printf("MCP endpoint available at http://%s/mcp", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for shutdown signal or server error.
	select {
	case <-shutdown:
		log.Println("Shutting down HTTP server...")
	case err := <-errCh:
		log.Printf("HTTP server failed: %v", err)
	}

	// Create shutdown context with timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown.
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}

	log.Println("HTTP server stopped")
}
