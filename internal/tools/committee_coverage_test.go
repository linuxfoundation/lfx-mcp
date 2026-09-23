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

// committeeDoc is one committee resource as the indexer stores it; values
// are test data.
func committeeDoc(uid, name, category, projectUID string) string {
	return fmt.Sprintf(`{
	  "type": "committee",
	  "id": %q,
	  "data": {
	    "uid": %q,
	    "name": %q,
	    "category": %q,
	    "project_uid": %q,
	    "description": "",
	    "public": true,
	    "total_members": 0,
	    "created_at": "2025-01-01T00:00:00Z",
	    "updated_at": "2026-01-01T00:00:00Z"
	  }
	}`, uid, uid, name, category, projectUID)
}

// groupedCountDoc builds a count response with groups.
func groupedCountDoc(count uint64, hasMore bool, groupsComplete *bool, groups map[string]uint64) string {
	var parts []string
	for k, v := range groups {
		parts = append(parts, fmt.Sprintf(`{"key": %q, "count": %d}`, k, v))
	}
	body := fmt.Sprintf(`{"count": %d, "has_more": %t`, count, hasMore)
	if len(parts) > 0 {
		body += `, "groups": [` + strings.Join(parts, ",") + `]`
	}
	if groupsComplete != nil {
		body += fmt.Sprintf(`, "groups_complete": %t`, *groupsComplete)
	}
	return body + "}"
}

// groupedCountDocWithBound is groupedCountDoc plus group_count_error_upper_bound.
func groupedCountDocWithBound(count uint64, hasMore bool, groupsComplete *bool, groups map[string]uint64, bound uint64) string {
	doc := groupedCountDoc(count, hasMore, groupsComplete, groups)
	return strings.TrimSuffix(doc, "}") + fmt.Sprintf(`, "group_count_error_upper_bound": %d}`, bound)
}

func coverageArgs(foundation string) AuditCommitteeCoverageArgs {
	return AuditCommitteeCoverageArgs{FoundationUID: foundation}
}

// requestsOfType filters the recorded requests to one path and type.
func requestsOfType(api *stubLFXAPI, path, resourceType string) []stubAPIRequest {
	var out []stubAPIRequest
	for _, r := range api.RequestsTo(path) {
		if r.Query.Get("type") == resourceType {
			out = append(out, r)
		}
	}
	return out
}

func TestCoverage_RequiresFoundation(t *testing.T) {
	api := setupOrgSeatsTest(t)
	res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs(" "))
	if !res.IsError || !strings.Contains(allResultText(t, res), "foundation_uid is required") {
		t.Errorf("empty foundation_uid must be rejected, got %q", allResultText(t, res))
	}
	if len(api.Requests()) != 0 {
		t.Error("validation must not reach the API")
	}
}

