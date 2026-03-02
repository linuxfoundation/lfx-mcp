// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

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
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// Config holds all configuration for the LFX MCP server.
type Config struct {
	Mode  string      `koanf:"mode"`
	HTTP  HTTPConfig  `koanf:"http"`
	Auth0 Auth0Config `koanf:"auth0"`
	Tools []string    `koanf:"tools"`
}

// HTTPConfig holds HTTP-specific configuration.
type HTTPConfig struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

// Auth0Config holds Auth0 authentication configuration.
type Auth0Config struct {
	Domain      string `koanf:"domain"`
	ResourceURL string `koanf:"resource_url"`
}

func main() {
	k := koanf.New(".")

	// Define flags.
	f := flag.NewFlagSet("lfx-mcp-server", flag.ExitOnError)
	f.String("mode", "stdio", "Transport mode: stdio or http")
	f.String("http.host", "127.0.0.1", "Host to bind to for HTTP transport")
	f.Int("http.port", 8080, "Port to listen on for HTTP transport")
	f.String("auth0.domain", "", "Auth0 domain (e.g., dev-lfx.us.auth0.com)")
	f.String("auth0.resource_url", "", "LFX API domain")
	f.String("tools", "", "Comma-separated list of tools to enable (default: none)")

	if err := f.Parse(os.Args[1:]); err != nil {
		log.Fatalf("Failed to parse flags: %v", err)
	}

	// Load configuration from environment variables with LFX_MCP_ prefix.
	if err := k.Load(env.Provider(".", env.Opt{
		Prefix: "LFX_MCP_",
		TransformFunc: func(k, v string) (string, any) {
			key := strings.Replace(strings.ToLower(
				strings.TrimPrefix(k, "LFX_MCP_")), "_", ".", -1)
			// Handle comma-separated list for tools.
			if key == "tools" && v != "" {
				return key, strings.Split(v, ",")
			}
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

	// Parse tools flag if provided as comma-separated string.
	if toolsFlag := f.Lookup("tools"); toolsFlag != nil && toolsFlag.Value.String() != "" {
		cfg.Tools = strings.Split(toolsFlag.Value.String(), ",")
	}

	// Configure user_info tool if Auth0 is configured.
	if cfg.Auth0.Domain != "" {
		tools.SetUserInfoConfig(&tools.UserInfoConfig{
			Auth0Domain: cfg.Auth0.Domain,
			HTTPClient:  &http.Client{Timeout: 30 * time.Second},
		})
	}

	// Validate OAuth configuration for HTTP mode.
	if cfg.Mode == "http" {
		if cfg.Auth0.Domain == "" {
			log.Println("Warning: auth0.domain not configured - OAuth metadata endpoint will not be available")
		}
		if cfg.Auth0.ResourceURL == "" {
			log.Println("Warning: auth0.resource_url not configured - LFX API clients will not be available")
		}
	}

	// Run server based on mode.
	switch cfg.Mode {
	case "stdio":
		runStdioServer(cfg)
	case "http":
		runHTTPServer(cfg)
	default:
		log.Fatalf("Invalid mode: %s (must be 'stdio' or 'http')", cfg.Mode)
	}
}

// newServer creates and configures a new MCP server with registered tools.
func newServer(cfg Config) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "lfx-mcp-server",
		Version: "0.1.0",
	}, nil)

	// Register tools based on configuration.
	enabledTools := make(map[string]bool)
	for _, tool := range cfg.Tools {
		enabledTools[strings.TrimSpace(tool)] = true
	}

	if enabledTools["hello_world"] {
		tools.RegisterHelloWorld(server)
	}
	if enabledTools["user_info"] {
		tools.RegisterUserInfo(server)
	}

	return server
}

func runStdioServer(cfg Config) {
	ctx := context.Background()

	// Create the MCP server.
	server := newServer(cfg)

	// Run the server on stdio transport.
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func runHTTPServer(cfg Config) {
	// Create server factory function for stateless mode.
	createServer := func(_ *http.Request) *mcp.Server {
		return newServer(cfg)
	}

	// Create streamable HTTP handler with stateless mode.
	handler := mcp.NewStreamableHTTPHandler(createServer, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})

	// Setup HTTP server with handler mounted on /mcp.
	mux := http.NewServeMux()

	// Apply OAuth bearer token middleware if Auth0 is configured.
	// Note: We don't verify tokens here - we just proxy the Authorization header
	// to upstream LFX APIs which perform the actual verification.
	// TODO: extract principal for logging.
	var mcpHandler http.Handler = handler
	if cfg.Auth0.Domain != "" && cfg.Auth0.ResourceURL != "" {
		resourceMetadataURL := fmt.Sprintf("http://%s:%d/.well-known/oauth-protected-resource", cfg.HTTP.Host, cfg.HTTP.Port)

		// Pass-through verifier - accepts any token without validation.
		verifyToken := func(ctx context.Context, token string, req *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{
				UserID: "_anonymous", // Placeholder until we extract from token.
			}, nil
		}

		authMiddleware := auth.RequireBearerToken(verifyToken, &auth.RequireBearerTokenOptions{
			ResourceMetadataURL: resourceMetadataURL,
		})
		mcpHandler = authMiddleware(handler)
		log.Println("OAuth bearer token required for /mcp endpoint (proxied to upstream)")
	}

	mux.Handle("/mcp", mcpHandler)

	// Add Protected Resource Metadata endpoint if Auth0 is configured.
	if cfg.Auth0.Domain != "" && cfg.Auth0.ResourceURL != "" {
		resourceURL := fmt.Sprintf("http://%s:%d/mcp", cfg.HTTP.Host, cfg.HTTP.Port)
		authServerURL := fmt.Sprintf("https://%s/.well-known/openid-configuration", cfg.Auth0.Domain)

		metadata := &oauthex.ProtectedResourceMetadata{
			Resource:             resourceURL,
			AuthorizationServers: []string{authServerURL},
			ScopesSupported:      []string{"openid", "profile", "email"},
		}

		mux.Handle("/.well-known/oauth-protected-resource", auth.ProtectedResourceMetadataHandler(metadata))
		log.Printf("OAuth Protected Resource Metadata endpoint available at http://%s:%d/.well-known/oauth-protected-resource", cfg.HTTP.Host, cfg.HTTP.Port)
	}

	addr := fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port)
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
