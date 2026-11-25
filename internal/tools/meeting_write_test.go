// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	meetingservice "github.com/linuxfoundation/lfx-v2-meeting-service/gen/meeting_service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const writeMeetingID = "91461158520"

// storedMeeting is what the fake meeting service holds for writeMeetingID.
// It carries call credentials so tests can check they are never echoed.
const storedMeeting = `{
  "id": "91461158520",
  "project_uid": "proj-1",
  "title": "AAIF Security WG",
  "start_time": "2026-12-01T16:00:00Z",
  "duration": 60,
  "timezone": "America/New_York",
  "visibility": "public",
  "description": "Weekly sync",
  "restricted": true,
  "meeting_type": "Technical",
  "committees": [{"uid": "7cad5a8d-19d0-41a4-81a6-043453daf9ee"}],
  "recording_enabled": true,
  "recurrence": {"type": 2, "repeat_interval": 1, "weekly_days": "3"},
  "passcode": "123456",
  "password": "00000000-0000-4000-8000-000000000001",
  "public_link": "https://zoom-lfx.platform.linuxfoundation.org/meeting/91461158520"
}`

func setupMeetingWriteTest(t *testing.T) *stubLFXAPI {
	t.Helper()
	api := newStubLFXAPI(t)
	prev := meetingConfig
	SetMeetingConfig(&MeetingConfig{Clients: api.Clients})
	t.Cleanup(func() { meetingConfig = prev })
	return api
}

func requestBody(t *testing.T, r stubAPIRequest) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatalf("request body is not JSON: %v\n%s", err, r.Body)
	}
	return body
}

func assertNoCredentials(t *testing.T, res *mcp.CallToolResult) {
	t.Helper()
	text := allResultText(t, res)
	for _, secret := range []string{"123456", "00000000-0000-4000-8000-000000000001", "passcode", "password"} {
		if strings.Contains(text, secret) {
			t.Errorf("result must not echo %q:\n%s", secret, text)
		}
	}
}

func intPtr(i int) *int { return &i }

