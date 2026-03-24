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

// --- Key contact write args ---

// CreateMembershipKeyContactArgs defines the input parameters for the
// create_membership_key_contact tool.
type CreateMembershipKeyContactArgs struct {
	ProjectUID     string  `json:"project_uid" jsonschema:"Project UUID"`
	MembershipUID  string  `json:"membership_uid" jsonschema:"Membership UID"`
	Email          string  `json:"email" jsonschema:"Contact email address; used to resolve or create the Salesforce Contact record"`
	FirstName      string  `json:"first_name" jsonschema:"Contact first name; used when creating a new Contact on miss"`
	LastName       string  `json:"last_name" jsonschema:"Contact last name; used when creating a new Contact on miss"`
	Title          *string `json:"title,omitempty" jsonschema:"Contact job title; used when creating a new Contact on miss"`
	Role           *string `json:"role,omitempty" jsonschema:"Contact role designation, e.g. 'Voting Representative'"`
	Status         *string `json:"status,omitempty" jsonschema:"Role record status, e.g. 'Active'"`
	BoardMember    *bool   `json:"board_member,omitempty" jsonschema:"Whether this contact holds a board member role"`
	PrimaryContact *bool   `json:"primary_contact,omitempty" jsonschema:"Whether this is the primary contact for the membership"`
}

// UpdateMembershipKeyContactArgs defines the input parameters for the
// update_membership_key_contact tool. Only provided (non-nil) fields are
// updated; omitted fields retain their current values.
type UpdateMembershipKeyContactArgs struct {
	ProjectUID     string  `json:"project_uid" jsonschema:"Project UUID"`
	MembershipUID  string  `json:"membership_uid" jsonschema:"Membership UID"`
	ContactUID     string  `json:"contact_uid" jsonschema:"Key contact UID"`
	Role           *string `json:"role,omitempty" jsonschema:"Contact role designation, e.g. 'Voting Representative'"`
	Status         *string `json:"status,omitempty" jsonschema:"Role record status, e.g. 'Active'"`
	BoardMember    *bool   `json:"board_member,omitempty" jsonschema:"Whether this contact holds a board member role"`
	PrimaryContact *bool   `json:"primary_contact,omitempty" jsonschema:"Whether this is the primary contact for the membership"`
}

// DeleteMembershipKeyContactArgs defines the input parameters for the
// delete_membership_key_contact tool.
type DeleteMembershipKeyContactArgs struct {
	ProjectUID    string `json:"project_uid" jsonschema:"Project UUID"`
	MembershipUID string `json:"membership_uid" jsonschema:"Membership UID"`
	ContactUID    string `json:"contact_uid" jsonschema:"Key contact UID to remove"`
}

// --- Registration ---

// RegisterCreateMembershipKeyContact registers the create_membership_key_contact
// tool with the MCP server.
func RegisterCreateMembershipKeyContact(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "create_membership_key_contact",
		Description: "Add a key contact to a membership. The contact is resolved by email address; if no matching Salesforce Contact exists one will be created using the supplied first name, last name, and title. Requires writer permission on the project.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Create Membership Key Contact",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, WriteScopes(), handleCreateMembershipKeyContact)
}

// RegisterUpdateMembershipKeyContact registers the update_membership_key_contact
// tool with the MCP server.
func RegisterUpdateMembershipKeyContact(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "update_membership_key_contact",
		Description: "Update an existing key contact on a membership. Only the fields that are provided will be changed. Requires writer permission on the project.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Update Membership Key Contact",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  true,
		},
	}, WriteScopes(), handleUpdateMembershipKeyContact)
}

// RegisterDeleteMembershipKeyContact registers the delete_membership_key_contact
// tool with the MCP server.
func RegisterDeleteMembershipKeyContact(server *mcp.Server) {
	AddToolWithScopes(server, &mcp.Tool{
		Name:        "delete_membership_key_contact",
		Description: "Remove a key contact from a membership. This also invalidates the key-contacts cache for the membership. Requires writer permission on the project.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Delete Membership Key Contact",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
		},
	}, WriteScopes(), handleDeleteMembershipKeyContact)
}

// --- Handlers ---

