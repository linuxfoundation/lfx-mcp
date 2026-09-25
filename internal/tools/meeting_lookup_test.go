// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setupMeetingLookupTest points the shared meetingConfig at a stub LFX API.
func setupMeetingLookupTest(t *testing.T) *stubLFXAPI {
	t.Helper()
	api := newStubLFXAPI(t)
	prev := meetingConfig
	SetMeetingConfig(&MeetingConfig{Clients: api.Clients})
	t.Cleanup(func() { meetingConfig = prev })
	return api
}

// singleResourcePage wraps one synthetic index document of the given type in
// a one-record query-service page. Field names are synthetic test data.
func singleResourcePage(resourceType, id, data string) string {
	return `{"resources": [{"type": "` + resourceType + `", "id": "` + id + `", "data": ` + data + `}]}`
}

// meetingDocWithJoinFields is a synthetic v1_meeting record carrying every
// denied key at several depths plus the join-page password that must stay.
const meetingDocWithJoinFields = `{
  "id": "meeting-1",
  "title": "Weekly sync",
  "project_uid": "11111111-1111-1111-1111-111111111111",
  "password": "join-page-password",
  "join_url": "https://example.com/j/1?pwd=abc",
  "zoom_config": {"meeting_id": "1", "passcode": "abc", "ai_companion_enabled": true},
  "extra": {"nested": {"passcode": "deep", "keep": "yes"}},
  "occurrences": [
    {"occurrence_id": "o1", "join_url": "https://example.com/j/1?occ=o1", "start_time": "2026-06-10T15:00:00Z"},
    {"occurrence_id": "o2", "join_url": "https://example.com/j/1?occ=o2", "start_time": "2026-06-17T15:00:00Z"}
  ]
}`

// pastMeetingDocWithPasswords is a synthetic v1_past_meeting record carrying
// the unused recording password and the join-page password that must stay.
const pastMeetingDocWithPasswords = `{
  "id": "past-1",
  "uid": "past-1",
  "meeting_id": "meeting-1",
  "title": "Weekly sync",
  "meeting_password": "join-page-password",
  "recording_password": "rec-secret",
  "join_url": "https://example.com/j/1"
}`

// filterParams are the query-service parameters a tag lookup must not send.
var filterParams = []string{"filters", "filters_all", "filters_or", "tags"}

func assertNoParams(t *testing.T, r stubAPIRequest, params ...string) {
	t.Helper()
	for _, p := range params {
		if v, has := r.Query[p]; has {
			t.Errorf("unexpected query param %s=%v", p, v)
		}
	}
}

