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
// A single *Clients instance should be created once at startup (via NewClients)
// and shared across all tool invocations. Per-request, call WithMCPToken to
// attach the caller's MCP bearer token to the context before making LFX API calls:
//
//	func handleMyTool(ctx context.Context, req *mcp.CallToolRequest, args MyToolArgs) (*mcp.CallToolResult, any, error) {
//	    // Extract raw MCP token from request.
//	    mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
//	    if err != nil {
//	        return nil, nil, err
//	    }
//
//	    // Attach token to context; the shared clients instance handles exchange.
//	    ctx = sharedClients.WithMCPToken(ctx, mcpToken)
//
//	    // Make API calls - token exchange and caching happen automatically.
//	    result, err := sharedClients.Project.GetOneProjectBase(ctx, &projectservice.GetOneProjectBasePayload{})
//	    // ...
//	}
//
// # Token Caching
//
// Exchanged tokens are cached per MCP token inside the long-lived *Clients
// instance to avoid redundant token-exchange round-trips on every request.
// The cache is goroutine-safe and automatically expires tokens with a
// fixed buffer of 5 minutes before their exp claim.
package lfxv2

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	gocache "github.com/patrickmn/go-cache"
	goahttp "goa.design/goa/v3/http"

	committeeservice "github.com/linuxfoundation/lfx-v2-committee-service/gen/committee_service"
	committeehttpclient "github.com/linuxfoundation/lfx-v2-committee-service/gen/http/committee_service/client"
	mailinglisthttpclient "github.com/linuxfoundation/lfx-v2-mailing-list-service/gen/http/mailing_list/client"
	mailinglist "github.com/linuxfoundation/lfx-v2-mailing-list-service/gen/mailing_list"
	memberhttpclient "github.com/linuxfoundation/lfx-v2-member-service/gen/http/membership_service/client"
	memberservice "github.com/linuxfoundation/lfx-v2-member-service/gen/membership_service"
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

// apiKeyMCPToken is the sentinel value stored in the MCP token context when a
// request was authenticated via a static API key. getOrExchangeToken recognises
// this value and uses the client_credentials grant directly, bypassing the
// RFC 8693 token-exchange path (which requires a real bearer token).
//
// TEMPORARY — remove when static API key support is retired.
const apiKeyMCPToken = "__api_key__"

