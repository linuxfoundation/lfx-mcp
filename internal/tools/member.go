// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	memberservice "github.com/linuxfoundation/lfx-v2-member-service/gen/membership_service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MemberConfig holds configuration shared by member tools.
type MemberConfig struct {
	// Clients is the shared LFX v2 API client instance. It must be created once
	// at startup so that its token cache persists across requests.
	Clients *lfxv2.Clients
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
	ProjectUID    string `json:"project_uid" jsonschema:"Project UUID"`
	MembershipUID string `json:"membership_uid" jsonschema:"The membership UID"`
}

// GetMembershipKeyContactsArgs defines the input parameters for the get_membership_key_contacts tool.
type GetMembershipKeyContactsArgs struct {
	ProjectUID    string `json:"project_uid" jsonschema:"Project UUID"`
	MembershipUID string `json:"membership_uid" jsonschema:"The membership UID"`
}

// ListProjectTiersArgs defines the input parameters for the list_project_tiers tool.
type ListProjectTiersArgs struct {
	ProjectUID string `json:"project_uid" jsonschema:"Project UUID (required)"`
}

// GetProjectTierArgs defines the input parameters for the get_project_tier tool.
type GetProjectTierArgs struct {
	ProjectUID string `json:"project_uid" jsonschema:"Project UUID (required)"`
	TierUID    string `json:"tier_uid" jsonschema:"Membership tier UID"`
}

// GetMembershipKeyContactArgs defines the input parameters for the get_membership_key_contact tool.
type GetMembershipKeyContactArgs struct {
	ProjectUID    string `json:"project_uid" jsonschema:"Project UUID"`
	MembershipUID string `json:"membership_uid" jsonschema:"The membership UID"`
	ContactUID    string `json:"contact_uid" jsonschema:"Key contact UID"`
}

// membershipTierView is a filtered view of MembershipTierResponse for MCP
// responses. Redundant fields that are either required inputs or always
// constant are omitted: project_uid (required input), family (always
// "Membership"), and product_type (always null).
type membershipTierView struct {
	UID       *string `json:"uid,omitempty"`
	Name      *string `json:"name,omitempty"`
	CreatedAt *string `json:"created_at,omitempty"`
	UpdatedAt *string `json:"updated_at,omitempty"`
}

// membershipView is a filtered view of ProjectMembershipResponse for MCP
// responses. Redundant fields omitted: project_uid (required input),
// tier_family (always "Membership"), tier_product_type (always null), and
// membership_type (a raw Salesforce record type ID, not useful to callers).
type membershipView struct {
	UID              *string  `json:"uid,omitempty"`
	TierUID          *string  `json:"tier_uid,omitempty"`
	Status           *string  `json:"status,omitempty"`
	Year             *string  `json:"year,omitempty"`
	Tier             *string  `json:"tier,omitempty"`
	AutoRenew        *bool    `json:"auto_renew,omitempty"`
	RenewalType      *string  `json:"renewal_type,omitempty"`
	Price            *float64 `json:"price,omitempty"`
	AnnualFullPrice  *float64 `json:"annual_full_price,omitempty"`
	PaymentFrequency *string  `json:"payment_frequency,omitempty"`
	PaymentTerms     *string  `json:"payment_terms,omitempty"`
	AgreementDate    *string  `json:"agreement_date,omitempty"`
	PurchaseDate     *string  `json:"purchase_date,omitempty"`
	StartDate        *string  `json:"start_date,omitempty"`
	EndDate          *string  `json:"end_date,omitempty"`
	CompanyName      *string  `json:"company_name,omitempty"`
	CompanyLogoURL   *string  `json:"company_logo_url,omitempty"`
	CompanyDomain    *string  `json:"company_domain,omitempty"`
	TierName         *string  `json:"tier_name,omitempty"`
	CreatedAt        *string  `json:"created_at,omitempty"`
	UpdatedAt        *string  `json:"updated_at,omitempty"`
}

