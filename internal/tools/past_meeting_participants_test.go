// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resourcesPath is the query-service search route.
const resourcesPath = "/query/resources"

// Contract fixtures. Field names and types follow lfx-v2-meeting-service
// internal/domain/models/event_models.go (PastMeetingParticipantEventData,
// PastMeetingEventData, ParticipantSession) at origin/main; values are test data.

func participantDoc(uid, email, first, last string, attended, invited bool, orgName string) string {
	return fmt.Sprintf(`{
	  "type": "v1_past_meeting_participant",
	  "id": %q,
	  "data": {
	    "uid": %q,
	    "meeting_and_occurrence_id": "91461158520-1771596000000",
	    "meeting_id": "91461158520",
	    "project_uid": "a0941000002wBz4AAE",
	    "project_slug": "cncf",
	    "committee_uid": "9f0c2e6a-6b1e-4c3e-9d1a-0c1b2a3d4e5f",
	    "email": %q,
	    "first_name": %q,
	    "last_name": %q,
	    "host": false,
	    "job_title": "Engineer",
	    "org_name": %q,
	    "org_is_member": true,
	    "org_is_project_member": false,
	    "avatar_url": "",
	    "username": "",
	    "is_invited": %t,
	    "is_attended": %t,
	    "is_verified": false,
	    "is_unknown": false,
	    "is_ai_reconciled": false,
	    "is_auto_matched": false,
	    "zoom_user_name": %q,
	    "mapped_invitee_name": "",
	    "sessions": [{"uid": "s1", "join_time": "2026-06-10T15:02:11Z", "leave_time": "2026-06-10T15:58:40Z", "leave_reason": "left"}],
	    "created_at": "2026-06-10T16:00:00Z",
	    "updated_at": "2026-06-10T16:00:00Z"
	  }
	}`, uid, uid, email, first, last, orgName, invited, attended, first+" "+last)
}

func pastMeetingDoc(occurrenceID string) string {
	return fmt.Sprintf(`{
	  "type": "v1_past_meeting",
	  "id": %q,
	  "data": {
	    "id": "0d4a1a5e-3f7d-4b1a-9c5e-1a2b3c4d5e6f",
	    "meeting_id": "91461158520",
	    "meeting_and_occurrence_id": %q,
	    "occurrence_id": "1771596000000",
	    "project_uid": "a0941000002wBz4AAE",
	    "project_slug": "cncf",
	    "committee_uid": "9f0c2e6a-6b1e-4c3e-9d1a-0c1b2a3d4e5f",
	    "title": "CNCF TOC",
	    "description": "",
	    "start_time": "2026-06-10T15:00:00Z",
	    "end_time": "2026-06-10T16:00:00Z",
	    "duration": 60,
	    "timezone": "UTC",
	    "restricted": false,
	    "recording_enabled": true,
	    "transcript_enabled": true,
	    "sessions": [{"uuid": "abc", "start_time": "2026-06-10T15:00:00Z", "end_time": "2026-06-10T16:00:00Z"}],
	    "created_at": "2026-06-10T16:00:00Z",
	    "updated_at": "2026-06-10T16:00:00Z",
	    "created_by": {}, "updated_by": {}
	  }
	}`, occurrenceID, occurrenceID)
}

func page(docs []string, token string) string {
	body := `{"resources": [` + strings.Join(docs, ",") + `]`
	if token != "" {
		body += fmt.Sprintf(`, "page_token": %q`, token)
	}
	return body + "}"
}

func setupParticipantTest(t *testing.T) *stubLFXAPI {
	t.Helper()
	api := newStubLFXAPI(t)
	prev := meetingConfig
	SetMeetingConfig(&MeetingConfig{Clients: api.Clients})
	t.Cleanup(func() { meetingConfig = prev })
	return api
}

func boolPtrT(b bool) *bool { return &b }