// ExtractMCPToken extracts the raw MCP token from auth.TokenInfo.Extra.
// This token can be passed to WithMCPToken for use in LFX API calls.
// Returns an error if the token cannot be extracted.
//
// For requests authenticated via a static API key (Extra["api_key_auth"]==true),
// the apiKeyMCPToken sentinel is returned so that the token-exchange layer uses
// the client_credentials grant rather than attempting to exchange a bearer token.
func ExtractMCPToken(tokenInfo *auth.TokenInfo) (string, error) {
	if tokenInfo == nil || tokenInfo.Extra == nil {
		return "", fmt.Errorf("tokenInfo or Extra map is nil")
	}

	// TEMPORARY: static API-key requests carry no bearer token; signal the
	// token-exchange layer to use client_credentials instead.
	if isAPIKey, _ := tokenInfo.Extra["api_key_auth"].(bool); isAPIKey {
		return apiKeyMCPToken, nil
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
	Committee   *committeeservice.Client
	MailingList *mailinglist.Client
	Member      *memberservice.Client
	Project     *projectservice.Client
	QuerySvc    *querysvc.Client

	tokenExchangeClient *TokenExchangeClient

	// tokenCache maps MCP token -> exchanged LFX token string, with per-entry TTL.
	tokenCache *gocache.Cache
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
		// No default expiration (TTL is set per item); run cleanup every 10 minutes.
		tokenCache: gocache.New(gocache.NoExpiration, 10*time.Minute),
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
		committeeHTTPClient.GetInvite(),
		committeeHTTPClient.CreateInvite(),
		committeeHTTPClient.RevokeInvite(),
		committeeHTTPClient.AcceptInvite(),
		committeeHTTPClient.DeclineInvite(),
		committeeHTTPClient.GetApplication(),
		committeeHTTPClient.SubmitApplication(),
		committeeHTTPClient.ApproveApplication(),
		committeeHTTPClient.RejectApplication(),
		committeeHTTPClient.JoinCommittee(),
		committeeHTTPClient.LeaveCommittee(),
	)

	// Initialize mailing list service client.
	mailingListURL, err := url.Parse(cfg.APIDomain + "/mailing-lists")
	if err != nil {
		return nil, fmt.Errorf("failed to parse mailing list service URL: %w", err)
	}

	mlHTTPClient := mailinglisthttpclient.NewClient(
		mailingListURL.Scheme,
		mailingListURL.Host,
		httpClient,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	)

	clients.MailingList = mailinglist.NewClient(
		mlHTTPClient.Livez(),
		mlHTTPClient.Readyz(),
		mlHTTPClient.CreateGrpsioService(),
		mlHTTPClient.GetGrpsioService(),
		mlHTTPClient.UpdateGrpsioService(),
		mlHTTPClient.DeleteGrpsioService(),
		mlHTTPClient.GetGrpsioServiceSettings(),
		mlHTTPClient.UpdateGrpsioServiceSettings(),
		mlHTTPClient.CreateGrpsioMailingList(),
		mlHTTPClient.GetGrpsioMailingList(),
		mlHTTPClient.UpdateGrpsioMailingList(),
		mlHTTPClient.DeleteGrpsioMailingList(),
		mlHTTPClient.GetGrpsioMailingListSettings(),
		mlHTTPClient.UpdateGrpsioMailingListSettings(),
		mlHTTPClient.CreateGrpsioMailingListMember(),
		mlHTTPClient.GetGrpsioMailingListMember(),
		mlHTTPClient.UpdateGrpsioMailingListMember(),
		mlHTTPClient.DeleteGrpsioMailingListMember(),
		mlHTTPClient.GroupsioWebhook(),
	)

	// Initialize member service client.
	memberURL, err := url.Parse(cfg.APIDomain + "/members")
	if err != nil {
		return nil, fmt.Errorf("failed to parse member service URL: %w", err)
	}

	memberHTTPClient := memberhttpclient.NewClient(
		memberURL.Scheme,
		memberURL.Host,
		httpClient,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	)

	clients.Member = memberservice.NewClient(
		memberHTTPClient.ListProjectTiers(),
		memberHTTPClient.GetProjectTier(),
		memberHTTPClient.ListProjectMemberships(),
		memberHTTPClient.GetProjectMembership(),
		memberHTTPClient.ListMembershipKeyContacts(),
		memberHTTPClient.CreateMembershipKeyContact(),
		memberHTTPClient.UpdateMembershipKeyContact(),
		memberHTTPClient.DeleteMembershipKeyContact(),
		memberHTTPClient.GetMembershipKeyContact(),
		memberHTTPClient.ListB2bOrgs(),
		memberHTTPClient.ListB2bOrgMemberships(),
		memberHTTPClient.Readyz(),
		memberHTTPClient.Livez(),
		memberHTTPClient.DebugVars(),
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

// WithMCPToken attaches mcpToken to ctx so the auth interceptor can retrieve
// it when forwarding requests to LFX APIs. Call this once per inbound request
// before invoking any LFX API method on the shared *Clients instance.
func (c *Clients) WithMCPToken(ctx context.Context, mcpToken string) context.Context {
	return WithMCPToken(ctx, mcpToken)
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

// m2mCacheKey is the cache key used for all M2M token requests. Since M2M callers
// all receive the same LFX token (minted via client_credentials), a single entry
// covers them all rather than one entry per unique M2M subject token.
const m2mCacheKey = "__m2m__"

// getOrExchangeToken gets a cached LFX token or obtains a new one.
// For user tokens, it performs RFC 8693 token exchange.
// For M2M tokens (Auth0 @clients subject), it uses the client_credentials grant
// because Auth0 cannot exchange M2M tokens via token exchange (no user to propagate).
// For static API-key requests (apiKeyMCPToken sentinel), it also uses the
// client_credentials grant since there is no bearer token to exchange.
func (c *Clients) getOrExchangeToken(ctx context.Context, mcpToken string) (string, error) {
	if c.tokenExchangeClient == nil {
		return "", fmt.Errorf("token exchange client not configured")
	}

	// API-key and M2M requests both use client_credentials; they share a cache entry.
	useClientCredentials := mcpToken == apiKeyMCPToken || isM2MToken(mcpToken)
	cacheKey := mcpToken
	if useClientCredentials {
		cacheKey = m2mCacheKey
	}

	// Return cached token if present and not yet expired.
	if accessToken, found := c.tokenCache.Get(cacheKey); found {
		return accessToken.(string), nil
	}

	// Obtain LFX token via the appropriate grant type.
	var resp *TokenExchangeResponse
	var err error
	if useClientCredentials {
		resp, err = c.tokenExchangeClient.ClientCredentials(ctx)
	} else {
		resp, err = c.tokenExchangeClient.ExchangeToken(ctx, mcpToken)
	}
	if err != nil {
		return "", err
	}

	// Cache the token with a 5-minute buffer before its exp claim.
	expiryBuffer := 5 * time.Minute
	ttl := time.Duration(resp.ExpiresIn)*time.Second - expiryBuffer
	c.tokenCache.Set(cacheKey, resp.AccessToken, ttl)

	return resp.AccessToken, nil
}