// keyContactView is a filtered view of ProjectKeyContactResponse for MCP
// responses. Redundant fields omitted: tier_uid (a degree removed from the
// contact and not directly useful), project_uid (required input), and
// membership_uid (the id required input).
type keyContactView struct {
	UID            *string `json:"uid,omitempty"`
	Role           *string `json:"role,omitempty"`
	Status         *string `json:"status,omitempty"`
	BoardMember    *bool   `json:"board_member,omitempty"`
	PrimaryContact *bool   `json:"primary_contact,omitempty"`
	FirstName      *string `json:"first_name,omitempty"`
	LastName       *string `json:"last_name,omitempty"`
	Title          *string `json:"title,omitempty"`
	Email          *string `json:"email,omitempty"`
	CompanyName    *string `json:"company_name,omitempty"`
	CompanyLogoURL *string `json:"company_logo_url,omitempty"`
	CompanyDomain  *string `json:"company_domain,omitempty"`
	CreatedAt      *string `json:"created_at,omitempty"`
	UpdatedAt      *string `json:"updated_at,omitempty"`
}

// toMembershipTierView converts a MembershipTierResponse to the filtered MCP
// view, dropping redundant fields.
func toMembershipTierView(t *memberservice.MembershipTierResponse) membershipTierView {
	return membershipTierView{
		UID:       t.UID,
		Name:      t.Name,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// toMembershipView converts a ProjectMembershipResponse to the filtered MCP
// view, dropping redundant fields.
func toMembershipView(m *memberservice.ProjectMembershipResponse) membershipView {
	return membershipView{
		UID:              m.UID,
		TierUID:          m.TierUID,
		Status:           m.Status,
		Year:             m.Year,
		Tier:             m.Tier,
		AutoRenew:        m.AutoRenew,
		RenewalType:      m.RenewalType,
		Price:            m.Price,
		AnnualFullPrice:  m.AnnualFullPrice,
		PaymentFrequency: m.PaymentFrequency,
		PaymentTerms:     m.PaymentTerms,
		AgreementDate:    m.AgreementDate,
		PurchaseDate:     m.PurchaseDate,
		StartDate:        m.StartDate,
		EndDate:          m.EndDate,
		CompanyName:      m.CompanyName,
		CompanyLogoURL:   m.CompanyLogoURL,
		CompanyDomain:    m.CompanyDomain,
		TierName:         m.TierName,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
}

// toKeyContactView converts a ProjectKeyContactResponse to the filtered MCP
// view, dropping redundant fields.
func toKeyContactView(c *memberservice.ProjectKeyContactResponse) keyContactView {
	return keyContactView{
		UID:            c.UID,
		Role:           c.Role,
		Status:         c.Status,
		BoardMember:    c.BoardMember,
		PrimaryContact: c.PrimaryContact,
		FirstName:      c.FirstName,
		LastName:       c.LastName,
		Title:          c.Title,
		Email:          c.Email,
		CompanyName:    c.CompanyName,
		CompanyLogoURL: c.CompanyLogoURL,
		CompanyDomain:  c.CompanyDomain,
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
	}
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
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member tools not configured"},
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

	ctx = memberConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := memberConfig.Clients

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

	logger.InfoContext(ctx, "searching members", "project_uid", args.ProjectUID, "filter_count", len(filters), "page_size", pageSize, "page_token", args.PageToken, "search", args.Search)

	result, err := clients.Member.ListProjectMemberships(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "ListProjectMemberships failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to search members: %s", err.Error())},
			},
			IsError: true,
		}, nil, nil
	}

	views := make([]membershipView, 0, len(result.Memberships))
	for _, m := range result.Memberships {
		views = append(views, toMembershipView(m))
	}

	type searchResult struct {
		Memberships []membershipView            `json:"memberships"`
		Metadata    *memberservice.ListMetadata `json:"metadata,omitempty"`
	}
	filtered := searchResult{
		Memberships: views,
		Metadata:    result.Metadata,
	}

	prettyJSON, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal search result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "search_members succeeded", "count", len(result.Memberships))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMemberMembership implements the get_member_membership tool logic.
