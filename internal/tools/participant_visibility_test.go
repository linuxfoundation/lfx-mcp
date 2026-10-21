// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Past-meeting participant fixtures for the LFX Self Serve parity rule.

const (
	pastOrganized  = "11111111111-1771596000000"
	pastPublic     = "22222222222-1771596000000"
	pastRestricted = "33333333333-1771596000000"
	pastCommittee  = "44444444444-1771596000000"
	pastHidden     = "55555555555-1771596000000"
	memberOfGroup  = "9f0c2e6a-6b1e-4c3e-9d1a-0c1b2a3d4e5f"
)

// participantDocFor is participantDoc with an explicit past meeting, host
// flag and username.
func participantDocFor(uid, occurrenceID, email, first, last, username string, host, attended bool) string {
	return fmt.Sprintf(`{
	  "type": "v1_past_meeting_participant",
	  "id": %q,
	  "data": {
	    "uid": %q,
	    "meeting_and_occurrence_id": %q,
	    "meeting_id": %q,
	    "project_uid": "a0941000002wBz4AAE",
	    "committee_uid": %q,
	    "email": %q,
	    "first_name": %q,
	    "last_name": %q,
	    "username": %q,
	    "host": %t,
	    "job_title": "Engineer",
	    "org_name": "Example Org",
	    "avatar_url": "https://avatars.example.test/%s.png",
	    "is_invited": true,
	    "is_attended": %t,
	    "zoom_user_name": %q,
	    "sessions": [{"uid": "s1", "join_time": "2026-06-10T15:02:11Z"}]
	  }
	}`, uid, uid, occurrenceID, strings.Split(occurrenceID, "-")[0], memberOfGroup, email, first, last, username, host, uid, attended, first+" "+last)
}

// pastDocs answers the past-meeting record lookup for the five fixtures.
func pastDocs(ids ...string) string {
	var docs []string
	for _, id := range ids {
		switch id {
		case pastPublic:
			docs = append(docs, pastMeetingDocWith(id, "public", false))
		case pastRestricted:
			docs = append(docs, pastMeetingDocWith(id, "public", true))
		case pastCommittee:
			docs = append(docs, pastMeetingDocWith(id, "private", false, memberOfGroup))
		default:
			docs = append(docs, pastMeetingDocWith(id, "private", false))
		}
	}
	return page(docs, "")
}

// meetingRoster is a host, an attendee and the caller for one past meeting.
func meetingRoster(id string) []string {
	n := id[:2]
	return []string{
		participantDocFor("host-"+n, id, "host"+n+"@example.test", "Hosty", n, "", true, true),
		participantDocFor("att-"+n, id, "att"+n+"@example.test", "Atty", n, "", false, true),
		participantDocFor("self-"+n, id, stubCallerEmail, "Stub", n, "", false, true),
	}
}

func uids(rs []map[string]any) string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r["uid"].(string))
	}
	return strings.Join(out, " ")
}

func participantsOf(t *testing.T, out any) []map[string]any {
	t.Helper()
	res, ok := out.(participantSearchResult)
	if !ok {
		t.Fatalf("unexpected output %T", out)
	}
	data := make([]map[string]any, 0, len(res.Resources))
	for _, r := range res.Resources {
		data = append(data, resourceData(r))
	}
	return data
}

func TestParticipants_FullViewIsUnchangedAndMakesNoChecks(t *testing.T) {
	api := setupParticipantTest(t)
	api.GrantRelations()
	api.Respond(resourcesPath, page(meetingRoster(pastHidden), ""))
	_, out, _ := handleSearchPastMeetingParticipants(fullViewCtx(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastHidden})
	if got := participantsOf(t, out); len(got) != 3 || got[1]["email"] != "att55@example.test" {
		t.Errorf("full view returns the page unchanged: %v", got)
	}
	if len(api.Requests()) != 1 {
		t.Errorf("full view makes the one search call, got %d", len(api.Requests()))
	}
}

func TestParticipants_SingleMeetingViews(t *testing.T) {
	cases := []struct {
		name     string
		id       string
		grants   []string
		wantUIDs string
	}{
		{"organizer sees everything", pastOrganized, []string{"v1_past_meeting:" + pastOrganized + "#organizer"}, "host-11 att-11 self-11"},
		{"public meeting: hosts and self", pastPublic, nil, "host-22 self-22"},
		{"restricted public meeting: self only", pastRestricted, nil, "self-33"},
		{"group member: hosts and self", pastCommittee, []string{"committee:" + memberOfGroup + "#member"}, "host-44 self-44"},
		{"direct attendee relation: hosts and self", pastHidden, []string{"v1_past_meeting:" + pastHidden + "#attendee"}, "host-55 self-55"},
		{"private meeting without relations: self only", pastHidden, nil, "self-55"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := setupParticipantTest(t)
			api.GrantRelations(tc.grants...)
			api.Respond(resourcesPath, pastDocs(tc.id))                // the view lookup, before the search
			api.Respond(resourcesPath, page(meetingRoster(tc.id), "")) // the search
			res, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: tc.id})
			if res.IsError {
				t.Fatalf("unexpected error: %s", allResultText(t, res))
			}
			got := participantsOf(t, out)
			if uids(got) != tc.wantUIDs {
				t.Fatalf("records = %q, want %q", uids(got), tc.wantUIDs)
			}
			text := allResultText(t, res)
			for _, r := range got {
				switch {
				case strings.HasPrefix(r["uid"].(string), "self-"):
					if r["email"] != stubCallerEmail || r["sessions"] == nil {
						t.Errorf("the caller's own record is unchanged: %v", r)
					}
				case tc.id == pastOrganized:
					if r["email"] == nil || r["sessions"] == nil {
						t.Errorf("organizer records are unchanged: %v", r)
					}
				default:
					want := []string{"first_name", "host", "is_attended", "last_name", "meeting_and_occurrence_id", "uid"}
					if keys := sortedKeys(r); strings.Join(keys, ",") != strings.Join(want, ",") {
						t.Errorf("host stub keys = %v, want %v", keys, want)
					}
				}
			}
			if tc.id != pastOrganized && strings.Contains(text, "host"+tc.id[:2]+"@example.test") {
				t.Error("a host's e-mail reached a non-organizer")
			}
			if strings.Contains(text, "att"+tc.id[:2]+"@example.test") && tc.id != pastOrganized {
				t.Error("an attendee reached a non-organizer")
			}
			result := out.(participantSearchResult)
			if result.People == nil || *result.People != len(got) || result.Records == nil || *result.Records != len(got) {
				t.Errorf("people/records must describe what is returned: %v %v", result.People, result.Records)
			}
			// One record lookup, one relation batch holding organizer, host,
			// invitee, attendee and the group membership where the record
			// names a group.
			bodies := api.AccessCheckBodies()
			if len(bodies) != 1 {
				t.Fatalf("expected one access-check batch, got %v", bodies)
			}
			assertExchangedAuth(t, api.RequestsTo(accessCheckPath)[0])
			wantReqs := 4
			if tc.id == pastCommittee {
				wantReqs = 5
			}
			if len(bodies[0]) != wantReqs {
				t.Errorf("batch = %v", bodies[0])
			}
		})
	}
}

