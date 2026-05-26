// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	mailinglist "github.com/linuxfoundation/lfx-v2-mailing-list-service/gen/mailing_list"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mailingListResourceType is the resource type filter for mailing list queries.
const mailingListResourceType = "groupsio_mailing_list"

// mailingListMemberResourceType is the resource type filter for mailing list member queries.
const mailingListMemberResourceType = "groupsio_member"

// MailingListConfig holds configuration shared by mailing list tools.
type MailingListConfig struct {
	// Clients is the shared LFX v2 API client instance. It must be created once
	// at startup so that its token cache persists across requests.
	Clients *lfxv2.Clients
}

var mailingListConfig *MailingListConfig

// SetMailingListConfig sets the configuration for mailing list tools.
func SetMailingListConfig(cfg *MailingListConfig) {
	mailingListConfig = cfg
}

// GetMailingListServiceArgs defines the input parameters for the get_mailing_list_service tool.
type GetMailingListServiceArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the mailing list service to retrieve"`
}

// GetMailingListArgs defines the input parameters for the get_mailing_list tool.
type GetMailingListArgs struct {
	ID string `json:"id" jsonschema:"The Groups.io numeric group ID of the mailing list to retrieve (e.g. 145670)"`
}

// GetMailingListMemberArgs defines the input parameters for the get_mailing_list_member tool.
type GetMailingListMemberArgs struct {
	MailingListID string `json:"mailing_list_id" jsonschema:"The Groups.io numeric group ID of the mailing list (e.g. 145670)"`
	MemberID      string `json:"member_id" jsonschema:"The Groups.io numeric member ID (e.g. 14875835)"`
}

// SearchMailingListMembersArgs defines the input parameters for the search_mailing_list_members tool.
type SearchMailingListMembersArgs struct {
	MailingListID string `json:"mailing_list_id,omitempty" jsonschema:"Optional Groups.io numeric group ID of the mailing list to filter members by (e.g. 145670)"`
	ProjectUID    string `json:"project_uid,omitempty" jsonschema:"Optional project UID to filter mailing list members by project"`
	Name          string `json:"name,omitempty" jsonschema:"Name or partial name of the member to search for"`
	PageSize      int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken     string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// SearchMailingListsArgs defines the input parameters for the search_mailing_lists tool.
type SearchMailingListsArgs struct {
	Name       string `json:"name,omitempty" jsonschema:"Name or partial name of the mailing list to search for"`
	ProjectUID string `json:"project_uid,omitempty" jsonschema:"Optional project UID to filter mailing lists by project"`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken  string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// RegisterGetMailingListService registers the get_mailing_list_service tool with the MCP server.
func RegisterGetMailingListService(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_mailing_list_service",
		Description: "Get a mailing list service's base info and settings by its UID. Privileged settings may be omitted if the caller lacks sufficient permissions.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Mailing List Service",
			ReadOnlyHint: true,
		},
	}, handleGetMailingListService)
}

// RegisterGetMailingList registers the get_mailing_list tool with the MCP server.
func RegisterGetMailingList(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_mailing_list",
		Description: "Get a mailing list by its Groups.io numeric group ID (e.g. 145670).",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Mailing List",
			ReadOnlyHint: true,
		},
	}, handleGetMailingList)
}

// RegisterGetMailingListMember registers the get_mailing_list_member tool with the MCP server.
func RegisterGetMailingListMember(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_mailing_list_member",
		Description: "Get a specific mailing list member by Groups.io mailing list ID and member ID.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Mailing List Member",
			ReadOnlyHint: true,
		},
	}, handleGetMailingListMember)
}

// RegisterSearchMailingListMembers registers the search_mailing_list_members tool with the MCP server.
func RegisterSearchMailingListMembers(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_mailing_list_members",
		Description: "Search for LFX mailing list members. Optionally filter by Groups.io mailing list ID, project UID, and/or name. At least one filter is recommended but not required.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Mailing List Members",
			ReadOnlyHint: true,
		},
	}, handleSearchMailingListMembers)
}

// RegisterSearchMailingLists registers the search_mailing_lists tool with the MCP server.
func RegisterSearchMailingLists(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_mailing_lists",
		Description: "Search for LFX mailing lists by name using the LFX query service. Optionally filter by project UID.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Mailing Lists",
			ReadOnlyHint: true,
		},
	}, handleSearchMailingLists)
}

