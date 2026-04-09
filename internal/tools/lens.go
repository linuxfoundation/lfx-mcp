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

// RegisterQueryLFXLens registers the query_lfx_lens tool.
func RegisterQueryLFXLens(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "query_lfx_lens",
		Description: "Ask natural language questions about a project's data. LFX Lens covers the following domains: events, education, activity, contributors, maintainers, affiliations, organizations, project health, and project value. It can answer both straightforward text-to-SQL queries and more exploratory, multi-step data questions. Pass your question directly — Lens handles data exploration, SQL generation, and interpretation for each domain. Use search_projects first to find the project slug.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "LFX Lens Query",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleLFXLensQuery)
}

// --- Tool args ---

// LFXLensQueryArgs defines the input for query_lfx_lens.
type LFXLensQueryArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug from search_projects (e.g. 'cncf')"`
	Input       string `json:"input" jsonschema:"Natural language query about the project (e.g. 'How many active maintainers does this project have?')"`
}

// lensWorkflowRequest is the JSON body sent to the Lens workflow endpoint.
type lensWorkflowRequest struct {
	Input          string                 `json:"input"`
	AdditionalData lensWorkflowAdditional `json:"additional_data"`
}

type lensWorkflowAdditional struct {
	Foundation lensFoundation `json:"foundation"`
}

type lensFoundation struct {
	Slug string `json:"slug"`
}

// lensQueryResponse is the JSON response from the Lens query endpoint.
type lensQueryResponse struct {
	Content   string `json:"content"`
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
}

// --- Tool handlers ---

func handleLFXLensQuery(ctx context.Context, req *mcp.CallToolRequest, args LFXLensQueryArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	// Staff check — LFX Lens is staff-only.
	if err := RequireLFStaff(req); err != nil {
		return nil, nil, err
	}

	// Project-level authorization — auditor relation required.
	ctx, err := lensConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationAuditor)
	if err != nil {
		return nil, nil, err
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
		"user_id":         req.Extra.TokenInfo.UserID,
		"session_id":      req.Extra.TokenInfo.UserID + "-" + time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("Lens API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("Lens service returned status %d: %s", statusCode, string(body))
	}

	var resp lensQueryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("failed to parse Lens response: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: resp.Content}},
	}, nil, nil
}
