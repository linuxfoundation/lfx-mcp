// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// pastMeetingArtifactFixture reads one recording or transcript document from
// testdata/past_meeting, written in the indexer contract's JSON spelling with
// every field of the contract set, links and people fields included.
func pastMeetingArtifactFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "past_meeting", name+".json"))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return string(body)
}

// artifactPage wraps a fixture document in a one-record query-service page.
func artifactPage(t *testing.T, resourceType, fixture string) stubAPIResponse {
	t.Helper()
	return stubAPIResponse{Status: http.StatusOK, Body: singleResourcePage(resourceType, "93699735000-1700000000", pastMeetingArtifactFixture(t, fixture))}
}

// artifactLeakMarkers are the strings that must never reach get_past_meeting's
// text or structured output: links, identifiers and people fields of the
// recording and transcript documents.
var artifactLeakMarkers = []string{
	"http://", "https://", "download_url", "play_url", "share_url", "uuid",
	"host_", "created_by", "updated_by", "file-aaa-111", "file-bbb-222", "file-ccc-333", "file-ddd-444",
	"session-uuid", "pwd=", "@example.test", "project_uid", "committees", "platform",
}

// TestGetPastMeeting_ArtifactDetailsWithoutLinks pins the projection of the
// recording and transcript documents: every kept field's value, and that no
// link, identifier or people field of either document reaches the text or the
// marshalled structured value.
func TestGetPastMeeting_ArtifactDetailsWithoutLinks(t *testing.T) {
	res, out := getPastMeetingWith(t,
		artifactPage(t, "v1_past_meeting_recording", "recording_full"),
		artifactPage(t, "v1_past_meeting_transcript", "transcript_full"))

	wantRecording := &pastMeetingRecordingDetails{
		RecordingAccess: "meeting_hosts",
		StartTime:       "2026-10-01T15:00:00Z",
		RecordingCount:  4,
		TotalSize:       104900000,
		Files: []pastMeetingArtifactFile{
			{FileType: "MP4", FileExtension: "MP4", FileSize: 104857600, RecordingType: "shared_screen_with_speaker_view", Status: "completed", RecordingStart: "2026-10-01T15:00:00Z", RecordingEnd: "2026-10-01T16:02:11Z"},
			{FileType: "CHAT", FileExtension: "TXT", FileSize: 2048, RecordingType: "chat_file", Status: "completed", RecordingStart: "2026-10-01T15:00:00Z", RecordingEnd: "2026-10-01T16:02:11Z"},
		},
		Sessions: []pastMeetingArtifactSession{{StartTime: "2026-10-01T15:00:00Z", TotalSize: 104859648}},
	}
	if !reflect.DeepEqual(out.Recording, wantRecording) {
		t.Errorf("recording details\n got: %+v\nwant: %+v", out.Recording, wantRecording)
	}
	wantTranscript := &pastMeetingTranscriptDetails{
		TranscriptAccess: "public",
		StartTime:        "2026-10-01T15:00:00Z",
		TotalSize:        104900000,
		Files: []pastMeetingArtifactFile{
			{FileType: "TRANSCRIPT", FileExtension: "VTT", FileSize: 40960, RecordingType: "audio_transcript", Status: "completed", RecordingStart: "2026-10-01T15:00:00Z", RecordingEnd: "2026-10-01T16:02:11Z"},
			{FileType: "TIMELINE", FileExtension: "JSON", FileSize: 8192, RecordingType: "timeline", Status: "completed", RecordingStart: "2026-10-01T15:00:00Z", RecordingEnd: "2026-10-01T16:02:11Z"},
		},
		Sessions: []pastMeetingArtifactSession{{StartTime: "2026-10-01T15:00:00Z", TotalSize: 104859648}},
	}
	if !reflect.DeepEqual(out.Transcript, wantTranscript) {
		t.Errorf("transcript details\n got: %+v\nwant: %+v", out.Transcript, wantTranscript)
	}

	text := allResultText(t, res)
	structured, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range artifactLeakMarkers {
		if strings.Contains(text, marker) {
			t.Errorf("text must not carry %q", marker)
		}
		if strings.Contains(string(structured), marker) {
			t.Errorf("structured value must not carry %q", marker)
		}
	}
	// The JSON block carries the projected shape under the contract's names.
	doc := resultJSON(t, res)
	rec, _ := doc["recording"].(map[string]any)
	if rec["recording_access"] != "meeting_hosts" || rec["recording_count"] != 4.0 {
		t.Errorf("JSON recording details: %v", rec)
	}
	files, _ := rec["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("JSON recording files: %v", rec["files"])
	}
	if first, _ := files[0].(map[string]any); first["file_type"] != "MP4" || first["file_size"] != 104857600.0 {
		t.Errorf("JSON recording file: %v", first)
	}
	tr, _ := doc["transcript"].(map[string]any)
	if tr["transcript_access"] != "public" || len(tr["files"].([]any)) != 2 || len(tr["sessions"].([]any)) != 1 {
		t.Errorf("JSON transcript details: %v", tr)
	}
}

