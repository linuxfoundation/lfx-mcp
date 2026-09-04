// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// countableResourceTypes lists the query-service resource types that
// count_lfx_resources accepts, in the order they are shown to the caller.
var countableResourceTypes = []string{
	committeeResourceType,
	committeeMemberResourceType,
	meetingResourceType,
	meetingRegistrantResourceType,
	pastMeetingResourceType,
	pastMeetingParticipantResourceType,
	projectResourceType,
	memberResourceType,
	b2bOrgResourceType,
	mailingListResourceType,
	mailingListMemberResourceType,
}

// callerVisibilityNote is the sentence attached to every count so a bare
// number is never mistaken for an LF-wide total.
const callerVisibilityNote = "Counts only the records visible to your identity; records you cannot see are not counted."

// countLowerBoundNote is appended when the query service stopped counting
// at its access-bucket limit.
const countLowerBoundNote = " The count stopped at the query service's access-bucket limit and is a lower bound; narrow the query (parent, date range, tags) or count per project."

// CountLFXResourcesArgs defines the input parameters for the count_lfx_resources tool.
type CountLFXResourcesArgs struct {
	Type       string   `json:"type" jsonschema:"(required) Resource type to count: committee, committee_member, v1_meeting, v1_meeting_registrant, v1_past_meeting, v1_past_meeting_participant, project, project_membership, b2b_org, groupsio_mailing_list, groupsio_member"`
	Parent     string   `json:"parent,omitempty" jsonschema:"Parent reference, e.g. project:<uid>, committee:<uid>, past_meeting:<meeting_and_occurrence_id>, meeting:<id>"`
	Name       string   `json:"name,omitempty" jsonschema:"Name or alias to match (typeahead)"`
	Tags       []string `json:"tags,omitempty" jsonschema:"Tags matched with OR, e.g. is_attended:true, project_slug:cncf"`
	TagsAll    []string `json:"tags_all,omitempty" jsonschema:"Tags that must all match"`
	DateField  string   `json:"date_field,omitempty" jsonschema:"Data field for the date range, e.g. start_time, updated_at (required with date_from or date_to)"`
	DateFrom   string   `json:"date_from,omitempty" jsonschema:"Inclusive start, ISO 8601 date or datetime (date-only = start of day UTC)"`
	DateTo     string   `json:"date_to,omitempty" jsonschema:"Inclusive end, ISO 8601 date or datetime (date-only = end of day UTC)"`
	Filters    []string `json:"filters,omitempty" jsonschema:"Exact field filters field:value on data fields, matched with OR"`
	FiltersAll []string `json:"filters_all,omitempty" jsonschema:"Exact field filters that must all match"`
}

// countResult is the output shape of count_lfx_resources. The count is never
// returned bare: complete and visibility travel with it.
type countResult struct {
	Count      uint64 `json:"count"`
	Complete   bool   `json:"complete"`
	Visibility string `json:"visibility"`
	Note       string `json:"note"`
}

// RegisterCountLFXResources registers the count_lfx_resources tool with the MCP server.
func RegisterCountLFXResources(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "count_lfx_resources",
		Description: "Count LFX resources of one type via the query service, over the records visible to the caller. " +
			"Accepts the same filters as the search tools: parent (project:<uid>, committee:<uid>, past_meeting:<meeting_and_occurrence_id>), " +
			"tags OR / tags_all AND (is_attended:true, project_slug:cncf), an inclusive date range on a data field (date_field=start_time date_from=2026-01-01 date_to=2026-06-30), " +
			"and exact stored-value filters / filters_all (org_name:<stored value>). " +
			"Returns {count, complete, visibility, note}; complete=false means the count is a lower bound and the query should be narrowed. " +
			"Use this instead of paging a search to count meetings, participants, committees, members or projects.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Count LFX Resources",
			ReadOnlyHint: true,
		},
	}, handleCountLFXResources)
}

// buildCountResult turns a query-service count into the tool's honest shape.
func buildCountResult(count uint64, hasMore bool) countResult {
	out := countResult{
		Count:      count,
		Complete:   !hasMore,
		Visibility: "caller",
		Note:       callerVisibilityNote,
	}
	if hasMore {
		out.Note += countLowerBoundNote
	}
	return out
}

// buildCountPayload maps the tool arguments onto the query-service count
// payload. Validation happens before this is called.
func buildCountPayload(args CountLFXResourcesArgs) *querysvc.QueryResourcesCountPayload {
	resourceType := args.Type
	payload := &querysvc.QueryResourcesCountPayload{
		Version: "1",
		Type:    &resourceType,
	}
	if args.Parent != "" {
		payload.Parent = strPtr(args.Parent)
	}
	if args.Name != "" {
		payload.Name = strPtr(args.Name)
	}
	if len(args.Tags) > 0 {
		payload.Tags = args.Tags
	}
	if len(args.TagsAll) > 0 {
		payload.TagsAll = args.TagsAll
	}
	if args.DateField != "" {
		payload.DateField = strPtr(args.DateField)
	}
	if args.DateFrom != "" {
		payload.DateFrom = strPtr(args.DateFrom)
	}
	if args.DateTo != "" {
		payload.DateTo = strPtr(args.DateTo)
	}
	if len(args.Filters) > 0 {
		payload.Filters = args.Filters
	}
	if len(args.FiltersAll) > 0 {
		payload.FiltersAll = args.FiltersAll
	}
	return payload
}

// validateCountArgs returns a caller-facing error message, or "" when the
// arguments are acceptable.
func validateCountArgs(args CountLFXResourcesArgs) string {
	valid := false
	for _, t := range countableResourceTypes {
		if args.Type == t {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Sprintf("Error: type %q is not countable; valid types: %s", args.Type, strings.Join(countableResourceTypes, ", "))
	}
	if (args.DateFrom != "" || args.DateTo != "") && args.DateField == "" {
		return "Error: date_field is required when date_from or date_to is set (e.g. start_time, updated_at)"
	}
	return ""
}

// handleCountLFXResources implements the count_lfx_resources tool logic.
func handleCountLFXResources(ctx context.Context, req *mcp.CallToolRequest, args CountLFXResourcesArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if projectConfig == nil {
		logger.ErrorContext(ctx, "count tool not configured")
		return errorResult("Error: count tool not configured"), nil, nil
	}

	if msg := validateCountArgs(args); msg != "" {
		return errorResult(msg), nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return errorResult(fmt.Sprintf("Error: failed to extract MCP token: %v", err)), nil, nil
	}

	ctx = projectConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := projectConfig.Clients

	payload := buildCountPayload(args)

	logger.InfoContext(ctx, "counting resources", "type", args.Type, "parent", args.Parent, "date_field", args.DateField)

	result, err := clients.QuerySvc.QueryResourcesCount(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResourcesCount failed", "error", err)
		return errorResult(friendlyAPIError("failed to count resources", err)), nil, nil
	}

	out := buildCountResult(result.Count, result.HasMore)

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal count result", "error", err)
		return errorResult(fmt.Sprintf("Error: failed to format result: %v", err)), nil, nil
	}

	logger.InfoContext(ctx, "count_lfx_resources succeeded", "type", args.Type, "count", result.Count, "complete", out.Complete)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(prettyJSON)}},
	}, nil, nil
}

// errorResult wraps a caller-facing message as an error tool result.
func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}
