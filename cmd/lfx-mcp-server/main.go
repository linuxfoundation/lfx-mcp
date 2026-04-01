// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package main provides the LFX MCP server binary with support for stdio and HTTP transports.
package main

import (
	"context"
	"errors"
	"expvar"
	"flag"
	"fmt"
	"log/slog"
	"net"
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
	localOtel "github.com/linuxfoundation/lfx-mcp/internal/otel"
	"github.com/linuxfoundation/lfx-mcp/internal/serviceapi"
	"github.com/linuxfoundation/lfx-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	slogotel "github.com/remychantenay/slog-otel"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
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
	// APICredentials is a consumer-key→shared-secret map for static API-key auth.
	// TEMPORARY: stop-gap for MCP clients that cannot complete OAuth2.
	APICredentials map[string]string `koanf:"api_credentials"`

	// Service API configuration.
	OnboardingAPIURL      string `koanf:"onboarding_api_url"`
	OnboardingAPIAudience string `koanf:"onboarding_api_audience"`
	LensAPIURL            string `koanf:"lens_api_url"`
	LensAPIAudience       string `koanf:"lens_api_audience"`
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

// Build-time variables set via ldflags.
var (
	// Version is the application version, set at build time via -ldflags.
	// On a tagged commit this is the tag (e.g. v0.4.1); between tags it includes
	// the offset and short hash (e.g. v0.4.1-3-gabcdef0); a -dirty suffix is
	// appended when there are uncommitted changes.
	Version = "dev"
)

const errKey = "error"

// defaultTools is the list of tools enabled by default.
var defaultTools = []string{
	"search_projects",
	"get_project",
	"search_committees",
	"get_committee",
	"get_committee_member",
	"search_committee_members",
	"create_committee",
	"update_committee",
	"update_committee_settings",
	"delete_committee",
	"create_committee_member",
	"update_committee_member",
	"delete_committee_member",
	"get_mailing_list_service",
	"get_mailing_list",
	"get_mailing_list_member",
	"search_mailing_lists",
	"search_mailing_list_members",
	"list_project_tiers",
	"get_project_tier",
	"search_members",
	"get_member_membership",
	"get_membership_key_contacts",
	"get_membership_key_contact",
	"create_membership_key_contact",
	"update_membership_key_contact",
	"delete_membership_key_contact",
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
	"onboarding_list_memberships",
	"lfx_lens_query",
}

var logger *slog.Logger

func main() {
	k := koanf.New(".")

	// Define flags.
	f := flag.NewFlagSet("lfx-mcp-server", flag.ExitOnError)
	f.Usage = func() {
		fmt.Fprintf(f.Output(), "lfx-mcp-server %s\n\nUsage:\n", Version)
		f.PrintDefaults()
	}
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
	f.String("onboarding_api_url", "", "Base URL of the member onboarding service")
	f.String("onboarding_api_audience", "", "Auth0 resource server audience for the member onboarding API")
	f.String("lens_api_url", "", "Base URL of the LFX Lens service")
	f.String("lens_api_audience", "", "Auth0 resource server audience for the LFX Lens API")

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
			// TEMPORARY: map LFXMCP_API_CREDENTIALS_<KEY>=<secret> env vars into the
			// api_credentials koanf map. Each env var contributes one entry.
			// Remove this block when static API key support is retired.
			const apiCredPrefix = "api_credentials_"
			if strings.HasPrefix(key, apiCredPrefix) {
				credKey := strings.TrimPrefix(key, apiCredPrefix)
				if credKey != "" {
					return "api_credentials." + credKey, v
				}
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

	// Build the handler chain: JSON → slog-otel (injects trace_id/span_id from context).
	jsonHandler := slog.NewJSONHandler(logOutput, logOptions)
	otelHandler := slogotel.OtelHandler{Next: jsonHandler}
	logger = slog.New(otelHandler)
	slog.SetDefault(logger)

	// Initialise the OpenTelemetry SDK.  A no-op provider is installed when
	// OTEL_TRACES_EXPORTER is unset (the default), so stdio/local-dev has zero
	// overhead.  The shutdown func is passed to the server runner so it can be
	// flushed inside the graceful shutdown path with a bounded context.
	otelCfg, otelShutdown, err := localOtel.SetupSDK(context.Background(), Version)
	if err != nil {
		logger.Error("failed to initialise OpenTelemetry SDK", errKey, err)
		os.Exit(1)
	}

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
			// Wrap the token-exchange HTTP client with OTel tracing so token
			// fetches appear as child spans under the active request trace.
			HTTPClient: &http.Client{
				Timeout:   30 * time.Second,
				Transport: otelhttp.NewTransport(http.DefaultTransport),
			},
		})
		if err != nil {
			logger.Warn("failed to create token exchange client - project and committee tools will not be available", errKey, err)
		} else {
			var debugLogger *slog.Logger
			if cfg.DebugTraffic {
				debugLogger = logger
			}
			// Shared HTTP client for all LFX API calls: OTel transport so upstream
			// service calls appear as child spans.
			lfxHTTPClient := &http.Client{
				Timeout:   30 * time.Second,
				Transport: otelhttp.NewTransport(http.DefaultTransport),
			}
			// Create a single shared Clients instance so that the token cache
			// persists across requests, eliminating redundant token-exchange
			// round-trips to Auth0 on every tool invocation.
			sharedClients, err := lfxv2.NewClients(context.Background(), lfxv2.ClientConfig{
				APIDomain:           cfg.LFXAPIURL,
				TokenExchangeClient: tokenExchangeClient,
				DebugLogger:         debugLogger,
				HTTPClient:          lfxHTTPClient,
			})

			if err != nil {
				logger.Warn("failed to create shared LFX v2 clients - LFX API tools will not be available", errKey, err)
			} else {
				tools.SetProjectConfig(&tools.ProjectConfig{
					Clients: sharedClients,
				})
				tools.SetCommitteeConfig(&tools.CommitteeConfig{
					Clients: sharedClients,
				})
				tools.SetMailingListConfig(&tools.MailingListConfig{
					Clients: sharedClients,
				})
				tools.SetMemberConfig(&tools.MemberConfig{
					Clients: sharedClients,
				})
				tools.SetMeetingConfig(&tools.MeetingConfig{
					Clients: sharedClients,
				})
				}

			// Configure service API infrastructure (shared across onboarding, lens, etc.).
			slugResolver := lfxv2.NewSlugResolver()
			accessChecker := lfxv2.NewAccessCheckClient(cfg.LFXAPIURL, &http.Client{Timeout: 30 * time.Second})

			sharedAuth := tools.ServiceAuth{
				LFXAPIURL:           cfg.LFXAPIURL,
				TokenExchangeClient: tokenExchangeClient,
				DebugLogger:         debugLogger,
				SlugResolver:        slugResolver,
				AccessChecker:       accessChecker,
			}

			// Shared M2M credentials for client credentials grants.
			ccBase := lfxv2.ClientCredentialsConfig{
				TokenEndpoint:             cfg.TokenEndpoint,
				ClientID:                  cfg.ClientID,
				ClientSecret:              cfg.ClientSecret,
				ClientAssertionSigningKey: cfg.ClientAssertionSigningKey,
			}

			if cfg.OnboardingAPIURL != "" && cfg.OnboardingAPIAudience != "" {
				ccCfg := ccBase
				ccCfg.Audience = cfg.OnboardingAPIAudience
				ccClient, err := lfxv2.NewClientCredentialsClient(ccCfg)
				if err != nil {
					logger.Warn("failed to create onboarding client credentials client", errKey, err)
				} else {
					onboardingClient, err := serviceapi.NewClient(serviceapi.Config{
						BaseURL:     cfg.OnboardingAPIURL,
						TokenSource: ccClient,
						HTTPClient:  &http.Client{Timeout: 30 * time.Second},
						DebugLogger: debugLogger,
					})
					if err != nil {
						logger.Warn("failed to create onboarding service client", errKey, err)
					} else {
						tools.SetOnboardingConfig(&tools.OnboardingConfig{
							ServiceAuth:   sharedAuth,
							ServiceClient: onboardingClient,
						})
						logger.Info("onboarding service tools configured", "url", cfg.OnboardingAPIURL)
					}
				}
			}

			if cfg.LensAPIURL != "" && cfg.LensAPIAudience != "" {
				ccCfg := ccBase
				ccCfg.Audience = cfg.LensAPIAudience
				ccClient, err := lfxv2.NewClientCredentialsClient(ccCfg)
				if err != nil {
					logger.Warn("failed to create Lens client credentials client", errKey, err)
				} else {
					lensClient, err := serviceapi.NewClient(serviceapi.Config{
						BaseURL:     cfg.LensAPIURL,
						TokenSource: ccClient,
						HTTPClient:  &http.Client{Timeout: 120 * time.Second},
						DebugLogger: debugLogger,
					})
					if err != nil {
						logger.Warn("failed to create Lens service client", errKey, err)
					} else {
						tools.SetLensConfig(&tools.LensConfig{
							ServiceAuth:   sharedAuth,
							ServiceClient: lensClient,
						})
						logger.Info("LFX Lens tools configured", "url", cfg.LensAPIURL)
					}
				}
			}
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
		runStdioServer(cfg, otelCfg, otelShutdown)
	case "http":
		runHTTPServer(cfg, otelCfg, otelShutdown)
	default:
		logger.With(errKey, fmt.Errorf("invalid mode: %s", cfg.Mode)).Error("invalid mode (must be 'stdio' or 'http')")
		os.Exit(1)
	}
}

