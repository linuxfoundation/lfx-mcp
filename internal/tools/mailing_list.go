// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	mailinglist "github.com/linuxfoundation/lfx-v2-mailing-list-service/gen/mailing_list"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mailingListResourceType is the resource type filter for mailing list queries.
const mailingListResourceType = "grpsio_mailing_list"

// mailingListMemberResourceType is the resource type filter for mailing list member queries.
const mailingListMemberResourceType = "grpsio_mailing_list_member"

// MailingListConfig holds configuration shared by mailing list tools.
type MailingListConfig struct {
	LFXAPIURL           string
	TokenExchangeClient *lfxv2.TokenExchangeClient
	DebugLogger         *slog.Logger
}

var mailingListConfig *MailingListConfig

// SetMailingListConfig sets the configuration for mailing list tools.
func SetMailingListConfig(cfg *MailingListConfig) {
	mailingListConfig = cfg
}

// GetMailingListServiceArgs defines the input parameters for the get_mailing_list_service tool.
type GetMailingListServiceArgs struct {
	UID string `json:"uid" jsonschema:"The v2 UID of the mailing list service to retrieve"`
}

// GetMailingListArgs defines the input parameters for the get_mailing_list tool.
type GetMailingListArgs struct {
	UID string `json:"uid" jsonschema:"The v2 UID of the mailing list to retrieve"`
}

// GetMailingListMemberArgs defines the input parameters for the get_mailing_list_member tool.
type GetMailingListMemberArgs struct {
	MailingListUID string `json:"mailing_list_uid" jsonschema:"The v2 UID of the mailing list"`
	MemberUID      string `json:"member_uid" jsonschema:"The v2 UID of the mailing list member"`
}

// SearchMailingListMembersArgs defines the input parameters for the search_mailing_list_members tool.
type SearchMailingListMembersArgs struct {
	MailingListUID string `json:"mailing_list_uid" jsonschema:"The v2 UID of the mailing list whose members to search"`
	Name           string `json:"name,omitempty" jsonschema:"Name or partial name of the member to search for"`
	PageSize       int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken      string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// SearchMailingListsArgs defines the input parameters for the search_mailing_lists tool.
type SearchMailingListsArgs struct {
	Name       string `json:"name,omitempty" jsonschema:"Name or partial name of the mailing list to search for"`
	ProjectUID string `json:"project_uid,omitempty" jsonschema:"Optional v2 project UID to filter mailing lists by project (e.g. a27394a3-7a6c-4d0f-9e0f-692d8753924f)"`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken  string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// RegisterGetMailingListService registers the get_mailing_list_service tool with the MCP server.
func RegisterGetMailingListService(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_mailing_list_service",
		Description: "Get a mailing list service's base info and settings by its UID. Privileged settings may be omitted if the caller lacks sufficient permissions.",
	}, handleGetMailingListService)
}

// RegisterGetMailingList registers the get_mailing_list tool with the MCP server.
func RegisterGetMailingList(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_mailing_list",
		Description: "Get a mailing list's base info and settings by its UID. Privileged settings may be omitted if the caller lacks sufficient permissions.",
	}, handleGetMailingList)
}

// RegisterGetMailingListMember registers the get_mailing_list_member tool with the MCP server.
func RegisterGetMailingListMember(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_mailing_list_member",
		Description: "Get a specific mailing list member by mailing list UID and member UID.",
	}, handleGetMailingListMember)
}

// RegisterSearchMailingListMembers registers the search_mailing_list_members tool with the MCP server.
func RegisterSearchMailingListMembers(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_mailing_list_members",
		Description: "Search for members of a specific mailing list. Requires a mailing list UID. Optionally filter by name.",
	}, handleSearchMailingListMembers)
}

// RegisterSearchMailingLists registers the search_mailing_lists tool with the MCP server.
func RegisterSearchMailingLists(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_mailing_lists",
		Description: "Search for LFX mailing lists by name using the LFX query service. Optionally filter by project UID.",
	}, handleSearchMailingLists)
}