func TestCreateMeeting_SendsPayloadAndHidesCredentials(t *testing.T) {
	api := setupMeetingWriteTest(t)
	api.RespondStatus("/itx/meetings", http.StatusCreated, storedMeeting)

	res, _, err := handleCreateMeeting(fullViewCtx(), stubCallToolRequest(), CreateMeetingArgs{
		ProjectUID:    "proj-1",
		Title:         "AAIF Security WG",
		StartTime:     "2026-12-01T16:00:00Z",
		Duration:      60,
		Timezone:      "America/New_York",
		Visibility:    "public",
		CommitteeUIDs: []string{"7cad5a8d-19d0-41a4-81a6-043453daf9ee"},
		MeetingSettingsArgs: MeetingSettingsArgs{
			MeetingType: strPtr("Technical"),
			Recurrence:  &MeetingRecurrenceArgs{Type: 2, WeeklyDays: strPtr("3")},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("create failed: err=%v result=%s", err, allResultText(t, res))
	}

	r := api.LastRequest()
	if r.Method != http.MethodPost || r.Path != "/itx/meetings" {
		t.Fatalf("want POST /itx/meetings, got %s %s", r.Method, r.Path)
	}
	assertExchangedAuth(t, r)
	body := requestBody(t, r)
	for k, want := range map[string]any{"project_uid": "proj-1", "title": "AAIF Security WG", "duration": float64(60), "visibility": "public", "meeting_type": "Technical"} {
		if body[k] != want {
			t.Errorf("body[%s] = %v, want %v", k, body[k], want)
		}
	}
	if cs, _ := body["committees"].([]any); len(cs) != 1 {
		t.Errorf("want one committee in body, got %v", body["committees"])
	}
	if !strings.Contains(allResultText(t, res), `"meeting_id": "91461158520"`) {
		t.Errorf("result should carry the new meeting ID:\n%s", allResultText(t, res))
	}
	assertNoCredentials(t, res)
}

func TestCreateMeeting_GroupModeMapsGroupUIDs(t *testing.T) {
	api := setupMeetingWriteTest(t)
	api.RespondStatus("/itx/meetings", http.StatusCreated, storedMeeting)

	res, _, _ := handleCreateMeetingGroupMode(fullViewCtx(), stubCallToolRequest(), CreateMeetingGroupArgs{
		ProjectUID: "proj-1", Title: "T", StartTime: "2026-12-01T16:00:00Z", Duration: 30,
		Timezone: "UTC", Visibility: "private", GroupUIDs: []string{"g-1", "g-2"},
	})
	if res.IsError {
		t.Fatalf("create failed: %s", allResultText(t, res))
	}
	if cs, _ := requestBody(t, api.LastRequest())["committees"].([]any); len(cs) != 2 {
		t.Errorf("group_uids must become committees, got %v", cs)
	}
}

func TestCreateMeeting_RejectsBadInputWithoutCallingAPI(t *testing.T) {
	valid := CreateMeetingArgs{ProjectUID: "p", Title: "T", StartTime: "2026-12-01T16:00:00Z", Duration: 30, Timezone: "UTC", Visibility: "public"}
	cases := map[string]func(a *CreateMeetingArgs){
		"missing project":   func(a *CreateMeetingArgs) { a.ProjectUID = "" },
		"zero duration":     func(a *CreateMeetingArgs) { a.Duration = 0 },
		"non-RFC3339 start": func(a *CreateMeetingArgs) { a.StartTime = "Dec 1 at 4pm" },
		"unknown timezone":  func(a *CreateMeetingArgs) { a.Timezone = "Eastern" },
		"bad visibility":    func(a *CreateMeetingArgs) { a.Visibility = "secret" },
		"bad meeting type":  func(a *CreateMeetingArgs) { a.MeetingType = strPtr("Standup") },
		"bad recurrence":    func(a *CreateMeetingArgs) { a.Recurrence = &MeetingRecurrenceArgs{Type: 9} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			api := setupMeetingWriteTest(t)
			args := valid
			mutate(&args)
			res, _, _ := handleCreateMeeting(fullViewCtx(), stubCallToolRequest(), args)
			if !res.IsError {
				t.Fatalf("want a tool error, got %s", allResultText(t, res))
			}
			if n := len(api.Requests()); n != 0 {
				t.Errorf("invalid input must not reach the API, saw %d requests", n)
			}
		})
	}
}

func TestUpdateMeeting_KeepsUnchangedFields(t *testing.T) {
	api := setupMeetingWriteTest(t)
	path := "/itx/meetings/" + writeMeetingID
	api.Handle(http.MethodGet, path, func(stubAPIRequest) stubAPIResponse {
		return stubAPIResponse{Status: http.StatusOK, Body: storedMeeting}
	})
	api.Handle(http.MethodPut, path, func(stubAPIRequest) stubAPIResponse {
		return stubAPIResponse{Status: http.StatusNoContent}
	})

	res, _, err := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), UpdateMeetingArgs{
		MeetingID:  writeMeetingID,
		StartTime:  strPtr("2026-12-01T17:00:00Z"),
		UpdateNote: strPtr("Moved an hour later"),
		MeetingSettingsArgs: MeetingSettingsArgs{
			RecordingEnabled: boolPtr(false),
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("update failed: err=%v result=%s", err, allResultText(t, res))
	}

	var put *stubAPIRequest
	for _, r := range api.RequestsTo(path) {
		if r.Method == http.MethodPut {
			r := r
			put = &r
		}
	}
	if put == nil {
		t.Fatal("want a PUT to the meeting")
	}
	body := requestBody(t, *put)
	for k, want := range map[string]any{
		"start_time":        "2026-12-01T17:00:00Z", // changed
		"recording_enabled": false,                  // changed
		"update_note":       "Moved an hour later",  // passed through
		"title":             "AAIF Security WG",     // kept
		"duration":          float64(60),            // kept
		"timezone":          "America/New_York",     // kept
		"description":       "Weekly sync",          // kept
		"meeting_type":      "Technical",            // kept
	} {
		if body[k] != want {
			t.Errorf("PUT body[%s] = %v, want %v", k, body[k], want)
		}
	}
	if rec, _ := body["recurrence"].(map[string]any); rec == nil || rec["weekly_days"] != "3" {
		t.Errorf("recurrence must be kept, got %v", body["recurrence"])
	}
	if cs, _ := body["committees"].([]any); len(cs) != 1 {
		t.Errorf("committees must be kept, got %v", body["committees"])
	}
	if _, ok := body["owner"]; ok {
		t.Errorf("owner must be omitted so the service keeps the stored owner")
	}
	assertNoCredentials(t, res)
}

func TestUpdateMeeting_GetFailureMakesNoPut(t *testing.T) {
	api := setupMeetingWriteTest(t)
	api.RespondStatus("/itx/meetings/"+writeMeetingID, http.StatusNotFound, `{"message":"not found"}`)

	res, _, _ := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), UpdateMeetingArgs{
		MeetingID: writeMeetingID, Title: strPtr("New"),
	})
	if !res.IsError {
		t.Fatalf("want a tool error, got %s", allResultText(t, res))
	}
	for _, r := range api.Requests() {
		if r.Method == http.MethodPut {
			t.Fatal("must not PUT when the current meeting cannot be read")
		}
	}
}

func TestUpdateMeeting_RejectsEmptyTitleAndBadDuration(t *testing.T) {
	for name, args := range map[string]UpdateMeetingArgs{
		"empty title":   {MeetingID: writeMeetingID, Title: strPtr("  ")},
		"zero duration": {MeetingID: writeMeetingID, Duration: intPtr(0)},
		"no meeting id": {Title: strPtr("x")},
	} {
		t.Run(name, func(t *testing.T) {
			api := setupMeetingWriteTest(t)
			res, _, _ := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), args)
			if !res.IsError {
				t.Fatalf("want a tool error, got %s", allResultText(t, res))
			}
			if n := len(api.Requests()); n != 0 {
				t.Errorf("invalid input must not reach the API, saw %d requests", n)
			}
		})
	}
}