func TestParticipants_DedupeMergesShownRecordsBeforeProjection(t *testing.T) {
	// The same host appears twice in a full-access meeting, as the invitee
	// record (host:true, not attended) and the join record (host:true,
	// attended): both are shown, merged into one stub carrying both flags,
	// which projecting first would have lost.
	api := setupParticipantTest(t)
	api.GrantRelations()
	api.Respond(resourcesPath, pastDocs(pastPublic))
	api.Respond(resourcesPath, page([]string{
		participantDocFor("inv", pastPublic, "Host@example.test", "Hosty", "H", "", true, false),
		participantDocFor("join", pastPublic, "host@example.test", "Hosty", "H", "", true, true),
	}, ""))
	_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastPublic})
	got := participantsOf(t, out)
	if len(got) != 1 || got[0]["host"] != true || got[0]["is_attended"] != true {
		t.Fatalf("expected one merged host stub with both flags, got %v", got)
	}
	if got[0]["email"] != nil {
		t.Error("the merged stub carries no e-mail")
	}
	result := out.(participantSearchResult)
	if *result.Records != 2 || *result.People != 1 {
		t.Errorf("records/people describe the shown records: %d/%d", *result.Records, *result.People)
	}
}

func TestParticipants_ProjectionNeverRunsBeforeDedupe(t *testing.T) {
	// Two different hosts with the same name but different e-mails: identity
	// matching on the full records keeps them apart; matching on the
	// projected stubs (name only) would merge two people into one.
	api := setupParticipantTest(t)
	api.GrantRelations()
	api.Respond(resourcesPath, pastDocs(pastPublic))
	api.Respond(resourcesPath, page([]string{
		participantDocFor("h1", pastPublic, "a@example.test", "Hosty", "H", "", true, true),
		participantDocFor("h2", pastPublic, "b@example.test", "Hosty", "H", "", true, false),
	}, ""))
	_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastPublic})
	if got := uids(participantsOf(t, out)); got != "h1 h2" {
		t.Fatalf("two people with one name must stay two records, got %q", got)
	}
}

func TestParticipants_OwnAndHostRecordsNeverMerge(t *testing.T) {
	// In a full-access meeting the caller's own record is returned in full
	// and a host's as a stub. Identity matching can join the two (same name
	// with no username or e-mail on the host record, or a stored e-mail they
	// share); merged, the own record would carry the host's fields in full.
	hostOrg := func(doc string) string {
		return strings.Replace(doc, `"org_name": "Example Org"`, `"org_name": "Host Org"`, 1)
	}
	cases := []struct {
		name string
		own  string
		host string
	}{
		{"same name, host without username or e-mail",
			participantDocFor("mine", pastPublic, stubCallerEmail, "Stub", "P", "", false, false),
			hostOrg(participantDocFor("host", pastPublic, "", "Stub", "P", "", true, true))},
		{"own by username, sharing a stored e-mail with the host",
			participantDocFor("mine", pastPublic, "shared@example.test", "Stub", "P", stubCallerUsername, false, false),
			hostOrg(participantDocFor("host", pastPublic, "shared@example.test", "Hosty", "H", "", true, true))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := setupParticipantTest(t)
			api.GrantRelations()
			api.Respond(resourcesPath, pastDocs(pastPublic))
			api.Respond(resourcesPath, page([]string{tc.own, tc.host}, ""))
			res, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastPublic})
			if res.IsError {
				t.Fatal(allResultText(t, res))
			}
			got := participantsOf(t, out)
			if uids(got) != "mine host" {
				t.Fatalf("own and host records must stay apart, got %q", uids(got))
			}
			if got[0]["org_name"] != "Example Org" || got[0]["host"] != false {
				t.Errorf("the own record must carry only its own fields: %v", got[0])
			}
			for _, field := range []string{"email", "org_name", "sessions", "username"} {
				if _, ok := got[1][field]; ok {
					t.Errorf("the host stub must not carry %s: %v", field, got[1])
				}
			}
		})
	}
}

