// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client configuration for LFX v2 API services.
//
// # Token Exchange
//
// The package supports automatic OAuth2 token exchange (RFC 8693) for converting
// MCP access tokens into LFX API tokens. Token exchange is performed per-request
// using tokens stored in the request context.
//
// # Usage in MCP Tools
//
// Tools should extract the raw MCP token from the request and attach it to the
// context before making LFX API calls:
//
//	func handleMyTool(ctx context.Context, req *mcp.CallToolRequest, args MyToolArgs) (*mcp.CallToolResult, any, error) {
//	    // Extract raw MCP token from request.
//	    mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
//	    if err != nil {
//	        return nil, nil, err
//	    }
//
//	    // Attach token to context for LFX API calls.
//	    ctx = lfxv2.WithMCPToken(ctx, mcpToken)
//
//	    // Create clients with token exchange enabled.
//	    clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
//	        APIDomain: "https://api.lfx.dev",
//	        TokenExchangeClient: tokenExchangeClient, // shared instance
//	    })
//	    if err != nil {
//	        return nil, nil, err
//	    }
//
//	    // Make API calls - token exchange happens automatically.
//	    result, err := clients.Project.GetOneProjectBase(ctx, &projectservice.GetOneProjectBasePayload{})
//	    // ...
//	}
//
// # Token Caching
//
// Exchanged tokens are cached per MCP token to avoid redundant exchanges.
// The cache is thread-safe and automatically expires tokens with a 5-minute buffer.
package lfxv2

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	goahttp "goa.design/goa/v3/http"

	committeeservice "github.com/linuxfoundation/lfx-v2-committee-service/gen/committee_service"
	committeehttpclient "github.com/linuxfoundation/lfx-v2-committee-service/gen/http/committee_service/client"
	projecthttpclient "github.com/linuxfoundation/lfx-v2-project-service/api/project/v1/gen/http/project_service/client"
	projectservice "github.com/linuxfoundation/lfx-v2-project-service/api/project/v1/gen/project_service"
	queryhttpclient "github.com/linuxfoundation/lfx-v2-query-service/gen/http/query_svc/client"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

// mcpTokenContextKey is used to store the MCP bearer token in request context.
type mcpTokenContextKey struct{}

// WithMCPToken returns a context with the MCP bearer token attached.
// This token will be used for OAuth2 token exchange when making LFX API calls.
func WithMCPToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, mcpTokenContextKey{}, token)
}

// mcpTokenFromContext extracts the MCP bearer token from the context.
func mcpTokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(mcpTokenContextKey{}).(string); ok {
		return token
	}
	return ""
}

// ExtractMCPToken extracts the raw MCP token from auth.TokenInfo.Extra.
// This token can be passed to WithMCPToken for use in LFX API calls.
// Returns an error if the token cannot be extracted.
func ExtractMCPToken(tokenInfo *auth.TokenInfo) (string, error) {
	if tokenInfo == nil || tokenInfo.Extra == nil {
		return "", fmt.Errorf("tokenInfo or Extra map is nil")
	}

	rawToken, ok := tokenInfo.Extra["raw_token"].(string)
	if !ok || rawToken == "" {
		return "", fmt.Errorf("raw MCP token not found in TokenInfo.Extra")
	}

	return rawToken, nil
}

// ClientConfig holds configuration for LFX v2 API clients.
type ClientConfig struct {
	// APIDomain is the LFX API base domain.
	APIDomain string

	// HTTPClient is the HTTP client to use for API calls.
	// If nil, a default client with 30s timeout will be created.
	HTTPClient *http.Client

	// TokenExchangeClient is the RFC 8693 OAuth2 token exchange client.
	// If provided, the client will automatically exchange MCP tokens (from request context)
	// for target API tokens.
	TokenExchangeClient *TokenExchangeClient

	// DebugLogger is used for debug-level HTTP request/response logging.
	// If nil, debug logging is disabled.
	DebugLogger *slog.Logger
}

