// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// occurrenceTestNow is the pinned request time for the occurrence tests.
var occurrenceTestNow = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

// beforeJoinFieldsOccurrences is a request time before every occurrence in
// meetingDocWithJoinFields, so none of them has ended.
var beforeJoinFieldsOccurrences = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// pinMeetingSearchNow fixes the search_meetings clock for one test.
func pinMeetingSearchNow(t *testing.T, now time.Time) {
	t.Helper()
	prev := meetingSearchNow
	meetingSearchNow = func() time.Time { return now }
	t.Cleanup(func() { meetingSearchNow = prev })
}

// decodeJSONObject decodes a JSON object the way the query service client
// does, so numbers are float64.
func decodeJSONObject(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("bad test JSON %s: %v", s, err)
	}
	return m
}

// assertJSONEqual fails unless got marshals to the same JSON value as want.
func assertJSONEqual(t *testing.T, got any, want string) {
	t.Helper()
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var g, w any
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want JSON %s: %v", want, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("want %s\n got %s", want, raw)
	}
}

// occ is a synthetic occurrence with a start and an optional duration.
func occ(id, start string, duration int) string {
	if duration < 0 {
		return `{"occurrence_id": "` + id + `", "start_time": "` + start + `"}`
	}
	d, _ := json.Marshal(duration)
	return `{"occurrence_id": "` + id + `", "start_time": "` + start + `", "duration": ` + string(d) + `}`
}

// occList is a JSON array of occurrence documents.
func occList(items ...string) string {
	return "[" + strings.Join(items, ", ") + "]"
}

func TestOccurrenceWindowFor(t *testing.T) {
	ts := func(s string) *time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return &v
	}
	for _, tc := range []struct {
		name string
		args SearchMeetingsArgs
		want occurrenceWindow
	}{
		{"no range", SearchMeetingsArgs{}, occurrenceWindow{}},
		{"date_field without a range", SearchMeetingsArgs{DateField: "start_time"}, occurrenceWindow{}},
		{"date-only range on the default field", SearchMeetingsArgs{DateFrom: "2026-07-06", DateTo: "2026-07-12"},
			occurrenceWindow{from: ts("2026-07-06T00:00:00Z"), to: ts("2026-07-12T23:59:59Z")}},
		{"RFC 3339 range on start_time", SearchMeetingsArgs{DateField: "start_time", DateFrom: "2026-07-06T09:00:00Z", DateTo: "2026-07-06T10:00:00+02:00"},
			occurrenceWindow{from: ts("2026-07-06T09:00:00Z"), to: ts("2026-07-06T08:00:00Z")}},
		{"range on occurrences.start_time", SearchMeetingsArgs{DateField: "occurrences.start_time", DateFrom: "2026-07-06", DateTo: "2026-07-12"},
			occurrenceWindow{from: ts("2026-07-06T00:00:00Z"), to: ts("2026-07-12T23:59:59Z")}},
		{"date_from only", SearchMeetingsArgs{DateFrom: "2026-07-06"}, occurrenceWindow{from: ts("2026-07-06T00:00:00Z")}},
		{"date_to only", SearchMeetingsArgs{DateTo: "2026-07-12"}, occurrenceWindow{to: ts("2026-07-12T23:59:59Z")}},
		{"range on another date field", SearchMeetingsArgs{DateField: "updated_at", DateFrom: "2026-07-06", DateTo: "2026-07-12"}, occurrenceWindow{}},
		{"unparseable bounds", SearchMeetingsArgs{DateFrom: "soon", DateTo: "later"}, occurrenceWindow{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := occurrenceWindowFor(tc.args)
			if got.set() != tc.want.set() {
				t.Fatalf("set: want %v, got %v", tc.want.set(), got.set())
			}
			for _, b := range []struct {
				name      string
				got, want *time.Time
			}{{"from", got.from, tc.want.from}, {"to", got.to, tc.want.to}} {
				if (b.got == nil) != (b.want == nil) || (b.got != nil && !b.got.Equal(*b.want)) {
					t.Errorf("%s: want %v, got %v", b.name, b.want, b.got)
				}
			}
		})
	}
}