func TestParticipants_HiddenRecordsNeverMergeIntoShownOnes(t *testing.T) {
	// One person with records in a meeting the caller organizes and in a
	// private meeting of the same date range. The private record must
	// neither fill fields into the organizer's record nor decide its fate.
	// The stub answers every drain unfiltered, so these also pin the
	// selection pass as a defence behind the narrowed query.
	rangeOver := func(api *stubLFXAPI, first, second string, firstPage, secondPage []string) {
		api.Respond(resourcesPath, page([]string{pastMeetingDoc(first), pastMeetingDoc(second)}, ""))
		api.Respond(resourcesPath, pastDocs(first, second))
		api.Respond(resourcesPath, page(firstPage, ""))
		api.Respond(resourcesPath, page(secondPage, ""))
	}
	rangeArgs := SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01"}
	t.Run("organizer record keeps only its own fields", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
		rangeOver(api, pastOrganized, pastHidden,
			[]string{participantDocFor("a-x", pastOrganized, "", "Xavier", "X", "", false, true)},
			[]string{participantDocFor("b-x", pastHidden, "xavier.hidden@example.test", "Xavier", "X", "", true, false)})
		res, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), rangeArgs)
		if res.IsError {
			t.Fatal(allResultText(t, res))
		}
		got := participantsOf(t, out)
		if uids(got) != "a-x" || got[0]["host"] != false || got[0]["email"] != "" {
			t.Fatalf("the organizer's record must survive unchanged and alone: %v", got)
		}
		if strings.Contains(allResultText(t, res), "xavier.hidden@example.test") {
			t.Error("the hidden meeting's address must not appear")
		}
	})
	t.Run("organizer record is not swallowed by an attended hidden record", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
		rangeOver(api, pastOrganized, pastHidden,
			[]string{participantDocFor("a-z", pastOrganized, "z@example.test", "Zed", "Z", "", false, false)},
			[]string{participantDocFor("b-z", pastHidden, "z@example.test", "Zed", "Z", "", false, true)})
		_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), rangeArgs)
		if got := participantsOf(t, out); uids(got) != "a-z" || got[0]["is_attended"] != false {
			t.Fatalf("the organizer's record must be returned as stored: %v", got)
		}
	})
	t.Run("a host of a hidden meeting is not a host of a public one", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		rangeOver(api, pastPublic, pastHidden,
			[]string{participantDocFor("a-y", pastPublic, "y@example.test", "Yan", "Y", "", false, true)},
			[]string{participantDocFor("b-y", pastHidden, "y@example.test", "Yan", "Y", "", true, true)})
		_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), rangeArgs)
		if got := participantsOf(t, out); len(got) != 0 {
			t.Fatalf("a non-host of the public meeting must not appear: %v", got)
		}
	})
	t.Run("same username: non-host in a full-access meeting, host in a hidden one", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		rangeOver(api, pastPublic, pastHidden,
			[]string{participantDocFor("a-u", pastPublic, "", "Uma", "U", "uma-user", false, true)},
			[]string{participantDocFor("b-u", pastHidden, "uma@example.test", "Uma", "U", "uma-user", true, true)})
		res, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), rangeArgs)
		if got := participantsOf(t, out); len(got) != 0 {
			t.Fatalf("username identity must not carry the hidden host flag across: %v", got)
		}
		if strings.Contains(allResultText(t, res), "uma@example.test") {
			t.Error("the hidden meeting's address must not appear")
		}
	})
	t.Run("a host of a shown meeting stays a host when also an attendee of a hidden one", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		rangeOver(api, pastPublic, pastHidden,
			[]string{participantDocFor("a-h", pastPublic, "h@example.test", "Hal", "H", "hal-user", true, true)},
			[]string{participantDocFor("b-h", pastHidden, "h@example.test", "Hal", "H", "hal-user", false, true)})
		res, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), rangeArgs)
		got := participantsOf(t, out)
		if uids(got) != "a-h" || got[0]["host"] != true || got[0]["email"] != nil {
			t.Fatalf("the shown host, reduced, and nothing else: %v", got)
		}
		if strings.Contains(allResultText(t, res), "h@example.test") {
			t.Error("a host's address is not shown to a full-access caller")
		}
	})
	t.Run("same username in an organized meeting and an own-only one", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
		rangeOver(api, pastOrganized, pastHidden,
			[]string{participantDocFor("a-v", pastOrganized, "v@example.test", "Val", "V", "val-user", false, false)},
			[]string{participantDocFor("b-v", pastHidden, "v@example.test", "Val", "V", "val-user", true, true)})
		_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), rangeArgs)
		if got := participantsOf(t, out); uids(got) != "a-v" || got[0]["host"] != false || got[0]["is_attended"] != false {
			t.Fatalf("the organized meeting's record as stored, the hidden one gone: %v", got)
		}
	})
	t.Run("a name-only stranger does not merge into the caller's own record", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, pastDocs(pastHidden))
		api.Respond(resourcesPath, page([]string{
			participantDocFor("me", pastHidden, stubCallerEmail, "Stub", "User", "", false, true),
			participantDocFor("them", pastHidden, "", "Stub", "User", "", false, true),
		}, ""))
		_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastHidden})
		got := participantsOf(t, out)
		if uids(got) != "me" || got[0]["job_title"] != "Engineer" {
			t.Fatalf("own record only, as stored: %v", got)
		}
		result := out.(participantSearchResult)
		if *result.Records != 1 || *result.People != 1 {
			t.Errorf("the stranger's record counts nowhere: %d/%d", *result.Records, *result.People)
		}
	})
}