// mcpOTelMiddleware creates middleware that instruments all MCP method calls with
// OpenTelemetry span attributes (per the MCP semconv spec) and structured logging;
// successful completions are logged at DEBUG, as they are observable via APM spans
// without needing to appear in the structured log stream.
func mcpOTelMiddleware(serverLogger *slog.Logger, serviceName string) mcp.Middleware {
	// Instantiate the tracer once; reused across all calls.
	// For HTTP requests this starts a child of the otelhttp server span.
	// For stdio requests (no HTTP layer) this starts a new root span, giving
	// stdio the same tracing coverage as HTTP without any extra setup.
	tracer := otel.Tracer(serviceName)

	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			start := time.Now()
			sessionID := req.GetSession().ID()

			// Start a dedicated MCP span. For HTTP, the otelhttp server span is
			// already in ctx so this becomes a child; for stdio there is no
			// parent and a fresh root span is created instead.
			// Build the span name following the MCP semconv format:
			// "{mcp.method.name} {target}" where target is gen_ai.tool.name
			// when available, otherwise just use the method name.
			// See: https://opentelemetry.io/docs/specs/semconv/gen-ai/mcp/#server
			spanName := method
			if method == "tools/call" {
				if params, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok && params.Name != "" {
					spanName = method + " " + params.Name
				}
			}
			ctx, span := tracer.Start(ctx, spanName)
			defer span.End()

			// Bind mcp.session.id and mcp.method.name onto a child logger and
			// store it in context so every tool handler that calls
			// newToolLogger(ctx, req) automatically inherits both fields on all
			// log records.
			callLogger := serverLogger.With("mcp.session.id", sessionID, "mcp.method.name", method)

			// Bind username to the logger and span if available in TokenInfo.
			extra := req.GetExtra()
			if extra != nil && extra.TokenInfo != nil {
				if username, ok := extra.TokenInfo.Extra["username"].(string); ok && username != "" {
					callLogger = callLogger.With("username", username)
					span.SetAttributes(semconv.EnduserID(username))
				}
			}

			ctx = tools.WithLogger(ctx, callLogger)

			// Tag the MCP span with semconv attributes per
			// https://opentelemetry.io/docs/specs/semconv/gen-ai/mcp/ so APM
			// can filter and group by method and session without parsing logs.
			span.SetAttributes(
				semconv.McpMethodNameKey.String(method),
				semconv.McpSessionID(sessionID),
			)

			// network.transport: "pipe" for stdio (no HTTP headers), "tcp" for HTTP.
			// Per the MCP semconv spec, stdio maps to the "pipe" transport value.
			if extra != nil && extra.Header != nil {
				span.SetAttributes(semconv.NetworkTransportTCP)
			} else {
				span.SetAttributes(semconv.NetworkTransportPipe)
			}

			// mcp.protocol.version: read the version the client advertised during
			// the initialize handshake and record it on the span (Recommended).
			if ss, ok := req.GetSession().(*mcp.ServerSession); ok {
				if initParams := ss.InitializeParams(); initParams != nil && initParams.ProtocolVersion != "" {
					span.SetAttributes(semconv.McpProtocolVersion(initParams.ProtocolVersion))
				}
			}

			// For tools/call, add gen_ai.tool.name (Conditionally Required) and
			// gen_ai.operation.name = "execute_tool" (Recommended).
			if method == "tools/call" {
				if params, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok && params.Name != "" {
					span.SetAttributes(
						semconv.GenAIToolName(params.Name),
						semconv.GenAIOperationNameExecuteTool,
					)
				}
			}

			// Call the actual handler.
			result, err := next(ctx, method, req)

			// Set error.type on the span when the call fails (Conditionally Required).
			// The SDK converts regular tool errors into IsError=true results, so err
			// here is only non-nil for protocol-level failures (e.g. unknown tool name).
			if err != nil {
				span.SetAttributes(semconv.ErrorTypeKey.String(err.Error()))
			} else if method == "tools/call" {
				if toolResult, ok := result.(*mcp.CallToolResult); ok && toolResult != nil && toolResult.IsError {
					span.SetAttributes(semconv.ErrorTypeKey.String("tool_error"))
				}
			}

			// Log completion.
			duration := time.Since(start)
			if err != nil {
				callLogger.WarnContext(ctx, "mcp method call failed",
					"duration_ms", duration.Milliseconds(),
					"error", err,
				)
			} else {
				// DEBUG only: successful calls are observable via APM spans.
				callLogger.DebugContext(ctx, "mcp method call completed",
					"duration_ms", duration.Milliseconds(),
				)
			}

			return result, err
		}
	}
}

