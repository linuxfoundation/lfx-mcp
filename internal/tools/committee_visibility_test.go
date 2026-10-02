// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Group (committee member) fixtures. UIDs are valid UUIDs because the
// committee service's response validator checks the format; every e-mail is
// under example.test so a test can assert that no address is returned.
const (
	committeeWriterUID  = "11111111-1111-4111-8111-111111111111"
	committeeAuditorUID = "22222222-2222-4222-8222-222222222222"
	committeeViewerUID  = "33333333-3333-4333-8333-333333333333"
	memberChairUID      = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	memberPlainUID      = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

// committeeMemberDoc is one committee_member query-service resource.
func groupMemberDoc(uid, committeeUID, role, email string) string {
	return fmt.Sprintf(`{
	  "type": "committee_member",
	  "id": %q,
	  "data": {
	    "uid": %q,
	    "committee_uid": %q,
	    "committee_name": "Technical Steering Committee",
	    "committee_category": "Technical",
	    "username": "member-%s",
	    "avatar": "https://avatars.example.test/%s.png",
	    "email": %q,
	    "first_name": "Pat",
	    "last_name": "Member",
	    "job_title": "Engineer",
	    "linkedin_profile": "https://example.test/in/pat",
	    "role": {"name": %q, "start_date": "2026-01-01"},
	    "appointed_by": "Community",
	    "status": "Active",
	    "voting": {"status": "Voting Rep", "start_date": "2026-01-01"},
	    "organization": {"id": "org-1", "name": "Example Org", "website": "https://example.test"},
	    "project_uid": "a0941000002wBz4AAE",
	    "project_slug": "cncf",
	    "created_at": "2026-01-01T00:00:00Z",
	    "updated_at": "2026-01-01T00:00:00Z"
	  }
	}`, uid, uid, committeeUID, uid[:8], uid[:8], email, role)
}

// committeeMemberRecord is the committee service's GET member response body.
func committeeMemberRecord(uid, committeeUID, role, email string) string {
	return fmt.Sprintf(`{
	  "uid": %q,
	  "committee_uid": %q,
	  "committee_name": "Technical Steering Committee",
	  "committee_category": "Technical",
	  "username": "member-%s",
	  "email": %q,
	  "first_name": "Pat",
	  "last_name": "Member",
	  "job_title": "Engineer",
	  "role": {"name": %q, "start_date": "2026-01-01"},
	  "appointed_by": "Community",
	  "status": "Active",
	  "voting": {"status": "Voting Rep", "start_date": "2026-01-01"},
	  "organization": {"id": "org-1", "name": "Example Org", "website": "https://example.test"},
	  "created_at": "2026-01-01T00:00:00Z",
	  "updated_at": "2026-01-01T00:00:00Z"
	}`, uid, committeeUID, uid[:8], email, role)
}

// committeeSettingsRecord is the committee service's GET settings body.
func committeeSettingsRecord(uid, memberVisibility string) string {
	return fmt.Sprintf(`{
	  "uid": %q,
	  "business_email_required": false,
	  "member_visibility": %q,
	  "show_meeting_attendees": false,
	  "last_reviewed_by": "reviewer-sub",
	  "writers": [{"email": "writer@example.test", "name": "W", "username": "w"}],
	  "auditors": [{"email": "auditor@example.test", "name": "A", "username": "a"}]
	}`, uid, memberVisibility)
}

// callerMembershipPage is the caller's own committee_member records.
func callerMembershipPage(committeeUIDs ...string) string {
	docs := make([]string, 0, len(committeeUIDs))
	for _, uid := range committeeUIDs {
		docs = append(docs, fmt.Sprintf(`{"type":"committee_member","id":"self-%s","data":{"uid":"self-%s","committee_uid":%q,"username":%q,"email":%q}}`, uid[:8], uid[:8], uid, stubCallerUsername, stubCallerEmail))
	}
	return page(docs, "")
}

// rosterPage is a page with one chair and one plain member per committee.
func rosterPage(committeeUIDs ...string) string {
	var docs []string
	for i, c := range committeeUIDs {
		docs = append(docs,
			groupMemberDoc(fmt.Sprintf("%08d-aaaa-4aaa-8aaa-aaaaaaaaaaaa", i), c, "Chair", fmt.Sprintf("chair%d@example.test", i)),
			groupMemberDoc(fmt.Sprintf("%08d-bbbb-4bbb-8bbb-bbbbbbbbbbbb", i), c, "None", fmt.Sprintf("plain%d@example.test", i)),
		)
	}
	return page(docs, "")
}

// assertNoEmail fails when any example.test address appears in the result.
func assertNoEmail(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(text, "@example.test") {
		t.Errorf("an e-mail address reached a caller the rule does not show it to:\n%s", text)
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// deterministic order for messages
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func TestSearchCommitteeMembers_FullViewIsUnchangedAndMakesNoChecks(t *testing.T) {
	api := setupCommitteeTest(t)
	api.GrantRelations() // would deny everything if asked
	api.Respond(resourcesPath, rosterPage(committeeViewerUID))

	res, out, err := handleSearchCommitteeMembers(fullViewCtx(), stubCallToolRequest(), SearchCommitteeMembersArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(api.RequestsTo(accessCheckPath)) != 0 {
		t.Error("a full-view caller must trigger no access-check")
	}
	if len(api.Requests()) != 1 {
		t.Errorf("a full-view caller must make exactly the search call, got %d requests", len(api.Requests()))
	}
	if len(out.Resources) != 2 || out.Resources[1].Data["email"] != "plain0@example.test" || out.Resources[1].Data["linkedin_profile"] == nil {
		t.Errorf("full view must return the page unchanged: %s", allResultText(t, res))
	}
}

func TestSearchCommitteeMembers_MissingFlagIsNotFullView(t *testing.T) {
	api := setupCommitteeTest(t)
	api.GrantRelations()
	api.Respond(resourcesPath, rosterPage(committeeViewerUID))

	res, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeViewerUID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(api.RequestsTo(accessCheckPath)) != 1 {
		t.Fatal("a context without the flag must be treated as no full view and check relations")
	}
	assertNoEmail(t, allResultText(t, res))
}

func TestSearchCommitteeMembers_ViewerGetsChairsOnly(t *testing.T) {
	api := setupCommitteeTest(t)
	api.GrantRelations() // neither writer nor auditor
	api.Respond(resourcesPath, rosterPage(committeeViewerUID))

	res, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeViewerUID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := allResultText(t, res)
	assertNoEmail(t, text)
	if len(out.Resources) != 1 {
		t.Fatalf("a viewer sees the chair stub only, got %d records:\n%s", len(out.Resources), text)
	}
	got := out.Resources[0].Data
	want := []string{"committee_name", "committee_uid", "first_name", "last_name", "role", "uid"}
	if keys := sortedKeys(got); strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("chair stub keys = %v, want %v", keys, want)
	}
	role, _ := got["role"].(map[string]any)
	if role["name"] != "Chair" || len(role) != 1 {
		t.Errorf("chair stub role must be the name only, got %v", role)
	}
	// Only the writer/auditor pair was asked; no settings or membership call.
	bodies := api.AccessCheckBodies()
	if len(bodies) != 1 || strings.Join(bodies[0], " ") != "committee:"+committeeViewerUID+"#writer committee:"+committeeViewerUID+"#auditor" {
		t.Errorf("unexpected access-check requests %v", bodies)
	}
	// The relation check runs as the caller, with the exchanged LFX token.
	assertExchangedAuth(t, api.RequestsTo(accessCheckPath)[0])
	// The view is decided first and the search reads the chairs only, so a
	// page never spans the members withheld.
	if got := api.RequestsTo(resourcesPath)[0].Query["filters_or"]; strings.Join(got, ",") != "role.name:Chair,role.name:Vice Chair" {
		t.Errorf("the search must be narrowed to the chairs, got filters_or %v", got)
	}
	if len(api.Requests()) != 2 {
		t.Errorf("a viewer costs the search plus one access-check, got %d requests", len(api.Requests()))
	}
}

func TestSearchCommitteeMembers_WriterGetsRecordsUnchanged(t *testing.T) {
	api := setupCommitteeTest(t)
	api.GrantRelations("committee:" + committeeWriterUID + "#writer")
	api.Respond(resourcesPath, rosterPage(committeeWriterUID))

	_, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeWriterUID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Resources) != 2 || out.Resources[1].Data["email"] != "plain0@example.test" || out.Resources[1].Data["linkedin_profile"] == nil {
		t.Errorf("a writer must get the page unchanged: %+v", out.Resources)
	}
	if n := len(api.Requests()); n != 2 {
		t.Errorf("a writer costs the search plus one access-check, got %d requests", n)
	}
}

func TestSearchCommitteeMembers_AuditorMemberBasicProfileGetsMembersTab(t *testing.T) {
	api := setupCommitteeTest(t)
	api.GrantRelations("committee:" + committeeAuditorUID + "#auditor")
	api.Respond(resourcesPath, callerMembershipPage(committeeAuditorUID)) // the caller's memberships, before the search
	api.Respond(resourcesPath, rosterPage(committeeAuditorUID))           // the search
	api.Respond("/committees/"+committeeAuditorUID+"/settings", committeeSettingsRecord(committeeAuditorUID, "basic_profile"))

	_, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeAuditorUID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Resources) != 2 {
		t.Fatalf("the Members tab lists every member, got %d", len(out.Resources))
	}
	plain := out.Resources[1].Data
	want := []string{"committee_category", "committee_name", "committee_uid", "email", "first_name", "last_name", "organization", "role", "uid", "voting"}
	if keys := sortedKeys(plain); strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("Members tab keys = %v, want %v", keys, want)
	}
	org, _ := plain["organization"].(map[string]any)
	if org["name"] != "Example Org" || org["website"] != "https://example.test" || org["id"] != nil {
		t.Errorf("organization must keep name and website only, got %v", org)
	}
	voting, _ := plain["voting"].(map[string]any)
	if voting["status"] != "Voting Rep" || voting["start_date"] != nil {
		t.Errorf("voting must keep status only, got %v", voting)
	}
	// The membership lookup ran as the caller on the username tag.
	reqs := api.RequestsTo(resourcesPath)
	if len(reqs) != 2 || reqs[0].Query.Get("tags_all") != "username:"+stubCallerUsername || reqs[0].Query.Get("type") != "committee_member" {
		t.Errorf("unexpected membership lookup %+v", reqs)
	}
	if got := reqs[1].Query["filters_or"]; len(got) != 0 {
		t.Errorf("a shown member list is read unnarrowed, got filters_or %v", got)
	}
	assertExchangedAuth(t, reqs[0])
	assertExchangedAuth(t, api.RequestsTo("/committees/" + committeeAuditorUID + "/settings")[0])
}

