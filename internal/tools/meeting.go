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

// meetingResourceType is the resource type filter for meeting queries.
const meetingResourceType = "v1_meeting"

// meetingRegistrantResourceType is the resource type filter for meeting registrant queries.
const meetingRegistrantResourceType = "v1_meeting_registrant"

// pastMeetingParticipantResourceType is the resource type filter for past meeting participant queries.
const pastMeetingParticipantResourceType = "v1_past_meeting_participant"

// pastMeetingSummaryResourceType is the resource type filter for past meeting summary queries.
const pastMeetingSummaryResourceType = "v1_past_meeting_summary"

// pastMeetingResourceType is the resource type filter for past meeting queries.
const pastMeetingResourceType = "v1_past_meeting"

// MeetingConfig holds configuration shared by meeting tools.
type MeetingConfig struct {
	// Clients is the shared LFX v2 API client instance. It must be created once
	// at startup so that its token cache persists across requests.
	Clients *lfxv2.Clients
}

var meetingConfig *MeetingConfig

// SetMeetingConfig sets the configuration for meeting tools.
func SetMeetingConfig(cfg *MeetingConfig) {
	meetingConfig = cfg
}

// RegisterSearchMeetings registers the search_meetings tool with the MCP server.
// When asGroups is true, the tool description uses group-oriented language and
// the committee_uid parameter is renamed to group_uid; otherwise the standard
// committee terminology is used.
func RegisterSearchMeetings(server *mcp.Server, asGroups bool) {
	if asGroups {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "search_meetings",
			Description: "Search for LFX meetings (group calls, also called committee calls, working group sessions) using the query service. IMPORTANT: When the user asks about events, or for event data (conferences, registrations, attendees, speakers, sponsorships), use query_lfx_semantic_layer (preferred) or query_lfx_lens if semantic layer struggles.",
			Annotations: &mcp.ToolAnnotations{
				Title:        "Search Meetings",
				ReadOnlyHint: true,
			},
		}, handleSearchMeetingsGroupMode)
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_meetings",
		Description: "Search for LFX meetings (committee calls, working group sessions) using the query service. IMPORTANT: When the user asks about events, or for event data (conferences, registrations, attendees, speakers, sponsorships), use query_lfx_semantic_layer (preferred) or query_lfx_lens if semantic layer struggles.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Meetings",
			ReadOnlyHint: true,
		},
	}, handleSearchMeetings)
}

// RegisterGetMeeting registers the get_meeting tool with the MCP server.
func RegisterGetMeeting(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_meeting",
		Description: "Get an LFX meeting by its UID using the query service.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Meeting",
			ReadOnlyHint: true,
		},
	}, handleGetMeeting)
}

// RegisterSearchMeetingRegistrants registers the search_meeting_registrants tool with the MCP server.
// When asGroups is true, the tool description uses group-oriented language and
// the committee_uid parameter is renamed to group_uid; otherwise the standard
// committee terminology is used.
func RegisterSearchMeetingRegistrants(server *mcp.Server, asGroups bool) {
	if asGroups {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "search_meeting_registrants",
			Description: "Search for LFX meeting registrants using the query service. Supports filtering by meeting, group (also known as committee), project, date range, and other fields.",
			Annotations: &mcp.ToolAnnotations{
				Title:        "Search Meeting Registrants",
				ReadOnlyHint: true,
			},
		}, handleSearchMeetingRegistrantsGroupMode)
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_meeting_registrants",
		Description: "Search for LFX meeting registrants using the query service. Supports filtering by meeting, committee, project, date range, and other fields.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Meeting Registrants",
			ReadOnlyHint: true,
		},
	}, handleSearchMeetingRegistrants)
}

// RegisterGetMeetingRegistrant registers the get_meeting_registrant tool with the MCP server.
func RegisterGetMeetingRegistrant(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_meeting_registrant",
		Description: "Get an LFX meeting registrant by its UID using the query service.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Meeting Registrant",
			ReadOnlyHint: true,
		},
	}, handleGetMeetingRegistrant)
}

