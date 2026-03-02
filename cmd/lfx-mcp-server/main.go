// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package main provides the LFX MCP server binary with support for stdio and HTTP transports.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
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
	OAuth OAuthConfig `koanf:"oauth"`
	Tools []string    `koanf:"tools"`
	Debug bool        `koanf:"debug"`
}

// HTTPConfig holds HTTP-specific configuration.
type HTTPConfig struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

// OAuthConfig holds OAuth authentication configuration.
type OAuthConfig struct {
	Domain      string `koanf:"domain"`
	ResourceURL string `koanf:"resource_url"`
	Scopes      string `koanf:"scopes"`
}

const errKey = "error"

var logger *slog.Logger

func main() {
	k := koanf.New(".")

	// Define flags.
	f := flag.NewFlagSet("lfx-mcp-server", flag.ExitOnError)
	f.String("mode", "stdio", "Transport mode: stdio or http")
	f.String("http.host", "127.0.0.1", "Host to bind to for HTTP transport")
	f.Int("http.port", 8080, "Port to listen on for HTTP transport")
	f.String("oauth.domain", "", "Issuer domain for OAuth")
	f.String("oauth.resource_url", "", "LFX API domain")
	f.String("oauth.scopes", "openid,profile", "OAuth scopes (comma-separated)")
	f.String("tools", "", "Comma-separated list of tools to enable (default: none)")
	f.Bool("debug", false, "Enable debug logging")

	if err := f.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse flags: %v\n", err)
		os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "Failed to load environment variables: %v\n", err)
		os.Exit(1)
	}

	// Load configuration from flags (flags override environment variables).
	if err := k.Load(basicflag.Provider(f, "."), nil); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load flags: %v\n", err)
		os.Exit(1)
	}

	// Unmarshal configuration.
	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to unmarshal config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger with JSON handler.
	logOptions := &slog.HandlerOptions{}

	// Optional debug logging.
	if cfg.Debug {
		logOptions.Level = slog.LevelDebug
		logOptions.AddSource = true
	}

	logger = slog.New(slog.NewJSONHandler(os.Stdout, logOptions))
	slog.SetDefault(logger)

	// Parse tools flag if provided as comma-separated string.
	if toolsFlag := f.Lookup("tools"); toolsFlag != nil && toolsFlag.Value.String() != "" {
		cfg.Tools = strings.Split(toolsFlag.Value.String(), ",")
	}

	// Configure user_info tool if OAuth is configured.
	if cfg.OAuth.Domain != "" {
		tools.SetUserInfoConfig(&tools.UserInfoConfig{
			OAuthDomain: cfg.OAuth.Domain,
			HTTPClient:  &http.Client{Timeout: 30 * time.Second},
		})
	}

	// Validate OAuth configuration for HTTP mode.
	if cfg.Mode == "http" {
		if cfg.OAuth.Domain == "" {
			logger.Warn("oauth.domain not configured - OAuth metadata endpoint will not be available")
		}
		if cfg.OAuth.ResourceURL == "" {
			logger.Warn("oauth.resource_url not configured - LFX API clients will not be available")
		}
	}

	// Run server based on mode.
	switch cfg.Mode {
	case "stdio":
		runStdioServer(cfg)
	case "http":
		runHTTPServer(cfg)
	default:
		logger.With(errKey, fmt.Errorf("invalid mode: %s", cfg.Mode)).Error("invalid mode (must be 'stdio' or 'http')")
		os.Exit(1)
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
		logger.With(errKey, err).Error("server failed")
		os.Exit(1)
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

	// Apply OAuth bearer token middleware if OAuth is configured.
	// Note: We don't verify tokens here - we just proxy the Authorization header
	// to upstream LFX APIs which perform the actual verification.
	// TODO: extract principal for logging.
	var mcpHandler http.Handler = handler
	if cfg.OAuth.Domain != "" && cfg.OAuth.ResourceURL != "" {
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
		logger.Info("OAuth bearer token required for /mcp endpoint (proxied to upstream)")
	}

	mux.Handle("/mcp", mcpHandler)

	// Add Protected Resource Metadata endpoint if OAuth is configured.
	if cfg.OAuth.Domain != "" && cfg.OAuth.ResourceURL != "" {
		authServerURL := fmt.Sprintf("https://%s/", cfg.OAuth.Domain)

		// Parse scopes from comma-separated string.
		scopes := strings.Split(cfg.OAuth.Scopes, ",")
		for i := range scopes {
			scopes[i] = strings.TrimSpace(scopes[i])
		}

		metadata := &oauthex.ProtectedResourceMetadata{
			Resource:             cfg.OAuth.ResourceURL,
			AuthorizationServers: []string{authServerURL},
			ScopesSupported:      scopes,
		}

		mux.Handle("/.well-known/oauth-protected-resource", auth.ProtectedResourceMetadataHandler(metadata))
		logger.With("url", fmt.Sprintf("http://%s:%d/.well-known/oauth-protected-resource", cfg.HTTP.Host, cfg.HTTP.Port)).Info("OAuth Protected Resource Metadata endpoint available")
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
		logger.With("addr", addr).Info("Starting HTTP server")
		logger.With("url", fmt.Sprintf("http://%s/mcp", addr)).Info("MCP endpoint available")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for shutdown signal or server error.
	select {
	case <-shutdown:
		logger.Info("Shutting down HTTP server")
	case err := <-errCh:
		logger.With(errKey, err).Error("HTTP server failed")
	}

	// Create shutdown context with timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown.
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.With(errKey, err).Error("Server shutdown failed")
		os.Exit(1)
	}

	logger.Info("HTTP server stopped")
}