// Clients holds initialized LFX v2 API service clients.
type Clients struct {
	Committee *committeeservice.Client
	Project   *projectservice.Client
	QuerySvc  *querysvc.Client

	tokenExchangeClient *TokenExchangeClient

	// Token cache: maps MCP token -> exchanged LFX token info.
	mu         sync.RWMutex
	tokenCache map[string]*cachedToken
}

// cachedToken holds an exchanged LFX API token with its expiration.
type cachedToken struct {
	accessToken string
	expiry      time.Time
}

// NewClients initializes and returns LFX v2 API service clients.
func NewClients(_ context.Context, cfg ClientConfig) (*Clients, error) {
	if cfg.APIDomain == "" {
		return nil, fmt.Errorf("APIDomain is required")
	}

	// Create HTTP client if not provided.
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	// Wrap HTTP client with auth interceptor if token exchange is enabled.
	clients := &Clients{
		tokenExchangeClient: cfg.TokenExchangeClient,
		tokenCache:          make(map[string]*cachedToken),
	}

	if cfg.DebugLogger != nil {
		httpClient = newDebugTransportClient(httpClient, cfg.DebugLogger)
	}

	if cfg.TokenExchangeClient != nil {
		httpClient = clients.wrapWithAuthInterceptor(httpClient)
	}

	// Initialize committee service client.
	committeeURL, err := url.Parse(cfg.APIDomain + "/committees")
	if err != nil {
		return nil, fmt.Errorf("failed to parse committee service URL: %w", err)
	}

	committeeHTTPClient := committeehttpclient.NewClient(
		committeeURL.Scheme,
		committeeURL.Host,
		httpClient,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	)

	clients.Committee = committeeservice.NewClient(
		committeeHTTPClient.CreateCommittee(),
		committeeHTTPClient.GetCommitteeBase(),
		committeeHTTPClient.UpdateCommitteeBase(),
		committeeHTTPClient.DeleteCommittee(),
		committeeHTTPClient.GetCommitteeSettings(),
		committeeHTTPClient.UpdateCommitteeSettings(),
		committeeHTTPClient.Readyz(),
		committeeHTTPClient.Livez(),
		committeeHTTPClient.CreateCommitteeMember(),
		committeeHTTPClient.GetCommitteeMember(),
		committeeHTTPClient.UpdateCommitteeMember(),
		committeeHTTPClient.DeleteCommitteeMember(),
	)

	// Initialize project service client.
	projectURL, err := url.Parse(cfg.APIDomain + "/projects")
	if err != nil {
		return nil, fmt.Errorf("failed to parse project service URL: %w", err)
	}

	projectHTTPClient := projecthttpclient.NewClient(
		projectURL.Scheme,
		projectURL.Host,
		httpClient,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	)

	clients.Project = projectservice.NewClient(
		projectHTTPClient.GetProjects(),
		projectHTTPClient.CreateProject(),
		projectHTTPClient.GetOneProjectBase(),
		projectHTTPClient.GetOneProjectSettings(),
		projectHTTPClient.UpdateProjectBase(),
		projectHTTPClient.UpdateProjectSettings(),
		projectHTTPClient.DeleteProject(),
		projectHTTPClient.Readyz(),
		projectHTTPClient.Livez(),
	)

	// Initialize query service client.
	queryURL, err := url.Parse(cfg.APIDomain + "/query")
	if err != nil {
		return nil, fmt.Errorf("failed to parse query service URL: %w", err)
	}

	queryHTTPClient := queryhttpclient.NewClient(
		queryURL.Scheme,
		queryURL.Host,
		httpClient,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	)

	clients.QuerySvc = querysvc.NewClient(
		queryHTTPClient.QueryResources(),
		queryHTTPClient.QueryResourcesCount(),
		queryHTTPClient.QueryOrgs(),
		queryHTTPClient.SuggestOrgs(),
		queryHTTPClient.Readyz(),
		queryHTTPClient.Livez(),
	)

	return clients, nil
}

