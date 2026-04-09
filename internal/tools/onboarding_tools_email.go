// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Tool registration ---

// RegisterListOnboardingEmailTemplates registers the list_onboarding_email_templates tool.
func RegisterListOnboardingEmailTemplates(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "list_onboarding_email_templates",
		Description: "List all available email templates for a project. Use this to discover what templates exist before rendering or sending.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "List Email Templates",
			DestructiveHint: boolPtr(false),
		},
	}, WriteScopes(), handleOnboardingToolsEmailListTemplates)
}

// RegisterRenderOnboardingEmailTemplate registers the render_onboarding_email_template tool.
func RegisterRenderOnboardingEmailTemplate(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "render_onboarding_email_template",
		Description: "Render an email template with variables and return a preview. Does not send anything. Use this to preview what an email will look like before sending, or to confirm the template and variables are correct. Depends on: list_onboarding_email_templates (for template_name).",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Render Email Template",
			DestructiveHint: boolPtr(false),
		},
	}, WriteScopes(), handleOnboardingToolsEmailRenderTemplate)
}

// RegisterSendOnboardingEmail registers the send_onboarding_email tool.
func RegisterSendOnboardingEmail(server *mcp.Server) {
	AddServiceTool(server, &mcp.Tool{
		Name:        "send_onboarding_email",
		Description: "Send a templated email to a recipient via AWS SES. Use this to send onboarding or welcome emails to key contacts or members. Always render the template first to confirm content before sending. Depends on: render_onboarding_email_template (call first to preview), list_onboarding_email_templates (for template_name).",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Send Email",
			DestructiveHint: boolPtr(true),
		},
	}, WriteScopes(), handleOnboardingToolsEmailSend)
}

// --- Tool args ---

// EmailProjectSlugArgs is the common argument for email tools that only need a project slug.
type EmailProjectSlugArgs struct {
	ProjectSlug string `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
}

// EmailRenderTemplateArgs defines the input for render_onboarding_email_template.
type EmailRenderTemplateArgs struct {
	ProjectSlug  string            `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
	TemplateName string            `json:"template_name" jsonschema:"Name of the email template"`
	Variables    map[string]string `json:"variables,omitempty" jsonschema:"Jinja2 template variables (e.g. company_name, project_name)"`
}

// EmailSendArgs defines the input for send_onboarding_email.
type EmailSendArgs struct {
	ProjectSlug  string            `json:"project_slug" jsonschema:"Project slug (e.g. 'pytorch')"`
	ToEmail      string            `json:"to_email" jsonschema:"Recipient email address"`
	ToName       string            `json:"to_name" jsonschema:"Recipient display name"`
	TemplateName string            `json:"template_name" jsonschema:"Name of the email template"`
	Variables    map[string]string `json:"variables,omitempty" jsonschema:"Jinja2 template variables (e.g. company_name, project_name)"`
}

// --- Tool handlers ---

func handleOnboardingToolsEmailListTemplates(ctx context.Context, req *mcp.CallToolRequest, args EmailProjectSlugArgs) (*mcp.CallToolResult, any, error) {
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
		return nil, nil, fmt.Errorf("onboarding API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("onboarding service returned status %d: %s", statusCode, string(body))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}

func handleOnboardingToolsEmailRenderTemplate(ctx context.Context, req *mcp.CallToolRequest, args EmailRenderTemplateArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
	}

	path := fmt.Sprintf("/member-onboarding/tools/email/%s/render", args.ProjectSlug)
	reqBody := map[string]any{
		"template_name": args.TemplateName,
	}
	if len(args.Variables) > 0 {
		reqBody["variables"] = args.Variables
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

func handleOnboardingToolsEmailSend(ctx context.Context, req *mcp.CallToolRequest, args EmailSendArgs) (*mcp.CallToolResult, any, error) {
	if onboardingConfig == nil {
		return nil, nil, fmt.Errorf("onboarding tools not configured")
	}

	ctx, err := onboardingConfig.AuthorizeProject(ctx, req, args.ProjectSlug, RelationWriter)
	if err != nil {
		return nil, nil, err
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
		return nil, nil, fmt.Errorf("onboarding API call failed: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("onboarding service returned status %d: %s", statusCode, string(body))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}
