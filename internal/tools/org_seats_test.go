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

// testSFID is a well-formed 18-character Salesforce Account SFID.
const testSFID = "001B000000IqhSLIAZ"

// seatsPath is the committee-service seats route for testSFID.
var seatsPath = "/committees/b2b-org/" + testSFID + "/seats"

// seatDoc is one OrgCommitteeSeat as committee-service 0.4.22 serves it
// (cmd/committee-api/design/type.go OrgCommitteeSeatType, decoded by the
// vendored client of the same version). uid/committee_uid/project_uid are
// uuid-formatted and avatar is omitted when empty, as the service does;
// values are test data.
func seatDoc(uid, committeeUID, committeeName, category, projectUID, projectSlug, first, last, email, role string, editable bool) string {
	reason := ""
	if !editable {
		reason = "This seat is foundation-controlled."
	}
	appointedBy := "Membership Entitlement"
	if !editable {
		appointedBy = "Community"
	}
	return fmt.Sprintf(`{
	  "uid": %q,
	  "committee_uid": %q,
	  "committee_name": %q,
	  "committee_category": %q,
	  "project_uid": %q,
	  "project_slug": %q,
	  "first_name": %q,
	  "last_name": %q,
	  "email": %q,
	  "job_title": "Director",
	  "role_name": %q,
	  "voting_status": "Voting Rep",
	  "appointed_by": %q,
	  "organization_id": %q,
	  "is_org_editable": %t,
	  "reason": %q,
	  "username": %q
	}`, uuidFor(uid), uuidFor(committeeUID), committeeName, category, uuidFor(projectUID), projectSlug, first, last, email, role, appointedBy, testSFID, editable, reason, strings.ToLower(first))
}

// uuidFor derives a deterministic, well-formed UUID from a short label so
// fixtures read naturally while satisfying the client's uuid format checks.
func uuidFor(label string) string {
	h := 0
	for _, c := range label {
		h = h*131 + int(c)
	}
	return fmt.Sprintf("%08x-%04x-4%03x-8%03x-%012x", h&0xffffffff, (h>>8)&0xffff, (h>>4)&0xfff, h&0xfff, h&0xffffffffffff)
}

// tenSeatsFixture: ten seats, two categories (Board / Technical), two
// projects (cncf / kubernetes), one duplicate e-mail (ann@x.org holds two
// seats), six editable / four foundation-controlled.
func tenSeatsFixture() []string {
	return []string{
		seatDoc("s01", "c-gb", "Governing Board", "Board", "p-cncf", "cncf", "Ann", "Alpha", "ann@x.org", "Chair", true),
		seatDoc("s02", "c-gb", "Governing Board", "Board", "p-cncf", "cncf", "Bob", "Beta", "bob@x.org", "None", true),
		seatDoc("s03", "c-gb", "Governing Board", "board ", "p-cncf", "cncf", "Cid", "Gamma", "cid@x.org", "None", true),
		seatDoc("s04", "c-toc", "TOC", "Technical", "p-cncf", "cncf", "Ann", "Alpha", "ANN@x.org", "None", false),
		seatDoc("s05", "c-toc", "TOC", "Technical", "p-cncf", "cncf", "Dee", "Delta", "dee@x.org", "Vice Chair", false),
		seatDoc("s06", "c-sc", "Steering", "Technical", "p-k8s", "kubernetes", "Eve", "Epsilon", "eve@x.org", "None", false),
		seatDoc("s07", "c-sc", "Steering", "Technical", "p-k8s", "kubernetes", "Fay", "Zeta", "fay@x.org", "None", false),
		seatDoc("s08", "c-sc", "Steering", "Technical", "p-k8s", "kubernetes", "Gus", "Eta", "gus@x.org", "None", true),
		seatDoc("s09", "c-k8b", "K8s Board", "Board", "p-k8s", "kubernetes", "Hal", "Theta", "hal@x.org", "None", true),
		seatDoc("s10", "c-k8b", "K8s Board", "Board", "p-k8s", "kubernetes", "Ivy", "Iota", "ivy@x.org", "Chair", true),
	}
}

func seatsPage(seats []string, token string) string {
	body := `{"seats": [` + strings.Join(seats, ",") + `]`
	if token != "" {
		body += fmt.Sprintf(`, "page_token": %q`, token)
	}
	return body + "}"
}

func setupOrgSeatsTest(t *testing.T) *stubLFXAPI {
	t.Helper()
	api := newStubLFXAPI(t)
	prev := orgSeatsConfig
	SetOrgSeatsConfig(&OrgSeatsConfig{Clients: api.Clients})
	t.Cleanup(func() { orgSeatsConfig = prev })
	return api
}

