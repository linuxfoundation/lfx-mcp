// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// identityFixture mirrors the synthetic participantRecord fixture in Self
// Serve meeting.service.spec.ts at 188c18c32. Nil overrides model undefined.
func identityFixture(uid string, overrides map[string]any) *querysvc.Resource {
	data := map[string]any{
		"uid": uid, "meeting_id": "meeting-1", "meeting_and_occurrence_id": "meeting-1-occ-1",
		"first_name": "Test", "last_name": "Person", "host": false,
		"is_attended": false, "is_invited": true, "org_is_member": false, "org_is_project_member": false,
	}
	for k, v := range overrides {
		if v == nil {
			delete(data, k)
		} else {
			data[k] = v
		}
	}
	return &querysvc.Resource{Type: strPtr(pastMeetingParticipantResourceType), ID: strPtr(uid), Data: data}
}

// Cases correspond one-for-one to the first ten getPastMeetingParticipants
// tests in Self Serve meeting.service.spec.ts:736–854. The bridge/split/fill
// tests below cover the remaining four upstream cases.
func TestParticipantIdentity_SelfServePairCases(t *testing.T) {
	cases := []struct {
		name string
		a, b map[string]any
		want int
	}{
		{"shared_username_different_emails", map[string]any{"username": "test-user", "email": "invite@example.org", "is_invited": true}, map[string]any{"username": "test-user", "email": "attended@example.org", "is_invited": false, "is_attended": true}, 1},
		{"shared_email_conflicting_usernames", map[string]any{"username": "test-a", "email": "shared@example.org"}, map[string]any{"username": "test-b", "email": "shared@example.org"}, 2},
		{"email_without_usernames", map[string]any{"email": "guest@example.org"}, map[string]any{"email": " GUEST@example.org "}, 1},
		{"email_with_asymmetric_username", map[string]any{"username": "test-user", "email": "guest@example.org"}, map[string]any{"email": "guest@example.org"}, 1},
		{"asymmetric_username_without_email", map[string]any{"username": "test-user"}, nil, 2},
		{"normalized_name_fallback", map[string]any{"first_name": " Test ", "last_name": "Person"}, map[string]any{"first_name": "test", "last_name": "person"}, 1},
		{"no_common_signal", map[string]any{"first_name": "First"}, map[string]any{"first_name": "Second"}, 2},
		{"unnamed_records", map[string]any{"first_name": nil, "last_name": nil}, map[string]any{"first_name": nil, "last_name": nil}, 2},
		{"placeholder_names", map[string]any{"first_name": "[unknown]", "last_name": "[unknown]"}, map[string]any{"first_name": "[unknown]", "last_name": "[unknown]"}, 2},
		{"name_with_asymmetric_email", map[string]any{"email": "guest@example.org"}, nil, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := dedupeParticipants([]*querysvc.Resource{identityFixture("a", tc.a), identityFixture("b", tc.b)})
			if len(out) != tc.want {
				t.Fatalf("people=%d, want %d", len(out), tc.want)
			}
			if tc.name == "shared_username_different_emails" {
				data := out[0].Data.(map[string]any)
				if data["email"] != "invite@example.org" || data["is_invited"] != true || data["is_attended"] != true {
					t.Errorf("must merge flags and prefer the invited email: %v", data)
				}
			}
		})
	}
}

func TestParticipantIdentity_BridgeRegardlessOfEncounterOrder(t *testing.T) {
	records := []*querysvc.Resource{
		identityFixture("a", map[string]any{"email": "shared@example.org"}),
		identityFixture("b", map[string]any{"email": "other@example.org", "username": "test-user"}),
		identityFixture("c", map[string]any{"email": "shared@example.org", "username": "test-user"}),
	}
	for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		out := dedupeParticipants([]*querysvc.Resource{records[order[0]], records[order[1]], records[order[2]]})
		if len(out) != 1 {
			t.Errorf("order %v: people=%d, want 1", order, len(out))
		}
	}
}

func TestParticipantIdentity_IsolatesConflictingUsernameBridge(t *testing.T) {
	records := []*querysvc.Resource{
		identityFixture("a", map[string]any{"username": "test-a", "email": "shared@example.org", "is_attended": true}),
		identityFixture("bridge", map[string]any{"email": "shared@example.org", "is_attended": true, "host": true}),
		identityFixture("c", map[string]any{"username": "test-b", "email": "shared@example.org"}),
	}
	for _, order := range [][3]int{{0, 1, 2}, {2, 1, 0}} {
		out := dedupeParticipants([]*querysvc.Resource{records[order[0]], records[order[1]], records[order[2]]})
		if len(out) != 3 {
			t.Fatalf("order %v: people=%d, want 3", order, len(out))
		}
		byUsername := map[string]map[string]any{}
		for _, r := range out {
			data := r.Data.(map[string]any)
			username, _ := data["username"].(string)
			byUsername[username] = data
		}
		if byUsername[""]["is_attended"] != true || byUsername[""]["host"] != true || byUsername["test-a"]["host"] != false || byUsername["test-b"]["is_attended"] != false {
			t.Errorf("bridge flags leaked across conflicting identities: %v", byUsername)
		}
		if out[2].Data.(map[string]any)["uid"] != "bridge" {
			t.Error("unkeyed bridge must follow keyed groups, like Self Serve's Map + floaters")
		}
	}
}

