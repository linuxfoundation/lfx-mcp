// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	committeeservice "github.com/linuxfoundation/lfx-v2-committee-service/gen/committee_service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Committee write args ---

// CreateCommitteeArgs defines the input parameters for the create_committee tool.
type CreateCommitteeArgs struct {
	ProjectUID            string   `json:"project_uid" jsonschema:"Project UID the committee belongs to"`
	Name                  string   `json:"name" jsonschema:"Name of the committee"`
	Category              string   `json:"category" jsonschema:"Category of the committee"`
	Description           *string  `json:"description,omitempty" jsonschema:"Description of the committee"`
	Website               *string  `json:"website,omitempty" jsonschema:"Website URL of the committee"`
	EnableVoting          bool     `json:"enable_voting,omitempty" jsonschema:"Whether voting is enabled"`
	SSOGroupEnabled       bool     `json:"sso_group_enabled,omitempty" jsonschema:"Whether SSO group integration is enabled"`
	RequiresReview        bool     `json:"requires_review,omitempty" jsonschema:"Whether the committee requires review"`
	Public                bool     `json:"public,omitempty" jsonschema:"Whether the committee is publicly visible"`
	CalendarPublic        *bool    `json:"calendar_public,omitempty" jsonschema:"Whether the committee calendar is publicly visible"`
	DisplayName           *string  `json:"display_name,omitempty" jsonschema:"Display name of the committee"`
	ParentUID             *string  `json:"parent_uid,omitempty" jsonschema:"UID of the parent committee, if any"`
	BusinessEmailRequired bool     `json:"business_email_required,omitempty" jsonschema:"Whether business email is required for members"`
	MemberVisibility      string   `json:"member_visibility,omitempty" jsonschema:"Visibility level of member profiles to other members"`
	ShowMeetingAttendees  bool     `json:"show_meeting_attendees,omitempty" jsonschema:"Whether to show meeting attendees by default"`
	Writers               []string `json:"writers,omitempty" jsonschema:"Manager user IDs who can edit this committee"`
	Auditors              []string `json:"auditors,omitempty" jsonschema:"Auditor user IDs who can audit this committee"`
}

// UpdateCommitteeArgs defines the input parameters for the update_committee tool.
type UpdateCommitteeArgs struct {
	UID             string  `json:"uid" jsonschema:"UID of the committee to update"`
	ProjectUID      string  `json:"project_uid" jsonschema:"Project UID the committee belongs to"`
	Name            string  `json:"name" jsonschema:"Name of the committee"`
	Category        string  `json:"category" jsonschema:"Category of the committee"`
	Description     *string `json:"description,omitempty" jsonschema:"Description of the committee"`
	Website         *string `json:"website,omitempty" jsonschema:"Website URL of the committee"`
	EnableVoting    bool    `json:"enable_voting,omitempty" jsonschema:"Whether voting is enabled"`
	SSOGroupEnabled bool    `json:"sso_group_enabled,omitempty" jsonschema:"Whether SSO group integration is enabled"`
	RequiresReview  bool    `json:"requires_review,omitempty" jsonschema:"Whether the committee requires review"`
	Public          bool    `json:"public,omitempty" jsonschema:"Whether the committee is publicly visible"`
	CalendarPublic  *bool   `json:"calendar_public,omitempty" jsonschema:"Whether the committee calendar is publicly visible"`
	DisplayName     *string `json:"display_name,omitempty" jsonschema:"Display name of the committee"`
	ParentUID       *string `json:"parent_uid,omitempty" jsonschema:"UID of the parent committee, if any"`
}

