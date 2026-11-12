// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	meetingservice "github.com/linuxfoundation/lfx-v2-meeting-service/gen/meeting_service"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Meeting write args ---

// MeetingRecurrenceArgs describes how a meeting repeats. It mirrors the
// meeting service's Recurrence type.
type MeetingRecurrenceArgs struct {
	Type           int     `json:"type" jsonschema:"Recurrence type: 1=daily, 2=weekly, 3=monthly"`
	RepeatInterval *int    `json:"repeat_interval,omitempty" jsonschema:"Repeat every N days, weeks or months (default 1)"`
	WeeklyDays     *string `json:"weekly_days,omitempty" jsonschema:"Weekly recurrence only: comma-separated days of the week, 1=Sunday through 7=Saturday (e.g. '3' for Tuesday, '2,4' for Monday and Wednesday)"`
	MonthlyDay     *int    `json:"monthly_day,omitempty" jsonschema:"Monthly recurrence by date: day of the month (1-31)"`
	MonthlyWeek    *int    `json:"monthly_week,omitempty" jsonschema:"Monthly recurrence by weekday: week of the month (1-4, or -1 for the last week)"`
	MonthlyWeekDay *int    `json:"monthly_week_day,omitempty" jsonschema:"Monthly recurrence by weekday: day of the week, 1=Sunday through 7=Saturday"`
	EndTimes       *int    `json:"end_times,omitempty" jsonschema:"End after this many occurrences (set this or end_date_time)"`
	EndDateTime    *string `json:"end_date_time,omitempty" jsonschema:"End on this date/time, RFC3339 (set this or end_times)"`
}

// MeetingSettingsArgs holds the optional meeting fields shared by
// create_meeting and update_meeting. On update, omitted fields keep their
// current values.
type MeetingSettingsArgs struct {
	Description              *string                `json:"description,omitempty" jsonschema:"Meeting description shown on the meeting page and in calendar invites"`
	Restricted               *bool                  `json:"restricted,omitempty" jsonschema:"When true, only invited registrants can join; when false, anyone with the link can join"`
	MeetingType              *string                `json:"meeting_type,omitempty" jsonschema:"Meeting type,enum=Board,enum=Maintainers,enum=Marketing,enum=Technical,enum=Legal,enum=Other,enum=None"`
	EarlyJoinTimeMinutes     *int                   `json:"early_join_time_minutes,omitempty" jsonschema:"Minutes before the start time that attendees may join"`
	RecordingEnabled         *bool                  `json:"recording_enabled,omitempty" jsonschema:"Whether the meeting is recorded"`
	TranscriptEnabled        *bool                  `json:"transcript_enabled,omitempty" jsonschema:"Whether a transcript is produced"`
	YoutubeUploadEnabled     *bool                  `json:"youtube_upload_enabled,omitempty" jsonschema:"Whether recordings are uploaded to YouTube automatically"`
	AiSummaryEnabled         *bool                  `json:"ai_summary_enabled,omitempty" jsonschema:"Whether a Zoom AI Companion summary is produced"`
	RequireAiSummaryApproval *bool                  `json:"require_ai_summary_approval,omitempty" jsonschema:"Whether the AI summary needs approval before it is shared"`
	ArtifactVisibility       *string                `json:"artifact_visibility,omitempty" jsonschema:"Who can see recordings, transcripts and summaries,enum=meeting_hosts,enum=meeting_participants,enum=public"`
	Recurrence               *MeetingRecurrenceArgs `json:"recurrence,omitempty" jsonschema:"How the meeting repeats; omit for a one-time meeting (on update, omit to keep the current recurrence)"`
	AutoEmailReminderEnabled *bool                  `json:"auto_email_reminder_enabled,omitempty" jsonschema:"Whether registrants get an automatic email reminder"`
	AutoEmailReminderTime    *int                   `json:"auto_email_reminder_time,omitempty" jsonschema:"Minutes before the meeting to send the email reminder"`
	ShowMeetingAttendees     *bool                  `json:"show_meeting_attendees,omitempty" jsonschema:"Whether guests can see each other on the meeting page and in calendar invites"`
}