func TestDeleteMeeting_DeletesByID(t *testing.T) {
	api := setupMeetingWriteTest(t)
	path := "/itx/meetings/" + writeMeetingID
	api.Handle(http.MethodDelete, path, func(stubAPIRequest) stubAPIResponse {
		return stubAPIResponse{Status: http.StatusNoContent}
	})

	res, _, err := handleDeleteMeeting(fullViewCtx(), stubCallToolRequest(), DeleteMeetingArgs{MeetingID: writeMeetingID})
	if err != nil || res.IsError {
		t.Fatalf("delete failed: err=%v result=%s", err, allResultText(t, res))
	}
	r := api.LastRequest()
	if r.Method != http.MethodDelete || r.Path != path {
		t.Fatalf("want DELETE %s, got %s %s", path, r.Method, r.Path)
	}
	assertExchangedAuth(t, r)
}

func TestDeleteMeeting_RequiresID(t *testing.T) {
	api := setupMeetingWriteTest(t)
	res, _, _ := handleDeleteMeeting(fullViewCtx(), stubCallToolRequest(), DeleteMeetingArgs{})
	if !res.IsError {
		t.Fatal("want a tool error for a missing meeting_id")
	}
	if n := len(api.Requests()); n != 0 {
		t.Errorf("must not call the API, saw %d requests", n)
	}
}

func TestMeetingWriteTools_AreManageScoped(t *testing.T) {
	for _, name := range []string{"create_meeting", "update_meeting", "delete_meeting", "add_meeting_registrant"} {
		if !ManageScopeTools[name] {
			t.Errorf("%s mutates data and must be in ManageScopeTools", name)
		}
	}
}

// privateMeeting is shaped like what the live service returned for a
// private, one-time meeting with reminders off: empty public_link,
// password and timestamps, reminder time stored as 0, and an empty
// recurrence. The generated client rejects all of these as-is.
const privateMeeting = `{
  "id": "91550996118",
  "project_uid": "proj-1",
  "title": "lfx-mcp write test",
  "start_time": "2026-11-19T16:00:00Z",
  "duration": 30,
  "timezone": "America/New_York",
  "visibility": "private",
  "restricted": true,
  "meeting_type": "Other",
  "auto_email_reminder_enabled": false,
  "auto_email_reminder_time": 0,
  "early_join_time_minutes": 10,
  "recurrence": {"type": 0},
  "public_link": "",
  "password": "",
  "created_at": "",
  "next_occurrence_start_time": "",
  "owner": {"username": "dawnwages", "email": "", "profile_picture": ""}
}`

func TestCreateMeeting_PrivateMeetingReplyIsReadable(t *testing.T) {
	api := setupMeetingWriteTest(t)
	api.RespondStatus("/itx/meetings", http.StatusCreated, privateMeeting)

	res, _, err := handleCreateMeeting(fullViewCtx(), stubCallToolRequest(), CreateMeetingArgs{
		ProjectUID: "proj-1", Title: "lfx-mcp write test", StartTime: "2026-11-19T16:00:00Z",
		Duration: 30, Timezone: "America/New_York", Visibility: "private",
	})
	if err != nil || res.IsError {
		t.Fatalf("a created private meeting must be reported as created: %s", allResultText(t, res))
	}
	if !strings.Contains(allResultText(t, res), `"meeting_id": "91550996118"`) {
		t.Errorf("result should carry the meeting ID:\n%s", allResultText(t, res))
	}
}

func TestUpdateMeeting_PrivateMeetingOmitsUnsetFields(t *testing.T) {
	api := setupMeetingWriteTest(t)
	path := "/itx/meetings/91550996118"
	api.Handle(http.MethodGet, path, func(stubAPIRequest) stubAPIResponse {
		return stubAPIResponse{Status: http.StatusOK, Body: privateMeeting}
	})
	api.Handle(http.MethodPut, path, func(stubAPIRequest) stubAPIResponse {
		return stubAPIResponse{Status: http.StatusNoContent}
	})

	res, _, err := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), UpdateMeetingArgs{
		MeetingID: "91550996118", Title: strPtr("lfx-mcp write test (updated)"),
	})
	if err != nil || res.IsError {
		t.Fatalf("update of a private meeting failed: %s", allResultText(t, res))
	}
	var put *stubAPIRequest
	for _, r := range api.RequestsTo(path) {
		if r.Method == http.MethodPut {
			r := r
			put = &r
		}
	}
	if put == nil {
		t.Fatal("want a PUT to the meeting")
	}
	body := requestBody(t, *put)
	if body["title"] != "lfx-mcp write test (updated)" || body["visibility"] != "private" || body["duration"] != float64(30) {
		t.Errorf("PUT must carry the new title and keep the rest, got %v", body)
	}
	for _, k := range []string{"auto_email_reminder_time", "recurrence"} {
		if _, ok := body[k]; ok {
			t.Errorf("PUT must omit unset %s, got %v", k, body[k])
		}
	}
}

