// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// peopleToolCase is one tool that returns people data, with the upstream
// answers it needs for one call and the call itself. The registry below is
// walked by the tests that pin the two invariants every people tool shares:
// a caller without full view whom every predicate denies gets no people
// data, and a caller with full view gets the upstream records unchanged
// without any predicate call.
type peopleToolCase struct {
	name string
	// setup configures the stub (the shared meeting, committee and project
	// configs all point at it) and queues the upstream answers for one call
	// under the given view; the order of query-service pages depends on it.
	setup func(t *testing.T, api *stubLFXAPI, full bool)
	call  func(ctx context.Context) (*mcp.CallToolResult, any, error)
	// emails are the addresses the fixtures carry; none is the caller's.
	emails []string
	// fixtures are the upstream record bodies (JSON objects) whose every
	// field a full-view caller must get back.
	fixtures []string
}

const (
	registryCommittee = "66666666-6666-4666-8666-666666666666"
	registryMember    = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	registryMeeting   = "77777777777"
	registryPast      = "77777777777-1771596000000"
)

// peopleTools is the registry. A tool that returns people records or people
// fields is added here the day it is added to the server.
var peopleTools = []peopleToolCase{
	{
		name: "search_committee_members",
		setup: func(_ *testing.T, api *stubLFXAPI, _ bool) {
			api.Respond(resourcesPath, page([]string{groupMemberDoc(registryMember, registryCommittee, "None", "member@example.test")}, ""))
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchCommitteeMembers(ctx, stubCallToolRequest(), SearchCommitteeMembersArgs{CommitteeUID: registryCommittee})
		},
		emails:   []string{"member@example.test"},
		fixtures: []string{groupMemberDoc(registryMember, registryCommittee, "None", "member@example.test")},
	},
	{
		name: "get_committee_member",
		setup: func(_ *testing.T, api *stubLFXAPI, _ bool) {
			api.Respond("/committees/"+registryCommittee+"/members/"+registryMember, committeeMemberRecord(registryMember, registryCommittee, "None", "member@example.test"))
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleGetCommitteeMember(ctx, stubCallToolRequest(), GetCommitteeMemberArgs{CommitteeUID: registryCommittee, MemberUID: registryMember})
		},
		emails:   []string{"member@example.test"},
		fixtures: []string{committeeMemberRecord(registryMember, registryCommittee, "None", "member@example.test")},
	},
	{
		name: "get_committee",
		setup: func(_ *testing.T, api *stubLFXAPI, _ bool) {
			api.Respond("/committees/"+registryCommittee, `{"uid": "`+registryCommittee+`", "name": "TSC"}`)
			api.Respond("/committees/"+registryCommittee+"/settings", committeeSettingsRecord(registryCommittee, "hidden"))
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleGetCommittee(ctx, stubCallToolRequest(), GetCommitteeArgs{UID: registryCommittee})
		},
		emails:   []string{"writer@example.test", "auditor@example.test"},
		fixtures: []string{committeeSettingsRecord(registryCommittee, "hidden")},
	},
	{
		name: "search_meeting_registrants",
		setup: func(_ *testing.T, api *stubLFXAPI, full bool) {
			if !full {
				api.Respond(resourcesPath, page(nil, "")) // the caller's own registrations, decided first
			}
			api.Respond(resourcesPath, page([]string{registrantDoc("reg", registryMeeting, "registrant@example.test", true)}, ""))
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchMeetingRegistrants(ctx, stubCallToolRequest(), SearchMeetingRegistrantsArgs{MeetingID: registryMeeting})
		},
		emails:   []string{"registrant@example.test", "creator@example.test", "editor@example.test"},
		fixtures: []string{registrantDoc("reg", registryMeeting, "registrant@example.test", true)},
	},
	{
		name: "get_meeting_registrant",
		setup: func(_ *testing.T, api *stubLFXAPI, full bool) {
			api.Respond(resourcesPath, page([]string{registrantDoc("reg", registryMeeting, "registrant@example.test", true)}, ""))
			if !full {
				api.Respond(resourcesPath, page(nil, ""))
			}
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleGetMeetingRegistrant(ctx, stubCallToolRequest(), GetMeetingRegistrantArgs{UID: "reg"})
		},
		emails:   []string{"registrant@example.test", "creator@example.test", "editor@example.test"},
		fixtures: []string{registrantDoc("reg", registryMeeting, "registrant@example.test", true)},
	},
	{
		name: "search_meetings",
		setup: func(t *testing.T, api *stubLFXAPI, _ bool) {
			// Occurrence fitting is clock-based; pin it before the fixture's.
			pinMeetingSearchNow(t, beforeJoinFieldsOccurrences)
			api.Respond(resourcesPath, singleResourcePage("v1_meeting", "meeting-1", meetingDocWithPeople))
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchMeetings(ctx, stubCallToolRequest(), SearchMeetingsArgs{ProjectUID: "11111111-1111-1111-1111-111111111111"})
		},
		// The organiser's name and e-mail stay (rendered with a mailto link);
		// editors go.
		emails:   []string{"editor@example.test"},
		fixtures: []string{meetingDocWithPeople},
	},
	{
		name: "get_meeting",
		setup: func(_ *testing.T, api *stubLFXAPI, _ bool) {
			api.Respond(resourcesPath, singleResourcePage("v1_meeting", "meeting-1", meetingDocWithPeople))
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleGetMeeting(ctx, stubCallToolRequest(), GetMeetingArgs{UID: "meeting-1"})
		},
		emails:   []string{"editor@example.test"},
		fixtures: []string{meetingDocWithPeople},
	},
	{
		name: "search_past_meeting_participants",
		setup: func(_ *testing.T, api *stubLFXAPI, full bool) {
			if !full {
				api.Respond(resourcesPath, pastDocs(registryPast)) // private, no groups
			}
			api.Respond(resourcesPath, page([]string{participantDocFor("p", registryPast, "participant@example.test", "Par", "T", "", true, true)}, ""))
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchPastMeetingParticipants(ctx, stubCallToolRequest(), SearchPastMeetingParticipantsArgs{PastMeetingID: registryPast})
		},
		emails:   []string{"participant@example.test"},
		fixtures: []string{participantDocFor("p", registryPast, "participant@example.test", "Par", "T", "", true, true)},
	},
	{
		name: "get_past_meeting_participant",
		setup: func(_ *testing.T, api *stubLFXAPI, full bool) {
			api.Respond(resourcesPath, page([]string{participantDocFor("p", registryPast, "participant@example.test", "Par", "T", "", true, true)}, ""))
			if !full {
				api.Respond(resourcesPath, pastDocs(registryPast))
			}
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleGetPastMeetingParticipant(ctx, stubCallToolRequest(), GetPastMeetingParticipantArgs{UID: "p"})
		},
		emails:   []string{"participant@example.test"},
		fixtures: []string{participantDocFor("p", registryPast, "participant@example.test", "Par", "T", "", true, true)},
	},
	{
		name: "search_past_meetings",
		setup: func(_ *testing.T, api *stubLFXAPI, _ bool) {
			api.Respond(resourcesPath, page([]string{pastMeetingDocWith(registryPast, "private", false)}, ""))
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchPastMeetings(ctx, stubCallToolRequest(), SearchPastMeetingsArgs{ProjectUID: "P1"})
		},
		emails:   []string{"editor@example.test"},
		fixtures: []string{pastMeetingDocWith(registryPast, "private", false)},
	},
	{
		name: "get_past_meeting",
		setup: func(_ *testing.T, api *stubLFXAPI, _ bool) {
			api.Respond(pastMeetingPath, pastMeetingBody)
			api.Respond(resourcesPath, singleResourcePage("v1_past_meeting_recording", "rec-1", `{"uid": "rec-1", "host_email": "host@example.test", "created_by": {"email": "creator@example.test"}}`))
			api.Respond(resourcesPath, singleResourcePage("v1_past_meeting_transcript", "tr-1", `{"uid": "tr-1", "host_email": "host@example.test", "updated_by": {"email": "editor@example.test"}}`))
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleGetPastMeeting(ctx, stubCallToolRequest(), GetPastMeetingArgs{UID: "past-1"})
		},
		emails:   []string{"host@example.test", "creator@example.test", "editor@example.test"},
		fixtures: []string{`{"uid": "rec-1", "host_email": "host@example.test", "created_by": {"email": "creator@example.test"}}`, `{"uid": "tr-1", "host_email": "host@example.test", "updated_by": {"email": "editor@example.test"}}`},
	},
	{
		name: "search_past_meeting_summaries",
		setup: func(_ *testing.T, api *stubLFXAPI, _ bool) {
			api.Respond(resourcesPath, page([]string{summaryDoc}, ""))
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleSearchPastMeetingSummaries(ctx, stubCallToolRequest(), SearchPastMeetingSummariesArgs{ProjectUID: "P1"})
		},
		emails:   []string{"host@example.test", "creator@example.test", "editor@example.test"},
		fixtures: []string{summaryDoc},
	},
	{
		name: "get_past_meeting_summary",
		setup: func(_ *testing.T, api *stubLFXAPI, _ bool) {
			api.Respond(resourcesPath, page([]string{summaryDoc}, ""))
		},
		call: func(ctx context.Context) (*mcp.CallToolResult, any, error) {
			return handleGetPastMeetingSummary(ctx, stubCallToolRequest(), GetPastMeetingSummaryArgs{UID: "sum-1"})
		},
		emails:   []string{"host@example.test", "creator@example.test", "editor@example.test"},
		fixtures: []string{summaryDoc},
	},
}