func TestSearchCommitteeMembers_AuditorMemberHiddenGetsChairsOnly(t *testing.T) {
	api := setupCommitteeTest(t)
	api.GrantRelations("committee:" + committeeAuditorUID + "#auditor")
	api.Respond(resourcesPath, callerMembershipPage(committeeAuditorUID))
	api.Respond(resourcesPath, rosterPage(committeeAuditorUID))
	api.Respond("/committees/"+committeeAuditorUID+"/settings", committeeSettingsRecord(committeeAuditorUID, "hidden"))

	res, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeAuditorUID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoEmail(t, allResultText(t, res))
	if len(out.Resources) != 1 || out.Resources[0].Data["email"] != nil {
		t.Errorf("a hidden group shows the chair stub only, got %+v", out.Resources)
	}
}

func TestSearchCommitteeMembers_AuditorNotMemberSkipsSettings(t *testing.T) {
	api := setupCommitteeTest(t)
	api.GrantRelations("committee:" + committeeAuditorUID + "#auditor")
	api.Respond(resourcesPath, callerMembershipPage(committeeViewerUID)) // member elsewhere
	api.Respond(resourcesPath, rosterPage(committeeAuditorUID))

	res, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeAuditorUID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoEmail(t, allResultText(t, res))
	if len(out.Resources) != 1 {
		t.Errorf("an auditor who is not a member sees the chair stub only, got %d", len(out.Resources))
	}
	if n := len(api.RequestsTo("/committees/" + committeeAuditorUID + "/settings")); n != 0 {
		t.Errorf("settings must not be read for a non-member, got %d calls", n)
	}
}

