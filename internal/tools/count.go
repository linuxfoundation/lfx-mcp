// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/auth"
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
const callerVisibilityNote = "Counts only the records indexed in LFX v2 and visible to you; records you cannot see, or not yet onboarded into LFX v2, are not counted."

// countLowerBoundWarning is returned when the query service cannot guarantee
// an exhaustive count.
const countLowerBoundWarning = "The count is not guaranteed exhaustive: the service stopped counting early, so it is a lower bound; narrow the query (parent, date range, tags) or count per project."

const countGroupsIncompleteWarning = "Not every group is guaranteed to be present: more groups may exist than group_by_size, or the count stopped early; raise group_by_size or narrow the query."
const countGroupErrorBoundWarning = "Each group's count may undercount by up to group_count_error_upper_bound within the records the service walked, or by more if the count stopped early."
const countMissingGroupErrorBoundWarning = "The service reported no error bound for the returned group counts."
const countNoMatchingGroupTagWarning = "No matching visible record carried the group_by tag."
const countMissingMetricWarning = "The service reported no distinct count for the requested metric."
const countMetricIncompleteWarning = "The distinct count stopped early and is a lower bound (narrow the query)."

// CountLFXResourcesArgs defines the input parameters for the count_lfx_resources tool.
type CountLFXResourcesArgs struct {
	Type        string   `json:"type" jsonschema:"(required) Resource type to count: committee, committee_member, v1_meeting, v1_meeting_registrant, v1_past_meeting, v1_past_meeting_participant, project, project_membership, b2b_org, groupsio_mailing_list, groupsio_member (the v2 index holds only onboarded projects)"`
	Parent      string   `json:"parent,omitempty" jsonschema:"Parent ref: committee:<uid> for committee_member, project:<uid> for committee, past_meeting:<meeting_and_occurrence_id> for v1_past_meeting_participant"`
	Name        string   `json:"name,omitempty" jsonschema:"Name or alias to match (typeahead)"`
	Tags        []string `json:"tags,omitempty" jsonschema:"Tags matched with OR, e.g. is_attended:true, project_slug:cncf"`
	TagsAll     []string `json:"tags_all,omitempty" jsonschema:"Tags that must all match"`
	DateField   string   `json:"date_field,omitempty" jsonschema:"Data field for the date range, e.g. start_time, updated_at (required with date_from or date_to)"`
	DateFrom    string   `json:"date_from,omitempty" jsonschema:"Inclusive start, ISO 8601 date or datetime (date-only = start of day UTC)"`
	DateTo      string   `json:"date_to,omitempty" jsonschema:"Inclusive end, ISO 8601 date or datetime (date-only = end of day UTC)"`
	FiltersOr   []string `json:"filters_or,omitempty" jsonschema:"Exact field filters field:value on data fields, at least one must match"`
	FiltersAll  []string `json:"filters_all,omitempty" jsonschema:"Exact field filters field:value on data fields that must all match"`
	GroupBy     string   `json:"group_by,omitempty" jsonschema:"Tag prefix; one row per value"`
	GroupBySize int      `json:"group_by_size,omitempty" jsonschema:"Maximum groups (default 100, max 1000); requires group_by"`
	Metric      string   `json:"metric,omitempty" jsonschema:"Distinct tag values: cardinality:<tag_prefix>, e.g. cardinality:email; not with group_by"`
}

// countResult is the output shape of count_lfx_resources. The count is never
// returned bare: complete and visibility travel with it.
type countResult struct {
	Count                     uint64        `json:"count"`
	Complete                  bool          `json:"complete"`
	Visibility                string        `json:"visibility"`
	Note                      string        `json:"note"`
	Warnings                  []string      `json:"warnings,omitempty"`
	Groups                    *[]countGroup `json:"groups,omitempty"`
	GroupsComplete            *bool         `json:"groups_complete,omitempty"`
	GroupCountErrorUpperBound *uint64       `json:"group_count_error_upper_bound,omitempty"`
	MetricValue               *uint64       `json:"metric_value,omitempty"`
	MetricComplete            *bool         `json:"metric_complete,omitempty"`
}

// countGroup is a tag value and its visible record count, in service order.
type countGroup struct {
	Key   string `json:"key"`
	Count uint64 `json:"count"`
}

// RegisterCountLFXResources registers the count_lfx_resources tool with the MCP server.
func RegisterCountLFXResources(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "count_lfx_resources",
		Description: "Count indexed LFX v2 records of one type visible to the caller. " +
			"Filters: parent (the type's own ref), name (typeahead), tags OR / tags_all AND, a date range (date_field=start_time), " +
			"filters_all / filters_or on data fields. " +
			"Returns {count, complete, visibility, note, warnings}. " +
			"group_by=<tag prefix> returns groups [{key, count}] with groups_complete; " +
			"metric=cardinality:<tag prefix> returns metric_value with metric_complete. " +
			"complete=true covers all requested counts; complete=false is partial. Records not yet onboarded into LFX v2 are never counted. " +
			"For how many projects a foundation or parent has, use the semantic layer's project metrics (the authoritative project directory), not this tool.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Count LFX Resources",
			ReadOnlyHint: true,
		},
	}, handleCountLFXResources)
}

