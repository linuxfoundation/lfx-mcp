// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fullViewCallCase is one call, beyond the people registry's single call per
// tool, whose full-view output and upstream requests must match what
// origin/main produced for it: the paths the people rule adds a branch to
// (counts, count_only, date ranges, whole-project scopes and the filters a
// caller without full view is refused). This file uses only helpers that
// exist at origin/main, so the goldens can be generated there.
type fullViewCallCase struct {
	name  string
	setup func(t *testing.T) *stubLFXAPI
	call  func(ctx context.Context) (*mcp.CallToolResult, any, error)
	// wantError marks a call whose expected output is a tool error; its
	// text is compared like any other output.
	wantError bool
}

const fullViewMemberDoc = `{
  "type": "committee_member",
  "id": "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
  "data": {
    "uid": "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
    "committee_uid": "66666666-6666-4666-8666-666666666666",
    "committee_name": "Technical Steering Committee",
    "username": "member-dddd",
    "email": "member@example.test",
    "first_name": "Pat",
    "last_name": "Member",
    "job_title": "Engineer",
    "role": {"name": "None", "start_date": "2026-01-01"},
    "voting": {"status": "Voting Rep", "start_date": "2026-01-01"},
    "organization": {"id": "org-1", "name": "Example Org", "website": "https://example.test"},
    "project_uid": "P1"
  }
}`

const fullViewRegistrantDoc = `{
  "type": "v1_meeting_registrant",
  "id": "reg-1",
  "data": {
    "uid": "reg-1",
    "meeting_id": "77777777777",
    "committee_uid": "66666666-6666-4666-8666-666666666666",
    "email": "registrant@example.test",
    "first_name": "Reg",
    "last_name": "Istrant",
    "username": "registrant",
    "host": false,
    "org_name": "Example Org"
  }
}`

// The mailing-list fixtures follow the groupsio_member indexer contract and
// the get-groupsio-member response body of lfx-v2-mailing-list-service
// v0.5.12; the list UID is the Groups.io group ID, as on the live index.
const (
	fullViewMailingListID         = "145670"
	fullViewMailingListMemberID   = "14875835"
	fullViewMailingListMemberPath = "/groupsio/mailing-lists/" + fullViewMailingListID + "/members/" + fullViewMailingListMemberID
)

const fullViewMailingListMemberDoc = `{
  "type": "groupsio_member",
  "id": "groupsio_member:gm-1",
  "data": {
    "uid": "gm-1",
    "mailing_list_uid": "145670",
    "member_id": 14875835,
    "group_id": 145670,
    "source": "v1-sync",
    "username": "subscriber-1",
    "first_name": "Sam",
    "last_name": "Subscriber",
    "email": "subscriber@example.test",
    "organization": "Example Org",
    "job_title": "Engineer",
    "member_type": "direct",
    "delivery_mode": "email_delivery_single",
    "mod_status": "none",
    "status": "normal",
    "last_reviewed_at": null,
    "last_reviewed_by": null,
    "project_uid": "P1",
    "project_slug": "example",
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-01-02T00:00:00Z"
  }
}`

const fullViewMailingListMemberRecord = `{
  "id": "14875835",
  "email": "subscriber@example.test",
  "name": "Sam Subscriber",
  "member_type": "direct",
  "delivery_mode": "email_delivery_single",
  "mod_status": "none",
  "status": "normal",
  "organization": "Example Org",
  "job_title": "Engineer",
  "username": "subscriber-1",
  "role": "None",
  "voting_status": "Non-Voting",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-02T00:00:00Z"
}`