// RegisterSearchPastMeetingParticipants registers the search_past_meeting_participants tool with the MCP server.
func RegisterSearchPastMeetingParticipants(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_past_meeting_participants",
		Description: "Search for LFX past meeting participants using the query service. Supports filtering by meeting ID and name.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Past Meeting Participants",
			ReadOnlyHint: true,
		},
	}, handleSearchPastMeetingParticipants)
}

// RegisterGetPastMeetingParticipant registers the get_past_meeting_participant tool with the MCP server.
func RegisterGetPastMeetingParticipant(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_past_meeting_participant",
		Description: "Get an LFX past meeting participant by its UID using the query service.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Past Meeting Participant",
			ReadOnlyHint: true,
		},
	}, handleGetPastMeetingParticipant)
}

// RegisterSearchPastMeetingSummaries registers the search_past_meeting_summaries tool with the MCP server.
func RegisterSearchPastMeetingSummaries(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_past_meeting_summaries",
		Description: "Search for LFX past meeting summaries using the query service. Supports filtering by meeting ID and name.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Past Meeting Summaries",
			ReadOnlyHint: true,
		},
	}, handleSearchPastMeetingSummaries)
}

// RegisterGetPastMeetingSummary registers the get_past_meeting_summary tool with the MCP server.
func RegisterGetPastMeetingSummary(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_past_meeting_summary",
		Description: "Get an LFX past meeting summary by its UID using the query service.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Past Meeting Summary",
			ReadOnlyHint: true,
		},
	}, handleGetPastMeetingSummary)
}

// RegisterSearchPastMeetings registers the search_past_meetings tool with the MCP server.
// When asGroups is true, the tool description uses group-oriented language and
// the committee_uid parameter is renamed to group_uid; otherwise the standard
// committee terminology is used.
func RegisterSearchPastMeetings(server *mcp.Server, asGroups bool) {
	if asGroups {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "search_past_meetings",
			Description: "Search for LFX past meetings (v1_past_meeting) using the query service. Supports filtering by project, group (also known as committee), meeting ID, date range, and name.",
			Annotations: &mcp.ToolAnnotations{
				Title:        "Search Past Meetings",
				ReadOnlyHint: true,
			},
		}, handleSearchPastMeetingsGroupMode)
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_past_meetings",
		Description: "Search for LFX past meetings using the query service. Supports filtering by project, committee, meeting ID, date range, and name.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Past Meetings",
			ReadOnlyHint: true,
		},
	}, handleSearchPastMeetings)
}

// RegisterGetPastMeeting registers the get_past_meeting tool with the MCP server.
func RegisterGetPastMeeting(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_past_meeting",
		Description: "Get an LFX past meeting by its UID using the query service.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Past Meeting",
			ReadOnlyHint: true,
		},
	}, handleGetPastMeeting)
}

