// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	committeeservice "github.com/linuxfoundation/lfx-v2-committee-service/gen/committee_service"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// committeeResourceType is the resource type filter for committee queries.
const committeeResourceType = "committee"

// committeeMemberResourceType is the resource type filter for committee member queries.
const committeeMemberResourceType = "committee_member"

// CommitteeConfig holds configuration shared by committee tools.
type CommitteeConfig struct {
	// Clients is the shared LFX v2 API client instance. It must be created once
	// at startup so that its token cache persists across requests.
	Clients *lfxv2.Clients
}

var committeeConfig *CommitteeConfig

// SetCommitteeConfig sets the configuration for committee tools.
func SetCommitteeConfig(cfg *CommitteeConfig) {
	committeeConfig = cfg
}

// RegisterSearchCommittees registers the search_committees (or search_groups) tool with the MCP server.
// When asGroups is true, the tool is registered under the "search_groups" name with group-oriented
// descriptions; otherwise the standard committee terminology is used.
func RegisterSearchCommittees(server *mcp.Server, asGroups bool) {
	if asGroups {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "search_groups",
			Description: "Search for LFX groups (also called committees) by name using the LFX query service. Optionally filter by project UID.",
			Annotations: &mcp.ToolAnnotations{
				Title:        "Search Groups",
				ReadOnlyHint: true,
			},
		}, handleSearchCommitteesGroupMode)
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_committees",
		Description: "Search for LFX committees by name using the LFX query service. Optionally filter by project UID.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Committees",
			ReadOnlyHint: true,
		},
	}, handleSearchCommittees)
}

// RegisterGetCommittee registers the get_committee (or get_group) tool with the MCP server.
// When asGroups is true, the tool is registered under the "get_group" name with group-oriented
// descriptions; otherwise the standard committee terminology is used.
func RegisterGetCommittee(server *mcp.Server, asGroups bool) {
	if asGroups {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "get_group",
			Description: "Get an LFX group's (also called committee) base info and settings by its UID. Privileged group settings may be omitted if the caller lacks sufficient permissions.",
			Annotations: &mcp.ToolAnnotations{
				Title:        "Get Group",
				ReadOnlyHint: true,
			},
		}, handleGetCommitteeGroupMode)
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_committee",
		Description: "Get an LFX committee's base info and settings by its UID. Privileged committee settings may be omitted if the caller lacks sufficient permissions.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Committee",
			ReadOnlyHint: true,
		},
	}, handleGetCommittee)
}

// RegisterGetCommitteeMember registers the get_committee_member (or get_group_member) tool with the MCP server.
// When asGroups is true, the tool is registered under the "get_group_member" name with group-oriented
// descriptions; otherwise the standard committee terminology is used.
func RegisterGetCommitteeMember(server *mcp.Server, asGroups bool) {
	if asGroups {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "get_group_member",
			Description: "Get a specific group (also called committee) member by group UID and member UID.",
			Annotations: &mcp.ToolAnnotations{
				Title:        "Get Group Member",
				ReadOnlyHint: true,
			},
		}, handleGetCommitteeMemberGroupMode)
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_committee_member",
		Description: "Get a specific committee member by committee UID and member UID.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Committee Member",
			ReadOnlyHint: true,
		},
	}, handleGetCommitteeMember)
}

// RegisterSearchCommitteeMembers registers the search_committee_members (or search_group_members) tool with the MCP server.
// When asGroups is true, the tool is registered under the "search_group_members" name with group-oriented
// descriptions; otherwise the standard committee terminology is used.
func RegisterSearchCommitteeMembers(server *mcp.Server, asGroups bool) {
	if asGroups {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "search_group_members",
			Description: "Search for LFX group (also called committee) members. Optionally filter by group UID, project UID, and/or name. At least one filter is recommended but not required.",
			Annotations: &mcp.ToolAnnotations{
				Title:        "Search Group Members",
				ReadOnlyHint: true,
			},
		}, handleSearchCommitteeMembersGroupMode)
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_committee_members",
		Description: "Search for LFX committee members. Optionally filter by committee UID, project UID, and/or name. At least one filter is recommended but not required.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Committee Members",
			ReadOnlyHint: true,
		},
	}, handleSearchCommitteeMembers)
}

// SearchCommitteesArgs defines the input parameters for the search_committees tool.
type SearchCommitteesArgs struct {
	Name       string `json:"name,omitempty" jsonschema:"Name or partial name of the committee to search for"`
	ProjectUID string `json:"project_uid,omitempty" jsonschema:"Optional project UID to filter committees by project"`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken  string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// SearchGroupsArgs defines the input parameters for the search_groups tool (groups mode).
type SearchGroupsArgs struct {
	Name       string `json:"name,omitempty" jsonschema:"Name or partial name of the group to search for"`
	ProjectUID string `json:"project_uid,omitempty" jsonschema:"Optional project UID to filter groups by project"`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken  string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetCommitteeArgs defines the input parameters for the get_committee tool.
type GetCommitteeArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the committee to retrieve"`
}

// GetGroupArgs defines the input parameters for the get_group tool (groups mode).
type GetGroupArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the group to retrieve"`
}

