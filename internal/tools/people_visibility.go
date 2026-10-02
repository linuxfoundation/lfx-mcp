// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
//
// This file holds the rules that make people data match what LFX Self Serve
// shows on screen to a caller without full view (see full_view.go): the
// predicates, evaluated with the caller's own exchanged token, and the
// projections that reduce or drop records. Handlers decide the view before
// the upstream query and narrow the query to it, then select and project the
// records after it; a caller with full view never reaches them. The rules
// only ever remove records or fields, never add any.
package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	committeeservice "github.com/linuxfoundation/lfx-v2-committee-service/gen/committee_service"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

// peopleVisibilityUnavailableMessage is the tool error returned when a
// predicate call fails for a caller without full view. The tool returns no
// records with it: an empty page next to a failure would read as absence.
const peopleVisibilityUnavailableMessage = "Error: could not confirm what LFX Self Serve shows you for this request; try again."

// errPeopleVisibilityCap is returned when a predicate lookup needed more
// pages than peopleLookupMaxPages; the caller fails closed.
var errPeopleVisibilityCap = errors.New("visibility lookup exceeded its page cap")

// peopleLookupPageSize is the page size of the predicate lookups.
const peopleLookupPageSize = 100

// peopleLookupMaxPages caps every predicate lookup's page_token loop.
const peopleLookupMaxPages = 10

// --- field projection ---

// keepFields returns a new map holding only the listed paths of data. A path
// is a top-level key, or "key.subkey" to keep one field of a nested object
// (several subkeys of the same key are collected into one nested map).
// Missing paths are skipped.
func keepFields(data map[string]any, paths ...string) map[string]any {
	out := make(map[string]any, len(paths))
	for _, path := range paths {
		key, sub, nested := strings.Cut(path, ".")
		value, ok := data[key]
		if !ok {
			continue
		}
		if !nested {
			out[key] = value
			continue
		}
		obj, ok := value.(map[string]any)
		if !ok {
			continue
		}
		subValue, ok := obj[sub]
		if !ok {
			continue
		}
		target, _ := out[key].(map[string]any)
		if target == nil {
			target = make(map[string]any, 2)
			out[key] = target
		}
		target[sub] = subValue
	}
	return out
}

// resourceData returns the resource's Data as an object, or nil.
func resourceData(r *querysvc.Resource) map[string]any {
	if r == nil {
		return nil
	}
	data, _ := r.Data.(map[string]any)
	return data
}

// dataString returns data[key] as a string, or "".
func dataString(data map[string]any, key string) string {
	v, _ := data[key].(string)
	return v
}

// --- groups (committee members) ---

// rosterView is what LFX Self Serve shows a caller of one group's members.
type rosterView int

const (
	// rosterChairsOnly is every viewer's Overview tab: the Chair and Vice
	// Chair names and roles, nothing else.
	rosterChairsOnly rosterView = iota
	// rosterBasicProfile is the Members tab for an auditor who is also a
	// member of a group whose member_visibility is basic_profile.
	rosterBasicProfile
	// rosterFull is a writer's view: records unchanged.
	rosterFull
)