func TestParticipants_LegacyCallUnchangedExceptDedupe(t *testing.T) {
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page([]string{
		participantDoc("p1", "a@example.org", "Ann", "A", true, true, "Red Hat"),
		participantDoc("p2", "b@example.org", "Bob", "B", false, true, "SUSE"),
	}, ""))

	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{
		ProjectUID: "a0941000002wBz4AAE", Name: "Ann", PageSize: 25, PageToken: "tok", Sort: "name_desc",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	reqs := api.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected exactly one query without a date range, got %d", len(reqs))
	}
	r := reqs[0]
	assertExchangedAuth(t, r)
	if r.Path != resourcesPath || r.Query.Get("type") != "v1_past_meeting_participant" || r.Query.Get("parent") != "project:a0941000002wBz4AAE" ||
		r.Query.Get("name") != "Ann" || r.Query.Get("page_size") != "25" || r.Query.Get("sort") != "name_desc" || r.Query.Get("page_token") != "tok" {
		t.Errorf("legacy payload changed: %v", r.Query)
	}
	if _, has := r.Query["filters"]; has {
		t.Errorf("legacy filters param must never be sent, got %v", r.Query["filters"])
	}
	if _, has := r.Query["tags"]; has {
		t.Errorf("no tags expected without attended_only, got %v", r.Query["tags"])
	}
	if _, has := r.Query["filters_all"]; has {
		t.Errorf("no filters_all expected without org_name, got %v", r.Query["filters_all"])
	}
	out := resultJSON(t, res)
	if out["people"] != float64(2) || out["records"] != float64(2) {
		t.Errorf("dedupe on by default: want people=2 records=2, got %v", out)
	}
}

func TestParticipants_ScopePrecedenceAndFilters(t *testing.T) {
	for _, tc := range []struct {
		name string
		args SearchPastMeetingParticipantsArgs
		want string
	}{
		{"past_meeting wins", SearchPastMeetingParticipantsArgs{PastMeetingID: "m-1", CommitteeUID: "c-1", ProjectUID: "p-1"}, "past_meeting:m-1"},
		{"committee over project", SearchPastMeetingParticipantsArgs{CommitteeUID: "c-1", ProjectUID: "p-1"}, "committee:c-1"},
		{"project alone", SearchPastMeetingParticipantsArgs{ProjectUID: "p-1"}, "project:p-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupParticipantTest(t)
			api.Respond(resourcesPath, page(nil, ""))
			tc.args.AttendedOnly = true
			tc.args.OrgName = "Red Hat, Inc."
			handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), tc.args) //nolint:errcheck
			r := api.LastRequest()
			if r.Query.Get("parent") != tc.want {
				t.Errorf("parent: want %s got %s", tc.want, r.Query.Get("parent"))
			}
			if got := r.Query["tags"]; len(got) != 1 || got[0] != "is_attended:true" {
				t.Errorf("attended_only tag: %v", got)
			}
			// flat_object: verbatim stored value, no .keyword, case preserved.
			if got := r.Query["filters_all"]; len(got) != 1 || got[0] != "org_name:Red Hat, Inc." {
				t.Errorf("org_name filter: %v", got)
			}
		})
	}
}

func TestParticipants_EmptyPageNotes(t *testing.T) {
	// Empty with no token: nothing visible.
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p"})
	if !strings.Contains(allResultText(t, res), "visible to your identity") {
		t.Errorf("empty page must carry the visibility note, got %s", allResultText(t, res))
	}
	if strings.Contains(allResultText(t, res), "WARNING") {
		t.Error("no page warning expected without a page token")
	}

	// Empty with a token: access-filtered page, warning as before.
	api2 := setupParticipantTest(t)
	api2.Respond(resourcesPath, page(nil, "next"))
	res2, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p"})
	text := allResultText(t, res2)
	if !strings.Contains(text, "WARNING: some results on this page were excluded") {
		t.Errorf("page warning missing: %s", text)
	}
	if out := resultJSON(t, res2); out["page_token"] != "next" {
		t.Errorf("page token must be passed through, got %v", out["page_token"])
	}
}

