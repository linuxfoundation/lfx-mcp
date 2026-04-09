// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Tool registration ---

// RegisterOnboardingToolsDiscordGetConfig registers the onboarding_tools_discord_get_config tool.
func RegisterOnboardingToolsDiscordGetConfig(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "onboarding_tools_discord_get_config",
		Description: "Check if Discord is configured for a project. IMPORTANT: Always call this before using any other onboarding_tools_discord_* tool. If configured is false, tell the user they need to set up Discord in the LFX Project Control Center (PCC) first.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Discord Config",
			ReadOnlyHint: true,
		},
	}, WriteScopes(), handleOnboardingToolsDiscordGetConfig)
}

// RegisterOnboardingToolsDiscordListRoles registers the onboarding_tools_discord_list_roles tool.
func RegisterOnboardingToolsDiscordListRoles(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "onboarding_tools_discord_list_roles",
		Description: "List all roles in the project's Discord guild. Use this to discover what roles exist before assigning one, or when the user asks about the role structure. Depends on: onboarding_tools_discord_get_config (call first).",
		Annotations: &mcp.ToolAnnotations{
			Title:        "List Discord Roles",
			ReadOnlyHint: true,
		},
	}, WriteScopes(), handleOnboardingToolsDiscordListRoles)
}

// RegisterOnboardingToolsDiscordFindRole registers the onboarding_tools_discord_find_role tool.
func RegisterOnboardingToolsDiscordFindRole(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "onboarding_tools_discord_find_role",
		Description: "Find a Discord role by name. Use this when you know the role name and need its ID for assignment or checking. Depends on: onboarding_tools_discord_get_config (call first).",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Find Discord Role",
			ReadOnlyHint: true,
		},
	}, WriteScopes(), handleOnboardingToolsDiscordFindRole)
}

// RegisterOnboardingToolsDiscordFindUser registers the onboarding_tools_discord_find_user tool.
func RegisterOnboardingToolsDiscordFindUser(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "onboarding_tools_discord_find_user",
		Description: "Find a Discord guild member by name and optional email. Use this to match a person (e.g. a key contact or committee member) to their Discord account. Returns up to 5 candidates ranked by similarity score — the caller must decide which match is correct. Depends on: onboarding_tools_discord_get_config (call first).",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Find Discord User",
			ReadOnlyHint: true,
		},
	}, WriteScopes(), handleOnboardingToolsDiscordFindUser)
}

// RegisterOnboardingToolsDiscordCheckUserRole registers the onboarding_tools_discord_check_user_role tool.
func RegisterOnboardingToolsDiscordCheckUserRole(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "onboarding_tools_discord_check_user_role",
		Description: "Check whether a Discord user already has a specific role. Call this before assign_role to avoid redundant assignments. Depends on: onboarding_tools_discord_find_user (for user_id), onboarding_tools_discord_find_role or list_roles (for role_id).",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Check Discord User Role",
			ReadOnlyHint: true,
		},
	}, WriteScopes(), handleOnboardingToolsDiscordCheckUserRole)
}

// RegisterOnboardingToolsDiscordAssignRole registers the onboarding_tools_discord_assign_role tool.
func RegisterOnboardingToolsDiscordAssignRole(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "onboarding_tools_discord_assign_role",
		Description: "Assign a Discord role to a user. Adding users to private channels means assigning them the corresponding role. Depends on: onboarding_tools_discord_check_user_role (call first to confirm user does not already have the role), onboarding_tools_discord_find_user (for user_id), onboarding_tools_discord_find_role or list_roles (for role_id).",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Assign Discord Role",
			DestructiveHint: boolPtr(false),
		},
	}, WriteScopes(), handleOnboardingToolsDiscordAssignRole)
}

// --- Tool args ---

// DiscordProjectSlugArgs is the common argument for tools that only need a project slug.
type DiscordProjectSlugArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
}

// DiscordFindRoleArgs defines the input for onboarding_tools_discord_find_role.
type DiscordFindRoleArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
	RoleName    string `json:"role_name" jsonschema:"Role name to search for"`
}