func TestCreateMeeting_UnreadableReplyWarnsAboutDuplicates(t *testing.T) {
	api := setupMeetingWriteTest(t)
	// A reply the cleanup cannot fix: an invalid visibility value.
	api.RespondStatus("/itx/meetings", http.StatusCreated, `{"id":"1","visibility":"secret"}`)

	res, _, _ := handleCreateMeeting(fullViewCtx(), stubCallToolRequest(), CreateMeetingArgs{
		ProjectUID: "p", Title: "T", StartTime: "2026-11-19T16:00:00Z", Duration: 30, Timezone: "UTC", Visibility: "private",
	})
	if !res.IsError {
		t.Fatal("want a tool error")
	}
	if !strings.Contains(allResultText(t, res), "may have created the meeting") {
		t.Errorf("must warn that the meeting may exist:\n%s", allResultText(t, res))
	}
}

func TestAddMeetingRegistrant_SendsDirectInvite(t *testing.T) {
	api := setupMeetingWriteTest(t)
	path := "/itx/meetings/" + writeMeetingID + "/registrants"
	api.RespondStatus(path, http.StatusCreated, `{"uid":"reg-1","type":"","email":"ada@example.org","first_name":"Ada","last_name":"Lovelace","created_at":""}`)

	res, _, err := handleAddMeetingRegistrant(fullViewCtx(), stubCallToolRequest(), AddMeetingRegistrantArgs{
		MeetingID: writeMeetingID, Email: " Ada@Example.org ", FirstName: "Ada", LastName: "Lovelace",
	})
	if err != nil || res.IsError {
		t.Fatalf("add registrant failed: %s", allResultText(t, res))
	}
	r := api.LastRequest()
	if r.Method != http.MethodPost || r.Path != path {
		t.Fatalf("want POST %s, got %s %s", path, r.Method, r.Path)
	}
	assertExchangedAuth(t, r)
	body := requestBody(t, r)
	if body["email"] != "ada@example.org" || body["first_name"] != "Ada" || body["last_name"] != "Lovelace" {
		t.Errorf("unexpected body %v", body)
	}
	if _, ok := body["host"]; ok {
		t.Error("the tool must never set host")
	}
	if !strings.Contains(allResultText(t, res), `"registrant_uid": "reg-1"`) {
		t.Errorf("result should carry the registrant UID:\n%s", allResultText(t, res))
	}
}

func TestAddMeetingRegistrant_RejectsMissingFields(t *testing.T) {
	for name, args := range map[string]AddMeetingRegistrantArgs{
		"no meeting": {Email: "a@b.c", FirstName: "A", LastName: "B"},
		"bad email":  {MeetingID: "1", Email: "ada", FirstName: "A", LastName: "B"},
		"no last":    {MeetingID: "1", Email: "a@b.c", FirstName: "A"},
	} {
		t.Run(name, func(t *testing.T) {
			api := setupMeetingWriteTest(t)
			res, _, _ := handleAddMeetingRegistrant(fullViewCtx(), stubCallToolRequest(), args)
			if !res.IsError {
				t.Fatal("want a tool error")
			}
			if n := len(api.Requests()); n != 0 {
				t.Errorf("must not call the API, saw %d requests", n)
			}
		})
	}
}

// --- Strict read before update_meeting's full-replace PUT ---

// recurringMeeting is a stored recurring meeting whose occurrences carry the
// title and description fields the meeting service returns per occurrence.
const recurringMeeting = `{
  "id": "91461158520",
  "project_uid": "proj-1",
  "title": "AAIF Security WG",
  "start_time": "2026-12-01T16:00:00Z",
  "duration": 60,
  "timezone": "America/New_York",
  "visibility": "public",
  "meeting_type": "Technical",
  "recurrence": {"type": 2, "repeat_interval": 1, "weekly_days": "3"},
  "occurrences": [
    {"occurrence_id": "1764604800000", "start_time": "2026-12-01T16:00:00Z", "duration": 60, "title": "AAIF Security WG", "description": "Weekly sync"},
    {"occurrence_id": "1765209600000", "start_time": "2026-12-08T16:00:00Z", "duration": 60, "title": "AAIF Security WG: planning", "description": "Roadmap"}
  ]
}`

// stubMeetingForUpdate serves getBodies in order for GET on the meeting (the
// last one repeats) and accepts the PUT. It returns the meeting's path.
func stubMeetingForUpdate(api *stubLFXAPI, meetingID string, getBodies ...string) string {
	path := "/itx/meetings/" + meetingID
	gets := 0
	api.Handle(http.MethodGet, path, func(stubAPIRequest) stubAPIResponse {
		body := getBodies[min(gets, len(getBodies)-1)]
		gets++
		return stubAPIResponse{Status: http.StatusOK, Body: body}
	})
	api.Handle(http.MethodPut, path, func(stubAPIRequest) stubAPIResponse {
		return stubAPIResponse{Status: http.StatusNoContent}
	})
	return path
}

// putRequests returns the PUT requests the fake saw for path.
func putRequests(api *stubLFXAPI, path string) []stubAPIRequest {
	var puts []stubAPIRequest
	for _, r := range api.RequestsTo(path) {
		if r.Method == http.MethodPut {
			puts = append(puts, r)
		}
	}
	return puts
}

