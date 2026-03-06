// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	membershipservice "github.com/linuxfoundation/lfx-v2-member-service/gen/membership_service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MembershipConfig holds configuration shared by membership tools.
type MembershipConfig struct {
	LFXAPIURL           string
	TokenExchangeClient *lfxv2.TokenExchangeClient
	DebugLogger         *slog.Logger
}

var membershipConfig *MembershipConfig

// SetMembershipConfig sets the configuration for membership tools.
func SetMembershipConfig(cfg *MembershipConfig) {
	membershipConfig = cfg
}

// SearchMembershipsArgs defines the input parameters for the search_memberships tool.
type SearchMembershipsArgs struct {
	Status         string `json:"status,omitempty" jsonschema:"Filter by membership status (e.g. active, expired)"`
	MembershipType string `json:"membership_type,omitempty" jsonschema:"Filter by membership type"`
	AccountID      string `json:"account_id,omitempty" jsonschema:"Filter by account ID"`
	ProjectID      string `json:"project_id,omitempty" jsonschema:"Filter by project ID"`
	ProductID      string `json:"product_id,omitempty" jsonschema:"Filter by product ID"`
	Year           string `json:"year,omitempty" jsonschema:"Filter by membership year"`
	Tier           string `json:"tier,omitempty" jsonschema:"Filter by membership tier"`
	ContactID      string `json:"contact_id,omitempty" jsonschema:"Filter by contact ID"`
	AutoRenew      string `json:"auto_renew,omitempty" jsonschema:"Filter by auto-renew status (true or false)"`
	PageSize       int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10)"`
	Offset         int    `json:"offset,omitempty" jsonschema:"Offset into the total result list (default 0)"`
}

// GetMembershipArgs defines the input parameters for the get_membership tool.
type GetMembershipArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the membership to retrieve"`
}

// GetMembershipContactsArgs defines the input parameters for the get_membership_contacts tool.
type GetMembershipContactsArgs struct {
	UID string `json:"uid" jsonschema:"The UID of the membership to get contacts for"`
}

// RegisterSearchMemberships registers the search_memberships tool with the MCP server.
func RegisterSearchMemberships(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_memberships",
		Description: "Search and filter members (memberships). Use this tool when users ask about members, memberships, or member organizations. Supports filtering by status, membership_type, account_id, project_id, product_id, year, tier (e.g. gold, platinum, silver), contact_id, and auto_renew. Uses offset-based pagination.",
	}, handleSearchMemberships)
}

// RegisterGetMembership registers the get_membership tool with the MCP server.
func RegisterGetMembership(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_membership",
		Description: "Get a single member (membership) by its UID. Use this when users ask for details about a specific member or membership.",
	}, handleGetMembership)
}

// RegisterGetMembershipContacts registers the get_membership_contacts tool with the MCP server.
func RegisterGetMembershipContacts(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_membership_contacts",
		Description: "Get key contacts for a member (membership) by its UID. Returns the people associated with a member such as primary contacts and board members.",
	}, handleGetMembershipContacts)
}

// handleSearchMemberships implements the search_memberships tool logic.
func handleSearchMemberships(ctx context.Context, req *mcp.CallToolRequest, args SearchMembershipsArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if membershipConfig == nil {
		logger.Error("membership tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: membership tools not configured"},
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
		APIDomain:           membershipConfig.LFXAPIURL,
		TokenExchangeClient: membershipConfig.TokenExchangeClient,
		DebugLogger:         membershipConfig.DebugLogger,
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
	payload := &membershipservice.ListMembershipsPayload{
		Version:  &version,
		PageSize: pageSize,
		Offset:   args.Offset,
	}

	if len(filters) > 0 {
		filterStr := strings.Join(filters, ";")
		payload.Filter = &filterStr
	}

	logger.Info("searching memberships", "filter_count", len(filters), "page_size", pageSize, "offset", args.Offset)

	result, err := clients.Membership.ListMemberships(ctx, payload)
	if err != nil {
		logger.Error("ListMemberships failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to search memberships: %s", lfxv2.ErrorMessage(err))},
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

	logger.Info("search_memberships succeeded", "count", len(result.Memberships))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMembership implements the get_membership tool logic.
func handleGetMembership(ctx context.Context, req *mcp.CallToolRequest, args GetMembershipArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if membershipConfig == nil {
		logger.Error("membership tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: membership tools not configured"},
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
		APIDomain:           membershipConfig.LFXAPIURL,
		TokenExchangeClient: membershipConfig.TokenExchangeClient,
		DebugLogger:         membershipConfig.DebugLogger,
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

	logger.Info("fetching membership", "uid", args.UID)

	version := "1"
	result, err := clients.Membership.GetMembership(ctx, &membershipservice.GetMembershipPayload{
		Version: &version,
		UID:     &args.UID,
	})
	if err != nil {
		logger.Error("GetMembership failed", "error", err, "uid", args.UID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get membership: %s", lfxv2.ErrorMessage(err))},
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

	logger.Info("get_membership succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMembershipContacts implements the get_membership_contacts tool logic.
func handleGetMembershipContacts(ctx context.Context, req *mcp.CallToolRequest, args GetMembershipContactsArgs) (*mcp.CallToolResult, any, error) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if membershipConfig == nil {
		logger.Error("membership tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: membership tools not configured"},
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
		APIDomain:           membershipConfig.LFXAPIURL,
		TokenExchangeClient: membershipConfig.TokenExchangeClient,
		DebugLogger:         membershipConfig.DebugLogger,
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

	logger.Info("fetching membership contacts", "uid", args.UID)

	version := "1"
	result, err := clients.Membership.ListMembershipContacts(ctx, &membershipservice.ListMembershipContactsPayload{
		Version: &version,
		UID:     &args.UID,
	})
	if err != nil {
		logger.Error("ListMembershipContacts failed", "error", err, "uid", args.UID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get membership contacts: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Contacts, "", "  ")
	if err != nil {
		logger.Error("failed to marshal membership contacts result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("get_membership_contacts succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}
