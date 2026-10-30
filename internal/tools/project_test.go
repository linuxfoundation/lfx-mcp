// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// projectDoc is a project resource shaped after lfx-v2-project-service
// docs/indexer-contract.md (Data Schema); values are test data.
func projectDoc(uid, slug, name, parentUID, legalParentUID string) string {
	return fmt.Sprintf(`{
	  "type": "project",
	  "id": %q,
	  "data": {
	    "uid": %q,
	    "slug": %q,
	    "name": %q,
	    "description": "",
	    "public": true,
	    "is_foundation": false,
	    "parent_uid": %q,
	    "stage": "Active",
	    "category": "Standards",
	    "legal_entity_type": "Series LLC",
	    "legal_entity_name": %q,
	    "legal_parent_uid": %q,
	    "autojoin_enabled": false,
	    "created_at": "2024-01-01T00:00:00Z",
	    "updated_at": "2026-01-01T00:00:00Z"
	  }
	}`, uid, uid, slug, name, parentUID, name+" LLC", legalParentUID)
}

func setupProjectTest(t *testing.T) *stubLFXAPI {
	t.Helper()
	api := newStubLFXAPI(t)
	prev := projectConfig
	SetProjectConfig(&ProjectConfig{Clients: api.Clients})
	t.Cleanup(func() { projectConfig = prev })
	return api
}