// SearchMeetingsArgs defines the input parameters for the search_meetings tool.
type SearchMeetingsArgs struct {
	Name         string   `json:"name,omitempty" jsonschema:"Name or partial name of the meeting to search for"`
	ProjectUID   string   `json:"project_uid,omitempty" jsonschema:"Filter meetings by project UID (ignored when committee_uid is set)"`
	CommitteeUID string   `json:"committee_uid,omitempty" jsonschema:"Filter meetings by committee UID"`
	DateField    string   `json:"date_field,omitempty" jsonschema:"Date field to filter on (default start_time when date_from or date_to is set)"`
	DateFrom     string   `json:"date_from,omitempty" jsonschema:"Start date inclusive in ISO 8601 format (e.g. 2025-01-01)"`
	DateTo       string   `json:"date_to,omitempty" jsonschema:"End date inclusive in ISO 8601 format (e.g. 2025-12-31)"`
	Filters      []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters (e.g. visibility:public or status:active)"`
	Sort         string   `json:"sort,omitempty" jsonschema:"Sort order: name_asc (default), name_desc, updated_asc, updated_desc"`
	PageSize     int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken    string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// SearchMeetingsGroupArgs is the groups-mode variant of SearchMeetingsArgs.
type SearchMeetingsGroupArgs struct {
	Name       string   `json:"name,omitempty" jsonschema:"Name or partial name of the meeting to search for"`
	ProjectUID string   `json:"project_uid,omitempty" jsonschema:"Filter meetings by project UID (ignored when group_uid is set)"`
	GroupUID   string   `json:"group_uid,omitempty" jsonschema:"Filter meetings by group UID (also known as committee UID)"`
	DateField  string   `json:"date_field,omitempty" jsonschema:"Date field to filter on (default start_time when date_from or date_to is set)"`
	DateFrom   string   `json:"date_from,omitempty" jsonschema:"Start date inclusive in ISO 8601 format (e.g. 2025-01-01)"`
	DateTo     string   `json:"date_to,omitempty" jsonschema:"End date inclusive in ISO 8601 format (e.g. 2025-12-31)"`
	Filters    []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters (e.g. visibility:public or status:active)"`
	Sort       string   `json:"sort,omitempty" jsonschema:"Sort order for results (default name_asc),enum=name_asc,enum=name_desc,enum=updated_asc,enum=updated_desc"`
	PageSize   int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken  string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetMeetingArgs defines the input parameters for the get_meeting tool.
type GetMeetingArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the meeting to retrieve"`
}

// SearchMeetingRegistrantsArgs defines the input parameters for the search_meeting_registrants tool.
type SearchMeetingRegistrantsArgs struct {
	MeetingID    string   `json:"meeting_id,omitempty" jsonschema:"Filter registrants by meeting ID"`
	CommitteeUID string   `json:"committee_uid,omitempty" jsonschema:"Filter registrants by committee UID (ignored when meeting_id is set)"`
	Name         string   `json:"name,omitempty" jsonschema:"Name or partial name of the registrant to search for"`
	Filters      []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters (e.g. host:true or type:committee)"`
	Sort         string   `json:"sort,omitempty" jsonschema:"Sort order: name_asc (default), name_desc, updated_asc, updated_desc"`
	PageSize     int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken    string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// SearchMeetingRegistrantsGroupArgs is the groups-mode variant of SearchMeetingRegistrantsArgs.
type SearchMeetingRegistrantsGroupArgs struct {
	MeetingID string   `json:"meeting_id,omitempty" jsonschema:"Filter registrants by meeting ID"`
	GroupUID  string   `json:"group_uid,omitempty" jsonschema:"Filter registrants by group UID (also known as committee UID; ignored when meeting_id is set)"`
	Name      string   `json:"name,omitempty" jsonschema:"Name or partial name of the registrant to search for"`
	Filters   []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters (e.g. host:true or type:committee)"`
	Sort      string   `json:"sort,omitempty" jsonschema:"Sort order for results (default name_asc),enum=name_asc,enum=name_desc,enum=updated_asc,enum=updated_desc"`
	PageSize  int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetMeetingRegistrantArgs defines the input parameters for the get_meeting_registrant tool.
type GetMeetingRegistrantArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the meeting registrant to retrieve"`
}

// SearchPastMeetingParticipantsArgs defines the input parameters for the search_past_meeting_participants tool.
type SearchPastMeetingParticipantsArgs struct {
	MeetingID string   `json:"meeting_id,omitempty" jsonschema:"Filter participants by meeting ID"`
	Name      string   `json:"name,omitempty" jsonschema:"Name or partial name of the participant to search for"`
	Filters   []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters"`
	Sort      string   `json:"sort,omitempty" jsonschema:"Sort order: name_asc (default), name_desc, updated_asc, updated_desc"`
	PageSize  int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetPastMeetingParticipantArgs defines the input parameters for the get_past_meeting_participant tool.
type GetPastMeetingParticipantArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the past meeting participant to retrieve"`
}

// SearchPastMeetingSummariesArgs defines the input parameters for the search_past_meeting_summaries tool.
type SearchPastMeetingSummariesArgs struct {
	MeetingID string   `json:"meeting_id,omitempty" jsonschema:"Filter summaries by meeting ID"`
	Name      string   `json:"name,omitempty" jsonschema:"Name or partial name of the summary to search for"`
	Filters   []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters"`
	Sort      string   `json:"sort,omitempty" jsonschema:"Sort order: name_asc (default), name_desc, updated_asc, updated_desc"`
	PageSize  int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetPastMeetingSummaryArgs defines the input parameters for the get_past_meeting_summary tool.
type GetPastMeetingSummaryArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the past meeting summary to retrieve"`
}

// handleSearchMeetings implements the search_meetings tool logic.
func handleSearchMeetings(ctx context.Context, req *mcp.CallToolRequest, args SearchMeetingsArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: meeting tools not configured"},
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

	ctx = meetingConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := meetingConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	sort := args.Sort
	if sort == "" {
		sort = "name_asc"
	}

	resourceType := meetingResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		PageSize: pageSize,
		Sort:     sort,
	}

	if args.Name != "" {
		payload.Name = &args.Name
	}

	// committee_uid takes precedence over project_uid when both are provided
	// because committee resolves to a more specific parent reference.
	if args.CommitteeUID != "" {
		parentRef := "committee:" + args.CommitteeUID
		payload.Parent = &parentRef
	} else if args.ProjectUID != "" {
		parentRef := "project:" + args.ProjectUID
		payload.Parent = &parentRef
	}

	if args.DateFrom != "" || args.DateTo != "" {
		dateField := args.DateField
		if dateField == "" {
			dateField = "start_time"
		}
		payload.DateField = &dateField
		if args.DateFrom != "" {
			payload.DateFrom = &args.DateFrom
		}
		if args.DateTo != "" {
			payload.DateTo = &args.DateTo
		}
	}

	if len(args.Filters) > 0 {
		payload.Filters = args.Filters
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "searching meetings", "name", args.Name, "project_uid", args.ProjectUID, "committee_uid", args.CommitteeUID, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to search meetings", err)},
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

	logger.InfoContext(ctx, "search_meetings succeeded", "count", len(result.Resources))

	content := []mcp.Content{}
	if pageWarning != "" {
		content = append(content, &mcp.TextContent{Text: pageWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})
	return &mcp.CallToolResult{Content: content}, nil, nil
}

// handleGetMeeting implements the get_meeting tool logic.
func handleGetMeeting(ctx context.Context, req *mcp.CallToolRequest, args GetMeetingArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: meeting tools not configured"},
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

	ctx = meetingConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := meetingConfig.Clients

	logger.InfoContext(ctx, "fetching meeting", "uid", args.UID)

	resourceType := meetingResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		Filters:  []string{fmt.Sprintf("uid:%s", args.UID)},
		PageSize: 1,
		Sort:     "name_asc",
	}

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to get meeting", err)},
			},
			IsError: true,
		}, nil, nil
	}

	if len(result.Resources) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: meeting not found with UID: %s", args.UID)},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Resources[0], "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal meeting result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_meeting succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleSearchMeetingRegistrants implements the search_meeting_registrants tool logic.