func handleGetMemberMembership(ctx context.Context, req *mcp.CallToolRequest, args GetMemberMembershipArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
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

	if args.MembershipUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: membership_uid is required"},
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

	ctx = memberConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := memberConfig.Clients

	logger.InfoContext(ctx, "fetching member membership", "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID)

	version := "1"
	result, err := clients.Member.GetProjectMembership(ctx, &memberservice.GetProjectMembershipPayload{
		Version:       &version,
		ProjectUID:    &args.ProjectUID,
		MembershipUID: &args.MembershipUID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetProjectMembership failed", "error", err, "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get member membership: %s", err.Error())},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Membership, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal membership result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_member_membership succeeded", "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleListProjectTiers implements the list_project_tiers tool logic.
func handleListProjectTiers(ctx context.Context, req *mcp.CallToolRequest, args ListProjectTiersArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
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
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	ctx = memberConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := memberConfig.Clients

	logger.InfoContext(ctx, "listing project tiers", "project_uid", args.ProjectUID)

	version := "1"
	result, err := clients.Member.ListProjectTiers(ctx, &memberservice.ListProjectTiersPayload{
		Version:    &version,
		ProjectUID: &args.ProjectUID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "ListProjectTiers failed", "error", err, "project_uid", args.ProjectUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to list project tiers: %s", err.Error())},
			},
			IsError: true,
		}, nil, nil
	}

	tierViews := make([]membershipTierView, 0, len(result.Tiers))
	for _, t := range result.Tiers {
		tierViews = append(tierViews, toMembershipTierView(t))
	}

	prettyJSON, err := json.MarshalIndent(tierViews, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal tiers result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "list_project_tiers succeeded", "project_uid", args.ProjectUID, "count", len(result.Tiers))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetProjectTier implements the get_project_tier tool logic.
func handleGetProjectTier(ctx context.Context, req *mcp.CallToolRequest, args GetProjectTierArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
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

	if args.TierUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: tier_uid is required"},
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

	ctx = memberConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := memberConfig.Clients

	logger.InfoContext(ctx, "fetching project tier", "project_uid", args.ProjectUID, "tier_uid", args.TierUID)

	version := "1"
	result, err := clients.Member.GetProjectTier(ctx, &memberservice.GetProjectTierPayload{
		Version:    &version,
		ProjectUID: &args.ProjectUID,
		TierUID:    &args.TierUID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetProjectTier failed", "error", err, "project_uid", args.ProjectUID, "tier_uid", args.TierUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get project tier: %s", err.Error())},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Tier, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal tier result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_project_tier succeeded", "project_uid", args.ProjectUID, "tier_uid", args.TierUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMembershipKeyContacts implements the get_membership_key_contacts tool logic.
func handleGetMembershipKeyContacts(ctx context.Context, req *mcp.CallToolRequest, args GetMembershipKeyContactsArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
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

	if args.MembershipUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: membership_uid is required"},
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

	ctx = memberConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := memberConfig.Clients

	logger.InfoContext(ctx, "fetching membership key contacts", "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID)

	version := "1"
	result, err := clients.Member.ListMembershipKeyContacts(ctx, &memberservice.ListMembershipKeyContactsPayload{
		Version:       &version,
		ProjectUID:    &args.ProjectUID,
		MembershipUID: &args.MembershipUID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "ListMembershipKeyContacts failed", "error", err, "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get membership key contacts: %s", err.Error())},
			},
			IsError: true,
		}, nil, nil
	}

	contactViews := make([]keyContactView, 0, len(result.Contacts))
	for _, c := range result.Contacts {
		contactViews = append(contactViews, toKeyContactView(c))
	}

	prettyJSON, err := json.MarshalIndent(contactViews, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal membership key contacts result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_membership_key_contacts succeeded", "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleGetMembershipKeyContact implements the get_membership_key_contact tool logic.
func handleGetMembershipKeyContact(ctx context.Context, req *mcp.CallToolRequest, args GetMembershipKeyContactArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
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

	if args.MembershipUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: membership_uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.ContactUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: contact_uid is required"},
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

	ctx = memberConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := memberConfig.Clients

	logger.InfoContext(ctx, "fetching membership key contact", "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID, "contact_uid", args.ContactUID)

	version := "1"
	result, err := clients.Member.GetMembershipKeyContact(ctx, &memberservice.GetMembershipKeyContactPayload{
		Version:       &version,
		ProjectUID:    &args.ProjectUID,
		MembershipUID: &args.MembershipUID,
		ContactUID:    &args.ContactUID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetMembershipKeyContact failed", "error", err, "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID, "contact_uid", args.ContactUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to get membership key contact: %s", err.Error())},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(toKeyContactView(result.Contact), "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal key contact result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_membership_key_contact succeeded", "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID, "contact_uid", args.ContactUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}
