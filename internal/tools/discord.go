// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serviceErrorDetail extracts the "detail" field from a FastAPI-style JSON
// error response ({"detail": "..."}). Returns the detail string if present,
// or an empty string otherwise.
func serviceErrorDetail(body []byte) string {
	var resp struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &resp) == nil && resp.Detail != "" {
		return resp.Detail
	}
	return ""
}

// handleServiceResponse checks the HTTP status code from a service API call.
// For 404 it extracts the FastAPI detail message and returns an IsError tool
// result so the LLM can relay it to the user. Other non-200 codes are returned
// as Go errors (which the MCP SDK wraps into IsError automatically).
func handleServiceResponse(body []byte, statusCode int) (*mcp.CallToolResult, error) {
	if statusCode == http.StatusOK {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil
	}
	if statusCode == http.StatusNotFound {
		detail := serviceErrorDetail(body)
		if detail == "" {
			detail = "The requested resource was not found."
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: detail}},
			IsError: true,
		}, nil
	}
	return nil, fmt.Errorf("service returned status %d: %s", statusCode, string(body))
}

// --- Tool registration ---

// RegisterListDiscordRoles registers the list_discord_roles tool.
func RegisterListDiscordRoles(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "list_discord_roles",
		Description: "List all roles in the project's Discord guild. Use this to discover what roles exist before assigning one, or when the user asks about the role structure.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "List Discord Roles",
			DestructiveHint: boolPtr(false),
		},
	}, WriteScopes(), handleListDiscordRoles)
}

// RegisterFindDiscordRole registers the find_discord_role tool.
func RegisterFindDiscordRole(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "find_discord_role",
		Description: "Find a Discord role by name. Use this when you know the role name and need its ID for assignment or checking.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Find Discord Role",
			DestructiveHint: boolPtr(false),
		},
	}, WriteScopes(), handleFindDiscordRole)
}

// RegisterFindDiscordUser registers the find_discord_user tool.
func RegisterFindDiscordUser(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "find_discord_user",
		Description: "Find a Discord guild member by name and optional email. Use this to match a person (e.g. a key contact or committee member) to their Discord account. Returns up to 5 candidates ranked by similarity score — the caller must decide which match is correct.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Find Discord User",
			DestructiveHint: boolPtr(false),
		},
	}, WriteScopes(), handleFindDiscordUser)
}

// RegisterCheckDiscordUserRole registers the check_discord_user_role tool.
func RegisterCheckDiscordUserRole(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "check_discord_user_role",
		Description: "Check whether a Discord user already has a specific role. Call this before assign_discord_role to avoid redundant assignments. Depends on: find_discord_user (for user_id), find_discord_role or list_discord_roles (for role_id).",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Check Discord User Role",
			DestructiveHint: boolPtr(false),
		},
	}, WriteScopes(), handleCheckDiscordUserRole)
}

// RegisterAssignDiscordRole registers the assign_discord_role tool.
func RegisterAssignDiscordRole(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "assign_discord_role",
		Description: "Assign a Discord role to a user. Adding users to private channels means assigning them the corresponding role. Depends on: check_discord_user_role (call first to confirm user does not already have the role), find_discord_user (for user_id), find_discord_role or list_discord_roles (for role_id).",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Assign Discord Role",
			DestructiveHint: boolPtr(false),
		},
	}, WriteScopes(), handleAssignDiscordRole)
}

// --- Tool args ---

// DiscordProjectSlugArgs is the common argument for tools that only need a project slug.
type DiscordProjectSlugArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
}

// DiscordFindRoleArgs defines the input for find_discord_role.
type DiscordFindRoleArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
	RoleName    string `json:"role_name" jsonschema:"Role name to search for"`
}

// DiscordFindUserArgs defines the input for find_discord_user.
type DiscordFindUserArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
	Name        string `json:"name" jsonschema:"Member name to search for"`
	Email       string `json:"email,omitempty" jsonschema:"Email address; local part used as additional search term"`
}

// DiscordCheckUserRoleArgs defines the input for check_discord_user_role.
type DiscordCheckUserRoleArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
	UserID      string `json:"user_id" jsonschema:"Discord user ID"`
	RoleID      string `json:"role_id" jsonschema:"Discord role ID"`
}

// DiscordAssignRoleArgs defines the input for assign_discord_role.
type DiscordAssignRoleArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
	UserID      string `json:"user_id" jsonschema:"Discord user ID"`
	UserName    string `json:"user_name" jsonschema:"Human-readable name (for logging/display)"`
	RoleID      string `json:"role_id" jsonschema:"Discord role ID"`
	RoleName    string `json:"role_name" jsonschema:"Human-readable role name (for logging/display)"`
}

// --- Tool handlers ---

func handleListDiscordRoles(ctx context.Context, req *mcp.CallToolRequest, args DiscordProjectSlugArgs) (*mcp.CallToolResult, any, error) {
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
		return nil, nil, fmt.Errorf("API call failed: %w", err)
	}
	result, svcErr := handleServiceResponse(body, statusCode)
	if svcErr != nil {
		return nil, nil, svcErr
	}
	return result, nil, nil
}

func handleFindDiscordRole(ctx context.Context, req *mcp.CallToolRequest, args DiscordFindRoleArgs) (*mcp.CallToolResult, any, error) {
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
		return nil, nil, fmt.Errorf("API call failed: %w", err)
	}
	result, svcErr := handleServiceResponse(body, statusCode)
	if svcErr != nil {
		return nil, nil, svcErr
	}
	return result, nil, nil
}

func handleFindDiscordUser(ctx context.Context, req *mcp.CallToolRequest, args DiscordFindUserArgs) (*mcp.CallToolResult, any, error) {
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
		return nil, nil, fmt.Errorf("API call failed: %w", err)
	}
	result, svcErr := handleServiceResponse(body, statusCode)
	if svcErr != nil {
		return nil, nil, svcErr
	}
	return result, nil, nil
}

func handleCheckDiscordUserRole(ctx context.Context, req *mcp.CallToolRequest, args DiscordCheckUserRoleArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
	}

	path := fmt.Sprintf("/member-onboarding/tools/discord/%s/users/%s/roles/%s", args.ProjectSlug, url.PathEscape(args.UserID), url.PathEscape(args.RoleID))
	body, statusCode, err := onboardingConfig.ServiceClient.Get(ctx, path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("API call failed: %w", err)
	}
	result, svcErr := handleServiceResponse(body, statusCode)
	if svcErr != nil {
		return nil, nil, svcErr
	}
	return result, nil, nil
}

func handleAssignDiscordRole(ctx context.Context, req *mcp.CallToolRequest, args DiscordAssignRoleArgs) (*mcp.CallToolResult, any, error) {
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
		return nil, nil, fmt.Errorf("API call failed: %w", err)
	}
	result, svcErr := handleServiceResponse(body, statusCode)
	if svcErr != nil {
		return nil, nil, svcErr
	}
	return result, nil, nil
}