// UpdateCommitteeSettingsArgs defines the input parameters for the update_committee_settings tool.
type UpdateCommitteeSettingsArgs struct {
	UID                   string   `json:"uid" jsonschema:"UID of the committee whose settings to update"`
	BusinessEmailRequired bool     `json:"business_email_required,omitempty" jsonschema:"Whether business email is required for members"`
	MemberVisibility      string   `json:"member_visibility,omitempty" jsonschema:"Visibility level of member profiles to other members"`
	ShowMeetingAttendees  bool     `json:"show_meeting_attendees,omitempty" jsonschema:"Whether to show meeting attendees by default"`
	Writers               []string `json:"writers,omitempty" jsonschema:"Manager user IDs who can edit this committee"`
	Auditors              []string `json:"auditors,omitempty" jsonschema:"Auditor user IDs who can audit this committee"`
}

// DeleteCommitteeArgs defines the input parameters for the delete_committee tool.
type DeleteCommitteeArgs struct {
	UID string `json:"uid" jsonschema:"UID of the committee to delete"`
}

// --- Committee member write args ---

// CommitteeMemberRoleArgs defines role information for a committee member.
type CommitteeMemberRoleArgs struct {
	Name      string  `json:"name" jsonschema:"Role name"`
	StartDate *string `json:"start_date,omitempty" jsonschema:"Role start date in RFC3339 format"`
	EndDate   *string `json:"end_date,omitempty" jsonschema:"Role end date in RFC3339 format"`
}

// CommitteeMemberVotingArgs defines voting information for a committee member.
type CommitteeMemberVotingArgs struct {
	Status    string  `json:"status" jsonschema:"Voting status"`
	StartDate *string `json:"start_date,omitempty" jsonschema:"Voting start date in RFC3339 format"`
	EndDate   *string `json:"end_date,omitempty" jsonschema:"Voting end date in RFC3339 format"`
}

// CommitteeMemberOrganizationArgs defines organization information for a committee member.
type CommitteeMemberOrganizationArgs struct {
	ID      *string `json:"id,omitempty" jsonschema:"Organization ID"`
	Name    *string `json:"name,omitempty" jsonschema:"Organization name"`
	Website *string `json:"website,omitempty" jsonschema:"Organization website URL"`
}

// CreateCommitteeMemberArgs defines the input parameters for the create_committee_member tool.
type CreateCommitteeMemberArgs struct {
	CommitteeUID    string                           `json:"committee_uid" jsonschema:"UID of the committee to add the member to"`
	Email           string                           `json:"email" jsonschema:"Primary email address of the member"`
	AppointedBy     string                           `json:"appointed_by" jsonschema:"How the member was appointed"`
	Status          string                           `json:"status" jsonschema:"Member status"`
	Username        *string                          `json:"username,omitempty" jsonschema:"LF ID username"`
	FirstName       *string                          `json:"first_name,omitempty" jsonschema:"First name"`
	LastName        *string                          `json:"last_name,omitempty" jsonschema:"Last name"`
	JobTitle        *string                          `json:"job_title,omitempty" jsonschema:"Job title at organization"`
	LinkedinProfile *string                          `json:"linkedin_profile,omitempty" jsonschema:"LinkedIn profile URL"`
	Role            *CommitteeMemberRoleArgs         `json:"role,omitempty" jsonschema:"Committee role information"`
	Voting          *CommitteeMemberVotingArgs       `json:"voting,omitempty" jsonschema:"Voting information"`
	Organization    *CommitteeMemberOrganizationArgs `json:"organization,omitempty" jsonschema:"Organization information"`
}