// GetCommitteeMemberArgs defines the input parameters for the get_committee_member tool.
type GetCommitteeMemberArgs struct {
	CommitteeUID string `json:"committee_uid" jsonschema:"The UID of the committee"`
	MemberUID    string `json:"member_uid" jsonschema:"The UID of the committee member"`
}

// GetGroupMemberArgs defines the input parameters for the get_group_member tool (groups mode).
type GetGroupMemberArgs struct {
	GroupUID  string `json:"group_uid" jsonschema:"The UID of the group"`
	MemberUID string `json:"member_uid" jsonschema:"The UID of the group member"`
}

// SearchCommitteeMembersArgs defines the input parameters for the search_committee_members tool.
type SearchCommitteeMembersArgs struct {
	CommitteeUID string `json:"committee_uid,omitempty" jsonschema:"Optional UID of the committee to filter members by"`
	ProjectUID   string `json:"project_uid,omitempty" jsonschema:"Optional project UID to filter committee members by project"`
	Name         string `json:"name,omitempty" jsonschema:"Name or partial name of the member to search for"`
	PageSize     int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken    string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// SearchGroupMembersArgs defines the input parameters for the search_group_members tool (groups mode).
type SearchGroupMembersArgs struct {
	GroupUID   string `json:"group_uid,omitempty" jsonschema:"Optional UID of the group to filter members by"`
	ProjectUID string `json:"project_uid,omitempty" jsonschema:"Optional project UID to filter group members by project"`
	Name       string `json:"name,omitempty" jsonschema:"Name or partial name of the member to search for"`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken  string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// handleSearchCommitteesGroupMode adapts group-mode args to the committee handler.
func handleSearchCommitteesGroupMode(ctx context.Context, req *mcp.CallToolRequest, args SearchGroupsArgs) (*mcp.CallToolResult, any, error) {
	return handleSearchCommittees(ctx, req, SearchCommitteesArgs{
		Name:       args.Name,
		ProjectUID: args.ProjectUID,
		PageSize:   args.PageSize,
		PageToken:  args.PageToken,
	})
}

// handleGetCommitteeGroupMode adapts group-mode args to the committee handler.
func handleGetCommitteeGroupMode(ctx context.Context, req *mcp.CallToolRequest, args GetGroupArgs) (*mcp.CallToolResult, any, error) {
	return handleGetCommittee(ctx, req, GetCommitteeArgs{UID: args.UID})
}

// handleGetCommitteeMemberGroupMode adapts group-mode args to the committee member handler.
func handleGetCommitteeMemberGroupMode(ctx context.Context, req *mcp.CallToolRequest, args GetGroupMemberArgs) (*mcp.CallToolResult, any, error) {
	return handleGetCommitteeMember(ctx, req, GetCommitteeMemberArgs{
		CommitteeUID: args.GroupUID,
		MemberUID:    args.MemberUID,
	})
}

// handleSearchCommitteeMembersGroupMode adapts group-mode args to the committee members handler.
func handleSearchCommitteeMembersGroupMode(ctx context.Context, req *mcp.CallToolRequest, args SearchGroupMembersArgs) (*mcp.CallToolResult, any, error) {
	return handleSearchCommitteeMembers(ctx, req, SearchCommitteeMembersArgs{
		CommitteeUID: args.GroupUID,
		ProjectUID:   args.ProjectUID,
		Name:         args.Name,
		PageSize:     args.PageSize,
		PageToken:    args.PageToken,
	})
}

// handleSearchCommittees implements the search_committees tool logic.
func handleSearchCommittees(ctx context.Context, req *mcp.CallToolRequest, args SearchCommitteesArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if committeeConfig == nil {
		logger.ErrorContext(ctx, "committee tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: committee tools not configured"},
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

	ctx = committeeConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := committeeConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	resourceType := committeeResourceType
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
		// The query service requires parent refs in the form "<type>:<id>".
		parentRef := "project:" + args.ProjectUID
		payload.Parent = &parentRef
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "searching committees", "name", args.Name, "project_uid", args.ProjectUID, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to search committees", err)},
			},
			IsError: true,
		}, nil, nil
	}

	type searchResult struct {
		Resources []*querysvc.Resource `json:"resources"`
		PageToken *string              `json:"page_token,omitempty"`
	}

	// Strip the unreliable total_members field from indexed committee data. The
	// query service index does not maintain an accurate member count, so this
	// field is always zero regardless of actual membership. Removing it prevents
	// MCP clients from incorrectly concluding that a committee has no members.
	for _, r := range result.Resources {
		if data, ok := r.Data.(map[string]any); ok {
			delete(data, "total_members")
		}
	}

	out := searchResult{
		Resources: result.Resources,
		PageToken: result.PageToken,
	}

	// Warn if fewer results than requested were returned but more pages exist.
	// This indicates some results on this page were excluded due to access controls.
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

	logger.InfoContext(ctx, "search_committees succeeded", "count", len(result.Resources))

	content := []mcp.Content{}
	if pageWarning != "" {
		content = append(content, &mcp.TextContent{Text: pageWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})
	return &mcp.CallToolResult{Content: content}, nil, nil
}

// handleGetCommittee implements the get_committee tool logic, fetching both base
// info and settings for the given committee UID.
func handleGetCommittee(ctx context.Context, req *mcp.CallToolRequest, args GetCommitteeArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if committeeConfig == nil {
		logger.ErrorContext(ctx, "committee tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: committee tools not configured"},
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

	ctx = committeeConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := committeeConfig.Clients

	logger.InfoContext(ctx, "fetching committee", "uid", args.UID)

	baseResult, err := clients.Committee.GetCommitteeBase(ctx, &committeeservice.GetCommitteeBasePayload{
		UID: &args.UID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetCommitteeBase failed", "error", err, "uid", args.UID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to get committee", err)},
			},
			IsError: true,
		}, nil, nil
	}

	// Settings may be unavailable due to insufficient permissions; treat that
	// as a partial result rather than a hard failure so callers still get the
	// base data they are authorised to see.
	var committeeSettings *committeeservice.CommitteeSettingsWithReadonlyAttributes
	settingsResult, err := clients.Committee.GetCommitteeSettings(ctx, &committeeservice.GetCommitteeSettingsPayload{
		UID: &args.UID,
	})
	var settingsWarning string
	if err != nil {
		settingsWarning = fmt.Sprintf("WARNING: committee settings unavailable - %s", err.Error())
		logger.ErrorContext(ctx, "getting privileged committee settings failed, returning base only", "error", err, "uid", args.UID)
	} else {
		committeeSettings = settingsResult.CommitteeSettings
	}

	// Strip the unreliable TotalMembers field from the committee base. The
	// service does not populate this count reliably, so it is always zero
	// regardless of actual membership. Removing it prevents MCP clients from
	// incorrectly concluding that a committee has no members.
	if baseResult.CommitteeBase != nil {
		baseResult.CommitteeBase.TotalMembers = nil
	}

	type committeeResult struct {
		Base     *committeeservice.CommitteeBaseWithReadonlyAttributes     `json:"base"`
		Settings *committeeservice.CommitteeSettingsWithReadonlyAttributes `json:"settings,omitempty"`
	}

	out := committeeResult{
		Base:     baseResult.CommitteeBase,
		Settings: committeeSettings,
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal committee result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_committee succeeded", "uid", args.UID)

	content := []mcp.Content{}
	if settingsWarning != "" {
		content = append(content, &mcp.TextContent{Text: settingsWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})

	return &mcp.CallToolResult{
		Content: content,
	}, nil, nil
}

// handleGetCommitteeMember implements the get_committee_member tool logic.
func handleGetCommitteeMember(ctx context.Context, req *mcp.CallToolRequest, args GetCommitteeMemberArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if committeeConfig == nil {
		logger.ErrorContext(ctx, "committee tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: committee tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.CommitteeUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: committee_uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.MemberUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member_uid is required"},
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

	ctx = committeeConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := committeeConfig.Clients

	logger.InfoContext(ctx, "fetching committee member", "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)

	result, err := clients.Committee.GetCommitteeMember(ctx, &committeeservice.GetCommitteeMemberPayload{
		Version:   "1",
		UID:       args.CommitteeUID,
		MemberUID: args.MemberUID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetCommitteeMember failed", "error", err, "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to get committee member", err)},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Member, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal committee member result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_committee_member succeeded", "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleSearchCommitteeMembers implements the search_committee_members tool logic.
func handleSearchCommitteeMembers(ctx context.Context, req *mcp.CallToolRequest, args SearchCommitteeMembersArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if committeeConfig == nil {
		logger.ErrorContext(ctx, "committee tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: committee tools not configured"},
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

	ctx = committeeConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := committeeConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	resourceType := committeeMemberResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		PageSize: pageSize,
		Sort:     "name_asc",
	}

	// Build tag filters: committee members are tagged by the committee service indexer.
	var tags []string
	if args.CommitteeUID != "" {
		tags = append(tags, fmt.Sprintf("committee_uid:%s", args.CommitteeUID))
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

	logger.InfoContext(ctx, "searching committee members", "committee_uid", args.CommitteeUID, "project_uid", args.ProjectUID, "name", args.Name, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to search committee members", err)},
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

	// Warn if fewer results than requested were returned but more pages exist.
	// This indicates some results on this page were excluded due to access controls.
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

	logger.InfoContext(ctx, "search_committee_members succeeded", "committee_uid", args.CommitteeUID, "project_uid", args.ProjectUID, "count", len(result.Resources))

	content := []mcp.Content{}
	if pageWarning != "" {
		content = append(content, &mcp.TextContent{Text: pageWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})
	return &mcp.CallToolResult{Content: content}, nil, nil
}
