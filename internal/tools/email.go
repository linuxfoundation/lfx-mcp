// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Tool registration ---

// RegisterListEmailTemplates registers the list_email_templates tool.
func RegisterListEmailTemplates(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "list_email_templates",
		Description: "List all available email templates for a project. Use this to discover what templates exist before drafting or sending.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "List Email Templates",
			DestructiveHint: boolPtr(false),
		},
	}, WriteScopes(), handleListEmailTemplates)
}

// RegisterSendEmail registers the send_email tool.
func RegisterSendEmail(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name: "send_email",
		Description: `Send a templated email via LFX mail servers. Supports two modes:
- mode=draft: render a preview of the email without sending. Always draft first to confirm content.
- mode=send:  deliver the email to the recipient. Requires to_email and to_name.
Use list_email_templates first to find available template names.`,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Send Email",
			DestructiveHint: boolPtr(true),
		},
	}, WriteScopes(), handleSendEmail)
}

// --- Tool args ---

// EmailProjectSlugArgs is the common argument for email tools that only need a project slug.
type EmailProjectSlugArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
}

// SendEmailArgs defines the input for send_email.
type SendEmailArgs struct {
	ProjectSlug  string            `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
	Mode         string            `json:"mode" jsonschema:"Operation mode,enum=draft,enum=send"`
	TemplateName string            `json:"template_name" jsonschema:"Name of the email template"`
	Variables    map[string]string `json:"variables,omitempty" jsonschema:"Jinja2 template variables (e.g. company_name, project_name)"`
	ToEmail      string            `json:"to_email,omitempty" jsonschema:"Recipient email address (required for mode=send)"`
	ToName       string            `json:"to_name,omitempty" jsonschema:"Recipient display name (required for mode=send)"`
}

// --- Tool handlers ---

func handleListEmailTemplates(ctx context.Context, req *mcp.CallToolRequest, args EmailProjectSlugArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
	}

	path := fmt.Sprintf("/member-onboarding/tools/email/%s/templates", args.ProjectSlug)
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

func handleSendEmail(ctx context.Context, req *mcp.CallToolRequest, args SendEmailArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
	}

	switch args.Mode {
	case "draft":
		path := fmt.Sprintf("/member-onboarding/tools/email/%s/render", args.ProjectSlug)
		reqBody := map[string]any{
			"template_name": args.TemplateName,
		}
		if len(args.Variables) > 0 {
			reqBody["variables"] = args.Variables
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

	case "send":
		if args.ToEmail == "" || args.ToName == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "to_email and to_name are required when mode is send"}},
				IsError: true,
			}, nil, nil
		}
		path := fmt.Sprintf("/member-onboarding/tools/email/%s/send", args.ProjectSlug)
		reqBody := map[string]any{
			"to_email":      args.ToEmail,
			"to_name":       args.ToName,
			"template_name": args.TemplateName,
		}
		if len(args.Variables) > 0 {
			reqBody["variables"] = args.Variables
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

	default:
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "mode must be 'draft' or 'send'"}},
			IsError: true,
		}, nil, nil
	}
}