// CreateMeetingArgs defines the input parameters for the create_meeting tool.
type CreateMeetingArgs struct {
	ProjectUID    string   `json:"project_uid" jsonschema:"UID of the LFX project that owns the meeting (from search_projects)"`
	Title         string   `json:"title" jsonschema:"Meeting title"`
	StartTime     string   `json:"start_time" jsonschema:"Start time in RFC3339 format, e.g. 2026-12-01T16:00:00Z; for a recurring meeting this is the first occurrence"`
	Duration      int      `json:"duration" jsonschema:"Duration in minutes"`
	Timezone      string   `json:"timezone" jsonschema:"IANA timezone the meeting is scheduled in, e.g. America/New_York; invites and recurrence follow this zone"`
	Visibility    string   `json:"visibility" jsonschema:"Whether the meeting is listed publicly,enum=public,enum=private"`
	CommitteeUIDs []string `json:"committee_uids,omitempty" jsonschema:"Committee UIDs whose members are invited to the meeting"`
	MeetingSettingsArgs
}

// CreateMeetingGroupArgs is the groups-mode variant of CreateMeetingArgs.
type CreateMeetingGroupArgs struct {
	ProjectUID string   `json:"project_uid" jsonschema:"UID of the LFX project that owns the meeting (from search_projects)"`
	Title      string   `json:"title" jsonschema:"Meeting title"`
	StartTime  string   `json:"start_time" jsonschema:"Start time in RFC3339 format, e.g. 2026-12-01T16:00:00Z; for a recurring meeting this is the first occurrence"`
	Duration   int      `json:"duration" jsonschema:"Duration in minutes"`
	Timezone   string   `json:"timezone" jsonschema:"IANA timezone the meeting is scheduled in, e.g. America/New_York; invites and recurrence follow this zone"`
	Visibility string   `json:"visibility" jsonschema:"Whether the meeting is listed publicly,enum=public,enum=private"`
	GroupUIDs  []string `json:"group_uids,omitempty" jsonschema:"Group UIDs (also known as committee UIDs) whose members are invited to the meeting"`
	MeetingSettingsArgs
}

// UpdateMeetingArgs defines the input parameters for the update_meeting tool.
// Only provided fields change; omitted fields keep their current values.
type UpdateMeetingArgs struct {
	MeetingID     string   `json:"meeting_id" jsonschema:"ID of the meeting to update (the uid / meeting_id from search_meetings or get_meeting)"`
	Title         *string  `json:"title,omitempty" jsonschema:"New meeting title"`
	StartTime     *string  `json:"start_time,omitempty" jsonschema:"New start time in RFC3339 format"`
	Duration      *int     `json:"duration,omitempty" jsonschema:"New duration in minutes"`
	Timezone      *string  `json:"timezone,omitempty" jsonschema:"New IANA timezone, e.g. Europe/London"`
	Visibility    *string  `json:"visibility,omitempty" jsonschema:"Whether the meeting is listed publicly,enum=public,enum=private"`
	CommitteeUIDs []string `json:"committee_uids,omitempty" jsonschema:"Replace the invited committees with this list"`
	UpdateNote    *string  `json:"update_note,omitempty" jsonschema:"Optional note included in the update email sent to registrants"`
	MeetingSettingsArgs
}

// UpdateMeetingGroupArgs is the groups-mode variant of UpdateMeetingArgs.
type UpdateMeetingGroupArgs struct {
	MeetingID  string   `json:"meeting_id" jsonschema:"ID of the meeting to update (the uid / meeting_id from search_meetings or get_meeting)"`
	Title      *string  `json:"title,omitempty" jsonschema:"New meeting title"`
	StartTime  *string  `json:"start_time,omitempty" jsonschema:"New start time in RFC3339 format"`
	Duration   *int     `json:"duration,omitempty" jsonschema:"New duration in minutes"`
	Timezone   *string  `json:"timezone,omitempty" jsonschema:"New IANA timezone, e.g. Europe/London"`
	Visibility *string  `json:"visibility,omitempty" jsonschema:"Whether the meeting is listed publicly,enum=public,enum=private"`
	GroupUIDs  []string `json:"group_uids,omitempty" jsonschema:"Replace the invited groups (also known as committees) with this list"`
	UpdateNote *string  `json:"update_note,omitempty" jsonschema:"Optional note included in the update email sent to registrants"`
	MeetingSettingsArgs
}