func handleSearchMeetingRegistrants(ctx context.Context, req *mcp.CallToolRequest, args SearchMeetingRegistrantsArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: meeting tools not configured"},
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

	ctx = meetingConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := meetingConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	sort := args.Sort
	if sort == "" {
		sort = "name_asc"
	}

	resourceType := meetingRegistrantResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		PageSize: pageSize,
		Sort:     sort,
	}

	// meeting_id takes precedence over committee_uid because Parent can only be
	// set once; prefer the more specific filter.
	if args.MeetingID != "" {
		parentRef := "meeting:" + args.MeetingID
		payload.Parent = &parentRef
	} else if args.CommitteeUID != "" {
		parentRef := "committee:" + args.CommitteeUID
		payload.Parent = &parentRef
	}

	if args.Name != "" {
		payload.Name = &args.Name
	}

	if len(args.Filters) > 0 {
		payload.Filters = args.Filters
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "searching meeting registrants", "meeting_id", args.MeetingID, "committee_uid", args.CommitteeUID, "name", args.Name, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to search meeting registrants", err)},
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

	logger.InfoContext(ctx, "search_meeting_registrants succeeded", "count", len(result.Resources))

	content := []mcp.Content{}
	if pageWarning != "" {
		content = append(content, &mcp.TextContent{Text: pageWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})
	return &mcp.CallToolResult{Content: content}, nil, nil
}

// handleGetMeetingRegistrant implements the get_meeting_registrant tool logic.
func handleGetMeetingRegistrant(ctx context.Context, req *mcp.CallToolRequest, args GetMeetingRegistrantArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: meeting tools not configured"},
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

	ctx = meetingConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := meetingConfig.Clients

	logger.InfoContext(ctx, "fetching meeting registrant", "uid", args.UID)

	resourceType := meetingRegistrantResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		Filters:  []string{fmt.Sprintf("uid:%s", args.UID)},
		PageSize: 1,
		Sort:     "name_asc",
	}

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to get meeting registrant", err)},
			},
			IsError: true,
		}, nil, nil
	}

	if len(result.Resources) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: meeting registrant not found with UID: %s", args.UID)},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Resources[0], "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal meeting registrant result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_meeting_registrant succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleSearchPastMeetingParticipants implements the search_past_meeting_participants tool logic.
