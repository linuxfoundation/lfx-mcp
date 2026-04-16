// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	mcp.AddTool(server, &mcp.Tool{
		Name: "query_lfx_lens",
		Description: `Ask natural language questions about a project's data. LFX Lens covers: events, education, activity, contributors, maintainers, affiliations, organizations, project health, and project value. It handles data exploration, SQL generation and execution. Use search_projects first to find the project slug.

This tool works in two modes:
1. START a query: pass project_slug and input. Returns run_id, session_id, and status=PENDING.
2. POLL for results: pass run_id and session_id from the start response. Returns status and content.

After starting a query, wait 10 seconds, then call this tool again with run_id and session_id to poll. If status is PENDING or RUNNING, wait 5 seconds and poll again. If status is COMPLETED, the content field has the answer. If status is ERROR, the content field has the error details.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "LFX Lens Query",
			ReadOnlyHint: true,
		},
	}, handleLFXLensQuery)
}

// --- Tool args ---

// QueryLFXLensArgs defines the input for query_lfx_lens.
// To start a query: provide project_slug and input.
// To poll for results: provide run_id and session_id.
type QueryLFXLensArgs struct {
	ProjectSlug string `json:"project_slug,omitempty" jsonschema:"Project slug from search_projects (e.g. 'cncf'). Required to start a query."`
	Input       string `json:"input,omitempty" jsonschema:"Natural language query about the project. Required to start a query."`
	RunID       string `json:"run_id,omitempty" jsonschema:"Run ID from a previous call. Pass this to poll for results."`
	SessionID   string `json:"session_id,omitempty" jsonschema:"Session ID from a previous call. Pass this to poll for results."`
}

// lensWorkflowAdditional is the additional_data payload sent to the Lens workflow endpoint.
type lensWorkflowAdditional struct {
	Foundation lensFoundation `json:"foundation"`
}

// lensFoundation holds the project slug for the Lens workflow request.
type lensFoundation struct {
	Slug string `json:"slug"`
}

// lensQueryResponse is the unified JSON response from start and poll endpoints.
type lensQueryResponse struct {
	Content    string `json:"content,omitempty"`
	Status     string `json:"status"`
	SessionID  string `json:"session_id"`
	RunID      string `json:"run_id,omitempty"`
	WorkflowID string `json:"workflow_id,omitempty"`
}

// --- Tool handlers ---

func handleLFXLensQuery(ctx context.Context, req *mcp.CallToolRequest, args QueryLFXLensArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return nil, nil, fmt.Errorf("LFX Lens tools not configured")
	}

	// Poll mode: run_id and session_id provided
	if args.RunID != "" && args.SessionID != "" {
		return pollLFXLensQuery(ctx, args.RunID, args.SessionID)
	}

	// Start mode: project_slug and input required
	if args.ProjectSlug == "" || args.Input == "" {
		return nil, nil, fmt.Errorf("project_slug and input are required to start a query, or run_id and session_id to poll")
	}

	// Derive a user ID for the Lens API.
	userID := AnonymousUserID
	if req.Extra.TokenInfo != nil && req.Extra.TokenInfo.UserID != "" {
		userID = req.Extra.TokenInfo.UserID
	}

	sessionID := userID + "-" + time.Now().UTC().Format("2006-01-02T15:04:05Z")

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
		"session_id":      sessionID,
		"background":      "true",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("lens API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("lens service returned status %d: %s", statusCode, string(body))
	}

	var resp lensQueryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("failed to parse Lens start response: %w", err)
	}

	text := fmt.Sprintf("Query started. Status: %s\nrun_id: %s\nsession_id: %s\n\nWait 10 seconds, then call this tool again with run_id and session_id to get the results.",
		resp.Status, resp.RunID, resp.SessionID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, nil, nil
}

// pollLFXLensQuery polls for the result of a background workflow run.
func pollLFXLensQuery(ctx context.Context, runID, sessionID string) (*mcp.CallToolResult, any, error) {
	pollPath := fmt.Sprintf("/lfx-lens/mcp/query/poll/%s", runID)
	pollQuery := url.Values{"session_id": {sessionID}}

	body, statusCode, err := lensConfig.ServiceClient.Get(ctx, pollPath, pollQuery)
	if err != nil {
		return nil, nil, fmt.Errorf("lens poll request failed: %w", err)
	}
	if statusCode == http.StatusNotFound {
		return nil, nil, fmt.Errorf("run not found: %s", runID)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("lens poll returned status %d: %s", statusCode, string(body))
	}

	var resp lensQueryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("failed to parse poll response: %w", err)
	}

	switch resp.Status {
	case "COMPLETED":
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: resp.Content}},
		}, nil, nil
	case "ERROR":
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Workflow error: %s", resp.Content)}},
			IsError: true,
		}, nil, nil
	default:
		text := fmt.Sprintf("Status: %s\nrun_id: %s\nsession_id: %s\n\nThe query is still running. Wait 5 seconds and poll again with run_id and session_id.",
			resp.Status, runID, sessionID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, nil, nil
	}
}