// DeleteMeetingArgs defines the input parameters for the delete_meeting tool.
type DeleteMeetingArgs struct {
	MeetingID string `json:"meeting_id" jsonschema:"ID of the meeting to delete (the uid / meeting_id from search_meetings or get_meeting)"`
}

// --- Registration ---

// RegisterCreateMeeting registers the create_meeting tool with the MCP server.
func RegisterCreateMeeting(server *mcp.Server, asGroups bool) {
	tool := &mcp.Tool{
		Name:        "create_meeting",
		Description: "Schedule a new LFX meeting (committee call, working group session) under a project. Creates the Zoom meeting and, when committee_uids is set, invites those committees' members, who get calendar invites by email. Supports one-time and recurring meetings. Requires writer permission on the project.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Create Meeting",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(true),
		},
	}
	if asGroups {
		tool.Description = "Schedule a new LFX meeting (group call, working group session) under a project. Creates the Zoom meeting and, when group_uids is set, invites those groups' members, who get calendar invites by email. Supports one-time and recurring meetings. Requires writer permission on the project."
		mcp.AddTool(server, tool, handleCreateMeetingGroupMode)
		return
	}
	mcp.AddTool(server, tool, handleCreateMeeting)
}

// RegisterUpdateMeeting registers the update_meeting tool with the MCP server.
func RegisterUpdateMeeting(server *mcp.Server, asGroups bool) {
	tool := &mcp.Tool{
		Name:        "update_meeting",
		Description: "Update an existing LFX meeting. Only the fields provided change; everything else keeps its current value. For a recurring meeting this changes the whole series. Registrants are emailed the update. Requires writer permission on the project.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Update Meeting",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}
	if asGroups {
		mcp.AddTool(server, tool, handleUpdateMeetingGroupMode)
		return
	}
	mcp.AddTool(server, tool, handleUpdateMeeting)
}

// RegisterDeleteMeeting registers the delete_meeting tool with the MCP server.
func RegisterDeleteMeeting(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_meeting",
		Description: "Delete (cancel) an LFX meeting, including every occurrence of a recurring meeting. Registrants are notified that it is cancelled. This cannot be undone. Requires writer permission on the project.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Delete Meeting",
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, handleDeleteMeeting)
}

// --- Handlers ---

func handleCreateMeetingGroupMode(ctx context.Context, req *mcp.CallToolRequest, args CreateMeetingGroupArgs) (*mcp.CallToolResult, any, error) {
	return handleCreateMeeting(ctx, req, CreateMeetingArgs{
		ProjectUID:          args.ProjectUID,
		Title:               args.Title,
		StartTime:           args.StartTime,
		Duration:            args.Duration,
		Timezone:            args.Timezone,
		Visibility:          args.Visibility,
		CommitteeUIDs:       args.GroupUIDs,
		MeetingSettingsArgs: args.MeetingSettingsArgs,
	})
}

