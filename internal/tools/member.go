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
	Search         string `json:"search,omitempty" jsonschema:"Free-text search across member name, project names, and tiers"`
	Status         string `json:"status,omitempty" jsonschema:"Filter by membership status (e.g. active, expired)"`
	MembershipType string `json:"membership_type,omitempty" jsonschema:"Filter by membership type"`
	AccountID      string `json:"account_id,omitempty" jsonschema:"Filter by account ID"`
	ProjectID      string `json:"project_id,omitempty" jsonschema:"Filter by project ID"`
	ProductID      string `json:"product_id,omitempty" jsonschema:"Filter by product ID"`
	Year           string `json:"year,omitempty" jsonschema:"Filter by membership year"`
	Tier           string `json:"tier,omitempty" jsonschema:"Filter by membership tier"`
	ContactID      string `json:"contact_id,omitempty" jsonschema:"Filter by contact ID"`
	AutoRenew      string `json:"auto_renew,omitempty" jsonschema:"Filter by auto-renew status (true or false)"`
	PageSize       int    `json:"page_size,omitempty" jsonschema:"Number of results per page (1-100, default 25)"`
	Offset         int    `json:"offset,omitempty" jsonschema:"Offset into the total result list (default 0)"`
}

// GetMemberMembershipArgs defines the input parameters for the get_member_membership tool.
type GetMemberMembershipArgs struct {
	MemberID string `json:"member_id" jsonschema:"The member UID"`
	ID       string `json:"id" jsonschema:"The membership UID"`
}

// GetMembershipKeyContactsArgs defines the input parameters for the get_membership_key_contacts tool.
type GetMembershipKeyContactsArgs struct {
	MemberID string `json:"member_id" jsonschema:"The member UID"`
	ID       string `json:"id" jsonschema:"The membership UID"`
}

// RegisterSearchMembers registers the search_members tool with the MCP server.
func RegisterSearchMembers(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "search_members",
		Description: "Search and filter members (memberships). Use this tool when users ask about members, memberships, or member organizations. Supports free-text search and filtering by status, membership_type, account_id, project_id, product_id, year, tier (e.g. gold, platinum, silver), contact_id, and auto_renew. Uses offset-based pagination.",
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
		Description: "Get a single member's membership by member ID and membership ID. Use this when users ask for details about a specific member or membership.",
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
		Description: "Get key contacts for a member's membership by member ID and membership ID. Returns the people associated with a member such as primary contacts and board members.",
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

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 25
	}

	// Build filter string from non-empty filter args.
	var filters []string
	if args.Status != "" {
		filters = append(filters, "status="+args.Status)
	}
	if args.MembershipType != "" {
		filters = append(filters, "membership_type="+args.MembershipType)
	}
	if args.AccountID != "" {
		filters = append(filters, "account_id="+args.AccountID)
	}
	if args.ProjectID != "" {
		filters = append(filters, "project_id="+args.ProjectID)
	}
	if args.ProductID != "" {
		filters = append(filters, "product_id="+args.ProductID)
	}
	if args.Year != "" {
		filters = append(filters, "year="+args.Year)
	}
	if args.Tier != "" {
		filters = append(filters, "tier="+args.Tier)
	}
	if args.ContactID != "" {
		filters = append(filters, "contact_id="+args.ContactID)
	}
	if args.AutoRenew != "" {
		filters = append(filters, "auto_renew="+args.AutoRenew)
	}

	version := "1"
	payload := &memberservice.ListMembersPayload{
		Version:  &version,
		PageSize: pageSize,
		Offset:   args.Offset,
	}

	if len(filters) > 0 {
		filterStr := strings.Join(filters, ";")
		payload.Filter = &filterStr
	}

	if args.Search != "" {
		payload.Search = &args.Search
	}

	logger.Info("searching members", "filter_count", len(filters), "page_size", pageSize, "offset", args.Offset, "search", args.Search)

	result, err := clients.Member.ListMembers(ctx, payload)
	if err != nil {
		logger.Error("ListMembers failed", "error", err)
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

	logger.Info("search_members succeeded", "count", len(result.Members))

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

	if args.MemberID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member_id is required"},
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

	logger.Info("fetching member membership", "member_id", args.MemberID, "id", args.ID)

	version := "1"
	result, err := clients.Member.GetMemberMembership(ctx, &memberservice.GetMemberMembershipPayload{
		Version:  &version,
		MemberID: &args.MemberID,
		ID:       &args.ID,
	})
	if err != nil {
		logger.Error("GetMemberMembership failed", "error", err, "member_id", args.MemberID, "id", args.ID)
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

	logger.Info("get_member_membership succeeded", "member_id", args.MemberID, "id", args.ID)

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

	if args.MemberID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member_id is required"},
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

	logger.Info("fetching membership key contacts", "member_id", args.MemberID, "id", args.ID)

	version := "1"
	result, err := clients.Member.ListMemberMembershipKeyContacts(ctx, &memberservice.ListMemberMembershipKeyContactsPayload{
		Version:  &version,
		MemberID: &args.MemberID,
		ID:       &args.ID,
	})
	if err != nil {
		logger.Error("ListMemberMembershipKeyContacts failed", "error", err, "member_id", args.MemberID, "id", args.ID)
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

	logger.Info("get_membership_key_contacts succeeded", "member_id", args.MemberID, "id", args.ID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}
