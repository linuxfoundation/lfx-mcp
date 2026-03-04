// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

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

// CommitteeConfig holds configuration shared by committee tools.
type CommitteeConfig struct {
	LFXAPIURL           string
	TokenExchangeClient *lfxv2.TokenExchangeClient
	DebugLogger         *slog.Logger
}

var committeeConfig *CommitteeConfig

// SetCommitteeConfig sets the configuration for committee tools.
func SetCommitteeConfig(cfg *CommitteeConfig) {
	committeeConfig = cfg
}

// RegisterSearchCommittees registers the search_committees tool with the MCP server.
func RegisterSearchCommittees(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_committees",
		Description: "Search for LFX committees by name using the LFX query service. Optionally filter by project UID.",
	}, handleSearchCommittees)
}

// RegisterGetCommittee registers the get_committee tool with the MCP server.
func RegisterGetCommittee(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_committee",
		Description: "Get an LFX committee's base info and settings by its UID. Privileged committee settings may be omitted if the caller lacks sufficient permissions.",
	}, handleGetCommittee)
}

// RegisterGetCommitteeMember registers the get_committee_member tool with the MCP server.
func RegisterGetCommitteeMember(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_committee_member",
		Description: "Get a specific committee member by committee UID and member UID.",
	}, handleGetCommitteeMember)
}

// RegisterSearchCommitteeMembers registers the search_committee_members tool with the MCP server.
func RegisterSearchCommitteeMembers(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_committee_members",
		Description: "Search for members of a specific LFX committee. Requires a committee UID. Optionally filter by name.",
	}, handleSearchCommitteeMembers)
}

// SearchCommitteesArgs defines the input parameters for the search_committees tool.
type SearchCommitteesArgs struct {
	Name       string `json:"name,omitempty" jsonschema:"Name or partial name of the committee to search for"`
	ProjectUID string `json:"project_uid,omitempty" jsonschema:"Optional v2 project UID to filter committees by project (e.g. a27394a3-7a6c-4d0f-9e0f-692d8753924f)"`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken  string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// GetCommitteeArgs defines the input parameters for the get_committee tool.
type GetCommitteeArgs struct {
	UID string `json:"uid" jsonschema:"The v2 UID of the committee to retrieve"`
}

// GetCommitteeMemberArgs defines the input parameters for the get_committee_member tool.
type GetCommitteeMemberArgs struct {
	CommitteeUID string `json:"committee_uid" jsonschema:"The v2 UID of the committee"`
	MemberUID    string `json:"member_uid" jsonschema:"The v2 UID of the committee member"`
}

// SearchCommitteeMembersArgs defines the input parameters for the search_committee_members tool.
type SearchCommitteeMembersArgs struct {
	CommitteeUID string `json:"committee_uid" jsonschema:"The v2 UID of the committee whose members to search"`
	Name         string `json:"name,omitempty" jsonschema:"Name or partial name of the member to search for"`
	PageSize     int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken    string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// handleSearchCommittees implements the search_committees tool logic.
func handleSearchCommittees(ctx context.Context, req *mcp.CallToolRequest, args SearchCommitteesArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if committeeConfig == nil {
		logger.Error("committee tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: committee tools not configured"},
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
		APIDomain:           committeeConfig.LFXAPIURL,
		TokenExchangeClient: committeeConfig.TokenExchangeClient,
		DebugLogger:         committeeConfig.DebugLogger,
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

	logger.Info("searching committees", "name", args.Name, "project_uid", args.ProjectUID, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.Error("QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to search committees: %s", lfxv2.ErrorMessage(err))},
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

	logger.Info("search_committees succeeded", "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetCommittee implements the get_committee tool logic, fetching both base
// info and settings for the given committee UID.
func handleGetCommittee(ctx context.Context, req *mcp.CallToolRequest, args GetCommitteeArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if committeeConfig == nil {
		logger.Error("committee tools not configured")
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
		APIDomain:           committeeConfig.LFXAPIURL,
		TokenExchangeClient: committeeConfig.TokenExchangeClient,
		DebugLogger:         committeeConfig.DebugLogger,
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

	logger.Info("fetching committee", "uid", args.UID)

	baseResult, err := clients.Committee.GetCommitteeBase(ctx, &committeeservice.GetCommitteeBasePayload{
		UID: &args.UID,
	})
	if err != nil {
		logger.Error("GetCommitteeBase failed", "error", err, "uid", args.UID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get committee: %s", lfxv2.ErrorMessage(err))},
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
	if err != nil {
		logger.Warn("getting privileged committee settings failed, returning base only", "error", lfxv2.ErrorMessage(err), "uid", args.UID)
	} else {
		committeeSettings = settingsResult.CommitteeSettings
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
		logger.Error("failed to marshal committee result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_committee succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetCommitteeMember implements the get_committee_member tool logic.
func handleGetCommitteeMember(ctx context.Context, req *mcp.CallToolRequest, args GetCommitteeMemberArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if committeeConfig == nil {
		logger.Error("committee tools not configured")
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
		APIDomain:           committeeConfig.LFXAPIURL,
		TokenExchangeClient: committeeConfig.TokenExchangeClient,
		DebugLogger:         committeeConfig.DebugLogger,
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

	logger.Info("fetching committee member", "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)

	result, err := clients.Committee.GetCommitteeMember(ctx, &committeeservice.GetCommitteeMemberPayload{
		Version:   "1",
		UID:       args.CommitteeUID,
		MemberUID: args.MemberUID,
	})
	if err != nil {
		logger.Error("GetCommitteeMember failed", "error", err, "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get committee member: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Member, "", "  ")
	if err != nil {
		logger.Error("failed to marshal committee member result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_committee_member succeeded", "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleSearchCommitteeMembers implements the search_committee_members tool logic.
func handleSearchCommitteeMembers(ctx context.Context, req *mcp.CallToolRequest, args SearchCommitteeMembersArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if committeeConfig == nil {
		logger.Error("committee tools not configured")
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
		APIDomain:           committeeConfig.LFXAPIURL,
		TokenExchangeClient: committeeConfig.TokenExchangeClient,
		DebugLogger:         committeeConfig.DebugLogger,
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

	resourceType := committeeMemberResourceType
	// Members are tagged with committee_uid:<uid> by the committee service indexer.
	committeeTag := fmt.Sprintf("committee_uid:%s", args.CommitteeUID)
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		Tags:     []string{committeeTag},
		PageSize: pageSize,
		Sort:     "name_asc",
	}

	if args.Name != "" {
		payload.Name = &args.Name
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.Info("searching committee members", "committee_uid", args.CommitteeUID, "name", args.Name, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.Error("QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to search committee members: %s", lfxv2.ErrorMessage(err))},
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

	logger.Info("search_committee_members succeeded", "committee_uid", args.CommitteeUID, "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}