// withJSONField returns the JSON object doc with one more top-level member.
func withJSONField(t *testing.T, doc, member string) string {
	t.Helper()
	i := strings.LastIndex(doc, "}")
	if i < 0 {
		t.Fatalf("not a JSON object: %s", doc)
	}
	return doc[:i] + "," + member + "}"
}

func TestUpdateMeeting_StrictReadRefusesUnknownTopLevelField(t *testing.T) {
	api := setupMeetingWriteTest(t)
	path := stubMeetingForUpdate(api, writeMeetingID, withJSONField(t, storedMeeting, `"brand_new": 1`))

	res, _, _ := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), UpdateMeetingArgs{
		MeetingID: writeMeetingID, Title: strPtr("Renamed"),
	})
	if !res.IsError {
		t.Fatalf("want a tool error, got %s", allResultText(t, res))
	}
	if text := allResultText(t, res); !strings.Contains(text, "does not know about") {
		t.Errorf("the error must carry the strict-decoding refusal, got:\n%s", text)
	}
	if n := len(putRequests(api, path)); n != 0 {
		t.Fatalf("an unknown field on the read must stop the update, saw %d PUT(s)", n)
	}
}

func TestUpdateMeeting_StrictReadRefusesUnknownNestedField(t *testing.T) {
	api := setupMeetingWriteTest(t)
	nested := strings.Replace(recurringMeeting, `"description": "Roadmap"`, `"description": "Roadmap", "brand_new": true`, 1)
	if nested == recurringMeeting {
		t.Fatal("fixture was not changed")
	}
	path := stubMeetingForUpdate(api, writeMeetingID, nested)

	res, _, _ := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), UpdateMeetingArgs{
		MeetingID: writeMeetingID, Title: strPtr("Renamed"),
	})
	if !res.IsError {
		t.Fatalf("want a tool error, got %s", allResultText(t, res))
	}
	if text := allResultText(t, res); !strings.Contains(text, "does not know about") {
		t.Errorf("the error must carry the strict-decoding refusal, got:\n%s", text)
	}
	if n := len(putRequests(api, path)); n != 0 {
		t.Fatalf("an unknown nested field on the read must stop the update, saw %d PUT(s)", n)
	}
}

// The cleaning before the client's validation removes empty strings and a
// recurrence that is not one. An unknown field inside what it removes must
// still stop the update.
func TestUpdateMeeting_StrictReadRefusesUnknownFieldTheCleaningRemoves(t *testing.T) {
	inOccurrence := strings.Replace(recurringMeeting, `"description": "Roadmap"`, `"description": "Roadmap", "brand_new": ""`, 1)
	if inOccurrence == recurringMeeting {
		t.Fatal("fixture was not changed")
	}
	for name, stored := range map[string]string{
		"empty string at the top level":    withJSONField(t, storedMeeting, `"brand_new": ""`),
		"empty string in an occurrence":    inOccurrence,
		"inside a one-time's recurrence":   withJSONField(t, storedMeeting, `"recurrence": {"type": 0, "brand_new": 1}`),
		"empty string in a private record": withJSONField(t, privateMeeting, `"brand_new": ""`),
		"beside a number too large":        withJSONField(t, storedMeeting, `"recurrence": {"type": 0, "brand_new": 1e400}`),
		"in a member given twice":          withJSONField(t, storedMeeting, `"recurrence": {"type": 0, "brand_new": 1}, "recurrence": {"type": 0}`),
	} {
		t.Run(name, func(t *testing.T) {
			api := setupMeetingWriteTest(t)
			path := stubMeetingForUpdate(api, writeMeetingID, stored)

			res, _, _ := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), UpdateMeetingArgs{
				MeetingID: writeMeetingID, Title: strPtr("Renamed"),
			})
			if !res.IsError {
				t.Fatalf("want a tool error, got %s", allResultText(t, res))
			}
			if text := allResultText(t, res); !strings.Contains(text, "does not know about") {
				t.Errorf("the error must carry the strict-decoding refusal, got:\n%s", text)
			}
			if n := len(putRequests(api, path)); n != 0 {
				t.Fatalf("an unknown field on the read must stop the update, saw %d PUT(s)", n)
			}
		})
	}
}

func TestUpdateMeeting_StrictReadAcceptsOccurrenceTitleAndDescription(t *testing.T) {
	api := setupMeetingWriteTest(t)
	path := stubMeetingForUpdate(api, writeMeetingID, recurringMeeting)

	res, _, err := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), UpdateMeetingArgs{
		MeetingID: writeMeetingID, Title: strPtr("Renamed"),
	})
	if err != nil || res.IsError {
		t.Fatalf("a recurring meeting whose occurrences carry title and description must update: err=%v result=%s", err, allResultText(t, res))
	}
	puts := putRequests(api, path)
	if len(puts) != 1 {
		t.Fatalf("want one PUT, saw %d", len(puts))
	}
	if body := requestBody(t, puts[0]); body["title"] != "Renamed" {
		t.Errorf("PUT must carry the new title, got %v", body["title"])
	}
}