func handleCreateMeeting(ctx context.Context, req *mcp.CallToolRequest, args CreateMeetingArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return meetingToolError("Error: meeting tools not configured"), nil, nil
	}

	switch {
	case args.ProjectUID == "":
		return meetingToolError("Error: project_uid is required"), nil, nil
	case strings.TrimSpace(args.Title) == "":
		return meetingToolError("Error: title is required"), nil, nil
	case args.Duration <= 0:
		return meetingToolError("Error: duration must be a positive number of minutes"), nil, nil
	}
	if msg := validateMeetingSchedule(args.StartTime, args.Timezone, args.Visibility); msg != "" {
		return meetingToolError(msg), nil, nil
	}
	if msg := validateMeetingSettings(args.MeetingSettingsArgs); msg != "" {
		return meetingToolError(msg), nil, nil
	}

	ctx, errResult := meetingWriteAuth(ctx, req)
	if errResult != nil {
		return errResult, nil, nil
	}

	s := args.MeetingSettingsArgs
	payload := &meetingservice.CreateItxMeetingPayload{
		Version:                  strPtr("1"),
		XSync:                    boolPtr(true),
		ProjectUID:               args.ProjectUID,
		Title:                    args.Title,
		StartTime:                args.StartTime,
		Duration:                 args.Duration,
		Timezone:                 args.Timezone,
		Visibility:               args.Visibility,
		Description:              s.Description,
		Restricted:               s.Restricted,
		Committees:               committeesFromUIDs(args.CommitteeUIDs),
		MeetingType:              s.MeetingType,
		EarlyJoinTimeMinutes:     s.EarlyJoinTimeMinutes,
		RecordingEnabled:         s.RecordingEnabled,
		TranscriptEnabled:        s.TranscriptEnabled,
		YoutubeUploadEnabled:     s.YoutubeUploadEnabled,
		AiSummaryEnabled:         s.AiSummaryEnabled,
		RequireAiSummaryApproval: s.RequireAiSummaryApproval,
		ArtifactVisibility:       s.ArtifactVisibility,
		Recurrence:               recurrenceFromArgs(s.Recurrence),
		AutoEmailReminderEnabled: s.AutoEmailReminderEnabled,
		AutoEmailReminderTime:    s.AutoEmailReminderTime,
		ShowMeetingAttendees:     s.ShowMeetingAttendees,
	}

	logger.InfoContext(ctx, "creating meeting", "project_uid", args.ProjectUID, "start_time", args.StartTime)

	result, err := meetingConfig.Clients.Meeting.CreateItxMeeting(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "CreateItxMeeting failed", "error", err, "project_uid", args.ProjectUID)
		return meetingToolError(friendlyAPIError("failed to create meeting", err)), nil, nil
	}

	logger.InfoContext(ctx, "create_meeting succeeded", "project_uid", args.ProjectUID, "meeting_id", ptrStr(result.ID))

	return meetingWriteResult(ctx, "Meeting created.", meetingSummaryFromResponse(result))
}

func handleUpdateMeetingGroupMode(ctx context.Context, req *mcp.CallToolRequest, args UpdateMeetingGroupArgs) (*mcp.CallToolResult, any, error) {
	return handleUpdateMeeting(ctx, req, UpdateMeetingArgs{
		MeetingID:           args.MeetingID,
		Title:               args.Title,
		StartTime:           args.StartTime,
		Duration:            args.Duration,
		Timezone:            args.Timezone,
		Visibility:          args.Visibility,
		CommitteeUIDs:       args.GroupUIDs,
		UpdateNote:          args.UpdateNote,
		MeetingSettingsArgs: args.MeetingSettingsArgs,
	})
}

