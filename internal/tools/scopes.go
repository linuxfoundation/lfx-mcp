// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import "strings"

// Scope constants used to gate tool access based on the caller's JWT scopes.
// These MUST match the scopes defined on the Auth0 resource server for the
// LFX MCP API (see auth0-terraform resource_servers.tf, lfx_mcp_api).
const (
	// ScopeRead is required for tools that only read data (ReadOnlyHint == true).
	ScopeRead = "read:all"

	// ScopeManage is required for tools that mutate data (ReadOnlyHint defaults to false).
	ScopeManage = "manage:all"
)

// ClaimClientID is the JWT claim holding the OAuth client identifier. For
// clients registered via a client ID metadata document (CIMD), the value is
// the metadata document URL rather than an opaque identifier.
const ClaimClientID = "client_id"

// scopeBlindClientIDPrefixes lists client ID prefixes for OAuth clients that
// ignore the scopes advertised in our protected resource metadata and in the
// WWW-Authenticate challenge, and therefore never request read:all or
// manage:all.
//
// Codex derives its scope request from the authorization server's
// scopes_supported instead, which cannot contain resource scopes. The result is
// a valid token carrying no MCP scope, which would otherwise register no tools
// and surface to the user as an empty connector with no error.
//
// Only the prefix is matched: the path segment after it is a per-server
// callback ID that differs between environments.
var scopeBlindClientIDPrefixes = []string{
	"https://chatgpt.com/oauth/codex/",
}

// IsScopeBlindClient reports whether clientID belongs to a client known to
// ignore advertised scopes. Callers use this to decide whether a token carrying
// no MCP scope should be treated as having requested the advertised set.
func IsScopeBlindClient(clientID string) bool {
	for _, prefix := range scopeBlindClientIDPrefixes {
		if strings.HasPrefix(clientID, prefix) {
			return true
		}
	}
	return false
}

// DefaultScopes returns the set of scopes the server advertises via the OAuth
// Protected Resource Metadata endpoint, plus the standard OIDC scopes clients
// need for the authorization flow. Both read:all and manage:all are
// advertised: write tools are registered for every authenticated caller
// holding at least read:all, but their handlers require manage:all at call
// time and respond with a step-up error when it is missing. Advertising
// manage:all here lets a client request both scopes up front if it chooses
// to, or discover that manage:all exists at all — the per-call step-up error
// (not the PRM) is what tells a caller which scope it is missing for a
// specific tool.
//
// This is also the fallback assumed for a scope-blind client (see
// IsScopeBlindClient) that presents a token carrying no MCP scope at all:
// such a client offers no consent UI to choose scopes from, so it is treated
// like any other client requesting the full advertised set, matching the
// default behavior every compliant client gets by requesting both scopes up
// front. Users rely on per-call "ask" policies for write tools regardless of
// how the scope was granted, not on withholding manage:all here.
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

// ManageScopeTools is the set of tool names whose handlers mutate data and
// therefore require manage:all at call time. These tools are registered for
// every caller holding at least read:all (see newServer in main.go) rather
// than hidden from tools/list — clients can discover the tool and its schema
// up front, and only need to complete an OAuth step-up for manage:all when
// they actually attempt to call one. Enforcement happens in
// requireManageScopeHTTP (cmd/lfx-mcp-server/main.go), which returns an HTTP
// 403 with an insufficient_scope challenge for a caller lacking manage:all,
// instead of invoking the handler.
var ManageScopeTools = map[string]bool{
	"create_committee":              true,
	"update_committee":              true,
	"update_committee_settings":     true,
	"delete_committee":              true,
	"create_committee_member":       true,
	"update_committee_member":       true,
	"delete_committee_member":       true,
	"create_membership_key_contact": true,
	"update_membership_key_contact": true,
	"delete_membership_key_contact": true,
	"list_discord_roles":            true,
	"find_discord_role":             true,
	"find_discord_user":             true,
	"check_discord_user_role":       true,
	"assign_discord_role":           true,
	"list_email_templates":          true,
	"send_email":                    true,
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
