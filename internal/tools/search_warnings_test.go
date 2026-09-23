// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSearchWarnings(t *testing.T) {
	partial := searchWarnings("meetings", 3, 10, true, false)
	emptyMore := searchWarnings("meetings", 0, 10, true, false)
	notVisible := searchWarnings("meetings", 0, 10, false, false)

	for _, tc := range []struct {
		name         string
		visible      int
		hasToken     bool
		continuation bool
		want         []string
	}{
		{"partial page with token", 3, true, false, partial},
		{"partial continuation page with token", 3, true, true, partial},
		{"empty page with token", 0, true, false, emptyMore},
		{"empty continuation page with token", 0, true, true, emptyMore},
		{"empty first page without token", 0, false, false, notVisible},
		{"empty terminal page of a walk", 0, false, true, nil},
		{"full page with token", 10, true, false, nil},
		{"partial page without token", 3, false, false, nil},
		{"partial terminal page of a walk", 3, false, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := searchWarnings("meetings", tc.visible, 10, tc.hasToken, tc.continuation)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}

	for _, w := range [][]string{partial, emptyMore, notVisible} {
		if len(w) != 1 {
			t.Fatalf("expected one warning, got %q", w)
		}
		if !strings.Contains(w[0], "meetings") {
			t.Errorf("warning must name the records: %q", w[0])
		}
		if strings.ContainsAny(w[0], "0123456789") {
			t.Errorf("warning must carry no numbers: %q", w[0])
		}
		for _, banned := range []string{"permission to view them", "exist", "withheld", "Insights", "WARNING:"} {
			if strings.Contains(w[0], banned) {
				t.Errorf("warning must not contain %q: %q", banned, w[0])
			}
		}
	}
	if !strings.Contains(partial[0], "continue with page_token") || !strings.Contains(emptyMore[0], "continue with page_token") {
		t.Error("warnings on a page with a token must say to continue with page_token")
	}
	if !strings.Contains(notVisible[0], "not proof of absence") || !strings.Contains(notVisible[0], "matching these filters") {
		t.Errorf("empty-page warning must be scoped to the query and not read as absence: %q", notVisible[0])
	}
	// The terminal page of a walk carries no warning, so the empty page with
	// a token must itself carry the visibility caveat and never invite an
	// absence conclusion.
	if !strings.Contains(emptyMore[0], "only records you can view") || strings.Contains(emptyMore[0], "concluding") {
		t.Errorf("empty page with a token must state the visibility caveat: %q", emptyMore[0])
	}
}

func TestNewResourceSearchResult(t *testing.T) {
	id, next := "r1", "next"
	out := newResourceSearchResult("meetings", &querysvc.QueryResourcesResult{
		Resources: []*querysvc.Resource{nil, {ID: &id}},
		PageToken: &next,
	}, 10, false)
	if len(out.Resources) != 1 || out.Resources[0].ID != id {
		t.Errorf("nil entries must be dropped and the rest kept, got %+v", out.Resources)
	}
	if out.PageToken != next {
		t.Errorf("page token must be passed through, got %q", out.PageToken)
	}
	if !reflect.DeepEqual(out.Warnings, searchWarnings("meetings", 1, 10, true, false)) {
		t.Errorf("warnings must count only the records returned, got %q", out.Warnings)
	}

	empty := newResourceSearchResult("meetings", &querysvc.QueryResourcesResult{}, 10, true)
	raw, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"resources":[]}` {
		t.Errorf("an empty terminal page is an empty list with no token or warnings, got %s", raw)
	}
}

func TestNewSearchResource(t *testing.T) {
	typ, id := "meeting", "m1"
	obj := map[string]any{"title": "Weekly"}
	for _, tc := range []struct {
		name string
		in   *querysvc.Resource
		want string
	}{
		{"object data is kept", &querysvc.Resource{Type: &typ, ID: &id, Data: obj}, `{"Type":"meeting","ID":"m1","Data":{"title":"Weekly"}}`},
		{"other data is kept under _raw", &querysvc.Resource{Type: &typ, ID: &id, Data: []any{"a"}}, `{"Type":"meeting","ID":"m1","Data":{"_raw":["a"]}}`},
		{"missing fields are plain values", &querysvc.Resource{}, `{"Type":"","ID":""}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(newSearchResource(tc.in))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(raw) != tc.want {
				t.Errorf("want %s, got %s", tc.want, raw)
			}
		})
	}
}