func handleCreateMembershipKeyContact(ctx context.Context, req *mcp.CallToolRequest, args CreateMembershipKeyContactArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: member tools not configured"}},
			IsError: true,
		}, nil, nil
	}

	if args.ProjectUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: project_uid is required"}},
			IsError: true,
		}, nil, nil
	}
	if args.MembershipUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: membership_uid is required"}},
			IsError: true,
		}, nil, nil
	}
	if args.Email == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: email is required"}},
			IsError: true,
		}, nil, nil
	}
	if args.FirstName == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: first_name is required"}},
			IsError: true,
		}, nil, nil
	}
	if args.LastName == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: last_name is required"}},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           memberConfig.LFXAPIURL,
		TokenExchangeClient: memberConfig.TokenExchangeClient,
		DebugLogger:         memberConfig.DebugLogger,
		HTTPClient:          memberConfig.HTTPClient,
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to create LFX v2 clients", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: failed to connect to LFX API: %s", lfxv2.ErrorMessage(err))}},
			IsError: true,
		}, nil, nil
	}

	version := "1"
	payload := &memberservice.CreateMembershipKeyContactPayload{
		Version:        &version,
		ProjectUID:     &args.ProjectUID,
		MembershipUID:  &args.MembershipUID,
		Email:          args.Email,
		FirstName:      args.FirstName,
		LastName:       args.LastName,
		Title:          args.Title,
		Role:           args.Role,
		Status:         args.Status,
		BoardMember:    args.BoardMember,
		PrimaryContact: args.PrimaryContact,
	}

	logger.InfoContext(ctx, "creating membership key contact", "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID, "email", args.Email)

	result, err := clients.Member.CreateMembershipKeyContact(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "CreateMembershipKeyContact failed", "error", err, "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: failed to create key contact: %s", lfxv2.ErrorMessage(err))}},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Contact, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal create result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "create_membership_key_contact succeeded", "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID, "contact_uid", ptrStr(result.Contact.UID))

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(prettyJSON)}},
	}, nil, nil
}

func handleUpdateMembershipKeyContact(ctx context.Context, req *mcp.CallToolRequest, args UpdateMembershipKeyContactArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: member tools not configured"}},
			IsError: true,
		}, nil, nil
	}

	if args.ProjectUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: project_uid is required"}},
			IsError: true,
		}, nil, nil
	}
	if args.MembershipUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: membership_uid is required"}},
			IsError: true,
		}, nil, nil
	}
	if args.ContactUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: contact_uid is required"}},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           memberConfig.LFXAPIURL,
		TokenExchangeClient: memberConfig.TokenExchangeClient,
		DebugLogger:         memberConfig.DebugLogger,
		HTTPClient:          memberConfig.HTTPClient,
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to create LFX v2 clients", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: failed to connect to LFX API: %s", lfxv2.ErrorMessage(err))}},
			IsError: true,
		}, nil, nil
	}

	version := "1"
	result, err := clients.Member.UpdateMembershipKeyContact(ctx, &memberservice.UpdateMembershipKeyContactPayload{
		Version:        &version,
		ProjectUID:     &args.ProjectUID,
		MembershipUID:  &args.MembershipUID,
		ContactUID:     &args.ContactUID,
		Role:           args.Role,
		Status:         args.Status,
		BoardMember:    args.BoardMember,
		PrimaryContact: args.PrimaryContact,
	})
	if err != nil {
		logger.ErrorContext(ctx, "UpdateMembershipKeyContact failed", "error", err, "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID, "contact_uid", args.ContactUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: failed to update key contact: %s", lfxv2.ErrorMessage(err))}},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result.Contact, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal update result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "update_membership_key_contact succeeded", "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID, "contact_uid", args.ContactUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(prettyJSON)}},
	}, nil, nil
}

func handleDeleteMembershipKeyContact(ctx context.Context, req *mcp.CallToolRequest, args DeleteMembershipKeyContactArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if memberConfig == nil {
		logger.ErrorContext(ctx, "member tools not configured")
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: member tools not configured"}},
			IsError: true,
		}, nil, nil
	}

	if args.ProjectUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: project_uid is required"}},
			IsError: true,
		}, nil, nil
	}
	if args.MembershipUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: membership_uid is required"}},
			IsError: true,
		}, nil, nil
	}
	if args.ContactUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: contact_uid is required"}},
			IsError: true,
		}, nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           memberConfig.LFXAPIURL,
		TokenExchangeClient: memberConfig.TokenExchangeClient,
		DebugLogger:         memberConfig.DebugLogger,
		HTTPClient:          memberConfig.HTTPClient,
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to create LFX v2 clients", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: failed to connect to LFX API: %s", lfxv2.ErrorMessage(err))}},
			IsError: true,
		}, nil, nil
	}

	version := "1"
	err = clients.Member.DeleteMembershipKeyContact(ctx, &memberservice.DeleteMembershipKeyContactPayload{
		Version:       &version,
		ProjectUID:    &args.ProjectUID,
		MembershipUID: &args.MembershipUID,
		ContactUID:    &args.ContactUID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "DeleteMembershipKeyContact failed", "error", err, "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID, "contact_uid", args.ContactUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: failed to delete key contact: %s", lfxv2.ErrorMessage(err))}},
			IsError: true,
		}, nil, nil
	}

	logger.InfoContext(ctx, "delete_membership_key_contact succeeded", "project_uid", args.ProjectUID, "membership_uid", args.MembershipUID, "contact_uid", args.ContactUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Key contact %s successfully removed from membership %s.", args.ContactUID, args.MembershipUID)}},
	}, nil, nil
}

// ptrStr safely dereferences a *string for logging; returns "<nil>" if nil.
func ptrStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
