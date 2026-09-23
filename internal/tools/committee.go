// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	committeeservice "github.com/linuxfoundation/lfx-v2-committee-service/gen/committee_service"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// committeeResourceType is the resource type filter for committee queries.
const committeeResourceType = "committee"

// committeeMemberResourceType is the resource type filter for committee member queries.
const committeeMemberResourceType = "committee_member"

// rosterCoverageNoneNote is the format of the warning on an empty
// search_committee_members result scoped by project_uid when the project has
// no committee visible to the caller in LFX v2. Its %s is the plural noun for
// the committees ("committees" or "groups").
const rosterCoverageNoneNote = "Roster coverage: no %s are onboarded into LFX v2 for this project, or none are visible to you. An empty result is not evidence that the person or organization holds no seat."

// rosterCoverageNoMatchNote is the format of the warning on an empty
// search_committee_members result scoped by project_uid when the project has
// committees visible to the caller in LFX v2. The count is access-filtered, so
// member records of committees the caller cannot see are not ruled out. Its
// %s is the plural noun for the committees.
const rosterCoverageNoMatchNote = "Roster coverage: this project has %s in LFX v2 visible to you, but no member record visible to you matched these filters. name is a typeahead and organization_name must equal the stored spelling; results cover only records you can view, so an empty result is not evidence that the person or organization holds no seat."

// CommitteeConfig holds configuration shared by committee tools.
type CommitteeConfig struct {
	// Clients is the shared LFX v2 API client instance. It must be created once
	// at startup so that its token cache persists across requests.
	Clients *lfxv2.Clients
}

var committeeConfig *CommitteeConfig

// committeeGetResult is the output type for get_committee / get_group.
type committeeGetResult struct {
	Base     *committeeservice.CommitteeBaseWithReadonlyAttributes     `json:"base"`
	Settings *committeeservice.CommitteeSettingsWithReadonlyAttributes `json:"settings,omitempty"`
}

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
			Description: "Search for LFX groups (also called committees) by name using the LFX query service. Optionally filter by project UID. Groups are the system of record for governance bodies - boards, TOCs/TACs, working groups, ambassador programs. Prefer this over the semantic layer or query_lfx_lens for who-sits-on-what and roster questions.",
			Annotations: &mcp.ToolAnnotations{
				Title:        "Search Groups",
				ReadOnlyHint: true,
			},
		}, handleSearchCommitteesGroupMode)
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_committees",
		Description: "Search for LFX committees by name using the LFX query service. Optionally filter by project UID. Committees are the system of record for governance bodies - boards, TOCs/TACs, working groups, ambassador programs. Prefer this over the semantic layer or query_lfx_lens for who-sits-on-what and roster questions.",
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
			Description: "Search for LFX group (also called committee) members. Optionally filter by group UID, project UID, and/or name. The authoritative source for committee rosters - prefer over the semantic layer or query_lfx_lens for board/TOC/ambassador membership. For counts, paginate until page_token is absent; records carry organization, role and voting status but no country. Filters combine with AND: a record must match every filter given. organization_name keeps one organization's members and must equal the stored spelling (copy it from a roster row or get_org_committee_seats). With project_uid set, an empty result carries a roster-coverage note saying whether the project has any committee onboarded into LFX v2; an empty result never proves that a person or organization holds no seat.",
			Annotations: &mcp.ToolAnnotations{
				Title:        "Search Group Members",
				ReadOnlyHint: true,
			},
		}, handleSearchCommitteeMembersGroupMode)
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_committee_members",
		Description: "Search for LFX committee members. Optionally filter by committee UID, project UID, and/or name. The authoritative source for committee rosters - prefer over the semantic layer or query_lfx_lens for board/TOC/ambassador membership. For counts, paginate until page_token is absent; records carry organization, role and voting status but no country. Filters combine with AND: a record must match every filter given. organization_name keeps one organization's members and must equal the stored spelling (copy it from a roster row or get_org_committee_seats). With project_uid set, an empty result carries a roster-coverage note saying whether the project has any committee onboarded into LFX v2; an empty result never proves that a person or organization holds no seat.",
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
	CommitteeUID     string `json:"committee_uid,omitempty" jsonschema:"Optional UID of the committee to filter members by"`
	ProjectUID       string `json:"project_uid,omitempty" jsonschema:"Optional project UID to filter committee members by project"`
	OrganizationName string `json:"organization_name,omitempty" jsonschema:"Exact stored organization name on the seat, as a roster row or a get_org_committee_seats row spells it; keeps one organization's members"`
	Name             string `json:"name,omitempty" jsonschema:"Name or partial name of the member to search for"`
	PageSize         int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken        string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// SearchGroupMembersArgs defines the input parameters for the search_group_members tool (groups mode).
