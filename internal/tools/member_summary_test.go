// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
)

const membershipSummaryPath = "/query/memberships/summary"

const wantSummaryContinuationWarning = "Continuation of an earlier summary read: this response holds the summaries from the supplied page_token onward; combine it with the earlier output. complete refers to the remainder of the read, not to the whole scope."

const summaryFirst = `{"b2b_org_uid":"o","company_name":"Example","project_uid":"p","project_slug":"example","term_count":1,"first_start":"2020-01-01","last_end":"2027-01-01","current_status":"Active","current_tier_name":"Gold","current_start":"2020-01-01","current_end":"2027-01-01","current_membership_uid":"m1","tier_names":["Gold"],"statuses":["Active"],"terms":[{"membership_uid":"m1","status":"Active","tier_name":"Gold","tier":"Large","start_date":"2020-01-01","end_date":"2027-01-01"}]}`
const summaryOther = `{"b2b_org_uid":"z","company_name":"Another","project_uid":"p","project_slug":"example","term_count":1,"tier_names":[],"statuses":[],"terms":[{"membership_uid":"m2","status":"","tier_name":""}]}`
const summarySplit = `{"b2b_org_uid":"o","company_name":"Example","project_uid":"p","project_slug":"example","term_count":1,"first_start":"2010-01-01","last_end":"2011-01-01","current_status":"Expired","current_start":"2010-01-01","current_end":"2011-01-01","current_membership_uid":"m0","tier_names":[],"statuses":["Expired"],"terms":[{"membership_uid":"m0","status":"Expired","tier_name":"","start_date":"2010-01-01","end_date":"2011-01-01"}]}`

func membershipSummaryPage(rows []string, total int, complete bool, token string) string {
	body := fmt.Sprintf(`{"summaries":[%s],"terms_total":%d,"complete":%t`, strings.Join(rows, ","), total, complete)
	if token != "" {
		body += fmt.Sprintf(`,"page_token":%q`, token)
	}
	return body + "}"
}

