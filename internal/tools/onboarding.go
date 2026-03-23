// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

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

// RegisterOnboardingListMemberships registers the onboarding_list_memberships tool.
func RegisterOnboardingListMemberships(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "onboarding_list_memberships",
		Description: "List memberships for a project with per-agent action and todo counts. Use search_projects first to find the project slug.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "List Onboarding Memberships",
			ReadOnlyHint: true,
		},
	}, handleOnboardingListMemberships)
}

// --- Tool args ---

// OnboardingListMembershipsArgs defines the input for onboarding_list_memberships.
type OnboardingListMembershipsArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug from search_projects (e.g. 'agentic-ai-foundation')"`
	Status      string `json:"status,omitempty" jsonschema:"Filter by status,enum=all,enum=pending,enum=in_progress,enum=closed"`
}

// --- Tool handlers ---

func handleOnboardingListMemberships(ctx context.Context, req *mcp.CallToolRequest, args OnboardingListMembershipsArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return toolError("onboarding tools not configured"), nil, nil
	}

	ctx, errResult := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if errResult != nil {
		return errResult, nil, nil
	}

	path := fmt.Sprintf("/member-onboarding/%s/memberships", args.ProjectSlug)
	query := url.Values{}
	if args.Status != "" {
		query.Set("status", args.Status)
	}

	body, statusCode, err := onboardingConfig.ServiceClient.Get(ctx, path, query)
	if err != nil {
		return toolError("onboarding API call failed: %v", err), nil, nil
	}
	if statusCode != http.StatusOK {
		return toolError("onboarding service returned status %d: %s", statusCode, string(body)), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}