type SearchGroupMembersArgs struct {
	GroupUID         string `json:"group_uid,omitempty" jsonschema:"Optional UID of the group to filter members by"`
	ProjectUID       string `json:"project_uid,omitempty" jsonschema:"Optional project UID to filter group members by project"`
	OrganizationName string `json:"organization_name,omitempty" jsonschema:"Exact stored organization name on the seat, as a roster row or a get_org_committee_seats row spells it; keeps one organization's members"`
	Name             string `json:"name,omitempty" jsonschema:"Name or partial name of the member to search for"`
	PageSize         int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken        string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// handleSearchCommitteesGroupMode adapts group-mode args to the committee
// handler, carrying the group-mode noun so user-facing warnings match the tool's
// terminology contract.
func handleSearchCommitteesGroupMode(ctx context.Context, req *mcp.CallToolRequest, args SearchGroupsArgs) (*mcp.CallToolResult, resourceSearchResult, error) {
	return searchCommittees(ctx, req, SearchCommitteesArgs(args), "groups")
}

// handleGetCommitteeGroupMode adapts group-mode args to the committee handler.
func handleGetCommitteeGroupMode(ctx context.Context, req *mcp.CallToolRequest, args GetGroupArgs) (*mcp.CallToolResult, committeeGetResult, error) {
	return handleGetCommittee(ctx, req, GetCommitteeArgs(args))
}

// handleGetCommitteeMemberGroupMode adapts group-mode args to the committee member handler.
func handleGetCommitteeMemberGroupMode(ctx context.Context, req *mcp.CallToolRequest, args GetGroupMemberArgs) (*mcp.CallToolResult, *committeeservice.CommitteeMemberFullWithReadonlyAttributes, error) {
	return handleGetCommitteeMember(ctx, req, GetCommitteeMemberArgs{
		CommitteeUID: args.GroupUID,
		MemberUID:    args.MemberUID,
	})
}

// handleSearchCommitteeMembersGroupMode adapts group-mode args to the
// committee members handler, carrying the group-mode noun so user-facing
// warnings match the tool's terminology contract.
func handleSearchCommitteeMembersGroupMode(ctx context.Context, req *mcp.CallToolRequest, args SearchGroupMembersArgs) (*mcp.CallToolResult, resourceSearchResult, error) {
	return searchCommitteeMembers(ctx, req, SearchCommitteeMembersArgs{
		CommitteeUID:     args.GroupUID,
		ProjectUID:       args.ProjectUID,
		OrganizationName: args.OrganizationName,
		Name:             args.Name,
		PageSize:         args.PageSize,
		PageToken:        args.PageToken,
	}, "group members", "groups")
}

// handleSearchCommittees implements the search_committees tool logic.
func handleSearchCommittees(ctx context.Context, req *mcp.CallToolRequest, args SearchCommitteesArgs) (*mcp.CallToolResult, resourceSearchResult, error) {
	return searchCommittees(ctx, req, args, "committees")
}

// searchCommittees is the shared search implementation; resourceNoun is
// "committees" or "groups" depending on which tool surface reached it, and is
// used in user-facing warnings.
func searchCommittees(ctx context.Context, req *mcp.CallToolRequest, args SearchCommitteesArgs, resourceNoun string) (*mcp.CallToolResult, resourceSearchResult, error) {
	logger := newToolLogger(ctx, req)

	if committeeConfig == nil {
		logger.ErrorContext(ctx, "committee tools not configured")
		return nil, resourceSearchResult{}, toolError("Error: committee tools not configured")
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return nil, resourceSearchResult{}, toolError(fmt.Sprintf("Error: failed to extract MCP token: %v", err))
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
		return nil, resourceSearchResult{}, toolError(friendlyAPIError("failed to search committees", err))
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

	out := newResourceSearchResult(resourceNoun, result, pageSize, args.PageToken != "")

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal search result", "error", err)
		return nil, resourceSearchResult{}, toolError(fmt.Sprintf("Error: failed to format result: %v", err))
	}

	logger.InfoContext(ctx, "search_committees succeeded", "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, out, nil
}

// handleGetCommittee implements the get_committee tool logic, fetching both base
// info and settings for the given committee UID.
func handleGetCommittee(ctx context.Context, req *mcp.CallToolRequest, args GetCommitteeArgs) (*mcp.CallToolResult, committeeGetResult, error) {
	logger := newToolLogger(ctx, req)

	if committeeConfig == nil {
		logger.ErrorContext(ctx, "committee tools not configured")
		return nil, committeeGetResult{}, toolError("Error: committee tools not configured")
	}

	if args.UID == "" {
		return nil, committeeGetResult{}, toolError("Error: uid is required")
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return nil, committeeGetResult{}, toolError(fmt.Sprintf("Error: failed to extract MCP token: %v", err))
	}

	ctx = committeeConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := committeeConfig.Clients

	logger.InfoContext(ctx, "fetching committee", "uid", args.UID)

	baseResult, err := clients.Committee.GetCommitteeBase(ctx, &committeeservice.GetCommitteeBasePayload{
		UID: &args.UID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetCommitteeBase failed", "error", err, "uid", args.UID)
		return nil, committeeGetResult{}, toolError(friendlyAPIError("failed to get committee", err))
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

	out := committeeGetResult{
		Base:     baseResult.CommitteeBase,
		Settings: committeeSettings,
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal committee result", "error", err)
		return nil, committeeGetResult{}, toolError(fmt.Sprintf("Error: failed to format result: %v", err))
	}

	logger.InfoContext(ctx, "get_committee succeeded", "uid", args.UID)

	content := []mcp.Content{}
	if settingsWarning != "" {
		content = append(content, &mcp.TextContent{Text: settingsWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})

	return &mcp.CallToolResult{
		Content: content,
	}, out, nil
}

// handleGetCommitteeMember implements the get_committee_member tool logic.
func handleGetCommitteeMember(ctx context.Context, req *mcp.CallToolRequest, args GetCommitteeMemberArgs) (*mcp.CallToolResult, *committeeservice.CommitteeMemberFullWithReadonlyAttributes, error) {
	logger := newToolLogger(ctx, req)

	if committeeConfig == nil {
		logger.ErrorContext(ctx, "committee tools not configured")
		return nil, nil, toolError("Error: committee tools not configured")
	}

	if args.CommitteeUID == "" {
		return nil, nil, toolError("Error: committee_uid is required")
	}

	if args.MemberUID == "" {
		return nil, nil, toolError("Error: member_uid is required")
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return nil, nil, toolError(fmt.Sprintf("Error: failed to extract MCP token: %v", err))
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
		return nil, nil, toolError(friendlyAPIError("failed to get committee member", err))
	}

	prettyJSON, err := json.MarshalIndent(result.Member, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal committee member result", "error", err)
		return nil, nil, toolError(fmt.Sprintf("Error: failed to format result: %v", err))
	}

	logger.InfoContext(ctx, "get_committee_member succeeded", "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, result.Member, nil
}

// handleSearchCommitteeMembers implements the search_committee_members tool logic.
func handleSearchCommitteeMembers(ctx context.Context, req *mcp.CallToolRequest, args SearchCommitteeMembersArgs) (*mcp.CallToolResult, resourceSearchResult, error) {
	return searchCommitteeMembers(ctx, req, args, "committee members", "committees")
}

// searchCommitteeMembers is the shared search implementation; resourceNoun is
// "committee members" or "group members", and committeeNoun "committees" or
// "groups", depending on which tool surface reached it. Both are used in
// user-facing warnings.
func searchCommitteeMembers(ctx context.Context, req *mcp.CallToolRequest, args SearchCommitteeMembersArgs, resourceNoun, committeeNoun string) (*mcp.CallToolResult, resourceSearchResult, error) {
	logger := newToolLogger(ctx, req)

	if committeeConfig == nil {
		logger.ErrorContext(ctx, "committee tools not configured")
		return nil, resourceSearchResult{}, toolError("Error: committee tools not configured")
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return nil, resourceSearchResult{}, toolError(fmt.Sprintf("Error: failed to extract MCP token: %v", err))
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
	// Filters go through tags_all so that every given filter must match.
	var tags []string
	if args.CommitteeUID != "" {
		tags = append(tags, fmt.Sprintf("committee_uid:%s", args.CommitteeUID))
	}
	if args.ProjectUID != "" {
		tags = append(tags, fmt.Sprintf("project_uid:%s", args.ProjectUID))
	}
	if args.OrganizationName != "" {
		tags = append(tags, "organization_name:"+args.OrganizationName)
	}
	if len(tags) > 0 {
		payload.TagsAll = tags
	}

	if args.Name != "" {
		payload.Name = &args.Name
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "searching committee members", "committee_uid", args.CommitteeUID, "project_uid", args.ProjectUID, "organization_name", args.OrganizationName, "name", args.Name, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return nil, resourceSearchResult{}, toolError(friendlyAPIError("failed to search committee members", err))
	}

	out := newResourceSearchResult(resourceNoun, result, pageSize, args.PageToken != "")
	// The roster-coverage note, when it applies, is the more specific
	// statement of an empty first page, so it replaces the generic warning.
	if note := rosterCoverageNote(ctx, logger, clients, args.ProjectUID, args.PageToken, committeeNoun, result); note != "" {
		out.Warnings = []string{note}
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal search result", "error", err)
		return nil, resourceSearchResult{}, toolError(fmt.Sprintf("Error: failed to format result: %v", err))
	}

	logger.InfoContext(ctx, "search_committee_members succeeded", "committee_uid", args.CommitteeUID, "project_uid", args.ProjectUID, "organization_name", args.OrganizationName, "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, out, nil
}

// rosterCoverageNote returns the roster-coverage note for a genuinely empty
// search_committee_members result scoped by project_uid: the first page, with
// no resources and no page token. A continuation page is never genuinely
// empty. It counts the project's committees visible to the caller; when the
// count call fails the note is dropped and the search result stands.
// committeeNoun names the committees ("committees" or "groups").
func rosterCoverageNote(ctx context.Context, logger *slog.Logger, clients *lfxv2.Clients, projectUID, pageToken, committeeNoun string, result *querysvc.QueryResourcesResult) string {
	if projectUID == "" || pageToken != "" || len(result.Resources) > 0 || result.PageToken != nil {
		return ""
	}
	committeeType := committeeResourceType
	count, err := clients.QuerySvc.QueryResourcesCount(ctx, &querysvc.QueryResourcesCountPayload{
		Version: "1",
		Type:    &committeeType,
		Parent:  strPtr("project:" + projectUID),
	})
	if err != nil {
		logger.WarnContext(ctx, "roster coverage count failed", "project_uid", projectUID, "error", err)
		return ""
	}
	if count.Count == 0 {
		return fmt.Sprintf(rosterCoverageNoneNote, committeeNoun)
	}
	return fmt.Sprintf(rosterCoverageNoMatchNote, committeeNoun)
}