func TestGetMeeting_LooksUpByMeetingIDTag(t *testing.T) {
	api := setupMeetingLookupTest(t)
	api.Respond(resourcesPath, singleResourcePage("v1_meeting", "meeting-1", `{"id": "meeting-1", "title": "Weekly sync"}`))

	res, _, err := handleGetMeeting(context.Background(), stubCallToolRequest(), GetMeetingArgs{UID: "meeting-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}

	r := api.LastRequest()
	if r.Path != resourcesPath {
		t.Fatalf("expected %s, got %s", resourcesPath, r.Path)
	}
	assertExchangedAuth(t, r)
	if got := strings.Join(r.Query["tags_all"], "|"); got != "meeting_id:meeting-1" {
		t.Errorf("tags_all: want meeting_id:meeting-1, got %q", got)
	}
	if r.Query.Get("type") != "v1_meeting" || r.Query.Get("page_size") != "1" || r.Query.Get("v") != "1" {
		t.Errorf("unexpected payload: %v", r.Query)
	}
	assertNoParams(t, r, filterParams...)

	// The document has `id` and no `uid`; the tool must still return it.
	out := resultJSON(t, res)
	data, _ := out["Data"].(map[string]any)
	if data["id"] != "meeting-1" {
		t.Errorf("expected the record with id meeting-1, got %v", out)
	}
}

func TestGetPastMeetingSummary_LooksUpBySummaryIDTag(t *testing.T) {
	api := setupMeetingLookupTest(t)
	api.Respond(resourcesPath, singleResourcePage("v1_past_meeting_summary", "summary-1", `{"id": "summary-1", "content": "notes"}`))

	res, _, err := handleGetPastMeetingSummary(context.Background(), stubCallToolRequest(), GetPastMeetingSummaryArgs{UID: "summary-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}

	r := api.LastRequest()
	assertExchangedAuth(t, r)
	if got := strings.Join(r.Query["tags_all"], "|"); got != "past_meeting_summary_id:summary-1" {
		t.Errorf("tags_all: want past_meeting_summary_id:summary-1, got %q", got)
	}
	if r.Query.Get("type") != "v1_past_meeting_summary" || r.Query.Get("page_size") != "1" {
		t.Errorf("unexpected payload: %v", r.Query)
	}
	assertNoParams(t, r, filterParams...)
}

// The registrant and participant lookups match on the `uid` field and must
// keep doing so; this pins that the tag lookup did not spread to them.
func TestGetRegistrantAndParticipant_StillLookUpByUIDFilter(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  string
		call func() (*mcp.CallToolResult, any, error)
	}{
		{
			name: "get_meeting_registrant",
			typ:  "v1_meeting_registrant",
			call: func() (*mcp.CallToolResult, any, error) {
				return handleGetMeetingRegistrant(context.Background(), stubCallToolRequest(), GetMeetingRegistrantArgs{UID: "reg-1"})
			},
		},
		{
			name: "get_past_meeting_participant",
			typ:  "v1_past_meeting_participant",
			call: func() (*mcp.CallToolResult, any, error) {
				return handleGetPastMeetingParticipant(context.Background(), stubCallToolRequest(), GetPastMeetingParticipantArgs{UID: "reg-1"})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMeetingLookupTest(t)
			api.Respond(resourcesPath, singleResourcePage(tc.typ, "reg-1", `{"uid": "reg-1"}`))

			res, _, err := tc.call()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.IsError {
				t.Fatalf("unexpected error result: %s", allResultText(t, res))
			}

			r := api.LastRequest()
			if got := strings.Join(r.Query["filters"], "|"); got != "uid:reg-1" {
				t.Errorf("filters: want uid:reg-1, got %q", got)
			}
			if r.Query.Get("type") != tc.typ || r.Query.Get("page_size") != "1" || r.Query.Get("sort") != "name_asc" {
				t.Errorf("unexpected payload: %v", r.Query)
			}
			assertNoParams(t, r, "tags_all", "tags", "filters_all", "filters_or")
		})
	}
}

// containsKeyAtAnyDepth reports whether key appears anywhere in a decoded
// JSON value, walking maps and slices.
func containsKeyAtAnyDepth(v any, key string) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if k == key || containsKeyAtAnyDepth(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range x {
			if containsKeyAtAnyDepth(child, key) {
				return true
			}
		}
	}
	return false
}

func assertMeetingJoinFieldsTrimmed(t *testing.T, data map[string]any) {
	t.Helper()
	for _, key := range []string{"join_url", "passcode"} {
		if containsKeyAtAnyDepth(data, key) {
			t.Errorf("%s must not appear at any depth: %v", key, data)
		}
	}
	if data["password"] != "join-page-password" {
		t.Errorf("password must be unchanged, got %v", data["password"])
	}
	zoom, _ := data["zoom_config"].(map[string]any)
	if zoom["ai_companion_enabled"] != true {
		t.Errorf("sibling keys of a denied key must survive: %v", zoom)
	}
	occ, _ := data["occurrences"].([]any)
	if len(occ) != 2 {
		t.Fatalf("occurrences must be kept, got %v", data["occurrences"])
	}
	if occ[0].(map[string]any)["occurrence_id"] != "o1" {
		t.Errorf("occurrence fields must survive: %v", occ[0])
	}
}

func TestSearchMeetings_TrimsJoinFields(t *testing.T) {
	api := setupMeetingLookupTest(t)
	pinMeetingSearchNow(t, beforeJoinFieldsOccurrences)
	api.Respond(resourcesPath, singleResourcePage("v1_meeting", "meeting-1", meetingDocWithJoinFields))

	res, _, _ := handleSearchMeetings(context.Background(), stubCallToolRequest(), SearchMeetingsArgs{ProjectUID: "11111111-1111-1111-1111-111111111111"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	resources := resultJSON(t, res)["resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("expected one resource, got %d", len(resources))
	}
	assertMeetingJoinFieldsTrimmed(t, resources[0].(map[string]any)["Data"].(map[string]any))
}

func TestGetMeeting_TrimsJoinFields(t *testing.T) {
	api := setupMeetingLookupTest(t)
	api.Respond(resourcesPath, singleResourcePage("v1_meeting", "meeting-1", meetingDocWithJoinFields))

	res, _, _ := handleGetMeeting(context.Background(), stubCallToolRequest(), GetMeetingArgs{UID: "meeting-1"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	assertMeetingJoinFieldsTrimmed(t, resultJSON(t, res)["Data"].(map[string]any))
}

func TestSearchPastMeetings_TrimsRecordingPasswordKeepsMeetingPassword(t *testing.T) {
	api := setupMeetingLookupTest(t)
	api.Respond(resourcesPath, singleResourcePage("v1_past_meeting", "past-1", pastMeetingDocWithPasswords))

	res, _, _ := handleSearchPastMeetings(context.Background(), stubCallToolRequest(), SearchPastMeetingsArgs{MeetingID: "meeting-1"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	resources := resultJSON(t, res)["resources"].([]any)
	data := resources[0].(map[string]any)["Data"].(map[string]any)
	for _, key := range []string{"recording_password", "join_url"} {
		if _, has := data[key]; has {
			t.Errorf("%s must be removed: %v", key, data)
		}
	}
	if data["meeting_password"] != "join-page-password" {
		t.Errorf("meeting_password must be unchanged, got %v", data["meeting_password"])
	}
	if data["title"] != "Weekly sync" || data["meeting_id"] != "meeting-1" {
		t.Errorf("unrelated fields must survive: %v", data)
	}
}

func TestTrimMeetingResultFields_Helper(t *testing.T) {
	t.Run("non-map data is returned untouched", func(t *testing.T) {
		for _, in := range []any{nil, "text", 42.0, true} {
			if got := trimMeetingResultFields(in); got != in {
				t.Errorf("trim(%v) = %v", in, got)
			}
		}
		array := []any{map[string]any{"passcode": "kept", "join_url": "kept"}}
		want := []any{map[string]any{"passcode": "kept", "join_url": "kept"}}
		if got := trimMeetingResultFields(array); !reflect.DeepEqual(got, want) || !reflect.DeepEqual(array, want) {
			t.Errorf("top-level non-map Data must remain untouched: got %v, input %v", got, array)
		}
	})

	t.Run("exact key match only", func(t *testing.T) {
		var data map[string]any
		if err := json.Unmarshal([]byte(`{
		  "password": "p", "meeting_password": "mp", "passcode_hint": "h",
		  "join_url_label": "l", "Passcode": "cased", "recording_password": "r",
		  "host_key": "hk", "join_url": "u", "passcode": "c",
		  "list": [{"passcode": "x", "password": "y"}, "s", 1]
		}`), &data); err != nil {
			t.Fatal(err)
		}
		got := trimMeetingResultFields(data).(map[string]any)
		for _, keep := range []string{"password", "meeting_password", "passcode_hint", "join_url_label", "Passcode"} {
			if _, has := got[keep]; !has {
				t.Errorf("%s must survive: %v", keep, got)
			}
		}
		for _, gone := range []string{"recording_password", "host_key", "join_url", "passcode"} {
			if _, has := got[gone]; has {
				t.Errorf("%s must be removed: %v", gone, got)
			}
		}
		list := got["list"].([]any)
		item := list[0].(map[string]any)
		if _, has := item["passcode"]; has {
			t.Errorf("nested list item passcode must be removed: %v", item)
		}
		if item["password"] != "y" || list[1] != "s" || list[2] != 1.0 {
			t.Errorf("list contents must otherwise survive: %v", list)
		}
	})
}
