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
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/knadh/koanf/providers/basicflag"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/v2"
	lfxauth "github.com/linuxfoundation/lfx-mcp/internal/auth"
	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
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
	DebugTraffic              bool         `koanf:"debug_traffic"`
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

// defaultTools is the list of tools enabled by default.
var defaultTools = []string{
	"search_projects",
	"get_project",
	"search_committees",
	"get_committee",
	"get_committee_member",
	"search_committee_members",
	"get_mailing_list_service",
	"get_mailing_list",
	"get_mailing_list_member",
	"search_mailing_lists",
	"search_mailing_list_members",
	"search_members",
	"get_member_membership",
	"get_membership_key_contacts",
	"search_meetings",
	"get_meeting",
	"search_meeting_registrants",
	"get_meeting_registrant",
	"search_past_meeting_participants",
	"get_past_meeting_participant",
	"search_past_meeting_transcripts",
	"get_past_meeting_transcript",
	"search_past_meeting_summaries",
	"get_past_meeting_summary",
}

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
	f.String("mcp_api.scopes", "", "Comma-separated list of OAuth scopes for PRM")
	f.String("client_id", "", "OAuth client ID for authentication")
	f.String("client_secret", "", "OAuth client secret (ignored if client_assertion_signing_key is set)")
	f.String("client_assertion_signing_key", "", "PEM-encoded RSA private key for client assertion (takes precedence over client_secret)")
	f.String("token_endpoint", "", "OAuth2 token endpoint URL for token exchange")
	f.String("lfx_api_url", "", "LFX API URL (used as token exchange audience)")
	f.String("tools", strings.Join(defaultTools, ","), "Comma-separated list of tools to enable")
	f.Bool("debug", false, "Enable debug logging")
	f.Bool("debug_traffic", false, "Enable HTTP request/response debug logging for outbound LFX API calls")

	if err := f.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse flags: %v\n", err)
		os.Exit(1)
	}

	// Load flags first (provides defaults).
	if err := k.Load(basicflag.ProviderWithValue(f, ".", func(key string, value string) (string, interface{}) {
		// Handle comma-separated lists.
		if (key == "tools" || key == "mcp_api.auth_servers" || key == "mcp_api.scopes") && value != "" {
			return key, strings.Split(value, ",")
		}
		return key, value
	}, k), nil); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load flags: %v\n", err)
		os.Exit(1)
	}

	// Load configuration from environment variables with LFXMCP_ prefix (overrides flags).
	if err := k.Load(env.Provider(".", env.Opt{
		Prefix: "LFXMCP_",
		TransformFunc: func(k, v string) (string, any) {
			key := strings.ToLower(strings.TrimPrefix(k, "LFXMCP_"))
			// Replace underscores with dots for nested config (e.g., MCP_API_AUTH_SERVERS -> mcp_api.auth_servers).
			key = strings.ReplaceAll(key, "mcp_api_", "mcp_api.")
			key = strings.ReplaceAll(key, "http_", "http.")
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

	// Configure user_info tool if auth servers are configured.
	if len(cfg.MCPAPI.AuthServers) > 0 {
		// Build complete userinfo endpoint URL from first auth server.
		authServerURL := strings.TrimSuffix(cfg.MCPAPI.AuthServers[0], "/")
		userInfoEndpoint := authServerURL + "/userinfo"
		tools.SetUserInfoConfig(&tools.UserInfoConfig{
			UserInfoEndpoint: userInfoEndpoint,
			HTTPClient:       &http.Client{Timeout: 30 * time.Second},
		})
	}

	// Configure project tools if token exchange is configured.
	if cfg.LFXAPIURL != "" && cfg.TokenEndpoint != "" && cfg.ClientID != "" {
		subjectTokenType := cfg.MCPAPI.PublicURL
		if subjectTokenType == "" {
			subjectTokenType = fmt.Sprintf("http://%s:%d/mcp", cfg.HTTP.Host, cfg.HTTP.Port)
		}

		tokenExchangeClient, err := lfxv2.NewTokenExchangeClient(lfxv2.TokenExchangeConfig{
			TokenEndpoint:             cfg.TokenEndpoint,
			ClientID:                  cfg.ClientID,
			ClientSecret:              cfg.ClientSecret,
			ClientAssertionSigningKey: cfg.ClientAssertionSigningKey,
			SubjectTokenType:          subjectTokenType,
			Audience:                  cfg.LFXAPIURL,
			HTTPClient:                &http.Client{Timeout: 30 * time.Second},
		})
		if err != nil {
			logger.Warn("failed to create token exchange client - project and committee tools will not be available", errKey, err)
		} else {
			var debugLogger *slog.Logger
			if cfg.DebugTraffic {
				debugLogger = logger
			}
			tools.SetProjectConfig(&tools.ProjectConfig{
				LFXAPIURL:           cfg.LFXAPIURL,
				TokenExchangeClient: tokenExchangeClient,
				DebugLogger:         debugLogger,
			})
			tools.SetCommitteeConfig(&tools.CommitteeConfig{
				LFXAPIURL:           cfg.LFXAPIURL,
				TokenExchangeClient: tokenExchangeClient,
				DebugLogger:         debugLogger,
			})
			tools.SetMailingListConfig(&tools.MailingListConfig{
				LFXAPIURL:           cfg.LFXAPIURL,
				TokenExchangeClient: tokenExchangeClient,
				DebugLogger:         debugLogger,
			})
			tools.SetMemberConfig(&tools.MemberConfig{
				LFXAPIURL:           cfg.LFXAPIURL,
				TokenExchangeClient: tokenExchangeClient,
				DebugLogger:         debugLogger,
			})
			tools.SetMeetingConfig(&tools.MeetingConfig{
				LFXAPIURL:           cfg.LFXAPIURL,
				TokenExchangeClient: tokenExchangeClient,
				DebugLogger:         debugLogger,
			})
		}
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

// mcpLoggingMiddleware creates middleware that logs all MCP method calls.
func mcpLoggingMiddleware(serverLogger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			start := time.Now()
			sessionID := req.GetSession().ID()

			// Call the actual handler.
			result, err := next(ctx, method, req)

			// Log completion.
			duration := time.Since(start)
			if err != nil {
				serverLogger.Warn("mcp method call failed",
					"session_id", sessionID,
					"method", method,
					"duration_ms", duration.Milliseconds(),
					"error", err,
				)
			} else {
				serverLogger.Info("mcp method call completed",
					"session_id", sessionID,
					"method", method,
					"duration_ms", duration.Milliseconds(),
				)
			}

			return result, err
		}
	}
}

// newServer creates and configures a new MCP server with registered tools.
func newServer(cfg Config) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "lfx-mcp-server",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Logger: logger,
	})

	// Add middleware for logging all MCP method calls from clients.
	server.AddReceivingMiddleware(mcpLoggingMiddleware(logger))

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
	if enabledTools["search_projects"] {
		tools.RegisterSearchProjects(server)
	}
	if enabledTools["get_project"] {
		tools.RegisterGetProject(server)
	}
	if enabledTools["search_committees"] {
		tools.RegisterSearchCommittees(server)
	}
	if enabledTools["get_committee"] {
		tools.RegisterGetCommittee(server)
	}
	if enabledTools["get_committee_member"] {
		tools.RegisterGetCommitteeMember(server)
	}
	if enabledTools["search_committee_members"] {
		tools.RegisterSearchCommitteeMembers(server)
	}
	if enabledTools["get_mailing_list_service"] {
		tools.RegisterGetMailingListService(server)
	}
	if enabledTools["get_mailing_list"] {
		tools.RegisterGetMailingList(server)
	}
	if enabledTools["get_mailing_list_member"] {
		tools.RegisterGetMailingListMember(server)
	}
	if enabledTools["search_mailing_lists"] {
		tools.RegisterSearchMailingLists(server)
	}
	if enabledTools["search_mailing_list_members"] {
		tools.RegisterSearchMailingListMembers(server)
	}
	if enabledTools["search_members"] {
		tools.RegisterSearchMembers(server)
	}
	if enabledTools["get_member_membership"] {
		tools.RegisterGetMemberMembership(server)
	}
	if enabledTools["get_membership_key_contacts"] {
		tools.RegisterGetMembershipKeyContacts(server)
	}
	if enabledTools["search_meetings"] {
		tools.RegisterSearchMeetings(server)
	}
	if enabledTools["get_meeting"] {
		tools.RegisterGetMeeting(server)
	}
	if enabledTools["search_meeting_registrants"] {
		tools.RegisterSearchMeetingRegistrants(server)
	}
	if enabledTools["get_meeting_registrant"] {
		tools.RegisterGetMeetingRegistrant(server)
	}
	if enabledTools["search_past_meeting_participants"] {
		tools.RegisterSearchPastMeetingParticipants(server)
	}
	if enabledTools["get_past_meeting_participant"] {
		tools.RegisterGetPastMeetingParticipant(server)
	}
	if enabledTools["search_past_meeting_transcripts"] {
		tools.RegisterSearchPastMeetingTranscripts(server)
	}
	if enabledTools["get_past_meeting_transcript"] {
		tools.RegisterGetPastMeetingTranscript(server)
	}
	if enabledTools["search_past_meeting_summaries"] {
		tools.RegisterSearchPastMeetingSummaries(server)
	}
	if enabledTools["get_past_meeting_summary"] {
		tools.RegisterGetPastMeetingSummary(server)
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
		Logger:    logger,
	})

	// Setup HTTP server with handler mounted on /mcp.
	mux := http.NewServeMux()

	// Register health endpoints unconditionally so probes work without auth.
	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Apply OAuth bearer token middleware if auth servers are configured.
	var mcpHandler http.Handler = handler
	if len(cfg.MCPAPI.AuthServers) > 0 {
		// Use the public URL base for the resource metadata URL so that MCP clients
		// outside the cluster can reach it via the WWW-Authenticate header.
		resourceMetadataBase := cfg.MCPAPI.PublicURL
		if resourceMetadataBase == "" {
			resourceMetadataBase = fmt.Sprintf("http://%s:%d/mcp", cfg.HTTP.Host, cfg.HTTP.Port)
		}
		// Derive the well-known URL from the public MCP endpoint by replacing the path.
		resourceMetadataBaseURL, _ := url.Parse(resourceMetadataBase)
		resourceMetadataBaseURL.Path = "/.well-known/oauth-protected-resource"
		resourceMetadataURL := resourceMetadataBaseURL.String()

		// Determine the expected audience for JWT verification.
		audience := cfg.MCPAPI.PublicURL
		if audience == "" {
			audience = fmt.Sprintf("http://%s:%d/mcp", cfg.HTTP.Host, cfg.HTTP.Port)
		}

		// Create JWT verifier with JWKS caching.
		jwtVerifier, err := lfxauth.NewJWTVerifier(lfxauth.JWTVerifierConfig{
			AuthServers: cfg.MCPAPI.AuthServers,
			Audience:    audience,
			HTTPClient:  &http.Client{Timeout: 30 * time.Second},
		})
		if err != nil {
			logger.Error("failed to create JWT verifier", "error", err)
			os.Exit(1)
		}

		// Token verifier that performs full JWT signature verification.
		verifyToken := func(ctx context.Context, tokenString string, _ *http.Request) (*auth.TokenInfo, error) {
			token, err := jwtVerifier.VerifyToken(ctx, tokenString)
			if err != nil {
				return nil, err
			}

			// Extract subject (user ID).
			userID := token.Subject()
			if userID == "" {
				userID = "_anonymous"
			}

			// Store raw token in Extra for use in token exchange.
			extra := make(map[string]any)
			extra["raw_token"] = tokenString

			return &auth.TokenInfo{
				UserID:     userID,
				Expiration: token.Expiration(),
				Scopes:     lfxauth.ExtractScopes(token),
				Extra:      extra,
			}, nil
		}

		authMiddleware := auth.RequireBearerToken(verifyToken, &auth.RequireBearerTokenOptions{
			ResourceMetadataURL: resourceMetadataURL,
		})
		mcpHandler = authMiddleware(handler)
		logger.Info("OAuth bearer token verification enabled for /mcp endpoint", "audience", audience)
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

		// Redirect buggy MCP clients to the *correct* PRM endpoint.
		redirectToPRM := func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/.well-known/oauth-protected-resource", http.StatusFound)
		}
		mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", redirectToPRM)
		mux.HandleFunc("/mcp/.well-known/oauth-protected-resource", redirectToPRM)
	}

	addr := fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           httpDebugLogging(logger)(mux),
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

// httpDebugLogging returns middleware that logs all incoming HTTP requests and their
// completion at DEBUG level, including paths not handled by any route (404s).
func httpDebugLogging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Extract and trim JWT token if present.
			logAttrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
			}

			// Add query parameters if present.
			if query := r.URL.RawQuery; query != "" {
				logAttrs = append(logAttrs, "query", query)
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				// Check if it's a JWT token (bearer eyJ... with exactly 2 periods).
				lowerAuth := strings.ToLower(authHeader)
				if strings.HasPrefix(lowerAuth, "bearer eyj") {
					// Extract token part after "Bearer ".
					parts := strings.SplitN(authHeader, " ", 2)
					if len(parts) == 2 {
						token := parts[1]
						periodCount := strings.Count(token, ".")
						if periodCount == 2 {
							// Trim everything after the second period (remove signature).
							secondPeriod := strings.LastIndex(token, ".")
							trimmedToken := token[:secondPeriod]
							logAttrs = append(logAttrs, "bearer_token", trimmedToken)
						}
					}
				}
			}

			// Log incoming request at DEBUG level.
			logger.Debug("http request", logAttrs...)

			// Call the next handler.
			next.ServeHTTP(w, r)

			// Log completion at DEBUG level.
			logger.Debug("http request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}
