// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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
// configured list is intentional and allowed; enforcement at dispatch time is
// independent of what is advertised.
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

// AddToolWithScopes registers a tool on the server and wraps its handler so
// that the caller's JWT scopes are checked before the handler is invoked.
//
// requiredScopes lists the scopes the caller must possess (any one is
// sufficient). If the caller's token is missing all of the required scopes the
// tool returns a structured IsError result rather than a JSON-RPC error, which
// keeps the failure inside the MCP tool-call protocol.
//
// When no TokenInfo is present (e.g. stdio transport with no auth) the scope
// check is skipped so that local development is not blocked.
func AddToolWithScopes[In, Out any](
	server *mcp.Server,
	tool *mcp.Tool,
	requiredScopes []string,
	handler mcp.ToolHandlerFor[In, Out],
) {
	wrapped := func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, Out, error) {
		// Skip enforcement when there is no auth context (e.g. stdio mode).
		if req.Extra != nil && req.Extra.TokenInfo != nil {
			if !hasAnyScope(req.Extra.TokenInfo.Scopes, requiredScopes) {
				var zero Out
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: fmt.Sprintf(
								"Error: insufficient scope — this tool requires one of [%s]",
								strings.Join(requiredScopes, ", "),
							),
						},
					},
					IsError: true,
				}, zero, nil
			}
		}
		return handler(ctx, req, args)
	}

	mcp.AddTool(server, tool, wrapped)
}

// hasAnyScope returns true if the token's scopes contain at least one of the
// required scopes.
func hasAnyScope(tokenScopes, required []string) bool {
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

// ReadScopes returns the scope list for read-only tools.
func ReadScopes() []string {
	return []string{ScopeRead}
}

// WriteScopes returns the scope list for write tools. A token carrying the
// manage scope is sufficient; the read scope is not required.
func WriteScopes() []string {
	return []string{ScopeManage}
}