func TestSearchCommitteeMembers_SettingsForbiddenMeansNotShown(t *testing.T) {
	api := setupCommitteeTest(t)
	api.GrantRelations("committee:" + committeeAuditorUID + "#auditor")
	api.Respond(resourcesPath, callerMembershipPage(committeeAuditorUID))
	api.Respond(resourcesPath, rosterPage(committeeAuditorUID))
	api.RespondStatus("/committees/"+committeeAuditorUID+"/settings", http.StatusForbidden, "")

	res, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeAuditorUID})
	if err != nil {
		t.Fatalf("a 403 on settings is a normal answer, got error: %v", err)
	}
	assertNoEmail(t, allResultText(t, res))
	if len(out.Resources) != 1 {
		t.Errorf("expected the chair stub only, got %d", len(out.Resources))
	}
}

func TestSearchCommitteeMembers_ProjectWideMixesViewsPerGroup(t *testing.T) {
	api := setupCommitteeTest(t)
	api.GrantRelations("committee:" + committeeWriterUID + "#writer")
	api.Respond(resourcesPath, projectGroupsPage(committeeWriterUID, committeeViewerUID)) // the project's groups, before the search
	api.Respond(resourcesPath, rosterPage(committeeWriterUID, committeeViewerUID))

	_, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Resources) != 3 {
		t.Fatalf("expected 2 writer records + 1 chair stub, got %d", len(out.Resources))
	}
	for _, r := range out.Resources {
		email, hasEmail := r.Data["email"]
		switch r.Data["committee_uid"] {
		case committeeWriterUID:
			if !hasEmail {
				t.Errorf("writer group record lost its fields: %v", r.Data)
			}
		case committeeViewerUID:
			if hasEmail {
				t.Errorf("viewer group record kept email %v", email)
			}
		}
	}
	if bodies := api.AccessCheckBodies(); len(bodies) != 1 || len(bodies[0]) != 4 {
		t.Errorf("both groups must be checked in one batch, got %v", bodies)
	}
	reqs := api.RequestsTo(resourcesPath)
	if len(reqs) != 2 || reqs[0].Query.Get("type") != "committee" || reqs[0].Query.Get("parent") != "project:P1" {
		t.Fatalf("the project's groups are read first, as the caller: %+v", reqs)
	}
	assertExchangedAuth(t, reqs[0])
	// The search reads the writer group's members and every group's chairs,
	// so a page never spans the members withheld.
	want := "role.name:Chair,role.name:Vice Chair,committee_uid:" + committeeWriterUID
	if got := strings.Join(reqs[1].Query["filters_or"], ","); got != want {
		t.Errorf("filters_or = %q, want %q", got, want)
	}
}

