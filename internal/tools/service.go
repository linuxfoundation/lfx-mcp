// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ClaimLFStaff is the namespaced JWT claim that indicates the user is LF staff.
// This claim is set by the CustomClaims Auth0 Action based on LDAP group
// membership (lf-staff group). Uses the lfxPrefix namespace per convention.
const ClaimLFStaff = "http://lfx.dev/claims/lf_staff"

// Relation constants for V2 access-check. These are the service API equivalents
// of ScopeRead / ScopeManage — they define what project-level relationship is
// required, but enforcement happens inside the tool handler via the V2
// access-check endpoint (not at dispatch like MCP scopes).
const (
	// RelationWriter is required for tools that mutate project resources
	// (e.g., member onboarding).
	RelationWriter = "writer"

	// RelationAuditor is required for tools that read privileged project
	// data (e.g., LFX Lens analytics).
	RelationAuditor = "auditor"
)

// --- Shared service tool infrastructure ---

// ServiceAuth holds the shared infrastructure needed by all service API tools
// for token exchange, slug resolution, and access-check. Both [OnboardingConfig]
// and [LensConfig] embed this struct.
type ServiceAuth struct {
	LFXAPIURL           string
	TokenExchangeClient *lfxv2.TokenExchangeClient
	DebugLogger         *slog.Logger
	SlugResolver        *lfxv2.SlugResolver
	AccessChecker       *lfxv2.AccessCheckClient
}

// AuthorizeProject performs the standard service tool authorization flow:
//  1. Extract MCP token from the request
//  2. Create V2 API clients (with token exchange)
//  3. Resolve the project slug to a V2 UUID
//  4. Verify the user has the required relation via access-check
//
// On success it returns the enriched context (with MCP token attached).
// On failure it returns an error — the MCP SDK automatically wraps returned
// errors into a CallToolResult with IsError set, so callers can propagate
// the error directly as the handler's error return value.
func (s *ServiceAuth) AuthorizeProject(ctx context.Context, req *mcp.CallToolRequest, slug, relation string) (context.Context, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	// Extract MCP token.
	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.Error("failed to extract MCP token", "error", err)
		return ctx, fmt.Errorf("failed to extract MCP token: %w", err)
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	// Create V2 clients.
	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           s.LFXAPIURL,
		TokenExchangeClient: s.TokenExchangeClient,
		DebugLogger:         s.DebugLogger,
	})
	if err != nil {
		logger.Error("failed to create V2 clients", "error", err)
		return ctx, fmt.Errorf("failed to create V2 clients: %w", err)
	}

	// Resolve slug → UUID.
	projectUUID, err := s.SlugResolver.Resolve(ctx, clients, slug)
	if err != nil {
		logger.Error("failed to resolve project slug", "error", err, "slug", slug)
		return ctx, slugResolveError(slug, err)
	}

	// Get exchanged V2 token for access-check.
	v2Token, err := clients.GetExchangedToken(ctx)
	if err != nil {
		logger.Error("failed to get V2 token", "error", err)
		return ctx, fmt.Errorf("failed to get V2 token: %w", err)
	}

	// Check project access.
	if err := s.AccessChecker.CheckProjectAccess(ctx, v2Token, projectUUID, relation); err != nil {
		logger.Warn("access denied", "error", err, "slug", slug, "relation", relation)
		return ctx, err
	}

	return ctx, nil
}

// --- Helpers ---

// IsLFStaff returns true if the authenticated user has the lf_staff custom
// claim set to true in their JWT. This claim is injected by an Auth0 Action
// based on LDAP group membership (lf-staff group).
func IsLFStaff(tokenInfo *auth.TokenInfo) bool {
	if tokenInfo == nil || tokenInfo.Extra == nil {
		return false
	}
	staff, ok := tokenInfo.Extra[ClaimLFStaff].(bool)
	return ok && staff
}
