// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// meetingAttachmentResourceType is the resource type of the files and links
// attached to an upcoming meeting.
const meetingAttachmentResourceType = "v1_meeting_attachment"

// pastMeetingAttachmentResourceType is the resource type of the files and
// links attached to a past meeting occurrence.
const pastMeetingAttachmentResourceType = "v1_past_meeting_attachment"

// The two values of meeting_kind, which selects the record type and the
// meeting id the scope takes.
const (
	meetingKindUpcoming = "upcoming"
	meetingKindPast     = "past"
)

// The two attachment types the index records under the type: tag.
const (
	attachmentTypeFile = "file"
	attachmentTypeLink = "link"
)

// Tool errors of search_meeting_attachments. Each names the accepted form and
// says nothing about any record. The scope is required: LFX Self Serve lists
// attachments per meeting and per project and has no LF-wide list, and an
// unscoped walk over the whole index is too slow to answer.
const (
	attachmentKindRefusal       = "Error: meeting_kind must be upcoming or past."
	attachmentTypeRefusal       = "Error: type must be file or link."
	attachmentScopeRefusal      = "Error: set meeting_id (meeting_kind=upcoming), past_meeting_id (meeting_kind=past) or project_uid."
	attachmentUpcomingIDRefusal = "Error: past_meeting_id is for meeting_kind=past; with meeting_kind=upcoming set meeting_id or project_uid."
	attachmentPastIDRefusal     = "Error: meeting_id is for meeting_kind=upcoming; with meeting_kind=past set past_meeting_id or project_uid."
)

// SearchMeetingAttachmentsArgs defines the input parameters for the
// search_meeting_attachments tool.
type SearchMeetingAttachmentsArgs struct {
	MeetingKind   string `json:"meeting_kind" jsonschema:"(required) upcoming or past"`
	MeetingID     string `json:"meeting_id,omitempty" jsonschema:"Upcoming meeting ID (meeting_kind=upcoming)"`
	PastMeetingID string `json:"past_meeting_id,omitempty" jsonschema:"Past meeting ID, the meeting_and_occurrence_id value, e.g. 91461158520-1771596000000 (meeting_kind=past)"`
	ProjectUID    string `json:"project_uid,omitempty" jsonschema:"Project UID; lists attachments of all meetings of that kind in the project (ignored when meeting_id or past_meeting_id is set)"`
	Type          string `json:"type,omitempty" jsonschema:"file or link"`
	Name          string `json:"name,omitempty" jsonschema:"Name or partial name of the attachment to search for"`
	Sort          string `json:"sort,omitempty" jsonschema:"Sort order: name_asc (default), name_desc, updated_asc, updated_desc"`
	PageSize      int    `json:"page_size,omitempty" jsonschema:"Number of results per page (default 10, max 100)"`
	PageToken     string `json:"page_token,omitempty" jsonschema:"Opaque pagination token from a previous search response"`
}

// RegisterSearchMeetingAttachments registers the search_meeting_attachments
// tool with the MCP server. The tool has one name in both terminology modes:
// nothing in it names a committee.
func RegisterSearchMeetingAttachments(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_meeting_attachments",
		Description: "Search files and links attached to LFX meetings via the query service. meeting_kind is required: upcoming (set meeting_id) or past (set past_meeting_id, the meeting_and_occurrence_id value); or set project_uid to list the attachments of every meeting of that kind in the project. Optional type (file or link) and name. Returns only attachments of meetings you can view, as LFX Self Serve shows them; download links are not included.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Search Meeting Attachments",
			ReadOnlyHint: true,
		},
	}, handleSearchMeetingAttachments)
}

// attachmentDroppedFields is the one place that decides which fields of an
// attachment record no caller gets back: file_url, the storage key of the
// upload, which is not an address a caller can fetch (downloads are
// presigned on request by the meeting service). Every other field of the
// record is returned, as the summary, recording and transcript tools return
// theirs, before the people trim below runs for a caller without full view.
var attachmentDroppedFields = []string{"file_url"}

// dropAttachmentFields removes attachmentDroppedFields from one record.
// Non-map data is left alone.
func dropAttachmentFields(data any) {
	if m, ok := data.(map[string]any); ok {
		dropFields(m, attachmentDroppedFields...)
	}
}

// trimAttachmentPeopleFields reduces an attachment record in place for a
// caller without full view: no LFX Self Serve surface renders who created,
// edited or uploaded an attachment (the documents table shows nobody as its
// sharer), so the three user references go. The link of a link attachment
// stays: it is the attachment itself, rendered to every viewer of the page.
func trimAttachmentPeopleFields(data any) {
	if m, ok := data.(map[string]any); ok {
		dropFields(m, "created_by", "updated_by", "file_uploaded_by")
	}
}

