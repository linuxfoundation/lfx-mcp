// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestParticipants_RecordCapNoteDoesNotInventExtraMatches(t *testing.T) {
	api := setupParticipantTest(t)
	api.Respond(resourcesPath, page([]string{pastMeetingDoc("m-1"), pastMeetingDoc("m-2")}, ""))
	for p := 0; p < participantMaxRecords/participantDrainPageSize; p++ {
		docs := make([]string, participantDrainPageSize)
		for i := range docs {
			n := p*participantDrainPageSize + i
			docs[i] = participantDoc(fmt.Sprintf("p%d", n), fmt.Sprintf("p%d@example.org", n), "Test", fmt.Sprint(n), true, false, "")
		}
		token := fmt.Sprintf("p%d", p)
		if p == participantMaxRecords/participantDrainPageSize-1 {
			token = "" // Exactly the cap, with this meeting exhausted.
		}
		api.Respond(resourcesPath, page(docs, token))
	}
	api.Respond(resourcesPath, page(nil, "")) // The unvisited meeting might be empty.

	res, _, err := handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), SearchPastMeetingParticipantsArgs{
		ProjectUID: "p", DateFrom: "2026-01-01",
	})
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v, %s", err, allResultText(t, res))
	}
	out := resultJSON(t, res)
	if out["records"] != float64(participantMaxRecords) || out["meetings"] != float64(1) || out["truncated_records"] != true {
		t.Fatalf("cap/count behavior changed: %v", out)
	}
	want := fmt.Sprintf("The record cap (%d) was reached before every matching past meeting was checked; participant records may have been omitted (truncated_records=true). Narrow the range, add attended_only or org_name, or use count_only.", participantMaxRecords)
	if out["note"] != want {
		t.Errorf("note = %q, want %q", out["note"], want)
	}
	for _, req := range api.RequestsTo(resourcesPath) {
		if req.Query.Get("parent") == "past_meeting:m-2" {
			t.Error("the cap must stop before querying the second meeting")
		}
	}
}

func TestParticipants_RecordCapDisclosureMatchesSchema(t *testing.T) {
	tool := listRegisteredTool(t, "search_past_meeting_participants", func(s *mcp.Server) { RegisterSearchPastMeetingParticipants(s, false) })
	want := "reached the record cap before all meetings were checked"
	if !strings.Contains(tool.Description, want) || !strings.Contains(tool.Description, "truncated_records") {
		t.Errorf("description must explain the conservative record cap: %s", tool.Description)
	}
	schema := tool.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	pageSize := properties["page_size"].(map[string]any)
	description, _ := pageSize["description"].(string)
	if !strings.Contains(description, want) || !strings.Contains(description, "truncated_records") {
		t.Errorf("page_size schema must disclose the date-range cap: %s", description)
	}
}