func TestParticipants_LegacyCommitteeUIDGrantsGroupMembers(t *testing.T) {
	// A past-meeting record that names its group only in the top-level
	// committee_uid (empty committees array) still counts as that group's
	// meeting; the same group named in both places is checked once.
	legacyDoc := `{"type":"v1_past_meeting","id":"` + pastCommittee + `","data":{"meeting_and_occurrence_id":"` + pastCommittee + `","visibility":"private","restricted":false,"committee_uid":"` + memberOfGroup + `","committees":[{"uid":"` + memberOfGroup + `"}]}}`
	api := setupParticipantTest(t)
	api.GrantRelations("committee:" + memberOfGroup + "#member")
	api.Respond(resourcesPath, page([]string{legacyDoc}, ""))
	api.Respond(resourcesPath, page(meetingRoster(pastCommittee), ""))
	_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastCommittee})
	if got := uids(participantsOf(t, out)); got != "host-44 self-44" {
		t.Fatalf("a group member has full access, got %q", got)
	}
	bodies := api.AccessCheckBodies()
	if len(bodies) != 1 || len(bodies[0]) != 5 || !strings.Contains(strings.Join(bodies[0], " "), "committee:"+memberOfGroup+"#member") {
		t.Errorf("expected four meeting relations plus one group membership, got %v", bodies)
	}
	// And with the array empty, the top-level field alone suffices.
	legacyOnly := strings.Replace(legacyDoc, `"committees":[{"uid":"`+memberOfGroup+`"}]`, `"committees":[]`, 1)
	api2 := setupParticipantTest(t)
	api2.GrantRelations("committee:" + memberOfGroup + "#member")
	api2.Respond(resourcesPath, page([]string{legacyOnly}, ""))
	api2.Respond(resourcesPath, page(meetingRoster(pastCommittee), ""))
	_, out2, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastCommittee})
	if got := uids(participantsOf(t, out2)); got != "host-44 self-44" {
		t.Fatalf("top-level committee_uid alone must grant the group view, got %q", got)
	}
}

func TestParticipants_PerMeetingDedupeKeepsPageOrder(t *testing.T) {
	// Merging within a meeting must leave the page in its sorted order,
	// with the merged person at the first of their original slots.
	api := setupParticipantTest(t)
	api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
	api.Respond(resourcesPath, pastDocs(pastOrganized))
	api.Respond(resourcesPath, page([]string{
		participantDocFor("carol", pastOrganized, "carol@example.test", "Carol", "C", "", false, true),
		participantDocFor("alice", pastOrganized, "alice@example.test", "Alice", "A", "", false, true),
		participantDocFor("carol-2", pastOrganized, "carol@example.test", "Carol", "C", "", false, false),
		participantDocFor("bob", pastOrganized, "bob@example.test", "Bob", "B", "", false, true),
	}, ""))
	_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastOrganized, Sort: "name_desc"})
	if got := uids(participantsOf(t, out)); got != "carol alice bob" {
		t.Fatalf("expected the page order with carol merged in place, got %q", got)
	}
}

func TestParticipants_DateRangeCapCountsShownRecordsOnly(t *testing.T) {
	// The hidden meeting holds more raw records than the whole cap, none of
	// them shown; the organized meeting after it must still be drained in
	// full and no truncation reported.
	api := setupParticipantTest(t)
	api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
	api.Respond(resourcesPath, page([]string{pastMeetingDoc(pastHidden), pastMeetingDoc(pastOrganized)}, ""))
	api.Respond(resourcesPath, pastDocs(pastHidden, pastOrganized))
	pages := participantMaxRecords/participantDrainPageSize + 1
	for p := 0; p < pages; p++ {
		docs := make([]string, 0, participantDrainPageSize)
		for i := 0; i < participantDrainPageSize; i++ {
			n := p*participantDrainPageSize + i
			docs = append(docs, participantDocFor(fmt.Sprintf("hid-%d", n), pastHidden, fmt.Sprintf("hid%d@example.test", n), "Hid", fmt.Sprint(n), "", false, true))
		}
		token := fmt.Sprintf("t%d", p+1)
		if p == pages-1 {
			token = ""
		}
		api.Respond(resourcesPath, page(docs, token))
	}
	api.Respond(resourcesPath, page(meetingRoster(pastOrganized), ""))
	res, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01"})
	if res.IsError {
		t.Fatal(allResultText(t, res))
	}
	result := out.(participantSearchResult)
	if result.TruncatedRecords || strings.Contains(result.Note, "record cap") {
		t.Errorf("hidden records must not count toward the cap: truncated=%v note=%q", result.TruncatedRecords, result.Note)
	}
	if got := uids(participantsOf(t, out)); got != "host-11 att-11 self-11" {
		t.Errorf("the organized meeting must be drained in full, got %q", got)
	}
	if *result.Meetings != 1 || *result.Records != 3 {
		t.Errorf("meetings=%d records=%d", *result.Meetings, *result.Records)
	}
}

func TestParticipants_DedupeFalseProjectsRawRecords(t *testing.T) {
	api := setupParticipantTest(t)
	api.GrantRelations()
	api.Respond(resourcesPath, pastDocs(pastPublic))
	api.Respond(resourcesPath, page(meetingRoster(pastPublic), ""))
	_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastPublic, Dedupe: boolPtrT(false)})
	result := out.(participantSearchResult)
	if uids(participantsOf(t, out)) != "host-22 self-22" || result.People != nil {
		t.Errorf("raw records are projected the same way: %v", result.Resources)
	}
}