func TestOrgSeats_RejectsBadSFID(t *testing.T) {
	api := setupOrgSeatsTest(t)
	for _, bad := range []string{"", "001B000000IqhSLIA", "001B000000IqhSLIAZ1", "001B000000IqhSLIA-"} {
		res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{OrgUID: bad})
		if !res.IsError || !strings.Contains(allResultText(t, res), "search_b2b_orgs") {
			t.Errorf("org_uid %q must be rejected with a pointer to search_b2b_orgs, got %q", bad, allResultText(t, res))
		}
	}
	if len(api.Requests()) != 0 {
		t.Error("validation must not reach the API")
	}
}

func TestOrgSeats_SummaryArithmetic(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(seatsPath, seatsPage(tenSeatsFixture(), ""))

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{OrgUID: testSFID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	r := api.LastRequest()
	if r.Path != seatsPath || r.Query.Get("v") != "1" || r.Query.Get("page_size") != "500" {
		t.Errorf("seats request wrong: %s %v", r.Path, r.Query)
	}
	if _, has := r.Query["project_uids"]; has {
		t.Error("org-wide call must not send project_uids")
	}
	assertExchangedAuth(t, r)

	out := resultJSON(t, res)
	checks := map[string]float64{
		"seats_total": 10, "people": 9, "board_seats": 5, "committee_seats": 5, "editable": 6, "foundation_controlled": 4,
	}
	for k, want := range checks {
		if out[k] != want {
			t.Errorf("%s: want %v got %v", k, want, out[k])
		}
	}
	byCategory := out["by_category"].(map[string]any)
	if byCategory["Board"] != float64(4) || byCategory["board "] != float64(1) || byCategory["Technical"] != float64(5) {
		t.Errorf("by_category keeps stored spelling: %v", byCategory)
	}
	byProject := out["by_project"].(map[string]any)
	if byProject["cncf"] != float64(5) || byProject["kubernetes"] != float64(5) {
		t.Errorf("by_project: %v", byProject)
	}
	byRole := out["by_role"].(map[string]any)
	if byRole["Chair"] != float64(2) || byRole["Vice Chair"] != float64(1) || byRole["None"] != float64(7) {
		t.Errorf("by_role: %v", byRole)
	}
	if out["visibility"] != "organization" || !strings.Contains(out["note"].(string), "Board & Committee tab") {
		t.Errorf("visibility/note: %v %v", out["visibility"], out["note"])
	}
	if _, has := out["seats"]; has {
		t.Error("seats rows must be omitted without include_seats")
	}
}

func TestOrgSeats_IncludeSeatsAndCategoryFilter(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(seatsPath, seatsPage(tenSeatsFixture(), ""))

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{OrgUID: testSFID, Category: "bOaRd", IncludeSeats: true})
	out := resultJSON(t, res)
	if out["seats_total"] != float64(5) || out["board_seats"] != float64(5) || out["committee_seats"] != float64(0) {
		t.Errorf("category=Board must keep the five board seats (incl. 'board '), got %v", out)
	}
	if out["people"] != float64(5) {
		t.Errorf("people within the board scope: want 5, got %v", out["people"])
	}
	seats := out["seats"].([]any)
	if len(seats) != 5 {
		t.Fatalf("include_seats must return the five rows, got %d", len(seats))
	}
	row := seats[0].(map[string]any)
	for _, k := range []string{"first_name", "last_name", "email", "role_name", "voting_status", "appointed_by", "committee_name", "project_slug", "is_org_editable"} {
		if _, has := row[k]; !has {
			t.Errorf("seat row missing %s: %v", k, row)
		}
	}
	if row["project_slug"] == "" {
		t.Error("project_slug must survive decoding (the v0.4.0 client dropped it; v0.4.22 carries it)")
	}
}

func TestOrgSeats_FoundationFamilyResolution(t *testing.T) {
	api := setupOrgSeatsTest(t)
	// Two pages of children; ROOT skipped.
	api.Respond(resourcesPath, page([]string{projectDoc("p-k8s", "kubernetes", "Kubernetes", "p-cncf", ""), projectDoc("p-root", "ROOT", "ROOT", "p-cncf", "")}, "more"))
	api.Respond(resourcesPath, page([]string{projectDoc("p-env", "envoy", "Envoy", "p-cncf", "")}, ""))
	api.Respond(seatsPath, seatsPage(tenSeatsFixture()[:3], ""))

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{OrgUID: testSFID, FoundationUID: "p-cncf"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	projReqs := api.RequestsTo(resourcesPath)
	if len(projReqs) != 2 || projReqs[0].Query.Get("type") != "project" || projReqs[0].Query.Get("parent") != "project:p-cncf" || projReqs[1].Query.Get("page_token") != "more" {
		t.Errorf("family resolution requests wrong: %+v", projReqs)
	}
	seatReq := api.RequestsTo(seatsPath)[0]
	want := []string{"p-cncf", "p-k8s", "p-env"}
	if got := seatReq.Query["project_uids"]; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("project_uids: want %v got %v (ROOT must be skipped, root first)", want, got)
	}
	out := resultJSON(t, res)
	if out["project_uids_in_scope"] != float64(3) || out["foundation_uid"] != "p-cncf" {
		t.Errorf("scope echo wrong: %v", out)
	}
}