func handleUpdateMeeting(ctx context.Context, req *mcp.CallToolRequest, args UpdateMeetingArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return meetingToolError("Error: meeting tools not configured"), nil, nil
	}
	if args.MeetingID == "" {
		return meetingToolError("Error: meeting_id is required"), nil, nil
	}
	if args.Title != nil && strings.TrimSpace(*args.Title) == "" {
		return meetingToolError("Error: title cannot be empty"), nil, nil
	}
	if args.Duration != nil && *args.Duration <= 0 {
		return meetingToolError("Error: duration must be a positive number of minutes"), nil, nil
	}
	if msg := validateMeetingSettings(args.MeetingSettingsArgs); msg != "" {
		return meetingToolError(msg), nil, nil
	}

	ctx, errResult := meetingWriteAuth(ctx, req)
	if errResult != nil {
		return errResult, nil, nil
	}
	clients := meetingConfig.Clients

	// The meeting service replaces the whole meeting on update, so start from
	// the stored meeting and overlay only the fields the caller supplied.
	current, err := clients.Meeting.GetItxMeeting(ctx, &meetingservice.GetItxMeetingPayload{
		Version:   strPtr("1"),
		MeetingID: args.MeetingID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "GetItxMeeting failed", "error", err, "meeting_id", args.MeetingID)
		return meetingToolError(friendlyAPIError("failed to load meeting to update", err)), nil, nil
	}

	payload := updatePayloadFromCurrent(args.MeetingID, current)
	applyMeetingUpdate(payload, args)

	if msg := validateMeetingSchedule(payload.StartTime, payload.Timezone, payload.Visibility); msg != "" {
		return meetingToolError(msg), nil, nil
	}

	logger.InfoContext(ctx, "updating meeting", "meeting_id", args.MeetingID)

	if err := clients.Meeting.UpdateItxMeeting(ctx, payload); err != nil {
		logger.ErrorContext(ctx, "UpdateItxMeeting failed", "error", err, "meeting_id", args.MeetingID)
		return meetingToolError(friendlyAPIError("failed to update meeting", err)), nil, nil
	}

	logger.InfoContext(ctx, "update_meeting succeeded", "meeting_id", args.MeetingID)

	// Read back the stored meeting so the caller sees what was saved. If the
	// read fails, the update itself still succeeded.
	updated, err := clients.Meeting.GetItxMeeting(ctx, &meetingservice.GetItxMeetingPayload{
		Version:   strPtr("1"),
		MeetingID: args.MeetingID,
	})
	if err != nil {
		logger.WarnContext(ctx, "GetItxMeeting after update failed", "error", err, "meeting_id", args.MeetingID)
		return meetingWriteResult(ctx, "Meeting updated.", meetingSummaryFromUpdatePayload(payload))
	}
	return meetingWriteResult(ctx, "Meeting updated.", meetingSummaryFromResponse(updated))
}

func handleDeleteMeeting(ctx context.Context, req *mcp.CallToolRequest, args DeleteMeetingArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return meetingToolError("Error: meeting tools not configured"), nil, nil
	}
	if args.MeetingID == "" {
		return meetingToolError("Error: meeting_id is required"), nil, nil
	}

	ctx, errResult := meetingWriteAuth(ctx, req)
	if errResult != nil {
		return errResult, nil, nil
	}

	logger.InfoContext(ctx, "deleting meeting", "meeting_id", args.MeetingID)

	err := meetingConfig.Clients.Meeting.DeleteItxMeeting(ctx, &meetingservice.DeleteItxMeetingPayload{
		Version:   strPtr("1"),
		MeetingID: args.MeetingID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "DeleteItxMeeting failed", "error", err, "meeting_id", args.MeetingID)
		return meetingToolError(friendlyAPIError("failed to delete meeting", err)), nil, nil
	}

	logger.InfoContext(ctx, "delete_meeting succeeded", "meeting_id", args.MeetingID)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Meeting %s successfully deleted.", args.MeetingID)}},
	}, nil, nil
}

// --- Helpers ---

// meetingWriteAuth resolves the caller's LFX credentials onto ctx. On
// failure it returns a tool error result for the handler to return.
func meetingWriteAuth(ctx context.Context, req *mcp.CallToolRequest) (context.Context, *mcp.CallToolResult) {
	var tokenInfo *auth.TokenInfo
	if req.Extra != nil {
		tokenInfo = req.Extra.TokenInfo
	}
	ctx, err := meetingConfig.Clients.TokenFromRequest(ctx, tokenInfo)
	if err != nil {
		newToolLogger(ctx, req).ErrorContext(ctx, "failed to resolve LFX authentication", "error", err)
		return ctx, meetingToolError(fmt.Sprintf("Error: failed to extract MCP token: %v", err))
	}
	return ctx, nil
}

func meetingToolError(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}

var (
	meetingTypes         = []string{"Board", "Maintainers", "Marketing", "Technical", "Legal", "Other", "None"}
	artifactVisibilities = []string{"meeting_hosts", "meeting_participants", "public"}
)