func TestParticipants_OwnRecordMatchesByUsername(t *testing.T) {
	api := setupParticipantTest(t)
	api.GrantRelations()
	api.Respond(resourcesPath, pastDocs(pastHidden))
	api.Respond(resourcesPath, page([]string{
		participantDocFor("mine", pastHidden, "other-address@example.test", "Stub", "User", stubCallerUsername, false, true),
		participantDocFor("theirs", pastHidden, "att@example.test", "Atty", "A", "someone-else", false, true),
	}, ""))
	_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastHidden})
	if got := participantsOf(t, out); uids(got) != "mine" || got[0]["email"] != "other-address@example.test" {
		t.Errorf("own record by username must be returned in full: %v", got)
	}
}

func TestParticipants_ScopeWithoutAMeetingOrRangeIsRefused(t *testing.T) {
	// LFX Self Serve has no cross-meeting participant list for a caller who
	// does not organize the meetings, so a page over a project or group
	// would only be read to be emptied page by page.
	for _, args := range []SearchPastMeetingParticipantsArgs{
		{ProjectUID: "P1"},
		{CommitteeUID: memberOfGroup},
		{},
		{ProjectUID: "P1", CountOnly: true},
		{ProjectUID: "P1", CountOnly: true, MaxMeetings: 1000000},
	} {
		api := setupParticipantTest(t)
		res, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), args)
		if !res.IsError || strings.TrimSpace(allResultText(t, res)) != participantScopeRefusal || out != nil || len(api.Requests()) != 0 {
			t.Errorf("%+v: expected the scope refusal before any call, got %s", args, allResultText(t, res))
		}
	}
	// Full view keeps the plain page.
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page(meetingRoster(pastPublic), ""))
	res, _, _ := handleSearchPastMeetingParticipants(fullViewCtx(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1"})
	if res.IsError {
		t.Fatalf("full view must keep project-wide pages: %s", allResultText(t, res))
	}
}

func TestParticipants_DateRangeAppliesTheRulePerMeeting(t *testing.T) {
	api := setupParticipantTest(t)
	api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
	api.Respond(resourcesPath, page([]string{pastMeetingDoc(pastOrganized), pastMeetingDoc(pastPublic), pastMeetingDoc(pastHidden)}, "")) // resolve ids
	api.Respond(resourcesPath, pastDocs(pastOrganized, pastPublic, pastHidden))                                                           // record lookup
	api.Respond(resourcesPath, page(meetingRoster(pastOrganized), ""))                                                                    // drains
	api.Respond(resourcesPath, page(meetingRoster(pastPublic), ""))
	api.Respond(resourcesPath, page(meetingRoster(pastHidden), ""))
	res, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01", DateTo: "2026-06-30"})
	if res.IsError {
		t.Fatal(allResultText(t, res))
	}
	if got := uids(participantsOf(t, out)); got != "host-11 att-11 self-11 host-22 self-22 self-55" {
		t.Errorf("records = %q", got)
	}
	result := out.(participantSearchResult)
	if result.Meetings == nil || *result.Meetings != 3 || *result.People != 6 || *result.Records != 6 {
		t.Errorf("totals must describe what is returned: meetings=%v people=%v records=%v", result.Meetings, result.People, result.Records)
	}
	if len(api.AccessCheckBodies()) != 1 {
		t.Error("one relation batch for every resolved meeting")
	}
}

func TestParticipants_DateRangeMeetingsTotalCountsShownMeetingsOnly(t *testing.T) {
	// The caller has no own record in the hidden meeting, so it contributes
	// nothing and is not counted.
	api := setupParticipantTest(t)
	api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
	api.Respond(resourcesPath, page([]string{pastMeetingDoc(pastOrganized), pastMeetingDoc(pastHidden)}, ""))
	api.Respond(resourcesPath, pastDocs(pastOrganized, pastHidden))
	api.Respond(resourcesPath, page(meetingRoster(pastOrganized), ""))
	api.Respond(resourcesPath, page([]string{participantDocFor("att-55", pastHidden, "att55@example.test", "Atty", "A", "", false, true)}, ""))
	_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01"})
	result := out.(participantSearchResult)
	if result.Meetings == nil || *result.Meetings != 1 || *result.Records != 3 || len(result.Warnings) != 0 {
		t.Errorf("meetings=%v records=%v warnings=%v", result.Meetings, result.Records, result.Warnings)
	}
}

func TestParticipants_MaxMeetingsValidatedOnTheCountPath(t *testing.T) {
	api := setupParticipantTest(t)
	res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01", CountOnly: true, MaxMeetings: participantHardMaxMeetings + 1})
	if !res.IsError || !strings.Contains(allResultText(t, res), "max_meetings") || len(api.Requests()) != 0 {
		t.Fatalf("expected the max_meetings refusal before any call, got %s", allResultText(t, res))
	}
}

func TestParticipants_PersonFiltersNeedOrganizedMeetings(t *testing.T) {
	t.Run("project scope without a range", func(t *testing.T) {
		api := setupParticipantTest(t)
		res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", Name: "Hosty"})
		if !res.IsError || strings.TrimSpace(allResultText(t, res)) != participantScopeRefusal || len(api.Requests()) != 0 {
			t.Fatalf("expected the refusal before any call, got %s", allResultText(t, res))
		}
	})
	t.Run("org_name on a public meeting", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, pastDocs(pastPublic))
		res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastPublic, OrgName: "Example Org"})
		if !res.IsError || strings.TrimSpace(allResultText(t, res)) != participantFilterRefusal {
			t.Fatalf("expected the refusal, got %s", allResultText(t, res))
		}
		if len(api.RequestsTo(resourcesPath)) != 1 {
			t.Error("no participant data may be read when the filter is refused")
		}
	})
	t.Run("name on an organized meeting", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
		api.Respond(resourcesPath, pastDocs(pastOrganized))
		api.Respond(resourcesPath, page(meetingRoster(pastOrganized), ""))
		res, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastOrganized, Name: "Hosty"})
		if res.IsError || len(participantsOf(t, out)) != 3 {
			t.Fatalf("an organizer keeps the filter: %s", allResultText(t, res))
		}
	})
	t.Run("date range with one unorganized meeting", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
		api.Respond(resourcesPath, page([]string{pastMeetingDoc(pastOrganized), pastMeetingDoc(pastPublic)}, ""))
		api.Respond(resourcesPath, pastDocs(pastOrganized, pastPublic))
		res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01", Name: "Hosty"})
		if !res.IsError || strings.TrimSpace(allResultText(t, res)) != participantFilterRefusal {
			t.Fatalf("expected the refusal, got %s", allResultText(t, res))
		}
		if len(api.RequestsTo(resourcesPath)) != 2 {
			t.Error("the drain must not start when the filter is refused")
		}
	})
}

