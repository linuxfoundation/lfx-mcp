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
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// meetingResourceType is the resource type filter for meeting queries.
const meetingResourceType = "v1_meeting"

// meetingRegistrantResourceType is the resource type filter for meeting registrant queries.
const meetingRegistrantResourceType = "v1_meeting_registrant"

// MeetingConfig holds configuration shared by meeting tools.
type MeetingConfig struct {
	LFXAPIURL           string
	TokenExchangeClient *lfxv2.TokenExchangeClient
	DebugLogger         *slog.Logger
}

var meetingConfig *MeetingConfig

// SetMeetingConfig sets the configuration for meeting tools.
func SetMeetingConfig(cfg *MeetingConfig) {
	meetingConfig = cfg
}

// RegisterSearchMeetings registers the search_meetings tool with the MCP server.
func RegisterSearchMeetings(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_meetings",
		Description: "Search for LFX meetings using the query service. Supports filtering by project, committee, date range, and other fields.",
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
func RegisterSearchMeetingRegistrants(server *mcp.Server) {
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

// handleSearchMeetings implements the search_meetings tool logic.
func handleSearchMeetings(ctx context.Context, req *mcp.CallToolRequest, args SearchMeetingsArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if meetingConfig == nil {
		logger.Error("meeting tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: meeting tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.Error("failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           meetingConfig.LFXAPIURL,
		TokenExchangeClient: meetingConfig.TokenExchangeClient,
		DebugLogger:         meetingConfig.DebugLogger,
	})
	if err != nil {
		logger.Error("failed to create LFX v2 clients", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to connect to LFX API: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

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

	logger.Info("searching meetings", "name", args.Name, "project_uid", args.ProjectUID, "committee_uid", args.CommitteeUID, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.Error("QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to search meetings: %s", lfxv2.ErrorMessage(err))},
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

	if result.PageToken != nil && len(result.Resources) < pageSize {
		logger.Warn("some results on this page were excluded because you do not have access to them; consider continuing with the next page token, increasing the page size, or narrowing your filters")
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.Error("failed to marshal search result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("search_meetings succeeded", "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMeeting implements the get_meeting tool logic.
func handleGetMeeting(ctx context.Context, req *mcp.CallToolRequest, args GetMeetingArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if meetingConfig == nil {
		logger.Error("meeting tools not configured")
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
		logger.Error("failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           meetingConfig.LFXAPIURL,
		TokenExchangeClient: meetingConfig.TokenExchangeClient,
		DebugLogger:         meetingConfig.DebugLogger,
	})
	if err != nil {
		logger.Error("failed to create LFX v2 clients", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to connect to LFX API: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("fetching meeting", "uid", args.UID)

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
		logger.Error("QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get meeting: %s", lfxv2.ErrorMessage(err))},
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
		logger.Error("failed to marshal meeting result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_meeting succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleSearchMeetingRegistrants implements the search_meeting_registrants tool logic.
func handleSearchMeetingRegistrants(ctx context.Context, req *mcp.CallToolRequest, args SearchMeetingRegistrantsArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if meetingConfig == nil {
		logger.Error("meeting tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: meeting tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.Error("failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           meetingConfig.LFXAPIURL,
		TokenExchangeClient: meetingConfig.TokenExchangeClient,
		DebugLogger:         meetingConfig.DebugLogger,
	})
	if err != nil {
		logger.Error("failed to create LFX v2 clients", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to connect to LFX API: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

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

	logger.Info("searching meeting registrants", "meeting_id", args.MeetingID, "committee_uid", args.CommitteeUID, "project_uid", args.ProjectUID, "name", args.Name, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.Error("QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to search meeting registrants: %s", lfxv2.ErrorMessage(err))},
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

	if result.PageToken != nil && len(result.Resources) < pageSize {
		logger.Warn("some results on this page were excluded because you do not have access to them; consider continuing with the next page token, increasing the page size, or narrowing your filters")
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.Error("failed to marshal search result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("search_meeting_registrants succeeded", "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMeetingRegistrant implements the get_meeting_registrant tool logic.
func handleGetMeetingRegistrant(ctx context.Context, req *mcp.CallToolRequest, args GetMeetingRegistrantArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if meetingConfig == nil {
		logger.Error("meeting tools not configured")
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
		logger.Error("failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           meetingConfig.LFXAPIURL,
		TokenExchangeClient: meetingConfig.TokenExchangeClient,
		DebugLogger:         meetingConfig.DebugLogger,
	})
	if err != nil {
		logger.Error("failed to create LFX v2 clients", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to connect to LFX API: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("fetching meeting registrant", "uid", args.UID)

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
		logger.Error("QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get meeting registrant: %s", lfxv2.ErrorMessage(err))},
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
		logger.Error("failed to marshal meeting registrant result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_meeting_registrant succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}
