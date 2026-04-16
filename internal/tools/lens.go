// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/linuxfoundation/lfx-mcp/internal/serviceapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// LensConfig holds configuration shared by LFX Lens tools.
type LensConfig struct {
	ServiceAuth
	ServiceClient *serviceapi.Client
}

var lensConfig *LensConfig

// SetLensConfig sets the configuration for LFX Lens tools.
func SetLensConfig(cfg *LensConfig) {
	lensConfig = cfg
}

// --- Tool registration ---

// RegisterResolveSlugLFXLens registers the resolve_slug_lfx_lens tool.
func RegisterResolveSlugLFXLens(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "resolve_slug_lfx_lens",
		Description: "Resolve a project name to its canonical slug using fuzzy matching. Returns the top 3 matches ranked by similarity. Use this when you have a project name but need the exact slug for other LFX Lens tools.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Resolve Slug (LFX Lens)",
			ReadOnlyHint: true,
		},
	}, handleResolveLensSlug)
}

// RegisterQueryLFXLens registers the query_lfx_lens tool.
func RegisterQueryLFXLens(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_lfx_lens",
		Description: "Ask natural language questions about a project's data. LFX Lens covers the following domains: events, education, activity, contributors, maintainers, affiliations, organizations, project health, and project value. It can answer both straightforward text-to-SQL queries and more exploratory, multi-step data questions. Pass your question directly — Lens handles data exploration, SQL generation, and interpretation for each domain. Use resolve_slug_lfx_lens first to find the project slug.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "LFX Lens Query",
			ReadOnlyHint: true,
		},
	}, handleLFXLensQuery)
}

// --- Tool args ---

// ResolveLensSlugArgs defines the input for resolve_slug_lfx_lens.
type ResolveLensSlugArgs struct {
	Name string `json:"name" jsonschema:"Project name to resolve into a slug (e.g. 'Kubernetes', 'Linux Kernel')"`
}

// QueryLFXLensArgs defines the input for query_lfx_lens.
type QueryLFXLensArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug from search_projects (e.g. 'cncf')"`
	Input       string `json:"input" jsonschema:"Natural language query about the project (e.g. 'How many active maintainers does this project have?')"`
}

// lensWorkflowAdditional is the additional_data payload sent to the Lens workflow endpoint.
type lensWorkflowAdditional struct {
	Foundation lensFoundation `json:"foundation"`
}

// lensFoundation holds the project slug for the Lens workflow request.
type lensFoundation struct {
	Slug string `json:"slug"`
}

// lensQueryResponse is the JSON response from the Lens query endpoint.
type lensQueryResponse struct {
	Content   string `json:"content"`
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
}

// lensSlugMatch is a single fuzzy-match result from the resolve-slug endpoint.
type lensSlugMatch struct {
	Slug       string  `json:"slug"`
	Name       string  `json:"name"`
	Similarity float64 `json:"similarity"`
}

// lensResolveSlugResponse is the JSON response from the resolve-slug endpoint.
type lensResolveSlugResponse struct {
	Query   string          `json:"query"`
	Matches []lensSlugMatch `json:"matches"`
}

// --- Tool handlers ---

func handleLFXLensQuery(ctx context.Context, req *mcp.CallToolRequest, args QueryLFXLensArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	// No project-level access-check: the tool is already restricted to LF
	// staff at registration time (isStaff gate in newServer), and LF staff
	// are being granted auditor on all projects. The self-service auditor
	// relation check is omitted because it does not exist for non-onboarded
	// projects, which would incorrectly block legitimate staff queries.

	// Derive a user ID for the Lens API. In stdio mode TokenInfo is nil
	// (unauthenticated local use), so fall back to a stable anonymous value.
	userID := AnonymousUserID
	if req.Extra.TokenInfo != nil && req.Extra.TokenInfo.UserID != "" {
		userID = req.Extra.TokenInfo.UserID
	}

	additionalData, err := json.Marshal(lensWorkflowAdditional{
		Foundation: lensFoundation{Slug: args.ProjectSlug},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal additional_data: %w", err)
	}

	body, statusCode, err := lensConfig.ServiceClient.PostMultipart(ctx, "/lfx-lens/mcp/query", map[string]string{
		"message":         args.Input,
		"additional_data": string(additionalData),
		"user_id":         userID,
		"session_id":      userID + "-" + time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("lens API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("lens service returned status %d: %s", statusCode, string(body))
	}

	var resp lensQueryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("failed to parse Lens response: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: resp.Content}},
	}, nil, nil
}

func handleResolveLensSlug(ctx context.Context, req *mcp.CallToolRequest, args ResolveLensSlugArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	body, statusCode, err := lensConfig.ServiceClient.PostMultipart(ctx, "/lfx-lens/resolve-slug", map[string]string{
		"name": args.Name,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("lens API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("lens service returned status %d: %s", statusCode, string(body))
	}

	var resp lensResolveSlugResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("failed to parse resolve-slug response: %w", err)
	}

	// Format matches as readable text for the LLM.
	var text string
	if len(resp.Matches) == 0 {
		text = fmt.Sprintf("No matches found for %q.", args.Name)
	} else {
		text = fmt.Sprintf("Top matches for %q:\n", args.Name)
		for i, m := range resp.Matches {
			text += fmt.Sprintf("%d. slug=%q  name=%q  similarity=%.1f\n", i+1, m.Slug, m.Name, m.Similarity)
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, nil, nil
}