// TestGetPastMeeting_ArtifactDetailsEmptyLists pins that a document without
// recording_files or sessions (absent or null) gives empty arrays, never
// null, that a file without file_size reads 0, and that a transcript without
// transcript_access omits the key.
func TestGetPastMeeting_ArtifactDetailsEmptyLists(t *testing.T) {
	recording := stubAPIResponse{Status: http.StatusOK, Body: singleResourcePage("v1_past_meeting_recording", "r", `{"recording_access": "public", "recording_count": 0, "total_size": 0, "recording_files": null}`)}
	transcript := stubAPIResponse{Status: http.StatusOK, Body: singleResourcePage("v1_past_meeting_transcript", "t", `{"recording_files": [{"file_type": "TRANSCRIPT", "file_extension": "VTT"}], "sessions": [{"start_time": "2026-10-01T15:00:00Z"}]}`)}
	_, out := getPastMeetingWith(t, recording, transcript)

	structured, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(structured, &doc); err != nil {
		t.Fatal(err)
	}
	if want := `{"recording_access":"public","recording_count":0,"total_size":0,"files":[],"sessions":[]}`; string(doc["recording"]) != want {
		t.Errorf("recording\n got: %s\nwant: %s", doc["recording"], want)
	}
	if want := `{"total_size":0,"files":[{"file_type":"TRANSCRIPT","file_extension":"VTT","file_size":0}],"sessions":[{"start_time":"2026-10-01T15:00:00Z","total_size":0}]}`; string(doc["transcript"]) != want {
		t.Errorf("transcript\n got: %s\nwant: %s", doc["transcript"], want)
	}
}

// TestProjectPastMeetingArtifacts_NonObjectData pins that a document whose
// data is not a JSON object projects to empty details rather than a panic.
func TestProjectPastMeetingArtifacts_NonObjectData(t *testing.T) {
	for _, data := range []any{nil, "text", 42.0, []any{"x"}} {
		rec := projectPastMeetingRecording(data)
		if rec == nil || rec.Files == nil || rec.Sessions == nil || len(rec.Files) != 0 {
			t.Errorf("recording from %v: %+v", data, rec)
		}
		tr := projectPastMeetingTranscript(data)
		if tr == nil || tr.Files == nil || tr.Sessions == nil || len(tr.Files) != 0 {
			t.Errorf("transcript from %v: %+v", data, tr)
		}
	}
}

// TestGetPastMeeting_NotesAndWarningsUnchanged pins, byte for byte, the
// "not visible" notes and the fetch-failure warnings of get_past_meeting and
// which artifacts the structured value carries, against goldens generated
// from the handler at main 97c867d (testdata/past_meeting/notes, see its
// README). The JSON block is excluded: its shape changed by design with the
// artifact projection, and the notes and warnings are the contract pinned.
func TestGetPastMeeting_NotesAndWarningsUnchanged(t *testing.T) {
	ok := func(resourceType, id string) stubAPIResponse {
		return stubAPIResponse{Status: http.StatusOK, Body: singleResourcePage(resourceType, id, `{"uid": "`+id+`"}`)}
	}
	empty := stubAPIResponse{Status: http.StatusOK, Body: emptyResourcePage}
	failed := stubAPIResponse{Status: http.StatusInternalServerError, Body: `{"message": "synthetic failure"}`}
	for name, tc := range map[string][2]stubAPIResponse{
		"both":            {ok("v1_past_meeting_recording", "rec-1"), ok("v1_past_meeting_transcript", "tr-1")},
		"recording_only":  {ok("v1_past_meeting_recording", "rec-1"), empty},
		"transcript_only": {empty, ok("v1_past_meeting_transcript", "tr-1")},
		"neither":         {empty, empty},
		"fetch_failures":  {failed, failed},
	} {
		t.Run(name, func(t *testing.T) {
			res, out := getPastMeetingWith(t, tc[0], tc[1])
			blocks := leadingBlocks(t, res)
			if blocks == nil {
				blocks = []string{}
			}
			gotNotes, _ := json.MarshalIndent(blocks, "", "  ")
			wantNotes, err := os.ReadFile(filepath.Join("testdata", "past_meeting", "notes", name+".golden"))
			if err != nil {
				t.Fatalf("golden missing: %v", err)
			}
			if string(gotNotes)+"\n" != string(wantNotes) {
				t.Errorf("notes and warnings differ from main's:\n--- want\n%s--- got\n%s\n", wantNotes, gotNotes)
			}
			gotPresence, _ := json.Marshal(map[string]bool{"recording": out.Recording != nil, "transcript": out.Transcript != nil})
			wantPresence, err := os.ReadFile(filepath.Join("testdata", "past_meeting", "notes", name+".structured.json"))
			if err != nil {
				t.Fatalf("golden missing: %v", err)
			}
			if string(gotPresence)+"\n" != string(wantPresence) {
				t.Errorf("artifact presence differs from main's: got %s, want %s", gotPresence, wantPresence)
			}
		})
	}
}