func handleSearchPastMeetingParticipants(ctx context.Context, req *mcp.CallToolRequest, args SearchPastMeetingParticipantsArgs) (*mcp.CallToolResult, any, error) {
	return handleSearchPastMeetingResource(ctx, req, pastMeetingParticipantResourceType, "past meeting participants", args.MeetingID, args.Name, args.Filters, args.Sort, args.PageSize, args.PageToken)
}

// handleGetPastMeetingParticipant implements the get_past_meeting_participant tool logic.
func handleGetPastMeetingParticipant(ctx context.Context, req *mcp.CallToolRequest, args GetPastMeetingParticipantArgs) (*mcp.CallToolResult, any, error) {
	return handleGetPastMeetingResource(ctx, req, pastMeetingParticipantResourceType, "past meeting participant", args.UID)
}

// handleSearchPastMeetingSummaries implements the search_past_meeting_summaries tool logic.
func handleSearchPastMeetingSummaries(ctx context.Context, req *mcp.CallToolRequest, args SearchPastMeetingSummariesArgs) (*mcp.CallToolResult, any, error) {
	return handleSearchPastMeetingResource(ctx, req, pastMeetingSummaryResourceType, "past meeting summaries", args.MeetingID, args.Name, args.Filters, args.Sort, args.PageSize, args.PageToken)
}

// handleGetPastMeetingSummary implements the get_past_meeting_summary tool logic.
func handleGetPastMeetingSummary(ctx context.Context, req *mcp.CallToolRequest, args GetPastMeetingSummaryArgs) (*mcp.CallToolResult, any, error) {
	return handleGetPastMeetingResource(ctx, req, pastMeetingSummaryResourceType, "past meeting summary", args.UID)
}

// handleSearchPastMeetingResource is a shared implementation for searching past meeting resource types.
func handleSearchPastMeetingResource(ctx context.Context, req *mcp.CallToolRequest, resourceType, resourceLabel, meetingID, name string, filters []string, sort string, pageSize int, pageToken string) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: meeting tools not configured"},
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

	ctx = meetingConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := meetingConfig.Clients

	if pageSize <= 0 {
		pageSize = 10
	}

	if sort == "" {
		sort = "name_asc"
	}

	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		PageSize: pageSize,
		Sort:     sort,
	}

	var tags []string
	if meetingID != "" {
		tags = append(tags, fmt.Sprintf("meeting_id:%s", meetingID))
	}
	if len(tags) > 0 {
		payload.Tags = tags
	}

	if name != "" {
		payload.Name = &name
	}

	if len(filters) > 0 {
		payload.Filters = filters
	}

	if pageToken != "" {
		payload.PageToken = &pageToken
	}

	logger.InfoContext(ctx, "searching "+resourceLabel, "meeting_id", meetingID, "name", name, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to search "+resourceLabel, err)},
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

	logger.InfoContext(ctx, "search "+resourceLabel+" succeeded", "count", len(result.Resources))

	content := []mcp.Content{}
	if pageWarning != "" {
		content = append(content, &mcp.TextContent{Text: pageWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})
	return &mcp.CallToolResult{Content: content}, nil, nil
}

// handleGetPastMeetingResource is a shared implementation for getting a past meeting resource by UID.
func handleGetPastMeetingResource(ctx context.Context, req *mcp.CallToolRequest, resourceType, resourceLabel, uid string) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: meeting tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if uid == "" {
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

	ctx = meetingConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := meetingConfig.Clients

	logger.InfoContext(ctx, "fetching "+resourceLabel, "uid", uid)

	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		Filters:  []string{fmt.Sprintf("uid:%s", uid)},
		PageSize: 1,
		Sort:     "name_asc",
	}

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to get "+resourceLabel, err)},
			},
			IsError: true,
		}, nil, nil
	}

	if len(result.Resources) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: %s not found with UID: %s", resourceLabel, uid)},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Resources[0], "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get "+resourceLabel+" succeeded", "uid", uid)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// SearchPastMeetingsArgs defines the input parameters for the search_past_meetings tool.