func TestHasPageToken(t *testing.T) {
	empty, next := "", "next"
	if hasPageToken(nil) || hasPageToken(&empty) || !hasPageToken(&next) {
		t.Error("hasPageToken must be true only for a non-empty token")
	}
}

// textWarnings returns the warnings key of a one-block JSON result and fails
// unless it equals the structured warnings.
func textWarnings(t *testing.T, res *mcp.CallToolResult, structured []string) []string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("expected exactly one content block, got %d", len(res.Content))
	}
	var text struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &text); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if !reflect.DeepEqual(text.Warnings, structured) {
		t.Errorf("text warnings %q differ from structured warnings %q", text.Warnings, structured)
	}
	return text.Warnings
}

func TestSearchMeetings_Warnings(t *testing.T) {
	api := setupMeetingLookupTest(t)
	pinMeetingSearchNow(t, beforeJoinFieldsOccurrences)

	// Empty first page without a token.
	api.Respond(resourcesPath, page(nil, ""))
	res, out, _ := handleSearchMeetings(context.Background(), stubCallToolRequest(), SearchMeetingsArgs{ProjectUID: "p"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	if got := textWarnings(t, res, out.Warnings); !reflect.DeepEqual(got, searchWarnings("meetings", 0, 10, false, false)) {
		t.Errorf("empty first page: got %q", got)
	}

	// Empty terminal page of a walk: no warnings key at all.
	api.Respond(resourcesPath, page(nil, ""))
	res, out, _ = handleSearchMeetings(context.Background(), stubCallToolRequest(), SearchMeetingsArgs{ProjectUID: "p", PageToken: "prev"})
	textWarnings(t, res, out.Warnings)
	if _, has := resultJSON(t, res)["warnings"]; has {
		t.Errorf("an empty continuation page carries no warnings key: %s", allResultText(t, res))
	}

	// Partial page with a token: continue warning, join fields still trimmed.
	api.Respond(resourcesPath, `{"resources": [{"type": "v1_meeting", "id": "meeting-1", "data": `+meetingDocWithJoinFields+`}], "page_token": "next"}`)
	res, out, _ = handleSearchMeetings(context.Background(), stubCallToolRequest(), SearchMeetingsArgs{ProjectUID: "p", PageSize: 2})
	if got := textWarnings(t, res, out.Warnings); !reflect.DeepEqual(got, searchWarnings("meetings", 1, 2, true, false)) || got == nil {
		t.Errorf("partial page: got %q", got)
	}
	if out.PageToken != "next" {
		t.Errorf("page token must be passed through, got %q", out.PageToken)
	}
	resources := resultJSON(t, res)["resources"].([]any)
	assertMeetingJoinFieldsTrimmed(t, resources[0].(map[string]any)["Data"].(map[string]any))
}

// callToolWithToken registers a tool on an in-memory server that stamps the
// stub MCP token on every request, calls it, and returns the listed tool and
// the call result as the client sees them.
func callToolWithToken(t *testing.T, name string, register func(*mcp.Server), args map[string]any) (*mcp.Tool, *mcp.CallToolResult) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "0.0.1"}, nil)
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if r, ok := req.(*mcp.CallToolRequest); ok {
				r.Extra = &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{Extra: map[string]any{"raw_token": stubMCPToken}}}
			}
			return next(ctx, method, req)
		}
	})
	register(server)

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect failed: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect failed: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	list, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	var tool *mcp.Tool
	for _, tl := range list.Tools {
		if tl.Name == name {
			tool = tl
		}
	}
	if tool == nil {
		t.Fatalf("%s not found in tool list", name)
	}
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	return tool, res
}

