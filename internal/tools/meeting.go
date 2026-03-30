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

// pastMeetingTranscriptResourceType is the resource type filter for past meeting transcript queries.
const pastMeetingTranscriptResourceType = "v1_past_meeting_transcript"

// pastMeetingSummaryResourceType is the resource type filter for past meeting summary queries.
const pastMeetingSummaryResourceType = "v1_past_meeting_summary"

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
func RegisterSearchMeetings(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "search_meetings",
		Description: "Search for LFX meetings using the query service. Supports filtering by project, committee, date range, and other fields.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Meetings",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleSearchMeetings)
}

// RegisterGetMeeting registers the get_meeting tool with the MCP server.
func RegisterGetMeeting(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "get_meeting",
		Description: "Get an LFX meeting by its UID using the query service.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Meeting",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleGetMeeting)
}

// RegisterSearchMeetingRegistrants registers the search_meeting_registrants tool with the MCP server.
func RegisterSearchMeetingRegistrants(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "search_meeting_registrants",
		Description: "Search for LFX meeting registrants using the query service. Supports filtering by meeting, committee, project, date range, and other fields.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Meeting Registrants",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleSearchMeetingRegistrants)
}

// RegisterGetMeetingRegistrant registers the get_meeting_registrant tool with the MCP server.
func RegisterGetMeetingRegistrant(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "get_meeting_registrant",
		Description: "Get an LFX meeting registrant by its UID using the query service.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Meeting Registrant",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleGetMeetingRegistrant)
}

// RegisterSearchPastMeetingParticipants registers the search_past_meeting_participants tool with the MCP server.
func RegisterSearchPastMeetingParticipants(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "search_past_meeting_participants",
		Description: "Search for LFX past meeting participants using the query service. Supports filtering by meeting, committee, project, and other fields.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Past Meeting Participants",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleSearchPastMeetingParticipants)
}

// RegisterGetPastMeetingParticipant registers the get_past_meeting_participant tool with the MCP server.
func RegisterGetPastMeetingParticipant(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "get_past_meeting_participant",
		Description: "Get an LFX past meeting participant by its UID using the query service.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Past Meeting Participant",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleGetPastMeetingParticipant)
}

// RegisterSearchPastMeetingTranscripts registers the search_past_meeting_transcripts tool with the MCP server.
func RegisterSearchPastMeetingTranscripts(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "search_past_meeting_transcripts",
		Description: "Search for LFX past meeting transcripts using the query service. Supports filtering by meeting, committee, project, and other fields.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Past Meeting Transcripts",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleSearchPastMeetingTranscripts)
}

// RegisterGetPastMeetingTranscript registers the get_past_meeting_transcript tool with the MCP server.
func RegisterGetPastMeetingTranscript(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "get_past_meeting_transcript",
		Description: "Get an LFX past meeting transcript by its UID using the query service.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Past Meeting Transcript",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleGetPastMeetingTranscript)
}

// RegisterSearchPastMeetingSummaries registers the search_past_meeting_summaries tool with the MCP server.
func RegisterSearchPastMeetingSummaries(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "search_past_meeting_summaries",
		Description: "Search for LFX past meeting summaries using the query service. Supports filtering by meeting, committee, project, and other fields.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Past Meeting Summaries",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleSearchPastMeetingSummaries)
}

// RegisterGetPastMeetingSummary registers the get_past_meeting_summary tool with the MCP server.
func RegisterGetPastMeetingSummary(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "get_past_meeting_summary",
		Description: "Get an LFX past meeting summary by its UID using the query service.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Past Meeting Summary",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleGetPastMeetingSummary)
}

// SearchMeetingsArgs defines the input parameters for the search_meetings tool.
type SearchMeetingsArgs struct {
	Name         string   `json:"name,omitempty" jsonschema:"Name or partial name of the meeting to search for"`
	ProjectUID   string   `json:"project_uid,omitempty" jsonschema:"Filter meetings by project UID"`
	CommitteeUID string   `json:"committee_uid,omitempty" jsonschema:"Filter meetings by committee UID"`
	DateField    string   `json:"date_field,omitempty" jsonschema:"Date field to filter on (default start_time when date_from or date_to is set)"`
	DateFrom     string   `json:"date_from,omitempty" jsonschema:"Start date inclusive in ISO 8601 format (e.g. 2025-01-01)"`
	DateTo       string   `json:"date_to,omitempty" jsonschema:"End date inclusive in ISO 8601 format (e.g. 2025-12-31)"`
	Filters      []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters (e.g. visibility:public or status:active)"`
	Sort         string   `json:"sort,omitempty" jsonschema:"Sort order for results (default name_asc),enum=name_asc,enum=name_desc,enum=updated_asc,enum=updated_desc"`
	PageSize     int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken    string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetMeetingArgs defines the input parameters for the get_meeting tool.
type GetMeetingArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the meeting to retrieve"`
}