// groupRosterViews decides the rosterView of each committee UID for the
// caller, with the caller's token: one access-check batch for writer and
// auditor, then, for groups where the caller audits but does not write, the
// caller's own membership (one committee_member query on the username tag)
// and the group's settings read as the caller (a 403 or 404 means the list
// is not shown). Any other failure is returned and the caller fails closed.
func groupRosterViews(ctx context.Context, clients *lfxv2.Clients, tokenInfo *auth.TokenInfo, committeeUIDs []string) (map[string]rosterView, error) {
	views := make(map[string]rosterView, len(committeeUIDs))
	uids := dedupeStrings(committeeUIDs)
	if len(uids) == 0 {
		return views, nil
	}
	reqs := make([]string, 0, 2*len(uids))
	for _, uid := range uids {
		reqs = append(reqs, "committee:"+uid+"#writer", "committee:"+uid+"#auditor")
	}
	relations, err := clients.CheckRelations(ctx, reqs)
	if err != nil {
		return nil, err
	}
	var auditorOnly []string
	for _, uid := range uids {
		switch {
		case relations["committee:"+uid+"#writer"]:
			views[uid] = rosterFull
		case relations["committee:"+uid+"#auditor"]:
			views[uid] = rosterChairsOnly
			auditorOnly = append(auditorOnly, uid)
		default:
			views[uid] = rosterChairsOnly
		}
	}
	if len(auditorOnly) == 0 {
		return views, nil
	}
	username := callerUsername(tokenInfo)
	if username == "" {
		return views, nil
	}
	memberOf, err := callerCommitteeMemberships(ctx, clients, username)
	if err != nil {
		return nil, err
	}
	for _, uid := range auditorOnly {
		if !memberOf[uid] {
			continue
		}
		settings, err := clients.Committee.GetCommitteeSettings(ctx, &committeeservice.GetCommitteeSettingsPayload{UID: strPtr(uid)})
		if err != nil {
			switch lfxv2.UpstreamStatus(err) {
			case http.StatusForbidden, http.StatusNotFound:
				continue
			}
			return nil, err
		}
		if settings != nil && settings.CommitteeSettings != nil && settings.CommitteeSettings.MemberVisibility == "basic_profile" {
			views[uid] = rosterBasicProfile
		}
	}
	return views, nil
}