func TestFitMeetingOccurrences(t *testing.T) {
	noRange := occurrenceWindow{}

	for _, tc := range []struct {
		name        string
		data        string
		window      occurrenceWindow
		wantData    string
		wantOmitted int
	}{
		{
			name: "no range keeps the next upcoming occurrences in index order",
			data: `{"title": "Weekly", "occurrences": ` + occList(
				occ("o0", "2026-06-24T12:00:00Z", 60),
				occ("o1", "2026-07-01T11:30:00Z", 60),
				occ("o2", "2026-07-08T11:30:00Z", 60),
				occ("o3", "2026-07-15T11:30:00Z", 60),
				occ("o4", "2026-07-22T11:30:00Z", 60),
				occ("o5", "2026-07-29T11:30:00Z", 60),
				occ("o6", "2026-08-05T11:30:00Z", 60),
				occ("o7", "2026-08-12T11:30:00Z", 60),
			) + `}`,
			window: noRange,
			wantData: `{"title": "Weekly", "occurrences_omitted": 3, "occurrences": ` + occList(
				occ("o1", "2026-07-01T11:30:00Z", 60),
				occ("o2", "2026-07-08T11:30:00Z", 60),
				occ("o3", "2026-07-15T11:30:00Z", 60),
				occ("o4", "2026-07-22T11:30:00Z", 60),
				occ("o5", "2026-07-29T11:30:00Z", 60),
			) + `}`,
			wantOmitted: 3,
		},
		{
			name: "ended boundary uses start plus duration plus the buffer, with fallbacks",
			data: `{"duration": 60, "occurrences": ` + occList(
				occ("own-at", "2026-07-01T10:20:00Z", 60),
				occ("own-past", "2026-07-01T10:19:59Z", 60),
				occ("meeting-at", "2026-07-01T10:20:00Z", -1),
				occ("meeting-past", "2026-07-01T10:19:59Z", -1),
			) + `}`,
			window: noRange,
			wantData: `{"duration": 60, "occurrences_omitted": 2, "occurrences": ` + occList(
				occ("own-at", "2026-07-01T10:20:00Z", 60),
				occ("meeting-at", "2026-07-01T10:20:00Z", -1),
			) + `}`,
			wantOmitted: 2,
		},
		{
			name: "no duration anywhere counts as zero",
			data: `{"occurrences": ` + occList(
				occ("at", "2026-07-01T11:20:00Z", -1),
				occ("past", "2026-07-01T11:19:59Z", -1),
			) + `}`,
			window:      noRange,
			wantData:    `{"occurrences_omitted": 1, "occurrences": ` + occList(occ("at", "2026-07-01T11:20:00Z", -1)) + `}`,
			wantOmitted: 1,
		},
		{
			name: "date-only range is inclusive through the end of date_to",
			data: `{"occurrences": ` + occList(
				occ("before", "2026-07-05T23:59:59Z", 60),
				occ("at-from", "2026-07-06T00:00:00Z", 60),
				occ("at-to", "2026-07-12T23:59:59Z", 60),
				occ("after", "2026-07-13T00:00:00Z", 60),
			) + `}`,
			window: occurrenceWindowFor(SearchMeetingsArgs{DateFrom: "2026-07-06", DateTo: "2026-07-12"}),
			wantData: `{"occurrences_omitted": 2, "occurrences": ` + occList(
				occ("at-from", "2026-07-06T00:00:00Z", 60),
				occ("at-to", "2026-07-12T23:59:59Z", 60),
			) + `}`,
			wantOmitted: 2,
		},
		{
			name: "RFC 3339 range is inclusive at both bounds",
			data: `{"occurrences": ` + occList(
				occ("before", "2026-07-06T08:59:59Z", 30),
				occ("at-from", "2026-07-06T09:00:00Z", 30),
				occ("at-to", "2026-07-06T10:00:00Z", 30),
				occ("after", "2026-07-06T10:00:01Z", 30),
			) + `}`,
			window: occurrenceWindowFor(SearchMeetingsArgs{DateFrom: "2026-07-06T09:00:00Z", DateTo: "2026-07-06T10:00:00Z"}),
			wantData: `{"occurrences_omitted": 2, "occurrences": ` + occList(
				occ("at-from", "2026-07-06T09:00:00Z", 30),
				occ("at-to", "2026-07-06T10:00:00Z", 30),
			) + `}`,
			wantOmitted: 2,
		},
		{
			name: "ended occurrences inside the range are left out",
			data: `{"occurrences": ` + occList(
				occ("ended", "2026-06-10T15:00:00Z", 60),
				occ("upcoming", "2026-07-08T15:00:00Z", 60),
			) + `}`,
			window:      occurrenceWindowFor(SearchMeetingsArgs{DateFrom: "2026-06-01", DateTo: "2026-07-31"}),
			wantData:    `{"occurrences_omitted": 1, "occurrences": ` + occList(occ("upcoming", "2026-07-08T15:00:00Z", 60)) + `}`,
			wantOmitted: 1,
		},
		{
			name: "date_from only keeps every upcoming occurrence from it, with no limit",
			data: `{"occurrences": ` + occList(
				occ("early", "2026-07-02T15:00:00Z", 60),
				occ("a", "2026-07-06T15:00:00Z", 60),
				occ("b", "2026-07-07T15:00:00Z", 60),
				occ("c", "2026-07-08T15:00:00Z", 60),
				occ("d", "2026-07-09T15:00:00Z", 60),
				occ("e", "2026-07-10T15:00:00Z", 60),
				occ("f", "2026-07-11T15:00:00Z", 60),
			) + `}`,
			window: occurrenceWindowFor(SearchMeetingsArgs{DateFrom: "2026-07-06"}),
			wantData: `{"occurrences_omitted": 1, "occurrences": ` + occList(
				occ("a", "2026-07-06T15:00:00Z", 60),
				occ("b", "2026-07-07T15:00:00Z", 60),
				occ("c", "2026-07-08T15:00:00Z", 60),
				occ("d", "2026-07-09T15:00:00Z", 60),
				occ("e", "2026-07-10T15:00:00Z", 60),
				occ("f", "2026-07-11T15:00:00Z", 60),
			) + `}`,
			wantOmitted: 1,
		},
		{
			name: "every indexed occurrence ended: an empty list, not null",
			data: `{"occurrences": ` + occList(
				occ("a", "2026-06-10T15:00:00Z", 60),
				occ("b", "2026-06-17T15:00:00Z", 60),
			) + `}`,
			window:      noRange,
			wantData:    `{"occurrences_omitted": 2, "occurrences": []}`,
			wantOmitted: 2,
		},
		{
			name: "entries whose start cannot be read are kept, not counted, and take a slot",
			data: `{"occurrences": ` + occList(
				`"not-an-object"`,
				`{"occurrence_id": "bad", "start_time": "next tuesday"}`,
				`{"occurrence_id": "none"}`,
				occ("ended", "2026-06-10T15:00:00Z", 60),
				occ("u1", "2026-07-08T15:00:00Z", 60),
				occ("u2", "2026-07-15T15:00:00Z", 60),
				occ("u3", "2026-07-22T15:00:00Z", 60),
				occ("u4", "2026-07-29T15:00:00Z", 60),
			) + `}`,
			window: noRange,
			wantData: `{"occurrences_omitted": 3, "occurrences": ` + occList(
				`"not-an-object"`,
				`{"occurrence_id": "bad", "start_time": "next tuesday"}`,
				`{"occurrence_id": "none"}`,
				occ("u1", "2026-07-08T15:00:00Z", 60),
				occ("u2", "2026-07-15T15:00:00Z", 60),
			) + `}`,
			wantOmitted: 3,
		},
		{
			name: "kept occurrences keep every field, cancelled ones included, and other meeting fields are untouched",
			data: `{
			  "title": "Weekly", "start_time": "2026-01-07T15:00:00Z", "duration": 60,
			  "recurrence": {"type": 2, "weekly_days": "4"},
			  "cancelled_occurrences": ["c1"],
			  "occurrences": [
			    {"occurrence_id": "e1", "start_time": "2026-06-24T15:00:00Z", "duration": 60},
			    {"occurrence_id": "c1", "start_time": "2026-07-08T15:00:00Z", "duration": 60, "is_cancelled": true,
			     "title": "Weekly", "description": "Agenda", "recurrence": {"type": 2}, "registrant_count": 4, "response_count_yes": 2}
			  ]
			}`,
			window: noRange,
			wantData: `{
			  "title": "Weekly", "start_time": "2026-01-07T15:00:00Z", "duration": 60,
			  "recurrence": {"type": 2, "weekly_days": "4"},
			  "cancelled_occurrences": ["c1"],
			  "occurrences_omitted": 1,
			  "occurrences": [
			    {"occurrence_id": "c1", "start_time": "2026-07-08T15:00:00Z", "duration": 60, "is_cancelled": true,
			     "title": "Weekly", "description": "Agenda", "recurrence": {"type": 2}, "registrant_count": 4, "response_count_yes": 2}
			  ]
			}`,
			wantOmitted: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := decodeJSONObject(t, tc.data)
			got := fitMeetingOccurrences([]searchResource{{Type: "v1_meeting", ID: "m1", Data: data}}, tc.window, occurrenceTestNow)
			if got != tc.wantOmitted {
				t.Errorf("omitted: want %d, got %d", tc.wantOmitted, got)
			}
			assertJSONEqual(t, data, tc.wantData)
		})
	}
}