// handleGetMailingListService implements the get_mailing_list_service tool logic.
func handleGetMailingListService(ctx context.Context, req *mcp.CallToolRequest, args GetMailingListServiceArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if mailingListConfig == nil {
		logger.ErrorContext(ctx, "mailing list tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: mailing list tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.UID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = mailingListConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := mailingListConfig.Clients

	logger.InfoContext(ctx, "fetching mailing list service", "uid", args.UID)

	baseResult, err := clients.MailingList.GetGroupsioService(ctx, &mailinglist.GetGroupsioServicePayload{
		ServiceID: args.UID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetGroupsioService failed", "error", err, "uid", args.UID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to get mailing list service", err)},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(baseResult, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal mailing list service result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_mailing_list_service succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMailingList implements the get_mailing_list tool logic.
func handleGetMailingList(ctx context.Context, req *mcp.CallToolRequest, args GetMailingListArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if mailingListConfig == nil {
		logger.ErrorContext(ctx, "mailing list tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: mailing list tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.ID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: id is required"},
			},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = mailingListConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := mailingListConfig.Clients

	logger.InfoContext(ctx, "fetching mailing list", "id", args.ID)

	baseResult, err := clients.MailingList.GetGroupsioMailingList(ctx, &mailinglist.GetGroupsioMailingListPayload{
		SubgroupID: args.ID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetGroupsioMailingList failed", "error", err, "id", args.ID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to get mailing list", err)},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(baseResult, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal mailing list result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_mailing_list succeeded", "id", args.ID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMailingListMember implements the get_mailing_list_member tool logic.
func handleGetMailingListMember(ctx context.Context, req *mcp.CallToolRequest, args GetMailingListMemberArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if mailingListConfig == nil {
		logger.ErrorContext(ctx, "mailing list tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: mailing list tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.MailingListID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: mailing_list_id is required"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.MemberID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member_id is required"},
			},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = mailingListConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := mailingListConfig.Clients

	logger.InfoContext(ctx, "fetching mailing list member", "mailing_list_id", args.MailingListID, "member_id", args.MemberID)

	result, err := clients.MailingList.GetGroupsioMember(ctx, &mailinglist.GetGroupsioMemberPayload{
		SubgroupID: args.MailingListID,
		MemberID:   args.MemberID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetGroupsioMember failed", "error", err, "mailing_list_id", args.MailingListID, "member_id", args.MemberID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to get mailing list member", err)},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal mailing list member result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_mailing_list_member succeeded", "mailing_list_id", args.MailingListID, "member_id", args.MemberID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleSearchMailingLists implements the search_mailing_lists tool logic.
func handleSearchMailingLists(ctx context.Context, req *mcp.CallToolRequest, args SearchMailingListsArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if mailingListConfig == nil {
		logger.ErrorContext(ctx, "mailing list tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: mailing list tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = mailingListConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := mailingListConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	resourceType := mailingListResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		PageSize: pageSize,
		Sort:     "name_asc",
	}

	if args.Name != "" {
		payload.Name = &args.Name
	}

	if args.ProjectUID != "" {
		parentRef := "project:" + args.ProjectUID
		payload.Parent = &parentRef
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "searching mailing lists", "name", args.Name, "project_uid", args.ProjectUID, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to search mailing lists", err)},
			},
			IsError: true,
		}, nil, nil
	}

	type searchResult struct {
		Resources []*querysvc.Resource `json:"resources"`
		PageToken *string              `json:"page_token,omitempty"`
	}

	out := searchResult{
		Resources: result.Resources,
		PageToken: result.PageToken,
	}

	var pageWarning string
	if result.PageToken != nil && len(result.Resources) < pageSize {
		pageWarning = "WARNING: some results on this page were excluded because you do not have access to them; consider continuing with the next page token, increasing the page size, or narrowing your filters"
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal search result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "search_mailing_lists succeeded", "count", len(result.Resources))

	content := []mcp.Content{}
	if pageWarning != "" {
		content = append(content, &mcp.TextContent{Text: pageWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})
	return &mcp.CallToolResult{Content: content}, nil, nil
}

// handleSearchMailingListMembers implements the search_mailing_list_members tool logic.
func handleSearchMailingListMembers(ctx context.Context, req *mcp.CallToolRequest, args SearchMailingListMembersArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if mailingListConfig == nil {
		logger.ErrorContext(ctx, "mailing list tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: mailing list tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = mailingListConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := mailingListConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	resourceType := mailingListMemberResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		PageSize: pageSize,
		Sort:     "name_asc",
	}

	var tags []string
	if args.MailingListID != "" {
		tags = append(tags, fmt.Sprintf("mailing_list_uid:%s", args.MailingListID))
	}
	if args.ProjectUID != "" {
		tags = append(tags, fmt.Sprintf("project_uid:%s", args.ProjectUID))
	}
	if len(tags) > 0 {
		payload.Tags = tags
	}

	if args.Name != "" {
		payload.Name = &args.Name
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "searching mailing list members", "mailing_list_id", args.MailingListID, "project_uid", args.ProjectUID, "name", args.Name, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to search mailing list members", err)},
			},
			IsError: true,
		}, nil, nil
	}

	type searchResult struct {
		Resources []*querysvc.Resource `json:"resources"`
		PageToken *string              `json:"page_token,omitempty"`
	}

	out := searchResult{
		Resources: result.Resources,
		PageToken: result.PageToken,
	}

	var pageWarning string
	if result.PageToken != nil && len(result.Resources) < pageSize {
		pageWarning = "WARNING: some results on this page were excluded because you do not have access to them; consider continuing with the next page token, increasing the page size, or narrowing your filters"
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal search result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "search_mailing_list_members succeeded", "mailing_list_id", args.MailingListID, "project_uid", args.ProjectUID, "count", len(result.Resources))

	content := []mcp.Content{}
	if pageWarning != "" {
		content = append(content, &mcp.TextContent{Text: pageWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})
	return &mcp.CallToolResult{Content: content}, nil, nil
}