func TestCoverage_RequestMapping(t *testing.T) {
	api := setupOrgSeatsTest(t)
	// Family: root f plus children a and b.
	api.Respond(resourcesPath, page([]string{projectDoc("a", "a", "A", "f", ""), projectDoc("b", "b", "B", "f", "")}, ""))
	api.Respond(resourcesPath, page([]string{committeeDoc("c1", "Governing Board", "Board", "a")}, ""))
	yes := true
	api.Respond(countPath, groupedCountDoc(3, false, &yes, map[string]uint64{"c1": 3}))
	api.Respond(countPath, groupedCountDoc(5, false, &yes, map[string]uint64{"a": 5}))

	res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}

	committeeReqs := requestsOfType(api, resourcesPath, "committee")
	if len(committeeReqs) != 1 {
		t.Fatalf("expected one committees request, got %d", len(committeeReqs))
	}
	cr := committeeReqs[0]
	assertExchangedAuth(t, cr)
	if got := cr.Query["tags"]; strings.Join(got, ",") != "project_uid:f,project_uid:a,project_uid:b" {
		t.Errorf("committees request must carry one project_uid tag per family member (OR), got %v", got)
	}
	if _, has := cr.Query["tags_all"]; has {
		t.Error("committees request must use tags (any project), not tags_all")
	}
	if cr.Query.Get("page_size") != "100" {
		t.Errorf("committees page size: %v", cr.Query)
	}

	counts := api.RequestsTo(countPath)
	if len(counts) != 2 {
		t.Fatalf("expected two count requests (members, memberships), got %d", len(counts))
	}
	members, memberships := counts[0], counts[1]
	if members.Query.Get("type") != "committee_member" || members.Query.Get("group_by") != "committee_uid" || members.Query.Get("group_by_size") != "1000" {
		t.Errorf("member count request wrong: %v", members.Query)
	}
	if got := members.Query["tags"]; strings.Join(got, ",") != "project_uid:f,project_uid:a,project_uid:b" {
		t.Errorf("member count tags: %v", got)
	}
	if _, has := members.Query["filters_all"]; has {
		t.Error("member count must not carry filters_all")
	}
	if memberships.Query.Get("type") != "project_membership" || memberships.Query.Get("group_by") != "project_uid" || memberships.Query.Get("group_by_size") != "1000" {
		t.Errorf("membership count request wrong: %v", memberships.Query)
	}
	if got := memberships.Query["filters_all"]; len(got) != 1 || got[0] != "status:Active" {
		t.Errorf("membership count must filter status:Active, got %v", got)
	}

	out := resultJSON(t, res)
	if out["foundation_uid"] != "f" || out["projects_in_scope"] != float64(3) || out["complete"] != true || out["visibility"] != "caller" {
		t.Errorf("result envelope wrong: %v", out)
	}
	if !strings.Contains(out["note"].(string), "never report a project as having no seats from this result alone") {
		t.Errorf("note: %v", out["note"])
	}
	projects := out["projects"].([]any)
	if len(projects) != 3 || projects[0].(map[string]any)["project_uid"] != "f" || projects[1].(map[string]any)["project_uid"] != "a" || projects[2].(map[string]any)["project_uid"] != "b" {
		t.Errorf("every family uid, root first then by uid: %v", projects)
	}
	a := projects[1].(map[string]any)
	if a["active_memberships"] != float64(5) || a["has_board_committee"] != true {
		t.Errorf("project a: %v", a)
	}
	committees := a["committees"].([]any)
	if len(committees) != 1 || committees[0].(map[string]any)["visible_members"] != float64(3) || committees[0].(map[string]any)["name"] != "Governing Board" {
		t.Errorf("project a committees: %v", committees)
	}
	if _, has := a["gap"]; has {
		t.Errorf("a board committee with members and active memberships is no gap: %v", a)
	}
	f := projects[0].(map[string]any)
	if f["active_memberships"] != float64(0) || len(f["committees"].([]any)) != 0 || len(f["committees_with_no_visible_members"].([]any)) != 0 {
		t.Errorf("root with nothing must have zero counts and empty lists (not null): %v", f)
	}
}

func TestCoverage_LargeFamilyIsChunkedPerStep(t *testing.T) {
	api := setupOrgSeatsTest(t)
	docs, family := familyOf("f", 89) // 90 uids -> 40, 40, 10
	api.Respond(resourcesPath, page(docs, ""))
	yes := true
	for i := 0; i < 3; i++ {
		api.Respond(resourcesPath, page(nil, ""))
		api.Respond(countPath, groupedCountDoc(0, false, &yes, nil))
		api.Respond(countPath, groupedCountDoc(0, false, &yes, nil))
	}
	res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	committeeReqs := requestsOfType(api, resourcesPath, "committee")
	counts := api.RequestsTo(countPath)
	if len(committeeReqs) != 3 || len(counts) != 6 {
		t.Fatalf("expected 3 committee requests and 6 counts, got %d and %d", len(committeeReqs), len(counts))
	}
	wantChunks := [][]string{family[:40], family[40:80], family[80:]}
	for i, want := range wantChunks {
		wantTags := strings.Join(projectUIDTags(want), ",")
		if got := strings.Join(committeeReqs[i].Query["tags"], ","); got != wantTags {
			t.Errorf("committee chunk %d tags do not partition the family in order", i)
		}
		if got := strings.Join(counts[2*i].Query["tags"], ","); got != wantTags {
			t.Errorf("member count chunk %d tags wrong", i)
		}
		if got := strings.Join(counts[2*i+1].Query["tags"], ","); got != wantTags {
			t.Errorf("membership count chunk %d tags wrong", i)
		}
	}
	out := resultJSON(t, res)
	if out["projects_in_scope"] != float64(90) || len(out["projects"].([]any)) != 90 || out["complete"] != true {
		t.Errorf("envelope: projects_in_scope=%v projects=%d complete=%v", out["projects_in_scope"], len(out["projects"].([]any)), out["complete"])
	}
}