// TestGetPastMeeting_DescriptionSaysNoLinks pins the one description change
// of lfx-self-serve#2656: the tool says it returns artifact details without
// links, in the same text whichever naming mode the server runs (the tool has
// no committee or group wording), within budget and naming no other tool.
func TestGetPastMeeting_DescriptionSaysNoLinks(t *testing.T) {
	const want = "Get an LFX past meeting by its UID, including nested recording and transcript details (no links) when available. The recording or transcript is omitted if absent or inaccessible to the caller."
	tool := listRegisteredTool(t, "get_past_meeting", RegisterGetPastMeeting)
	if tool.Description != want {
		t.Errorf("description\n got: %q\nwant: %q", tool.Description, want)
	}
	if strings.Contains(tool.Description, "with links") || strings.Contains(tool.Description, "sub-objects") {
		t.Errorf("description must not promise links or raw documents: %q", tool.Description)
	}
	if n := len(tool.Description); n > schemaDescriptionBudget {
		t.Errorf("description is %d bytes, budget %d", n, schemaDescriptionBudget)
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.Title != "Get Past Meeting" {
		t.Errorf("annotations changed: %+v", tool.Annotations)
	}
}

// TestGetPastMeeting_ArtifactDetailsCarryNoPeopleFields pins, explicitly,
// what the removed per-caller trim used to guarantee and the projection now
// guarantees for every caller: the recording and transcript details carry no
// host_email, host_id, created_by or updated_by, and none of the documents'
// addresses, in the text or the structured output, with and without full view.
func TestGetPastMeeting_ArtifactDetailsCarryNoPeopleFields(t *testing.T) {
	for name, ctx := range map[string]context.Context{"full view": fullViewCtx(), "no full view": WithFullView(context.Background(), false)} {
		t.Run(name, func(t *testing.T) {
			api := setupMeetingLookupTest(t)
			api.Respond(pastMeetingPath, pastMeetingBody)
			api.RespondStatus(resourcesPath, http.StatusOK, singleResourcePage("v1_past_meeting_recording", "93699735000-1700000000", pastMeetingArtifactFixture(t, "recording_full")))
			api.RespondStatus(resourcesPath, http.StatusOK, singleResourcePage("v1_past_meeting_transcript", "93699735000-1700000000", pastMeetingArtifactFixture(t, "transcript_full")))

			res, out, err := handleGetPastMeeting(ctx, stubCallToolRequest(), GetPastMeetingArgs{UID: "past-1"})
			if err != nil || res == nil || res.IsError || out.Recording == nil || out.Transcript == nil {
				t.Fatalf("expected both artifacts: %v %+v", err, out)
			}
			structured, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			text := allResultText(t, res)
			for _, field := range []string{"host_email", "host_id", "created_by", "updated_by", "host@example.test", "creator@example.test", "editor@example.test", "zoom-host-1"} {
				if strings.Contains(text, field) {
					t.Errorf("text carries %q", field)
				}
				if strings.Contains(string(structured), field) {
					t.Errorf("structured value carries %q", field)
				}
			}
			assertNoEmail(t, text)
		})
	}
}
