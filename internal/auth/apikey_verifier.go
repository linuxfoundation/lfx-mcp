// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package auth provides JWT verification with JWKS caching for the LFX MCP server.
package auth

// APIKeyVerifier validates static API-key credentials passed as Authorization: Bearer
// tokens, and synthesises auth.TokenInfo equivalent to an OAuth2 M2M
// client_credentials token.
//
// TEMPORARY: This is an intentional stop-gap for MCP clients that cannot complete a
// full OAuth2 authorization code flow. It MUST be removed once all clients support
// proper OAuth2.
//
// Authenticated requests carry Extra["api_key_auth"]=true in their TokenInfo. The
// lfxv2 token-exchange layer checks this marker to select the client_credentials
// grant instead of attempting to parse and exchange the bearer token as a JWT.

import (
	"context"
	"strings"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// M2MScopes are the scopes granted to every API-key-authenticated request.
// They mirror what an OAuth2 M2M client_credentials token carries.
var M2MScopes = []string{"read:all", "manage:all"}

// APIKeyVerifier validates Authorization: Bearer header values against a static map
// of consumer-key → shared-secret pairs.
//
// TEMPORARY — see package-level comment.
type APIKeyVerifier struct {
	// credentials maps API consumer key identifiers to their shared-secret values.
	credentials map[string]string
}

// NewAPIKeyVerifier creates a new verifier from the supplied key→secret map.
// Returns nil when credentials is empty so callers can skip wiring it up entirely.
//
// TEMPORARY — see package-level comment.
func NewAPIKeyVerifier(credentials map[string]string) *APIKeyVerifier {
	if len(credentials) == 0 {
		return nil
	}
	return &APIKeyVerifier{credentials: credentials}
}

// VerifyAPIKey checks whether the Authorization: Bearer value in the request matches
// a known static secret.
//
// Return semantics:
//   - (TokenInfo, true, nil)        — secret present and valid; caller should proceed.
//   - (nil, true, ErrInvalidToken)  — bearer value present but not a known secret;
//     caller must not fall through to JWT verification.
//   - (nil, false, nil)             — no Authorization header present; caller should
//     try the next verifier.
//
// The verifier is called from within the existing verifyToken closure, which already
// receives the raw bearer value from RequireBearerToken. Callers should invoke this
// before attempting JWT parsing.
//
// TEMPORARY — see package-level comment.
func (v *APIKeyVerifier) VerifyAPIKey(_ context.Context, bearerValue string) (*sdkauth.TokenInfo, bool, error) {
	if bearerValue == "" {
		return nil, false, nil
	}

	// Reject anything that looks like a JWT so that real tokens always reach the
	// JWT verifier rather than getting a spurious "invalid key" error.
	if looksLikeJWT(bearerValue) {
		return nil, false, nil
	}

	// Validate against known credentials.
	for consumerKey, knownSecret := range v.credentials {
		if knownSecret == bearerValue {
			return buildTokenInfo(consumerKey), true, nil
		}
	}

	// Bearer value was present and not a JWT, but didn't match any known secret.
	return nil, true, sdkauth.ErrInvalidToken
}

// APIKeyAuthExtraKey is the key used in TokenInfo.Extra to signal that the request
// was authenticated via a static API key rather than a bearer JWT. The lfxv2
// token-exchange layer checks this marker to select the client_credentials grant.
const APIKeyAuthExtraKey = "api_key_auth"

// buildTokenInfo constructs a TokenInfo for a successfully authenticated API-key request.
// Extra[APIKeyAuthExtraKey]=true signals to the lfxv2 token-exchange layer that it
// should use the client_credentials grant rather than attempting to exchange the
// bearer value as a JWT.
//
// TEMPORARY — see package-level comment.
func buildTokenInfo(consumerKey string) *sdkauth.TokenInfo {
	return &sdkauth.TokenInfo{
		UserID:     consumerKey,
		Scopes:     M2MScopes,
		Expiration: time.Now().Add(time.Hour), // arbitrary; re-evaluated each request
		Extra: map[string]any{
			// Signals to lfxv2.getOrExchangeToken to use the client_credentials grant.
			APIKeyAuthExtraKey: true,
		},
	}
}

// looksLikeJWT returns true when s has the structural signature of a compact-
// serialised JWT: exactly two '.' separators (header.payload.signature).
func looksLikeJWT(s string) bool {
	return strings.Count(s, ".") == 2
}