// projectGroupsPage is the committee records of project P1.
func projectGroupsPage(committeeUIDs ...string) string {
	docs := make([]string, 0, len(committeeUIDs))
	for _, uid := range committeeUIDs {
		docs = append(docs, committeeDoc(uid, "Group "+uid[:4], "Technical", "P1"))
	}
	return page(docs, "")
}

func TestSearchCommitteeMembers_ProjectNarrowingAndCoverageNote(t *testing.T) {
	t.Run("a page the view would empty is never read", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, projectGroupsPage(committeeViewerUID))
		api.Respond(resourcesPath, page(nil, ""))
		_, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1"})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(api.RequestsTo(resourcesPath)[1].Query["filters_or"], ","); got != "role.name:Chair,role.name:Vice Chair" {
			t.Errorf("a project whose lists are not shown is read as its chairs, got %q", got)
		}
		// No group has a chair: the warning is the generic one, never the
		// roster-coverage note blaming filters the caller did not pass.
		if len(out.Warnings) != 1 || strings.Contains(out.Warnings[0], "name is a typeahead") {
			t.Errorf("expected the generic warning, got %v", out.Warnings)
		}
		if n := len(api.RequestsTo(countPath)); n != 0 {
			t.Errorf("no coverage count for a narrowed page, got %d", n)
		}
	})
	t.Run("no visible group keeps the coverage note", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, page(nil, ""))
		api.Respond(resourcesPath, page(nil, ""))
		api.Respond(countPath, `{"count": 0, "has_more": false}`)
		_, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1"})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "no committees are onboarded") {
			t.Errorf("expected the no-groups coverage note, got %v", out.Warnings)
		}
	})
	t.Run("a stale project tag is decided after the query", func(t *testing.T) {
		// A member record tagged with the project whose group is no longer
		// among the project's groups: its view is decided after the query.
		api := setupCommitteeTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, projectGroupsPage(committeeWriterUID))
		api.Respond(resourcesPath, rosterPage(committeeViewerUID))
		_, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1"})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Resources) != 1 || out.Resources[0].Data["email"] != nil {
			t.Errorf("the stale group's records follow its own view (chairs only): %+v", out.Resources)
		}
		if bodies := api.AccessCheckBodies(); len(bodies) != 2 {
			t.Errorf("the stale group is checked after the query, got %v", bodies)
		}
	})
	t.Run("group lookup failure fails closed", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.RespondStatus(resourcesPath, http.StatusBadGateway, "")
		_, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1"})
		if err == nil || err.Error() != peopleVisibilityUnavailableMessage || len(out.Resources) != 0 {
			t.Fatalf("expected the unavailable message and no records, got %v", err)
		}
	})
}

func TestSearchCommitteeMembers_FailsClosed(t *testing.T) {
	cases := []struct {
		name  string
		uid   string
		setup func(api *stubLFXAPI)
	}{
		{"access-check 503", committeeViewerUID, func(api *stubLFXAPI) {
			api.FailAccessCheck(http.StatusServiceUnavailable)
			api.Respond(resourcesPath, rosterPage(committeeViewerUID))
		}},
		{"settings 500", committeeAuditorUID, func(api *stubLFXAPI) {
			api.GrantRelations("committee:" + committeeAuditorUID + "#auditor")
			api.Respond(resourcesPath, callerMembershipPage(committeeAuditorUID))
			api.RespondStatus("/committees/"+committeeAuditorUID+"/settings", http.StatusInternalServerError, `{"message":"boom"}`)
			api.Respond(resourcesPath, rosterPage(committeeAuditorUID))
		}},
		{"membership lookup error", committeeAuditorUID, func(api *stubLFXAPI) {
			api.GrantRelations("committee:" + committeeAuditorUID + "#auditor")
			api.RespondStatus(resourcesPath, http.StatusBadGateway, "")
			api.Respond(resourcesPath, rosterPage(committeeAuditorUID))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := setupCommitteeTest(t)
			tc.setup(api)
			res, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: tc.uid})
			if err == nil {
				t.Fatalf("expected a tool error, got result %s", allResultText(t, res))
			}
			if err.Error() != peopleVisibilityUnavailableMessage {
				t.Errorf("error = %q", err.Error())
			}
			if len(out.Resources) != 0 {
				t.Error("no records may travel with the failure")
			}
			assertNoEmail(t, err.Error())
		})
	}
}