// validateMeetingSchedule checks the fields the meeting service requires on
// every create and update.
func validateMeetingSchedule(startTime, timezone, visibility string) string {
	if startTime == "" {
		return "Error: start_time is required"
	}
	if _, err := time.Parse(time.RFC3339, startTime); err != nil {
		return fmt.Sprintf("Error: start_time must be RFC3339, e.g. 2026-12-01T16:00:00Z (got %q)", startTime)
	}
	if timezone == "" {
		return "Error: timezone is required"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Sprintf("Error: timezone must be an IANA zone name such as America/New_York (got %q)", timezone)
	}
	if visibility != "public" && visibility != "private" {
		return fmt.Sprintf("Error: visibility must be public or private (got %q)", visibility)
	}
	return ""
}

// validateMeetingSettings checks the enumerated optional fields so the caller
// gets a clear message instead of an upstream validation error.
func validateMeetingSettings(s MeetingSettingsArgs) string {
	if s.MeetingType != nil && !slices.Contains(meetingTypes, *s.MeetingType) {
		return fmt.Sprintf("Error: meeting_type must be one of %s (got %q)", strings.Join(meetingTypes, ", "), *s.MeetingType)
	}
	if s.ArtifactVisibility != nil && !slices.Contains(artifactVisibilities, *s.ArtifactVisibility) {
		return fmt.Sprintf("Error: artifact_visibility must be one of %s (got %q)", strings.Join(artifactVisibilities, ", "), *s.ArtifactVisibility)
	}
	if r := s.Recurrence; r != nil {
		if r.Type < 1 || r.Type > 3 {
			return fmt.Sprintf("Error: recurrence.type must be 1 (daily), 2 (weekly) or 3 (monthly) (got %d)", r.Type)
		}
		if r.EndDateTime != nil {
			if _, err := time.Parse(time.RFC3339, *r.EndDateTime); err != nil {
				return fmt.Sprintf("Error: recurrence.end_date_time must be RFC3339 (got %q)", *r.EndDateTime)
			}
		}
	}
	return ""
}

func committeesFromUIDs(uids []string) []*meetingservice.Committee {
	if len(uids) == 0 {
		return nil
	}
	out := make([]*meetingservice.Committee, 0, len(uids))
	for _, uid := range uids {
		out = append(out, &meetingservice.Committee{UID: strPtr(uid)})
	}
	return out
}

func recurrenceFromArgs(r *MeetingRecurrenceArgs) *meetingservice.Recurrence {
	if r == nil {
		return nil
	}
	t := r.Type
	return &meetingservice.Recurrence{
		Type:           &t,
		RepeatInterval: r.RepeatInterval,
		WeeklyDays:     r.WeeklyDays,
		MonthlyDay:     r.MonthlyDay,
		MonthlyWeek:    r.MonthlyWeek,
		MonthlyWeekDay: r.MonthlyWeekDay,
		EndTimes:       r.EndTimes,
		EndDateTime:    r.EndDateTime,
	}
}

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

// updatePayloadFromCurrent copies a stored meeting into a full update
// payload. Owner is left nil so the service keeps the stored owner.
func updatePayloadFromCurrent(meetingID string, cur *meetingservice.ITXZoomMeetingResponse) *meetingservice.UpdateItxMeetingPayload {
	return &meetingservice.UpdateItxMeetingPayload{
		Version:                  strPtr("1"),
		XSync:                    boolPtr(true),
		MeetingID:                meetingID,
		ProjectUID:               derefStr(cur.ProjectUID),
		Title:                    derefStr(cur.Title),
		StartTime:                derefStr(cur.StartTime),
		Duration:                 derefInt(cur.Duration),
		Timezone:                 derefStr(cur.Timezone),
		Visibility:               derefStr(cur.Visibility),
		Description:              cur.Description,
		Restricted:               cur.Restricted,
		Committees:               cur.Committees,
		MeetingType:              cur.MeetingType,
		EarlyJoinTimeMinutes:     cur.EarlyJoinTimeMinutes,
		RecordingEnabled:         cur.RecordingEnabled,
		TranscriptEnabled:        cur.TranscriptEnabled,
		YoutubeUploadEnabled:     cur.YoutubeUploadEnabled,
		AiSummaryEnabled:         cur.AiSummaryEnabled,
		RequireAiSummaryApproval: cur.RequireAiSummaryApproval,
		ArtifactVisibility:       cur.ArtifactVisibility,
		Recurrence:               cur.Recurrence,
		AutoEmailReminderEnabled: cur.AutoEmailReminderEnabled,
		AutoEmailReminderTime:    cur.AutoEmailReminderTime,
		ShowMeetingAttendees:     cur.ShowMeetingAttendees,
	}
}

