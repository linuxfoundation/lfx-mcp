// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	memberservice "github.com/linuxfoundation/lfx-v2-member-service/gen/membership_service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchB2bOrgsArgs defines the input parameters for the search_b2b_orgs tool.
type SearchB2bOrgsArgs struct {
	SearchName string `json:"search_name,omitempty" jsonschema:"Search B2B organizations by name (case-insensitive substring match)."`
	Sort       string `json:"sort,omitempty" jsonschema:"Sort order: newest (default), name, last_modified"`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 1000)"`
	PageToken  string `json:"page_token,omitempty" jsonschema:"Opaque cursor from a previous response to fetch the next page"`
}

// ListB2bOrgMembershipsArgs defines the input parameters for the list_b2b_org_memberships tool.
type ListB2bOrgMembershipsArgs struct {
	B2bOrgUID string `json:"b2b_org_uid" jsonschema:"B2B organization UID (required)"`
	Sort      string `json:"sort,omitempty" jsonschema:"Sort order: newest (default), name, last_modified"`
	PageSize  int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 1000)"`
	PageToken string `json:"page_token,omitempty" jsonschema:"Opaque cursor from a previous response to fetch the next page"`
}

// b2bOrgView is a filtered view of B2bOrgResponse for MCP responses.
type b2bOrgView struct {
	UID           *string  `json:"uid,omitempty"`
	Name          *string  `json:"name,omitempty"`
	Website       *string  `json:"website,omitempty"`
	PrimaryDomain *string  `json:"primary_domain,omitempty"`
	DomainAliases []string `json:"domain_aliases,omitempty"`
	LogoURL       *string  `json:"logo_url,omitempty"`
	CreatedAt     *string  `json:"created_at,omitempty"`
	UpdatedAt     *string  `json:"updated_at,omitempty"`
}

// b2bOrgMembershipView is a filtered view of ProjectMembershipResponse for
// MCP responses from the list_b2b_org_memberships tool (org drill-down).
// Omitted fields: b2b_org_uid (redundant input), company_name/logo/domain
// (always the same org for every result), tier_family (always "Membership"),
// tier_product_type (always null), and membership_type (raw Salesforce record
// type ID). project_uid and project_slug are included because this is a
// cross-project view and callers need to identify which project each
// membership belongs to.
type b2bOrgMembershipView struct {
	UID              *string  `json:"uid,omitempty"`
	TierUID          *string  `json:"tier_uid,omitempty"`
	ProjectUID       *string  `json:"project_uid,omitempty"`
	ProjectSlug      *string  `json:"project_slug,omitempty"`
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
	TierName         *string  `json:"tier_name,omitempty"`
	CreatedAt        *string  `json:"created_at,omitempty"`
	UpdatedAt        *string  `json:"updated_at,omitempty"`
}

// toB2bOrgMembershipView converts a ProjectMembershipResponse to the filtered
// MCP view for list_b2b_org_memberships, dropping b2b_org_uid (redundant
// input) and company fields (same org for every row in an org-scoped query).
func toB2bOrgMembershipView(m *memberservice.ProjectMembershipResponse) b2bOrgMembershipView {
	return b2bOrgMembershipView{
		UID:              m.UID,
		TierUID:          m.TierUID,
		ProjectUID:       m.ProjectUID,
		ProjectSlug:      m.ProjectSlug,
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
		TierName:         m.TierName,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
}

func toB2bOrgView(o *memberservice.B2bOrgResponse) b2bOrgView {
	return b2bOrgView{
		UID:           o.UID,
		Name:          o.Name,
		Website:       o.Website,
		PrimaryDomain: o.PrimaryDomain,
		DomainAliases: o.DomainAliases,
		LogoURL:       o.LogoURL,
		CreatedAt:     o.CreatedAt,
		UpdatedAt:     o.UpdatedAt,
	}
}

// RegisterSearchB2bOrgs registers the search_b2b_orgs tool with the MCP server.
func RegisterSearchB2bOrgs(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "search_b2b_orgs",
		Description: "Search and list B2B organizations. Use this tool when users ask about B2B orgs, member companies, or organizations across LFX. Supports search_name for case-insensitive name substring search and sort order (newest, name, last_modified). Uses cursor-based pagination via page_token.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search B2B Orgs",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleSearchB2bOrgs)
}

