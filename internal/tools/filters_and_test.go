// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setupCommitteeTest points the committee tools at a stub LFX API.
func setupCommitteeTest(t *testing.T) *stubLFXAPI {
	t.Helper()
	api := newStubLFXAPI(t)
	prev := committeeConfig
	SetCommitteeConfig(&CommitteeConfig{Clients: api.Clients})
	t.Cleanup(func() { committeeConfig = prev })
	return api
}

// setupMailingListTest points the mailing list tools at a stub LFX API.
func setupMailingListTest(t *testing.T) *stubLFXAPI {
	t.Helper()
	api := newStubLFXAPI(t)
	prev := mailingListConfig
	SetMailingListConfig(&MailingListConfig{Clients: api.Clients})
	t.Cleanup(func() { mailingListConfig = prev })
	return api
}

// assertTagsAllQuery checks that the recorded query-resources request carries
// the given filters in tags_all (AND) and nothing in tags (OR).
func assertTagsAllQuery(t *testing.T, r stubAPIRequest, wantTagsAll []string) {
	t.Helper()
	if r.Method != http.MethodGet || r.Path != resourcesPath {
		t.Fatalf("expected GET %s, got %s %s", resourcesPath, r.Method, r.Path)
	}
	assertExchangedAuth(t, r)
	if got := r.Query["tags_all"]; strings.Join(got, "|") != strings.Join(wantTagsAll, "|") {
		t.Errorf("query tags_all: want %v, got %v", wantTagsAll, got)
	}
	if got, ok := r.Query["tags"]; ok {
		t.Errorf("query must not carry tags (OR); got %v", got)
	}
}

func TestSearchCommitteeMembers_FiltersCombineWithAND(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page(nil, ""))

	res, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{
		CommitteeUID: "C1",
		ProjectUID:   "P1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	assertTagsAllQuery(t, api.Requests()[0], []string{"committee_uid:C1", "project_uid:P1"})
}

func TestSearchGroupMembers_ForwardsFiltersAsAND(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page(nil, ""))

	res, _, err := handleSearchCommitteeMembersGroupMode(context.Background(), stubCallToolRequest(), SearchGroupMembersArgs{
		GroupUID:   "C1",
		ProjectUID: "P1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	assertTagsAllQuery(t, api.Requests()[0], []string{"committee_uid:C1", "project_uid:P1"})
}

func TestSearchPastMeetings_FiltersCombineWithAND(t *testing.T) {
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page(nil, ""))

	res, _, err := handleSearchPastMeetings(context.Background(), stubCallToolRequest(), SearchPastMeetingsArgs{
		CommitteeUID: "C1",
		MeetingID:    "M1",
		ProjectUID:   "P1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	r := api.LastRequest()
	assertTagsAllQuery(t, r, []string{"committee_uid:C1", "meeting_id:M1"})
	if got := r.Query.Get("parent"); got != "project:P1" {
		t.Errorf("query parent: want project:P1, got %q", got)
	}
}

func TestSearchMailingListMembers_FiltersCombineWithAND(t *testing.T) {
	api := setupMailingListTest(t)
	api.Respond(resourcesPath, page(nil, ""))

	res, _, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{
		MailingListID: "145670",
		ProjectUID:    "P1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	assertTagsAllQuery(t, api.LastRequest(), []string{"mailing_list_uid:145670", "project_uid:P1"})
}

// TestNarrowingSearchDescriptionsStateANDFilters pins the clause that tells
// callers two filters intersect rather than union.
func TestNarrowingSearchDescriptionsStateANDFilters(t *testing.T) {
	const want = "Filters combine with AND: a record must match every filter given."
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
	}{
		{"search_committee_members", func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, false) }},
		{"search_group_members", func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, true) }},
		{"search_past_meetings", func(s *mcp.Server) { RegisterSearchPastMeetings(s, false) }},
		{"search_past_meetings", func(s *mcp.Server) { RegisterSearchPastMeetings(s, true) }},
		{"search_mailing_list_members", RegisterSearchMailingListMembers},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := listRegisteredTool(t, tc.name, tc.register)
			if !strings.Contains(tool.Description, want) {
				t.Errorf("%s description missing %q", tc.name, want)
			}
		})
	}
}

// committeeMemberDoc builds a minimal committee_member query-resources
// document with fabricated values.
func committeeMemberDoc(uid, orgName string) string {
	return `{"type": "committee_member", "id": "` + uid + `", "data": {"uid": "` + uid + `", "username": "test-user", "organization": {"name": "` + orgName + `"}, "voting": {"status": "Voting Rep"}}}`
}