func TestUpdateMeeting_StrictReadCleansBeforeDecoding(t *testing.T) {
	api := setupMeetingWriteTest(t)
	// privateMeeting carries public_link "" and auto_email_reminder_time 0,
	// which the generated client rejects unless the record is cleaned first.
	path := stubMeetingForUpdate(api, "91550996118", privateMeeting)

	res, _, err := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), UpdateMeetingArgs{
		MeetingID: "91550996118", Title: strPtr("Renamed"),
	})
	if err != nil || res.IsError {
		t.Fatalf("the record must be cleaned before the strict decode: err=%v result=%s", err, allResultText(t, res))
	}
	if n := len(putRequests(api, path)); n != 1 {
		t.Fatalf("want one PUT, saw %d", n)
	}
}

func TestUpdateMeeting_ReadBackIsTolerant(t *testing.T) {
	api := setupMeetingWriteTest(t)
	// The read before the PUT is clean; the read-back after it carries a
	// field this client does not know.
	path := stubMeetingForUpdate(api, writeMeetingID, storedMeeting, withJSONField(t, storedMeeting, `"brand_new": 1`))

	res, _, err := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), UpdateMeetingArgs{
		MeetingID: writeMeetingID, Title: strPtr("Renamed"),
	})
	if err != nil || res.IsError {
		t.Fatalf("a saved update must be reported as saved whatever the read-back carries: err=%v result=%s", err, allResultText(t, res))
	}
	if n := len(putRequests(api, path)); n != 1 {
		t.Fatalf("want one PUT, saw %d", n)
	}
	if !strings.Contains(allResultText(t, res), "Meeting updated.") {
		t.Errorf("want the update headline, got:\n%s", allResultText(t, res))
	}
}

// --- The attendee-sharing rule LFX Self Serve's server applies on write ---

// attendeesInBody reports the show_meeting_attendees member of a request
// body: its value, and whether it was sent at all.
func attendeesInBody(t *testing.T, r stubAPIRequest) (value, sent bool) {
	t.Helper()
	raw, sent := requestBody(t, r)["show_meeting_attendees"]
	if !sent {
		return false, false
	}
	value, ok := raw.(bool)
	if !ok {
		t.Fatalf("show_meeting_attendees must be a boolean, got %v", raw)
	}
	return value, true
}

func TestCreateMeeting_AttendeeSharingRule(t *testing.T) {
	cases := []struct {
		name        string
		meetingType *string
		restricted  *bool
		show        *bool
		wantSent    bool
		wantValue   bool
		wantNote    bool
	}{
		{name: "board with an explicit yes", meetingType: strPtr("Board"), show: boolPtr(true), wantSent: true, wantValue: false, wantNote: true},
		{name: "board with no choice", meetingType: strPtr("Board"), wantSent: true, wantValue: false},
		{name: "board with an explicit no", meetingType: strPtr("Board"), show: boolPtr(false), wantSent: true, wantValue: false},
		{name: "restricted with no choice", meetingType: strPtr("Technical"), restricted: boolPtr(true), wantSent: true, wantValue: false},
		{name: "restricted with an explicit yes", meetingType: strPtr("Technical"), restricted: boolPtr(true), show: boolPtr(true), wantSent: true, wantValue: true},
		{name: "restricted board with an explicit yes", meetingType: strPtr("Board"), restricted: boolPtr(true), show: boolPtr(true), wantSent: true, wantValue: false, wantNote: true},
		{name: "technical with an explicit yes", meetingType: strPtr("Technical"), show: boolPtr(true), wantSent: true, wantValue: true},
		{name: "nothing set", wantSent: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMeetingWriteTest(t)
			api.RespondStatus("/itx/meetings", http.StatusCreated, storedMeeting)

			res, _, err := handleCreateMeeting(fullViewCtx(), stubCallToolRequest(), CreateMeetingArgs{
				ProjectUID: "proj-1", Title: "T", StartTime: "2026-12-01T16:00:00Z", Duration: 30,
				Timezone: "UTC", Visibility: "private",
				MeetingSettingsArgs: MeetingSettingsArgs{
					MeetingType: tc.meetingType, Restricted: tc.restricted, ShowMeetingAttendees: tc.show,
				},
			})
			if err != nil || res.IsError {
				t.Fatalf("create failed: err=%v result=%s", err, allResultText(t, res))
			}
			value, sent := attendeesInBody(t, api.LastRequest())
			if sent != tc.wantSent || value != tc.wantValue {
				t.Errorf("show_meeting_attendees sent=%v value=%v, want sent=%v value=%v", sent, value, tc.wantSent, tc.wantValue)
			}
			if got := strings.Contains(allResultText(t, res), attendeeSharingNotKeptNote); got != tc.wantNote {
				t.Errorf("note present=%v, want %v:\n%s", got, tc.wantNote, allResultText(t, res))
			}
		})
	}
}

func TestIsBoardMeetingType_NormalizesLikeSelfServe(t *testing.T) {
	for value, want := range map[string]bool{"Board": true, "board": true, " Board ": true, "BOARD": true, "Technical": false, "None": false, "": false, "Boards": false} {
		if got := isBoardMeetingType(strPtr(value)); got != want {
			t.Errorf("isBoardMeetingType(%q) = %v, want %v", value, got, want)
		}
	}
	if isBoardMeetingType(nil) {
		t.Error("a meeting with no type is not a Board meeting")
	}
}

