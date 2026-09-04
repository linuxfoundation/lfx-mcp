// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// countPath is the query-service count route (vendored client v0.4.22).
const countPath = "/query/resources/count"

// setupCountTest points the shared projectConfig (which count_lfx_resources
// borrows for its clients) at a stub LFX API.
func setupCountTest(t *testing.T) *stubLFXAPI {
	t.Helper()
	api := newStubLFXAPI(t)
	prev := projectConfig
	SetProjectConfig(&ProjectConfig{Clients: api.Clients})
	t.Cleanup(func() { projectConfig = prev })
	return api
}

func TestCountLFXResources_PayloadMapping(t *testing.T) {
	api := setupCountTest(t)
	api.Respond(countPath, `{"count": 42, "has_more": false}`)

	res, _, err := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{
		Type:       "v1_past_meeting",
		Parent:     "project:a09410000182dD2AAI",
		Name:       "TOC",
		Tags:       []string{"is_attended:true", "project_slug:cncf"},
		TagsAll:    []string{"committee_uid:abc"},
		DateField:  "start_time",
		DateFrom:   "2026-01-01",
		DateTo:     "2026-06-30",
		Filters:    []string{"org_name:Red Hat"},
		FiltersAll: []string{"is_invited:true"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}

	r := api.LastRequest()
	if r.Method != http.MethodGet || r.Path != countPath {
		t.Fatalf("expected GET %s, got %s %s", countPath, r.Method, r.Path)
	}
	assertExchangedAuth(t, r)

	want := map[string][]string{
		"v":           {"1"},
		"type":        {"v1_past_meeting"},
		"parent":      {"project:a09410000182dD2AAI"},
		"name":        {"TOC"},
		"tags":        {"is_attended:true", "project_slug:cncf"},
		"tags_all":    {"committee_uid:abc"},
		"date_field":  {"start_time"},
		"date_from":   {"2026-01-01"},
		"date_to":     {"2026-06-30"},
		"filters":     {"org_name:Red Hat"},
		"filters_all": {"is_invited:true"},
	}
	for k, v := range want {
		if got := r.Query[k]; strings.Join(got, "|") != strings.Join(v, "|") {
			t.Errorf("query %s: want %v, got %v", k, v, got)
		}
	}
	for k := range r.Query {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected query param %s=%v", k, r.Query[k])
		}
	}

	out := resultJSON(t, res)
	if out["count"] != float64(42) || out["complete"] != true || out["visibility"] != "caller" {
		t.Errorf("unexpected result: %v", out)
	}
	if note, _ := out["note"].(string); note != callerVisibilityNote {
		t.Errorf("complete count must carry only the visibility note, got %q", note)
	}
}

func TestCountLFXResources_MinimalPayloadOmitsUnsetFields(t *testing.T) {
	api := setupCountTest(t)
	api.Respond(countPath, `{"count": 0, "has_more": false}`)

	res, _, _ := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{Type: "committee"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	r := api.LastRequest()
	if len(r.Query) != 2 || r.Query.Get("v") != "1" || r.Query.Get("type") != "committee" {
		t.Errorf("expected only v and type, got %v", r.Query)
	}
}

func TestCountLFXResources_HasMoreIsLowerBound(t *testing.T) {
	api := setupCountTest(t)
	api.Respond(countPath, `{"count": 100, "has_more": true}`)

	res, _, _ := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{Type: "v1_past_meeting_participant"})
	if res.IsError {
		t.Fatalf("unexpected error result: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	if out["complete"] != false {
		t.Errorf("has_more:true must yield complete:false, got %v", out["complete"])
	}
	note, _ := out["note"].(string)
	if !strings.Contains(note, "lower bound") || !strings.Contains(note, callerVisibilityNote) {
		t.Errorf("lower-bound note missing or missing visibility sentence: %q", note)
	}
	if out["count"] != float64(100) {
		t.Errorf("count must still be reported, got %v", out["count"])
	}
}

func TestCountLFXResources_RejectsUnknownType(t *testing.T) {
	api := setupCountTest(t)

	res, _, _ := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{Type: "v1_past_meeting_summary"})
	if !res.IsError {
		t.Fatal("expected an error result for an uncountable type")
	}
	text := allResultText(t, res)
	for _, want := range []string{"v1_past_meeting_summary", "valid types", "committee_member", "groupsio_member"} {
		if !strings.Contains(text, want) {
			t.Errorf("type error should name %q, got %q", want, text)
		}
	}
	if len(api.Requests()) != 0 {
		t.Error("invalid type must not reach the query service")
	}
}