func TestSearchCommitteeMembers_OrganizationNameFilter(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page(nil, ""))

	res, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{
		ProjectUID:       "P1",
		OrganizationName: "Oracle America, Inc.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	reqs := api.Requests()
	assertTagsAllQuery(t, reqs[0], []string{"project_uid:P1", "organization_name:Oracle America, Inc."})
}

func TestSearchGroupMembers_ForwardsOrganizationName(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page([]string{committeeMemberDoc("m1", "Oracle America, Inc.")}, ""))

	res, _, err := handleSearchCommitteeMembersGroupMode(context.Background(), stubCallToolRequest(), SearchGroupMembersArgs{
		GroupUID:         "C1",
		OrganizationName: "Oracle America, Inc.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	assertTagsAllQuery(t, api.LastRequest(), []string{"committee_uid:C1", "organization_name:Oracle America, Inc."})
}

// assertRosterCoverageCount checks that the second recorded request is the
// committee count for the project.
func assertRosterCoverageCount(t *testing.T, api *stubLFXAPI, projectUID string) {
	t.Helper()
	reqs := api.Requests()
	if len(reqs) != 2 {
		t.Fatalf("expected search then count, got %d requests", len(reqs))
	}
	r := reqs[1]
	if r.Method != http.MethodGet || r.Path != countPath {
		t.Fatalf("expected GET %s, got %s %s", countPath, r.Method, r.Path)
	}
	assertExchangedAuth(t, r)
	if got := r.Query.Get("type"); got != committeeResourceType {
		t.Errorf("count type: want %s, got %q", committeeResourceType, got)
	}
	if got := r.Query.Get("parent"); got != "project:"+projectUID {
		t.Errorf("count parent: want project:%s, got %q", projectUID, got)
	}
}

func TestSearchCommitteeMembers_EmptyWithProjectNotesNoCommittees(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	api.Respond(countPath, `{"count": 0, "has_more": false}`)

	res, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{
		ProjectUID: "P1",
		Name:       "Test User",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	assertRosterCoverageCount(t, api, "P1")
	text := allResultText(t, res)
	none := fmt.Sprintf(rosterCoverageNoneNote, "committees")
	if !strings.Contains(text, none) {
		t.Errorf("expected the no-committees note, got %q", text)
	}
	if strings.Contains(text, "matched these filters") {
		t.Error("must not also carry the no-match note")
	}
	// The roster note is the only warning: it replaces the generic one.
	assertSingleBlockWarnings(t, res, out.Warnings, []string{none})
}

// assertSingleBlockWarnings checks that a search result is one JSON text
// block whose warnings key equals both the structured output's warnings and
// want.
func assertSingleBlockWarnings(t *testing.T, res *mcp.CallToolResult, structured, want []string) {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("expected exactly one content block, got %d", len(res.Content))
	}
	if !reflect.DeepEqual(structured, want) {
		t.Errorf("structured warnings: want %q, got %q", want, structured)
	}
	var text struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &text); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if !reflect.DeepEqual(text.Warnings, structured) {
		t.Errorf("text warnings %q differ from structured warnings %q", text.Warnings, structured)
	}
}