func TestParticipants_DedupeMergesLikeSelfServe(t *testing.T) {
	api := setupParticipantTest(t)
	// Same person twice (case/space differences), attended only on the second
	// record, which also carries the org. Plus an unmatched name with no e-mail.
	invitedOnly := strings.Replace(participantDoc("p1", "Ann@Example.org ", "Ann", "A", false, true, "Old Org"), `"job_title": "Engineer"`, `"job_title": "CTO"`, 1)
	attended := strings.Replace(participantDoc("p2", "ann@example.org", "Ann", "A", true, false, "Red Hat"), `"job_title": "Engineer"`, `"job_title": ""`, 1)
	noEmail := participantDoc("p3", "", "Ghost", "G", true, false, "")
	api.Respond(resourcesPath, page([]string{invitedOnly, attended, noEmail}, ""))

	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p"})
	out := resultJSON(t, res)
	if out["records"] != float64(3) || out["people"] != float64(2) {
		t.Fatalf("want records=3 people=2, got %v %v", out["records"], out["people"])
	}
	resources := out["resources"].([]any)
	ann := resources[0].(map[string]any)["Data"].(map[string]any)
	if ann["is_attended"] != true || ann["is_invited"] != true {
		t.Errorf("flags must be OR'd: %v", ann)
	}
	if ann["org_name"] != "Red Hat" {
		t.Errorf("attended record's org_name must win, got %v", ann["org_name"])
	}
	if ann["uid"] != "p2" {
		t.Errorf("attended record's identity must win, got %v", ann["uid"])
	}
	if ann["job_title"] != "CTO" {
		t.Errorf("empty field on the preferred record must be filled from the other, got %v", ann["job_title"])
	}
	ghost := resources[1].(map[string]any)["Data"].(map[string]any)
	if ghost["uid"] != "p3" {
		t.Errorf("the unmatched no-email record must survive as a singleton, got %v", ghost)
	}
}

func TestParticipants_IdentityDedupePreservesOutputCountsAndScope(t *testing.T) {
	api := setupParticipantTest(t)
	invited := strings.Replace(participantDoc("p1", "account@example.org", "Test", "Person", false, true, "Invited Org"), `"username": ""`, `"username": "synthetic-user"`, 1)
	attended := strings.Replace(participantDoc("p2", "session@example.org", "Test", "Person", true, false, "Attended Org"), `"username": ""`, `"username": "synthetic-user"`, 1)
	api.Respond(resourcesPath, page([]string{invited, attended}, "next"))
	res, _, err := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p"})
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v, %s", err, allResultText(t, res))
	}
	out := resultJSON(t, res)
	if out["people"] != float64(1) || out["records"] != float64(2) || out["page_token"] != "next" {
		t.Errorf("identity dedup must keep raw records and the page token: %v", out)
	}
	if note, _ := out["note"].(string); !strings.Contains(note, "this page only") {
		t.Errorf("per-page scope note missing: %s", note)
	}
	data := out["resources"].([]any)[0].(map[string]any)["Data"].(map[string]any)
	if data["email"] != "account@example.org" || data["org_name"] != "Attended Org" || data["is_attended"] != true || data["is_invited"] != true {
		t.Errorf("handler must expose the Self Serve merge: %v", data)
	}
	assertExchangedAuth(t, api.LastRequest())
}

func TestParticipants_DedupeFalseReturnsRawRecords(t *testing.T) {
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page([]string{
		participantDoc("p1", "ann@example.org", "Ann", "A", false, true, ""),
		participantDoc("p2", "ann@example.org", "Ann", "A", true, false, ""),
	}, ""))
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p", Dedupe: boolPtrT(false)})
	out := resultJSON(t, res)
	if len(out["resources"].([]any)) != 2 {
		t.Errorf("dedupe=false must return both records")
	}
	if _, has := out["people"]; has {
		t.Error("people must be omitted when dedupe is off")
	}
}