// callerCommitteeMemberships returns the committee UIDs the caller holds a
// member record in, from one committee_member query on the username tag (the
// check LFX Self Serve itself makes), as the caller.
func callerCommitteeMemberships(ctx context.Context, clients *lfxv2.Clients, username string) (map[string]bool, error) {
	resourceType := committeeMemberResourceType
	memberOf := make(map[string]bool)
	var pageToken *string
	for pages := 0; ; pages++ {
		if pages >= peopleLookupMaxPages {
			return nil, errPeopleVisibilityCap
		}
		result, err := clients.QuerySvc.QueryResources(ctx, &querysvc.QueryResourcesPayload{
			Version:   "1",
			Type:      &resourceType,
			TagsAll:   []string{"username:" + username},
			PageSize:  peopleLookupPageSize,
			Sort:      "name_asc",
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range result.Resources {
			if uid := dataString(resourceData(r), "committee_uid"); uid != "" {
				memberOf[uid] = true
			}
		}
		if !hasPageToken(result.PageToken) {
			return memberOf, nil
		}
		pageToken = result.PageToken
	}
}

// committeeChairRoles are the role names every viewer of a group sees on its
// Overview tab.
var committeeChairRoles = map[string]struct{}{"Chair": {}, "Vice Chair": {}}

// committeeChairFilters is the filters_or clause that narrows a
// committee_member query to the records rosterChairsOnly shows.
func committeeChairFilters() []string {
	filters := make([]string, 0, len(committeeChairRoles))
	for role := range committeeChairRoles {
		filters = append(filters, "role.name:"+role)
	}
	slices.Sort(filters)
	return filters
}

// projectGroupUIDs returns the UIDs of the project's groups visible to the
// caller, from the committee records whose parent is the project, read as
// the caller. Member records carry the same visibility as their group, so
// these are the groups whose members a project-wide search can return.
func projectGroupUIDs(ctx context.Context, clients *lfxv2.Clients, projectUID string) ([]string, error) {
	resourceType := committeeResourceType
	var uids []string
	var pageToken *string
	for pages := 0; ; pages++ {
		if pages >= peopleLookupMaxPages {
			return nil, errPeopleVisibilityCap
		}
		result, err := clients.QuerySvc.QueryResources(ctx, &querysvc.QueryResourcesPayload{
			Version:   "1",
			Type:      &resourceType,
			Parent:    strPtr("project:" + projectUID),
			PageSize:  peopleLookupPageSize,
			Sort:      "name_asc",
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range result.Resources {
			uid := dataString(resourceData(r), "uid")
			if uid == "" && r != nil && r.ID != nil {
				uid = *r.ID
			}
			if uid != "" {
				uids = append(uids, uid)
			}
		}
		if !hasPageToken(result.PageToken) {
			return dedupeStrings(uids), nil
		}
		pageToken = result.PageToken
	}
}

// projectRosterFilters is the filters_or clause that narrows a project-wide
// committee_member query to what views shows: every member of the groups
// whose member list is shown, and the chairs of the others. ok is false when
// the clause would exceed peopleFilterChunk terms, and the search is then
// refused rather than read unnarrowed.
func projectRosterFilters(uids []string, views map[string]rosterView) (filters []string, ok bool) {
	chairs := false
	for _, uid := range uids {
		if views[uid] == rosterChairsOnly {
			chairs = true
			continue
		}
		filters = append(filters, "committee_uid:"+uid)
	}
	if chairs || len(filters) == 0 {
		filters = append(committeeChairFilters(), filters...)
	}
	if len(filters) > peopleFilterChunk {
		return nil, false
	}
	return filters, true
}

// personFilterShown reports whether a person filter may run over the
// groups uids: every one of them shows the caller its member list, and, for
// name (needFull), every one is managed by the caller. No groups, no list.
func personFilterShown(uids []string, views map[string]rosterView, needFull bool) bool {
	if len(uids) == 0 {
		return false
	}
	for _, uid := range uids {
		view := views[uid]
		if view == rosterChairsOnly || (needFull && view != rosterFull) {
			return false
		}
	}
	return true
}

// committeeMemberRoleName returns the record's role.name, or "".
func committeeMemberRoleName(data map[string]any) string {
	role, _ := data["role"].(map[string]any)
	return dataString(role, "name")
}

// projectCommitteeMember reduces one committee_member record to the view.
// ok is false when the record is not shown at all.
func projectCommitteeMember(data map[string]any, view rosterView) (out map[string]any, ok bool) {
	switch view {
	case rosterFull:
		return data, true
	case rosterBasicProfile:
		return keepFields(data,
			"uid", "committee_uid", "committee_name", "committee_category",
			"first_name", "last_name", "email",
			"organization.name", "organization.website",
			"role.name", "voting.status",
		), true
	}
	if _, chair := committeeChairRoles[committeeMemberRoleName(data)]; !chair {
		return nil, false
	}
	return keepFields(data, "uid", "committee_uid", "committee_name", "first_name", "last_name", "role.name"), true
}

// projectCommitteeMemberRecord applies projectCommitteeMember to the typed
// record the committee service returns. It builds a new record from the
// fields the view shows, so a field the service adds later is never
// returned by default. ok is false when the record is not shown at all.
func projectCommitteeMemberRecord(m *committeeservice.CommitteeMemberFullWithReadonlyAttributes, view rosterView) (out *committeeservice.CommitteeMemberFullWithReadonlyAttributes, ok bool) {
	if m == nil {
		return nil, false
	}
	if view == rosterFull {
		return m, true
	}
	roleName := ""
	if m.Role != nil {
		roleName = m.Role.Name
	}
	if view == rosterChairsOnly {
		if _, chair := committeeChairRoles[roleName]; !chair {
			return nil, false
		}
	}
	out = &committeeservice.CommitteeMemberFullWithReadonlyAttributes{
		UID:           m.UID,
		CommitteeUID:  m.CommitteeUID,
		CommitteeName: m.CommitteeName,
		FirstName:     m.FirstName,
		LastName:      m.LastName,
	}
	if m.Role != nil {
		out.Role = zeroOf(m.Role)
		out.Role.Name = m.Role.Name
	}
	if view == rosterChairsOnly {
		return out, true
	}
	out.CommitteeCategory = m.CommitteeCategory
	out.Email = m.Email
	if m.Voting != nil {
		out.Voting = zeroOf(m.Voting)
		out.Voting.Status = m.Voting.Status
	}
	if m.Organization != nil {
		out.Organization = zeroOf(m.Organization)
		out.Organization.Name = m.Organization.Name
		out.Organization.Website = m.Organization.Website
	}
	return out, true
}

// zeroOf returns a new zero value of the type p points to, for the generated
// records' anonymous nested structs, which have no type name to construct.
func zeroOf[T any](_ *T) *T {
	return new(T)
}

// filterCommitteeMembers applies the roster rule to a page of committee_member
// resources, returning the records shown. views must cover every
// committee_uid on the page; a record without one is not shown.
func filterCommitteeMembers(resources []*querysvc.Resource, views map[string]rosterView) []*querysvc.Resource {
	out := make([]*querysvc.Resource, 0, len(resources))
	for _, r := range resources {
		data := resourceData(r)
		uid := dataString(data, "committee_uid")
		view, known := views[uid]
		if data == nil || !known {
			continue
		}
		projected, ok := projectCommitteeMember(data, view)
		if !ok {
			continue
		}
		out = append(out, &querysvc.Resource{Type: r.Type, ID: r.ID, Data: projected})
	}
	return out
}

// dataStrings collects the distinct non-empty values of key across a page.
func dataStrings(resources []*querysvc.Resource, key string) []string {
	var out []string
	for _, r := range resources {
		if v := dataString(resourceData(r), key); v != "" {
			out = append(out, v)
		}
	}
	return dedupeStrings(out)
}

// --- count_lfx_resources ---

// peopleCountGate decides whether a count_lfx_resources call on a people
// type is one LFX Self Serve shows the caller. It returns the refusal text
// (or "" to proceed) and an error when a predicate could not be evaluated.
// Types that are not people records pass unchanged.
//
//   - committee_member: member counts are on every group page. Allowed with
//     a committee: or project: parent and committee_uid:, project_uid:,
//     committee_category: or voting_status: tags; nothing that names a
//     person (name, filters_or, filters_all, other tags) and no date range.
//   - v1_meeting_registrant: the count is shown to a meeting's organizers
//     and registrants. Allowed only as parent=meeting:<id> with no other
//     filter, when the caller has one of those two views.
//   - v1_past_meeting_participant: attendance counts are shown to callers
//     with full access to the past meeting. Allowed only as
//     parent=past_meeting:<id>, optionally with the is_attended:true tag,
//     when the caller is its organizer or has full access.
//   - v1_meeting and v1_past_meeting: the records are not people records,
//     but filters_or / filters_all on a field that names a person (editors,
//     owner, organizer accounts, user id, registrant counts) would single
//     one out; only the meeting's own fields (meetingCountFilterFields) and
//     date fields (meetingCountDateFields) are accepted.
func peopleCountGate(ctx context.Context, clients *lfxv2.Clients, tokenInfo *auth.TokenInfo, args CountLFXResourcesArgs) (refusal string, err error) {
	hasPersonFilter := args.Name != "" || len(args.FiltersOr) > 0 || len(args.FiltersAll) > 0
	hasDateRange := args.DateField != "" || args.DateFrom != "" || args.DateTo != ""
	switch args.Type {
	case committeeMemberResourceType:
		allowed := "an optional committee:<uid> or project:<uid> parent and committee_uid:, project_uid:, committee_category: or voting_status: tags only"
		parentOK := args.Parent == "" || strings.HasPrefix(args.Parent, "committee:") || strings.HasPrefix(args.Parent, "project:")
		tagPrefixes := []string{"committee_uid:", "project_uid:", "committee_category:", "voting_status:"}
		if hasPersonFilter || hasDateRange || !parentOK ||
			!tagsHaveOnlyPrefixes(args.Tags, tagPrefixes...) || !tagsHaveOnlyPrefixes(args.TagsAll, tagPrefixes...) {
			return countRefusal(args.Type, allowed), nil
		}
		return "", nil
	case meetingRegistrantResourceType:
		allowed := "parent=meeting:<id> alone, for a meeting you organize or are registered for"
		id, ok := strings.CutPrefix(args.Parent, "meeting:")
		if !ok || id == "" || hasPersonFilter || hasDateRange || len(args.Tags) > 0 || len(args.TagsAll) > 0 {
			return countRefusal(args.Type, allowed), nil
		}
		views, err := registrantViews(ctx, clients, tokenInfo, []string{id})
		if err != nil {
			return "", err
		}
		if views[id] == registrantHidden {
			return countRefusal(args.Type, allowed), nil
		}
		return "", nil
	case pastMeetingParticipantResourceType:
		allowed := "parent=past_meeting:<meeting_and_occurrence_id>, optionally with the is_attended:true tag, for a past meeting you organize or have full access to"
		id, ok := strings.CutPrefix(args.Parent, "past_meeting:")
		if !ok || id == "" || hasPersonFilter || hasDateRange ||
			!tagsAreExactly(args.Tags, "is_attended:true") || !tagsAreExactly(args.TagsAll, "is_attended:true") {
			return countRefusal(args.Type, allowed), nil
		}
		views, err := participantViews(ctx, clients, []string{id}, nil)
		if err != nil {
			return "", err
		}
		if views[id] == participantOwnOnly {
			return countRefusal(args.Type, allowed), nil
		}
		return "", nil
	case meetingResourceType, pastMeetingResourceType:
		refusal := fmt.Sprintf("Error: counting %s accepts filters_or / filters_all only on the meeting's own fields (%s) and a date_field only of %s.", args.Type, strings.Join(meetingCountFilterFields, ", "), strings.Join(meetingCountDateFields, ", "))
		for _, filter := range append(append([]string{}, args.FiltersOr...), args.FiltersAll...) {
			// The query service trims the field name before it applies the
			// filter, so match the trimmed form.
			field, _, _ := strings.Cut(filter, ":")
			if !slices.Contains(meetingCountFilterFields, strings.TrimSpace(field)) {
				return refusal, nil
			}
		}
		if args.DateField != "" && !slices.Contains(meetingCountDateFields, strings.TrimSpace(args.DateField)) {
			return refusal, nil
		}
		return "", nil
	}
	return "", nil
}

// meetingCountFilterFields are the only data fields a caller without full
// view may filter v1_meeting and v1_past_meeting counts on: the fields the
// meeting search tools themselves filter on, plus a record's own identifiers,
// schedule and artifact settings (names as the meeting service publishes
// them, internal/domain/models/event_models.go). An allowlist, so that every
// field naming a person (created_by, owner, organizers, user_id, updated_by,
// updated_by_list, registrant counts), every meeting secret (the passwords
// and the Zoom settings) and any nested path stays out without being
// enumerated.
var meetingCountFilterFields = []string{
	"id", "project_uid", "project_slug", "committee", "committee_uid",
	"meeting_id", "meeting_and_occurrence_id", "occurrence_id",
	"visibility", "restricted", "meeting_type", "platform", "title", "duration", "timezone",
	"is_manually_created", "show_meeting_attendees", "artifact_visibility",
	"recording_enabled", "recording_access", "transcript_enabled", "transcript_access",
	"ai_summary_access", "zoom_ai_enabled", "youtube_upload_enabled",
}

// meetingCountDateFields are the date fields a caller without full view may
// range v1_meeting and v1_past_meeting counts over.
var meetingCountDateFields = []string{"start_time", "end_time", "created_at", "updated_at"}

// PeopleToolNames are the tools whose results follow the people rule above,
// under both terminology modes. The tests in internal/tools walk every name
// (people_tools_registry_test.go) and cmd/lfx-mcp-server checks that every
// tool newServer registers is either here or declared in its nonPeopleTools
// with a reason (no people data, or out of this rule's scope), so a new tool
// cannot ship without that decision.
var PeopleToolNames = []string{
	"search_committee_members", "search_group_members",
	"get_committee_member", "get_group_member",
	"get_committee", "get_group",
	"search_meeting_registrants", "get_meeting_registrant",
	"search_meetings", "get_meeting",
	"search_past_meeting_participants", "get_past_meeting_participant",
	"search_past_meetings", "get_past_meeting",
	"search_past_meeting_summaries", "get_past_meeting_summary",
}

// countRefusal is the tool error refusing a count of a people type for a
// caller without full view; allowed names the accepted form.
func countRefusal(resourceType, allowed string) string {
	return fmt.Sprintf("Error: counting %s is available in the form LFX Self Serve shows you: %s.", resourceType, allowed)
}

// tagsHaveOnlyPrefixes reports whether every tag starts with one of prefixes.
func tagsHaveOnlyPrefixes(tags []string, prefixes ...string) bool {
	for _, tag := range tags {
		ok := false
		for _, p := range prefixes {
			if strings.HasPrefix(tag, p) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