var fullViewCallCases = []fullViewCallCase{
	{
		name: "search_mailing_list_members.one_list",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupMailingListTest(t)
			api.Respond(resourcesPath, page([]string{fullViewMailingListMemberDoc}, "next"))
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchMailingListMembers(ctx, stubCallToolRequest(), SearchMailingListMembersArgs{
				MailingListID: fullViewMailingListID, Name: "Sam", PageSize: 25,
			})
		},
	},
	{
		name: "search_mailing_list_members.project",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupMailingListTest(t)
			api.Respond(resourcesPath, page([]string{fullViewMailingListMemberDoc}, ""))
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchMailingListMembers(ctx, stubCallToolRequest(), SearchMailingListMembersArgs{ProjectUID: "P1"})
		},
	},
	{
		name: "search_mailing_list_members.unscoped_name",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupMailingListTest(t)
			api.Respond(resourcesPath, page([]string{fullViewMailingListMemberDoc}, ""))
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchMailingListMembers(ctx, stubCallToolRequest(), SearchMailingListMembersArgs{Name: "subscriber@example.test"})
		},
	},
	{
		name: "get_mailing_list_member.lookup",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupMailingListTest(t)
			api.Respond(fullViewMailingListMemberPath, fullViewMailingListMemberRecord)
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleGetMailingListMember(ctx, stubCallToolRequest(), GetMailingListMemberArgs{
				MailingListID: fullViewMailingListID, MemberID: fullViewMailingListMemberID,
			})
		},
	},
	{
		// The text a caller without full view gets for a member it is not
		// shown must equal this one, byte for byte.
		name: "get_mailing_list_member.not_found",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupMailingListTest(t)
			api.RespondStatus(fullViewMailingListMemberPath, http.StatusNotFound, `{"name":"NotFound","message":"member not found"}`)
			return api
		},
		wantError: true,
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleGetMailingListMember(ctx, stubCallToolRequest(), GetMailingListMemberArgs{
				MailingListID: fullViewMailingListID, MemberID: fullViewMailingListMemberID,
			})
		},
	},
	{
		name: "count_lfx_resources.mailing_list_member_parent",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupCountTest(t)
			api.Respond(countPath, `{"count": 24, "has_more": false}`)
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleCountLFXResources(ctx, stubCallToolRequest(), CountLFXResourcesArgs{
				Type: "groupsio_member", Parent: "groupsio_mailing_list:" + fullViewMailingListID, Tags: []string{"status:normal"},
			})
		},
	},
	{
		name: "count_lfx_resources.mailing_list_member_without_parent",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupCountTest(t)
			api.Respond(countPath, `{"count": 7, "has_more": false}`)
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleCountLFXResources(ctx, stubCallToolRequest(), CountLFXResourcesArgs{
				Type: "groupsio_member", TagsAll: []string{"project_uid:P1"}, FiltersOr: []string{"email:subscriber@example.test"},
			})
		},
	},
	{
		name:      "search_past_meeting_participants.date_range_without_scope",
		setup:     setupParticipantTest,
		wantError: true,
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchPastMeetingParticipants(ctx, stubCallToolRequest(), SearchPastMeetingParticipantsArgs{DateFrom: "2026-06-01"})
		},
	},
	{
		// No token at all: the max_meetings bound is checked before
		// authentication, so it is the error returned.
		name:      "search_past_meeting_participants.max_meetings_without_token",
		setup:     setupParticipantTest,
		wantError: true,
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchPastMeetingParticipants(ctx, &mcp.CallToolRequest{}, SearchPastMeetingParticipantsArgs{
				ProjectUID: "P1", DateFrom: "2026-06-01", MaxMeetings: 201,
			})
		},
	},
	{
		name: "count_lfx_resources.committee_member_email",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupCountTest(t)
			api.Respond(countPath, `{"count": 3, "has_more": false}`)
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleCountLFXResources(ctx, stubCallToolRequest(), CountLFXResourcesArgs{
				Type: "committee_member", Parent: "project:P1", FiltersAll: []string{"email:member@example.test"},
			})
		},
	},
	{
		name: "count_lfx_resources.meeting_people_fields",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupCountTest(t)
			api.Respond(countPath, `{"count": 2, "has_more": false}`)
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleCountLFXResources(ctx, stubCallToolRequest(), CountLFXResourcesArgs{
				Type: "v1_meeting", FiltersOr: []string{"created_by.email:editor@example.test", "organizers:someone"},
				DateField: "updated_at", DateFrom: "2026-01-01",
			})
		},
	},
	{
		name: "count_lfx_resources.registrant_tags",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupCountTest(t)
			api.Respond(countPath, `{"count": 1, "has_more": false}`)
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleCountLFXResources(ctx, stubCallToolRequest(), CountLFXResourcesArgs{
				Type: "v1_meeting_registrant", Tags: []string{"email:registrant@example.test"},
			})
		},
	},
	{
		name: "count_lfx_resources.participant_name",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupCountTest(t)
			api.Respond(countPath, `{"count": 4, "has_more": true}`)
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleCountLFXResources(ctx, stubCallToolRequest(), CountLFXResourcesArgs{
				Type: "v1_past_meeting_participant", Parent: "project:P1", Name: "Ann",
			})
		},
	},
	{
		name: "search_past_meeting_participants.count_only",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupParticipantTest(t)
			api.Respond(countPath, `{"count": 2, "has_more": false}`)
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchPastMeetingParticipants(ctx, stubCallToolRequest(), SearchPastMeetingParticipantsArgs{
				PastMeetingID: "91461158520-1771596000000", CountOnly: true, AttendedOnly: true,
			})
		},
	},
	{
		name: "search_past_meeting_participants.project_filters",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupParticipantTest(t)
			api.Respond(resourcesPath, page([]string{
				participantDoc("p1", "ann@example.test", "Ann", "A", true, true, "Red Hat"),
				participantDoc("p2", "ann@example.test", "Ann", "A", false, true, "Red Hat"),
			}, "next"))
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchPastMeetingParticipants(ctx, stubCallToolRequest(), SearchPastMeetingParticipantsArgs{
				ProjectUID: "P1", Name: "Ann", OrgName: "Red Hat", Sort: "name_desc",
			})
		},
	},
	{
		name: "search_past_meeting_participants.date_range",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupParticipantTest(t)
			api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-1"), pastMeetingDoc("m-2")}, ""))
			api.Respond(resourcesPath, page([]string{
				participantDoc("p1", "a@example.test", "A", "A", true, true, "Red Hat"),
				participantDoc("p2", "b@example.test", "B", "B", false, true, "SUSE"),
			}, ""))
			api.Respond(resourcesPath, page([]string{participantDoc("p3", "a@example.test", "A", "A", false, true, "Red Hat")}, ""))
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchPastMeetingParticipants(ctx, stubCallToolRequest(), SearchPastMeetingParticipantsArgs{
				ProjectUID: "P1", DateFrom: "2026-06-01", DateTo: "2026-06-30",
			})
		},
	},
	{
		name: "search_past_meeting_participants.date_range_count_only",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupParticipantTest(t)
			api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-1"), pastMeetingDoc("m-2")}, ""))
			api.Respond(countPath, `{"count": 5, "has_more": false}`)
			api.Respond(countPath, `{"count": 7, "has_more": false}`)
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchPastMeetingParticipants(ctx, stubCallToolRequest(), SearchPastMeetingParticipantsArgs{
				CommitteeUID: "66666666-6666-4666-8666-666666666666", DateFrom: "2026-06-01", CountOnly: true, OrgName: "Red Hat",
			})
		},
	},
	{
		name: "search_committee_members.project_filters",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupCommitteeTest(t)
			api.Respond(resourcesPath, page([]string{fullViewMemberDoc}, ""))
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchCommitteeMembers(ctx, stubCallToolRequest(), SearchCommitteeMembersArgs{
				ProjectUID: "P1", Name: "Pat", OrganizationName: "Example Org",
			})
		},
	},
	{
		name: "search_committee_members.one_group",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupCommitteeTest(t)
			api.Respond(resourcesPath, page([]string{fullViewMemberDoc}, ""))
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchCommitteeMembers(ctx, stubCallToolRequest(), SearchCommitteeMembersArgs{
				CommitteeUID: "66666666-6666-4666-8666-666666666666", PageSize: 25,
			})
		},
	},
	{
		name: "search_meeting_registrants.group_scope",
		setup: func(t *testing.T) *stubLFXAPI {
			api := setupParticipantTest(t)
			api.Respond(resourcesPath, page([]string{fullViewRegistrantDoc}, ""))
			return api
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchMeetingRegistrants(ctx, stubCallToolRequest(), SearchMeetingRegistrantsArgs{
				CommitteeUID: "66666666-6666-4666-8666-666666666666", Name: "Reg",
			})
		},
	},
}

// fullViewRequestLog renders the upstream requests a call made, one per
// line as method, path and sorted query, for the request golden.
func fullViewRequestLog(api *stubLFXAPI) string {
	var lines []string
	for _, r := range api.Requests() {
		keys := make([]string, 0, len(r.Query))
		for k := range r.Query {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			for _, v := range r.Query[k] {
				parts = append(parts, k+"="+v)
			}
		}
		// A request without a query ends at its path, with no trailing space.
		lines = append(lines, strings.TrimSuffix(r.Method+" "+r.Path+" "+strings.Join(parts, "&"), " "))
	}
	return strings.Join(lines, "\n") + "\n"
}