// RegisterListB2bOrgMemberships registers the list_b2b_org_memberships tool with the MCP server.
func RegisterListB2bOrgMemberships(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "list_b2b_org_memberships",
		Description: "List all project memberships for a given B2B organization across all projects. Use this tool when users ask which projects or foundations a B2B org is a member of, or want to see all memberships for a company. Requires b2b_org_uid. Uses cursor-based pagination via page_token.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "List B2B Org Memberships",
			ReadOnlyHint: true,
		},
	}, ReadScopes(), handleListB2bOrgMemberships)
}

// handleSearchB2bOrgs implements the search_b2b_orgs tool logic.
func handleSearchB2bOrgs(ctx context.Context, req *mcp.CallToolRequest, args SearchB2bOrgsArgs) (*mcp.CallToolResult, any, error) {
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

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	payload := &memberservice.ListB2bOrgsPayload{
		PageSize: pageSize,
		Sort:     args.Sort,
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	if args.SearchName != "" {
		payload.SearchName = &args.SearchName
	}

	version := "1"
	payload.Version = &version

	logger.InfoContext(ctx, "searching B2B orgs", "search_name", args.SearchName, "page_size", pageSize, "page_token", args.PageToken)

	result, err := clients.Member.ListB2bOrgs(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "ListB2bOrgs failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to search B2B orgs", err)},
			},
			IsError: true,
		}, nil, nil
	}

	views := make([]b2bOrgView, 0, len(result.Orgs))
	for _, o := range result.Orgs {
		views = append(views, toB2bOrgView(o))
	}

	type searchResult struct {
		Orgs     []b2bOrgView                `json:"orgs"`
		Metadata *memberservice.ListMetadata `json:"metadata,omitempty"`
	}
	output := searchResult{
		Orgs:     views,
		Metadata: result.Metadata,
	}

	prettyJSON, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal search result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "search_b2b_orgs succeeded", "count", len(result.Orgs))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleListB2bOrgMemberships implements the list_b2b_org_memberships tool logic.
func handleListB2bOrgMemberships(ctx context.Context, req *mcp.CallToolRequest, args ListB2bOrgMembershipsArgs) (*mcp.CallToolResult, any, error) {
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

	if args.B2bOrgUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: b2b_org_uid is required"},
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

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	version := "1"
	payload := &memberservice.ListB2bOrgMembershipsPayload{
		Version:   &version,
		B2bOrgUID: args.B2bOrgUID,
		PageSize:  pageSize,
		Sort:      args.Sort,
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "listing B2B org memberships", "b2b_org_uid", args.B2bOrgUID, "page_size", pageSize, "page_token", args.PageToken)

	result, err := clients.Member.ListB2bOrgMemberships(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "ListB2bOrgMemberships failed", "error", err, "b2b_org_uid", args.B2bOrgUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to list B2B org memberships", err)},
			},
			IsError: true,
		}, nil, nil
	}

	views := make([]b2bOrgMembershipView, 0, len(result.Memberships))
	for _, m := range result.Memberships {
		views = append(views, toB2bOrgMembershipView(m))
	}

	// Warn if any membership has a project_slug but no project_uid, which
	// indicates the project is not yet onboarded into LFX Self Service.
	var onboardingWarning string
	if len(result.Memberships) > 0 {
		for _, m := range result.Memberships {
			if m.ProjectSlug != nil && *m.ProjectSlug != "" && (m.ProjectUID == nil || *m.ProjectUID == "") {
				onboardingWarning = "WARNING: projects missing a `project_uid` are not yet onboarded into *LFX Self Service*"
				break
			}
		}
	}

	type listResult struct {
		Memberships []b2bOrgMembershipView      `json:"memberships"`
		Metadata    *memberservice.ListMetadata `json:"metadata,omitempty"`
	}
	output := listResult{
		Memberships: views,
		Metadata:    result.Metadata,
	}

	prettyJSON, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal memberships result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "list_b2b_org_memberships succeeded", "b2b_org_uid", args.B2bOrgUID, "count", len(result.Memberships))

	content := []mcp.Content{}
	if onboardingWarning != "" {
		content = append(content, &mcp.TextContent{Text: onboardingWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})
	return &mcp.CallToolResult{Content: content}, nil, nil
}