func TestParticipants_DateRangeTwoStep(t *testing.T) {
	api := setupParticipantTest(t)
	// Step 1: two pages of past meetings.
	api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-1"), pastMeetingDoc("m-2")}, "pm-next"))
	api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-3")}, ""))
	// Step 2: participants for m-1 (two pages), m-2, m-3.
	api.Respond(resourcesPath, page([]string{participantDoc("p1", "a@x.org", "A", "A", true, true, "Red Hat")}, "pp-next"))
	api.Respond(resourcesPath, page([]string{participantDoc("p2", "b@x.org", "B", "B", true, true, "Red Hat")}, ""))
	api.Respond(resourcesPath, page([]string{participantDoc("p3", "a@x.org", "A", "A", false, true, "Red Hat")}, ""))
	api.Respond(resourcesPath, page(nil, ""))

	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{
		ProjectUID: "cncf-uid", DateFrom: "2026-06-01", DateTo: "2026-06-30", AttendedOnly: true, OrgName: "Red Hat",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	reqs := api.Requests()
	if len(reqs) != 6 {
		t.Fatalf("expected 6 requests (2 meeting pages + 4 participant pages), got %d", len(reqs))
	}
	// Step 1 shape.
	for _, r := range reqs[:2] {
		if r.Query.Get("type") != "v1_past_meeting" || r.Query.Get("parent") != "project:cncf-uid" ||
			r.Query.Get("date_field") != "start_time" || r.Query.Get("date_from") != "2026-06-01" || r.Query.Get("date_to") != "2026-06-30" ||
			r.Query.Get("page_size") != "100" {
			t.Errorf("step-1 payload wrong: %v", r.Query)
		}
	}
	if reqs[1].Query.Get("page_token") != "pm-next" {
		t.Errorf("step 1 must follow the page token, got %v", reqs[1].Query)
	}
	// Step 2 shape.
	wantParents := []string{"past_meeting:m-1", "past_meeting:m-1", "past_meeting:m-2", "past_meeting:m-3"}
	for i, r := range reqs[2:] {
		if r.Query.Get("type") != "v1_past_meeting_participant" || r.Query.Get("parent") != wantParents[i] ||
			r.Query.Get("page_size") != "100" || r.Query["tags"][0] != "is_attended:true" || r.Query["filters_all"][0] != "org_name:Red Hat" {
			t.Errorf("step-2 payload %d wrong: %v", i, r.Query)
		}
		if _, has := r.Query["date_field"]; has {
			t.Error("participant queries must not carry the date range (no start time on the participant)")
		}
	}
	if reqs[3].Query.Get("page_token") != "pp-next" {
		t.Error("step 2 must follow the participant page token")
	}
	out := resultJSON(t, res)
	if out["records"] != float64(3) || out["people"] != float64(2) || out["meetings"] != float64(3) {
		t.Errorf("want records=3 people=2 meetings=3, got %v", out)
	}
	if _, has := out["truncated_meetings"]; has {
		t.Error("truncated_meetings must be omitted when the cap was not hit")
	}
}

func TestParticipants_DateRangeMaxMeetingsTruncates(t *testing.T) {
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-1"), pastMeetingDoc("m-2"), pastMeetingDoc("m-3")}, "more"))
	api.Respond(resourcesPath, page(nil, ""))
	api.Respond(resourcesPath, page(nil, ""))

	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{
		CommitteeUID: "c", DateFrom: "2026-01-01", MaxMeetings: 2,
	})
	out := resultJSON(t, res)
	if out["truncated_meetings"] != true || out["meetings"] != float64(2) {
		t.Errorf("expected truncated_meetings=true meetings=2, got %v", out)
	}
	if !strings.Contains(out["note"].(string), "max_meetings") {
		t.Errorf("truncation note missing: %v", out["note"])
	}
	if len(api.Requests()) != 3 {
		t.Errorf("must stop after the cap: 1 meeting page + 2 participant drains, got %d", len(api.Requests()))
	}
	if reqs := api.Requests(); reqs[0].Query.Get("parent") != "committee:c" {
		t.Errorf("step 1 must use the committee parent, got %v", reqs[0].Query)
	}
}

func TestParticipants_CountOnly(t *testing.T) {
	api := setupParticipantTest(t)
	api.Respond(countPath, `{"count": 17, "has_more": false}`)
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{
		ProjectUID: "p", AttendedOnly: true, OrgName: "SUSE", Name: "Bo", CountOnly: true,
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	r := api.LastRequest()
	if r.Path != countPath || r.Query.Get("parent") != "project:p" || r.Query["tags"][0] != "is_attended:true" ||
		r.Query["filters_all"][0] != "org_name:SUSE" || r.Query.Get("name") != "Bo" {
		t.Errorf("count payload must carry the same filters: %s %v", r.Path, r.Query)
	}
	out := resultJSON(t, res)
	if out["count"] != float64(17) || out["complete"] != true || out["visibility"] != "caller" {
		t.Errorf("unexpected count result: %v", out)
	}
	if !strings.Contains(out["note"].(string), "not distinct people") {
		t.Errorf("records-not-people caveat missing: %v", out["note"])
	}
}

func TestParticipants_CountOnlyWithDateRangeSumsAndTracksComplete(t *testing.T) {
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-1"), pastMeetingDoc("m-2")}, ""))
	api.Respond(countPath, `{"count": 10, "has_more": false}`)
	api.Respond(countPath, `{"count": 5, "has_more": true}`)

	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{
		ProjectUID: "p", DateFrom: "2026-06-01", CountOnly: true,
	})
	out := resultJSON(t, res)
	if out["count"] != float64(15) {
		t.Errorf("count must be the per-meeting sum, got %v", out["count"])
	}
	if out["complete"] != false {
		t.Error("one incomplete per-meeting count must make the total incomplete")
	}
	counts := api.RequestsTo(countPath)
	if len(counts) != 2 || counts[0].Query.Get("parent") != "past_meeting:m-1" || counts[1].Query.Get("parent") != "past_meeting:m-2" {
		t.Errorf("expected one count per resolved meeting, got %v", counts)
	}
}