func TestSearchMembersSummary_Reads(t *testing.T) {
	var capped []string
	var capRows []string
	var capTokens []string
	for i := 0; i < maxMembershipSummaryReads; i++ {
		row := strings.Replace(summaryFirst, `"b2b_org_uid":"o"`, fmt.Sprintf(`"b2b_org_uid":"o%d"`, i), 1)
		capRows = append(capRows, row)
		capped = append(capped, membershipSummaryPage([]string{row}, 1, false, fmt.Sprintf("next-%d", i+1)))
		if i == 0 {
			capTokens = append(capTokens, "")
		} else {
			capTokens = append(capTokens, fmt.Sprintf("next-%d", i))
		}
	}
	for _, tc := range []struct {
		name      string
		args      SearchMembersArgs
		pages     []string
		tokens    []string
		wantRows  []string
		wantTotal uint64
		complete  bool
		wantToken string
		warning   string
	}{
		{
			name: "complete one read", args: SearchMembersArgs{ProjectUID: "p", B2bOrgUID: "o"},
			pages: []string{membershipSummaryPage([]string{summaryFirst}, 1, true, "")}, tokens: []string{""},
			wantRows: []string{summaryFirst}, wantTotal: 1, complete: true,
		},
		{
			name: "new pair on continuation preserves service order", args: SearchMembersArgs{ProjectUID: "p"},
			pages: []string{membershipSummaryPage([]string{summaryFirst}, 1, false, "next"), membershipSummaryPage([]string{summaryOther}, 1, true, "")}, tokens: []string{"", "next"},
			wantRows: []string{summaryFirst, summaryOther}, wantTotal: 2, complete: true,
		},
		{
			name: "split pair kept not folded and remains incomplete", args: SearchMembersArgs{ProjectUID: "p"},
			pages: []string{membershipSummaryPage([]string{summaryFirst}, 1, false, "next"), membershipSummaryPage([]string{summarySplit}, 1, false, "last"), membershipSummaryPage([]string{summaryOther}, 1, true, "")}, tokens: []string{"", "next", "last"},
			wantRows: []string{summaryFirst, summarySplit, summaryOther}, wantTotal: 3,
			warning: "The service split an organisation's membership records across reads, so its summary is partial; re-read with that organisation's b2b_org_uid and project_uid as the scope to get it whole.",
		},
		{
			name: "repeated records stay intact and totals are cumulative", args: SearchMembersArgs{ProjectUID: "p"},
			pages: []string{membershipSummaryPage([]string{summaryFirst}, 1, false, "next"), membershipSummaryPage([]string{summaryFirst}, 1, false, "last"), membershipSummaryPage([]string{summaryFirst}, 1, true, "")}, tokens: []string{"", "next", "last"},
			wantRows: []string{summaryFirst, summaryFirst, summaryFirst}, wantTotal: 3,
			warning: "The service split an organisation's membership records across reads, so its summary is partial; re-read with that organisation's b2b_org_uid and project_uid as the scope to get it whole.",
		},
		{
			name: "caller token starts continuation", args: SearchMembersArgs{B2bOrgUID: "o", PageToken: "start"},
			pages: []string{membershipSummaryPage([]string{summaryFirst}, 1, true, "")}, tokens: []string{"start"},
			wantRows: []string{summaryFirst}, wantTotal: 1, complete: true,
		},
		{
			name: "caller token warning precedes ignored parameters", args: SearchMembersArgs{B2bOrgUID: "o", PageToken: "start", IncludeInactive: true},
			pages: []string{membershipSummaryPage([]string{summaryFirst}, 1, true, "")}, tokens: []string{"start"},
			wantRows: []string{summaryFirst}, wantTotal: 1, complete: true,
			warning: "Ignored parameters for summary=true: include_inactive.",
		},
		{
			name: "bounded continuation", args: SearchMembersArgs{ProjectUID: "p"},
			pages: capped, tokens: capTokens, wantToken: fmt.Sprintf("next-%d", maxMembershipSummaryReads),
			wantRows: capRows, wantTotal: uint64(maxMembershipSummaryReads),
			warning: "This membership summary is partial; continue with summary=true and the returned page_token using the same project_uid and b2b_org_uid scope.",
		},
		{
			name: "same token stops", args: SearchMembersArgs{ProjectUID: "p"},
			pages: []string{membershipSummaryPage([]string{summaryFirst}, 1, false, "stuck"), membershipSummaryPage([]string{summaryOther}, 1, false, "stuck")}, tokens: []string{"", "stuck"},
			wantRows: []string{summaryFirst, summaryOther}, wantTotal: 2, wantToken: "stuck",
			warning: "This membership summary is partial because its page_token did not advance; re-read with the organisation's b2b_org_uid and project_uid as the scope.",
		},
		{
			name: "same as caller token stops immediately", args: SearchMembersArgs{B2bOrgUID: "o", PageToken: "stuck"},
			pages: []string{membershipSummaryPage(nil, 0, false, "stuck")}, tokens: []string{"stuck"}, wantToken: "stuck",
			warning: "This membership summary is partial because its page_token did not advance; re-read with the organisation's b2b_org_uid and project_uid as the scope.",
		},
		{
			name: "incomplete without token stops", args: SearchMembersArgs{B2bOrgUID: "o"},
			pages: []string{membershipSummaryPage([]string{summaryFirst}, 1, false, "")}, tokens: []string{""}, wantRows: []string{summaryFirst}, wantTotal: 1,
			warning: "This membership summary is partial and has no continuation token; re-read with the organisation's b2b_org_uid and project_uid as the scope.",
		},
		{
			name: "empty first read visibility warning", args: SearchMembersArgs{B2bOrgUID: "o"},
			pages: []string{membershipSummaryPage(nil, 0, true, "")}, tokens: []string{""}, complete: true,
			warning: "No membership summaries matching these filters are visible to you; results cover only records you can view, so this is not proof of absence.",
		},
		{
			name: "empty continued read is not absence", args: SearchMembersArgs{B2bOrgUID: "o", PageToken: "start"},
			pages: []string{membershipSummaryPage(nil, 0, true, "")}, tokens: []string{"start"}, complete: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMemberTest(t)
			for _, p := range tc.pages {
				api.Respond(membershipSummaryPath, p)
			}
			tc.args.Summary = true
			res, out, err := handleSearchMembers(context.Background(), stubCallToolRequest(), tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if out.Complete == nil || *out.Complete != tc.complete || out.TermsTotal == nil || *out.TermsTotal != tc.wantTotal || derefStr(out.PageToken) != tc.wantToken {
				t.Fatalf("unexpected output: %+v", out)
			}
			got := resultJSON(t, res)
			var wantRows []any
			if err := json.Unmarshal([]byte("["+strings.Join(tc.wantRows, ",")+"]"), &wantRows); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got["summaries"], wantRows) {
				t.Errorf("rows changed or reordered: got %#v, want %#v", got["summaries"], wantRows)
			}
			if _, exists := got["resources"]; exists {
				t.Error("summary must not emit list resources")
			}
			wantWarnings := []string(nil)
			if tc.args.PageToken != "" {
				wantWarnings = append(wantWarnings, wantSummaryContinuationWarning)
			}
			if tc.warning != "" {
				wantWarnings = append(wantWarnings, tc.warning)
			}
			if !reflect.DeepEqual(out.Warnings, wantWarnings) {
				t.Errorf("warnings = %q, want %q", out.Warnings, wantWarnings)
			}
			textWarnings(t, res, out.Warnings)
			requests := api.RequestsTo(membershipSummaryPath)
			if len(requests) != len(tc.tokens) || len(api.Requests()) != len(requests) {
				t.Fatalf("requests = %d, want %d summary reads only", len(api.Requests()), len(tc.tokens))
			}
			for i, r := range requests {
				assertExchangedAuth(t, r)
				if r.Method != http.MethodGet || r.Query.Get("v") != "1" || r.Query.Get("project_uid") != tc.args.ProjectUID || r.Query.Get("b2b_org_uid") != tc.args.B2bOrgUID || r.Query.Get("page_token") != tc.tokens[i] {
					t.Errorf("request %d changed scope/token: %v", i, r.Query)
				}
				for key := range r.Query {
					if key != "v" && key != "project_uid" && key != "b2b_org_uid" && key != "page_token" {
						t.Errorf("unexpected summary query parameter: %s", key)
					}
				}
			}
		})
	}
}

func TestMembershipSummaryKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  querysvc.MembershipTermSummary
		want [2]string
	}{
		{"UIDs exact not normalized", querysvc.MembershipTermSummary{B2bOrgUID: " O ", ProjectUID: "P", CompanyName: "unused", ProjectSlug: "unused"}, [2]string{"uid: O ", "uid:P"}},
		{"labels trimmed and lowercased", querysvc.MembershipTermSummary{CompanyName: " ÉXAMPLE ", ProjectSlug: " PROJECT "}, [2]string{"label:éxample", "label:project"}},
		{"missing project UID", querysvc.MembershipTermSummary{B2bOrgUID: "o", ProjectSlug: "One"}, [2]string{"uid:o", "label:one"}},
		{"missing organisation UID", querysvc.MembershipTermSummary{CompanyName: "One", ProjectUID: "p"}, [2]string{"label:one", "uid:p"}},
		{"empty labels remain a key", querysvc.MembershipTermSummary{}, [2]string{"label:", "label:"}},
		{"UID spelling in labels stays distinct", querysvc.MembershipTermSummary{CompanyName: "o", ProjectSlug: "p"}, [2]string{"label:o", "label:p"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := membershipSummaryKey(&tc.row); got != tc.want {
				t.Errorf("key = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSearchMembersSummary_RepeatFallbackKey(t *testing.T) {
	for _, tc := range []struct {
		name, org, project, company, slug string
		repeated                          bool
	}{
		{"same normalized fallback", "", "", " example ", " PROJECT ", true},
		{"different slug", "", "", "Example", "other", false},
		{"different company", "", "", "Other", "project", false},
		{"UID is not label", "example", "project", "Example", "project", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMemberTest(t)
			first := `{"b2b_org_uid":"","project_uid":"","company_name":"Example","project_slug":"project","term_count":0,"tier_names":[],"statuses":[],"terms":[]}`
			second := fmt.Sprintf(`{"b2b_org_uid":%q,"project_uid":%q,"company_name":%q,"project_slug":%q,"term_count":0,"tier_names":[],"statuses":[],"terms":[]}`, tc.org, tc.project, tc.company, tc.slug)
			api.Respond(membershipSummaryPath, membershipSummaryPage([]string{first}, 0, false, "next"))
			api.Respond(membershipSummaryPath, membershipSummaryPage([]string{second}, 0, true, ""))
			_, out, err := handleSearchMembers(context.Background(), stubCallToolRequest(), SearchMembersArgs{Summary: true, ProjectUID: "p"})
			if err != nil {
				t.Fatal(err)
			}
			if *out.Complete == tc.repeated || len(out.Summaries) != 2 || (len(out.Warnings) == 1) != tc.repeated {
				t.Fatalf("unexpected repeat outcome: %+v", out)
			}
		})
	}
}

func TestSearchMembersSummary_IgnoredParameters(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"all nonzero", map[string]any{"search_name": "Example", "tier_uid": "tier", "tier_name": "Gold", "include_inactive": true, "page_size": 99}, "search_name, tier_uid, tier_name, include_inactive, page_size"},
		{"explicit zero values", map[string]any{"search_name": "", "tier_uid": "", "tier_name": "", "include_inactive": false, "page_size": 0}, "search_name, tier_uid, tier_name, include_inactive, page_size"},
		{"only page size", map[string]any{"page_size": -1}, "page_size"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMemberTest(t)
			api.Respond(membershipSummaryPath, membershipSummaryPage([]string{summaryFirst}, 1, true, ""))
			tc.args["summary"] = true
			tc.args["project_uid"] = "p"
			_, res := callToolWithToken(t, "search_members", RegisterSearchMembers, tc.args)
			if res.IsError {
				t.Fatal(allResultText(t, res))
			}
			got := resultJSON(t, res)
			want := []any{"Ignored parameters for summary=true: " + tc.want + "."}
			if !reflect.DeepEqual(got["warnings"], want) {
				t.Errorf("warnings = %v, want %v", got["warnings"], want)
			}
			if q := api.LastRequest().Query; len(q) != 2 || q.Get("v") != "1" || q.Get("project_uid") != "p" {
				t.Errorf("ignored args leaked into summary request: %v", q)
			}
		})
	}
}

