// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// b2bOrgResourceType is the resource type filter for B2B org queries.
const b2bOrgResourceType = "b2b_org"

// SearchB2bOrgsArgs defines the input parameters for the search_b2b_orgs tool.
type SearchB2bOrgsArgs struct {
	SearchName string `json:"search_name,omitempty" jsonschema:"Search B2B organizations by name or domain (typeahead)."`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)."`
	PageToken  string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response."`
}

// b2bOrgSearchResult is the output type for the search_b2b_orgs tool.
type b2bOrgSearchResult struct {
	Resources []*querysvc.Resource `json:"resources"`
	PageToken *string              `json:"page_token,omitempty"`
}

// RegisterSearchB2bOrgs registers the search_b2b_orgs tool with the MCP server.
func RegisterSearchB2bOrgs(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_b2b_orgs",
		Description: "Search and list B2B organizations. Use this tool when users ask about B2B orgs, member companies, or organizations across LFX. Supports search_name for name and domain typeahead. Uses cursor-based pagination via page_token.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search B2B Orgs",
			ReadOnlyHint: true,
		},
	}, handleSearchB2bOrgs)
}

// handleSearchB2bOrgs implements the search_b2b_orgs tool logic using the Query Service.
func handleSearchB2bOrgs(ctx context.Context, req *mcp.CallToolRequest, args SearchB2bOrgsArgs) (*mcp.CallToolResult, b2bOrgSearchResult, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member tools not configured"},
			},
			IsError: true,
		}, b2bOrgSearchResult{}, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, b2bOrgSearchResult{}, nil
	}

	ctx = memberConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := memberConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	resourceType := b2bOrgResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		PageSize: pageSize,
	}

	if args.SearchName != "" {
		payload.Name = &args.SearchName
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "searching B2B orgs", "search_name", args.SearchName, "page_size", pageSize, "page_token", args.PageToken)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources (b2b_org) failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to search B2B orgs", err)},
			},
			IsError: true,
		}, b2bOrgSearchResult{}, nil
	}

	out := b2bOrgSearchResult{
		Resources: result.Resources,
		PageToken: result.PageToken,
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal search result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, b2bOrgSearchResult{}, nil
	}

	logger.InfoContext(ctx, "search_b2b_orgs succeeded", "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, out, nil
}
