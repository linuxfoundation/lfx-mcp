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

// participantRecordsCapNote is added when the date range collected more
// participant records than participantMaxRecords.
const participantRecordsCapNote = "The date range matched more than %d participant records; the result stops there (truncated_records=true). Narrow the range, add attended_only or org_name, or use count_only."

// participantPerPageNote explains dedup scope on a paged call.
const participantPerPageNote = "people and records describe this page only; a person whose records straddle pages can appear on more than one page."

// participantCountRecordsNote distinguishes counted records from people.
const participantCountRecordsNote = " This counts participant records, not distinct people; use count_only=false for de-duplicated people."

// participantRecord is the subset of a v1_past_meeting_participant document
// the dedup merge reads and writes. Every other field is carried through
// untouched in Raw.
type participantRecord struct {
	UID                string `json:"uid"`
	Email              string `json:"email"`
	IsAttended         bool   `json:"is_attended"`
	IsInvited          bool   `json:"is_invited"`
	Host               bool   `json:"host"`
	OrgIsMember        bool   `json:"org_is_member"`
	OrgIsProjectMember bool   `json:"org_is_project_member"`
	AvatarURL          string `json:"avatar_url"`
	JobTitle           string `json:"job_title"`
	OrgName            string `json:"org_name"`
	Username           string `json:"username"`
}

// participantSearchResult is the output shape of search_past_meeting_participants.
type participantSearchResult struct {
	Resources         []*querysvc.Resource `json:"resources"`
	PageToken         *string              `json:"page_token,omitempty"`
	People            *int                 `json:"people,omitempty"`
	Records           *int                 `json:"records,omitempty"`
	Meetings          *int                 `json:"meetings,omitempty"`
	TruncatedMeetings bool                 `json:"truncated_meetings,omitempty"`
	TruncatedRecords  bool                 `json:"truncated_records,omitempty"`
	Note              string               `json:"note,omitempty"`
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
func resolvePastMeetingIDs(ctx context.Context, clients *lfxv2.Clients, parent string, args SearchPastMeetingParticipantsArgs, maxMeetings int) (ids []string, truncated bool, err error) {
	resourceType := pastMeetingResourceType
	dateField := "start_time"
	var pageToken *string
	for pages := 0; ; pages++ {
		if pages >= participantMaxDrainPages {
			return nil, false, errDrainPageCap
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
// pages run out or budget records have been collected (capped reports the
// latter).
func drainParticipants(ctx context.Context, clients *lfxv2.Clients, parent string, args SearchPastMeetingParticipantsArgs, sort string, budget int) (out []*querysvc.Resource, capped bool, err error) {
	resourceType := pastMeetingParticipantResourceType
	tags, filtersAll := participantFilters(args)
	var pageToken *string
	for pages := 0; ; pages++ {
		if pages >= participantMaxDrainPages {
			return nil, false, errDrainPageCap
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
		if len(out) >= budget {
			return out[:budget], len(out) > budget || (result.PageToken != nil && *result.PageToken != ""), nil
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

// dedupeParticipants collapses participant records into people the way LFX
// Self Serve's getPastMeetingParticipants does: key = trimmed lower-cased
// e-mail, else uid; attendance flags OR'd; the attended record's fields win;
// missing fields filled from the other record. Order of first appearance is kept.
func dedupeParticipants(resources []*querysvc.Resource) []*querysvc.Resource {
	type entry struct {
		res *querysvc.Resource
		rec participantRecord
		raw map[string]any
	}
	index := map[string]int{}
	var entries []entry

	for _, res := range resources {
		raw, ok := res.Data.(map[string]any)
		if !ok {
			// Not a participant document we can read; keep as-is under its id.
			entries = append(entries, entry{res: res})
			continue
		}
		rec := participantFromMap(raw)
		key := strings.ToLower(strings.TrimSpace(rec.Email))
		if key == "" {
			key = rec.UID
		}
		if key == "" && res.ID != nil {
			key = *res.ID
		}
		i, seen := index[key]
		if !seen || key == "" {
			index[key] = len(entries)
			entries = append(entries, entry{res: res, rec: rec, raw: raw})
			continue
		}

		existing := entries[i]
		preferred, other := existing, entry{res: res, rec: rec, raw: raw}
		if rec.IsAttended && !existing.rec.IsAttended {
			preferred, other = other, preferred
		}

		merged := map[string]any{}
		for k, v := range preferred.raw {
			merged[k] = v
		}
		merged["is_attended"] = existing.rec.IsAttended || rec.IsAttended
		merged["is_invited"] = existing.rec.IsInvited || rec.IsInvited
		merged["host"] = existing.rec.Host || rec.Host
		merged["org_is_member"] = existing.rec.OrgIsMember || rec.OrgIsMember
		merged["org_is_project_member"] = existing.rec.OrgIsProjectMember || rec.OrgIsProjectMember
		for _, field := range []string{"avatar_url", "job_title", "org_name", "username"} {
			if isEmptyValue(merged[field]) {
				if v, ok := other.raw[field]; ok && !isEmptyValue(v) {
					merged[field] = v
				}
			}
		}

		mergedRes := &querysvc.Resource{Type: preferred.res.Type, ID: preferred.res.ID, Data: merged}
		entries[i] = entry{res: mergedRes, rec: participantFromMap(merged), raw: merged}
	}

	out := make([]*querysvc.Resource, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.res)
	}
	return out
}

// participantFromMap reads the dedup-relevant fields from a document map.
func participantFromMap(m map[string]any) participantRecord {
	str := func(k string) string {
		v, _ := m[k].(string)
		return v
	}
	b := func(k string) bool {
		v, _ := m[k].(bool)
		return v
	}
	return participantRecord{
		UID:                str("uid"),
		Email:              str("email"),
		IsAttended:         b("is_attended"),
		IsInvited:          b("is_invited"),
		Host:               b("host"),
		OrgIsMember:        b("org_is_member"),
		OrgIsProjectMember: b("org_is_project_member"),
		AvatarURL:          str("avatar_url"),
		JobTitle:           str("job_title"),
		OrgName:            str("org_name"),
		Username:           str("username"),
	}
}

// isEmptyValue treats nil and "" as absent for the fill-from-other merge.
func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
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
	if maxMeetings > participantHardMaxMeetings {
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
	if hasDateRange {
		ids, trunc, err := resolvePastMeetingIDs(ctx, clients, parent, args, maxMeetings)
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
		for _, p := range parents {
			rs, capped, err := drainParticipants(ctx, clients, p, args, sort, participantMaxRecords-len(all))
			if err != nil {
				logger.ErrorContext(ctx, "QueryResources failed", "error", err)
				return errorResult(friendlyAPIError("failed to search past meeting participants", err)), nil, nil
			}
			all = append(all, rs...)
			if capped || len(all) >= participantMaxRecords {
				out.TruncatedRecords = true
				break
			}
		}
		n := len(parents)
		out.Meetings = &n
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