func TestSearchMembersSummary_ErrorsHaveNoOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		resume bool
		want   string
	}{
		{"missing scope delegated to service", http.StatusBadRequest, `{"message":"at least one summary parameter must be provided: project_uid or b2b_org_uid"}`, false, "at least one summary parameter"},
		{"forbidden", http.StatusForbidden, `{"message":"not allowed"}`, false, "Failed to search members:"},
		{"continuation fails", http.StatusInternalServerError, `{"message":"unavailable"}`, true, "Failed to search members:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMemberTest(t)
			if tc.resume {
				api.Respond(membershipSummaryPath, membershipSummaryPage([]string{summaryFirst}, 1, false, "next"))
			}
			api.RespondStatus(membershipSummaryPath, tc.status, tc.body)
			_, res := callToolWithToken(t, "search_members", RegisterSearchMembers, map[string]any{"summary": true})
			if !res.IsError || res.StructuredContent != nil || !strings.Contains(allResultText(t, res), tc.want) {
				t.Fatalf("expected error without partial output: %+v", res)
			}
			wantReads := 1
			if tc.resume {
				wantReads++
			}
			if len(api.Requests()) != wantReads {
				t.Errorf("must delegate to summary service, requests = %d", len(api.Requests()))
			}
		})
	}
}

func TestSearchMembers_OutputModesOverWire(t *testing.T) {
	for _, tc := range []struct {
		name    string
		summary bool
		rows    string
	}{
		{"list empty", false, ""},
		{"list populated", false, `{"type":"project_membership","id":"m1","data":{"tier":"Large","tier_name":"Gold","project_sfid":"internal"}}`},
		{"summary empty", true, ""},
		{"summary populated", true, summaryFirst},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMemberTest(t)
			if tc.summary {
				total := 0
				if tc.rows != "" {
					total = 1
				}
				api.Respond(membershipSummaryPath, membershipSummaryPage([]string{tc.rows}, total, true, ""))
			} else {
				api.Respond(resourcesPath, `{"resources":[`+tc.rows+`]}`)
			}
			tool, res := callToolWithToken(t, "search_members", RegisterSearchMembers, map[string]any{"project_uid": "p", "summary": tc.summary})
			if res.IsError {
				t.Fatal(allResultText(t, res))
			}
			if !schemaHasProperties(t, tool.OutputSchema, "resources", "summaries", "terms_total", "complete", "warnings") || !schemaHasProperties(t, tool.InputSchema, "summary") {
				t.Fatal("typed schemas must include both modes and summary input")
			}
			text := resultJSON(t, res)
			raw, err := json.Marshal(res.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			var structured map[string]any
			if err := json.Unmarshal(raw, &structured); err != nil {
				t.Fatal(err)
			}
			if len(res.Content) != 1 || !reflect.DeepEqual(text, structured) {
				t.Errorf("one text block and matching structured output required: %v vs %v", text, structured)
			}
			if tc.summary {
				if _, ok := text["resources"]; ok {
					t.Error("resources in summary output")
				}
				if _, ok := text["summaries"].([]any); !ok {
					t.Error("summary array missing or null")
				}
			} else {
				for _, field := range []string{"summaries", "terms_total", "complete"} {
					if _, ok := text[field]; ok {
						t.Errorf("summary field %s in ordinary list", field)
					}
				}
				rows, ok := text["resources"].([]any)
				if !ok {
					t.Fatal("list resources must remain an array even when empty")
				}
				if tc.rows != "" {
					want := map[string]any{"type": "project_membership", "id": "m1", "data": map[string]any{"tier_range": "Large", "tier_name": "Gold"}}
					if len(rows) != 1 || !reflect.DeepEqual(rows[0], want) {
						t.Errorf("list view changed: %v", rows)
					}
				}
				r := api.LastRequest()
				if r.Path != resourcesPath || !reflect.DeepEqual(r.Query["filters_all"], []string{"project_uid:p", "status:Active"}) || r.Query.Get("page_size") != "10" {
					t.Errorf("ordinary list query changed: %v", r.Query)
				}
			}
		})
	}
}