func TestParticipantIdentity_IsolatesConflictingGuestEmailBridge(t *testing.T) {
	out := dedupeParticipants([]*querysvc.Resource{
		identityFixture("a", map[string]any{"email": "guest-one@example.org"}),
		identityFixture("bridge", nil),
		identityFixture("c", map[string]any{"email": "guest-two@example.org"}),
	})
	if len(out) != 3 {
		t.Fatalf("people=%d, want 3", len(out))
	}
	got := []any{out[0].Data.(map[string]any)["email"], out[1].Data.(map[string]any)["email"], out[2].Data.(map[string]any)["email"]}
	if !reflect.DeepEqual(got, []any{"guest-one@example.org", "guest-two@example.org", nil}) {
		t.Errorf("email groups and isolated no-email bridge: %v", got)
	}
}

func TestParticipantIdentity_BlankPreferredEmailDoesNotDiscardFallback(t *testing.T) {
	out := dedupeParticipants([]*querysvc.Resource{
		identityFixture("a", map[string]any{"email": "", "is_attended": true}),
		identityFixture("b", map[string]any{"email": "guest@example.org"}),
	})
	if len(out) != 1 || out[0].Data.(map[string]any)["email"] != "guest@example.org" {
		t.Fatalf("blank preferred email must be filled from the matching record: %v", out)
	}
}

func TestParticipantIdentity_UnresolvableNamesAreSingletonsEvenWithSameUID(t *testing.T) {
	for _, names := range [][2]string{{"", ""}, {" UNKNOWN ", "[UnKnOwN]"}, {"", "[unknown]"}, {"\t", "\n"}} {
		rows := []*querysvc.Resource{
			identityFixture("a", map[string]any{"uid": "same", "first_name": names[0], "last_name": names[1]}),
			identityFixture("b", map[string]any{"uid": "same", "first_name": names[0], "last_name": names[1]}),
		}
		if got := len(dedupeParticipants(rows)); got != 2 {
			t.Errorf("names %q: %d groups; empty normalized names never match, regardless of UID", names, got)
		}
	}
	// A meaningful token makes the full name resolvable; do not delete the
	// placeholder token from the normalized name after the guard.
	rows := []*querysvc.Resource{
		identityFixture("a", map[string]any{"first_name": "unknown", "last_name": "Person"}),
		identityFixture("b", map[string]any{"first_name": " UNKNOWN ", "last_name": "person"}),
	}
	if got := len(dedupeParticipants(rows)); got != 1 {
		t.Errorf("meaningful last name should allow name fallback: %d groups", got)
	}
}

func TestParticipantIdentity_UsernamesAreRawCaseSensitiveSignals(t *testing.T) {
	for _, other := range []string{"Test-User", " test-user "} {
		out := dedupeParticipants([]*querysvc.Resource{
			identityFixture("a", map[string]any{"username": "test-user", "email": "shared@example.org"}),
			identityFixture("b", map[string]any{"username": other, "email": "shared@example.org"}),
		})
		if len(out) != 2 {
			t.Errorf("distinct raw usernames must not merge on shared email: %q", other)
		}
	}
}

func TestParticipantIdentity_MergeFoldAndCanonicalEmail(t *testing.T) {
	first := identityFixture("invited", map[string]any{
		"username": "test-user", "email": " invite@example.org ", "is_invited": true, "host": true,
		"avatar_url": "https://example.org/avatar", "job_title": "Engineer", "org_name": "Example", "org_is_member": true,
	})
	attended := identityFixture("first-attended", map[string]any{
		"username": "test-user", "email": "session@example.org", "is_invited": false, "is_attended": true,
		"avatar_url": " \t", "job_title": "", "org_name": "Attended Org", "org_is_project_member": true,
	})
	later := identityFixture("later-attended", map[string]any{
		"username": "test-user", "email": "later@example.org", "is_invited": false, "is_attended": true, "org_name": "Later Org",
	})
	out := dedupeParticipants([]*querysvc.Resource{first, attended, later})
	if len(out) != 1 {
		t.Fatalf("people=%d, want 1", len(out))
	}
	data := out[0].Data.(map[string]any)
	for _, flag := range []string{"is_attended", "is_invited", "host", "org_is_member", "org_is_project_member"} {
		if data[flag] != true {
			t.Errorf("%s must be OR'd", flag)
		}
	}
	for k, want := range map[string]any{"uid": "first-attended", "email": " invite@example.org ", "avatar_url": "https://example.org/avatar", "job_title": "Engineer", "org_name": "Attended Org"} {
		if data[k] != want {
			t.Errorf("%s=%v, want %v", k, data[k], want)
		}
	}
	if *out[0].ID != "first-attended" || attended.Data.(map[string]any)["avatar_url"] != " \t" {
		t.Error("preferred resource metadata or input immutability changed")
	}
	// No invited record: the first original nonblank email wins, not the
	// email on the attended record chosen by the merge fold.
	out = dedupeParticipants([]*querysvc.Resource{
		identityFixture("first", map[string]any{"username": "test-user", "email": "first@example.org", "is_invited": false}),
		identityFixture("attended", map[string]any{"username": "test-user", "email": "attended@example.org", "is_invited": false, "is_attended": true}),
	})
	if len(out) != 1 || out[0].Data.(map[string]any)["email"] != "first@example.org" {
		t.Error("without an invited email, preserve the first nonblank email in encounter order")
	}
	// An invited email later in the group outranks the first attended email;
	// OR'ing is_invited must not mutate the input and change that selection.
	firstAttended := identityFixture("first-attended", map[string]any{"username": "test-user", "email": "session@example.org", "is_invited": false, "is_attended": true})
	out = dedupeParticipants([]*querysvc.Resource{
		firstAttended,
		identityFixture("later-invited", map[string]any{"username": "test-user", "email": "account@example.org", "is_invited": true}),
	})
	if len(out) != 1 || out[0].Data.(map[string]any)["email"] != "account@example.org" || firstAttended.Data.(map[string]any)["is_invited"] != false {
		t.Error("canonical email must prefer the original invited record without mutating inputs")
	}
}