// UpdateCommitteeMemberArgs defines the input parameters for the update_committee_member tool.
type UpdateCommitteeMemberArgs struct {
	CommitteeUID    string                           `json:"committee_uid" jsonschema:"UID of the committee"`
	MemberUID       string                           `json:"member_uid" jsonschema:"UID of the member to update"`
	Email           string                           `json:"email" jsonschema:"Primary email address of the member"`
	AppointedBy     string                           `json:"appointed_by" jsonschema:"How the member was appointed"`
	Status          string                           `json:"status" jsonschema:"Member status"`
	Username        *string                          `json:"username,omitempty" jsonschema:"LF ID username"`
	FirstName       *string                          `json:"first_name,omitempty" jsonschema:"First name"`
	LastName        *string                          `json:"last_name,omitempty" jsonschema:"Last name"`
	JobTitle        *string                          `json:"job_title,omitempty" jsonschema:"Job title at organization"`
	LinkedinProfile *string                          `json:"linkedin_profile,omitempty" jsonschema:"LinkedIn profile URL"`
	Role            *CommitteeMemberRoleArgs         `json:"role,omitempty" jsonschema:"Committee role information"`
	Voting          *CommitteeMemberVotingArgs       `json:"voting,omitempty" jsonschema:"Voting information"`
	Organization    *CommitteeMemberOrganizationArgs `json:"organization,omitempty" jsonschema:"Organization information"`
}

// DeleteCommitteeMemberArgs defines the input parameters for the delete_committee_member tool.
type DeleteCommitteeMemberArgs struct {
	CommitteeUID string `json:"committee_uid" jsonschema:"UID of the committee"`
	MemberUID    string `json:"member_uid" jsonschema:"UID of the member to delete"`
}

// --- Registration functions ---

// RegisterCreateCommittee registers the create_committee tool with the MCP server.
func RegisterCreateCommittee(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_committee",
		Description: "Create a new committee under a project.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Create Committee",
			DestructiveHint: boolPtr(false),
		},
	}, handleCreateCommittee)
}

// RegisterUpdateCommittee registers the update_committee tool with the MCP server.
func RegisterUpdateCommittee(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_committee",
		Description: "Update a committee's base information (name, category, visibility, etc.).",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Update Committee",
			DestructiveHint: boolPtr(false),
		},
	}, handleUpdateCommittee)
}

// RegisterUpdateCommitteeSettings registers the update_committee_settings tool with the MCP server.
func RegisterUpdateCommitteeSettings(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_committee_settings",
		Description: "Update a committee's settings (member visibility, writers, auditors, etc.).",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Update Committee Settings",
			DestructiveHint: boolPtr(false),
		},
	}, handleUpdateCommitteeSettings)
}

// RegisterDeleteCommittee registers the delete_committee tool with the MCP server.
func RegisterDeleteCommittee(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_committee",
		Description: "Delete a committee by its UID.",
		Annotations: &mcp.ToolAnnotations{
			Title: "Delete Committee",
		},
	}, handleDeleteCommittee)
}

// RegisterCreateCommitteeMember registers the create_committee_member tool with the MCP server.
func RegisterCreateCommitteeMember(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_committee_member",
		Description: "Add a new member to a committee.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Create Committee Member",
			DestructiveHint: boolPtr(false),
		},
	}, handleCreateCommitteeMember)
}

// RegisterUpdateCommitteeMember registers the update_committee_member tool with the MCP server.
func RegisterUpdateCommitteeMember(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_committee_member",
		Description: "Update an existing committee member's information.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Update Committee Member",
			DestructiveHint: boolPtr(false),
		},
	}, handleUpdateCommitteeMember)
}

// RegisterDeleteCommitteeMember registers the delete_committee_member tool with the MCP server.
func RegisterDeleteCommitteeMember(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_committee_member",
		Description: "Remove a member from a committee.",
		Annotations: &mcp.ToolAnnotations{
			Title: "Delete Committee Member",
		},
	}, handleDeleteCommitteeMember)
}

// --- Handler helpers ---