func TestParticipants_ArgumentGuards(t *testing.T) {
	api := setupParticipantTest(t)
	for _, tc := range []struct {
		args SearchPastMeetingParticipantsArgs
		want string
	}{
		{SearchPastMeetingParticipantsArgs{PastMeetingID: "m", DateFrom: "2026-01-01"}, "past_meeting_id"},
		{SearchPastMeetingParticipantsArgs{DateFrom: "2026-01-01"}, "require project_uid or committee_uid"},
		{SearchPastMeetingParticipantsArgs{Name: "Ann", DateTo: "2026-01-31"}, "require project_uid or committee_uid"},
		{SearchPastMeetingParticipantsArgs{ProjectUID: "p", DateFrom: "2026-01-01", PageToken: "x"}, "page_token"},
		{SearchPastMeetingParticipantsArgs{ProjectUID: "p", DateFrom: "2026-01-01", MaxMeetings: 201}, "max_meetings"},
	} {
		res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), tc.args)
		if !res.IsError || !strings.Contains(allResultText(t, res), tc.want) {
			t.Errorf("expected error mentioning %q for %+v, got %q", tc.want, tc.args, allResultText(t, res))
		}
	}
	if len(api.Requests()) != 0 {
		t.Error("guards must fire before any API call")
	}
}

func TestParticipants_APIErrorIsFriendly(t *testing.T) {
	api := setupParticipantTest(t)
	api.RespondStatus(resourcesPath, http.StatusForbidden, `{"message":"forbidden"}`)
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p"})
	if !res.IsError || !strings.Contains(allResultText(t, res), accessDeniedMessage) {
		t.Errorf("403 should map to access-denied wording, got %q", allResultText(t, res))
	}
}

func TestMeetingRegistrantsDescriptionMatchesHandler(t *testing.T) {
	for _, asGroups := range []bool{false, true} {
		tool := listRegisteredTool(t, "search_meeting_registrants", func(s *mcp.Server) { RegisterSearchMeetingRegistrants(s, asGroups) })
		desc := strings.ToLower(tool.Description)
		for _, banned := range []string{"project", "date range"} {
			if strings.Contains(desc, banned) {
				t.Errorf("asGroups=%v: description claims %q, which the handler does not filter on: %s", asGroups, banned, tool.Description)
			}
		}
		for _, want := range []string{"meeting", "name"} {
			if !strings.Contains(desc, want) {
				t.Errorf("asGroups=%v: description should mention %q", asGroups, want)
			}
		}
		if asGroups && !strings.Contains(desc, "group") {
			t.Error("group mode must mention group")
		}
		if !asGroups && !strings.Contains(desc, "committee") {
			t.Error("committee mode must mention committee")
		}
	}
}

func TestParticipantsDescriptionAdvertisesNewFilters(t *testing.T) {
	tool := listRegisteredTool(t, "search_past_meeting_participants", func(s *mcp.Server) { RegisterSearchPastMeetingParticipants(s, false) })
	if n := len(tool.Description); n > 1000 {
		t.Errorf("description is %d bytes, keep it under 1000", n)
	}
	for _, want := range []string{"committee UID", "date_from", "attended_only", "org_name", "count_only", "dedupe", "visible to the caller"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("description missing %q", want)
		}
	}
	for _, banned := range []string{"Insights", "Jim", "because"} {
		if strings.Contains(tool.Description, banned) {
			t.Errorf("description must not contain %q", banned)
		}
	}
}