func TestSearchCommitteeMembers_EmptyWithProjectNotesNoMatch(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	api.Respond(countPath, `{"count": 3, "has_more": false}`)

	res, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{
		ProjectUID:       "P1",
		OrganizationName: "Oracle",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	assertRosterCoverageCount(t, api, "P1")
	text := allResultText(t, res)
	noMatch := fmt.Sprintf(rosterCoverageNoMatchNote, "committees")
	if !strings.Contains(text, noMatch) {
		t.Errorf("expected the no-match note, got %q", text)
	}
	if strings.Contains(text, "none are visible to you") {
		t.Error("must not also carry the no-committees note")
	}
	// The committee count is access-filtered, so the note replacing the
	// generic warning must keep its visibility caveat and never claim that
	// no member record matched at all.
	for _, want := range []string{"no member record visible to you matched", "results cover only records you can view"} {
		if !strings.Contains(noMatch, want) {
			t.Errorf("no-match note must say %q: %q", want, noMatch)
		}
	}
	assertSingleBlockWarnings(t, res, out.Warnings, []string{noMatch})
}

func TestSearchGroupMembers_RosterNoteSaysGroups(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	api.Respond(countPath, `{"count": 3, "has_more": false}`)

	res, out, err := handleSearchCommitteeMembersGroupMode(context.Background(), stubCallToolRequest(), SearchGroupMembersArgs{ProjectUID: "P1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	assertSingleBlockWarnings(t, res, out.Warnings, []string{fmt.Sprintf(rosterCoverageNoMatchNote, "groups")})
	if strings.Contains(allResultText(t, res), "committee") {
		t.Errorf("group-mode warnings must not say committee: %q", out.Warnings)
	}
}

func TestSearchCommitteeMembers_EmptyWithoutProjectHasNoNote(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	api.Respond(countPath, `{"count": 0, "has_more": false}`)

	for _, args := range []SearchCommitteeMembersArgs{
		{Name: "Test User"},
		{CommitteeUID: "C1"},
		{OrganizationName: "Oracle America, Inc."},
	} {
		api.Respond(resourcesPath, page(nil, ""))
		before := len(api.Requests())
		res, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected error result: %s", allResultText(t, res))
		}
		if got := len(api.Requests()) - before; got != 1 {
			t.Errorf("%+v: expected exactly one API request, got %d", args, got)
		}
		if text := allResultText(t, res); strings.Contains(text, "Roster coverage") {
			t.Errorf("%+v: no note without project_uid, got %q", args, text)
		}
	}
	if n := len(api.RequestsTo(countPath)); n != 0 {
		t.Errorf("no count call without project_uid, got %d", n)
	}
}

func TestSearchCommitteeMembers_NonEmptyWithProjectHasNoNote(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page([]string{committeeMemberDoc("m1", "Oracle America, Inc.")}, ""))
	api.Respond(countPath, `{"count": 0, "has_more": false}`)

	res, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	if n := len(api.Requests()); n != 1 {
		t.Errorf("expected exactly one API request, got %d", n)
	}
	if text := allResultText(t, res); strings.Contains(text, "Roster coverage") {
		t.Errorf("no note on a non-empty result, got %q", text)
	}
}

func TestSearchCommitteeMembers_EmptyPageWithTokenHasNoNote(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page(nil, "next"))
	api.Respond(countPath, `{"count": 0, "has_more": false}`)

	res, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := len(api.Requests()); n != 1 {
		t.Errorf("an access-filtered page is not a genuinely empty result; expected one request, got %d", n)
	}
	if text := allResultText(t, res); strings.Contains(text, "Roster coverage") {
		t.Errorf("no note on an access-filtered page, got %q", text)
	}
}

func TestSearchCommitteeMembers_EmptyContinuationPageHasNoNote(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	api.Respond(countPath, `{"count": 0, "has_more": false}`)

	res, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1", PageToken: "next"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := len(api.Requests()); n != 1 {
		t.Errorf("a continuation page is not a genuinely empty result; expected one request, got %d", n)
	}
	if text := allResultText(t, res); strings.Contains(text, "Roster coverage") {
		t.Errorf("no note on a continuation page, got %q", text)
	}
}

func TestSearchCommitteeMembers_CountFailureDropsNote(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	// countPath unscripted: the stub answers 404.

	res, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("a failed coverage count must not fail the search: %s", allResultText(t, res))
	}
	// Without the roster note the generic empty-page warning stands.
	assertSingleBlockWarnings(t, res, out.Warnings, searchWarnings("committee members", 0, 10, false, false))
	if n := len(api.RequestsTo(countPath)); n != 1 {
		t.Errorf("expected one count attempt, got %d", n)
	}
	if text := allResultText(t, res); strings.Contains(text, "Roster coverage") {
		t.Errorf("no note when the count fails, got %q", text)
	}
}

// TestSearchCommitteeMembersDescriptionsAdvertiseOrganizationName pins the
// organization_name clause and the roster-coverage sentence on both modes.
func TestSearchCommitteeMembersDescriptionsAdvertiseOrganizationName(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
	}{
		{"search_committee_members", func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, false) }},
		{"search_group_members", func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := listRegisteredTool(t, tc.name, tc.register)
			for _, want := range []string{
				"organization_name keeps one organization's members and must equal the stored spelling (copy it from a roster row or get_org_committee_seats).",
				"With project_uid set, an empty result carries a roster-coverage note saying whether the project has any committee onboarded into LFX v2; an empty result never proves that a person or organization holds no seat.",
			} {
				if !strings.Contains(tool.Description, want) {
					t.Errorf("%s description missing %q", tc.name, want)
				}
			}
		})
	}
}