// setupPeopleToolsTest points every tool family at one stub.
func setupPeopleToolsTest(t *testing.T) *stubLFXAPI {
	t.Helper()
	api := newStubLFXAPI(t)
	prevMeeting, prevCommittee, prevProject := meetingConfig, committeeConfig, projectConfig
	SetMeetingConfig(&MeetingConfig{Clients: api.Clients})
	SetCommitteeConfig(&CommitteeConfig{Clients: api.Clients})
	SetProjectConfig(&ProjectConfig{Clients: api.Clients})
	t.Cleanup(func() { meetingConfig, committeeConfig, projectConfig = prevMeeting, prevCommittee, prevProject })
	return api
}

// everything returns the text blocks, the structured output and the error
// of a tool result as one searchable string.
func everything(t *testing.T, res *mcp.CallToolResult, out any, err error) string {
	t.Helper()
	var sb strings.Builder
	if res != nil {
		sb.WriteString(allResultText(t, res))
	}
	if out != nil {
		b, merr := json.Marshal(out)
		if merr != nil {
			t.Fatalf("structured output does not marshal: %v", merr)
		}
		sb.Write(b)
	}
	if err != nil {
		sb.WriteString(err.Error())
	}
	return sb.String()
}

// fixtureDataPaths returns the normalised key paths (lower-case, no
// underscores, dotted) of every field in a fixture, descending into the
// query-service envelope's "data" when present and into nested objects and
// arrays of objects.
func fixtureDataPaths(t *testing.T, fixture string) []string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(fixture), &doc); err != nil {
		t.Fatalf("fixture is not JSON: %v\n%s", err, fixture)
	}
	if data, ok := doc["data"].(map[string]any); ok {
		doc = data
	}
	var out []string
	collectPaths("", doc, &out)
	return out
}

