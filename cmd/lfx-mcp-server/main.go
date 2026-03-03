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
	Mode                      string       `koanf:"mode"`
	HTTP                      HTTPConfig   `koanf:"http"`
	MCPAPI                    MCPAPIConfig `koanf:"mcp_api"`
	ClientID                  string       `koanf:"client_id"`
	ClientSecret              string       `koanf:"client_secret"`
	ClientAssertionSigningKey string       `koanf:"client_assertion_signing_key"`
	TokenEndpoint             string       `koanf:"token_endpoint"`
	LFXAPIURL                 string       `koanf:"lfx_api_url"`
	Tools                     []string     `koanf:"tools"`
	Debug                     bool         `koanf:"debug"`
}

// HTTPConfig holds HTTP transport configuration.
type HTTPConfig struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

// MCPAPIConfig holds MCP API configuration for OAuth Protected Resource Metadata.
type MCPAPIConfig struct {
	PublicURL   string   `koanf:"public_url"`
	AuthServers []string `koanf:"auth_servers"`
	Scopes      []string `koanf:"scopes"`
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
	f.String("mcp_api.public_url", "", "Public URL for MCP API (for OAuth PRM; if not set, uses http://host:port/mcp)")
	f.String("mcp_api.auth_servers", "", "Comma-separated list of authorization server URLs for OAuth PRM")
	f.String("mcp_api.scopes", "openid,profile", "Comma-separated list of OAuth scopes for PRM")
	f.String("client_id", "", "OAuth client ID for authentication")
	f.String("client_secret", "", "OAuth client secret (ignored if client_assertion_signing_key is set)")
	f.String("client_assertion_signing_key", "", "PEM-encoded RSA private key for client assertion (takes precedence over client_secret)")
	f.String("token_endpoint", "", "OAuth2 token endpoint URL for token exchange")
	f.String("lfx_api_url", "", "LFX API URL (used as token exchange audience)")
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
			// Handle comma-separated lists.
			if (key == "tools" || key == "mcp_api.auth_servers" || key == "mcp_api.scopes") && v != "" {
				return key, strings.Split(v, ",")
			}
			return key, v
		},
	}), nil); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load environment variables: %v\n", err)
		os.Exit(1)
	}

	// Load configuration from flags (only explicitly set flags override environment variables).
	if err := k.Load(basicflag.ProviderWithValue(f, ".", func(key string, value string) (string, interface{}) {
		return key, value
	}, k), nil); err != nil {
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
	// In stdio mode, logs must go to stderr to avoid interfering with MCP protocol on stdout.
	logOptions := &slog.HandlerOptions{}

	// Optional debug logging.
	if cfg.Debug {
		logOptions.Level = slog.LevelDebug
		logOptions.AddSource = true
	}

	logOutput := os.Stdout
	if cfg.Mode == "stdio" {
		logOutput = os.Stderr
	}

	logger = slog.New(slog.NewJSONHandler(logOutput, logOptions))
	slog.SetDefault(logger)

	// Parse comma-separated flags.
	if toolsFlag := f.Lookup("tools"); toolsFlag != nil && toolsFlag.Value.String() != "" {
		cfg.Tools = strings.Split(toolsFlag.Value.String(), ",")
	}
	if authServersFlag := f.Lookup("mcp_api.auth_servers"); authServersFlag != nil && authServersFlag.Value.String() != "" {
		cfg.MCPAPI.AuthServers = strings.Split(authServersFlag.Value.String(), ",")
	}
	if scopesFlag := f.Lookup("mcp_api.scopes"); scopesFlag != nil && scopesFlag.Value.String() != "" {
		cfg.MCPAPI.Scopes = strings.Split(scopesFlag.Value.String(), ",")
	}

	// Configure user_info tool if auth servers are configured.
	if len(cfg.MCPAPI.AuthServers) > 0 {
		// Use first auth server for user info lookups.
		authServerURL := strings.TrimSuffix(cfg.MCPAPI.AuthServers[0], "/")
		tools.SetUserInfoConfig(&tools.UserInfoConfig{
			OAuthDomain: authServerURL,
			HTTPClient:  &http.Client{Timeout: 30 * time.Second},
		})
	}

	// Validate configuration for HTTP mode.
	if cfg.Mode == "http" {
		if len(cfg.MCPAPI.AuthServers) == 0 {
			logger.Warn("mcp_api.auth_servers not configured - OAuth metadata endpoint will not be available")
		}
		if cfg.LFXAPIURL == "" {
			logger.Warn("lfx_api_url not configured - token exchange will not be available")
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

	// Apply OAuth bearer token middleware if auth servers are configured.
	// Note: We don't verify tokens here - we just proxy the Authorization header
	// to upstream LFX APIs which perform the actual verification.
	// TODO: extract principal for logging.
	var mcpHandler http.Handler = handler
	if len(cfg.MCPAPI.AuthServers) > 0 {
		resourceMetadataURL := fmt.Sprintf("http://%s:%d/.well-known/oauth-protected-resource", cfg.HTTP.Host, cfg.HTTP.Port)

		// Pass-through verifier - accepts any token without validation.
		verifyToken := func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
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

	// Add Protected Resource Metadata endpoint if auth servers are configured.
	if len(cfg.MCPAPI.AuthServers) > 0 {
		// Ensure auth server URLs end with /.
		authServers := make([]string, len(cfg.MCPAPI.AuthServers))
		for i, server := range cfg.MCPAPI.AuthServers {
			authServers[i] = strings.TrimSuffix(server, "/") + "/"
		}

		// Determine resource URL (public URL takes precedence).
		resourceURL := cfg.MCPAPI.PublicURL
		if resourceURL == "" {
			resourceURL = fmt.Sprintf("http://%s:%d/mcp", cfg.HTTP.Host, cfg.HTTP.Port)
		}

		metadata := &oauthex.ProtectedResourceMetadata{
			Resource:             resourceURL,
			AuthorizationServers: authServers,
			ScopesSupported:      cfg.MCPAPI.Scopes,
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
