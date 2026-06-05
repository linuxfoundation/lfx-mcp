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
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// memberResourceType is the resource type filter for project membership queries.
const memberResourceType = "project_membership"

// keyContactResourceType is the resource type filter for key contact queries.
const keyContactResourceType = "key_contact"

// b2bOrgResourceType is the resource type filter for B2B org queries.
const b2bOrgResourceType = "b2b_org"

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
	ProjectUID string `json:"project_uid,omitempty" jsonschema:"Filter by project UUID. At least one of project_uid or b2b_org_uid is strongly recommended."`
	B2bOrgUID  string `json:"b2b_org_uid,omitempty" jsonschema:"Filter by B2B organization UID. At least one of project_uid or b2b_org_uid is strongly recommended. Also replaces the removed list_b2b_org_memberships tool."`
	SearchName string `json:"search_name,omitempty" jsonschema:"Search memberships by member company name (typeahead)."`
	TierUID    string `json:"tier_uid,omitempty" jsonschema:"Filter by membership tier UID."`
	TierName   string `json:"tier_name,omitempty" jsonschema:"Filter by tier label, e.g. 'Gold'."`
	PageSize   int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)."`
	PageToken  string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response."`
}

// GetMemberMembershipArgs defines the input parameters for the get_member_membership tool.
type GetMemberMembershipArgs struct {
	MembershipUID string `json:"membership_uid" jsonschema:"The membership UID"`
}

// GetMembershipKeyContactsArgs defines the input parameters for the get_membership_key_contacts tool.
type GetMembershipKeyContactsArgs struct {
	MembershipUID string `json:"membership_uid" jsonschema:"The membership UID"`
	PageSize      int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)."`
	PageToken     string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous response."`
}

// GetMembershipKeyContactArgs defines the input parameters for the get_membership_key_contact tool.
type GetMembershipKeyContactArgs struct {
	MembershipUID string `json:"membership_uid" jsonschema:"The membership UID"`
	ContactUID    string `json:"contact_uid" jsonschema:"Key contact UID"`
}

// memberSearchResult is the output type for the search_members tool.
type memberSearchResult struct {
	Resources []*querysvc.Resource `json:"resources"`
	PageToken *string              `json:"page_token,omitempty"`
}

// keyContactListResult is the output type for get_membership_key_contacts.
type keyContactListResult struct {
	Resources []*querysvc.Resource `json:"resources"`
	PageToken *string              `json:"page_token,omitempty"`
}

// keyContactView is a filtered view of ProjectKeyContactResponse for MCP
// responses from the member service single-resource endpoint.
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
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_members",
		Description: "List and search project memberships. At least one of project_uid or b2b_org_uid is strongly recommended — an unfiltered search across all memberships is unlikely to be useful. Supports search_name for company name typeahead, tier_uid or tier_name filter, and cursor-based pagination via page_token. Also accepts b2b_org_uid to list all memberships for a given org across all projects (replaces the removed list_b2b_org_memberships tool).",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Members",
			ReadOnlyHint: true,
		},
	}, handleSearchMembers)
}

// RegisterGetMemberMembership registers the get_member_membership tool with the MCP server.
func RegisterGetMemberMembership(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_member_membership",
		Description: "Get a single membership by membership UID. Use this when users ask for details about a specific membership.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Member Membership",
			ReadOnlyHint: true,
		},
	}, handleGetMemberMembership)
}

// RegisterGetMembershipKeyContacts registers the get_membership_key_contacts tool with the MCP server.
func RegisterGetMembershipKeyContacts(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_membership_key_contacts",
		Description: "List key contacts for a membership by membership UID. Returns the people associated with a membership such as primary contacts and board members.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Membership Key Contacts",
			ReadOnlyHint: true,
		},
	}, handleGetMembershipKeyContacts)
}

// RegisterGetMembershipKeyContact registers the get_membership_key_contact tool with the MCP server.
func RegisterGetMembershipKeyContact(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_membership_key_contact",
		Description: "Get a single key contact for a membership by membership UID and key contact UID.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Membership Key Contact",
			ReadOnlyHint: true,
		},
	}, handleGetMembershipKeyContact)
}

