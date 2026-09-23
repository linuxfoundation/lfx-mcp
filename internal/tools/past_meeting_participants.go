// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// participantDrainPageSize is the page size used when the tool drains every
// page of a participant or past-meeting query itself.
const participantDrainPageSize = 100

// participantMaxDrainPages caps every page_token loop the tool runs itself,
// so a stale or repeating token can never spin.
const participantMaxDrainPages = 200

// participantMaxRecords caps the participant records collected under a date
// range before dedup; max_meetings bounds meetings, not people.
const participantMaxRecords = 5000

// errDrainPageCap is returned when a page_token loop hits participantMaxDrainPages.
var errDrainPageCap = fmt.Errorf("paging exceeded the %d-page cap; narrow the query", participantMaxDrainPages)

// participantMaxRequests caps the query-service calls one tool invocation may
// make on the date-range path (meeting pages + participant pages together).
// Without it, max_meetings x per-meeting page cap allows 40k calls.
const participantMaxRequests = 2000

// errRequestBudget is returned when a date-range call exhausts participantMaxRequests.
var errRequestBudget = fmt.Errorf("the date range needed more than %d query-service requests; narrow the range, add attended_only or org_name, or use count_only", participantMaxRequests)

// requestBudget counts upstream calls across the steps of one tool call.
type requestBudget struct{ remaining int }

// take consumes one request; it returns errRequestBudget when none are left.
func (b *requestBudget) take() error {
	if b.remaining <= 0 {
		return errRequestBudget
	}
	b.remaining--
	return nil
}

// participantDefaultMaxMeetings caps the past meetings expanded by a date
// range when max_meetings is not given.
const participantDefaultMaxMeetings = 50

// participantHardMaxMeetings is the largest max_meetings accepted.
const participantHardMaxMeetings = 200

// participantPageWarning is the existing access-filtered-page warning.
const participantPageWarning = "WARNING: some results on this page were excluded because you do not have access to them; consider continuing with the next page token, increasing the page size, or narrowing your filters"

// participantEmptyNote is returned with an empty page so silence is never
// mistaken for "no participants": only records the caller can see are
// returned.
const participantEmptyNote = "No past-meeting participants are visible to your identity for these filters; records you cannot see are not returned."

// participantTruncatedNote is added when the date range matched more past
// meetings than max_meetings.
const participantTruncatedNote = "The date range matched more past meetings than max_meetings; only the first %d were expanded. Narrow the range or raise max_meetings (max %d)."

// participantRecordsCapNote reports reaching the cap without claiming that
// any unvisited meetings or pages contain additional matching records.
const participantRecordsCapNote = "The record cap (%d) was reached before every matching past meeting was checked; participant records may have been omitted (truncated_records=true). Narrow the range, add attended_only or org_name, or use count_only."

// participantPerPageNote explains dedup scope on a paged call.
const participantPerPageNote = "people and records describe this page only; a person whose records straddle pages can appear on more than one page."

// participantCountRecordsNote distinguishes counted records from people.
const participantCountRecordsNote = " This counts participant records, not distinct people; use count_only=false for de-duplicated people."

// participantSearchResult is the output shape of search_past_meeting_participants.
type participantSearchResult struct {
	Resources []*querysvc.Resource `json:"resources"`
	PageToken *string              `json:"page_token,omitempty"`
	People    *int                 `json:"people,omitempty"`
	Records   *int                 `json:"records,omitempty"`
	Meetings  *int                 `json:"meetings,omitempty"` // past meetings actually expanded

	TruncatedMeetings bool   `json:"truncated_meetings,omitempty"`
	TruncatedRecords  bool   `json:"truncated_records,omitempty"`
	Note              string `json:"note,omitempty"`
}

// participantScope is the parent reference chosen from the three scope args.
func participantScope(args SearchPastMeetingParticipantsArgs) (parent string, ok bool) {
	switch {
	case args.PastMeetingID != "":
		return "past_meeting:" + args.PastMeetingID, true
	case args.CommitteeUID != "":
		return "committee:" + args.CommitteeUID, true
	case args.ProjectUID != "":
		return "project:" + args.ProjectUID, true
	}
	return "", false
}