func TestOrgSeats_FamilyResolutionFailureFailsClosed(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.RespondStatus(resourcesPath, http.StatusInternalServerError, `{"message":"boom"}`)
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{OrgUID: testSFID, FoundationUID: "p-cncf"})
	if !res.IsError {
		t.Fatal("a failed family lookup must not fall back to the root alone")
	}
	if len(api.RequestsTo(seatsPath)) != 0 {
		t.Error("seats must not be fetched after a failed family lookup")
	}
}

func TestOrgSeats_DrainsPagesAndErrorsAtCap(t *testing.T) {
	// Drain: three pages.
	api := setupOrgSeatsTest(t)
	fx := tenSeatsFixture()
	api.Respond(seatsPath, seatsPage(fx[:4], "t1"))
	api.Respond(seatsPath, seatsPage(fx[4:8], "t2"))
	api.Respond(seatsPath, seatsPage(fx[8:], ""))
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{OrgUID: testSFID})
	if out := resultJSON(t, res); out["seats_total"] != float64(10) {
		t.Errorf("drain must collect every page, got %v", out["seats_total"])
	}
	reqs := api.RequestsTo(seatsPath)
	if len(reqs) != 3 || reqs[1].Query.Get("page_token") != "t1" || reqs[2].Query.Get("page_token") != "t2" {
		t.Errorf("page tokens not followed: %+v", reqs)
	}

	// Cap: every page returns a token; must error, never a partial roster.
	api2 := setupOrgSeatsTest(t)
	for i := 0; i < orgSeatsMaxPages+5; i++ {
		api2.Respond(seatsPath, seatsPage(fx[:1], fmt.Sprintf("t%d", i)))
	}
	res2, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{OrgUID: testSFID})
	if !res2.IsError || !strings.Contains(allResultText(t, res2), "foundation_uid") {
		t.Errorf("cap must produce an error pointing at foundation_uid, got %q", allResultText(t, res2))
	}
	if n := len(api2.RequestsTo(seatsPath)); n != orgSeatsMaxPages {
		t.Errorf("must stop at exactly %d pages, made %d", orgSeatsMaxPages, n)
	}
}

func TestOrgSeats_ForbiddenMapsToOrgGrantMessage(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.RespondStatus(seatsPath, http.StatusForbidden, `{"message":"forbidden"}`)
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{OrgUID: testSFID})
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	text := allResultText(t, res)
	if !strings.Contains(text, "organisation grant") || !strings.Contains(text, "auditor or writer") {
		t.Errorf("403 must explain the org grant, got %q", text)
	}
	if strings.Contains(text, accessDeniedMessage) {
		t.Error("403 on seats must use the org-grant wording, not the generic access-denied message")
	}
}

func TestOrgSeats_OtherErrorsAreFriendly(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.RespondStatus(seatsPath, http.StatusNotFound, `{"message":"org not found"}`)
	// Goa's default branch wraps unknown statuses as "invalid response code N".
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{OrgUID: testSFID})
	if !res.IsError || !strings.Contains(allResultText(t, res), "404") {
		t.Errorf("404 must pass through friendlyAPIError, got %q", allResultText(t, res))
	}
}

func TestOrgSeats_DescriptionBudgetAndContent(t *testing.T) {
	tool := listRegisteredTool(t, "get_org_committee_seats", RegisterGetOrgCommitteeSeats)
	if n := len(tool.Description); n > 1000 {
		t.Errorf("description is %d bytes, keep it under 1000", n)
	}
	for _, want := range []string{"search_b2b_orgs", "foundation_uid", "category", "organization grant", "include_seats", "Board & Committee"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("description missing %q", want)
		}
	}
	for _, banned := range []string{"Insights", "Jim", "because", "65 KB"} {
		if strings.Contains(tool.Description, banned) {
			t.Errorf("description must not contain %q", banned)
		}
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Error("tool must be read-only")
	}
}

func TestOrgSeats_FamilyResolutionIsCapped(t *testing.T) {
	api := setupOrgSeatsTest(t)
	for i := 0; i < participantMaxDrainPages+5; i++ {
		api.Respond(resourcesPath, page(nil, fmt.Sprintf("t%d", i)))
	}
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{OrgUID: testSFID, FoundationUID: "p"})
	if !res.IsError || !strings.Contains(allResultText(t, res), "page cap") {
		t.Errorf("expected a page-cap error, got %q", allResultText(t, res))
	}
	if len(api.RequestsTo(seatsPath)) != 0 {
		t.Error("seats must not be fetched after a capped family resolution")
	}
}
