// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client configuration for LFX v2 API services.
package lfxv2

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	goahttp "goa.design/goa/v3/http"

	projectclient "github.com/linuxfoundation/lfx-v2-project-service/api/project/v1/gen/http/project_service/client"
	projectservice "github.com/linuxfoundation/lfx-v2-project-service/api/project/v1/gen/project_service"
)

// ClientConfig holds configuration for LFX v2 API clients.
type ClientConfig struct {
	// APIDomain is the LFX API base domain.
	APIDomain string

	// HTTPClient is the HTTP client to use for API calls.
	// If nil, a default client with 30s timeout will be created.
	HTTPClient *http.Client

	// TokenExchangeClient is the RFC 8693 OAuth2 token exchange client.
	// If provided, the client will automatically exchange subject tokens for target API tokens.
	TokenExchangeClient *TokenExchangeClient

	// MCPToken is the subject token to exchange for a target API token.
	// Required if TokenExchangeClient is provided.
	MCPToken string
}

// Clients holds initialized LFX v2 API service clients.
type Clients struct {
	Project             *projectservice.Client
	tokenExchangeClient *TokenExchangeClient
	mcpToken            string
	lfxV2Token          string
	tokenExpiry         time.Time
}

// NewClients initializes and returns LFX v2 API service clients.
func NewClients(_ context.Context, cfg ClientConfig) (*Clients, error) {
	if cfg.APIDomain == "" {
		return nil, fmt.Errorf("APIDomain is required")
	}

	// Validate token exchange configuration.
	if cfg.TokenExchangeClient != nil && cfg.MCPToken == "" {
		return nil, fmt.Errorf("MCPToken is required when TokenExchangeClient is provided")
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
		mcpToken:            cfg.MCPToken,
	}

	if cfg.TokenExchangeClient != nil {
		httpClient = clients.wrapWithAuthInterceptor(httpClient)
	}

	// Initialize project service client.
	projectURL, err := url.Parse(cfg.APIDomain + "/projects")
	if err != nil {
		return nil, fmt.Errorf("failed to parse project service URL: %w", err)
	}

	projectHTTPClient := projectclient.NewClient(
		projectURL.Scheme,
		projectURL.Host,
		httpClient,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	)

	projectClient := projectservice.NewClient(
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

	clients.Project = projectClient

	return clients, nil
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
	// Check if we need to exchange or refresh the token.
	if a.clients.lfxV2Token == "" || time.Now().After(a.clients.tokenExpiry) {
		if err := a.clients.exchangeToken(req.Context()); err != nil {
			return nil, fmt.Errorf("failed to exchange token: %w", err)
		}
	}

	// Clone request to avoid modifying the original.
	reqClone := req.Clone(req.Context())
	reqClone.Header.Set("Authorization", "Bearer "+a.clients.lfxV2Token)

	return a.base.RoundTrip(reqClone)
}

// exchangeToken performs the token exchange and updates the cached token.
func (c *Clients) exchangeToken(ctx context.Context) error {
	if c.tokenExchangeClient == nil {
		return fmt.Errorf("token exchange client not configured")
	}

	resp, err := c.tokenExchangeClient.ExchangeToken(ctx, c.mcpToken)
	if err != nil {
		return err
	}

	c.lfxV2Token = resp.AccessToken
	// Set expiry with 5-minute buffer to avoid edge cases.
	expiryBuffer := 5 * time.Minute
	c.tokenExpiry = time.Now().Add(time.Duration(resp.ExpiresIn)*time.Second - expiryBuffer)

	return nil
}
