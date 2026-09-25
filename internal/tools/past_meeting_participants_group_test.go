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

func TestParticipantsGroupMode_SchemaAndDescription(t *testing.T) {
	committee := listRegisteredTool(t, "search_past_meeting_participants", func(s *mcp.Server) {
		RegisterSearchPastMeetingParticipants(s, false)
	})
	group := listRegisteredTool(t, "search_past_meeting_participants", func(s *mcp.Server) {
		RegisterSearchPastMeetingParticipants(s, true)
	})
	wantDescription := strings.NewReplacer(
		"committee UID", "group UID (also known as committee UID)",
		"project or committee", "project or group",
	).Replace(committee.Description)
	if group.Description != wantDescription {
		t.Errorf("group description must change only the sibling-style terminology: %q", group.Description)
	}
	for mode, tool := range []*mcp.Tool{committee, group} {
		t.Logf("asGroups=%v description: %d UTF-8 bytes", mode == 1, len(tool.Description))
		if len(tool.Description) > 1000 || len(tool.Description) > schemaDescriptionBudget {
			t.Errorf("description exceeds its unchanged budget: %d", len(tool.Description))
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Error("participant search must remain read-only in both modes")
		}
	}
	committeeProps := committee.InputSchema.(map[string]any)["properties"].(map[string]any)
	groupProps := group.InputSchema.(map[string]any)["properties"].(map[string]any)
	if len(committeeProps) != len(groupProps) {
		t.Fatal("group schema changed the number of fields")
	}
	for key, value := range committeeProps {
		want := make(map[string]any)
		for name, fieldValue := range value.(map[string]any) {
			want[name] = fieldValue
		}
		description := strings.ReplaceAll(want["description"].(string), "committee_uid", "group_uid")
		if key == "committee_uid" {
			key = "group_uid"
			description = strings.Replace(description, "committee UID", "group UID (also known as committee UID)", 1)
		}
		want["description"] = description
		if !reflect.DeepEqual(groupProps[key], want) {
			t.Errorf("group property %s differs beyond the terminology substitution: got %#v, want %#v", key, groupProps[key], want)
		}
	}
	if _, exists := groupProps["committee_uid"]; exists {
		t.Error("group mode must not expose committee_uid")
	}
}

func TestParticipantsGroupMode_FieldOrderAndTypes(t *testing.T) {
	committee := reflect.TypeFor[SearchPastMeetingParticipantsArgs]()
	group := reflect.TypeFor[SearchPastMeetingParticipantsGroupArgs]()
	if group.NumField() != committee.NumField() {
		t.Fatal("group args must preserve every field")
	}
	for i := 0; i < committee.NumField(); i++ {
		old, got := committee.Field(i), group.Field(i)
		name := old.Name
		if name == "CommitteeUID" {
			name = "GroupUID"
		}
		if got.Name != name || got.Type != old.Type || got.Tag.Get("json") != strings.ReplaceAll(old.Tag.Get("json"), "committee_uid", "group_uid") {
			t.Errorf("field %d changed order/type/json shape: %v -> %v", i, old, got)
		}
	}
}

func TestParticipantsGroupMode_DelegatesToSameHandler(t *testing.T) {
	for _, tc := range []struct {
		name string
		args SearchPastMeetingParticipantsGroupArgs
	}{
		{"past_meeting_precedence_paging_raw", SearchPastMeetingParticipantsGroupArgs{
			PastMeetingID: "m", GroupUID: "g", ProjectUID: "p", Name: "Test", Sort: "name_desc",
			PageSize: 25, PageToken: "cursor", AttendedOnly: true, OrgName: "Example Org", MaxMeetings: 8, Dedupe: boolPtrT(false),
		}},
		{"group_precedence_identity_default", SearchPastMeetingParticipantsGroupArgs{
			GroupUID: "g", ProjectUID: "p", Name: "Test", PageSize: 17, Sort: "updated_desc", AttendedOnly: true, OrgName: "Example Org",
		}},
		{"project_fallback_identity_true", SearchPastMeetingParticipantsGroupArgs{
			ProjectUID: "p", Dedupe: boolPtrT(true), PageSize: 13, Sort: "updated_asc",
		}},
		{"date_range_raw_cap", SearchPastMeetingParticipantsGroupArgs{
			GroupUID: "g", ProjectUID: "p", DateFrom: "2026-06-01", DateTo: "2026-06-30", MaxMeetings: 1,
			Name: "Test", AttendedOnly: true, OrgName: "Example Org", PageSize: 17, Sort: "updated_asc", Dedupe: boolPtrT(false),
		}},
		{"date_range_count_cap", SearchPastMeetingParticipantsGroupArgs{
			GroupUID: "g", ProjectUID: "p", DateFrom: "2026-06-01", DateTo: "2026-06-30", MaxMeetings: 1,
			Name: "Test", AttendedOnly: true, OrgName: "Example Org", CountOnly: true,
		}},
		{"legacy_count_filters", SearchPastMeetingParticipantsGroupArgs{
			GroupUID: "g", CountOnly: true, AttendedOnly: true, OrgName: "Example Org", Name: "Test",
		}},
		{"date_requires_scope", SearchPastMeetingParticipantsGroupArgs{DateFrom: "2026-06-01"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Derive canonical input via its JSON contract rather than duplicating
			// the production adapter's field-by-field mapping in the test.
			raw, err := json.Marshal(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if uid, ok := fields["group_uid"]; ok {
				fields["committee_uid"] = uid
				delete(fields, "group_uid")
			}
			raw, err = json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			var canonical SearchPastMeetingParticipantsArgs
			if err := json.Unmarshal(raw, &canonical); err != nil {
				t.Fatal(err)
			}
			run := func(asGroups bool) (string, bool, []string) {
				api := setupParticipantTest(t)
				token := "next"
				if tc.args.DateFrom != "" || tc.args.DateTo != "" {
					api.Respond(resourcesPath, page([]string{pastMeetingDoc("m1"), pastMeetingDoc("m2")}, ""))
					token = ""
				}
				api.Respond(resourcesPath, page([]string{
					participantDoc("p1", "person@example.org", "Test", "Person", true, false, "Example Org"),
					participantDoc("p2", "person@example.org", "Test", "Person", false, true, "Example Org"),
				}, token))
				api.Respond(countPath, `{"count":2,"has_more":false}`)
				var result *mcp.CallToolResult
				if asGroups {
					result, _, err = handleSearchPastMeetingParticipantsGroupMode(context.Background(), stubCallToolRequest(), tc.args)
				} else {
					result, _, err = handleSearchPastMeetingParticipants(context.Background(), stubCallToolRequest(), canonical)
				}
				if err != nil {
					t.Fatal(err)
				}
				requests := make([]string, 0)
				for _, request := range api.Requests() {
					assertExchangedAuth(t, request)
					requests = append(requests, request.Method+" "+request.Path+"?"+request.Query.Encode())
				}
				return allResultText(t, result), result.IsError, requests
			}
			want, wantError, wantRequests := run(false)
			got, gotError, gotRequests := run(true)
			if got != want || gotError != wantError || !reflect.DeepEqual(gotRequests, wantRequests) {
				t.Errorf("group adapter changed the handler contract:\nresult %s\nwant %s\nrequests %v\nwant %v", got, want, gotRequests, wantRequests)
			}
		})
	}
}
