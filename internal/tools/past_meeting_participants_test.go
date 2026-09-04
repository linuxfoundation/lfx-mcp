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
		ProjectUID: "a0941000002wBz4AAE", Name: "Ann", PageSize: 25,
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
		r.Query.Get("name") != "Ann" || r.Query.Get("page_size") != "25" || r.Query.Get("sort") != "name_asc" {
		t.Errorf("legacy payload changed: %v", r.Query)
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
	// record, which also carries the org. Plus one record with no e-mail.
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
		t.Errorf("no-email record must survive under its uid, got %v", ghost)
	}
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
	tool := listRegisteredTool(t, "search_past_meeting_participants", RegisterSearchPastMeetingParticipants)
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