// applyMeetingUpdate overlays the caller-supplied fields onto p.
func applyMeetingUpdate(p *meetingservice.UpdateItxMeetingPayload, a UpdateMeetingArgs) {
	if a.Title != nil {
		p.Title = *a.Title
	}
	if a.StartTime != nil {
		p.StartTime = *a.StartTime
	}
	if a.Duration != nil {
		p.Duration = *a.Duration
	}
	if a.Timezone != nil {
		p.Timezone = *a.Timezone
	}
	if a.Visibility != nil {
		p.Visibility = *a.Visibility
	}
	if a.CommitteeUIDs != nil {
		p.Committees = committeesFromUIDs(a.CommitteeUIDs)
	}
	p.UpdateNote = a.UpdateNote

	s := a.MeetingSettingsArgs
	if s.Description != nil {
		p.Description = s.Description
	}
	if s.Restricted != nil {
		p.Restricted = s.Restricted
	}
	if s.MeetingType != nil {
		p.MeetingType = s.MeetingType
	}
	if s.EarlyJoinTimeMinutes != nil {
		p.EarlyJoinTimeMinutes = s.EarlyJoinTimeMinutes
	}
	if s.RecordingEnabled != nil {
		p.RecordingEnabled = s.RecordingEnabled
	}
	if s.TranscriptEnabled != nil {
		p.TranscriptEnabled = s.TranscriptEnabled
	}
	if s.YoutubeUploadEnabled != nil {
		p.YoutubeUploadEnabled = s.YoutubeUploadEnabled
	}
	if s.AiSummaryEnabled != nil {
		p.AiSummaryEnabled = s.AiSummaryEnabled
	}
	if s.RequireAiSummaryApproval != nil {
		p.RequireAiSummaryApproval = s.RequireAiSummaryApproval
	}
	if s.ArtifactVisibility != nil {
		p.ArtifactVisibility = s.ArtifactVisibility
	}
	if s.Recurrence != nil {
		p.Recurrence = recurrenceFromArgs(s.Recurrence)
	}
	if s.AutoEmailReminderEnabled != nil {
		p.AutoEmailReminderEnabled = s.AutoEmailReminderEnabled
	}
	if s.AutoEmailReminderTime != nil {
		p.AutoEmailReminderTime = s.AutoEmailReminderTime
	}
	if s.ShowMeetingAttendees != nil {
		p.ShowMeetingAttendees = s.ShowMeetingAttendees
	}
}

// meetingWriteSummary is what create_meeting and update_meeting return. It is
// built field by field so that call credentials (passcode, join-page
// password, host details) are never echoed, matching what get_meeting trims.
type meetingWriteSummary struct {
	MeetingID               string                    `json:"meeting_id,omitempty"`
	ProjectUID              string                    `json:"project_uid,omitempty"`
	Title                   string                    `json:"title,omitempty"`
	StartTime               string                    `json:"start_time,omitempty"`
	Duration                int                       `json:"duration,omitempty"`
	Timezone                string                    `json:"timezone,omitempty"`
	Visibility              string                    `json:"visibility,omitempty"`
	Restricted              *bool                     `json:"restricted,omitempty"`
	MeetingType             string                    `json:"meeting_type,omitempty"`
	CommitteeUIDs           []string                  `json:"committee_uids,omitempty"`
	Recurrence              *meetingRecurrenceSummary `json:"recurrence,omitempty"`
	NextOccurrenceStartTime string                    `json:"next_occurrence_start_time,omitempty"`
	RegistrantCount         *int                      `json:"registrant_count,omitempty"`
	PublicLink              string                    `json:"public_link,omitempty"`
	CreatedAt               string                    `json:"created_at,omitempty"`
	ModifiedAt              string                    `json:"modified_at,omitempty"`
}