func TestCoverage_GapVocabulary(t *testing.T) {
	api := setupOrgSeatsTest(t)
	// Family: root f + A, B, C, D.
	api.Respond(resourcesPath, page([]string{
		projectDoc("A", "a", "A", "f", ""), projectDoc("B", "b", "B", "f", ""), projectDoc("C", "c", "C", "f", ""), projectDoc("D", "d", "D", "f", ""),
	}, ""))
	// A: board committee with 0 members. B: no committee. C: no committee. D: Technical only.
	api.Respond(resourcesPath, page([]string{
		committeeDoc("cA", "A Board", "board", "A"),
		committeeDoc("cD", "D TOC", "Technical", "D"),
	}, ""))
	yes := true
	api.Respond(countPath, groupedCountDoc(4, false, &yes, map[string]uint64{"cD": 4}))
	api.Respond(countPath, groupedCountDoc(8, false, &yes, map[string]uint64{"A": 5, "B": 2, "D": 1}))

	res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	byUID := map[string]map[string]any{}
	for _, p := range out["projects"].([]any) {
		row := p.(map[string]any)
		byUID[row["project_uid"].(string)] = row
	}
	if byUID["A"]["gap"] != "empty_board" || byUID["A"]["has_board_committee"] != true {
		t.Errorf("A (memberships, board with no visible member) -> empty_board: %v", byUID["A"])
	}
	if got := byUID["A"]["committees_with_no_visible_members"].([]any); len(got) != 1 || got[0] != "cA" {
		t.Errorf("A must list its empty committee: %v", got)
	}
	if byUID["B"]["gap"] != "no_committee" {
		t.Errorf("B (memberships, no committee) -> no_committee: %v", byUID["B"])
	}
	if _, has := byUID["C"]["gap"]; has {
		t.Errorf("C (no memberships, no committee) -> no gap: %v", byUID["C"])
	}
	if byUID["D"]["gap"] != "no_board_committee" || byUID["D"]["has_board_committee"] != false {
		t.Errorf("D (memberships, Technical only) -> no_board_committee: %v", byUID["D"])
	}
	if dc := byUID["D"]["committees"].([]any); len(dc) != 1 || dc[0].(map[string]any)["visible_members"] != float64(4) || dc[0].(map[string]any)["category"] != "Technical" {
		t.Errorf("D committees: %v", dc)
	}
	if _, has := byUID["f"]["gap"]; has {
		t.Errorf("root without memberships -> no gap: %v", byUID["f"])
	}
}

func TestCoverage_MissingGroupsWithCountIsAnError(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	api.Respond(resourcesPath, page([]string{committeeDoc("c1", "Board", "Board", "f")}, ""))
	// An older server ignores group_by: count without groups.
	api.Respond(countPath, `{"count": 7, "has_more": false}`)
	res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if !res.IsError || !strings.Contains(allResultText(t, res), "audit unavailable") || !strings.Contains(allResultText(t, res), "did not return grouped counts") {
		t.Errorf("count without groups must fail closed naming the audit as unavailable, got %q", allResultText(t, res))
	}
	if n := len(api.RequestsTo(countPath)); n != 1 {
		t.Errorf("must stop at the first ungrouped count, made %d count requests", n)
	}

	// Zero count and no groups is a legitimate empty answer.
	api2 := setupOrgSeatsTest(t)
	api2.Respond(resourcesPath, page(nil, ""))
	api2.Respond(resourcesPath, page(nil, ""))
	yes := true
	api2.Respond(countPath, groupedCountDoc(0, false, &yes, nil))
	api2.Respond(countPath, groupedCountDoc(0, false, &yes, nil))
	res2, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if res2.IsError {
		t.Errorf("zero count without groups is not an error: %s", allResultText(t, res2))
	}
}

