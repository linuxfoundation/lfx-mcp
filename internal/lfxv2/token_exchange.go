// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client utilities for interacting with LFX v2 APIs, including OAuth2 token exchange.
package lfxv2

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// TokenExchangeConfig holds configuration for OAuth2 token exchange (RFC 8693).
type TokenExchangeConfig struct {
	// TokenEndpoint is the OAuth2 token endpoint URL (e.g., "https://example.auth0.com/oauth/token").
	TokenEndpoint string

	// ClientID is the M2M client ID for token exchange.
	ClientID string

	// ClientSecret is the M2M client secret for token exchange.
	// Ignored if ClientAssertionSigningKey is provided.
	ClientSecret string

	// ClientAssertionSigningKey is the PEM-encoded RSA private key for generating client assertions.
	// If provided, this takes precedence over ClientSecret for client authentication.
	// The key is used to sign a JWT assertion per RFC 7523.
	ClientAssertionSigningKey string

	// SubjectTokenType is the token type of the incoming subject token (e.g., LFX MCP API identifier).
	SubjectTokenType string

	// Audience is the target audience for the exchanged token (e.g., LFX V2 API identifier).
	Audience string

	// HTTPClient is the HTTP client to use for token exchange.
	// If nil, a default client with 30s timeout will be created.
	HTTPClient *http.Client
}

// TokenExchangeResponse represents the response from OAuth2 token exchange per RFC 8693.
type TokenExchangeResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type,omitempty"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
	Scope           string `json:"scope,omitempty"`
	RefreshToken    string `json:"refresh_token,omitempty"`
}

// TokenExchangeClient handles OAuth2 token exchange per RFC 8693.
type TokenExchangeClient struct {
	config TokenExchangeConfig
	client *http.Client
}

// NewTokenExchangeClient creates a new RFC 8693 token exchange client.
func NewTokenExchangeClient(cfg TokenExchangeConfig) (*TokenExchangeClient, error) {
	if cfg.TokenEndpoint == "" {
		return nil, fmt.Errorf("TokenEndpoint is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("ClientID is required")
	}
	if cfg.ClientSecret == "" && cfg.ClientAssertionSigningKey == "" {
		return nil, fmt.Errorf("either ClientSecret or ClientAssertionSigningKey is required")
	}
	if cfg.SubjectTokenType == "" {
		return nil, fmt.Errorf("SubjectTokenType is required")
	}
	if cfg.Audience == "" {
		return nil, fmt.Errorf("audience is required")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	return &TokenExchangeClient{
		config: cfg,
		client: httpClient,
	}, nil
}

// generateClientAssertion creates a JWT client assertion per RFC 7523.
// This is a package-level function shared by TokenExchangeClient and ClientCredentialsClient.
func generateClientAssertion(clientID, tokenEndpoint, signingKeyPEM string) (string, error) {
	// Parse the PEM-encoded private key.
	block, _ := pem.Decode([]byte(signingKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block containing private key")
	}

	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Try parsing as PKCS1.
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("failed to parse private key: %w", err)
		}
	}

	rsaKey, ok := privateKey.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("private key is not RSA")
	}

	now := time.Now()

	// Generate a random JTI (JWT ID).
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return "", fmt.Errorf("failed to generate JTI: %w", err)
	}

	// Build JWT token.
	token, err := jwt.NewBuilder().
		Issuer(clientID).
		Subject(clientID).
		Audience([]string{tokenEndpoint}).
		IssuedAt(now).
		Expiration(now.Add(60 * time.Second)).
		JwtID(fmt.Sprintf("%x", jtiBytes)).
		Build()
	if err != nil {
		return "", fmt.Errorf("failed to build JWT: %w", err)
	}

	// Sign the token.
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256, rsaKey))
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	return string(signed), nil
}

// isM2MToken reports whether token is a machine-to-machine (client credentials)
// JWT, identified by Auth0's convention of a subject claim ending in "@clients".
func isM2MToken(token string) bool {
	parsed, err := jwt.ParseInsecure([]byte(token))
	if err != nil {
		return false
	}
	return strings.HasSuffix(parsed.Subject(), "@clients")
}

// addClientAuth adds client authentication fields (secret or JWT assertion) to data.
func (c *TokenExchangeClient) addClientAuth(data url.Values) error {
	if c.config.ClientAssertionSigningKey != "" {
		assertion, err := generateClientAssertion(c.config.ClientID, c.config.TokenEndpoint, c.config.ClientAssertionSigningKey)
		if err != nil {
			return fmt.Errorf("failed to generate client assertion: %w", err)
		}
		data.Set("client_assertion", assertion)
		data.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	} else {
		data.Set("client_secret", c.config.ClientSecret)
	}
	return nil
}

// postTokenRequest sends a token request and returns the parsed response.
func (c *TokenExchangeClient) postTokenRequest(ctx context.Context, data url.Values) (*TokenExchangeResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute token request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Response body close errors are not actionable after reading.

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenExchangeResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}

// ExchangeToken exchanges a subject token for a new access token per RFC 8693.
func (c *TokenExchangeClient) ExchangeToken(ctx context.Context, subjectToken string) (*TokenExchangeResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	data.Set("client_id", c.config.ClientID)
	data.Set("subject_token", subjectToken)
	data.Set("subject_token_type", c.config.SubjectTokenType)
	data.Set("audience", c.config.Audience)

	if err := c.addClientAuth(data); err != nil {
		return nil, err
	}

	return c.postTokenRequest(ctx, data)
}

// ClientCredentials obtains an LFX API token using the client_credentials grant.
// This is used when the caller presents an M2M token, which Auth0 cannot exchange
// via RFC 8693 token exchange (it requires a user subject). Instead, the MCP server
// mints a fresh LFX token using its own client identity.
func (c *TokenExchangeClient) ClientCredentials(ctx context.Context) (*TokenExchangeResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.config.ClientID)
	data.Set("audience", c.config.Audience)

	if err := c.addClientAuth(data); err != nil {
		return nil, err
	}

	return c.postTokenRequest(ctx, data)
}