// participantFilters returns the tag and filter clauses shared by the search
// and count payloads.
func participantFilters(args SearchPastMeetingParticipantsArgs) (tags, filtersAll []string) {
	if args.AttendedOnly {
		tags = append(tags, "is_attended:true")
	}
	if args.OrgName != "" {
		// data is a flat_object: exact, case-sensitive match on the stored value.
		filtersAll = append(filtersAll, "org_name:"+args.OrgName)
	}
	return tags, filtersAll
}

// resolvePastMeetingIDs runs step 1 of a date-ranged participant search: the
// v1_past_meeting search on the same parent and start_time range, drained up
// to maxMeetings. truncated reports that more meetings remained.
func resolvePastMeetingIDs(ctx context.Context, clients *lfxv2.Clients, parent string, args SearchPastMeetingParticipantsArgs, maxMeetings int, budget *requestBudget) (ids []string, truncated bool, err error) {
	resourceType := pastMeetingResourceType
	dateField := "start_time"
	var pageToken *string
	for pages := 0; ; pages++ {
		if pages >= participantMaxDrainPages {
			return nil, false, errDrainPageCap
		}
		if err := budget.take(); err != nil {
			return nil, false, err
		}
		payload := &querysvc.QueryResourcesPayload{
			Version:   "1",
			Type:      &resourceType,
			DateField: &dateField,
			PageSize:  participantDrainPageSize,
			Sort:      "name_asc",
			PageToken: pageToken,
		}
		if parent != "" {
			payload.Parent = strPtr(parent)
		}
		if args.DateFrom != "" {
			payload.DateFrom = strPtr(args.DateFrom)
		}
		if args.DateTo != "" {
			payload.DateTo = strPtr(args.DateTo)
		}
		result, err := clients.QuerySvc.QueryResources(ctx, payload)
		if err != nil {
			return nil, false, err
		}
		for _, r := range result.Resources {
			id := pastMeetingOccurrenceID(r)
			if id == "" {
				continue
			}
			if len(ids) >= maxMeetings {
				return ids, true, nil
			}
			ids = append(ids, id)
		}
		if result.PageToken == nil || *result.PageToken == "" {
			return ids, false, nil
		}
		if len(ids) >= maxMeetings {
			return ids, true, nil
		}
		pageToken = result.PageToken
	}
}

// pastMeetingOccurrenceID extracts meeting_and_occurrence_id from a
// v1_past_meeting resource, falling back to the resource id.
func pastMeetingOccurrenceID(r *querysvc.Resource) string {
	if data, ok := r.Data.(map[string]any); ok {
		if v, ok := data["meeting_and_occurrence_id"].(string); ok && v != "" {
			return v
		}
	}
	if r.ID != nil {
		return *r.ID
	}
	return ""
}

// drainParticipants fetches pages of participants for one parent until the
// pages run out or recordBudget records have been collected. capped reports
// that records were left behind (more than the budget, or a token remained).
func drainParticipants(ctx context.Context, clients *lfxv2.Clients, parent string, args SearchPastMeetingParticipantsArgs, sort string, recordBudget int, budget *requestBudget) (out []*querysvc.Resource, capped bool, err error) {
	resourceType := pastMeetingParticipantResourceType
	tags, filtersAll := participantFilters(args)
	var pageToken *string
	for pages := 0; ; pages++ {
		if pages >= participantMaxDrainPages {
			return nil, false, errDrainPageCap
		}
		if err := budget.take(); err != nil {
			return nil, false, err
		}
		payload := &querysvc.QueryResourcesPayload{
			Version:    "1",
			Type:       &resourceType,
			Parent:     strPtr(parent),
			Tags:       tags,
			FiltersAll: filtersAll,
			PageSize:   participantDrainPageSize,
			Sort:       sort,
			PageToken:  pageToken,
		}
		if args.Name != "" {
			payload.Name = strPtr(args.Name)
		}
		result, err := clients.QuerySvc.QueryResources(ctx, payload)
		if err != nil {
			return nil, false, err
		}
		out = append(out, result.Resources...)
		if len(out) >= recordBudget {
			return out[:recordBudget], len(out) > recordBudget || (result.PageToken != nil && *result.PageToken != ""), nil
		}
		if result.PageToken == nil || *result.PageToken == "" {
			return out, false, nil
		}
		pageToken = result.PageToken
	}
}