func TestSearchCommitteeMembers_RefusesPersonFiltersWithoutShownList(t *testing.T) {
	t.Run("no scope", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations("committee:" + committeeWriterUID + "#writer")
		_, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{Name: "Pat"})
		if err == nil || !strings.Contains(err.Error(), "committee_uid") {
			t.Fatalf("expected a refusal naming committee_uid, got %v", err)
		}
		if len(api.Requests()) != 0 {
			t.Error("the refusal must happen before any upstream call")
		}
	})
	memberQueries := func(api *stubLFXAPI) int {
		n := 0
		for _, r := range api.RequestsTo(resourcesPath) {
			if r.Query.Get("type") == "committee_member" {
				n++
			}
		}
		return n
	}
	t.Run("project with a group the caller does not manage", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations("committee:" + committeeWriterUID + "#writer")
		api.Respond(resourcesPath, projectGroupsPage(committeeWriterUID, committeeViewerUID))
		_, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1", Name: "Pat"})
		if err == nil || !strings.Contains(err.Error(), "committee_uid") {
			t.Fatalf("expected a refusal naming committee_uid, got %v", err)
		}
		if n := memberQueries(api); n != 0 {
			t.Error("the search must not run when the filter is refused")
		}
	})
	t.Run("project whose groups the caller all manages", func(t *testing.T) {
		// Writer on each group directly, without any project role.
		api := setupCommitteeTest(t)
		api.GrantRelations("committee:" + committeeWriterUID + "#writer")
		api.Respond(resourcesPath, projectGroupsPage(committeeWriterUID))
		api.Respond(resourcesPath, rosterPage(committeeWriterUID))
		_, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1", Name: "Pat", OrganizationName: "Example Org"})
		if err != nil {
			t.Fatalf("a caller managing every group may filter by name and organization: %v", err)
		}
		if len(out.Resources) != 2 || out.Resources[1].Data["email"] != "plain0@example.test" {
			t.Errorf("the member list is returned unchanged: %+v", out.Resources)
		}
		// Narrowed to the groups checked, so a member record whose project
		// tag is stale is never matched by the filter.
		if got := strings.Join(api.RequestsTo(resourcesPath)[1].Query["filters_or"], ","); got != "committee_uid:"+committeeWriterUID {
			t.Errorf("filters_or = %q", got)
		}
	})
	t.Run("project with no visible group", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, page(nil, ""))
		_, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1", OrganizationName: "Example Org"})
		if err == nil || !strings.Contains(err.Error(), "member lists") || memberQueries(api) != 0 {
			t.Fatalf("expected the refusal and no member query, got %v", err)
		}
	})
	t.Run("relation check fails closed", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.FailAccessCheck(http.StatusServiceUnavailable)
		api.Respond(resourcesPath, projectGroupsPage(committeeWriterUID))
		_, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1", OrganizationName: "Example Org"})
		if err == nil || err.Error() != peopleVisibilityUnavailableMessage || memberQueries(api) != 0 {
			t.Fatalf("expected the unavailable message and no search, got %v", err)
		}
	})
	t.Run("group mode names group_uid", func(t *testing.T) {
		setupCommitteeTest(t)
		_, _, err := handleSearchCommitteeMembersGroupMode(context.Background(), stubCallToolRequest(), SearchGroupMembersArgs{OrganizationName: "Example Org"})
		if err == nil || !strings.Contains(err.Error(), "group_uid") || strings.Contains(err.Error(), "committee") {
			t.Fatalf("expected a group-mode refusal, got %v", err)
		}
	})
	t.Run("chairs-only group", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations()
		_, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeViewerUID, Name: "Pat"})
		if err == nil || !strings.Contains(err.Error(), "committee_uid") {
			t.Fatalf("expected a refusal, got %v", err)
		}
		if n := len(api.RequestsTo(resourcesPath)); n != 0 {
			t.Error("the search must not run when the filter is refused")
		}
	})
	t.Run("chairs-only group: organization_name is refused before the search", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations()
		_, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeViewerUID, OrganizationName: "Example Org"})
		if err == nil || !strings.Contains(err.Error(), "member list") || len(api.RequestsTo(resourcesPath)) != 0 {
			t.Fatalf("expected the refusal and no member query, got %v (%d queries)", err, len(api.RequestsTo(resourcesPath)))
		}
	})
	t.Run("roster-shown group: organization_name passes, name does not", func(t *testing.T) {
		// An auditor who is a member of a basic_profile group sees the
		// Members tab, which shows organisations but not usernames; name
		// matches the username too, so it stays with writers.
		api := setupCommitteeTest(t)
		api.GrantRelations("committee:" + committeeAuditorUID + "#auditor")
		api.Respond(resourcesPath, callerMembershipPage(committeeAuditorUID)) // membership, during the up-front check
		api.Respond("/committees/"+committeeAuditorUID+"/settings", committeeSettingsRecord(committeeAuditorUID, "basic_profile"))
		_, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeAuditorUID, Name: "Pat"})
		if err == nil || !strings.Contains(err.Error(), "manage") || len(api.RequestsTo(resourcesPath)) != 1 {
			t.Fatalf("name must be refused for a roster-shown group before the search, got %v", err)
		}
		api = setupCommitteeTest(t)
		api.GrantRelations("committee:" + committeeAuditorUID + "#auditor")
		api.Respond(resourcesPath, callerMembershipPage(committeeAuditorUID))
		api.Respond("/committees/"+committeeAuditorUID+"/settings", committeeSettingsRecord(committeeAuditorUID, "basic_profile"))
		api.Respond(resourcesPath, rosterPage(committeeAuditorUID)) // the search
		_, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeAuditorUID, OrganizationName: "Example Org"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out.Resources) != 2 || out.Resources[1].Data["email"] == nil || out.Resources[1].Data["job_title"] != nil {
			t.Errorf("expected the Members-tab projection of the whole page, got %+v", out.Resources)
		}
		if n := len(api.RequestsTo(accessCheckPath)); n != 1 {
			t.Errorf("the up-front view must be reused for the page, got %d checks", n)
		}
	})
	t.Run("writer group passes and checks once", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations("committee:" + committeeWriterUID + "#writer")
		api.Respond(resourcesPath, rosterPage(committeeWriterUID))
		_, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeWriterUID, Name: "Pat"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out.Resources) != 2 || len(api.RequestsTo(accessCheckPath)) != 1 {
			t.Errorf("expected the full page after one check, got %d records, %d checks", len(out.Resources), len(api.RequestsTo(accessCheckPath)))
		}
	})
	t.Run("predicate failure before the search fails closed", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.FailAccessCheck(http.StatusBadGateway)
		_, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: committeeWriterUID, Name: "Pat"})
		if err == nil || err.Error() != peopleVisibilityUnavailableMessage {
			t.Fatalf("expected the unavailable error, got %v", err)
		}
	})
}