type SearchPastMeetingsArgs struct {
	Name         string   `json:"name,omitempty" jsonschema:"Name or partial name of the past meeting to search for"`
	ProjectUID   string   `json:"project_uid,omitempty" jsonschema:"Filter past meetings by project UID"`
	CommitteeUID string   `json:"committee_uid,omitempty" jsonschema:"Filter past meetings by committee UID"`
	MeetingID    string   `json:"meeting_id,omitempty" jsonschema:"Filter past meetings by meeting ID"`
	DateField    string   `json:"date_field,omitempty" jsonschema:"Date field to filter on (default start_time when date_from or date_to is set); also accepts end_time"`
	DateFrom     string   `json:"date_from,omitempty" jsonschema:"Start date inclusive in ISO 8601 format (e.g. 2025-01-01)"`
	DateTo       string   `json:"date_to,omitempty" jsonschema:"End date inclusive in ISO 8601 format (e.g. 2025-12-31)"`
	Filters      []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters"`
	Sort         string   `json:"sort,omitempty" jsonschema:"Sort order: name_asc (default), name_desc, updated_asc, updated_desc"`
	PageSize     int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken    string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// SearchPastMeetingsGroupArgs is the groups-mode variant of SearchPastMeetingsArgs.
type SearchPastMeetingsGroupArgs struct {
	Name       string   `json:"name,omitempty" jsonschema:"Name or partial name of the past meeting to search for"`
	ProjectUID string   `json:"project_uid,omitempty" jsonschema:"Filter past meetings by project UID"`
	GroupUID   string   `json:"group_uid,omitempty" jsonschema:"Filter past meetings by group UID (also known as committee UID)"`
	MeetingID  string   `json:"meeting_id,omitempty" jsonschema:"Filter past meetings by meeting ID"`
	DateField  string   `json:"date_field,omitempty" jsonschema:"Date field to filter on (default start_time when date_from or date_to is set); also accepts end_time"`
	DateFrom   string   `json:"date_from,omitempty" jsonschema:"Start date inclusive in ISO 8601 format (e.g. 2025-01-01)"`
	DateTo     string   `json:"date_to,omitempty" jsonschema:"End date inclusive in ISO 8601 format (e.g. 2025-12-31)"`
	Filters    []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters"`
	Sort       string   `json:"sort,omitempty" jsonschema:"Sort order: name_asc (default), name_desc, updated_asc, updated_desc"`
	PageSize   int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken  string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetPastMeetingArgs defines the input parameters for the get_past_meeting tool.
type GetPastMeetingArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the past meeting to retrieve"`
}

// handleSearchMeetingsGroupMode adapts group-mode args to the meetings handler.
func handleSearchMeetingsGroupMode(ctx context.Context, req *mcp.CallToolRequest, args SearchMeetingsGroupArgs) (*mcp.CallToolResult, any, error) {
	return handleSearchMeetings(ctx, req, SearchMeetingsArgs{
		Name:         args.Name,
		ProjectUID:   args.ProjectUID,
		CommitteeUID: args.GroupUID,
		DateField:    args.DateField,
		DateFrom:     args.DateFrom,
		DateTo:       args.DateTo,
		Filters:      args.Filters,
		Sort:         args.Sort,
		PageSize:     args.PageSize,
		PageToken:    args.PageToken,
	})
}

// handleSearchMeetingRegistrantsGroupMode adapts group-mode args to the meeting registrants handler.
func handleSearchMeetingRegistrantsGroupMode(ctx context.Context, req *mcp.CallToolRequest, args SearchMeetingRegistrantsGroupArgs) (*mcp.CallToolResult, any, error) {
	return handleSearchMeetingRegistrants(ctx, req, SearchMeetingRegistrantsArgs{
		MeetingID:    args.MeetingID,
		CommitteeUID: args.GroupUID,
		Name:         args.Name,
		Filters:      args.Filters,
		Sort:         args.Sort,
		PageSize:     args.PageSize,
		PageToken:    args.PageToken,
	})
}

// handleSearchPastMeetingsGroupMode adapts group-mode args to the past meetings handler.
func handleSearchPastMeetingsGroupMode(ctx context.Context, req *mcp.CallToolRequest, args SearchPastMeetingsGroupArgs) (*mcp.CallToolResult, any, error) {
	return handleSearchPastMeetings(ctx, req, SearchPastMeetingsArgs{
		Name:         args.Name,
		ProjectUID:   args.ProjectUID,
		CommitteeUID: args.GroupUID,
		MeetingID:    args.MeetingID,
		DateField:    args.DateField,
		DateFrom:     args.DateFrom,
		DateTo:       args.DateTo,
		Filters:      args.Filters,
		Sort:         args.Sort,
		PageSize:     args.PageSize,
		PageToken:    args.PageToken,
	})
}

// handleSearchPastMeetings implements the search_past_meetings tool logic.
func handleSearchPastMeetings(ctx context.Context, req *mcp.CallToolRequest, args SearchPastMeetingsArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: meeting tools not configured"},
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

	ctx = meetingConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := meetingConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	sort := args.Sort
	if sort == "" {
		sort = "name_asc"
	}

	resourceType := pastMeetingResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		PageSize: pageSize,
		Sort:     sort,
	}

	if args.Name != "" {
		payload.Name = &args.Name
	}

	// project_uid uses parent_ref; committee_uid and meeting_id use tags and can coexist.
	if args.ProjectUID != "" {
		parentRef := "project:" + args.ProjectUID
		payload.Parent = &parentRef
	}

	var tags []string
	if args.CommitteeUID != "" {
		tags = append(tags, fmt.Sprintf("committee_uid:%s", args.CommitteeUID))
	}
	if args.MeetingID != "" {
		tags = append(tags, fmt.Sprintf("meeting_id:%s", args.MeetingID))
	}
	if len(tags) > 0 {
		payload.Tags = tags
	}

	if args.DateFrom != "" || args.DateTo != "" {
		dateField := args.DateField
		if dateField == "" {
			dateField = "start_time"
		}
		payload.DateField = &dateField
		if args.DateFrom != "" {
			payload.DateFrom = &args.DateFrom
		}
		if args.DateTo != "" {
			payload.DateTo = &args.DateTo
		}
	}

	if len(args.Filters) > 0 {
		payload.Filters = args.Filters
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "searching past meetings",
		"name", args.Name,
		"project_uid", args.ProjectUID,
		"committee_uid", args.CommitteeUID,
		"meeting_id", args.MeetingID,
		"page_size", pageSize,
	)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to search past meetings", err)},
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

	logger.InfoContext(ctx, "search_past_meetings succeeded", "count", len(result.Resources))

	content := []mcp.Content{}
	if pageWarning != "" {
		content = append(content, &mcp.TextContent{Text: pageWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})
	return &mcp.CallToolResult{Content: content}, nil, nil
}

// handleGetPastMeeting implements the get_past_meeting tool logic.
func handleGetPastMeeting(ctx context.Context, req *mcp.CallToolRequest, args GetPastMeetingArgs) (*mcp.CallToolResult, any, error) {
	return handleGetPastMeetingResource(ctx, req, pastMeetingResourceType, "past meeting", args.UID)
}
