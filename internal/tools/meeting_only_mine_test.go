// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"strings"
	"testing"

	lfxauth "github.com/linuxfoundation/lfx-mcp/internal/auth"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// onlyMineSearch is one way of calling a meeting search tool with only_mine
// set: the tool, its naming mode, and the handler call for a given request.
type onlyMineSearch struct {
	name     string
	asGroups bool
	register func(*mcp.Server)
	call     func(ctx context.Context, req *mcp.CallToolRequest, onlyMine bool) (*mcp.CallToolResult, resourceSearchResult, error)
	wantType string
}

// onlyMineSearches covers search_meetings and search_past_meetings in both
// naming modes, so a test that walks it proves the argument reaches the query
// service through every registered entry point.
func onlyMineSearches() []onlyMineSearch {
	return []onlyMineSearch{
		{
			name: "search_meetings", asGroups: false,
			register: func(s *mcp.Server) { RegisterSearchMeetings(s, false) },
			call: func(ctx context.Context, req *mcp.CallToolRequest, onlyMine bool) (*mcp.CallToolResult, resourceSearchResult, error) {
				return handleSearchMeetings(ctx, req, SearchMeetingsArgs{OnlyMine: onlyMine})
			},
			wantType: meetingResourceType,
		},
		{
			name: "search_meetings", asGroups: true,
			register: func(s *mcp.Server) { RegisterSearchMeetings(s, true) },
			call: func(ctx context.Context, req *mcp.CallToolRequest, onlyMine bool) (*mcp.CallToolResult, resourceSearchResult, error) {
				return handleSearchMeetingsGroupMode(ctx, req, SearchMeetingsGroupArgs{OnlyMine: onlyMine})
			},
			wantType: meetingResourceType,
		},
		{
			name: "search_past_meetings", asGroups: false,
			register: func(s *mcp.Server) { RegisterSearchPastMeetings(s, false) },
			call: func(ctx context.Context, req *mcp.CallToolRequest, onlyMine bool) (*mcp.CallToolResult, resourceSearchResult, error) {
				return handleSearchPastMeetings(ctx, req, SearchPastMeetingsArgs{OnlyMine: onlyMine})
			},
			wantType: pastMeetingResourceType,
		},
		{
			name: "search_past_meetings", asGroups: true,
			register: func(s *mcp.Server) { RegisterSearchPastMeetings(s, true) },
			call: func(ctx context.Context, req *mcp.CallToolRequest, onlyMine bool) (*mcp.CallToolResult, resourceSearchResult, error) {
				return handleSearchPastMeetingsGroupMode(ctx, req, SearchPastMeetingsGroupArgs{OnlyMine: onlyMine})
			},
			wantType: pastMeetingResourceType,
		},
	}
}

func modeName(asGroups bool) string {
	if asGroups {
		return "groups"
	}
	return "committees"
}