func TestParticipants_PageTokenLoopsAreCapped(t *testing.T) {
	// Every participant page returns a token: the drain must stop with an error.
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-1")}, ""))
	for i := 0; i < participantMaxDrainPages+5; i++ {
		api.Respond(resourcesPath, page(nil, fmt.Sprintf("t%d", i)))
	}
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p", DateFrom: "2026-01-01"})
	if !res.IsError || !strings.Contains(allResultText(t, res), "page cap") {
		t.Errorf("expected a page-cap error, got %q", allResultText(t, res))
	}
	if n := len(api.Requests()); n != participantMaxDrainPages+1 {
		t.Errorf("expected 1 meeting page + %d participant pages, got %d", participantMaxDrainPages, n)
	}

	// Same for the past-meeting resolution loop.
	api2 := setupParticipantTest(t)
	for i := 0; i < participantMaxDrainPages+5; i++ {
		api2.Respond(resourcesPath, page(nil, fmt.Sprintf("t%d", i)))
	}
	res2, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p", DateFrom: "2026-01-01"})
	if !res2.IsError || len(api2.Requests()) != participantMaxDrainPages {
		t.Errorf("meeting resolution must stop at the page cap, got error=%v requests=%d", res2.IsError, len(api2.Requests()))
	}
}

func TestParticipants_DateRangeRecordCap(t *testing.T) {
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-1"), pastMeetingDoc("m-2")}, ""))
	// m-1 alone yields more than the cap across pages; m-2 must never be queried.
	perPage := 100
	pages := participantMaxRecords/perPage + 1
	for p := 0; p < pages; p++ {
		docs := make([]string, perPage)
		for i := range docs {
			n := p*perPage + i
			docs[i] = participantDoc(fmt.Sprintf("p%d", n), fmt.Sprintf("u%d@x.org", n), "U", fmt.Sprintf("%d", n), true, true, "")
		}
		api.Respond(resourcesPath, page(docs, fmt.Sprintf("t%d", p)))
	}
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p", DateFrom: "2026-01-01"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	if out["truncated_records"] != true || out["records"] != float64(participantMaxRecords) {
		t.Errorf("expected truncated_records=true records=%d, got %v %v", participantMaxRecords, out["truncated_records"], out["records"])
	}
	if !strings.Contains(out["note"].(string), "count_only") {
		t.Errorf("record-cap note missing: %v", out["note"])
	}
	for _, r := range api.RequestsTo(resourcesPath) {
		if r.Query.Get("parent") == "past_meeting:m-2" {
			t.Error("must stop draining once the record cap is hit")
		}
	}
}

func TestParticipants_PerPageDedupeIsDisclosed(t *testing.T) {
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page([]string{participantDoc("p1", "a@x.org", "A", "A", true, true, "")}, "next"))
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p", PageSize: 1})
	if !strings.Contains(resultJSON(t, res)["note"].(string), "this page only") {
		t.Errorf("a paged dedupe must say people/records are per page, got %v", resultJSON(t, res)["note"])
	}
}

func TestParticipants_CountOnlyTruncatedMeetingsIsIncomplete(t *testing.T) {
	// Step 1 hits max_meetings with a token left: the summed count is a lower bound.
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-1"), pastMeetingDoc("m-2"), pastMeetingDoc("m-3")}, "more"))
	api.Respond(countPath, `{"count": 4, "has_more": false}`)
	api.Respond(countPath, `{"count": 6, "has_more": false}`)
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{
		ProjectUID: "p", DateFrom: "2026-01-01", MaxMeetings: 2, CountOnly: true,
	})
	out := resultJSON(t, res)
	if out["count"] != float64(10) || out["complete"] != false {
		t.Errorf("want count=10 complete=false, got %v", out)
	}
	if !strings.Contains(out["note"].(string), "max_meetings") || !strings.Contains(out["note"].(string), "lower bound") {
		t.Errorf("note must explain both the truncation and the lower bound: %v", out["note"])
	}
	if n := len(api.RequestsTo(countPath)); n != 2 {
		t.Errorf("expected exactly 2 count calls (the expanded meetings), got %d", n)
	}
}