func TestParticipantIdentity_JavaScriptNormalizationParity(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b map[string]any
		want int
	}{
		{"final_sigma", map[string]any{"first_name": "ΟΣ", "last_name": ""}, map[string]any{"first_name": "ος", "last_name": ""}, 1},
		{"expanding_lowercase", map[string]any{"first_name": "İ", "last_name": ""}, map[string]any{"first_name": "i\u0307", "last_name": ""}, 1},
		{"trim_bom", map[string]any{"email": "\ufeffguest@example.org\ufeff"}, map[string]any{"email": "GUEST@example.org"}, 1},
		{"do_not_trim_next_line", map[string]any{"email": "\u0085guest@example.org"}, map[string]any{"email": "guest@example.org"}, 2},
		{"placeholder_with_bom", map[string]any{"first_name": "\ufeff[unknown]\ufeff", "last_name": ""}, map[string]any{"first_name": "\ufeff[unknown]\ufeff", "last_name": ""}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(dedupeParticipants([]*querysvc.Resource{identityFixture("a", tc.a), identityFixture("b", tc.b)})); got != tc.want {
				t.Errorf("people=%d, want JavaScript trim/toLowerCase result %d", got, tc.want)
			}
		})
	}
	out := dedupeParticipants([]*querysvc.Resource{
		identityFixture("invited", map[string]any{"email": "same@example.org", "job_title": "Engineer"}),
		identityFixture("attended", map[string]any{"email": "same@example.org", "is_attended": true, "job_title": "\ufeff"}),
	})
	if len(out) != 1 || out[0].Data.(map[string]any)["job_title"] != "Engineer" {
		t.Error("pickNonBlank must treat JavaScript BOM whitespace as blank")
	}
}

func TestParticipantIdentity_DescriptionAndSchema(t *testing.T) {
	tool := listRegisteredTool(t, "search_past_meeting_participants", func(s *mcp.Server) { RegisterSearchPastMeetingParticipants(s, false) })
	t.Logf("search_past_meeting_participants description: %d UTF-8 bytes", len(tool.Description))
	want := "People are de-duplicated by identity like LFX Self Serve: LFX username when both records have one, else e-mail, else normalised name; dedupe=false returns raw records."
	if !strings.Contains(tool.Description, want) {
		t.Errorf("description missing the exact identity contract: %s", tool.Description)
	}
	schema := tool.InputSchema.(map[string]any)
	dedupe := schema["properties"].(map[string]any)["dedupe"].(map[string]any)
	description, _ := dedupe["description"].(string)
	if !strings.Contains(description, want) || !strings.Contains(description, "default true") {
		t.Errorf("dedupe schema missing identity contract/default: %s", description)
	}
}

func BenchmarkDedupeParticipants5000(b *testing.B) {
	for _, scenario := range []string{"distinct", "connected"} {
		b.Run(scenario, func(b *testing.B) {
			rows := make([]*querysvc.Resource, participantMaxRecords)
			for i := range rows {
				username := fmt.Sprintf("synthetic-user-%d", i)
				if scenario == "connected" {
					username = "synthetic-same-user"
				}
				rows[i] = identityFixture(fmt.Sprintf("synthetic-%d", i), map[string]any{"username": username, "email": fmt.Sprintf("synthetic-%d@example.org", i)})
			}
			want := participantMaxRecords
			if scenario == "connected" {
				want = 1
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got := len(dedupeParticipants(rows)); got != want {
					b.Fatalf("people=%d, want %d", got, want)
				}
			}
		})
	}
}
