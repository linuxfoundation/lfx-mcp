// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/linuxfoundation/lfx-mcp/internal/serviceapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// OnboardingConfig holds configuration shared by member onboarding tools.
type OnboardingConfig struct {
	ServiceAuth
	ServiceClient *serviceapi.Client
}

var onboardingConfig *OnboardingConfig

// SetOnboardingConfig sets the configuration for onboarding tools.
func SetOnboardingConfig(cfg *OnboardingConfig) {
	onboardingConfig = cfg
}

// --- Tool registration ---

// RegisterListMembershipActions registers the list_membership_actions tool.
func RegisterListMembershipActions(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "list_membership_actions",
		Description: "List memberships for a project with per-agent action and todo counts. Use search_projects first to find the project slug.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "List Membership Actions",
			ReadOnlyHint: true,
		},
	}, WriteScopes(), handleListMembershipActions)
}

// --- Tool args ---

// ListMembershipActionsArgs defines the input for list_membership_actions.
type ListMembershipActionsArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug from search_projects (e.g. 'agentic-ai-foundation')"`
	Status      string `json:"status,omitempty" jsonschema:"Filter by status,enum=all,enum=pending,enum=in_progress,enum=closed"`
}

// --- Tool handlers ---

func handleListMembershipActions(ctx context.Context, req *mcp.CallToolRequest, args ListMembershipActionsArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
	}

	// TODO: Proxy to onboarding service API once Auth0 resource server is deployed.
	// The actual call will be:
	//
	//   GET /member-onboarding/{slug}/memberships?status={status}
	//   Authorization: Bearer <m2m_token>
	//
	// path := fmt.Sprintf("/member-onboarding/%s/memberships", args.ProjectSlug)
	// query := url.Values{}
	// if args.Status != "" {
	// 	query.Set("status", args.Status)
	// }
	// body, statusCode, err := onboardingConfig.ServiceClient.Get(ctx, path, query)
	// if err != nil {
	// 	return nil, nil, fmt.Errorf("onboarding API call failed: %w", err)
	// }
	// if statusCode != http.StatusOK {
	// 	return nil, nil, fmt.Errorf("onboarding service returned status %d: %s", statusCode, string(body))
	// }

	_ = ctx // used by actual API call

	status := args.Status
	if status == "" {
		status = "all"
	}
	dummyResponse := map[string]any{
		"project_slug": args.ProjectSlug,
		"status":       status,
		"memberships":  []any{},
		"message":      fmt.Sprintf("[dry-run] Would list membership actions for project %q with status %q", args.ProjectSlug, status),
	}
	body, _ := json.Marshal(dummyResponse)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}