// buildCountResult turns a query-service count into the tool's honest shape.
// scopeTruncated reports a caller-side scope cut, not an early stop in the service's count walk.
// The caller appends the scope-specific truncation warning.
func buildCountResult(ctx context.Context, logger *slog.Logger, result *querysvc.QueryResourcesCountResult, resourceType string, groupsRequested, metricRequested, scopeTruncated bool) countResult {
	baseCountComplete := !result.HasMore && !scopeTruncated
	out := countResult{
		Count:      result.Count,
		Complete:   baseCountComplete,
		Visibility: "caller",
		Note:       callerVisibilityNote,
	}
	if result.HasMore {
		out.Warnings = append(out.Warnings, countLowerBoundWarning)
	}
	if groupsRequested {
		// A pointer distinguishes an unrequested field from a requested empty
		// array; a non-nil empty slice encodes as [] rather than null.
		groups := make([]countGroup, 0, len(result.Groups))
		invalidGroups := false
		for _, group := range result.Groups {
			if group == nil {
				invalidGroups = true
				continue
			}
			groups = append(groups, countGroup{Key: group.Key, Count: group.Count})
		}
		out.Groups = &groups
		out.GroupsComplete = result.GroupsComplete
		if invalidGroups {
			out.GroupsComplete = boolPtr(false)
			logger.WarnContext(ctx, "discarded nil groups in query service count response")
		}
		out.GroupCountErrorUpperBound = result.GroupCountErrorUpperBound
		if out.GroupsComplete == nil || !*out.GroupsComplete {
			out.Complete = false
			out.Warnings = append(out.Warnings, countGroupsIncompleteWarning)
		}
		if result.GroupCountErrorUpperBound == nil {
			out.Complete = false
			out.Warnings = append(out.Warnings, countMissingGroupErrorBoundWarning)
		} else if *result.GroupCountErrorUpperBound > 0 {
			out.Complete = false
			out.Warnings = append(out.Warnings, countGroupErrorBoundWarning)
		}
		if len(groups) == 0 && result.Count > 0 && !invalidGroups {
			out.Warnings = append(out.Warnings, countNoMatchingGroupTagWarning)
		}
	}
	if metricRequested {
		out.MetricValue = result.MetricValue
		out.MetricComplete = result.MetricComplete
		if result.MetricValue == nil {
			out.MetricComplete = boolPtr(false)
		}
		if result.MetricValue == nil || result.MetricComplete == nil {
			out.Complete = false
			out.Warnings = append(out.Warnings, countMissingMetricWarning)
		} else if !*out.MetricComplete {
			out.Complete = false
			out.Warnings = append(out.Warnings, countMetricIncompleteWarning)
		}
	}
	if result.Count == 0 && baseCountComplete {
		out.Warnings = append(out.Warnings, searchWarnings(resourceType+" records", 0, 0, false, false)...)
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
	if len(args.FiltersOr) > 0 {
		payload.FiltersOr = args.FiltersOr
	}
	if len(args.FiltersAll) > 0 {
		payload.FiltersAll = args.FiltersAll
	}
	if args.GroupBy != "" {
		payload.GroupBy = strPtr(args.GroupBy)
	}
	if args.GroupBySize != 0 {
		payload.GroupBySize = &args.GroupBySize
	}
	if args.Metric != "" {
		payload.Metric = strPtr(args.Metric)
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

	var tokenInfo *auth.TokenInfo
	if req.Extra != nil {
		tokenInfo = req.Extra.TokenInfo
	}
	ctx, err := projectConfig.Clients.TokenFromRequest(ctx, tokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to resolve LFX authentication", "error", err)
		return errorResult(fmt.Sprintf("Error: failed to extract MCP token: %v", err)), nil, nil
	}

	clients := projectConfig.Clients

	payload := buildCountPayload(args)

	logger.InfoContext(ctx, "counting resources", "type", args.Type, "parent", args.Parent, "date_field", args.DateField)

	result, err := clients.QuerySvc.QueryResourcesCount(ctx, payload)
	if err != nil {
		logger.ErrorContext(ctx, "QueryResourcesCount failed", "error", err)
		return errorResult(friendlyAPIError("failed to count resources", err)), nil, nil
	}

	out := buildCountResult(ctx, logger, result, args.Type, args.GroupBy != "", args.Metric != "", false)

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

// toolError is the error a handler with a typed output returns for a failed
// call. The SDK turns it into an IsError result whose one text block is msg and
// that carries no structured content. Returning an IsError result together with
// a zero output value would instead publish that zero value as structured
// content, which reads as an empty result rather than a failure.
func toolError(msg string) error {
	return errors.New(msg)
}