// committeeWriteClients creates LFX v2 clients for committee write operations,
// returning the clients and MCP logger, or an error tool result.
func committeeWriteClients(ctx context.Context, req *mcp.CallToolRequest) (context.Context, *lfxv2.Clients, *slog.Logger, *mcp.CallToolResult) {
	logger := slog.New(mcp.NewLoggingHandler(req.Session, nil))

	if committeeConfig == nil {
		logger.Error("committee tools not configured")
		return ctx, nil, logger, &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: committee tools not configured"},
			},
			IsError: true,
		}
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.Error("failed to extract MCP token", "error", err)
		return ctx, nil, logger, &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to extract MCP token: %v", err)},
			},
			IsError: true,
		}
	}

	ctx = lfxv2.WithMCPToken(ctx, mcpToken)

	clients, err := lfxv2.NewClients(ctx, lfxv2.ClientConfig{
		APIDomain:           committeeConfig.LFXAPIURL,
		TokenExchangeClient: committeeConfig.TokenExchangeClient,
		DebugLogger:         committeeConfig.DebugLogger,
	})
	if err != nil {
		logger.Error("failed to create LFX v2 clients", "error", err)
		return ctx, nil, logger, &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to connect to LFX API: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}
	}

	return ctx, clients, logger, nil
}

// --- Committee handlers ---

