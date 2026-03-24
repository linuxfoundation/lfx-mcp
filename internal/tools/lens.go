// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

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

// RegisterLFXLensQuery registers the lfx_lens_query tool.
func RegisterLFXLensQuery(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "lfx_lens_query",
		Description: "Query LFX Lens analytics for a project. Requires LF staff access and auditor relation to the project. Use search_projects first to find the project slug.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "LFX Lens Query",
			ReadOnlyHint: true,
		},
	}, handleLFXLensQuery)
}

// --- Tool args ---

// LFXLensQueryArgs defines the input for lfx_lens_query.
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

// --- Tool handlers ---

func handleLFXLensQuery(ctx context.Context, req *mcp.CallToolRequest, args LFXLensQueryArgs) (*mcp.CallToolResult, any, error) {
	if lensConfig == nil {
		return toolError("LFX Lens tools not configured"), nil, nil
	}

	// Staff check — LFX Lens is staff-only.
	if errResult := RequireLFStaff(req); errResult != nil {
		return errResult, nil, nil
	}

	// Project-level authorization — auditor relation required.
	ctx, errResult := lensConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationAuditor)
	if errResult != nil {
		return errResult, nil, nil
	}

	// TODO: Proxy to Lens service API once Auth0 resource server is deployed.
	// The actual call will be:
	//
	//   POST /workflows/lfx-lens-mcp-workflow/runs (multipart/form-data)
	//   Fields: message (string), additional_data (JSON: {"foundation": {"slug": "<slug>"}}), stream ("false")
	//   Authorization: Bearer <m2m_token>
	//
	// Response: {"content": "...", "content_type": "str", "status": "COMPLETED"}
	//
	// payload := lensWorkflowRequest{
	// 	Input: args.Input,
	// 	AdditionalData: lensWorkflowAdditional{
	// 		Foundation: lensFoundation{Slug: args.ProjectSlug},
	// 	},
	// }
	// body, statusCode, err := lensConfig.ServiceClient.PostJSON(ctx, "/workflows/lfx-lens-mcp-workflow/runs", payload)
	// if err != nil {
	// 	return toolError("Lens API call failed: %v", err), nil, nil
	// }
	// if statusCode != http.StatusOK {
	// 	return toolError("Lens service returned status %d: %s", statusCode, string(body)), nil, nil
	// }

	_ = ctx // used by actual API call

	dummyResponse := map[string]string{
		"content":      fmt.Sprintf("[dry-run] LFX Lens query for project %q: %s", args.ProjectSlug, args.Input),
		"content_type": "str",
		"status":       "COMPLETED",
	}
	body, _ := json.Marshal(dummyResponse)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}