func TestGetCommitteeMember_Views(t *testing.T) {
	memberPath := "/committees/" + committeeViewerUID + "/members/"
	t.Run("full view unchanged, no checks", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations()
		api.Respond(memberPath+memberPlainUID, committeeMemberRecord(memberPlainUID, committeeViewerUID, "None", "plain@example.test"))
		res, out, err := handleGetCommitteeMember(fullViewCtx(), stubCallToolRequest(), GetCommitteeMemberArgs{CommitteeUID: committeeViewerUID, MemberUID: memberPlainUID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out == nil || out.Email == nil || *out.Email != "plain@example.test" || out.JobTitle == nil {
			t.Errorf("full view must return the record unchanged: %s", allResultText(t, res))
		}
		if len(api.RequestsTo(accessCheckPath)) != 0 {
			t.Error("no access-check for full view")
		}
	})
	t.Run("writer unchanged", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations("committee:" + committeeViewerUID + "#writer")
		api.Respond(memberPath+memberPlainUID, committeeMemberRecord(memberPlainUID, committeeViewerUID, "None", "plain@example.test"))
		_, out, err := handleGetCommitteeMember(context.Background(), stubCallToolRequest(), GetCommitteeMemberArgs{CommitteeUID: committeeViewerUID, MemberUID: memberPlainUID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Email == nil || out.JobTitle == nil {
			t.Error("a writer gets the record unchanged")
		}
	})
	t.Run("viewer gets the chair stub", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations()
		api.Respond(memberPath+memberChairUID, committeeMemberRecord(memberChairUID, committeeViewerUID, "Vice Chair", "chair@example.test"))
		res, out, err := handleGetCommitteeMember(context.Background(), stubCallToolRequest(), GetCommitteeMemberArgs{CommitteeUID: committeeViewerUID, MemberUID: memberChairUID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		text := allResultText(t, res)
		assertNoEmail(t, text)
		// The typed record marshals with its Go field names; reduced fields
		// are null or blank, never populated.
		var got map[string]any
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"Email", "Username", "JobTitle", "Voting", "Organization", "CommitteeCategory", "CreatedAt", "UpdatedAt"} {
			if got[key] != nil {
				t.Errorf("%s must be null in the chair stub, got %v", key, got[key])
			}
		}
		for _, key := range []string{"AppointedBy", "Status"} {
			if got[key] != "" {
				t.Errorf("%s must be blank in the chair stub, got %v", key, got[key])
			}
		}
		for _, key := range []string{"UID", "CommitteeUID", "CommitteeName", "FirstName", "LastName"} {
			if got[key] == nil || got[key] == "" {
				t.Errorf("%s must stay in the chair stub", key)
			}
		}
		if out.Role == nil || out.Role.Name != "Vice Chair" || out.Role.StartDate != nil {
			t.Errorf("role must keep the name only: %+v", out.Role)
		}
	})
	t.Run("viewer: plain member is not visible", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations()
		api.Respond(memberPath+memberPlainUID, committeeMemberRecord(memberPlainUID, committeeViewerUID, "None", "plain@example.test"))
		_, out, err := handleGetCommitteeMember(context.Background(), stubCallToolRequest(), GetCommitteeMemberArgs{CommitteeUID: committeeViewerUID, MemberUID: memberPlainUID})
		if err == nil {
			t.Fatal("expected an error")
		}
		if out != nil {
			t.Error("no record may travel with the refusal")
		}
		assertNoEmail(t, err.Error())
		// Byte-identical to the committee service answering 404 for a UID
		// that does not exist, so the two cannot be told apart.
		api.RespondStatus(memberPath+"missing", http.StatusNotFound, `{"code":"404","message":"member not found"}`)
		_, _, notFound := handleGetCommitteeMember(context.Background(), stubCallToolRequest(), GetCommitteeMemberArgs{CommitteeUID: committeeViewerUID, MemberUID: "missing"})
		if notFound == nil || notFound.Error() != err.Error() {
			t.Fatalf("dropped record text %q must equal the 404 text %v", err.Error(), notFound)
		}
	})
	t.Run("group mode gives the same text", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations()
		api.Respond(memberPath+memberPlainUID, committeeMemberRecord(memberPlainUID, committeeViewerUID, "None", "plain@example.test"))
		_, _, err := handleGetCommitteeMemberGroupMode(context.Background(), stubCallToolRequest(), GetGroupMemberArgs{GroupUID: committeeViewerUID, MemberUID: memberPlainUID})
		if err == nil || err.Error() != serviceLookupNotVisibleMessage(getCommitteeMemberOp) {
			t.Fatalf("expected the shared text, got %v", err)
		}
	})
	t.Run("auditor member basic_profile keeps the Members tab fields", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.GrantRelations("committee:" + committeeViewerUID + "#auditor")
		api.Respond(memberPath+memberPlainUID, committeeMemberRecord(memberPlainUID, committeeViewerUID, "None", "plain@example.test"))
		api.Respond(resourcesPath, callerMembershipPage(committeeViewerUID))
		api.Respond("/committees/"+committeeViewerUID+"/settings", committeeSettingsRecord(committeeViewerUID, "basic_profile"))
		res, out, err := handleGetCommitteeMember(context.Background(), stubCallToolRequest(), GetCommitteeMemberArgs{CommitteeUID: committeeViewerUID, MemberUID: memberPlainUID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(allResultText(t, res)), &got); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"Username", "JobTitle", "CreatedAt", "UpdatedAt"} {
			if got[key] != nil {
				t.Errorf("%s must be null on the Members tab, got %v", key, got[key])
			}
		}
		if got["Email"] != "plain@example.test" || got["CommitteeCategory"] != "Technical" {
			t.Errorf("the Members tab keeps email and category: %v %v", got["Email"], got["CommitteeCategory"])
		}
		if out.Organization == nil || out.Organization.ID != nil || out.Organization.Name == nil || out.Voting == nil || out.Voting.StartDate != nil || out.Voting.Status != "Voting Rep" {
			t.Errorf("nested fields not reduced: %+v %+v", out.Organization, out.Voting)
		}
		// The record is built from an allowlist: exactly these are populated.
		var populated []string
		for _, key := range sortedKeys(got) {
			if got[key] != nil && got[key] != "" {
				populated = append(populated, key)
			}
		}
		if want := "CommitteeCategory,CommitteeName,CommitteeUID,Email,FirstName,LastName,Organization,Role,UID,Voting"; strings.Join(populated, ",") != want {
			t.Errorf("populated fields = %v, want %s", populated, want)
		}
	})
	t.Run("predicate failure fails closed", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.FailAccessCheck(http.StatusServiceUnavailable)
		api.Respond(memberPath+memberPlainUID, committeeMemberRecord(memberPlainUID, committeeViewerUID, "Chair", "chair@example.test"))
		_, out, err := handleGetCommitteeMember(context.Background(), stubCallToolRequest(), GetCommitteeMemberArgs{CommitteeUID: committeeViewerUID, MemberUID: memberPlainUID})
		if err == nil || err.Error() != peopleVisibilityUnavailableMessage || out != nil {
			t.Fatalf("expected the unavailable error and no record, got %v %v", err, out)
		}
	})
}