// attachmentQuery is the record type and parent ref a validated argument set
// resolves to.
type attachmentQuery struct {
	resourceType string
	parent       string
}

// resolveAttachmentQuery validates the scope arguments and maps them onto the
// query: meeting_kind picks the record type; a meeting id of that kind, else
// project_uid, is the parent ref. It returns the tool error text, or "" with
// the query, and never calls upstream.
func resolveAttachmentQuery(args SearchMeetingAttachmentsArgs) (attachmentQuery, string) {
	var q attachmentQuery
	switch args.MeetingKind {
	case meetingKindUpcoming:
		if args.PastMeetingID != "" {
			return q, attachmentUpcomingIDRefusal
		}
		q.resourceType = meetingAttachmentResourceType
		if args.MeetingID != "" {
			q.parent = "meeting:" + args.MeetingID
		}
	case meetingKindPast:
		if args.MeetingID != "" {
			return q, attachmentPastIDRefusal
		}
		q.resourceType = pastMeetingAttachmentResourceType
		if args.PastMeetingID != "" {
			q.parent = "past_meeting:" + args.PastMeetingID
		}
	default:
		return q, attachmentKindRefusal
	}
	if args.Type != "" && args.Type != attachmentTypeFile && args.Type != attachmentTypeLink {
		return q, attachmentTypeRefusal
	}
	if q.parent == "" {
		if args.ProjectUID == "" {
			return q, attachmentScopeRefusal
		}
		q.parent = "project:" + args.ProjectUID
	}
	return q, ""
}

// handleSearchMeetingAttachments implements the search_meeting_attachments
// tool logic. The query service admits an attachment on the viewer relation
// of its parent meeting or past meeting, as LFX Self Serve lists them; the
// handler adds no access logic of its own and only removes fields.
func handleSearchMeetingAttachments(ctx context.Context, req *mcp.CallToolRequest, args SearchMeetingAttachmentsArgs) (*mcp.CallToolResult, resourceSearchResult, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return nil, resourceSearchResult{}, toolError("Error: meeting tools not configured")
	}

	query, refusal := resolveAttachmentQuery(args)
	if refusal != "" {
		return nil, resourceSearchResult{}, toolError(refusal)
	}

	var tokenInfo *auth.TokenInfo
	if req.Extra != nil {
		tokenInfo = req.Extra.TokenInfo
	}
	ctx, err := meetingConfig.Clients.TokenFromRequest(ctx, tokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to resolve LFX authentication", "error", err)
		return nil, resourceSearchResult{}, toolError(fmt.Sprintf("Error: failed to extract MCP token: %v", err))
	}

	clients := meetingConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	sort := args.Sort
	if sort == "" {
		sort = "name_asc"
	}

	payload := &querysvc.QueryResourcesPayload{
		Version:  "1",
		Type:     &query.resourceType,
		Parent:   &query.parent,
		PageSize: pageSize,
		Sort:     sort,
	}

	// The type filter must narrow the scope, never widen it: tags_all is
	// ANDed with the parent, where tags would be ORed among themselves.
	if args.Type != "" {
		payload.TagsAll = []string{"type:" + args.Type}
	}

	if args.Name != "" {
		payload.Name = &args.Name
	}

	if args.PageToken != "" {
		payload.PageToken = &args.PageToken
	}

	logger.InfoContext(ctx, "searching meeting attachments",
		"meeting_kind", args.MeetingKind,
		"parent", query.parent,
		"type", args.Type,
		"name", args.Name,
		"page_size", pageSize,
	)

	result, err := clients.QuerySvc.QueryResources(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResources failed", "error", err)
		return nil, resourceSearchResult{}, toolError(friendlyAPIError("failed to search meeting attachments", err))
	}

	trimResourcesData(result.Resources, dropAttachmentFields)
	if !HasFullView(ctx) {
		trimResourcesData(result.Resources, trimAttachmentPeopleFields)
	}

	out := newResourceSearchResult("meeting attachments", result, pageSize, args.PageToken != "")

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal search result", "error", err)
		return nil, resourceSearchResult{}, toolError(fmt.Sprintf("Error: failed to format result: %v", err))
	}

	logger.InfoContext(ctx, "search meeting attachments succeeded", "meeting_kind", args.MeetingKind, "parent", query.parent, "count", len(result.Resources))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(prettyJSON)},
		},
	}, out, nil
}