func TestParticipants_CountOnlyFollowsTheView(t *testing.T) {
	t.Run("single public meeting counts", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, pastDocs(pastPublic))
		api.Respond(countPath, `{"count": 9, "has_more": false}`)
		res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastPublic, CountOnly: true, AttendedOnly: true})
		if res.IsError || resultJSON(t, res)["count"] != float64(9) {
			t.Fatalf("expected the count, got %s", allResultText(t, res))
		}
	})
	t.Run("single hidden meeting is refused, not zero", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, pastDocs(pastHidden))
		res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastHidden, CountOnly: true})
		if !res.IsError || strings.TrimSpace(allResultText(t, res)) != participantCountNotShownMessage || len(api.RequestsTo(countPath)) != 0 {
			t.Fatalf("expected the refusal and no count call, got %s", allResultText(t, res))
		}
	})
	t.Run("date range counts the shown meetings only", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
		api.Respond(resourcesPath, page([]string{pastMeetingDoc(pastOrganized), pastMeetingDoc(pastPublic), pastMeetingDoc(pastHidden)}, ""))
		api.Respond(resourcesPath, pastDocs(pastOrganized, pastPublic, pastHidden))
		api.Respond(countPath, `{"count": 5, "has_more": false}`)
		api.Respond(countPath, `{"count": 7, "has_more": false}`)
		res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01", CountOnly: true})
		if res.IsError {
			t.Fatal(allResultText(t, res))
		}
		out := resultJSON(t, res)
		if out["count"] != float64(12) || out["complete"] != true {
			t.Errorf("count = %v complete = %v", out["count"], out["complete"])
		}
		if note, _ := out["note"].(string); !strings.Contains(note, strings.TrimSpace(participantCountScopeNote)) {
			t.Errorf("note must say which meetings are counted: %q", note)
		}
		counts := api.RequestsTo(countPath)
		if len(counts) != 2 || counts[0].Query.Get("parent") != "past_meeting:"+pastOrganized || counts[1].Query.Get("parent") != "past_meeting:"+pastPublic {
			t.Errorf("only shown meetings are counted: %+v", counts)
		}
	})
	t.Run("full view keeps the single count call", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.Respond(countPath, `{"count": 3, "has_more": false}`)
		res, _, _ := handleSearchPastMeetingParticipants(fullViewCtx(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", CountOnly: true})
		if res.IsError || len(api.Requests()) != 1 {
			t.Fatalf("full view counts the scope in one call: %s", allResultText(t, res))
		}
		if note, _ := resultJSON(t, res)["note"].(string); strings.Contains(note, strings.TrimSpace(participantCountScopeNote)) {
			t.Error("full view carries no scope note")
		}
	})
}

func TestParticipants_FailsClosed(t *testing.T) {
	cases := []struct {
		name  string
		args  SearchPastMeetingParticipantsArgs
		setup func(api *stubLFXAPI)
	}{
		{"access-check 503 on a single meeting", SearchPastMeetingParticipantsArgs{PastMeetingID: pastPublic}, func(api *stubLFXAPI) {
			api.FailAccessCheck(http.StatusServiceUnavailable)
			api.Respond(resourcesPath, pastDocs(pastPublic))
		}},
		{"record lookup error on a single meeting", SearchPastMeetingParticipantsArgs{PastMeetingID: pastPublic}, func(api *stubLFXAPI) {
			api.GrantRelations()
			api.RespondStatus(resourcesPath, http.StatusBadGateway, "")
		}},
		{"access-check 503 under a date range", SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01"}, func(api *stubLFXAPI) {
			api.FailAccessCheck(http.StatusServiceUnavailable)
			api.Respond(resourcesPath, page([]string{pastMeetingDoc(pastPublic)}, ""))
			api.Respond(resourcesPath, pastDocs(pastPublic))
		}},
		{"access-check 503 on a count", SearchPastMeetingParticipantsArgs{PastMeetingID: pastPublic, CountOnly: true}, func(api *stubLFXAPI) {
			api.FailAccessCheck(http.StatusServiceUnavailable)
			api.Respond(resourcesPath, pastDocs(pastPublic))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := setupParticipantTest(t)
			tc.setup(api)
			res, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), tc.args)
			if !res.IsError || strings.TrimSpace(allResultText(t, res)) != peopleVisibilityUnavailableMessage || out != nil {
				t.Fatalf("expected the unavailable error and no records, got %s / %v", allResultText(t, res), out)
			}
			if len(api.RequestsTo(countPath)) != 0 {
				t.Error("no count may run after a predicate failure")
			}
		})
	}
}