func TestCoverage_IncompleteWhenAnyCountIsPartial(t *testing.T) {
	yes, no := true, false
	for name, tc := range map[string]struct {
		members, memberships string
	}{
		"groups_complete false on members":      {groupedCountDoc(1, false, &no, map[string]uint64{"c1": 1}), groupedCountDoc(1, false, &yes, map[string]uint64{"f": 1})},
		"has_more on memberships":               {groupedCountDoc(1, false, &yes, map[string]uint64{"c1": 1}), groupedCountDoc(1, true, &yes, map[string]uint64{"f": 1})},
		"groups_complete absent on memberships": {groupedCountDoc(1, false, &yes, map[string]uint64{"c1": 1}), groupedCountDoc(1, false, nil, map[string]uint64{"f": 1})},
	} {
		t.Run(name, func(t *testing.T) {
			api := setupOrgSeatsTest(t)
			api.Respond(resourcesPath, page(nil, ""))
			api.Respond(resourcesPath, page([]string{committeeDoc("c1", "Board", "Board", "f")}, ""))
			api.Respond(countPath, tc.members)
			api.Respond(countPath, tc.memberships)
			res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
			if res.IsError {
				t.Fatalf("unexpected error: %s", allResultText(t, res))
			}
			if out := resultJSON(t, res); out["complete"] != false {
				t.Errorf("complete must be false, got %v", out["complete"])
			}
		})
	}
}

func TestCoverage_AccuracyBoundMakesTheAuditIncomplete(t *testing.T) {
	yes := true
	// Groups complete, has_more false, but the member count carries a
	// nonzero undercount bound: the audit is not complete and reports the bound.
	api := setupOrgSeatsTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	api.Respond(resourcesPath, page([]string{committeeDoc("c1", "Board", "Board", "f")}, ""))
	api.Respond(countPath, groupedCountDocWithBound(3, false, &yes, map[string]uint64{"c1": 3}, 1))
	api.Respond(countPath, groupedCountDocWithBound(2, false, &yes, map[string]uint64{"f": 2}, 0))
	res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	if out["complete"] != false || out["count_accuracy_bound"] != float64(1) {
		t.Errorf("a nonzero accuracy bound must make complete false and be reported: complete=%v bound=%v", out["complete"], out["count_accuracy_bound"])
	}
	// The counts themselves are still returned as the service gave them.
	row := out["projects"].([]any)[0].(map[string]any)
	if row["committees"].([]any)[0].(map[string]any)["visible_members"] != float64(3) || row["active_memberships"] != float64(2) {
		t.Errorf("counts must pass through unchanged: %v", row)
	}

	// A zero bound (exact) leaves the audit complete and reports zero; the
	// largest bound across counts wins.
	api2 := setupOrgSeatsTest(t)
	api2.Respond(resourcesPath, page(nil, ""))
	api2.Respond(resourcesPath, page([]string{committeeDoc("c1", "Board", "Board", "f")}, ""))
	api2.Respond(countPath, groupedCountDocWithBound(3, false, &yes, map[string]uint64{"c1": 3}, 0))
	api2.Respond(countPath, groupedCountDoc(2, false, &yes, map[string]uint64{"f": 2}))
	res2, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if out2 := resultJSON(t, res2); out2["complete"] != true || out2["count_accuracy_bound"] != float64(0) {
		t.Errorf("an exact or absent bound keeps the audit complete: %v %v", out2["complete"], out2["count_accuracy_bound"])
	}
	api3 := setupOrgSeatsTest(t)
	api3.Respond(resourcesPath, page(nil, ""))
	api3.Respond(resourcesPath, page([]string{committeeDoc("c1", "Board", "Board", "f")}, ""))
	api3.Respond(countPath, groupedCountDocWithBound(3, false, &yes, map[string]uint64{"c1": 3}, 2))
	api3.Respond(countPath, groupedCountDocWithBound(2, false, &yes, map[string]uint64{"f": 2}, 5))
	res3, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if out3 := resultJSON(t, res3); out3["complete"] != false || out3["count_accuracy_bound"] != float64(5) {
		t.Errorf("the largest bound across counts is reported: %v %v", out3["complete"], out3["count_accuracy_bound"])
	}
}

