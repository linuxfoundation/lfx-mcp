// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// emptyResourcePage is a query-service page with no records.
const emptyResourcePage = `{"resources": []}`

// pastMeetingPath is the meeting-service path get_past_meeting reads the base
// past meeting from, for the synthetic id past-1.
const pastMeetingPath = "/itx/past_meetings/past-1"

// pastMeetingBody is a synthetic meeting-service past meeting.
const pastMeetingBody = `{"id": "past-1", "title": "Weekly sync"}`

// The wording is pinned literally so a change to it is a deliberate edit of
// this test, not a side effect.
func TestLookupNotVisibleMessage_Wording(t *testing.T) {
	want := "Error: no meeting with UID m-1 is visible to you; it may not exist, or it may not be shared with you. Check the UID, or ask someone with access to confirm it."
	if got := lookupNotVisibleMessage("meeting", "m-1"); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	wantNote := "NOTE: no recording of this past meeting is visible to you; it may not exist, or it may not be shared with you."
	if got := pastMeetingChildNotVisibleNote("recording"); got != wantNote {
		t.Errorf("got  %q\nwant %q", got, wantNote)
	}
}

// Every query-backed lookup by UID answers an empty page with the shared
// not-visible wording, never with a bare not-found.
func TestQueryBackedLookups_EmptyPageSaysNotVisible(t *testing.T) {
	for _, tc := range []struct {
		name  string
		label string
		call  func() (*mcp.CallToolResult, any, error)
	}{
		{
			name:  "get_meeting",
			label: "meeting",
			call: func() (*mcp.CallToolResult, any, error) {
				return handleGetMeeting(context.Background(), stubCallToolRequest(), GetMeetingArgs{UID: "id-1"})
			},
		},
		{
			name:  "get_meeting_registrant",
			label: "meeting registrant",
			call: func() (*mcp.CallToolResult, any, error) {
				return handleGetMeetingRegistrant(context.Background(), stubCallToolRequest(), GetMeetingRegistrantArgs{UID: "id-1"})
			},
		},
		{
			name:  "get_past_meeting_participant",
			label: "past meeting participant",
			call: func() (*mcp.CallToolResult, any, error) {
				return handleGetPastMeetingParticipant(context.Background(), stubCallToolRequest(), GetPastMeetingParticipantArgs{UID: "id-1"})
			},
		},
		{
			name:  "get_past_meeting_summary",
			label: "past meeting summary",
			call: func() (*mcp.CallToolResult, any, error) {
				return handleGetPastMeetingSummary(context.Background(), stubCallToolRequest(), GetPastMeetingSummaryArgs{UID: "id-1"})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMeetingLookupTest(t)
			api.Respond(resourcesPath, emptyResourcePage)

			res, out, err := tc.call()
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if out != nil {
				t.Errorf("an error result must carry no structured output, got %v", out)
			}
			if res == nil || !res.IsError || len(res.Content) != 1 {
				t.Fatalf("expected a single-block error result, got %+v", res)
			}
			text := allResultText(t, res)
			want := lookupNotVisibleMessage(tc.label, "id-1")
			if strings.TrimSuffix(text, "\n") != want {
				t.Errorf("got  %q\nwant %q", text, want)
			}
			if strings.Contains(text, "not found") {
				t.Errorf("must not claim the record is absent: %q", text)
			}
		})
	}
}

// getPastMeetingWith runs get_past_meeting with the base past meeting present
// and the recording and transcript query pages given, in the order the
// handler reads them.
func getPastMeetingWith(t *testing.T, recording, transcript stubAPIResponse) (*mcp.CallToolResult, pastMeetingGetResult) {
	t.Helper()
	api := setupMeetingLookupTest(t)
	api.Respond(pastMeetingPath, pastMeetingBody)
	api.RespondStatus(resourcesPath, recording.Status, recording.Body)
	api.RespondStatus(resourcesPath, transcript.Status, transcript.Body)

	res, out, err := handleGetPastMeeting(context.Background(), stubCallToolRequest(), GetPastMeetingArgs{UID: "past-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected a successful result, got %+v", res)
	}
	if out.Meeting == nil || derefStr(out.Meeting.ID) != "past-1" {
		t.Fatalf("expected the base past meeting, got %+v", out.Meeting)
	}
	return res, out
}

// leadingBlocks returns the text of every content block before the JSON one.
func leadingBlocks(t *testing.T, res *mcp.CallToolResult) []string {
	t.Helper()
	var out []string
	for _, c := range res.Content[:len(res.Content)-1] {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", c)
		}
		out = append(out, tc.Text)
	}
	return out
}