func collectPaths(prefix string, v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			path := normaliseKey(k)
			if prefix != "" {
				path = prefix + "." + path
			}
			*out = append(*out, path)
			collectPaths(path, child, out)
		}
	case []any:
		for _, child := range x {
			collectPaths(prefix, child, out)
		}
	}
}

func normaliseKey(k string) string {
	return strings.ReplaceAll(strings.ToLower(k), "_", "")
}

// outputPaths returns the normalised key paths of a tool's structured
// output, or of its JSON text block for the handlers that publish none.
func outputPaths(t *testing.T, res *mcp.CallToolResult, out any) map[string]bool {
	t.Helper()
	var b []byte
	if out != nil {
		var err error
		if b, err = json.Marshal(out); err != nil {
			t.Fatal(err)
		}
	} else {
		text, ok := res.Content[len(res.Content)-1].(*mcp.TextContent)
		if !ok {
			t.Fatal("expected a JSON text block")
		}
		b = []byte(text.Text)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	var paths []string
	collectPaths("", v, &paths)
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
	}
	return set
}

// assertFixtureFieldsReturned fails unless every field of every fixture
// reaches the structured output (under any prefix the tool wraps it in).
// With the fixtures carrying none of the keys the tools trimmed before this
// change, that pins a full-view caller's output to the upstream record.
func assertFixtureFieldsReturned(t *testing.T, fixtures []string, res *mcp.CallToolResult, out any) {
	t.Helper()
	got := outputPaths(t, res, out)
	for _, fixture := range fixtures {
		for _, want := range fixtureDataPaths(t, fixture) {
			found := false
			for path := range got {
				if path == want || strings.HasSuffix(path, "."+want) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("full view lost fixture field %q", want)
			}
		}
	}
}

