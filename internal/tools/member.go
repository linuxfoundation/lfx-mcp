// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	memberservice "github.com/linuxfoundation/lfx-v2-member-service/gen/membership_service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MemberConfig holds configuration shared by member tools.
type MemberConfig struct {
	LFXAPIURL           string
	TokenExchangeClient *lfxv2.TokenExchangeClient
	DebugLogger         *slog.Logger
}

var memberConfig *MemberConfig

// SetMemberConfig sets the configuration for member tools.
func SetMemberConfig(cfg *MemberConfig) {
	memberConfig = cfg
}

// SearchMembersArgs defines the input parameters for the search_members tool.
type SearchMembersArgs struct {
	ProjectUID string `json:"project_uid" jsonschema:"V2 project UUID (required)"`
	Search     string `json:"search,omitempty" jsonschema:"Free-text search across company name, project name, and tier"`
	TierUID    string `json:"tier_uid,omitempty" jsonschema:"Filter by membership tier UID (UUID from list_project_tiers)"`
	Sort       string `json:"sort,omitempty" jsonschema:"Sort order: newest (default), name, last_modified"`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"Number of results per page (1-1000, default 25)"`
	PageToken  string `json:"page_token,omitempty" jsonschema:"Opaque cursor from a previous response to fetch the next page"`
}

// GetMemberMembershipArgs defines the input parameters for the get_member_membership tool.
type GetMemberMembershipArgs struct {
	ProjectUID string `json:"project_uid" jsonschema:"V2 project UUID"`
	ID         string `json:"id" jsonschema:"The membership UID"`
}

// GetMembershipKeyContactsArgs defines the input parameters for the get_membership_key_contacts tool.
type GetMembershipKeyContactsArgs struct {
	ProjectUID string `json:"project_uid" jsonschema:"V2 project UUID"`
	ID         string `json:"id" jsonschema:"The membership UID"`
}

// RegisterSearchMembers registers the search_members tool with the MCP server.
func RegisterSearchMembers(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "search_members",
		Description: "List and search memberships for a project. Use this tool when users ask about members, memberships, or member organizations for a specific project. Requires project_uid. Supports free-text search, tier_uid filter, and sort order (newest, name, last_modified). Uses cursor-based pagination via page_token.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Members",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleSearchMembers)
}

// RegisterGetMemberMembership registers the get_member_membership tool with the MCP server.
func RegisterGetMemberMembership(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "get_member_membership",
		Description: "Get a single membership by project UID and membership ID. Use this when users ask for details about a specific membership.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Member Membership",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleGetMemberMembership)
}

// RegisterGetMembershipKeyContacts registers the get_membership_key_contacts tool with the MCP server.
func RegisterGetMembershipKeyContacts(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "get_membership_key_contacts",
		Description: "Get key contacts for a membership by project UID and membership ID. Returns the people associated with a membership such as primary contacts and board members.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Membership Key Contacts",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleGetMembershipKeyContacts)
}

// handleSearchMembers implements the search_members tool logic.
func handleSearchMembers(ctx context.Context, req *mcp.CallToolRequest, args SearchMembersArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if memberConfig == nil {
		logger.Error("member tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member tools not configured"},
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
		APIDomain:           memberConfig.LFXAPIURL,
		TokenExchangeClient: memberConfig.TokenExchangeClient,
		DebugLogger:         memberConfig.DebugLogger,
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

	if args.ProjectUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: project_uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 25
	}

	// Build filter string from non-empty filter args.
	var filters []string
	if args.TierUID != "" {
		filters = append(filters, "tier_uid="+args.TierUID)
	}

	version := "1"
	payload := &memberservice.ListProjectMembershipsPayload{
		Version:    &version,
		ProjectUID: &args.ProjectUID,
		PageSize:   pageSize,
		Sort:       args.Sort,
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	if len(filters) > 0 {
		filterStr := strings.Join(filters, ";")
		payload.Filter = &filterStr
	}

	if args.Search != "" {
		payload.Search = &args.Search
	}

	logger.Info("searching members", "project_uid", args.ProjectUID, "filter_count", len(filters), "page_size", pageSize, "page_token", args.PageToken, "search", args.Search)

	result, err := clients.Member.ListProjectMemberships(ctx, payload)
	if err != nil {
		logger.Error("ListProjectMemberships failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to search members: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		logger.Error("failed to marshal search result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("search_members succeeded", "count", len(result.Memberships))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMemberMembership implements the get_member_membership tool logic.
func handleGetMemberMembership(ctx context.Context, req *mcp.CallToolRequest, args GetMemberMembershipArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if memberConfig == nil {
		logger.Error("member tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.ProjectUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: project_uid is required"},
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
		APIDomain:           memberConfig.LFXAPIURL,
		TokenExchangeClient: memberConfig.TokenExchangeClient,
		DebugLogger:         memberConfig.DebugLogger,
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

	logger.Info("fetching member membership", "project_uid", args.ProjectUID, "id", args.ID)

	version := "1"
	result, err := clients.Member.GetProjectMembership(ctx, &memberservice.GetProjectMembershipPayload{
		Version:    &version,
		ProjectUID: &args.ProjectUID,
		ID:         &args.ID,
	})
	if err != nil {
		logger.Error("GetProjectMembership failed", "error", err, "project_uid", args.ProjectUID, "id", args.ID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get member membership: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Membership, "", "  ")
	if err != nil {
		logger.Error("failed to marshal membership result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_member_membership succeeded", "project_uid", args.ProjectUID, "id", args.ID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMembershipKeyContacts implements the get_membership_key_contacts tool logic.
func handleGetMembershipKeyContacts(ctx context.Context, req *mcp.CallToolRequest, args GetMembershipKeyContactsArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if memberConfig == nil {
		logger.Error("member tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member tools not configured"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.ProjectUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: project_uid is required"},
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
		APIDomain:           memberConfig.LFXAPIURL,
		TokenExchangeClient: memberConfig.TokenExchangeClient,
		DebugLogger:         memberConfig.DebugLogger,
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

	logger.Info("fetching membership key contacts", "project_uid", args.ProjectUID, "id", args.ID)

	version := "1"
	result, err := clients.Member.ListMembershipKeyContacts(ctx, &memberservice.ListMembershipKeyContactsPayload{
		Version:    &version,
		ProjectUID: &args.ProjectUID,
		ID:         &args.ID,
	})
	if err != nil {
		logger.Error("ListMembershipKeyContacts failed", "error", err, "project_uid", args.ProjectUID, "id", args.ID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get membership key contacts: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Contacts, "", "  ")
	if err != nil {
		logger.Error("failed to marshal membership key contacts result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_membership_key_contacts succeeded", "project_uid", args.ProjectUID, "id", args.ID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}