func TestParticipants_MaxMeetingsDefaultIs50(t *testing.T) {
	api := setupParticipantTest(t)
	docs := make([]string, participantDefaultMaxMeetings+1)
	for i := range docs {
		docs[i] = pastMeetingDoc(fmt.Sprintf("m-%d", i))
	}
	api.Respond(resourcesPath, page(docs, ""))
	for i := 0; i < participantDefaultMaxMeetings; i++ {
		api.Respond(resourcesPath, page(nil, ""))
	}
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p", DateFrom: "2026-01-01"})
	out := resultJSON(t, res)
	if out["meetings"] != float64(participantDefaultMaxMeetings) || out["truncated_meetings"] != true {
		t.Errorf("default cap must be %d with truncation flagged, got %v", participantDefaultMaxMeetings, out)
	}
	if n := len(api.Requests()); n != participantDefaultMaxMeetings+1 {
		t.Errorf("expected 1 meeting page + %d participant drains, got %d", participantDefaultMaxMeetings, n)
	}
}

func TestParticipants_MaxMeetingsOnlyValidatedWithDateRange(t *testing.T) {
	// Without a date range max_meetings is inert; a large value must not error.
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p", MaxMeetings: 999})
	if res.IsError {
		t.Errorf("max_meetings must only be validated with a date range, got %s", allResultText(t, res))
	}
}

func TestParticipants_PastMeetingIDFallsBackToResourceID(t *testing.T) {
	api := setupParticipantTest(t)
	noOcc := `{"type":"v1_past_meeting","id":"rid-1","data":{"title":"x","start_time":"2026-06-10T15:00:00Z"}}`
	noID := `{"type":"v1_past_meeting","id":"","data":{"title":"y"}}`
	api.Respond(resourcesPath, page([]string{noOcc, noID}, ""))
	api.Respond(resourcesPath, page(nil, ""))
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p", DateFrom: "2026-01-01"})
	out := resultJSON(t, res)
	if out["meetings"] != float64(1) {
		t.Errorf("a meeting without meeting_and_occurrence_id must fall back to its id; one without either is skipped: %v", out["meetings"])
	}
	if reqs := api.RequestsTo(resourcesPath); len(reqs) != 2 || reqs[1].Query.Get("parent") != "past_meeting:rid-1" {
		t.Errorf("step 2 must use the resource id as parent, got %+v", reqs)
	}
}

func TestParticipants_DedupeEdgeShapes(t *testing.T) {
	api := setupParticipantTest(t)
	nonMap := `{"type":"v1_past_meeting_participant","id":"odd","data":"not-an-object"}`
	blankA := `{"type":"v1_past_meeting_participant","id":"ba","data":{"uid":"","email":""}}`
	blankB := `{"type":"v1_past_meeting_participant","id":"bb","data":{"uid":"","email":""}}`
	api.Respond(resourcesPath, page([]string{nonMap, blankA, blankB}, ""))
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p"})
	out := resultJSON(t, res)
	if out["records"] != float64(3) || out["people"] != float64(3) {
		t.Errorf("non-object data and blank email+uid records must each survive as their own entry: %v", out)
	}
}

func TestParticipants_RecordCapIsHonestAtExactBoundary(t *testing.T) {
	// Exactly participantMaxRecords with no token and no further meetings: NOT truncated.
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-1")}, ""))
	perPage := 100
	for p := 0; p < participantMaxRecords/perPage; p++ {
		docs := make([]string, perPage)
		for i := range docs {
			n := p*perPage + i
			docs[i] = participantDoc(fmt.Sprintf("p%d", n), fmt.Sprintf("u%d@x.org", n), "U", fmt.Sprintf("%d", n), true, true, "")
		}
		token := fmt.Sprintf("t%d", p)
		if p == participantMaxRecords/perPage-1 {
			token = ""
		}
		api.Respond(resourcesPath, page(docs, token))
	}
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p", DateFrom: "2026-01-01"})
	out := resultJSON(t, res)
	if _, has := out["truncated_records"]; has {
		t.Errorf("exactly the cap with nothing left is complete, got truncated_records=%v", out["truncated_records"])
	}
	if out["records"] != float64(participantMaxRecords) || out["meetings"] != float64(1) {
		t.Errorf("want records=%d meetings=1, got %v %v", participantMaxRecords, out["records"], out["meetings"])
	}
	if note, _ := out["note"].(string); strings.Contains(note, "more than") {
		t.Errorf("no cap note expected: %v", note)
	}
}

