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
	ProjectUID string `json:"project_uid" jsonschema:"Project UUID (required)"`
	Search     string `json:"search,omitempty" jsonschema:"Free-text search across company name, project name, and tier"`
	TierUID    string `json:"tier_uid,omitempty" jsonschema:"Filter by membership tier UID (UUID from list_project_tiers)"`
	Sort       string `json:"sort,omitempty" jsonschema:"Sort order: newest (default), name, last_modified"`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken  string `json:"page_token,omitempty" jsonschema:"Opaque cursor from a previous response to fetch the next page"`
}

// GetMemberMembershipArgs defines the input parameters for the get_member_membership tool.
type GetMemberMembershipArgs struct {
	ProjectUID string `json:"project_uid" jsonschema:"Project UUID"`
	ID         string `json:"id" jsonschema:"The membership UID"`
}

// GetMembershipKeyContactsArgs defines the input parameters for the get_membership_key_contacts tool.
type GetMembershipKeyContactsArgs struct {
	ProjectUID string `json:"project_uid" jsonschema:"Project UUID"`
	ID         string `json:"id" jsonschema:"The membership UID"`
}

// ListProjectTiersArgs defines the input parameters for the list_project_tiers tool.
type ListProjectTiersArgs struct {
	ProjectUID string `json:"project_uid" jsonschema:"Project UUID (required)"`
}

// GetProjectTierArgs defines the input parameters for the get_project_tier tool.
type GetProjectTierArgs struct {
	ProjectUID string `json:"project_uid" jsonschema:"Project UUID (required)"`
	TierID     string `json:"tier_id" jsonschema:"Membership tier UID"`
}

// GetMembershipKeyContactArgs defines the input parameters for the get_membership_key_contact tool.
type GetMembershipKeyContactArgs struct {
	ProjectUID string `json:"project_uid" jsonschema:"Project UUID"`
	ID         string `json:"id" jsonschema:"The membership UID"`
	Cid        string `json:"cid" jsonschema:"Key contact UID"`
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

// RegisterListProjectTiers registers the list_project_tiers tool with the MCP server.
func RegisterListProjectTiers(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "list_project_tiers",
		Description: "List all membership tiers (e.g. Gold, Silver, Bronze) defined for a project. Use this to discover available tier UIDs before filtering search_members by tier_uid.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "List Project Tiers",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleListProjectTiers)
}

// RegisterGetProjectTier registers the get_project_tier tool with the MCP server.
func RegisterGetProjectTier(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "get_project_tier",
		Description: "Get a single membership tier by project UID and tier UID.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Project Tier",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleGetProjectTier)
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

// RegisterGetMembershipKeyContact registers the get_membership_key_contact tool with the MCP server.
func RegisterGetMembershipKeyContact(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "get_membership_key_contact",
		Description: "Get a single key contact for a membership by project UID, membership UID, and key contact UID.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Membership Key Contact",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleGetMembershipKeyContact)
}

// handleSearchMembers implements the search_members tool logic.
func handleSearchMembers(ctx context.Context, req *mcp.CallToolRequest, args SearchMembersArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(req)

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
		pageSize = 10
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
	logger := newToolLogger(req)

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

// handleListProjectTiers implements the list_project_tiers tool logic.
func handleListProjectTiers(ctx context.Context, req *mcp.CallToolRequest, args ListProjectTiersArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(req)

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

	logger.Info("listing project tiers", "project_uid", args.ProjectUID)

	version := "1"
	result, err := clients.Member.ListProjectTiers(ctx, &memberservice.ListProjectTiersPayload{
		Version:    &version,
		ProjectUID: &args.ProjectUID,
	})
	if err != nil {
		logger.Error("ListProjectTiers failed", "error", err, "project_uid", args.ProjectUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to list project tiers: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Tiers, "", "  ")
	if err != nil {
		logger.Error("failed to marshal tiers result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("list_project_tiers succeeded", "project_uid", args.ProjectUID, "count", len(result.Tiers))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetProjectTier implements the get_project_tier tool logic.
func handleGetProjectTier(ctx context.Context, req *mcp.CallToolRequest, args GetProjectTierArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(req)

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

	if args.TierID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: tier_id is required"},
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

	logger.Info("fetching project tier", "project_uid", args.ProjectUID, "tier_id", args.TierID)

	version := "1"
	result, err := clients.Member.GetProjectTier(ctx, &memberservice.GetProjectTierPayload{
		Version:    &version,
		ProjectUID: &args.ProjectUID,
		TierID:     &args.TierID,
	})
	if err != nil {
		logger.Error("GetProjectTier failed", "error", err, "project_uid", args.ProjectUID, "tier_id", args.TierID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get project tier: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Tier, "", "  ")
	if err != nil {
		logger.Error("failed to marshal tier result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_project_tier succeeded", "project_uid", args.ProjectUID, "tier_id", args.TierID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMembershipKeyContacts implements the get_membership_key_contacts tool logic.
func handleGetMembershipKeyContacts(ctx context.Context, req *mcp.CallToolRequest, args GetMembershipKeyContactsArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(req)

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

// handleGetMembershipKeyContact implements the get_membership_key_contact tool logic.
func handleGetMembershipKeyContact(ctx context.Context, req *mcp.CallToolRequest, args GetMembershipKeyContactArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(req)

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
				&mcp.TextContent{Text: "Error: id (membership UID) is required"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.Cid == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: cid (key contact UID) is required"},
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

	logger.Info("fetching membership key contact", "project_uid", args.ProjectUID, "id", args.ID, "cid", args.Cid)

	version := "1"
	result, err := clients.Member.GetMembershipKeyContact(ctx, &memberservice.GetMembershipKeyContactPayload{
		Version:    &version,
		ProjectUID: &args.ProjectUID,
		ID:         &args.ID,
		Cid:        &args.Cid,
	})
	if err != nil {
		logger.Error("GetMembershipKeyContact failed", "error", err, "project_uid", args.ProjectUID, "id", args.ID, "cid", args.Cid)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get membership key contact: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Contact, "", "  ")
	if err != nil {
		logger.Error("failed to marshal key contact result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_membership_key_contact succeeded", "project_uid", args.ProjectUID, "id", args.ID, "cid", args.Cid)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}