// SearchMeetingRegistrantsArgs defines the input parameters for the search_meeting_registrants tool.
type SearchMeetingRegistrantsArgs struct {
	MeetingID    string   `json:"meeting_id,omitempty" jsonschema:"Filter registrants by meeting ID"`
	CommitteeUID string   `json:"committee_uid,omitempty" jsonschema:"Filter registrants by committee UID"`
	ProjectUID   string   `json:"project_uid,omitempty" jsonschema:"Filter registrants by project UID"`
	Name         string   `json:"name,omitempty" jsonschema:"Name or partial name of the registrant to search for"`
	Filters      []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters (e.g. host:true or type:committee)"`
	Sort         string   `json:"sort,omitempty" jsonschema:"Sort order for results (default name_asc),enum=name_asc,enum=name_desc,enum=updated_asc,enum=updated_desc"`
	PageSize     int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken    string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetMeetingRegistrantArgs defines the input parameters for the get_meeting_registrant tool.
type GetMeetingRegistrantArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the meeting registrant to retrieve"`
}

// SearchPastMeetingParticipantsArgs defines the input parameters for the search_past_meeting_participants tool.
type SearchPastMeetingParticipantsArgs struct {
	MeetingID    string   `json:"meeting_id,omitempty" jsonschema:"Filter participants by meeting ID"`
	CommitteeUID string   `json:"committee_uid,omitempty" jsonschema:"Filter participants by committee UID"`
	ProjectUID   string   `json:"project_uid,omitempty" jsonschema:"Filter participants by project UID"`
	Name         string   `json:"name,omitempty" jsonschema:"Name or partial name of the participant to search for"`
	Filters      []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters"`
	Sort         string   `json:"sort,omitempty" jsonschema:"Sort order for results (default name_asc),enum=name_asc,enum=name_desc,enum=updated_asc,enum=updated_desc"`
	PageSize     int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken    string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetPastMeetingParticipantArgs defines the input parameters for the get_past_meeting_participant tool.
type GetPastMeetingParticipantArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the past meeting participant to retrieve"`
}

// SearchPastMeetingTranscriptsArgs defines the input parameters for the search_past_meeting_transcripts tool.
type SearchPastMeetingTranscriptsArgs struct {
	MeetingID    string   `json:"meeting_id,omitempty" jsonschema:"Filter transcripts by meeting ID"`
	CommitteeUID string   `json:"committee_uid,omitempty" jsonschema:"Filter transcripts by committee UID"`
	ProjectUID   string   `json:"project_uid,omitempty" jsonschema:"Filter transcripts by project UID"`
	Name         string   `json:"name,omitempty" jsonschema:"Name or partial name of the transcript to search for"`
	Filters      []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters"`
	Sort         string   `json:"sort,omitempty" jsonschema:"Sort order for results (default name_asc),enum=name_asc,enum=name_desc,enum=updated_asc,enum=updated_desc"`
	PageSize     int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken    string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetPastMeetingTranscriptArgs defines the input parameters for the get_past_meeting_transcript tool.
type GetPastMeetingTranscriptArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the past meeting transcript to retrieve"`
}