func TestGetPastMeetingParticipant_Views(t *testing.T) {
	t.Run("full access host stub", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, page([]string{participantDocFor("h", pastPublic, "host@example.test", "Hosty", "H", "", true, true)}, ""))
		api.Respond(resourcesPath, pastDocs(pastPublic))
		res, _, _ := handleGetPastMeetingParticipant(context.Background(), stubCallToolRequest(), GetPastMeetingParticipantArgs{UID: "h"})
		if res.IsError {
			t.Fatal(allResultText(t, res))
		}
		assertNoEmail(t, allResultText(t, res))
		if data := resultJSON(t, res)["Data"].(map[string]any); data["host"] != true || data["first_name"] != "Hosty" || data["job_title"] != nil {
			t.Errorf("host stub wrong: %v", data)
		}
	})
	t.Run("full access non-host reads as not visible", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, page([]string{participantDocFor("a", pastPublic, "att@example.test", "Atty", "A", "", false, true)}, ""))
		api.Respond(resourcesPath, pastDocs(pastPublic))
		res, _, _ := handleGetPastMeetingParticipant(context.Background(), stubCallToolRequest(), GetPastMeetingParticipantArgs{UID: "a"})
		if !res.IsError || strings.TrimSpace(allResultText(t, res)) != lookupNotVisibleMessage("past meeting participant", "a") {
			t.Fatalf("expected the not-visible message, got %s", allResultText(t, res))
		}
	})
	t.Run("own record in a hidden meeting is returned in full", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, page([]string{participantDocFor("me", pastHidden, strings.ToUpper(stubCallerEmail), "Stub", "U", "", false, true)}, ""))
		api.Respond(resourcesPath, pastDocs(pastHidden))
		res, _, _ := handleGetPastMeetingParticipant(context.Background(), stubCallToolRequest(), GetPastMeetingParticipantArgs{UID: "me"})
		if res.IsError || resultJSON(t, res)["Data"].(map[string]any)["sessions"] == nil {
			t.Fatalf("own record (matched case-insensitively) is unchanged: %s", allResultText(t, res))
		}
	})
	t.Run("fails closed", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.FailAccessCheck(http.StatusBadGateway)
		api.Respond(resourcesPath, page([]string{participantDocFor("h", pastPublic, "host@example.test", "Hosty", "H", "", true, true)}, ""))
		api.Respond(resourcesPath, pastDocs(pastPublic))
		res, _, _ := handleGetPastMeetingParticipant(context.Background(), stubCallToolRequest(), GetPastMeetingParticipantArgs{UID: "h"})
		if !res.IsError || strings.TrimSpace(allResultText(t, res)) != peopleVisibilityUnavailableMessage {
			t.Fatalf("expected the unavailable error, got %s", allResultText(t, res))
		}
	})
}

func TestParticipants_QueriesAreNarrowedToWhatIsShown(t *testing.T) {
	// The participant query itself is limited to the records the caller may
	// be shown, so no page is read only to be emptied and a page token never
	// spans records the caller is not shown.
	ownIdentity := "username:" + stubCallerUsername + " email:" + stubCallerEmail + " email:" + strings.ToLower(stubCallerEmail)
	t.Run("own-only meeting reads the caller's records only", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, pastDocs(pastHidden))
		api.Respond(resourcesPath, page([]string{participantDocFor("me", pastHidden, stubCallerEmail, "Stub", "U", "", false, true)}, "tok"))
		_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastHidden, AttendedOnly: true})
		q := api.RequestsTo(resourcesPath)[1].Query
		if strings.Join(q["filters_or"], " ") != ownIdentity {
			t.Errorf("filters_or = %v", q["filters_or"])
		}
		if q["tags"][0] != "is_attended:true" {
			t.Errorf("the attended tag stays a separate clause: %v", q["tags"])
		}
		if result := out.(participantSearchResult); result.PageToken == nil || *result.PageToken != "tok" || uids(participantsOf(t, out)) != "me" {
			t.Errorf("the narrowed query's own page token and records are returned: %+v", result)
		}
	})
	t.Run("full-access meeting reads hosts and the caller's records", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, pastDocs(pastPublic))
		api.Respond(resourcesPath, page(meetingRoster(pastPublic), ""))
		handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastPublic}) //nolint:errcheck // query shape only
		q := api.RequestsTo(resourcesPath)[1].Query
		if strings.Join(q["filters_or"], " ") != "host:true "+ownIdentity {
			t.Errorf("filters_or = %v", q["filters_or"])
		}
	})
	t.Run("organizer reads the meeting unnarrowed", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
		api.Respond(resourcesPath, pastDocs(pastOrganized))
		api.Respond(resourcesPath, page(meetingRoster(pastOrganized), ""))
		handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: pastOrganized}) //nolint:errcheck // query shape only
		if q := api.RequestsTo(resourcesPath)[1].Query; len(q["filters_or"]) != 0 {
			t.Errorf("organizer query must not be narrowed: %v", q["filters_or"])
		}
	})
	t.Run("no identity and own-only: nothing is read", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, pastDocs(pastHidden))
		req := stubCallToolRequest()
		delete(req.Extra.TokenInfo.Extra, "username")
		delete(req.Extra.TokenInfo.Extra, ClaimEmail)
		res, out, _ := handleSearchPastMeetingParticipants(context.Background(), req, SearchPastMeetingParticipantsArgs{PastMeetingID: pastHidden})
		if res.IsError {
			t.Fatal(allResultText(t, res))
		}
		result := out.(participantSearchResult)
		if len(result.Resources) != 0 || result.PageToken != nil || len(api.RequestsTo(resourcesPath)) != 1 {
			t.Errorf("expected an empty page, no token and no participant query: %+v / %d queries", result, len(api.RequestsTo(resourcesPath)))
		}
		if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "not proof of absence") {
			t.Errorf("the empty page keeps the standard warning: %v", result.Warnings)
		}
	})
	t.Run("date range, no identity: own-only meetings are not read at all", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
		api.Respond(resourcesPath, page([]string{pastMeetingDoc(pastOrganized), pastMeetingDoc(pastHidden)}, ""))
		api.Respond(resourcesPath, pastDocs(pastOrganized, pastHidden))
		api.Respond(resourcesPath, page(meetingRoster(pastOrganized), ""))
		req := stubCallToolRequest()
		delete(req.Extra.TokenInfo.Extra, "username")
		delete(req.Extra.TokenInfo.Extra, ClaimEmail)
		_, out, _ := handleSearchPastMeetingParticipants(context.Background(), req, SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01"})
		if n := len(api.RequestsTo(resourcesPath)); n != 3 {
			t.Errorf("expected the range, the docs and one drain, got %d queries", n)
		}
		if result := out.(participantSearchResult); *result.Meetings != 1 || *result.Records != 3 {
			t.Errorf("meetings=%d records=%d", *result.Meetings, *result.Records)
		}
	})
	t.Run("date range: hidden meetings are read narrowed, organized ones in full", func(t *testing.T) {
		api := setupParticipantTest(t)
		api.GrantRelations("v1_past_meeting:" + pastOrganized + "#organizer")
		api.Respond(resourcesPath, page([]string{pastMeetingDoc(pastOrganized), pastMeetingDoc(pastHidden)}, ""))
		api.Respond(resourcesPath, pastDocs(pastOrganized, pastHidden))
		api.Respond(resourcesPath, page(meetingRoster(pastOrganized), ""))
		api.Respond(resourcesPath, page([]string{participantDocFor("self-55", pastHidden, stubCallerEmail, "Stub", "U", "", false, true)}, ""))
		_, out, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01"})
		reqs := api.RequestsTo(resourcesPath)
		if len(reqs) != 4 || len(reqs[2].Query["filters_or"]) != 0 || strings.Join(reqs[3].Query["filters_or"], " ") != ownIdentity {
			t.Errorf("unexpected drain queries: %+v", reqs)
		}
		result := out.(participantSearchResult)
		if uids(participantsOf(t, out)) != "host-11 att-11 self-11 self-55" || *result.Meetings != 2 || *result.Records != 4 || *result.People != 4 {
			t.Errorf("records=%q meetings=%d records=%d people=%d", uids(participantsOf(t, out)), *result.Meetings, *result.Records, *result.People)
		}
	})
}