func TestFitMeetingOccurrences_KeptValuesAreTheOriginals(t *testing.T) {
	data := decodeJSONObject(t, `{"occurrences": `+occList(
		occ("ended", "2026-06-10T15:00:00Z", 60),
		`{"occurrence_id": "k", "start_time": "2026-07-08T15:00:00Z", "duration": 60, "title": "T", "is_cancelled": false}`,
	)+`}`)
	original := data["occurrences"].([]any)[1]
	want := decodeJSONObject(t, `{"occurrence_id": "k", "start_time": "2026-07-08T15:00:00Z", "duration": 60, "title": "T", "is_cancelled": false}`)

	fitMeetingOccurrences([]searchResource{{Data: data}}, occurrenceWindow{}, occurrenceTestNow)
	kept := data["occurrences"].([]any)
	if len(kept) != 1 || !reflect.DeepEqual(kept[0], want) || !reflect.DeepEqual(kept[0], original) {
		t.Errorf("the kept occurrence must be the original value with every field, got %v", kept)
	}
}

func TestFitMeetingOccurrences_UnchangedMeetings(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{"one-time meeting without occurrences", `{"title": "Kickoff", "start_time": "2026-06-10T15:00:00Z", "duration": 60}`},
		{"null occurrences", `{"title": "Kickoff", "occurrences": null}`},
		{"empty occurrences", `{"title": "Kickoff", "occurrences": []}`},
		{"occurrences that is not a list", `{"title": "Kickoff", "occurrences": {"a": 1}}`},
		{"nothing to leave out", `{"title": "Weekly", "occurrences": ` + occList(occ("a", "2026-07-08T15:00:00Z", 60), occ("b", "2026-07-15T15:00:00Z", 60)) + `}`},
		{"data that is not an object", `{"_raw": [{"occurrences": [{"start_time": "2026-06-10T15:00:00Z"}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := decodeJSONObject(t, tc.data)
			if got := fitMeetingOccurrences([]searchResource{{Data: data}}, occurrenceWindow{}, occurrenceTestNow); got != 0 {
				t.Errorf("omitted: want 0, got %d", got)
			}
			assertJSONEqual(t, data, tc.data)
		})
	}

	if got := fitMeetingOccurrences([]searchResource{{Type: "v1_meeting", ID: "m"}}, occurrenceWindow{}, occurrenceTestNow); got != 0 {
		t.Errorf("a resource with no Data must pass through, got %d", got)
	}
}

func TestOccurrenceNote(t *testing.T) {
	if strings.ContainsAny(occurrenceNote, "0123456789") {
		t.Errorf("the note must carry no numbers: %q", occurrenceNote)
	}
	for _, banned := range []string{"Insights", "WARNING:", "exist", "withheld"} {
		if strings.Contains(occurrenceNote, banned) {
			t.Errorf("the note must not contain %q: %q", banned, occurrenceNote)
		}
	}
	for _, required := range []string{"get_meeting", "occurrences_omitted", "date_from", "date_to"} {
		if !strings.Contains(occurrenceNote, required) {
			t.Errorf("the note must name %s: %q", required, occurrenceNote)
		}
	}
}

// recurringMeetingDoc is a synthetic v1_meeting record with one ended
// occurrence and seven upcoming weekly ones at occurrenceTestNow.
const recurringMeetingDoc = `{
  "id": "meeting-1",
  "title": "Weekly sync",
  "start_time": "2026-01-07T15:00:00Z",
  "duration": 60,
  "recurrence": {"type": 2, "weekly_days": "4"},
  "cancelled_occurrences": ["o3"],
  "occurrences": [
    {"occurrence_id": "o0", "start_time": "2026-06-24T15:00:00Z", "duration": 60},
    {"occurrence_id": "o1", "start_time": "2026-07-01T15:00:00Z", "duration": 60},
    {"occurrence_id": "o2", "start_time": "2026-07-08T15:00:00Z", "duration": 60},
    {"occurrence_id": "o3", "start_time": "2026-07-15T15:00:00Z", "duration": 60, "is_cancelled": true},
    {"occurrence_id": "o4", "start_time": "2026-07-22T15:00:00Z", "duration": 60},
    {"occurrence_id": "o5", "start_time": "2026-07-29T15:00:00Z", "duration": 60},
    {"occurrence_id": "o6", "start_time": "2026-08-05T15:00:00Z", "duration": 60},
    {"occurrence_id": "o7", "start_time": "2026-08-12T15:00:00Z", "duration": 60}
  ]
}`

// assertTextEqualsStructured fails unless the one text block is the JSON of
// the structured output.
func assertTextEqualsStructured(t *testing.T, res *mcp.CallToolResult, out resourceSearchResult) {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("expected exactly one content block, got %d", len(res.Content))
	}
	assertJSONEqual(t, out, res.Content[0].(*mcp.TextContent).Text)
}

// occurrenceIDs lists the occurrence_id of each occurrence in meeting data.
func occurrenceIDs(t *testing.T, data map[string]any) []string {
	t.Helper()
	list, ok := data["occurrences"].([]any)
	if !ok {
		t.Fatalf("occurrences must be a list, got %v", data["occurrences"])
	}
	ids := make([]string, 0, len(list))
	for _, o := range list {
		ids = append(ids, o.(map[string]any)["occurrence_id"].(string))
	}
	return ids
}

// assertMeetingFieldsUnchanged fails unless every field of the indexed
// document other than the occurrence list is returned as indexed.
func assertMeetingFieldsUnchanged(t *testing.T, data map[string]any, doc string) {
	t.Helper()
	want := decodeJSONObject(t, doc)
	for k, v := range want {
		if k == "occurrences" {
			continue
		}
		if !reflect.DeepEqual(data[k], v) {
			t.Errorf("%s must be unchanged: want %v, got %v", k, v, data[k])
		}
	}
}

func TestSearchMeetings_OccurrencesFitDateRange(t *testing.T) {
	api := setupMeetingLookupTest(t)
	pinMeetingSearchNow(t, occurrenceTestNow)
	api.Respond(resourcesPath, singleResourcePage("v1_meeting", "meeting-1", recurringMeetingDoc))

	res, out, err := handleSearchMeetings(context.Background(), stubCallToolRequest(), SearchMeetingsArgs{
		CommitteeUID: "c1", DateFrom: "2026-07-06", DateTo: "2026-07-19",
	})
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v %s", err, allResultText(t, res))
	}

	wantQuery := url.Values{
		"v": {"1"}, "type": {"v1_meeting"}, "parent": {"committee:c1"},
		"date_field": {"start_time"}, "date_from": {"2026-07-06"}, "date_to": {"2026-07-19"},
		"sort": {"name_asc"}, "page_size": {"10"},
	}
	if got := api.LastRequest().Query; !reflect.DeepEqual(got, wantQuery) {
		t.Errorf("the query must be unchanged:\nwant %v\n got %v", wantQuery, got)
	}

	data := out.Resources[0].Data
	if got, want := occurrenceIDs(t, data), []string{"o2", "o3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("occurrences: want %v, got %v", want, got)
	}
	if data["occurrences_omitted"] != 6 {
		t.Errorf("occurrences_omitted: want 6, got %v", data["occurrences_omitted"])
	}
	assertMeetingFieldsUnchanged(t, data, recurringMeetingDoc)
	if !reflect.DeepEqual(out.Warnings, []string{occurrenceNote}) {
		t.Errorf("warnings: want only the occurrence note, got %q", out.Warnings)
	}
	assertTextEqualsStructured(t, res, out)
}

func TestSearchMeetings_OccurrencesWithoutRange(t *testing.T) {
	api := setupMeetingLookupTest(t)
	pinMeetingSearchNow(t, occurrenceTestNow)
	second := strings.Replace(recurringMeetingDoc, `"meeting-1"`, `"meeting-2"`, 1)
	api.Respond(resourcesPath, page([]string{
		`{"type": "v1_meeting", "id": "meeting-1", "data": ` + recurringMeetingDoc + `}`,
		`{"type": "v1_meeting", "id": "meeting-2", "data": ` + second + `}`,
	}, "next"))

	res, out, err := handleSearchMeetings(context.Background(), stubCallToolRequest(), SearchMeetingsArgs{ProjectUID: "p", PageSize: 5})
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v %s", err, allResultText(t, res))
	}
	if _, has := api.LastRequest().Query["date_field"]; has {
		t.Errorf("no date_field is sent without a range: %v", api.LastRequest().Query)
	}
	docs := map[string]string{"meeting-1": recurringMeetingDoc, "meeting-2": second}
	if len(out.Resources) != len(docs) {
		t.Fatalf("expected %d meetings, got %d", len(docs), len(out.Resources))
	}
	for _, r := range out.Resources {
		if got, want := occurrenceIDs(t, r.Data), []string{"o1", "o2", "o3", "o4", "o5"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s occurrences: want %v, got %v", r.ID, want, got)
		}
		if r.Data["occurrences_omitted"] != 3 {
			t.Errorf("%s occurrences_omitted: want 3, got %v", r.ID, r.Data["occurrences_omitted"])
		}
		assertMeetingFieldsUnchanged(t, r.Data, docs[r.ID])
	}
	want := append(searchWarnings("meetings", 2, 5, true, false), occurrenceNote)
	if len(want) != 2 || !reflect.DeepEqual(out.Warnings, want) {
		t.Errorf("warnings: want the access warning then one occurrence note, got %q", out.Warnings)
	}
	textWarnings(t, res, out.Warnings)
	assertTextEqualsStructured(t, res, out)
}

func TestSearchMeetings_OccurrencesGroupMode(t *testing.T) {
	api := setupMeetingLookupTest(t)
	pinMeetingSearchNow(t, occurrenceTestNow)
	api.Respond(resourcesPath, singleResourcePage("v1_meeting", "meeting-1", recurringMeetingDoc))

	res, out, err := handleSearchMeetingsGroupMode(context.Background(), stubCallToolRequest(), SearchMeetingsGroupArgs{
		GroupUID: "g1", DateFrom: "2026-07-06", DateTo: "2026-07-19",
	})
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v %s", err, allResultText(t, res))
	}
	q := api.LastRequest().Query
	if q.Get("parent") != "committee:g1" || q.Get("date_field") != "start_time" {
		t.Errorf("group mode must search the group's meetings on start_time: %v", q)
	}
	data := out.Resources[0].Data
	if got, want := occurrenceIDs(t, data), []string{"o2", "o3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("occurrences: want %v, got %v", want, got)
	}
	if data["occurrences_omitted"] != 6 || !reflect.DeepEqual(out.Warnings, []string{occurrenceNote}) {
		t.Errorf("group mode must report the omitted occurrences: %v %q", data["occurrences_omitted"], out.Warnings)
	}
	assertTextEqualsStructured(t, res, out)
}

func TestSearchMeetings_NothingOmitted(t *testing.T) {
	api := setupMeetingLookupTest(t)
	pinMeetingSearchNow(t, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	doc := `{"id": "meeting-1", "title": "Weekly", "occurrences": ` + occList(
		occ("a", "2026-06-10T15:00:00Z", 60), occ("b", "2026-06-17T15:00:00Z", 60),
	) + `}`
	api.Respond(resourcesPath, singleResourcePage("v1_meeting", "meeting-1", doc))

	res, out, _ := handleSearchMeetings(context.Background(), stubCallToolRequest(), SearchMeetingsArgs{ProjectUID: "p"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	body := resultJSON(t, res)
	if _, has := body["warnings"]; has {
		t.Errorf("no warnings key when nothing was omitted: %s", allResultText(t, res))
	}
	assertJSONEqual(t, out.Resources[0].Data, doc)
}

func TestGetMeeting_KeepsFullOccurrenceList(t *testing.T) {
	api := setupMeetingLookupTest(t)
	pinMeetingSearchNow(t, occurrenceTestNow)
	api.Respond(resourcesPath, singleResourcePage("v1_meeting", "meeting-1", recurringMeetingDoc))

	res, _, err := handleGetMeeting(context.Background(), stubCallToolRequest(), GetMeetingArgs{UID: "meeting-1"})
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v %s", err, allResultText(t, res))
	}
	data := resultJSON(t, res)["Data"].(map[string]any)
	assertJSONEqual(t, data, recurringMeetingDoc)
}
