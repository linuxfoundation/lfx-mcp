// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
//
// This file defines "full view": which callers receive people records exactly
// as the upstream services return them, and how that decision reaches tool
// handlers. Every other caller gets what LFX Self Serve renders on screen to
// that same person (see people_visibility.go).
package tools

import (
	"context"
	"strings"

	lfxauth "github.com/linuxfoundation/lfx-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

// ClaimEmail is the namespaced JWT claim carrying the user's e-mail address.
// The CustomClaims Auth0 Action sets it for the MCP audience alongside
// ClaimLFStaff; it is the same address LFX Self Serve matches registrant and
// participant records on.
const ClaimEmail = "http://lfx.dev/claims/email"

// IsStaffCaller reports whether tokenInfo is treated as LF staff for tool
// registration: no token at all (stdio, or HTTP with no auth servers), the
// lf_staff claim, or a machine (client-credentials) account. It is the single
// expression newServer's isStaff is written in terms of.
func IsStaffCaller(tokenInfo *auth.TokenInfo) bool {
	return tokenInfo == nil || IsLFStaff(tokenInfo) || IsMachineAccount(tokenInfo)
}

// IsAPIKeyCaller reports whether the request was authenticated with a static
// API key (lfxauth.APIKeyAuthExtraKey). Such callers are LF-configured service
// integrations that act upstream with the M2M token.
func IsAPIKeyCaller(tokenInfo *auth.TokenInfo) bool {
	if tokenInfo == nil || tokenInfo.Extra == nil {
		return false
	}
	isAPIKey, _ := tokenInfo.Extra[lfxauth.APIKeyAuthExtraKey].(bool)
	return isAPIKey
}

// IsFullViewCaller reports whether tokenInfo receives people records as the
// upstream services return them. Staff callers do; so do static API-key
// callers, whose M2M principal holds no relations of its own and would
// otherwise match nothing. Everyone else gets the LFX Self Serve on-screen
// view. API-key callers are deliberately not staff for registration: the
// staff-only Lens tools stay absent for them.
func IsFullViewCaller(tokenInfo *auth.TokenInfo) bool {
	return IsStaffCaller(tokenInfo) || IsAPIKeyCaller(tokenInfo)
}

// fullViewContextKey is the unexported key type for the full-view flag.
type fullViewContextKey struct{}

// WithFullView returns a context carrying the caller's full-view decision.
// newServer's receiving middleware sets it from the same token that drove tool
// registration, so handlers have one source of truth per request.
func WithFullView(ctx context.Context, full bool) context.Context {
	return context.WithValue(ctx, fullViewContextKey{}, full)
}

// HasFullView reports the full-view flag stored by WithFullView. A context
// without the flag reports false: a handler reached without the middleware
// narrows what it returns rather than widening it.
func HasFullView(ctx context.Context) bool {
	full, ok := ctx.Value(fullViewContextKey{}).(bool)
	return ok && full
}

// callerUsername returns the LFX username from the caller's token, or "" when
// there is none (stdio, machine accounts without the claim, API keys).
func callerUsername(tokenInfo *auth.TokenInfo) string {
	if tokenInfo == nil || tokenInfo.Extra == nil {
		return ""
	}
	username, _ := tokenInfo.Extra["username"].(string)
	return username
}

// callerEmail returns the e-mail address from the caller's token (ClaimEmail),
// trimmed, or "" when there is none. Callers compare it case-insensitively.
func callerEmail(tokenInfo *auth.TokenInfo) string {
	if tokenInfo == nil || tokenInfo.Extra == nil {
		return ""
	}
	email, _ := tokenInfo.Extra[ClaimEmail].(string)
	return strings.TrimSpace(email)
}
