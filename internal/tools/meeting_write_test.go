// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

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
  "password": "0d6c1f2e-5b7a-4c3d-9e8f-1a2b3c4d5e6f",
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
	for _, secret := range []string{"123456", "0d6c1f2e-5b7a-4c3d-9e8f-1a2b3c4d5e6f", "passcode", "password"} {
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