func TestGetCommittee_SettingsPeopleListsFollowFullView(t *testing.T) {
	for _, full := range []bool{true, false} {
		t.Run(fmt.Sprintf("full view %v", full), func(t *testing.T) {
			api := setupCommitteeTest(t)
			api.Respond("/committees/"+committeeViewerUID, `{"uid": "`+committeeViewerUID+`"}`)
			api.Respond("/committees/"+committeeViewerUID+"/settings", committeeSettingsRecord(committeeViewerUID, "basic_profile"))
			ctx := context.Background()
			if full {
				ctx = fullViewCtx()
			}
			res, out, err := handleGetCommittee(ctx, stubCallToolRequest(), GetCommitteeArgs{UID: committeeViewerUID})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			text := allResultText(t, res)
			if full {
				if len(out.Settings.Writers) != 1 || len(out.Settings.Auditors) != 1 || out.Settings.LastReviewedBy == nil {
					t.Errorf("full view must keep writers, auditors and last_reviewed_by: %s", text)
				}
				return
			}
			assertNoEmail(t, text)
			if out.Settings == nil || out.Settings.Writers != nil || out.Settings.Auditors != nil || out.Settings.LastReviewedBy != nil {
				t.Errorf("writers, auditors and last_reviewed_by must be absent: %s", text)
			}
			if out.Settings.MemberVisibility != "basic_profile" {
				t.Error("the rest of the settings must stay")
			}
			if len(api.RequestsTo(accessCheckPath)) != 0 {
				t.Error("get_committee needs no relation check")
			}
		})
	}
}