// SearchPastMeetingSummariesArgs defines the input parameters for the search_past_meeting_summaries tool.
type SearchPastMeetingSummariesArgs struct {
	MeetingID    string   `json:"meeting_id,omitempty" jsonschema:"Filter summaries by meeting ID"`
	CommitteeUID string   `json:"committee_uid,omitempty" jsonschema:"Filter summaries by committee UID"`
	ProjectUID   string   `json:"project_uid,omitempty" jsonschema:"Filter summaries by project UID"`
	Name         string   `json:"name,omitempty" jsonschema:"Name or partial name of the summary to search for"`
	Filters      []string `json:"filters,omitempty" jsonschema:"Direct field:value term filters"`
	Sort         string   `json:"sort,omitempty" jsonschema:"Sort order for results (default name_asc),enum=name_asc,enum=name_desc,enum=updated_asc,enum=updated_desc"`
	PageSize     int      `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken    string   `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
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

	if args.ProjectUID != "" {
		parentRef := "project:" + args.ProjectUID
		payload.Parent = &parentRef
	}

	var tags []string
	if args.CommitteeUID != "" {
		tags = append(tags, fmt.Sprintf("committee_uid:%s", args.CommitteeUID))
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

	var tags []string
	if args.MeetingID != "" {
		tags = append(tags, fmt.Sprintf("meeting_id:%s", args.MeetingID))
	}
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

	if len(args.Filters) > 0 {
		payload.Filters = args.Filters
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "searching meeting registrants", "meeting_id", args.MeetingID, "committee_uid", args.CommitteeUID, "project_uid", args.ProjectUID, "name", args.Name, "page_size", pageSize)

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
	return handleSearchPastMeetingResource(ctx, req, pastMeetingParticipantResourceType, "past meeting participants", args.MeetingID, args.CommitteeUID, args.ProjectUID, args.Name, args.Filters, args.Sort, args.PageSize, args.PageToken)
}

// handleGetPastMeetingParticipant implements the get_past_meeting_participant tool logic.
func handleGetPastMeetingParticipant(ctx context.Context, req *mcp.CallToolRequest, args GetPastMeetingParticipantArgs) (*mcp.CallToolResult, any, error) {
	return handleGetPastMeetingResource(ctx, req, pastMeetingParticipantResourceType, "past meeting participant", args.UID)
}

// handleSearchPastMeetingTranscripts implements the search_past_meeting_transcripts tool logic.
func handleSearchPastMeetingTranscripts(ctx context.Context, req *mcp.CallToolRequest, args SearchPastMeetingTranscriptsArgs) (*mcp.CallToolResult, any, error) {
	return handleSearchPastMeetingResource(ctx, req, pastMeetingTranscriptResourceType, "past meeting transcripts", args.MeetingID, args.CommitteeUID, args.ProjectUID, args.Name, args.Filters, args.Sort, args.PageSize, args.PageToken)
}

// handleGetPastMeetingTranscript implements the get_past_meeting_transcript tool logic.
func handleGetPastMeetingTranscript(ctx context.Context, req *mcp.CallToolRequest, args GetPastMeetingTranscriptArgs) (*mcp.CallToolResult, any, error) {
	return handleGetPastMeetingResource(ctx, req, pastMeetingTranscriptResourceType, "past meeting transcript", args.UID)
}

// handleSearchPastMeetingSummaries implements the search_past_meeting_summaries tool logic.
func handleSearchPastMeetingSummaries(ctx context.Context, req *mcp.CallToolRequest, args SearchPastMeetingSummariesArgs) (*mcp.CallToolResult, any, error) {
	return handleSearchPastMeetingResource(ctx, req, pastMeetingSummaryResourceType, "past meeting summaries", args.MeetingID, args.CommitteeUID, args.ProjectUID, args.Name, args.Filters, args.Sort, args.PageSize, args.PageToken)
}

// handleGetPastMeetingSummary implements the get_past_meeting_summary tool logic.
func handleGetPastMeetingSummary(ctx context.Context, req *mcp.CallToolRequest, args GetPastMeetingSummaryArgs) (*mcp.CallToolResult, any, error) {
	return handleGetPastMeetingResource(ctx, req, pastMeetingSummaryResourceType, "past meeting summary", args.UID)
}

// handleSearchPastMeetingResource is a shared implementation for searching past meeting resource types.
func handleSearchPastMeetingResource(ctx context.Context, req *mcp.CallToolRequest, resourceType, resourceLabel, meetingID, committeeUID, projectUID, name string, filters []string, sort string, pageSize int, pageToken string) (*mcp.CallToolResult, any, error) {
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
	if committeeUID != "" {
		tags = append(tags, fmt.Sprintf("committee_uid:%s", committeeUID))
	}
	if projectUID != "" {
		tags = append(tags, fmt.Sprintf("project_uid:%s", projectUID))
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

	logger.InfoContext(ctx, "searching "+resourceLabel, "meeting_id", meetingID, "committee_uid", committeeUID, "project_uid", projectUID, "name", name, "page_size", pageSize)

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