func TestCountLFXResources_EveryListedTypeIsAccepted(t *testing.T) {
	api := setupCountTest(t)
	for _, typ := range countableResourceTypes {
		api.Respond(countPath, `{"count": 1, "has_more": false}`)
		res, _, _ := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{Type: typ})
		if res.IsError {
			t.Errorf("type %s rejected: %s", typ, allResultText(t, res))
		}
	}
	// The description must advertise exactly the accepted set.
	// The type list lives on the required parameter, which survives
	// schema compaction (see tool-description-budget lesson).
	tag := countTypeTag(t)
	for _, typ := range countableResourceTypes {
		if !strings.Contains(tag, typ) {
			t.Errorf("type %s is accepted but not advertised on the type parameter", typ)
		}
	}
}

func TestCountLFXResources_DateRangeRequiresDateField(t *testing.T) {
	api := setupCountTest(t)
	for _, args := range []CountLFXResourcesArgs{
		{Type: "v1_meeting", DateFrom: "2026-01-01"},
		{Type: "v1_meeting", DateTo: "2026-01-31"},
	} {
		res, _, _ := handleCountLFXResources(context.Background(), stubCallToolRequest(), args)
		if !res.IsError || !strings.Contains(allResultText(t, res), "date_field") {
			t.Errorf("expected a date_field error for %+v, got %q", args, allResultText(t, res))
		}
	}
	if len(api.Requests()) != 0 {
		t.Error("date guard must fire before the query service is called")
	}
}

func TestCountLFXResources_APIErrorIsFriendly(t *testing.T) {
	api := setupCountTest(t)
	api.RespondStatus(countPath, http.StatusForbidden, `{"message":"forbidden"}`)

	res, _, _ := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{Type: "project"})
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if text := allResultText(t, res); !strings.Contains(text, accessDeniedMessage) {
		t.Errorf("403 should map to the access-denied wording, got %q", text)
	}
}

func TestCountLFXResources_MissingTokenFails(t *testing.T) {
	api := setupCountTest(t)
	// A stdio-style request has no token info; the tool must fail closed.
	req := stubCallToolRequest()
	req.Extra.TokenInfo = nil
	res, _, _ := handleCountLFXResources(context.Background(), req, CountLFXResourcesArgs{Type: "project"})
	if !res.IsError {
		t.Fatal("expected an error without a caller token")
	}
	if len(api.Requests()) != 0 {
		t.Error("no query-service call may be made without a caller token")
	}
}

func TestCountLFXResources_DescriptionBudgetAndContent(t *testing.T) {
	tool := listRegisteredTool(t, "count_lfx_resources", RegisterCountLFXResources)
	if n := len(tool.Description); n > 1000 {
		t.Errorf("description is %d bytes, budget is 1000", n)
	}
	for _, want := range []string{"visible to the caller", "complete=false", "lower bound", "project:<uid>", "date_field", "filters_all"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("description missing %q", want)
		}
	}
	for _, banned := range []string{"Insights", "because", "Jim"} {
		if strings.Contains(tool.Description, banned) {
			t.Errorf("description must not contain %q (rationale or absolute figure)", banned)
		}
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Error("count_lfx_resources must be read-only")
	}
}

// countTypeTag returns the jsonschema description of the required type arg.
func countTypeTag(t *testing.T) string {
	t.Helper()
	tool := listRegisteredTool(t, "count_lfx_resources", RegisterCountLFXResources)
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		return ""
	}
	props, _ := schema["properties"].(map[string]any)
	typ, _ := props["type"].(map[string]any)
	desc, _ := typ["description"].(string)
	return desc
}