// meetingWith returns storedMeeting with its meeting_type replaced and
// show_meeting_attendees set to shared.
func meetingWith(t *testing.T, meetingType string, shared bool) string {
	t.Helper()
	doc := strings.Replace(storedMeeting, `"meeting_type": "Technical"`, `"meeting_type": `+string(mustJSON(t, meetingType)), 1)
	if doc == storedMeeting && meetingType != "Technical" {
		t.Fatal("fixture was not changed")
	}
	return withJSONField(t, doc, `"show_meeting_attendees": `+string(mustJSON(t, shared)))
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

// The stored types here are the spellings the meeting client accepts on a
// read; isBoardMeetingType's own test covers the other spellings.
func TestUpdateMeeting_AttendeeSharingRule(t *testing.T) {
	cases := []struct {
		name       string
		storedType string
		storedShow bool
		argType    *string
		argShow    *bool
		wantValue  bool
		wantNote   bool
	}{
		{name: "stored board, nothing about sharing", storedType: "Board", storedShow: true, wantValue: false},
		{name: "stored board, explicit yes", storedType: "Board", argShow: boolPtr(true), wantValue: false, wantNote: true},
		{name: "leaves board with no choice", storedType: "Board", storedShow: true, argType: strPtr("Technical"), wantValue: false},
		{name: "leaves board with an explicit yes", storedType: "Board", argType: strPtr("Technical"), argShow: boolPtr(true), wantValue: true},
		{name: "technical keeps its stored yes", storedType: "Technical", storedShow: true, wantValue: true},
		{name: "technical keeps its stored no", storedType: "Technical", storedShow: false, wantValue: false},
		{name: "becomes board with a stored yes", storedType: "Technical", storedShow: true, argType: strPtr("Board"), wantValue: false},
		{name: "becomes board with an explicit yes", storedType: "Technical", argType: strPtr("Board"), argShow: boolPtr(true), wantValue: false, wantNote: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMeetingWriteTest(t)
			path := stubMeetingForUpdate(api, writeMeetingID, meetingWith(t, tc.storedType, tc.storedShow))

			res, _, err := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), UpdateMeetingArgs{
				MeetingID: writeMeetingID, Title: strPtr("Renamed"),
				MeetingSettingsArgs: MeetingSettingsArgs{MeetingType: tc.argType, ShowMeetingAttendees: tc.argShow},
			})
			if err != nil || res.IsError {
				t.Fatalf("update failed: err=%v result=%s", err, allResultText(t, res))
			}
			puts := putRequests(api, path)
			if len(puts) != 1 {
				t.Fatalf("want one PUT, saw %d", len(puts))
			}
			value, sent := attendeesInBody(t, puts[0])
			if !sent || value != tc.wantValue {
				t.Errorf("show_meeting_attendees sent=%v value=%v, want it sent as %v", sent, value, tc.wantValue)
			}
			if got := strings.Contains(allResultText(t, res), attendeeSharingNotKeptNote); got != tc.wantNote {
				t.Errorf("note present=%v, want %v:\n%s", got, tc.wantNote, allResultText(t, res))
			}
		})
	}
}

func TestMeetingWriteResult_ShowsSavedAttendeeSharing(t *testing.T) {
	api := setupMeetingWriteTest(t)
	stubMeetingForUpdate(api, writeMeetingID, meetingWith(t, "Technical", true))

	res, _, err := handleUpdateMeeting(fullViewCtx(), stubCallToolRequest(), UpdateMeetingArgs{
		MeetingID: writeMeetingID, Title: strPtr("Renamed"),
	})
	if err != nil || res.IsError {
		t.Fatalf("update failed: err=%v result=%s", err, allResultText(t, res))
	}
	if !strings.Contains(allResultText(t, res), `"show_meeting_attendees": true`) {
		t.Errorf("the result must show the saved attendee-sharing value:\n%s", allResultText(t, res))
	}
}

// TestMeetingWriteTools_KnownGapComments keeps the marker the repository uses
// to find every rule a tool applies in place of the owning service (see
// "Known gaps" in AGENTS.md) on both places this file applies one.
func TestMeetingWriteTools_KnownGapComments(t *testing.T) {
	src, err := os.ReadFile("meeting_write.go")
	if err != nil {
		t.Fatalf("read meeting_write.go: %v", err)
	}
	if n := strings.Count(string(src), "// Known gap: "); n < 2 {
		t.Errorf("want the Known gap marker at the create and the update rule, found %d", n)
	}
}

// --- Descriptions ---

// showMeetingAttendeesTextCeiling is the measured size of the
// show_meeting_attendees parameter text.
const showMeetingAttendeesTextCeiling = 77