func TestSearchProjects_LegacyPayloadUnchanged(t *testing.T) {
	api := setupProjectTest(t)
	api.Respond(resourcesPath, page([]string{projectDoc("u1", "cncf", "CNCF", "", "")}, ""))

	res, _, _ := handleSearchProjects(context.Background(), stubCallToolRequest(), SearchProjectsArgs{Name: "cn", ParentUID: "tlf", PageSize: 5, PageToken: "tok"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	reqs := api.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected one request without include_total, got %d", len(reqs))
	}
	r := reqs[0]
	assertExchangedAuth(t, r)
	want := map[string]string{"v": "1", "type": "project", "name": "cn", "parent": "project:tlf", "page_size": "5", "page_token": "tok", "sort": "name_asc"}
	for k, v := range want {
		if r.Query.Get(k) != v {
			t.Errorf("%s: want %q got %q", k, v, r.Query.Get(k))
		}
	}
	for _, absent := range []string{"tags", "filters_all", "filters"} {
		if _, has := r.Query[absent]; has {
			t.Errorf("%s must be absent on a legacy call, got %v", absent, r.Query[absent])
		}
	}
	out := resultJSON(t, res)
	for _, absent := range []string{"total", "total_complete"} {
		if _, has := out[absent]; has {
			t.Errorf("%s must be omitted without include_total", absent)
		}
	}
}

func TestSearchProjects_NewFiltersMapToTagsAndFiltersAll(t *testing.T) {
	api := setupProjectTest(t)
	api.Respond(resourcesPath, page(nil, ""))

	handleSearchProjects(context.Background(), stubCallToolRequest(), SearchProjectsArgs{ //nolint:errcheck
		Slug:           "c2pa-fund",
		NameExact:      "Joint Development Foundation Projects, LLC",
		ParentUID:      "jdf-root",
		LegalParentUID: "jdf-llc-uid",
	})
	r := api.LastRequest()
	if got := r.Query["tags"]; len(got) != 1 || got[0] != "project_slug:c2pa-fund" {
		t.Errorf("slug tag: %v", got)
	}
	if r.Query.Get("parent") != "project:jdf-root" {
		t.Errorf("slug and parent_uid must coexist, parent=%q", r.Query.Get("parent"))
	}
	// flat_object: verbatim values, case preserved, no .keyword.
	wantFilters := []string{"name:Joint Development Foundation Projects, LLC", "legal_parent_uid:jdf-llc-uid"}
	if got := r.Query["filters_all"]; strings.Join(got, "|") != strings.Join(wantFilters, "|") {
		t.Errorf("filters_all: want %v got %v", wantFilters, got)
	}
	if _, has := r.Query["filters"]; has {
		t.Error("exact filters must be AND'd via filters_all, not OR'd via filters")
	}
}

func TestSearchProjects_IncludeTotalUsesIdenticalFilters(t *testing.T) {
	api := setupProjectTest(t)
	api.Respond(resourcesPath, page([]string{projectDoc("u1", "a", "A", "root", "llc"), projectDoc("u2", "b", "B", "root", "llc")}, "next"))
	api.Respond(countPath, `{"count": 12, "has_more": false}`)

	res, _, _ := handleSearchProjects(context.Background(), stubCallToolRequest(), SearchProjectsArgs{
		Name: "x", Slug: "a", ParentUID: "root", LegalParentUID: "llc", NameExact: "A", IncludeTotal: true, PageSize: 2,
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	reqs := api.Requests()
	if len(reqs) != 2 || reqs[0].Path != resourcesPath || reqs[1].Path != countPath {
		t.Fatalf("expected search then count, got %+v", reqs)
	}
	search, count := reqs[0].Query, reqs[1].Query
	for _, k := range []string{"v", "type", "name", "parent", "tags", "filters_all"} {
		if strings.Join(search[k], "|") != strings.Join(count[k], "|") {
			t.Errorf("count %s differs from search: %v vs %v", k, count[k], search[k])
		}
	}
	for _, k := range []string{"page_size", "page_token", "sort"} {
		if _, has := count[k]; has {
			t.Errorf("count route must not receive %s", k)
		}
	}
	assertExchangedAuth(t, reqs[1])
	out := resultJSON(t, res)
	if out["total"] != float64(12) || out["total_complete"] != true {
		t.Errorf("want total=12 total_complete=true, got %v %v", out["total"], out["total_complete"])
	}
	if out["page_token"] != "next" || len(out["resources"].([]any)) != 2 {
		t.Errorf("page must be returned alongside the total: %v", out)
	}
}

func TestSearchProjects_IncludeTotalLowerBound(t *testing.T) {
	api := setupProjectTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	api.Respond(countPath, `{"count": 100, "has_more": true}`)
	res, _, _ := handleSearchProjects(context.Background(), stubCallToolRequest(), SearchProjectsArgs{IncludeTotal: true})
	out := resultJSON(t, res)
	if out["total_complete"] != false || out["total"] != float64(100) {
		t.Errorf("has_more must surface as total_complete=false, got %v", out)
	}
}

func TestSearchProjects_IncludeTotalCountErrorDegradesToPage(t *testing.T) {
	// A failed count must not discard a successful page: total/total_complete
	// stay absent (never a complete zero) and a note names the recovery.
	api := setupProjectTest(t)
	api.Respond(resourcesPath, page([]string{projectDoc("u1", "a", "A", "", "")}, "next"))
	api.RespondStatus(countPath, http.StatusInternalServerError, `{"message":"search backend unavailable"}`)
	res, _, _ := handleSearchProjects(context.Background(), stubCallToolRequest(), SearchProjectsArgs{IncludeTotal: true})
	if res.IsError {
		t.Fatalf("page must be returned despite the count failure: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	if len(out["resources"].([]any)) != 1 || out["page_token"] != "next" {
		t.Errorf("page lost: %v", out)
	}
	for _, absent := range []string{"total", "total_complete"} {
		if _, has := out[absent]; has {
			t.Errorf("%s must be absent when the count failed (never a complete zero)", absent)
		}
	}
	note, _ := out["note"].(string)
	for _, want := range []string{"Include_total", "total unavailable", "search backend unavailable", "the requested page is retained"} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q: %q", want, note)
		}
	}
	if strings.Contains(strings.ToLower(note), "complete") {
		t.Errorf("a retained page with a next token must not claim completeness: %q", note)
	}

	// 403 on the count keeps the access-denied wording inside the note.
	api2 := setupProjectTest(t)
	api2.Respond(resourcesPath, page(nil, ""))
	api2.RespondStatus(countPath, http.StatusForbidden, `{"message":"forbidden"}`)
	res2, _, _ := handleSearchProjects(context.Background(), stubCallToolRequest(), SearchProjectsArgs{IncludeTotal: true})
	if res2.IsError || !strings.Contains(resultJSON(t, res2)["note"].(string), accessDeniedMessage) {
		t.Errorf("count 403 should degrade with the access-denied wording in the note, got %q", allResultText(t, res2))
	}
}

func TestSearchProjects_IndexScopeDescriptionAndSchema(t *testing.T) {
	tool := listRegisteredTool(t, "search_projects", RegisterSearchProjects)
	t.Logf("search_projects description: %d UTF-8 bytes", len(tool.Description))
	const want = "include_total adds total (over projects indexed in LFX v2 and visible to the caller, not the authoritative project directory) and total_complete (false means total is a lower bound)."
	if !strings.Contains(tool.Description, want) {
		t.Error("project totals must disclose index/caller scope and align false with total_complete")
	}
	const wantParent = "parent_uid lists the direct children (which may themselves have children) of a foundation or umbrella project;"
	if !strings.Contains(tool.Description, wantParent) {
		t.Error("parent_uid must explicitly describe direct children, not recursive descendants")
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("unexpected schema type %T", tool.InputSchema)
	}
	properties, _ := schema["properties"].(map[string]any)
	parent, _ := properties["parent_uid"].(map[string]any)
	const wantParentSchema = "UID of a foundation or umbrella project whose direct children to list"
	if got := parent["description"]; got != wantParentSchema {
		t.Errorf("parent_uid schema must pin direct-child scope: got %q", got)
	}
	includeTotal, _ := properties["include_total"].(map[string]any)
	const wantSchema = "Also return total (matching projects indexed in LFX v2 and visible to the caller, not the authoritative project directory) and total_complete (false means total is a lower bound)"
	if got := includeTotal["description"]; got != wantSchema {
		t.Errorf("include_total schema must disclose index scope and align false with total_complete: got %q", got)
	}
}

func TestSearchProjectsDescriptionMentionsEachNewParameter(t *testing.T) {
	tool := listRegisteredTool(t, "search_projects", RegisterSearchProjects)
	if n := len(tool.Description); n > 1000 {
		t.Errorf("description is %d bytes, keep it under 1000", n)
	}
	for _, want := range []string{"slug", "name_exact", "legal_parent_uid", "include_total", "total_complete", "indexed in LFX v2", "visible to the caller", "not the authoritative project directory"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("description missing %q", want)
		}
	}
	for _, banned := range []string{"Insights", "Jim", "eleven"} {
		if strings.Contains(tool.Description, banned) {
			t.Errorf("description must not contain %q", banned)
		}
	}

	// Keep the SM-2 optional project scope contract verbatim, after the
	// TOOLS-1 parameter documentation.
	const scopeContract = " The Linux Foundation's own entry (slug tlf) is one project bucket, not the LF-wide scope. LF-wide questions take no project on the query tools, query_lfx_lens included (omit project_slugs); pass project_slugs only to restrict to named projects."
	if !strings.HasSuffix(tool.Description, scopeContract) {
		t.Error("description must retain the SM-2 LF-wide/tlf contract verbatim")
	}
	if strings.Contains(strings.TrimSuffix(tool.Description, scopeContract), "because") {
		t.Error("TOOLS-1 parameter description must not contain rationale")
	}
}