// newDebugTransportClient wraps an HTTP client with a transport that logs the
// full HTTP wire dump of every outbound request and inbound response at DEBUG level.
func newDebugTransportClient(client *http.Client, logger *slog.Logger) *http.Client {
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{
		Transport: &debugTransport{
			transport: base,
			logger:    logger,
		},
		Timeout: client.Timeout,
	}
}

// debugTransport is an http.RoundTripper that logs the full HTTP wire dump of
// every outbound request and inbound response at DEBUG level.
type debugTransport struct {
	transport http.RoundTripper
	logger    *slog.Logger
}

// RoundTrip implements http.RoundTripper.
func (dt *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	reqDump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		dt.logger.Error("failed to dump outbound request", "error", err)
	} else {
		dt.logger.Debug("lfxv2 outbound request", "dump", string(reqDump))
	}

	resp, err := dt.transport.RoundTrip(req)
	if err != nil {
		dt.logger.Error("lfxv2 outbound request failed", "error", err, "url", req.URL.String())
		return nil, err
	}

	respDump, err := httputil.DumpResponse(resp, true)
	if err != nil {
		dt.logger.Error("failed to dump inbound response", "error", err)
	} else {
		dt.logger.Debug("lfxv2 inbound response", "dump", string(respDump))
	}

	return resp, nil
}

// wrapWithAuthInterceptor wraps an HTTP client with automatic token exchange.
func (c *Clients) wrapWithAuthInterceptor(client *http.Client) *http.Client {
	originalTransport := client.Transport
	if originalTransport == nil {
		originalTransport = http.DefaultTransport
	}

	return &http.Client{
		Transport: &authInterceptor{
			base:    originalTransport,
			clients: c,
		},
		Timeout: client.Timeout,
	}
}

// authInterceptor intercepts HTTP requests to inject the exchanged LFX V2 API token.
type authInterceptor struct {
	base    http.RoundTripper
	clients *Clients
}

// RoundTrip implements http.RoundTripper.
func (a *authInterceptor) RoundTrip(req *http.Request) (*http.Response, error) {
	// Extract MCP token from request context.
	mcpToken := mcpTokenFromContext(req.Context())
	if mcpToken == "" {
		return nil, fmt.Errorf("MCP token not found in request context")
	}

	// Get or exchange token for this MCP token.
	lfxToken, err := a.clients.getOrExchangeToken(req.Context(), mcpToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get LFX token: %w", err)
	}

	// Clone request to avoid modifying the original.
	reqClone := req.Clone(req.Context())
	reqClone.Header.Set("Authorization", "Bearer "+lfxToken)

	return a.base.RoundTrip(reqClone)
}

// getOrExchangeToken gets a cached LFX token or performs token exchange.
func (c *Clients) getOrExchangeToken(ctx context.Context, mcpToken string) (string, error) {
	if c.tokenExchangeClient == nil {
		return "", fmt.Errorf("token exchange client not configured")
	}

	// Check cache first (read lock).
	c.mu.RLock()
	cached, exists := c.tokenCache[mcpToken]
	c.mu.RUnlock()

	// Return cached token if valid.
	if exists && time.Now().Before(cached.expiry) {
		return cached.accessToken, nil
	}

	// Need to exchange token (write lock).
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check cache in case another goroutine just updated it.
	cached, exists = c.tokenCache[mcpToken]
	if exists && time.Now().Before(cached.expiry) {
		return cached.accessToken, nil
	}

	// Perform token exchange.
	resp, err := c.tokenExchangeClient.ExchangeToken(ctx, mcpToken)
	if err != nil {
		return "", err
	}

	// Cache the exchanged token with 5-minute buffer.
	expiryBuffer := 5 * time.Minute
	c.tokenCache[mcpToken] = &cachedToken{
		accessToken: resp.AccessToken,
		expiry:      time.Now().Add(time.Duration(resp.ExpiresIn)*time.Second - expiryBuffer),
	}

	return resp.AccessToken, nil
}