// handleGetMailingListService implements the get_mailing_list_service tool logic.
func handleGetMailingListService(ctx context.Context, req *mcp.CallToolRequest, args GetMailingListServiceArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if mailingListConfig == nil {
		logger.Error("mailing list tools not configured")
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
		APIDomain:           mailingListConfig.LFXAPIURL,
		TokenExchangeClient: mailingListConfig.TokenExchangeClient,
		DebugLogger:         mailingListConfig.DebugLogger,
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

	logger.Info("fetching mailing list service", "uid", args.UID)

	baseResult, err := clients.MailingList.GetGrpsioService(ctx, &mailinglist.GetGrpsioServicePayload{
		UID: &args.UID,
	})
	if err != nil {
		logger.Error("GetGrpsioService failed", "error", err, "uid", args.UID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get mailing list service: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	var serviceSettings *mailinglist.GrpsIoServiceSettings
	settingsResult, err := clients.MailingList.GetGrpsioServiceSettings(ctx, &mailinglist.GetGrpsioServiceSettingsPayload{
		UID: &args.UID,
	})
	if err != nil {
		logger.Warn("getting mailing list service settings failed, returning base only", "error", lfxv2.ErrorMessage(err), "uid", args.UID)
	} else {
		serviceSettings = settingsResult.ServiceSettings
	}

	type serviceResult struct {
		Base     *mailinglist.GrpsIoServiceWithReadonlyAttributes `json:"base"`
		Settings *mailinglist.GrpsIoServiceSettings               `json:"settings,omitempty"`
	}

	out := serviceResult{
		Base:     baseResult.Service,
		Settings: serviceSettings,
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.Error("failed to marshal mailing list service result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_mailing_list_service succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMailingList implements the get_mailing_list tool logic.
func handleGetMailingList(ctx context.Context, req *mcp.CallToolRequest, args GetMailingListArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if mailingListConfig == nil {
		logger.Error("mailing list tools not configured")
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
		APIDomain:           mailingListConfig.LFXAPIURL,
		TokenExchangeClient: mailingListConfig.TokenExchangeClient,
		DebugLogger:         mailingListConfig.DebugLogger,
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

	logger.Info("fetching mailing list", "uid", args.UID)

	baseResult, err := clients.MailingList.GetGrpsioMailingList(ctx, &mailinglist.GetGrpsioMailingListPayload{
		Version: "1",
		UID:     &args.UID,
	})
	if err != nil {
		logger.Error("GetGrpsioMailingList failed", "error", err, "uid", args.UID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get mailing list: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	var mlSettings *mailinglist.GrpsIoMailingListSettings
	settingsResult, err := clients.MailingList.GetGrpsioMailingListSettings(ctx, &mailinglist.GetGrpsioMailingListSettingsPayload{
		UID: &args.UID,
	})
	if err != nil {
		logger.Warn("getting mailing list settings failed, returning base only", "error", lfxv2.ErrorMessage(err), "uid", args.UID)
	} else {
		mlSettings = settingsResult.MailingListSettings
	}

	type mailingListResult struct {
		Base     *mailinglist.GrpsIoMailingListWithReadonlyAttributes `json:"base"`
		Settings *mailinglist.GrpsIoMailingListSettings               `json:"settings,omitempty"`
	}

	out := mailingListResult{
		Base:     baseResult.MailingList,
		Settings: mlSettings,
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.Error("failed to marshal mailing list result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_mailing_list succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMailingListMember implements the get_mailing_list_member tool logic.
func handleGetMailingListMember(ctx context.Context, req *mcp.CallToolRequest, args GetMailingListMemberArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if mailingListConfig == nil {
		logger.Error("mailing list tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: mailing list tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.MailingListUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: mailing_list_uid is required"},
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
		APIDomain:           mailingListConfig.LFXAPIURL,
		TokenExchangeClient: mailingListConfig.TokenExchangeClient,
		DebugLogger:         mailingListConfig.DebugLogger,
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

	logger.Info("fetching mailing list member", "mailing_list_uid", args.MailingListUID, "member_uid", args.MemberUID)

	result, err := clients.MailingList.GetGrpsioMailingListMember(ctx, &mailinglist.GetGrpsioMailingListMemberPayload{
		Version:   "1",
		UID:       args.MailingListUID,
		MemberUID: args.MemberUID,
	})
	if err != nil {
		logger.Error("GetGrpsioMailingListMember failed", "error", err, "mailing_list_uid", args.MailingListUID, "member_uid", args.MemberUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get mailing list member: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Member, "", "  ")
	if err != nil {
		logger.Error("failed to marshal mailing list member result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_mailing_list_member succeeded", "mailing_list_uid", args.MailingListUID, "member_uid", args.MemberUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleSearchMailingLists implements the search_mailing_lists tool logic.
func handleSearchMailingLists(ctx context.Context, req *mcp.CallToolRequest, args SearchMailingListsArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if mailingListConfig == nil {
		logger.Error("mailing list tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: mailing list tools not configured"},
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
		APIDomain:           mailingListConfig.LFXAPIURL,
		TokenExchangeClient: mailingListConfig.TokenExchangeClient,
		DebugLogger:         mailingListConfig.DebugLogger,
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

	logger.Info("searching mailing lists", "name", args.Name, "project_uid", args.ProjectUID, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.Error("QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to search mailing lists: %s", lfxv2.ErrorMessage(err))},
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

	logger.Info("search_mailing_lists succeeded", "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleSearchMailingListMembers implements the search_mailing_list_members tool logic.
func handleSearchMailingListMembers(ctx context.Context, req *mcp.CallToolRequest, args SearchMailingListMembersArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if mailingListConfig == nil {
		logger.Error("mailing list tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: mailing list tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.MailingListUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: mailing_list_uid is required"},
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
		APIDomain:           mailingListConfig.LFXAPIURL,
		TokenExchangeClient: mailingListConfig.TokenExchangeClient,
		DebugLogger:         mailingListConfig.DebugLogger,
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

	resourceType := mailingListMemberResourceType
	// Members are tagged with mailing_list_uid:<uid> by the mailing list service indexer.
	mlTag := fmt.Sprintf("mailing_list_uid:%s", args.MailingListUID)
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		Tags:     []string{mlTag},
		PageSize: pageSize,
		Sort:     "name_asc",
	}

	if args.Name != "" {
		payload.Name = &args.Name
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.Info("searching mailing list members", "mailing_list_uid", args.MailingListUID, "name", args.Name, "page_size", pageSize)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.Error("QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to search mailing list members: %s", lfxv2.ErrorMessage(err))},
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

	logger.Info("search_mailing_list_members succeeded", "mailing_list_uid", args.MailingListUID, "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}
