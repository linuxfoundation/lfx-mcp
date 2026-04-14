// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

// Scope constants used to gate tool access based on the caller's JWT scopes.
// These MUST match the scopes defined on the Auth0 resource server for the
// LFX MCP API (see auth0-terraform resource_servers.tf, lfx_mcp_api).
const (
	// ScopeRead is required for tools that only read data (ReadOnlyHint == true).
	ScopeRead = "read:all"

	// ScopeManage is required for tools that mutate data (ReadOnlyHint defaults to false).
	ScopeManage = "manage:all"
)

// DefaultScopes returns the set of scopes the server advertises via the OAuth
// Protected Resource Metadata endpoint. This is the enforced set plus the
// standard OIDC scopes that clients typically need for the authorization flow.
func DefaultScopes() []string {
	return []string{"openid", "profile", "email", ScopeRead, ScopeManage}
}

// ValidateScopes checks a configured scope list for unrecognised entries and
// returns it unchanged. It logs a warning for any scope that is neither an
// enforced scope nor a standard OIDC scope — those will be advertised via the
// PRM but are not enforced by the server. Omitting an enforced scope from the
// configured list is intentional and allowed; enforcement at registration time
// is independent of what is advertised.
func ValidateScopes(configured []string, warn func(msg string, args ...any)) []string {
	known := map[string]struct{}{
		"openid":    {},
		"profile":   {},
		"email":     {},
		ScopeRead:   {},
		ScopeManage: {},
	}

	for _, s := range configured {
		if _, ok := known[s]; !ok {
			warn("unrecognised scope in configuration — it will be advertised but is not enforced by the server", "scope", s)
		}
	}

	return configured
}

// HasAnyScope returns true if tokenScopes contains at least one of the
// required scopes.
func HasAnyScope(tokenScopes, required []string) bool {
	set := make(map[string]struct{}, len(tokenScopes))
	for _, s := range tokenScopes {
		set[s] = struct{}{}
	}
	for _, r := range required {
		if _, ok := set[r]; ok {
			return true
		}
	}
	return false
}