type meetingRecurrenceSummary struct {
	Type           *int    `json:"type,omitempty"`
	RepeatInterval *int    `json:"repeat_interval,omitempty"`
	WeeklyDays     *string `json:"weekly_days,omitempty"`
	MonthlyDay     *int    `json:"monthly_day,omitempty"`
	MonthlyWeek    *int    `json:"monthly_week,omitempty"`
	MonthlyWeekDay *int    `json:"monthly_week_day,omitempty"`
	EndTimes       *int    `json:"end_times,omitempty"`
	EndDateTime    *string `json:"end_date_time,omitempty"`
}

func recurrenceSummary(r *meetingservice.Recurrence) *meetingRecurrenceSummary {
	if r == nil {
		return nil
	}
	return &meetingRecurrenceSummary{
		Type:           r.Type,
		RepeatInterval: r.RepeatInterval,
		WeeklyDays:     r.WeeklyDays,
		MonthlyDay:     r.MonthlyDay,
		MonthlyWeek:    r.MonthlyWeek,
		MonthlyWeekDay: r.MonthlyWeekDay,
		EndTimes:       r.EndTimes,
		EndDateTime:    r.EndDateTime,
	}
}

func committeeUIDs(cs []*meetingservice.Committee) []string {
	var out []string
	for _, c := range cs {
		if c != nil && c.UID != nil {
			out = append(out, *c.UID)
		}
	}
	return out
}

func meetingSummaryFromResponse(r *meetingservice.ITXZoomMeetingResponse) meetingWriteSummary {
	return meetingWriteSummary{
		MeetingID:               derefStr(r.ID),
		ProjectUID:              derefStr(r.ProjectUID),
		Title:                   derefStr(r.Title),
		StartTime:               derefStr(r.StartTime),
		Duration:                derefInt(r.Duration),
		Timezone:                derefStr(r.Timezone),
		Visibility:              derefStr(r.Visibility),
		Restricted:              r.Restricted,
		MeetingType:             derefStr(r.MeetingType),
		CommitteeUIDs:           committeeUIDs(r.Committees),
		Recurrence:              recurrenceSummary(r.Recurrence),
		NextOccurrenceStartTime: derefStr(r.NextOccurrenceStartTime),
		RegistrantCount:         r.RegistrantCount,
		PublicLink:              derefStr(r.PublicLink),
		CreatedAt:               derefStr(r.CreatedAt),
		ModifiedAt:              derefStr(r.ModifiedAt),
	}
}

func meetingSummaryFromUpdatePayload(p *meetingservice.UpdateItxMeetingPayload) meetingWriteSummary {
	return meetingWriteSummary{
		MeetingID:     p.MeetingID,
		ProjectUID:    p.ProjectUID,
		Title:         p.Title,
		StartTime:     p.StartTime,
		Duration:      p.Duration,
		Timezone:      p.Timezone,
		Visibility:    p.Visibility,
		Restricted:    p.Restricted,
		MeetingType:   derefStr(p.MeetingType),
		CommitteeUIDs: committeeUIDs(p.Committees),
		Recurrence:    recurrenceSummary(p.Recurrence),
	}
}

func meetingWriteResult(_ context.Context, headline string, summary meetingWriteSummary) (*mcp.CallToolResult, any, error) {
	prettyJSON, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return meetingToolError(fmt.Sprintf("Error: failed to format result: %v", err)), nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: headline + "\n" + string(prettyJSON)}},
	}, nil, nil
}