// newServer creates and configures a new MCP server with registered tools.
func newServer(cfg Config, serviceName string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "lfx-mcp-server",
		Version: Version,
	}, &mcp.ServerOptions{
		Logger: logger,
	})

	// Add middleware for OTel instrumentation and logging of all MCP method calls.
	server.AddReceivingMiddleware(mcpOTelMiddleware(logger, serviceName))

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
	if enabledTools["create_committee"] {
		tools.RegisterCreateCommittee(server)
	}
	if enabledTools["update_committee"] {
		tools.RegisterUpdateCommittee(server)
	}
	if enabledTools["update_committee_settings"] {
		tools.RegisterUpdateCommitteeSettings(server)
	}
	if enabledTools["delete_committee"] {
		tools.RegisterDeleteCommittee(server)
	}
	if enabledTools["create_committee_member"] {
		tools.RegisterCreateCommitteeMember(server)
	}
	if enabledTools["update_committee_member"] {
		tools.RegisterUpdateCommitteeMember(server)
	}
	if enabledTools["delete_committee_member"] {
		tools.RegisterDeleteCommitteeMember(server)
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
	if enabledTools["list_project_tiers"] {
		tools.RegisterListProjectTiers(server)
	}
	if enabledTools["get_project_tier"] {
		tools.RegisterGetProjectTier(server)
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
	if enabledTools["get_membership_key_contact"] {
		tools.RegisterGetMembershipKeyContact(server)
	}
	if enabledTools["create_membership_key_contact"] {
		tools.RegisterCreateMembershipKeyContact(server)
	}
	if enabledTools["update_membership_key_contact"] {
		tools.RegisterUpdateMembershipKeyContact(server)
	}
	if enabledTools["delete_membership_key_contact"] {
		tools.RegisterDeleteMembershipKeyContact(server)
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

	// Service API tools.
	if enabledTools["onboarding_list_memberships"] {
		tools.RegisterOnboardingListMemberships(server)
	}
	if enabledTools["lfx_lens_query"] {
		tools.RegisterLFXLensQuery(server)
	}

	return server
}

func runStdioServer(cfg Config, otelCfg localOtel.Config, otelShutdown func(context.Context) error) {
	ctx := context.Background()

	// Create the MCP server.
	server := newServer(cfg, otelCfg.ServiceName)

	// Run the server on stdio transport.
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		logger.With(errKey, err).Error("server failed")
		// Flush OTel spans before exiting.
		if serr := otelShutdown(ctx); serr != nil {
			logger.Error("OpenTelemetry SDK shutdown failed", errKey, serr)
		}
		os.Exit(1)
	}

	// Flush OTel spans on clean exit.
	if err := otelShutdown(ctx); err != nil {
		logger.Error("OpenTelemetry SDK shutdown failed", errKey, err)
	}
}

func runHTTPServer(cfg Config, otelCfg localOtel.Config, otelShutdown func(context.Context) error) {
	// Create server factory function for stateless mode.
	createServer := func(_ *http.Request) *mcp.Server {
		return newServer(cfg, otelCfg.ServiceName)
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

	// Register expvar debug endpoint; restricted to loopback-only so it is
	// accessible via kubectl port-forward but blocked from ingress traffic.
	mux.Handle("/debug/vars", localhostOnly(expvar.Handler()))

	// TEMPORARY: build API-key verifier when credentials are configured.
	// Returns nil when no credentials are set, so the check is zero-cost in the common path.
	apiKeyVerifier := lfxauth.NewAPIKeyVerifier(cfg.APICredentials)
	if apiKeyVerifier != nil {
		logger.Info("static API-key authentication enabled (TEMPORARY)")
	}

	// Apply auth middleware to the /mcp handler.
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

		// Token verifier that:
		//  1. TEMPORARY: tries static API-key auth first when configured.
		//  2. Falls back to full JWT signature verification.
		verifyToken := func(ctx context.Context, tokenString string, _ *http.Request) (*auth.TokenInfo, error) {
			// TEMPORARY: check whether the bearer value is a static API key.
			if apiKeyVerifier != nil {
				if info, handled, err := apiKeyVerifier.VerifyAPIKey(ctx, tokenString); handled {
					if err != nil {
						logger.Debug("api key verification failed", errKey, err)
						return nil, err
					}
					return info, nil
				}
			}

			// Standard JWT verification path.
			token, err := jwtVerifier.VerifyToken(ctx, tokenString)
			if err != nil {
				if errors.Is(err, auth.ErrInvalidToken) {
					// Expected invalid/expired token: log at debug and return as-is
					// (jwt_verifier already wraps with auth.ErrInvalidToken).
					logger.Debug("token verification failed", errKey, err)
				} else {
					// Infrastructure or unexpected verification failure: log at error level.
					logger.Error("token verification failed", errKey, err)
				}
				return nil, err
			}

			// Extract subject (user ID).
			userID := token.Subject()
			if userID == "" {
				userID = "_anonymous"
			}

			// Store raw token and custom claims in Extra for use by tool handlers.
			// Also extract username for observability (span enduser.id, log field).
			extra := make(map[string]any)
			extra["raw_token"] = tokenString
			if username := lfxauth.ExtractUsername(token); username != "" {
				extra["username"] = username
			}

			// Extract lf_staff custom claim for service tool authorization (LFX Lens).
			if staffClaim, ok := token.Get(tools.ClaimLFStaff); ok {
				extra[tools.ClaimLFStaff] = staffClaim
			}

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

		// Use defaults when no scopes are configured, then validate to ensure
		// the enforced scopes are always present and warn about unknown ones.
		scopesSupported := cfg.MCPAPI.Scopes
		if len(scopesSupported) == 0 {
			scopesSupported = tools.DefaultScopes()
		}
		scopesSupported = tools.ValidateScopes(scopesSupported, logger.Warn)

		metadata := &oauthex.ProtectedResourceMetadata{
			Resource:             resourceURL,
			AuthorizationServers: authServers,
			ScopesSupported:      scopesSupported,
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

	// Wrap the mux with OTel instrumentation so every inbound HTTP request gets
	// a root span with W3C TraceContext propagation.
	// Health-check probes are excluded from tracing to avoid cluttering the
	// tracing backend with high-frequency, low-value spans.
	var rootHandler http.Handler = mux
	rootHandler = otelhttp.NewHandler(rootHandler, otelCfg.ServiceName,
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/livez" && r.URL.Path != "/readyz"
		}),
	)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           rootHandler,
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
		// Flush OTel spans before exiting.
		if serr := otelShutdown(ctx); serr != nil {
			logger.Error("OpenTelemetry SDK shutdown failed", errKey, serr)
		}
		os.Exit(1)
	}

	// Flush pending OTel spans before the process exits.
	if err := otelShutdown(ctx); err != nil {
		logger.Error("OpenTelemetry SDK shutdown failed", errKey, err)
	}

	logger.Info("HTTP server stopped")
}

// localhostOnly wraps h and returns 403 Forbidden for any request whose remote
// address is not the IPv4 or IPv6 loopback address. This allows the handler to
// be reached via kubectl port-forward (which arrives as 127.0.0.1) while
// blocking traffic that originates from the ingress or other cluster sources.
func localhostOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}