func TestKeepFields_NestedPaths(t *testing.T) {
	data := map[string]any{
		"a":    1,
		"org":  map[string]any{"id": "x", "name": "n", "website": "w"},
		"role": "not-an-object",
	}
	got := keepFields(data, "a", "missing", "org.name", "org.website", "org.absent", "role.name")
	want := map[string]any{"a": 1, "org": map[string]any{"name": "n", "website": "w"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("keepFields = %v, want %v", got, want)
	}
	if _, shared := got["org"].(map[string]any); !shared {
		t.Fatal("nested object expected")
	}
	// The source must be untouched.
	if len(data["org"].(map[string]any)) != 3 {
		t.Error("keepFields must not mutate its input")
	}
}

// TestPeopleToolsDescribeTheVisibilityRule pins the one clause each people
// search tool carries about what a caller gets, in both terminology modes,
// in user terms, with no figures and no mention of other products.
func TestPeopleToolsDescribeTheVisibilityRule(t *testing.T) {
	for _, tc := range []struct {
		toolName string
		register func(*mcp.Server)
		want     string
	}{
		{"search_committee_members", func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, false) },
			"You get the member list LFX Self Serve shows you: the list for committees you manage or that share their member list with you, otherwise their chairs."},
		{"search_group_members", func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, true) },
			"You get the member list LFX Self Serve shows you: the list for groups you manage or that share their member list with you, otherwise their chairs."},
		{"search_meeting_registrants", func(s *mcp.Server) { RegisterSearchMeetingRegistrants(s, false) },
			"You get, per meeting_id, the registrant list LFX Self Serve shows you: the full list for meetings you organize, the guest list without e-mail for meetings you are registered for, nothing for others."},
		{"search_meeting_registrants", func(s *mcp.Server) { RegisterSearchMeetingRegistrants(s, true) },
			"You get, per meeting_id, the registrant list LFX Self Serve shows you: the full list for meetings you organize, the guest list without e-mail for meetings you are registered for, nothing for others."},
		{"search_past_meeting_participants", func(s *mcp.Server) { RegisterSearchPastMeetingParticipants(s, false) },
			"Per past_meeting_id or date range you get what LFX Self Serve shows you: the full list for past meetings you organize; hosts' names and your own record for public unrestricted ones and ones you hosted, were invited to, attended or whose committee you belong to; else only yours."},
		{"search_past_meeting_participants", func(s *mcp.Server) { RegisterSearchPastMeetingParticipants(s, true) },
			"Per past_meeting_id or date range you get what LFX Self Serve shows you: the full list for past meetings you organize; hosts' names and your own record for public unrestricted ones and ones you hosted, were invited to, attended or whose group you belong to; else only yours."},
	} {
		t.Run(tc.toolName, func(t *testing.T) {
			tool := listRegisteredTool(t, tc.toolName, tc.register)
			if !strings.Contains(tool.Description, tc.want) {
				t.Errorf("%s description missing %q", tc.toolName, tc.want)
			}
			if n := len(tool.Description); n > schemaDescriptionBudget {
				t.Errorf("description is %d bytes, over the %d budget", n, schemaDescriptionBudget)
			}
			for _, banned := range []string{"Insights", "%", "staff"} {
				if strings.Contains(tool.Description, banned) {
					t.Errorf("description must not contain %q", banned)
				}
			}
		})
	}
}

func TestSearchCommitteeMembers_RefusesSearchesItCannotNarrow(t *testing.T) {
	t.Run("no scope reads nothing", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.Respond(resourcesPath, rosterPage(committeeViewerUID))
		_, out, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{PageSize: 1})
		if err == nil || !strings.Contains(err.Error(), "set committee_uid or project_uid") {
			t.Fatalf("expected the scope refusal, got %v", err)
		}
		if len(api.Requests()) != 0 || len(out.Resources) != 0 {
			t.Errorf("a refused search must make no upstream call, got %d", len(api.Requests()))
		}
	})
	t.Run("project beyond the filter cap reads no members", func(t *testing.T) {
		api := setupCommitteeTest(t)
		uids := make([]string, peopleFilterChunk+1)
		docs := make([]string, len(uids))
		var relations []string
		for i := range uids {
			uids[i] = fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
			docs[i] = fmt.Sprintf(`{"type":"committee","id":%q,"data":{"uid":%q}}`, uids[i], uids[i])
			relations = append(relations, "committee:"+uids[i]+"#writer")
		}
		api.Respond(resourcesPath, page(docs, ""))
		api.GrantRelations(relations...)
		api.Respond(resourcesPath, rosterPage(committeeViewerUID))
		_, _, err := handleSearchCommitteeMembers(context.Background(), stubCallToolRequest(), SearchCommitteeMembersArgs{ProjectUID: "P1", PageSize: 1})
		if err == nil || !strings.Contains(err.Error(), "set committee_uid") {
			t.Fatalf("expected the cap refusal, got %v", err)
		}
		for _, r := range api.RequestsTo(resourcesPath) {
			if r.Query.Get("type") == committeeMemberResourceType {
				t.Errorf("no member query may run past the cap: %v", r.Query)
			}
		}
	})
}