func TestCoverage_CategoryFilter(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	api.Respond(resourcesPath, page([]string{
		committeeDoc("cB", "Governing Board", "Board", "f"),
		committeeDoc("cT", "TOC", "Technical", "f"),
	}, ""))
	yes := true
	api.Respond(countPath, groupedCountDoc(7, false, &yes, map[string]uint64{"cB": 3, "cT": 4}))
	api.Respond(countPath, groupedCountDoc(2, false, &yes, map[string]uint64{"f": 2}))
	res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), AuditCommitteeCoverageArgs{FoundationUID: "f", Category: "board"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	if out["category"] != "board" {
		t.Errorf("category echo: %v", out["category"])
	}
	row := out["projects"].([]any)[0].(map[string]any)
	committees := row["committees"].([]any)
	if len(committees) != 1 || committees[0].(map[string]any)["uid"] != "cB" || committees[0].(map[string]any)["visible_members"] != float64(3) {
		t.Errorf("category=board must keep only the board committee, got %v", committees)
	}
	if _, has := row["gap"]; has {
		t.Errorf("a board committee with members is no gap: %v", row)
	}
	// The count request carries no category filter: every committee is
	// counted; category only narrows what is listed.
	if _, has := api.RequestsTo(countPath)[0].Query["filters_all"]; has {
		t.Error("member count must not filter by category")
	}
}

func TestCoverage_CategoryNarrowsTheListNotTheGap(t *testing.T) {
	yes := true
	// Root f has a populated board and a TOC; child g has only a TOC. Both
	// hold active memberships.
	setup := func(t *testing.T) *stubLFXAPI {
		api := setupOrgSeatsTest(t)
		api.Respond(resourcesPath, page([]string{projectDoc("g", "g", "G", "f", "")}, ""))
		api.Respond(resourcesPath, page([]string{
			committeeDoc("cB", "Governing Board", "Board", "f"),
			committeeDoc("cT", "TOC", "Technical", "f"),
			committeeDoc("gT", "G TOC", "Technical", "g"),
		}, ""))
		api.Respond(countPath, groupedCountDoc(9, false, &yes, map[string]uint64{"cB": 3, "cT": 4, "gT": 2}))
		api.Respond(countPath, groupedCountDoc(3, false, &yes, map[string]uint64{"f": 2, "g": 1}))
		return api
	}

	// category=Technical: the board is not listed, yet f still has a board
	// and no gap.
	setup(t)
	res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), AuditCommitteeCoverageArgs{FoundationUID: "f", Category: "Technical"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	rows := resultJSON(t, res)["projects"].([]any)
	f, g := rows[0].(map[string]any), rows[1].(map[string]any)
	if fc := f["committees"].([]any); len(fc) != 1 || fc[0].(map[string]any)["uid"] != "cT" {
		t.Errorf("f must list only its Technical committee: %v", fc)
	}
	if f["has_board_committee"] != true {
		t.Errorf("has_board_committee is evaluated over every committee, got %v", f)
	}
	if _, has := f["gap"]; has {
		t.Errorf("a project with a populated board has no gap whatever the category filter: %v", f)
	}
	if g["gap"] != "no_board_committee" {
		t.Errorf("g (Technical only) -> no_board_committee: %v", g)
	}

	// category=board: g lists nothing, but its gap stays no_board_committee,
	// never no_committee.
	setup(t)
	res2, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), AuditCommitteeCoverageArgs{FoundationUID: "f", Category: "board"})
	if res2.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res2))
	}
	rows2 := resultJSON(t, res2)["projects"].([]any)
	g2 := rows2[1].(map[string]any)
	if len(g2["committees"].([]any)) != 0 || g2["gap"] != "no_board_committee" {
		t.Errorf("category=board on a Technical-only project: empty list, gap no_board_committee (it has a committee): %v", g2)
	}
}

func TestCoverage_EmptyGroupsArrayWithCountIsAnError(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(resourcesPath, page(nil, ""))
	api.Respond(resourcesPath, page([]string{committeeDoc("c1", "Board", "Board", "f")}, ""))
	api.Respond(countPath, `{"count": 7, "has_more": false, "groups": []}`)
	res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if !res.IsError || !strings.Contains(allResultText(t, res), "audit unavailable") {
		t.Errorf("an empty groups array with records matched must fail closed, got %q", allResultText(t, res))
	}
}