func TestParticipants_RecordCapReportsDrainedMeetingsOnly(t *testing.T) {
	// Cap hit on the first of three meetings: meetings=1, truncated_records=true.
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-1"), pastMeetingDoc("m-2"), pastMeetingDoc("m-3")}, ""))
	perPage := 100
	for p := 0; p < participantMaxRecords/perPage; p++ {
		docs := make([]string, perPage)
		for i := range docs {
			n := p*perPage + i
			docs[i] = participantDoc(fmt.Sprintf("p%d", n), fmt.Sprintf("u%d@x.org", n), "U", fmt.Sprintf("%d", n), true, true, "")
		}
		api.Respond(resourcesPath, page(docs, fmt.Sprintf("t%d", p)))
	}
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p", DateFrom: "2026-01-01"})
	out := resultJSON(t, res)
	if out["truncated_records"] != true || out["meetings"] != float64(1) {
		t.Errorf("want truncated_records=true meetings=1 (only the drained meeting), got %v %v", out["truncated_records"], out["meetings"])
	}
}

func TestParticipants_RequestBudgetBoundsFanOut(t *testing.T) {
	// 200 meetings, each with 50 empty-with-token pages then an end: every
	// per-meeting page cap is respected (50 < 200) yet the total would be
	// 10,001 requests. The shared request budget must stop it as an error.
	api := setupParticipantTest(t)
	docs := make([]string, participantHardMaxMeetings)
	for i := range docs {
		docs[i] = pastMeetingDoc(fmt.Sprintf("m-%d", i))
	}
	api.Respond(resourcesPath, page(docs, ""))
	const pagesPerMeeting = 50
	for m := 0; m < participantHardMaxMeetings; m++ {
		for p := 0; p < pagesPerMeeting; p++ {
			token := fmt.Sprintf("m%d-t%d", m, p)
			if p == pagesPerMeeting-1 {
				token = ""
			}
			api.Respond(resourcesPath, page(nil, token))
		}
	}
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p", DateFrom: "2026-01-01", MaxMeetings: participantHardMaxMeetings})
	if !res.IsError || !strings.Contains(allResultText(t, res), "count_only") {
		t.Errorf("expected a request-budget error naming count_only, got %q", allResultText(t, res))
	}
	if n := len(api.Requests()); n != participantMaxRequests {
		t.Errorf("must stop at exactly %d upstream requests, made %d", participantMaxRequests, n)
	}
}

func TestParticipants_UpstreamErrorsAreNeverBlank(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusBadRequest, `{"message":"date_from must be ISO 8601"}`, "ISO 8601"},
		{http.StatusInternalServerError, `{"message":"search backend unavailable"}`, "unavailable"},
		{http.StatusServiceUnavailable, `{"message":"try again"}`, "try again"},
	} {
		api := setupParticipantTest(t)
		api.RespondStatus(resourcesPath, tc.status, tc.body)
		res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "p"})
		text := allResultText(t, res)
		if !res.IsError || strings.TrimSpace(strings.TrimPrefix(text, "Failed to search past meeting participants:")) == "" {
			t.Errorf("%d: blank error text: %q", tc.status, text)
		}
		if !strings.Contains(text, tc.want) {
			t.Errorf("%d: upstream message %q missing from %q", tc.status, tc.want, text)
		}
	}
}

func TestTools1_MissingTokenFailsClosed(t *testing.T) {
	// Every TOOLS-1 handler must refuse a sessionless/tokenless request before any upstream call.
	req := stubCallToolRequest()
	req.Extra.TokenInfo = nil

	pAPI := setupParticipantTest(t)
	if res, _, _ := handleSearchPastMeetingParticipants(context.Background(), req, SearchPastMeetingParticipantsArgs{ProjectUID: "p"}); !res.IsError || len(pAPI.Requests()) != 0 {
		t.Error("participants: must fail closed without a token")
	}
	prAPI := setupProjectTest(t)
	if res, _, _ := handleSearchProjects(context.Background(), req, SearchProjectsArgs{}); !res.IsError || len(prAPI.Requests()) != 0 {
		t.Error("projects: must fail closed without a token")
	}
	sAPI := setupOrgSeatsTest(t)
	if res, _, _ := handleGetOrgCommitteeSeats(context.Background(), req, GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID}); !res.IsError || len(sAPI.Requests()) != 0 {
		t.Error("org seats: must fail closed without a token")
	}
}
