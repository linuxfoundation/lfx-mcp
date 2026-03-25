// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client utilities for interacting with LFX v2 APIs, including OAuth2 token exchange.
package lfxv2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ClientCredentialsConfig holds configuration for OAuth2 client credentials grants.
type ClientCredentialsConfig struct {
	// TokenEndpoint is the OAuth2 token endpoint URL (e.g., "https://example.auth0.com/oauth/token").
	TokenEndpoint string

	// ClientID is the M2M client ID.
	ClientID string

	// ClientSecret is the M2M client secret.
	// Ignored if ClientAssertionSigningKey is provided.
	ClientSecret string

	// ClientAssertionSigningKey is the PEM-encoded RSA private key for generating client assertions.
	// If provided, this takes precedence over ClientSecret for client authentication.
	ClientAssertionSigningKey string

	// Audience is the resource server identifier (the "aud" claim in the issued token).
	Audience string

	// HTTPClient is the HTTP client to use. If nil, a default client with 30s timeout is created.
	HTTPClient *http.Client
}

// ClientCredentialsClient handles OAuth2 client credentials grants for M2M authentication.
// It caches tokens and refreshes them automatically when they expire.
type ClientCredentialsClient struct {
	config ClientCredentialsConfig
	client *http.Client

	mu          sync.RWMutex
	cachedToken string
	expiry      time.Time
}

// NewClientCredentialsClient creates a new OAuth2 client credentials client.
func NewClientCredentialsClient(cfg ClientCredentialsConfig) (*ClientCredentialsClient, error) {
	if cfg.TokenEndpoint == "" {
		return nil, fmt.Errorf("TokenEndpoint is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("ClientID is required")
	}
	if cfg.ClientSecret == "" && cfg.ClientAssertionSigningKey == "" {
		return nil, fmt.Errorf("either ClientSecret or ClientAssertionSigningKey is required")
	}
	if cfg.Audience == "" {
		return nil, fmt.Errorf("Audience is required")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &ClientCredentialsClient{
		config: cfg,
		client: httpClient,
	}, nil
}

// GetToken returns a valid bearer token, fetching a new one if the cached token has expired.
func (c *ClientCredentialsClient) GetToken(ctx context.Context) (string, error) {
	// Fast path: check cache with read lock.
	c.mu.RLock()
	if c.cachedToken != "" && time.Now().Before(c.expiry) {
		token := c.cachedToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	// Slow path: acquire write lock and fetch a new token.
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock.
	if c.cachedToken != "" && time.Now().Before(c.expiry) {
		return c.cachedToken, nil
	}

	token, expiresIn, err := c.fetchToken(ctx)
	if err != nil {
		return "", err
	}

	c.cachedToken = token
	// Cache with 5-minute buffer before actual expiry.
	c.expiry = time.Now().Add(time.Duration(expiresIn)*time.Second - 5*time.Minute)

	return token, nil
}

// fetchToken performs the client credentials grant against the token endpoint.
func (c *ClientCredentialsClient) fetchToken(ctx context.Context) (token string, expiresIn int, err error) {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.config.ClientID)
	data.Set("audience", c.config.Audience)

	// Use client assertion if signing key is provided, otherwise use client secret.
	if c.config.ClientAssertionSigningKey != "" {
		assertion, err := generateClientAssertion(c.config.ClientID, c.config.TokenEndpoint, c.config.ClientAssertionSigningKey)
		if err != nil {
			return "", 0, fmt.Errorf("failed to generate client assertion: %w", err)
		}
		data.Set("client_assertion", assertion)
		data.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	} else {
		data.Set("client_secret", c.config.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create client credentials request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("client credentials request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read client credentials response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("client credentials grant failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", 0, fmt.Errorf("failed to parse client credentials response: %w", err)
	}

	return tokenResp.AccessToken, tokenResp.ExpiresIn, nil
}