func TestCoverage_RepeatedFamilyUIDIsReadOnce(t *testing.T) {
	api := setupOrgSeatsTest(t)
	// The index echoes child a twice.
	api.Respond(resourcesPath, page([]string{projectDoc("a", "a", "A", "f", ""), projectDoc("a", "a", "A", "f", "")}, ""))
	api.Respond(resourcesPath, page(nil, ""))
	yes := true
	api.Respond(countPath, groupedCountDoc(0, false, &yes, nil))
	api.Respond(countPath, groupedCountDoc(1, false, &yes, map[string]uint64{"a": 1}))
	res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	if out["projects_in_scope"] != float64(2) || len(out["projects"].([]any)) != 2 {
		t.Errorf("a repeated uid counts once: %v / %d rows", out["projects_in_scope"], len(out["projects"].([]any)))
	}
	if got := requestsOfType(api, resourcesPath, "committee")[0].Query["tags"]; strings.Join(got, ",") != "project_uid:f,project_uid:a" {
		t.Errorf("the repeated uid must be sent once: %v", got)
	}
}

func TestCoverage_ErrorsFailClosed(t *testing.T) {
	// Family resolution failure.
	api := setupOrgSeatsTest(t)
	api.RespondStatus(resourcesPath, http.StatusInternalServerError, `{"message":"boom"}`)
	res, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if !res.IsError || !strings.Contains(allResultText(t, res), "Failed to resolve the foundation's projects") {
		t.Errorf("family failure: %q", allResultText(t, res))
	}
	if len(api.RequestsTo(countPath)) != 0 {
		t.Error("no counts after a failed family resolution")
	}

	// Committee list failure.
	api2 := setupOrgSeatsTest(t)
	api2.Respond(resourcesPath, page(nil, ""))
	api2.RespondStatus(resourcesPath, http.StatusInternalServerError, `{"message":"search down"}`)
	res2, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if !res2.IsError || !strings.Contains(allResultText(t, res2), "Failed to list committees") || !strings.Contains(allResultText(t, res2), "search down") {
		t.Errorf("committee list failure: %q", allResultText(t, res2))
	}

	// Count failure.
	api3 := setupOrgSeatsTest(t)
	api3.Respond(resourcesPath, page(nil, ""))
	api3.Respond(resourcesPath, page(nil, ""))
	api3.RespondStatus(countPath, http.StatusInternalServerError, `{"message":"count down"}`)
	res3, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if !res3.IsError || !strings.Contains(allResultText(t, res3), "Failed to count committee members") || !strings.Contains(allResultText(t, res3), "count down") {
		t.Errorf("count failure: %q", allResultText(t, res3))
	}

	// Committee drain page cap.
	api4 := setupOrgSeatsTest(t)
	api4.Respond(resourcesPath, page(nil, ""))
	for i := 0; i < participantMaxDrainPages+5; i++ {
		api4.Respond(resourcesPath, page([]string{committeeDoc(fmt.Sprintf("c%d", i), "C", "Board", "f")}, fmt.Sprintf("t%d", i)))
	}
	res4, _, _ := handleAuditCommitteeCoverage(context.Background(), stubCallToolRequest(), coverageArgs("f"))
	if !res4.IsError || !strings.Contains(allResultText(t, res4), "page cap") {
		t.Errorf("committee drain cap must be an error, got %q", allResultText(t, res4))
	}
}

func TestCoverage_DescriptionBudgetAndContent(t *testing.T) {
	tool := listRegisteredTool(t, "audit_committee_coverage", RegisterAuditCommitteeCoverage)
	// 993 is the description's size when the tool shipped; it must not grow.
	if n := len(tool.Description); n > 993 {
		t.Errorf("description is %d bytes, keep it at or under 993", n)
	}
	for _, want := range []string{"foundation_uid", "direct child projects", "visible to the caller", "complete", "no_committee", "no_board_committee", "empty_board", "get_org_committee_seats", "search_committee_members", "category", "For a person's or organization's seats use", "count_accuracy_bound"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("description missing %q", want)
		}
	}
	for _, banned := range []string{"Insights", "because"} {
		if strings.Contains(tool.Description, banned) {
			t.Errorf("description must not contain %q", banned)
		}
	}
	// No figures: the only digit allowed is the one in "LFX v2".
	digits := 0
	for _, r := range strings.ReplaceAll(tool.Description, "LFX v2", "") {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if digits != 0 {
		t.Errorf("description must carry no figures, found %d digits", digits)
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Error("tool must be read-only")
	}
	if strings.Contains(coverageNote, "Insights") {
		t.Error("note must not mention Insights")
	}
}