// DiscordFindUserArgs defines the input for onboarding_tools_discord_find_user.
type DiscordFindUserArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
	Name        string `json:"name" jsonschema:"Member name to search for"`
	Email       string `json:"email,omitempty" jsonschema:"Email address; local part used as additional search term"`
}

// DiscordCheckUserRoleArgs defines the input for onboarding_tools_discord_check_user_role.
type DiscordCheckUserRoleArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
	UserID      string `json:"user_id" jsonschema:"Discord user ID"`
	RoleID      string `json:"role_id" jsonschema:"Discord role ID"`
}

// DiscordAssignRoleArgs defines the input for onboarding_tools_discord_assign_role.
type DiscordAssignRoleArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
	UserID      string `json:"user_id" jsonschema:"Discord user ID"`
	UserName    string `json:"user_name" jsonschema:"Human-readable name (for logging/display)"`
	RoleID      string `json:"role_id" jsonschema:"Discord role ID"`
	RoleName    string `json:"role_name" jsonschema:"Human-readable role name (for logging/display)"`
}

// --- Tool handlers ---

func handleOnboardingToolsDiscordGetConfig(ctx context.Context, req *mcp.CallToolRequest, args DiscordProjectSlugArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
	}

	path := fmt.Sprintf("/member-onboarding/tools/discord/%s/config", args.ProjectSlug)
	body, statusCode, err := onboardingConfig.ServiceClient.Get(ctx, path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("onboarding API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("onboarding service returned status %d: %s", statusCode, string(body))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}

func handleOnboardingToolsDiscordListRoles(ctx context.Context, req *mcp.CallToolRequest, args DiscordProjectSlugArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
	}

	path := fmt.Sprintf("/member-onboarding/tools/discord/%s/roles", args.ProjectSlug)
	body, statusCode, err := onboardingConfig.ServiceClient.Get(ctx, path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("onboarding API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("onboarding service returned status %d: %s", statusCode, string(body))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}

func handleOnboardingToolsDiscordFindRole(ctx context.Context, req *mcp.CallToolRequest, args DiscordFindRoleArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
	}

	path := fmt.Sprintf("/member-onboarding/tools/discord/%s/roles/%s", args.ProjectSlug, url.PathEscape(args.RoleName))
	body, statusCode, err := onboardingConfig.ServiceClient.Get(ctx, path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("onboarding API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("onboarding service returned status %d: %s", statusCode, string(body))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}

func handleOnboardingToolsDiscordFindUser(ctx context.Context, req *mcp.CallToolRequest, args DiscordFindUserArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
	}

	path := fmt.Sprintf("/member-onboarding/tools/discord/%s/find-user", args.ProjectSlug)
	reqBody := map[string]string{"name": args.Name}
	if args.Email != "" {
		reqBody["email"] = args.Email
	}
	body, statusCode, err := onboardingConfig.ServiceClient.PostJSON(ctx, path, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("onboarding API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("onboarding service returned status %d: %s", statusCode, string(body))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}

func handleOnboardingToolsDiscordCheckUserRole(ctx context.Context, req *mcp.CallToolRequest, args DiscordCheckUserRoleArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
	}

	path := fmt.Sprintf("/member-onboarding/tools/discord/%s/users/%s/roles/%s", args.ProjectSlug, args.UserID, args.RoleID)
	body, statusCode, err := onboardingConfig.ServiceClient.Get(ctx, path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("onboarding API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("onboarding service returned status %d: %s", statusCode, string(body))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}

func handleOnboardingToolsDiscordAssignRole(ctx context.Context, req *mcp.CallToolRequest, args DiscordAssignRoleArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
	}

	path := fmt.Sprintf("/member-onboarding/tools/discord/%s/assign-role", args.ProjectSlug)
	reqBody := map[string]string{
		"user_id":   args.UserID,
		"user_name": args.UserName,
		"role_id":   args.RoleID,
		"role_name": args.RoleName,
	}
	body, statusCode, err := onboardingConfig.ServiceClient.PostJSON(ctx, path, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("onboarding API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("onboarding service returned status %d: %s", statusCode, string(body))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}