func TestParticipants_NonFullViewPathsChargeTheRequestBudget(t *testing.T) {
	// The view lookups and the per-meeting counts are charged to the same
	// request budget as the drains, so neither can fan out past it.
	lower := func(t *testing.T, n int) {
		t.Helper()
		prev := participantMaxRequests
		participantMaxRequests = n
		t.Cleanup(func() { participantMaxRequests = prev })
	}
	t.Run("count loop stops at the budget", func(t *testing.T) {
		lower(t, 2) // the range page and one view chunk, then nothing for the first count
		api := setupParticipantTest(t)
		api.GrantRelations("v1_past_meeting:"+pastOrganized+"#organizer", "v1_past_meeting:"+pastPublic+"#organizer")
		api.Respond(resourcesPath, page([]string{pastMeetingDoc(pastOrganized), pastMeetingDoc(pastPublic)}, ""))
		api.Respond(resourcesPath, pastDocs(pastOrganized, pastPublic))
		api.Respond(countPath, `{"count": 5, "has_more": false}`)
		api.Respond(countPath, `{"count": 7, "has_more": false}`)
		res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01", CountOnly: true})
		if !res.IsError || !strings.Contains(allResultText(t, res), "count_only") {
			t.Fatalf("expected the request-budget error, got %s", allResultText(t, res))
		}
		if n := len(api.RequestsTo(countPath)); n != 0 {
			t.Errorf("no count may run once the budget is spent, got %d", n)
		}
	})
	t.Run("view lookups are charged", func(t *testing.T) {
		lower(t, 1) // the range page; no view chunk left
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, page([]string{pastMeetingDoc(pastPublic)}, ""))
		api.Respond(resourcesPath, pastDocs(pastPublic))
		res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01"})
		if !res.IsError || !strings.Contains(allResultText(t, res), "count_only") {
			t.Fatalf("expected the request-budget error, got %s", allResultText(t, res))
		}
		if n := len(api.RequestsTo(accessCheckPath)) + len(api.RequestsTo(resourcesPath)) - 1; n != 0 {
			t.Errorf("neither the docs lookup nor a relation check may run once the budget is spent, got %d extra calls", n)
		}
	})
	t.Run("every page of a view lookup is charged", func(t *testing.T) {
		// The query service may return empty pages with a continuation
		// token; each one is a request the budget must count.
		lower(t, 3) // the range page and two lookup pages
		api := setupParticipantTest(t)
		api.GrantRelations()
		api.Respond(resourcesPath, page([]string{pastMeetingDoc(pastPublic)}, ""))
		for i := 0; i < peopleLookupMaxPages; i++ {
			api.Respond(resourcesPath, page(nil, "more"))
		}
		res, _, _ := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{ProjectUID: "P1", DateFrom: "2026-06-01"})
		if !res.IsError || !strings.Contains(allResultText(t, res), "count_only") {
			t.Fatalf("expected the request-budget error, got %s", allResultText(t, res))
		}
		if n := len(api.RequestsTo(resourcesPath)); n != participantMaxRequests {
			t.Errorf("must stop at exactly %d query-service requests, made %d", participantMaxRequests, n)
		}
		if n := len(api.RequestsTo(accessCheckPath)); n != 0 {
			t.Errorf("no relation check may run once the budget is spent, got %d", n)
		}
	})
}