func TestGetPastMeeting_NestedDocumentVisibility(t *testing.T) {
	ok := func(resourceType, id string) stubAPIResponse {
		return stubAPIResponse{Status: http.StatusOK, Body: singleResourcePage(resourceType, id, `{"uid": "`+id+`"}`)}
	}
	empty := stubAPIResponse{Status: http.StatusOK, Body: emptyResourcePage}
	failed := stubAPIResponse{Status: http.StatusInternalServerError, Body: `{"message": "synthetic failure"}`}
	recordingNote := pastMeetingChildNotVisibleNote("recording")
	transcriptNote := pastMeetingChildNotVisibleNote("transcript")

	for _, tc := range []struct {
		name           string
		recording      stubAPIResponse
		transcript     stubAPIResponse
		wantBlocks     []string // exact leading blocks; a "WARNING:" entry matches by prefix
		wantRecording  bool
		wantTranscript bool
	}{
		{
			name:           "both present: no note",
			recording:      ok("v1_past_meeting_recording", "rec-1"),
			transcript:     ok("v1_past_meeting_transcript", "tr-1"),
			wantBlocks:     nil,
			wantRecording:  true,
			wantTranscript: true,
		},
		{
			name:       "both absent: one note each",
			recording:  empty,
			transcript: empty,
			wantBlocks: []string{recordingNote, transcriptNote},
		},
		{
			name:           "recording absent, transcript present",
			recording:      empty,
			transcript:     ok("v1_past_meeting_transcript", "tr-1"),
			wantBlocks:     []string{recordingNote},
			wantTranscript: true,
		},
		{
			name:          "recording present, transcript absent",
			recording:     ok("v1_past_meeting_recording", "rec-1"),
			transcript:    empty,
			wantBlocks:    []string{transcriptNote},
			wantRecording: true,
		},
		{
			name:       "fetch error keeps its warning, absence still noted",
			recording:  failed,
			transcript: empty,
			wantBlocks: []string{"WARNING: past meeting recording unavailable - ", transcriptNote},
		},
		{
			name:           "recording failed, transcript present",
			recording:      failed,
			transcript:     ok("v1_past_meeting_transcript", "tr-1"),
			wantBlocks:     []string{"WARNING: past meeting recording unavailable - "},
			wantTranscript: true,
		},
		{
			name:          "recording present, transcript failed",
			recording:     ok("v1_past_meeting_recording", "rec-1"),
			transcript:    failed,
			wantBlocks:    []string{"WARNING: past meeting transcript unavailable - "},
			wantRecording: true,
		},
		{
			name:       "recording absent, transcript failed",
			recording:  empty,
			transcript: failed,
			wantBlocks: []string{recordingNote, "WARNING: past meeting transcript unavailable - "},
		},
		{
			name:       "both failed: one warning each",
			recording:  failed,
			transcript: failed,
			wantBlocks: []string{"WARNING: past meeting recording unavailable - ", "WARNING: past meeting transcript unavailable - "},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, out := getPastMeetingWith(t, tc.recording, tc.transcript)

			got := leadingBlocks(t, res)
			if len(got) != len(tc.wantBlocks) {
				t.Fatalf("leading blocks: got %q, want %q", got, tc.wantBlocks)
			}
			for i, want := range tc.wantBlocks {
				if strings.HasPrefix(want, "WARNING:") {
					if !strings.HasPrefix(got[i], want) {
						t.Errorf("block %d: got %q, want prefix %q", i, got[i], want)
					}
					continue
				}
				if got[i] != want {
					t.Errorf("block %d: got %q, want %q", i, got[i], want)
				}
			}

			if (out.Recording != nil) != tc.wantRecording {
				t.Errorf("recording present = %v, want %v", out.Recording != nil, tc.wantRecording)
			}
			if (out.Transcript != nil) != tc.wantTranscript {
				t.Errorf("transcript present = %v, want %v", out.Transcript != nil, tc.wantTranscript)
			}
			doc := resultJSON(t, res)
			if _, has := doc["recording"]; has != tc.wantRecording {
				t.Errorf("JSON recording key present = %v, want %v", has, tc.wantRecording)
			}
			if _, has := doc["transcript"]; has != tc.wantTranscript {
				t.Errorf("JSON transcript key present = %v, want %v", has, tc.wantTranscript)
			}
		})
	}
}

// The base past meeting comes from the meeting service, not the query
// service: its 404 gets the shared access message (a 404 cannot tell a
// missing record from one the caller may not see), not the query-backed
// not-visible wording.
func TestGetPastMeeting_ServiceNotFoundGetsAccessMessage(t *testing.T) {
	api := setupMeetingLookupTest(t)
	api.RespondStatus(pastMeetingPath, http.StatusNotFound, `{"code": "404", "message": "past meeting not found"}`)

	res, _, err := handleGetPastMeeting(context.Background(), stubCallToolRequest(), GetPastMeetingArgs{UID: "past-1"})
	if err == nil {
		t.Fatalf("expected an error, got result %+v", res)
	}
	if want := "Failed to get past meeting: " + accessDeniedMessage; err.Error() != want {
		t.Errorf("got  %q\nwant %q", err.Error(), want)
	}
	if strings.Contains(err.Error(), "visible to you") {
		t.Errorf("a service not-found must not use the query-backed wording: %q", err.Error())
	}
	if n := len(api.RequestsTo(resourcesPath)); n != 0 {
		t.Errorf("no query-service call expected after the base lookup failed, got %d", n)
	}
}