// assertGoldenOutput compares a full-view result, byte for byte, with the
// output origin/main's handler produced for the same fixtures: the JSON text
// block and the structured output (or "null"). The goldens in
// testdata/people_full_view were generated by running the registry's setups
// against the handlers at origin/main 6eeebb2; regenerate them the same way
// when a fixture changes, never from this branch's handlers.
func assertGoldenOutput(t *testing.T, name string, res *mcp.CallToolResult, out any) {
	t.Helper()
	wantText, err := os.ReadFile(filepath.Join("testdata", "people_full_view", name+".golden"))
	if err != nil {
		t.Fatalf("golden missing: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("expected one content block, got %d", len(res.Content))
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected a text block, got %T", res.Content[0])
	}
	gotText := text.Text
	if gotText != string(wantText) {
		t.Errorf("full-view text differs from origin/main's output:\n--- want\n%s\n--- got\n%s", wantText, gotText)
	}
	wantStructured, err := os.ReadFile(filepath.Join("testdata", "people_full_view", name+".structured.json"))
	if err != nil {
		t.Fatalf("golden missing: %v", err)
	}
	gotStructured := "null"
	if out != nil {
		b, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		gotStructured = string(b)
	}
	if gotStructured != string(wantStructured) {
		t.Errorf("full-view structured output differs from origin/main's output:\n--- want\n%s\n--- got\n%s", wantStructured, gotStructured)
	}
}

// groupModePeopleToolNames maps the group-mode names of people tools to the
// committee-mode names the registry uses.
var groupModePeopleToolNames = map[string]string{
	"search_group_members": "search_committee_members",
	"get_group_member":     "get_committee_member",
	"get_group":            "get_committee",
}

// TestPeopleTools_RegistryIsComplete ties the registry to PeopleToolNames,
// the exported list cmd/lfx-mcp-server checks newServer's registrations
// against: every name there has a registry entry and every entry is named
// there, so a people tool cannot be registered without the three walks.
// count_lfx_resources is gated separately (peopleCountGate) and has its own
// tests.
func TestPeopleTools_RegistryIsComplete(t *testing.T) {
	named := map[string]bool{}
	for _, name := range PeopleToolNames {
		if canonical, ok := groupModePeopleToolNames[name]; ok {
			name = canonical
		}
		named[name] = true
	}
	inRegistry := map[string]bool{}
	for _, tc := range peopleTools {
		inRegistry[tc.name] = true
		if !named[tc.name] {
			t.Errorf("registry entry %q is not in PeopleToolNames", tc.name)
		}
	}
	for name := range named {
		if !inRegistry[name] {
			t.Errorf("people tool %q has no registry entry; add it to peopleTools", name)
		}
	}
	// Each name registers under its own mode.
	for _, name := range PeopleToolNames {
		_, groups := groupModePeopleToolNames[name]
		if findRegisteredTool(t, name, func(s *mcp.Server) { registerPeopleTools(s, groups) }) == nil {
			t.Errorf("%q is in PeopleToolNames but no people Register* function registers it (groups=%v)", name, groups)
		}
	}
}

// registerPeopleTools registers every people tool in one terminology mode.
func registerPeopleTools(s *mcp.Server, groups bool) {
	RegisterSearchCommitteeMembers(s, groups)
	RegisterGetCommitteeMember(s, groups)
	RegisterGetCommittee(s, groups)
	RegisterSearchMeetingRegistrants(s, groups)
	RegisterGetMeetingRegistrant(s)
	RegisterSearchMeetings(s, groups)
	RegisterGetMeeting(s)
	RegisterSearchPastMeetingParticipants(s, groups)
	RegisterGetPastMeetingParticipant(s)
	RegisterSearchPastMeetings(s, groups)
	RegisterGetPastMeeting(s)
	RegisterSearchPastMeetingSummaries(s)
	RegisterGetPastMeetingSummary(s)
}

// predicateRequests counts the access-check calls. Together with the queue
// check in the full-view test (every queued upstream answer consumed, and an
// unqueued call 404s and fails the tool), it pins that a full-view caller
// causes exactly the upstream calls the tool made before this change.
func predicateRequests(api *stubLFXAPI) int {
	return len(api.RequestsTo(accessCheckPath))
}

// TestPeopleTools_DeniedCallerGetsNoPeopleData walks the registry as a
// caller without full view (an explicit false flag) whom every relation
// check denies, whose own lookups are empty and whose past meetings are
// private: no fixture address appears anywhere in the text, the structured
// output or the error.
func TestPeopleTools_DeniedCallerGetsNoPeopleData(t *testing.T) {
	for _, tc := range peopleTools {
		t.Run(tc.name, func(t *testing.T) {
			api := setupPeopleToolsTest(t)
			api.GrantRelations()
			tc.setup(t, api, false)
			res, out, err := tc.call(WithFullView(context.Background(), false))
			all := everything(t, res, out, err)
			for _, email := range tc.emails {
				if strings.Contains(all, email) {
					t.Errorf("%s reached a denied caller:\n%s", email, all)
				}
			}
			if strings.Contains(all, stubCallerEmail) {
				t.Errorf("the fixtures must not carry the caller's own address")
			}
		})
	}
}

// TestPeopleTools_FullViewIsUnchangedWithoutPredicateCalls walks the
// registry as a caller with full view: every fixture address is returned,
// as before this change, and no predicate call is made.
func TestPeopleTools_FullViewIsUnchangedWithoutPredicateCalls(t *testing.T) {
	for _, tc := range peopleTools {
		t.Run(tc.name, func(t *testing.T) {
			api := setupPeopleToolsTest(t)
			api.GrantRelations()
			tc.setup(t, api, true)
			res, out, err := tc.call(fullViewCtx())
			if err != nil || res == nil || res.IsError {
				t.Fatalf("unexpected failure: %v %s", err, allResultText(t, res))
			}
			all := everything(t, res, out, nil)
			for _, email := range tc.emails {
				if !strings.Contains(all, email) {
					t.Errorf("full view must keep %s:\n%s", email, all)
				}
			}
			assertFixtureFieldsReturned(t, tc.fixtures, res, out)
			assertGoldenOutput(t, tc.name, res, out)
			if n := predicateRequests(api); n != 0 {
				t.Errorf("full view made %d predicate calls: %+v", n, api.Requests())
			}
			// The upstream answers queued for full view are exactly the calls
			// the tool made before this change; a leftover means a call was
			// skipped, an extra one 404s and fails the call above.
			for path, queue := range api.queues {
				if len(queue) != 0 {
					t.Errorf("%d queued answers for %s were not consumed", len(queue), path)
				}
			}
		})
	}
}

// TestPeopleTools_FullViewCallsMatchMain runs the calls in fullViewCallCases
// as a caller with full view: the output and the upstream requests match,
// byte for byte, what origin/main's handlers produced for them (goldens in
// testdata/people_full_view, generated at origin/main 6eeebb2 from the same
// cases file), and no predicate call is made.
func TestPeopleTools_FullViewCallsMatchMain(t *testing.T) {
	for _, tc := range fullViewCallCases {
		t.Run(tc.name, func(t *testing.T) {
			api := tc.setup(t)
			res, out, err := tc.call(fullViewCtx())
			if err != nil || res == nil || res.IsError != tc.wantError {
				t.Fatalf("unexpected outcome (want error %v): %v %s", tc.wantError, err, allResultText(t, res))
			}
			assertGoldenOutput(t, tc.name, res, out)
			want, err := os.ReadFile(filepath.Join("testdata", "people_full_view", tc.name+".requests.golden"))
			if err != nil {
				t.Fatalf("golden missing: %v", err)
			}
			if got := fullViewRequestLog(api); got != string(want) {
				t.Errorf("full-view upstream requests differ from origin/main's:\n--- want\n%s--- got\n%s", want, got)
			}
			if n := predicateRequests(api); n != 0 {
				t.Errorf("full view made %d predicate calls", n)
			}
		})
	}
}

// TestPeopleTools_MissingFlagBehavesAsNoFullView walks the registry with a
// bare context: the outcome matches the denied caller, never the full view.
func TestPeopleTools_MissingFlagBehavesAsNoFullView(t *testing.T) {
	for _, tc := range peopleTools {
		t.Run(tc.name, func(t *testing.T) {
			api := setupPeopleToolsTest(t)
			api.GrantRelations()
			tc.setup(t, api, false)
			res, out, err := tc.call(context.Background())
			all := everything(t, res, out, err)
			for _, email := range tc.emails {
				if strings.Contains(all, email) {
					t.Errorf("a bare context returned %s", email)
				}
			}
		})
	}
}
