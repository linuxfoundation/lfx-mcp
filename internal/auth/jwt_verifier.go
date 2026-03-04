// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package auth provides JWT verification with JWKS caching for the LFX MCP server.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// JWTVerifierConfig holds configuration for JWT verification.
type JWTVerifierConfig struct {
	// AuthServers is the list of authorization server URLs (e.g., ["https://example.auth0.com"]).
	// JWKS will be fetched from {authServer}/.well-known/jwks.json for each server.
	AuthServers []string

	// Audience is the expected audience claim (aud) in the JWT.
	Audience string

	// HTTPClient is the HTTP client to use for fetching JWKS.
	// If nil, a default client with 30s timeout will be created.
	HTTPClient *http.Client

	// CacheRefreshInterval is how often to refresh the JWKS cache.
	// If zero, defaults to 15 minutes.
	CacheRefreshInterval time.Duration
}

// JWTVerifier verifies JWT tokens using cached JWKS from authorization servers.
type JWTVerifier struct {
	config    JWTVerifierConfig
	jwksCache *jwk.Cache
}

// NewJWTVerifier creates a new JWT verifier with JWKS caching.
func NewJWTVerifier(cfg JWTVerifierConfig) (*JWTVerifier, error) {
	if len(cfg.AuthServers) == 0 {
		return nil, fmt.Errorf("at least one auth server is required")
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

	refreshInterval := cfg.CacheRefreshInterval
	if refreshInterval == 0 {
		refreshInterval = 15 * time.Minute
	}

	// Create JWKS cache with automatic refresh.
	cache := jwk.NewCache(context.Background())

	// Register each auth server's JWKS endpoint.
	for _, authServer := range cfg.AuthServers {
		authServer = strings.TrimSuffix(authServer, "/")
		jwksURL := authServer + "/.well-known/jwks.json"

		if err := cache.Register(jwksURL, jwk.WithMinRefreshInterval(refreshInterval), jwk.WithHTTPClient(httpClient)); err != nil {
			return nil, fmt.Errorf("failed to register JWKS endpoint %s: %w", jwksURL, err)
		}
	}

	return &JWTVerifier{
		config:    cfg,
		jwksCache: cache,
	}, nil
}

// VerifyToken verifies a JWT token and returns the parsed token.
func (v *JWTVerifier) VerifyToken(ctx context.Context, tokenString string) (jwt.Token, error) {
	// Parse token to get the issuer for JWKS lookup.
	unverifiedToken, err := jwt.ParseString(tokenString, jwt.WithVerify(false), jwt.WithValidate(false))
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	issuer := unverifiedToken.Issuer()
	if issuer == "" {
		return nil, fmt.Errorf("token missing issuer claim")
	}

	// Normalize issuer (remove trailing slash).
	issuer = strings.TrimSuffix(issuer, "/")

	// Check if issuer is in our list of auth servers.
	var jwksURL string
	for _, authServer := range v.config.AuthServers {
		normalizedServer := strings.TrimSuffix(authServer, "/")
		if issuer == normalizedServer {
			jwksURL = issuer + "/.well-known/jwks.json"
			break
		}
	}

	if jwksURL == "" {
		return nil, fmt.Errorf("token issuer %s not in configured auth servers", issuer)
	}

	// Fetch JWKS from cache.
	keySet, err := v.jwksCache.Get(ctx, jwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS from %s: %w", jwksURL, err)
	}

	// Parse and verify token with JWKS.
	token, err := jwt.ParseString(
		tokenString,
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithAudience(v.config.Audience),
	)
	if err != nil {
		return nil, fmt.Errorf("token verification failed: %w", err)
	}

	return token, nil
}

// ExtractScopes extracts scopes from a JWT token.
// Handles both "scope" (space-separated string) and "scopes" (array) claims.
func ExtractScopes(token jwt.Token) []string {
	// Try "scope" claim (space-separated string).
	if scopeClaim, ok := token.Get("scope"); ok {
		if scopeStr, ok := scopeClaim.(string); ok {
			return strings.Fields(scopeStr)
		}
	}

	// Try "scopes" claim (array).
	if scopesClaim, ok := token.Get("scopes"); ok {
		switch v := scopesClaim.(type) {
		case []string:
			return v
		case []interface{}:
			scopes := make([]string, 0, len(v))
			for _, s := range v {
				if str, ok := s.(string); ok {
					scopes = append(scopes, str)
				}
			}
			return scopes
		}
	}

	return nil
}