// handleSearchMembers implements the search_members tool logic using the Query Service.
func handleSearchMembers(ctx context.Context, req *mcp.CallToolRequest, args SearchMembersArgs) (*mcp.CallToolResult, memberSearchResult, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member tools not configured"},
			},
			IsError: true,
		}, memberSearchResult{}, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, memberSearchResult{}, nil
	}

	ctx = memberConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := memberConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	resourceType := memberResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &resourceType,
		PageSize: pageSize,
	}

	if args.SearchName != "" {
		payload.Name = &args.SearchName
	}

	// Build FiltersAll for data-field equality filters.
	var filtersAll []string
	if args.ProjectUID != "" {
		filtersAll = append(filtersAll, "project_uid:"+args.ProjectUID)
	}
	if args.B2bOrgUID != "" {
		filtersAll = append(filtersAll, "b2b_org_uid:"+args.B2bOrgUID)
	}
	if args.TierUID != "" {
		filtersAll = append(filtersAll, "tier_uid:"+args.TierUID)
	}
	if args.TierName != "" {
		// The indexed field is "tier" (the short label such as "Gold"), not "tier_name".
		filtersAll = append(filtersAll, "tier:"+args.TierName)
	}
	if len(filtersAll) > 0 {
		payload.FiltersAll = filtersAll
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "searching members",
		"project_uid", args.ProjectUID,
		"b2b_org_uid", args.B2bOrgUID,
		"search_name", args.SearchName,
		"filter_count", len(filtersAll),
		"page_size", pageSize,
	)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to search members", err)},
			},
			IsError: true,
		}, memberSearchResult{}, nil
	}

	out := memberSearchResult{
		Resources: result.Resources,
		PageToken: result.PageToken,
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal search result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, memberSearchResult{}, nil
	}

	logger.InfoContext(ctx, "search_members succeeded", "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, out, nil
}

// handleGetMemberMembership implements the get_member_membership tool logic.
func handleGetMemberMembership(ctx context.Context, req *mcp.CallToolRequest, args GetMemberMembershipArgs) (*mcp.CallToolResult, *memberservice.ProjectMembershipResponse, error) {
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

	logger.InfoContext(ctx, "fetching member membership", "membership_uid", args.MembershipUID)

	version := "1"
	result, err := clients.Member.GetProjectMembership(ctx, &memberservice.GetProjectMembershipPayload{
		Version: &version,
		UID:     args.MembershipUID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetProjectMembership failed", "error", err, "membership_uid", args.MembershipUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to get member membership", err)},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.ProjectMembership, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal membership result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "get_member_membership succeeded", "membership_uid", args.MembershipUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, result.ProjectMembership, nil
}

// handleGetMembershipKeyContacts implements the get_membership_key_contacts tool
// logic using the Query Service with a membership_uid filter.
func handleGetMembershipKeyContacts(ctx context.Context, req *mcp.CallToolRequest, args GetMembershipKeyContactsArgs) (*mcp.CallToolResult, keyContactListResult, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member tools not configured"},
			},
			IsError: true,
		}, keyContactListResult{}, nil
	}

	if args.MembershipUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: membership_uid is required"},
			},
			IsError: true,
		}, keyContactListResult{}, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, keyContactListResult{}, nil
	}

	ctx = memberConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := memberConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	resourceType := keyContactResourceType
	payload := &querysvc.QueryResourcesPayload{
		Version:    "1",
		Type:       &resourceType,
		FiltersAll: []string{"membership_uid:" + args.MembershipUID},
		PageSize:   pageSize,
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "fetching membership key contacts", "membership_uid", args.MembershipUID)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources (key_contact) failed", "error", err, "membership_uid", args.MembershipUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to get membership key contacts", err)},
			},
			IsError: true,
		}, keyContactListResult{}, nil
	}

	out := keyContactListResult{
		Resources: result.Resources,
		PageToken: result.PageToken,
	}

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal key contacts result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, keyContactListResult{}, nil
	}

	logger.InfoContext(ctx, "get_membership_key_contacts succeeded", "membership_uid", args.MembershipUID, "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, out, nil
}

// handleGetMembershipKeyContact implements the get_membership_key_contact tool logic.
func handleGetMembershipKeyContact(ctx context.Context, req *mcp.CallToolRequest, args GetMembershipKeyContactArgs) (*mcp.CallToolResult, keyContactView, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member tools not configured"},
			},
			IsError: true,
		}, keyContactView{}, nil
	}

	if args.MembershipUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: membership_uid is required"},
			},
			IsError: true,
		}, keyContactView{}, nil
	}

	if args.ContactUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: contact_uid is required"},
			},
			IsError: true,
		}, keyContactView{}, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}, keyContactView{}, nil
	}

	ctx = memberConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := memberConfig.Clients

	logger.InfoContext(ctx, "fetching membership key contact", "membership_uid", args.MembershipUID, "contact_uid", args.ContactUID)

	version := "1"
	result, err := clients.Member.GetKeyContact(ctx, &memberservice.GetKeyContactPayload{
		Version:       &version,
		MembershipUID: args.MembershipUID,
		UID:           args.ContactUID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetKeyContact failed", "error", err, "membership_uid", args.MembershipUID, "contact_uid", args.ContactUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: friendlyAPIError("failed to get membership key contact", err)},
			},
			IsError: true,
		}, keyContactView{}, nil
	}

	contactView := toKeyContactView(result.KeyContact)

	prettyJSON, err := json.MarshalIndent(contactView, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal key contact result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, keyContactView{}, nil
	}

	logger.InfoContext(ctx, "get_membership_key_contact succeeded", "membership_uid", args.MembershipUID, "contact_uid", args.ContactUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, contactView, nil
}