// TestOnlyMine_SendsDirectGrantsFilter checks that only_mine reaches the
// query service as filter_grants=direct together with the record type the
// service requires next to it, under the caller's exchanged token, through
// both naming modes of both tools.
func TestOnlyMine_SendsDirectGrantsFilter(t *testing.T) {
	for _, tc := range onlyMineSearches() {
		t.Run(tc.name+"/"+modeName(tc.asGroups), func(t *testing.T) {
			api := setupParticipantTest(t)
			pinMeetingSearchNow(t, occurrenceTestNow)
			api.Respond(resourcesPath, page(nil, ""))

			res, _, err := tc.call(context.Background(), stubCallToolRequest(), true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.IsError {
				t.Fatalf("unexpected error result: %s", allResultText(t, res))
			}
			r := api.LastRequest()
			if r.Path != resourcesPath {
				t.Fatalf("expected a %s request, got %s", resourcesPath, r.Path)
			}
			assertExchangedAuth(t, r)
			if got := r.Query.Get("filter_grants"); got != "direct" {
				t.Errorf("filter_grants: want %q, got %q", "direct", got)
			}
			if got := r.Query.Get("type"); got != tc.wantType {
				t.Errorf("type: want %q, got %q (filter_grants requires type)", tc.wantType, got)
			}
		})
	}
}

// TestOnlyMine_UnsetSendsNoFilterGrants pins that a search without only_mine
// is byte-for-byte the request main sent: no filter_grants parameter at all.
func TestOnlyMine_UnsetSendsNoFilterGrants(t *testing.T) {
	for _, tc := range onlyMineSearches() {
		t.Run(tc.name+"/"+modeName(tc.asGroups), func(t *testing.T) {
			api := setupParticipantTest(t)
			pinMeetingSearchNow(t, occurrenceTestNow)
			api.Respond(resourcesPath, page(nil, ""))

			res, _, err := tc.call(context.Background(), stubCallToolRequest(), false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.IsError {
				t.Fatalf("unexpected error result: %s", allResultText(t, res))
			}
			if got, ok := api.LastRequest().Query["filter_grants"]; ok {
				t.Errorf("query must not carry filter_grants when only_mine is unset; got %v", got)
			}
		})
	}
}

// nonPersonRequest builds a CallToolRequest for a caller that is not a
// signed-in person: a machine (client-credentials) account or a static
// API-key integration, as the HTTP verifiers flag them.
func nonPersonRequest(extraKey string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{
		Extra: &mcp.RequestExtra{
			TokenInfo: &auth.TokenInfo{
				Extra: map[string]any{
					"raw_token": stubMCPToken,
					"username":  "service-client@clients",
					extraKey:    true,
				},
			},
		},
	}
}

// TestOnlyMine_RefusesMachineAndAPIKeyCallers checks that only_mine from a
// machine or API-key identity is a tool error before any upstream call: the
// query service would otherwise answer from the client's own, empty, grants
// and the empty page would read as "no meetings".
func TestOnlyMine_RefusesMachineAndAPIKeyCallers(t *testing.T) {
	for _, tc := range onlyMineSearches() {
		for _, kind := range []struct {
			name string
			key  string
		}{
			{"machine", lfxauth.MachineAccountExtraKey},
			{"api-key", lfxauth.APIKeyAuthExtraKey},
		} {
			t.Run(tc.name+"/"+modeName(tc.asGroups)+"/"+kind.name, func(t *testing.T) {
				api := setupParticipantTest(t)
				pinMeetingSearchNow(t, occurrenceTestNow)

				res, out, err := tc.call(fullViewCtx(), nonPersonRequest(kind.key), true)
				if err == nil {
					t.Fatalf("expected a tool error, got result %v", res)
				}
				if err.Error() != onlyMineNeedsLoginMessage {
					t.Errorf("error text: want %q, got %q", onlyMineNeedsLoginMessage, err.Error())
				}
				if res != nil || len(out.Resources) != 0 || out.PageToken != "" {
					t.Errorf("a refused call must publish no result or page, got res=%v out=%+v", res, out)
				}
				if got := api.RequestsTo(resourcesPath); len(got) != 0 {
					t.Errorf("a refused call must not reach the query service; saw %d request(s)", len(got))
				}
			})
		}
	}
}

// TestOnlyMine_MachineRefusalIsOnlyForOnlyMine pins that the refusal is tied
// to the argument: the same machine caller without only_mine searches as
// before.
func TestOnlyMine_MachineRefusalIsOnlyForOnlyMine(t *testing.T) {
	for _, tc := range onlyMineSearches() {
		t.Run(tc.name+"/"+modeName(tc.asGroups), func(t *testing.T) {
			api := setupParticipantTest(t)
			pinMeetingSearchNow(t, occurrenceTestNow)
			api.Respond(resourcesPath, page(nil, ""))

			res, _, err := tc.call(fullViewCtx(), nonPersonRequest(lfxauth.MachineAccountExtraKey), false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.IsError {
				t.Fatalf("unexpected error result: %s", allResultText(t, res))
			}
			if got := api.RequestsTo(resourcesPath); len(got) != 1 {
				t.Errorf("expected one query request, saw %d", len(got))
			}
		})
	}
}

// TestOnlyMine_DescribedInBothModes checks that only_mine is published, in
// both naming modes of both tools, as an optional boolean whose description
// names the LFX Self Serve view it matches and the login it needs, and that
// the tool description points at it.
func TestOnlyMine_DescribedInBothModes(t *testing.T) {
	for _, tc := range onlyMineSearches() {
		t.Run(tc.name+"/"+modeName(tc.asGroups), func(t *testing.T) {
			tool := listRegisteredTool(t, tc.name, tc.register)
			found := false
			for _, p := range schemaProperties(t, tool) {
				if p == "only_mine" {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s publishes no only_mine parameter", tc.name)
			}
			for _, req := range schemaRequired(t, tool) {
				if req == "only_mine" {
					t.Errorf("only_mine must be optional")
				}
			}
			desc := schemaPropertyDescription(t, tool, "only_mine")
			for _, want := range []string{"LFX Self Serve", "My Meetings", "under your LFX login", "Needs a personal login", "not that none exist"} {
				if !strings.Contains(desc, want) {
					t.Errorf("only_mine description missing %q: %q", want, desc)
				}
			}
			if !strings.Contains(tool.Description, "only_mine") || !strings.Contains(tool.Description, "My Meetings") {
				t.Errorf("%s description must point at only_mine and the My Meetings view: %q", tc.name, tool.Description)
			}
		})
	}
}

// staffPersonRequest builds a CallToolRequest for a signed-in LF staff
// member: a person with full view, and neither the machine nor the API-key
// flag.
func staffPersonRequest() *mcp.CallToolRequest {
	return &mcp.CallToolRequest{
		Extra: &mcp.RequestExtra{
			TokenInfo: &auth.TokenInfo{
				Extra: map[string]any{
					"raw_token":  stubMCPToken,
					"username":   stubCallerUsername,
					ClaimEmail:   stubCallerEmail,
					ClaimLFStaff: true,
				},
			},
		},
	}
}

// TestOnlyMine_StaffPersonPassesThrough pins that the refusal is about the
// kind of identity, not about full view: a staff member is a person with
// registrations of their own, and LFX Self Serve's My Meetings works for
// them, so only_mine reaches the service as for any other person. A guard
// written on the full-view flag would refuse them and pass every other test.
func TestOnlyMine_StaffPersonPassesThrough(t *testing.T) {
	for _, tc := range onlyMineSearches() {
		t.Run(tc.name+"/"+modeName(tc.asGroups), func(t *testing.T) {
			api := setupParticipantTest(t)
			pinMeetingSearchNow(t, occurrenceTestNow)
			api.Respond(resourcesPath, page(nil, ""))

			res, _, err := tc.call(fullViewCtx(), staffPersonRequest(), true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.IsError {
				t.Fatalf("unexpected error result: %s", allResultText(t, res))
			}
			if got := api.LastRequest().Query.Get("filter_grants"); got != "direct" {
				t.Errorf("filter_grants: want %q, got %q", "direct", got)
			}
		})
	}
}

// TestApplyOnlyMine_NoTokenPassesThrough pins the stdio case: with no token
// at all there is no identity to refuse on, so the filter is set and the
// service decides from the token it is configured with.
func TestApplyOnlyMine_NoTokenPassesThrough(t *testing.T) {
	resourceType := meetingResourceType
	payload := &querysvc.QueryResourcesPayload{Type: &resourceType}
	if err := applyOnlyMine(payload, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload.FilterGrants == nil || *payload.FilterGrants != "direct" {
		t.Errorf("filter_grants: want %q, got %v", "direct", payload.FilterGrants)
	}
}