// handleCreateCommittee implements the create_committee tool logic.
func handleCreateCommittee(ctx context.Context, req *mcp.CallToolRequest, args CreateCommitteeArgs) (*mcp.CallToolResult, any, error) {
	ctx, clients, logger, errResult := committeeWriteClients(ctx, req)
	if errResult != nil {
		return errResult, nil, nil
	}

	logger.Info("creating committee", "project_uid", args.ProjectUID, "name", args.Name)

	payload := &committeeservice.CreateCommitteePayload{
		Version:               strPtr("1"),
		ProjectUID:            args.ProjectUID,
		Name:                  args.Name,
		Category:              args.Category,
		Description:           args.Description,
		Website:               args.Website,
		EnableVoting:          args.EnableVoting,
		SsoGroupEnabled:       args.SSOGroupEnabled,
		RequiresReview:        args.RequiresReview,
		Public:                args.Public,
		DisplayName:           args.DisplayName,
		ParentUID:             args.ParentUID,
		BusinessEmailRequired: args.BusinessEmailRequired,
		MemberVisibility:      args.MemberVisibility,
		ShowMeetingAttendees:  args.ShowMeetingAttendees,
		Writers:               args.Writers,
		Auditors:              args.Auditors,
	}

	if args.CalendarPublic != nil {
		payload.Calendar = &struct{ Public bool }{Public: *args.CalendarPublic}
	}

	result, err := clients.Committee.CreateCommittee(ctx, payload)
	if err != nil {
		logger.Error("CreateCommittee failed", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to create committee: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		logger.Error("failed to marshal result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("create_committee succeeded", "project_uid", args.ProjectUID, "name", args.Name)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleUpdateCommittee implements the update_committee tool logic.
func handleUpdateCommittee(ctx context.Context, req *mcp.CallToolRequest, args UpdateCommitteeArgs) (*mcp.CallToolResult, any, error) {
	ctx, clients, logger, errResult := committeeWriteClients(ctx, req)
	if errResult != nil {
		return errResult, nil, nil
	}

	if args.UID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("updating committee", "uid", args.UID)

	payload := &committeeservice.UpdateCommitteeBasePayload{
		Version:         strPtr("1"),
		UID:             &args.UID,
		ProjectUID:      args.ProjectUID,
		Name:            args.Name,
		Category:        args.Category,
		Description:     args.Description,
		Website:         args.Website,
		EnableVoting:    args.EnableVoting,
		SsoGroupEnabled: args.SSOGroupEnabled,
		RequiresReview:  args.RequiresReview,
		Public:          args.Public,
		DisplayName:     args.DisplayName,
		ParentUID:       args.ParentUID,
	}

	if args.CalendarPublic != nil {
		payload.Calendar = &struct{ Public bool }{Public: *args.CalendarPublic}
	}

	result, err := clients.Committee.UpdateCommitteeBase(ctx, payload)
	if err != nil {
		logger.Error("UpdateCommitteeBase failed", "error", err, "uid", args.UID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to update committee: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		logger.Error("failed to marshal result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("update_committee succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleUpdateCommitteeSettings implements the update_committee_settings tool logic.
func handleUpdateCommitteeSettings(ctx context.Context, req *mcp.CallToolRequest, args UpdateCommitteeSettingsArgs) (*mcp.CallToolResult, any, error) {
	ctx, clients, logger, errResult := committeeWriteClients(ctx, req)
	if errResult != nil {
		return errResult, nil, nil
	}

	if args.UID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("updating committee settings", "uid", args.UID)

	payload := &committeeservice.UpdateCommitteeSettingsPayload{
		Version:               strPtr("1"),
		UID:                   &args.UID,
		BusinessEmailRequired: args.BusinessEmailRequired,
		MemberVisibility:      args.MemberVisibility,
		ShowMeetingAttendees:  args.ShowMeetingAttendees,
		Writers:               args.Writers,
		Auditors:              args.Auditors,
	}

	result, err := clients.Committee.UpdateCommitteeSettings(ctx, payload)
	if err != nil {
		logger.Error("UpdateCommitteeSettings failed", "error", err, "uid", args.UID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to update committee settings: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		logger.Error("failed to marshal result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("update_committee_settings succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleDeleteCommittee implements the delete_committee tool logic.
func handleDeleteCommittee(ctx context.Context, req *mcp.CallToolRequest, args DeleteCommitteeArgs) (*mcp.CallToolResult, any, error) {
	ctx, clients, logger, errResult := committeeWriteClients(ctx, req)
	if errResult != nil {
		return errResult, nil, nil
	}

	if args.UID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("deleting committee", "uid", args.UID)

	err := clients.Committee.DeleteCommittee(ctx, &committeeservice.DeleteCommitteePayload{
		Version: strPtr("1"),
		UID:     &args.UID,
	})
	if err != nil {
		logger.Error("DeleteCommittee failed", "error", err, "uid", args.UID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to delete committee: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("delete_committee succeeded", "uid", args.UID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Committee %s deleted successfully.", args.UID)},
		},
	}, nil, nil
}

// --- Committee member handlers ---

// buildMemberRole converts CommitteeMemberRoleArgs to the anonymous struct expected by the API payload.
func buildMemberRole(r *CommitteeMemberRoleArgs) *struct {
	Name      string
	StartDate *string
	EndDate   *string
} {
	if r == nil {
		return nil
	}
	return &struct {
		Name      string
		StartDate *string
		EndDate   *string
	}{
		Name:      r.Name,
		StartDate: r.StartDate,
		EndDate:   r.EndDate,
	}
}

// buildMemberVoting converts CommitteeMemberVotingArgs to the anonymous struct expected by the API payload.
func buildMemberVoting(v *CommitteeMemberVotingArgs) *struct {
	Status    string
	StartDate *string
	EndDate   *string
} {
	if v == nil {
		return nil
	}
	return &struct {
		Status    string
		StartDate *string
		EndDate   *string
	}{
		Status:    v.Status,
		StartDate: v.StartDate,
		EndDate:   v.EndDate,
	}
}

// buildMemberOrganization converts CommitteeMemberOrganizationArgs to the anonymous struct expected by the API payload.
func buildMemberOrganization(o *CommitteeMemberOrganizationArgs) *struct {
	ID      *string
	Name    *string
	Website *string
} {
	if o == nil {
		return nil
	}
	return &struct {
		ID      *string
		Name    *string
		Website *string
	}{
		ID:      o.ID,
		Name:    o.Name,
		Website: o.Website,
	}
}

// handleCreateCommitteeMember implements the create_committee_member tool logic.
func handleCreateCommitteeMember(ctx context.Context, req *mcp.CallToolRequest, args CreateCommitteeMemberArgs) (*mcp.CallToolResult, any, error) {
	ctx, clients, logger, errResult := committeeWriteClients(ctx, req)
	if errResult != nil {
		return errResult, nil, nil
	}

	if args.CommitteeUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: committee_uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("creating committee member", "committee_uid", args.CommitteeUID, "email", args.Email)

	payload := &committeeservice.CreateCommitteeMemberPayload{
		Version:         "1",
		UID:             args.CommitteeUID,
		Email:           args.Email,
		AppointedBy:     args.AppointedBy,
		Status:          args.Status,
		Username:        args.Username,
		FirstName:       args.FirstName,
		LastName:        args.LastName,
		JobTitle:        args.JobTitle,
		LinkedinProfile: args.LinkedinProfile,
		Role:            buildMemberRole(args.Role),
		Voting:          buildMemberVoting(args.Voting),
		Organization:    buildMemberOrganization(args.Organization),
	}

	result, err := clients.Committee.CreateCommitteeMember(ctx, payload)
	if err != nil {
		logger.Error("CreateCommitteeMember failed", "error", err, "committee_uid", args.CommitteeUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to create committee member: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		logger.Error("failed to marshal result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("create_committee_member succeeded", "committee_uid", args.CommitteeUID, "email", args.Email)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleUpdateCommitteeMember implements the update_committee_member tool logic.
func handleUpdateCommitteeMember(ctx context.Context, req *mcp.CallToolRequest, args UpdateCommitteeMemberArgs) (*mcp.CallToolResult, any, error) {
	ctx, clients, logger, errResult := committeeWriteClients(ctx, req)
	if errResult != nil {
		return errResult, nil, nil
	}

	if args.CommitteeUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: committee_uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.MemberUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member_uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("updating committee member", "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)

	payload := &committeeservice.UpdateCommitteeMemberPayload{
		Version:         "1",
		UID:             args.CommitteeUID,
		MemberUID:       args.MemberUID,
		Email:           args.Email,
		AppointedBy:     args.AppointedBy,
		Status:          args.Status,
		Username:        args.Username,
		FirstName:       args.FirstName,
		LastName:        args.LastName,
		JobTitle:        args.JobTitle,
		LinkedinProfile: args.LinkedinProfile,
		Role:            buildMemberRole(args.Role),
		Voting:          buildMemberVoting(args.Voting),
		Organization:    buildMemberOrganization(args.Organization),
	}

	result, err := clients.Committee.UpdateCommitteeMember(ctx, payload)
	if err != nil {
		logger.Error("UpdateCommitteeMember failed", "error", err, "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to update committee member: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		logger.Error("failed to marshal result", "error", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to format result: %v", err)},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("update_committee_member succeeded", "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, nil, nil
}

// handleDeleteCommitteeMember implements the delete_committee_member tool logic.
func handleDeleteCommitteeMember(ctx context.Context, req *mcp.CallToolRequest, args DeleteCommitteeMemberArgs) (*mcp.CallToolResult, any, error) {
	ctx, clients, logger, errResult := committeeWriteClients(ctx, req)
	if errResult != nil {
		return errResult, nil, nil
	}

	if args.CommitteeUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: committee_uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	if args.MemberUID == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Error: member_uid is required"},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("deleting committee member", "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)

	err := clients.Committee.DeleteCommitteeMember(ctx, &committeeservice.DeleteCommitteeMemberPayload{
		Version:   "1",
		UID:       args.CommitteeUID,
		MemberUID: args.MemberUID,
	})
	if err != nil {
		logger.Error("DeleteCommitteeMember failed", "error", err, "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error: failed to delete committee member: %s", lfxv2.ErrorMessage(err))},
			},
			IsError: true,
		}, nil, nil
	}

	logger.Info("delete_committee_member succeeded", "committee_uid", args.CommitteeUID, "member_uid", args.MemberUID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Committee member %s deleted successfully from committee %s.", args.MemberUID, args.CommitteeUID)},
		},
	}, nil, nil
}