func TestSearchMeetings_StructuredWarningsOverTheWire(t *testing.T) {
	api := setupMeetingLookupTest(t)
	api.Respond(resourcesPath, page(nil, ""))

	tool, res := callToolWithToken(t, "search_meetings", func(s *mcp.Server) { RegisterSearchMeetings(s, false) }, map[string]any{"project_uid": "p"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	if !schemaHasProperties(t, tool.OutputSchema, "resources", "warnings") {
		t.Fatalf("search_meetings must publish an output schema with resources and warnings: %v", tool.OutputSchema)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var structured struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(raw, &structured); err != nil {
		t.Fatalf("structured content is not the result object: %v", err)
	}
	if len(structured.Warnings) != 1 {
		t.Fatalf("expected one structured warning, got %q", structured.Warnings)
	}
	textWarnings(t, res, structured.Warnings)
}

// TestSearchMeetings_ResourceOverTheWire checks that a returned resource
// passes the published output schema and keeps the query service's keys.
func TestSearchMeetings_ResourceOverTheWire(t *testing.T) {
	api := setupMeetingLookupTest(t)
	api.Respond(resourcesPath, page([]string{`{"type": "meeting", "id": "m1", "data": {"title": "Weekly"}}`}, ""))

	_, res := callToolWithToken(t, "search_meetings", func(s *mcp.Server) { RegisterSearchMeetings(s, false) }, map[string]any{"project_uid": "p"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if want := `{"resources":[{"Data":{"title":"Weekly"},"ID":"m1","Type":"meeting"}]}`; string(raw) != want {
		t.Errorf("want %s, got %s", want, raw)
	}
}

// TestSearchTools_ErrorCarriesNoStructuredContent checks that a failed call
// is an error result with no structured content, so it cannot read as an
// empty page.
func TestSearchTools_ErrorCarriesNoStructuredContent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setup    func(*testing.T) *stubLFXAPI
		register func(*mcp.Server)
		args     map[string]any
	}{
		{"search_meetings", setupMeetingLookupTest, func(s *mcp.Server) { RegisterSearchMeetings(s, false) }, map[string]any{"project_uid": "p"}},
		{"search_mailing_lists", setupMailingListTest, RegisterSearchMailingLists, map[string]any{"project_uid": "p"}},
		{"search_projects", setupProjectTest, RegisterSearchProjects, map[string]any{"name": "p"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := tc.setup(t)
			api.RespondStatus(resourcesPath, http.StatusInternalServerError, `{"message":"search backend unavailable"}`)

			_, res := callToolWithToken(t, tc.name, tc.register, tc.args)
			if !res.IsError {
				t.Fatalf("expected an error result, got %s", allResultText(t, res))
			}
			if res.StructuredContent != nil {
				t.Errorf("an error result must carry no structured content, got %v", res.StructuredContent)
			}
			if text := allResultText(t, res); !strings.Contains(text, "unavailable") {
				t.Errorf("error text must carry the upstream message, got %q", text)
			}
		})
	}
}

// schemaHasProperties reports whether a JSON schema declares every name as a
// property.
func schemaHasProperties(t *testing.T, schema any, names ...string) bool {
	t.Helper()
	if schema == nil {
		return false
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	for _, n := range names {
		if _, ok := s.Properties[n]; !ok {
			return false
		}
	}
	return true
}

// TestSearchTools_PublishWarningsOutputSchema pins an output schema carrying
// resources and warnings on every query-backed search tool that returns one.
func TestSearchTools_PublishWarningsOutputSchema(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
	}{
		{"search_meetings", func(s *mcp.Server) { RegisterSearchMeetings(s, false) }},
		{"search_meetings", func(s *mcp.Server) { RegisterSearchMeetings(s, true) }},
		{"search_meeting_registrants", func(s *mcp.Server) { RegisterSearchMeetingRegistrants(s, false) }},
		{"search_meeting_registrants", func(s *mcp.Server) { RegisterSearchMeetingRegistrants(s, true) }},
		{"search_past_meetings", func(s *mcp.Server) { RegisterSearchPastMeetings(s, false) }},
		{"search_past_meetings", func(s *mcp.Server) { RegisterSearchPastMeetings(s, true) }},
		{"search_past_meeting_summaries", RegisterSearchPastMeetingSummaries},
		{"search_mailing_lists", RegisterSearchMailingLists},
		{"search_mailing_list_members", RegisterSearchMailingListMembers},
		{"search_b2b_orgs", RegisterSearchB2bOrgs},
		{"search_projects", RegisterSearchProjects},
		{"search_members", RegisterSearchMembers},
		{"get_membership_key_contacts", RegisterGetMembershipKeyContacts},
		{"search_committees", func(s *mcp.Server) { RegisterSearchCommittees(s, false) }},
		{"search_groups", func(s *mcp.Server) { RegisterSearchCommittees(s, true) }},
		{"search_committee_members", func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, false) }},
		{"search_group_members", func(s *mcp.Server) { RegisterSearchCommitteeMembers(s, true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := listRegisteredTool(t, tc.name, tc.register)
			if !schemaHasProperties(t, tool.OutputSchema, "resources", "warnings") {
				t.Errorf("%s must publish an output schema with resources and warnings", tc.name)
			}
			// A resource's Data is a plain object and its Type and ID plain
			// strings: no bare true schema and no nullable item fields
			// (lfx-mcp#154).
			raw, err := json.Marshal(tool.OutputSchema)
			if err != nil {
				t.Fatalf("marshal schema: %v", err)
			}
			for _, bad := range []string{`"Data":true`, `"Type":{"type":["null","string"]}`, `"ID":{"type":["null","string"]}`} {
				if strings.Contains(string(raw), bad) {
					t.Errorf("%s output schema must not contain %s: %s", tc.name, bad, raw)
				}
			}
		})
	}
}

// setupMemberTest points the member and b2b org tools at a stub LFX API.
func setupMemberTest(t *testing.T) *stubLFXAPI {
	t.Helper()
	api := newStubLFXAPI(t)
	prev := memberConfig
	SetMemberConfig(&MemberConfig{Clients: api.Clients})
	t.Cleanup(func() { memberConfig = prev })
	return api
}

func TestSearchB2bOrgs_PartialPageWarning(t *testing.T) {
	api := setupMemberTest(t)
	api.Respond(resourcesPath, page([]string{`{"type": "b2b_org", "id": "o1", "data": {"name": "Example Org"}}`}, "next"))

	res, out, _ := handleSearchB2bOrgs(context.Background(), stubCallToolRequest(), SearchB2bOrgsArgs{SearchName: "exa", PageSize: 5})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	if got := textWarnings(t, res, out.Warnings); !reflect.DeepEqual(got, searchWarnings("organizations", 1, 5, true, false)) || got == nil {
		t.Errorf("partial page: got %q", got)
	}
}

func TestSearchMembers_EmptyPageWithTokenWarning(t *testing.T) {
	api := setupMemberTest(t)
	api.Respond(resourcesPath, page(nil, "next"))

	res, out, _ := handleSearchMembers(context.Background(), stubCallToolRequest(), SearchMembersArgs{ProjectUID: "p"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	if got := textWarnings(t, res, out.Warnings); !reflect.DeepEqual(got, searchWarnings("memberships", 0, 10, true, false)) || got == nil {
		t.Errorf("empty page with token: got %q", got)
	}
}

func TestSearchProjects_WarningsSeparateFromIncludeTotalNote(t *testing.T) {
	api := setupProjectTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	api.RespondStatus(countPath, http.StatusInternalServerError, `{"message":"search backend unavailable"}`)

	res, out, _ := handleSearchProjects(context.Background(), stubCallToolRequest(), SearchProjectsArgs{Name: "x", IncludeTotal: true})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	if got := textWarnings(t, res, out.Warnings); !reflect.DeepEqual(got, searchWarnings("projects", 0, 10, false, false)) {
		t.Errorf("empty first page: got %q", got)
	}
	if !strings.Contains(out.Note, "total unavailable") || strings.Contains(out.Note, "visible") {
		t.Errorf("note must carry only the include_total failure: %q", out.Note)
	}
}

func TestSearchGroups_EmptyPageWarningSaysGroups(t *testing.T) {
	api := setupCommitteeTest(t)
	api.Respond(resourcesPath, page(nil, ""))

	res, out, _ := handleSearchCommitteesGroupMode(context.Background(), stubCallToolRequest(), SearchGroupsArgs{ProjectUID: "p"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	got := textWarnings(t, res, out.Warnings)
	if !reflect.DeepEqual(got, searchWarnings("groups", 0, 10, false, false)) {
		t.Errorf("empty group page: got %q", got)
	}
	if strings.Contains(allResultText(t, res), "committee") {
		t.Errorf("group mode must not say committee: %s", allResultText(t, res))
	}
}