// countParticipants runs the count route for one parent with the shared filters.
func countParticipants(ctx context.Context, clients *lfxv2.Clients, parent string, args SearchPastMeetingParticipantsArgs) (*querysvc.QueryResourcesCountResult, error) {
	resourceType := pastMeetingParticipantResourceType
	tags, filtersAll := participantFilters(args)
	payload := &querysvc.QueryResourcesCountPayload{
		Version:    "1",
		Type:       &resourceType,
		Tags:       tags,
		FiltersAll: filtersAll,
	}
	if parent != "" {
		payload.Parent = strPtr(parent)
	}
	if args.Name != "" {
		payload.Name = strPtr(args.Name)
	}
	return clients.QuerySvc.QueryResourcesCount(ctx, payload)
}

// handleSearchPastMeetingParticipants implements the search_past_meeting_participants tool logic.
func handleSearchPastMeetingParticipants(ctx context.Context, req *mcp.CallToolRequest, args SearchPastMeetingParticipantsArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if meetingConfig == nil {
		logger.ErrorContext(ctx, "meeting tools not configured")
		return errorResult("Error: meeting tools not configured"), nil, nil
	}

	hasDateRange := args.DateFrom != "" || args.DateTo != ""
	if hasDateRange && args.PastMeetingID != "" {
		return errorResult("Error: date_from/date_to cannot be combined with past_meeting_id; a past meeting already has one start time"), nil, nil
	}
	if hasDateRange && args.CommitteeUID == "" && args.ProjectUID == "" {
		return errorResult("Error: date_from/date_to require project_uid or committee_uid; without a scope the range would cover only the first past meetings visible to you across all of LFX"), nil, nil
	}
	if hasDateRange && args.PageToken != "" {
		return errorResult("Error: page_token cannot be used with a date range; the tool drains every matching meeting itself"), nil, nil
	}
	maxMeetings := args.MaxMeetings
	if maxMeetings <= 0 {
		maxMeetings = participantDefaultMaxMeetings
	}
	if hasDateRange && maxMeetings > participantHardMaxMeetings {
		return errorResult(fmt.Sprintf("Error: max_meetings must be at most %d", participantHardMaxMeetings)), nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return errorResult(fmt.Sprintf("Error: failed to extract MCP token: %v", err)), nil, nil
	}

	ctx = meetingConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := meetingConfig.Clients

	pageSize := args.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	sort := args.Sort
	if sort == "" {
		sort = "name_asc"
	}
	dedupe := args.Dedupe == nil || *args.Dedupe

	parent, _ := participantScope(args)

	logger.InfoContext(ctx, "searching past meeting participants",
		"past_meeting_id", args.PastMeetingID,
		"committee_uid", args.CommitteeUID,
		"project_uid", args.ProjectUID,
		"name", args.Name,
		"date_from", args.DateFrom,
		"date_to", args.DateTo,
		"attended_only", args.AttendedOnly,
		"org_name", args.OrgName,
		"count_only", args.CountOnly,
		"dedupe", dedupe,
		"page_size", pageSize,
	)

	// Step 1 (date range only): resolve the past meetings in range.
	var parents []string
	truncated := false
	budget := &requestBudget{remaining: participantMaxRequests}
	if hasDateRange {
		ids, trunc, err := resolvePastMeetingIDs(ctx, clients, parent, args, maxMeetings, budget)
		if err != nil {
			logger.ErrorContext(ctx, "past meeting resolution failed", "error", err)
			return errorResult(friendlyAPIError("failed to resolve past meetings for the date range", err)), nil, nil
		}
		truncated = trunc
		for _, id := range ids {
			parents = append(parents, "past_meeting:"+id)
		}
	}

	// count_only: sum the count route over the scope(s).
	if args.CountOnly {
		var total uint64
		complete := !truncated
		if hasDateRange {
			for _, p := range parents {
				res, err := countParticipants(ctx, clients, p, args)
				if err != nil {
					logger.ErrorContext(ctx, "QueryResourcesCount failed", "error", err)
					return errorResult(friendlyAPIError("failed to count past meeting participants", err)), nil, nil
				}
				total += res.Count
				if res.HasMore {
					complete = false
				}
			}
		} else {
			res, err := countParticipants(ctx, clients, parent, args)
			if err != nil {
				logger.ErrorContext(ctx, "QueryResourcesCount failed", "error", err)
				return errorResult(friendlyAPIError("failed to count past meeting participants", err)), nil, nil
			}
			total = res.Count
			complete = !res.HasMore
		}
		out := buildCountResult(total, !complete)
		out.Note += participantCountRecordsNote
		if truncated {
			out.Note += " " + fmt.Sprintf(participantTruncatedNote, maxMeetings, participantHardMaxMeetings)
		}
		return jsonResult(ctx, logger, "search_past_meeting_participants count succeeded", out)
	}

	out := participantSearchResult{}
	var pageWarning string

	if hasDateRange {
		var all []*querysvc.Resource
		drained := 0
		for i, p := range parents {
			rs, capped, err := drainParticipants(ctx, clients, p, args, sort, participantMaxRecords-len(all), budget)
			if err != nil {
				logger.ErrorContext(ctx, "QueryResources failed", "error", err)
				return errorResult(friendlyAPIError("failed to search past meeting participants", err)), nil, nil
			}
			all = append(all, rs...)
			drained = i + 1
			// Records were left behind only if this meeting had more, or
			// later meetings were never queried.
			if capped || (len(all) >= participantMaxRecords && i < len(parents)-1) {
				out.TruncatedRecords = true
				break
			}
			if len(all) >= participantMaxRecords {
				break
			}
		}
		// meetings = past meetings actually expanded (equals the resolved
		// count unless the record cap stopped the drain early).
		out.Meetings = &drained
		out.TruncatedMeetings = truncated
		out.Resources = all
	} else {
		resourceType := pastMeetingParticipantResourceType
		tags, filtersAll := participantFilters(args)
		payload := &querysvc.QueryResourcesPayload{
			Version:    "1",
			Type:       &resourceType,
			Tags:       tags,
			FiltersAll: filtersAll,
			PageSize:   pageSize,
			Sort:       sort,
		}
		if parent != "" {
			payload.Parent = strPtr(parent)
		}
		if args.Name != "" {
			payload.Name = strPtr(args.Name)
		}
		if args.PageToken != "" {
			payload.PageToken = strPtr(args.PageToken)
		}
		result, err := clients.QuerySvc.QueryResources(ctx, payload)
		if err != nil {
			logger.ErrorContext(ctx, "QueryResources failed", "error", err)
			return errorResult(friendlyAPIError("failed to search past meeting participants", err)), nil, nil
		}
		out.Resources = result.Resources
		out.PageToken = result.PageToken
		if result.PageToken != nil && len(result.Resources) < pageSize {
			pageWarning = participantPageWarning
		}
	}

	records := len(out.Resources)
	if dedupe {
		out.Resources = dedupeParticipants(out.Resources)
		people := len(out.Resources)
		out.People = &people
		out.Records = &records
	}
	if out.Resources == nil {
		out.Resources = []*querysvc.Resource{}
	}
	var notes []string
	if len(out.Resources) == 0 {
		notes = append(notes, participantEmptyNote)
	}
	if dedupe && !hasDateRange && (out.PageToken != nil || args.PageToken != "") {
		notes = append(notes, participantPerPageNote)
	}
	if truncated {
		notes = append(notes, fmt.Sprintf(participantTruncatedNote, maxMeetings, participantHardMaxMeetings))
	}
	if out.TruncatedRecords {
		notes = append(notes, fmt.Sprintf(participantRecordsCapNote, participantMaxRecords))
	}
	out.Note = strings.Join(notes, " ")

	prettyJSON, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal search result", "error", err)
		return errorResult(fmt.Sprintf("Error: failed to format result: %v", err)), nil, nil
	}

	logger.InfoContext(ctx, "search past meeting participants succeeded", "records", records, "returned", len(out.Resources))

	content := []mcp.Content{}
	if pageWarning != "" {
		content = append(content, &mcp.TextContent{Text: pageWarning})
	}
	content = append(content, &mcp.TextContent{Text: string(prettyJSON)})
	return &mcp.CallToolResult{Content: content}, nil, nil
}

// jsonResult marshals v as the single text block of a successful result.
func jsonResult(ctx context.Context, logger *slog.Logger, msg string, v any) (*mcp.CallToolResult, any, error) {
	prettyJSON, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		logger.ErrorContext(ctx, "failed to marshal result", "error", err)
		return errorResult(fmt.Sprintf("Error: failed to format result: %v", err)), nil, nil
	}
	logger.InfoContext(ctx, msg)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(prettyJSON)}}}, nil, nil
}