func TestMeetingWriteTools_Descriptions(t *testing.T) {
	// ceiling is the measured size of each description. A change that
	// grows one has to move its number in the same change, with its reason.
	type registration struct {
		tool     string
		register func(*mcp.Server)
		clause   string
		ceiling  int
	}
	var regs []registration
	for _, asGroups := range []bool{false, true} {
		unit, createCeiling := "committee", 352
		if asGroups {
			unit, createCeiling = "group", 336
		}
		regs = append(regs,
			registration{"create_meeting", func(s *mcp.Server) { RegisterCreateMeeting(s, asGroups) }, "For a project you manage or are meeting coordinator of, or when you manage every " + unit + " listed.", createCeiling},
			registration{"update_meeting", func(s *mcp.Server) { RegisterUpdateMeeting(s, asGroups) }, "For meetings you organize.", 221},
		)
	}
	regs = append(regs,
		registration{"delete_meeting", RegisterDeleteMeeting, "For meetings you organize.", 179},
		registration{"add_meeting_registrant", RegisterAddMeetingRegistrant, "For meetings you organize.", 274},
	)

	for _, reg := range regs {
		tool := listRegisteredTool(t, reg.tool, reg.register)
		t.Logf("%s description: %d UTF-8 bytes", reg.tool, len(tool.Description))
		if strings.Contains(tool.Description, "writer permission") {
			t.Errorf("%s: the description must not name writer permission, which is not the rule:\n%s", reg.tool, tool.Description)
		}
		if !strings.HasSuffix(tool.Description, reg.clause) {
			t.Errorf("%s: the description must end with %q:\n%s", reg.tool, reg.clause, tool.Description)
		}
		if n := len(tool.Description); n > reg.ceiling {
			t.Errorf("%s: description is %d bytes, ceiling %d", reg.tool, n, reg.ceiling)
		}
		if reg.tool == "update_meeting" && !strings.Contains(tool.Description, "Omitted fields keep their value, except as show_meeting_attendees says.") {
			t.Errorf("update_meeting: the description must not promise that every omitted field keeps its value:\n%s", tool.Description)
		}
		if reg.tool == "create_meeting" || reg.tool == "update_meeting" {
			got := schemaPropertyDescription(t, tool, "show_meeting_attendees")
			if strings.Contains(got, "calendar invites") || !strings.Contains(got, "Board") {
				t.Errorf("%s: show_meeting_attendees must name the Board rule and not calendar invites, got %q", reg.tool, got)
			}
			if n := len(got); n > showMeetingAttendeesTextCeiling {
				t.Errorf("%s: show_meeting_attendees text is %d bytes, ceiling %d", reg.tool, n, showMeetingAttendeesTextCeiling)
			}
		}
	}
}

// --- The update's merge carries every field of the payload ---

// updatePayloadFieldsNotFromStored lists the UpdateItxMeetingPayload fields
// updatePayloadFromCurrent does not take from the stored meeting, each with
// its reason. Any other field must be carried, or a full-replace PUT erases
// it.
var updatePayloadFieldsNotFromStored = map[string]string{
	"BearerToken": "set by the auth interceptor",
	"Version":     "API version, a constant",
	"XSync":       "request option, a constant",
	"MeetingID":   "the path parameter, from the caller",
	"UpdateNote":  "a note for this update only, from the caller",
	"Owner":       "left unset so the service keeps the stored owner",
}

// fillNonZero sets v, and everything it holds, to a non-zero value.
func fillNonZero(t *testing.T, v reflect.Value) {
	t.Helper()
	switch v.Kind() {
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillNonZero(t, v.Elem())
	case reflect.String:
		v.SetString("x")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillNonZero(t, s.Index(0))
		v.Set(s)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Field(i).CanSet() {
				fillNonZero(t, v.Field(i))
			}
		}
	default:
		t.Fatalf("fillNonZero: add kind %s (type %s)", v.Kind(), v.Type())
	}
}

// TestUpdatePayloadFromCurrent_CarriesEveryField walks the update payload
// type. Strict decoding refuses a field the client does not know; this
// refuses the other way to lose one: a field the client knows that the
// merge does not copy from the stored meeting. It fails when a client bump
// adds a payload field, until the merge carries it or it is listed above.
func TestUpdatePayloadFromCurrent_CarriesEveryField(t *testing.T) {
	stored := &meetingservice.ITXZoomMeetingResponse{}
	fillNonZero(t, reflect.ValueOf(stored).Elem())
	storedType := reflect.TypeOf(*stored)

	payload := reflect.ValueOf(updatePayloadFromCurrent("m-1", stored)).Elem()
	seen := map[string]bool{}
	for i := range payload.NumField() {
		name := payload.Type().Field(i).Name
		seen[name] = true
		if _, exempt := updatePayloadFieldsNotFromStored[name]; exempt {
			continue
		}
		if _, ok := storedType.FieldByName(name); !ok {
			t.Errorf("payload field %s has no field of that name on the stored meeting: carry it from where it lives, or list it with its reason", name)
			continue
		}
		if payload.Field(i).IsZero() {
			t.Errorf("payload field %s is not carried from the stored meeting, so an update would erase it", name)
		}
	}
	for name := range updatePayloadFieldsNotFromStored {
		if !seen[name] {
			t.Errorf("%s is listed as not carried but is no longer a payload field: remove it from the list", name)
		}
	}
}
