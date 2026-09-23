// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"net/http"
	"sort"
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
	// Board seats carry Voting Rep, every other seat None, so the two ways a
	// seat can represent the organisation stay distinguishable in fixtures.
	votingStatus := "None"
	if isBoardCategory(category) {
		votingStatus = "Voting Rep"
	}
	return seatDocVoting(uid, committeeUID, committeeName, category, projectUID, projectSlug, first, last, email, role, votingStatus, editable)
}

// seatDocVoting is seatDoc with an explicit stored voting_status.
func seatDocVoting(uid, committeeUID, committeeName, category, projectUID, projectSlug, first, last, email, role, votingStatus string, editable bool) string {
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
	  "voting_status": %q,
	  "appointed_by": %q,
	  "organization_id": %q,
	  "is_org_editable": %t,
	  "reason": %q,
	  "username": %q
	}`, uuidFor(uid), uuidFor(committeeUID), committeeName, category, uuidFor(projectUID), projectSlug, first, last, email, role, votingStatus, appointedBy, testSFID, editable, reason, strings.ToLower(first))
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
		res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: bad})
		if !res.IsError || !strings.Contains(allResultText(t, res), "search_b2b_orgs") {
			t.Errorf("b2b_org_uid %q must be rejected with a pointer to search_b2b_orgs, got %q", bad, allResultText(t, res))
		}
	}
	if len(api.Requests()) != 0 {
		t.Error("validation must not reach the API")
	}
}

func TestOrgSeats_SummaryArithmetic(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(seatsPath, seatsPage(tenSeatsFixture(), ""))

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID})
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

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, Category: "bOaRd", IncludeSeats: true})
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

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, FoundationUID: "p-cncf"})
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
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, FoundationUID: "p-cncf"})
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
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID})
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
	res2, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID})
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
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID})
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	text := allResultText(t, res)
	if !strings.Contains(text, "organisation grant") || !strings.Contains(text, "auditor or writer") {
		t.Errorf("403 must explain the org grant, got %q", text)
	}
	// Heimdall answers 403 for an unknown SFID too, so the text must also point
	// at the identifier check.
	if !strings.Contains(text, "not a known organisation") || !strings.Contains(text, "search_b2b_orgs") {
		t.Errorf("403 must carry the unknown-SFID hint, got %q", text)
	}
	if strings.Contains(text, accessDeniedMessage) {
		t.Error("403 on seats must use the org-grant wording, not the generic access-denied message")
	}
}

func TestOrgSeats_OtherErrorsAreFriendly(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.RespondStatus(seatsPath, http.StatusNotFound, `{"message":"org not found"}`)
	// Goa's default branch wraps unknown statuses as "invalid response code N".
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID})
	if !res.IsError || !strings.Contains(allResultText(t, res), "404") {
		t.Errorf("404 must pass through friendlyAPIError, got %q", allResultText(t, res))
	}
}

func TestOrgSeats_DescriptionBudgetAndContent(t *testing.T) {
	tool := listRegisteredTool(t, "get_org_committee_seats", RegisterGetOrgCommitteeSeats)
	// The client cap is schemaDescriptionBudget (2048). The description must
	// not grow: 1508 is its size before the representation rule was widened,
	// and every later edit trims at least as much as it adds.
	if n := len(tool.Description); n > 1508 {
		t.Errorf("description is %d bytes, keep it at or under 1508", n)
	}
	for _, want := range []string{"search_b2b_orgs", "foundation_uid", "category", "organization grant", "include_seats", "Board & Committee", "direct child projects as visible to the caller", "the way LFX Self Serve scopes it", "an organization grant does not make project discovery exhaustive"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("description missing %q", want)
		}
	}
	if strings.Contains(tool.Description, "every descendant") {
		t.Error("description must not claim descendants beyond direct children")
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
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, FoundationUID: "p"})
	if !res.IsError || !strings.Contains(allResultText(t, res), "page cap") {
		t.Errorf("expected a page-cap error, got %q", allResultText(t, res))
	}
	if len(api.RequestsTo(seatsPath)) != 0 {
		t.Error("seats must not be fetched after a capped family resolution")
	}
}

func TestOrgSeats_ByProjectFallbacksAndRowOrder(t *testing.T) {
	api := setupOrgSeatsTest(t)
	// Same committee + last name twice to exercise the first-name/e-mail tie-breakers;
	// one seat with no project_slug (keyed by uid) and one with neither (keyed "(none)").
	noSlug := strings.Replace(seatDoc("s20", "c-x", "Zed Committee", "Technical", "p-only-uid", "", "Bea", "Same", "bea@x.org", "None", true), `"project_slug": "",`, "", 1)
	noProject := strings.Replace(strings.Replace(seatDoc("s21", "c-x", "Zed Committee", "Technical", "p-none", "", "Abe", "Same", "abe@x.org", "None", true), `"project_slug": "",`, "", 1), fmt.Sprintf(`"project_uid": %q,`, uuidFor("p-none")), "", 1)
	api.Respond(seatsPath, seatsPage([]string{noSlug, noProject}, ""))
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, IncludeSeats: true})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	byProject := out["by_project"].(map[string]any)
	if byProject[uuidFor("p-only-uid")] != float64(1) || byProject["(none)"] != float64(1) {
		t.Errorf("by_project must fall back to project_uid then \"(none)\": %v", byProject)
	}
	rows := out["seats"].([]any)
	first, second := rows[0].(map[string]any), rows[1].(map[string]any)
	if first["first_name"] != "Abe" || second["first_name"] != "Bea" {
		t.Errorf("rows with equal committee and last name must order by first name: %v, %v", first["first_name"], second["first_name"])
	}
}

func TestOrgSeats_UpstreamErrorsAreNeverBlank(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusBadRequest, `{"message":"page_size out of range"}`, "page_size"},
		{http.StatusInternalServerError, `{"message":"kv unavailable"}`, "kv unavailable"},
		{http.StatusServiceUnavailable, `{"message":"try again"}`, "try again"},
	} {
		api := setupOrgSeatsTest(t)
		api.RespondStatus(seatsPath, tc.status, tc.body)
		res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID})
		text := allResultText(t, res)
		if !res.IsError || strings.TrimSpace(strings.TrimPrefix(text, "Failed to get organization committee seats:")) == "" {
			t.Errorf("%d: blank error text: %q", tc.status, text)
		}
		if !strings.Contains(text, tc.want) {
			t.Errorf("%d: upstream message %q missing from %q", tc.status, tc.want, text)
		}
	}
}

// keyContactDoc is one key_contact resource as the member service indexes it
// (fields read on a live record; values are test data). project_uid is
// uuid-formatted through uuidFor so it matches the seat fixtures' project.
func keyContactDoc(uid, membershipUID, projectUID, projectName, role, status, first, last, email string) string {
	return fmt.Sprintf(`{
	  "type": "key_contact",
	  "id": %q,
	  "data": {
	    "uid": %q,
	    "membership_uid": %q,
	    "project_uid": %q,
	    "project_name": %q,
	    "project_sfid": "a09410000182dD2AAI",
	    "b2b_org_uid": %q,
	    "role": %q,
	    "status": %q,
	    "board_member": true,
	    "primary_contact": false,
	    "first_name": %q,
	    "last_name": %q,
	    "email": %q,
	    "title": "VP",
	    "company_name": "X Corp",
	    "company_domain": "x.org",
	    "tier_uid": "t1",
	    "created_at": "2026-01-01T00:00:00Z",
	    "updated_at": "2026-01-01T00:00:00Z"
	  }
	}`, uid, uid, membershipUID, uuidFor(projectUID), projectName, testSFID, role, status, first, last, email)
}

func TestOrgSeats_DefaultCallMakesNoContactsRequestAndReturnsVotingStatus(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(seatsPath, seatsPage(tenSeatsFixture(), ""))

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	if n := len(api.RequestsTo(resourcesPath)); n != 0 {
		t.Errorf("default call must not query key contacts, made %d resources requests", n)
	}
	out := resultJSON(t, res)
	byVoting, ok := out["by_voting_status"].(map[string]any)
	if !ok || byVoting["Voting Rep"] != float64(5) || byVoting["None"] != float64(5) {
		t.Errorf("by_voting_status must always be returned from the seats, got %v", out["by_voting_status"])
	}
	for _, k := range []string{"membership_contacts", "representation", "contacts_note"} {
		if _, has := out[k]; has {
			t.Errorf("%s must be omitted without include_membership_contacts", k)
		}
	}
}

func TestOrgSeats_IncludeMembershipContactsRequestAndRepresentation(t *testing.T) {
	api := setupOrgSeatsTest(t)
	// Family: root p-cncf plus child p-k8s, as uuids matching the seat and
	// contact fixtures' project_uid values.
	api.Respond(resourcesPath, page([]string{projectDoc(uuidFor("p-k8s"), "kubernetes", "Kubernetes", uuidFor("p-cncf"), "")}, ""))
	api.Respond(seatsPath, seatsPage(tenSeatsFixture(), ""))
	// Contacts: two pages; one voting contact on p-cncf (Active), one Inactive
	// voting contact on p-k8s, a billing contact on p-cncf, and a voting contact
	// on a project outside the family.
	api.Respond(resourcesPath, page([]string{
		keyContactDoc("kc1", "m-cncf", "p-cncf", "CNCF", "Representative/Voting Contact", "Active", "Vic", "Vote", "vic@x.org"),
		keyContactDoc("kc2", "m-cncf", "p-cncf", "CNCF", "Billing Contact", "Active", "Bill", "Pay", "bill@x.org"),
	}, "kc-more"))
	api.Respond(resourcesPath, page([]string{
		keyContactDoc("kc3", "m-k8s", "p-k8s", "Kubernetes", "Representative/Voting Contact", "Inactive", "Ina", "Old", "ina@x.org"),
		keyContactDoc("kc4", "m-other", "p-other", "Other", "Representative/Voting Contact", "Active", "Out", "Side", "out@x.org"),
	}, ""))

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, FoundationUID: uuidFor("p-cncf"), IncludeMembershipContacts: true})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}

	// Request shape: type=key_contact, tags_all=b2b_org_uid:<sfid>, no tags, no
	// project filter, sorted, paged with the drain page size; token followed.
	var contactReqs []stubAPIRequest
	for _, r := range api.RequestsTo(resourcesPath) {
		if r.Query.Get("type") == "key_contact" {
			contactReqs = append(contactReqs, r)
		}
	}
	if len(contactReqs) != 2 {
		t.Fatalf("expected two key_contact requests (two pages), got %d", len(contactReqs))
	}
	r := contactReqs[0]
	assertExchangedAuth(t, r)
	if got := r.Query["tags_all"]; len(got) != 1 || got[0] != "b2b_org_uid:"+testSFID {
		t.Errorf("tags_all must be exactly b2b_org_uid:<sfid>, got %v", got)
	}
	for _, forbidden := range []string{"tags", "parent", "filters_all", "filters_or"} {
		if _, has := r.Query[forbidden]; has {
			t.Errorf("contacts request must not carry %s (the family is filtered client-side), got %v", forbidden, r.Query[forbidden])
		}
	}
	if r.Query.Get("page_size") != "100" || r.Query.Get("sort") != "name_asc" {
		t.Errorf("contacts request paging wrong: %v", r.Query)
	}
	if contactReqs[1].Query.Get("page_token") != "kc-more" {
		t.Errorf("contacts page token not followed: %v", contactReqs[1].Query)
	}

	out := resultJSON(t, res)
	contacts := out["membership_contacts"].([]any)
	// kc4 is outside the family and must be dropped.
	if len(contacts) != 3 {
		t.Fatalf("expected the three in-family contacts, got %d: %v", len(contacts), contacts)
	}
	for _, c := range contacts {
		row := c.(map[string]any)
		if row["kind"] != "membership_contact" {
			t.Errorf("contact kind must be membership_contact, got %v", row["kind"])
		}
		if row["email"] == "out@x.org" {
			t.Error("a contact on a project outside the family must be dropped")
		}
		if _, has := row["project_sfid"]; has {
			t.Error("project_sfid must not be surfaced")
		}
	}

	reps := out["representation"].([]any)
	if len(reps) != 2 {
		t.Fatalf("expected one representation row per project with a voting contact or board seat, got %d: %v", len(reps), reps)
	}
	// Ordered by project uid: uuidFor("p-cncf") vs uuidFor("p-k8s").
	wantOrder := []string{uuidFor("p-cncf"), uuidFor("p-k8s")}
	sort.Strings(wantOrder)
	for i, want := range wantOrder {
		if reps[i].(map[string]any)["project_uid"] != want {
			t.Errorf("representation[%d].project_uid: want %s got %v", i, want, reps[i].(map[string]any)["project_uid"])
		}
	}
	for _, rp := range reps {
		row := rp.(map[string]any)
		voting := row["voting_contacts"].([]any)
		seats := row["seats"].([]any)
		switch row["project_uid"] {
		case uuidFor("p-cncf"):
			if row["project_slug"] != "cncf" {
				t.Errorf("project_slug must come from the seat rows, got %v", row["project_slug"])
			}
			if len(voting) != 1 || voting[0].(map[string]any)["email"] != "vic@x.org" {
				t.Errorf("cncf voting contacts: the billing contact must not be there, got %v", voting)
			}
			// Three board seats on cncf (Board, Board, "board ").
			if len(seats) != 3 {
				t.Errorf("cncf board seats: want 3 got %d", len(seats))
			}
			for _, b := range seats {
				seat := b.(map[string]any)
				if seat["kind"] != "board_seat" || seat["voting_status"] != "Voting Rep" {
					t.Errorf("board seat rows carry kind and voting_status as stored: %v", seat)
				}
			}
		case uuidFor("p-k8s"):
			if len(voting) != 1 || voting[0].(map[string]any)["status"] != "Inactive" {
				t.Errorf("an Inactive voting contact is kept with status Inactive, got %v", voting)
			}
			if len(seats) != 2 {
				t.Errorf("kubernetes board seats: want 2 got %d", len(seats))
			}
		default:
			t.Errorf("unexpected representation row %v", row)
		}
	}
	note, _ := out["contacts_note"].(string)
	if !strings.Contains(note, "contacts of record") || !strings.Contains(note, "updated_at where one is returned") || strings.Contains(note, "no dates") || !strings.Contains(note, "cite which one you mean") {
		t.Errorf("contacts_note wrong: %q", note)
	}
	if strings.Contains(note, "No key contact is indexed") {
		t.Error("fallback sentence must not appear when contacts came back")
	}
	// Seat rows themselves are still omitted without include_seats.
	if _, has := out["seats"]; has {
		t.Error("seats rows must stay omitted without include_seats")
	}
}

func TestOrgSeats_ContactsCarryUpdatedAt(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(seatsPath, seatsPage(tenSeatsFixture()[:1], ""))
	api.Respond(resourcesPath, page([]string{
		keyContactDoc("kc1", "m-cncf", "p-cncf", "CNCF", "Representative/Voting Contact", "Active", "Vic", "Vote", "vic@x.org"),
	}, ""))
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, IncludeMembershipContacts: true})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	const want = "2026-01-01T00:00:00Z" // the fixture's updated_at, as stored
	contact := out["membership_contacts"].([]any)[0].(map[string]any)
	if contact["updated_at"] != want {
		t.Errorf("membership_contacts must carry the contact's updated_at as stored, got %v", contact["updated_at"])
	}
	voting := out["representation"].([]any)[0].(map[string]any)["voting_contacts"].([]any)[0].(map[string]any)
	if voting["updated_at"] != want {
		t.Errorf("representation voting_contacts must carry updated_at too, got %v", voting["updated_at"])
	}
	// Seat rows carry no date: the committee-service seat has none.
	for _, k := range []string{"created_at", "updated_at"} {
		if _, has := out["representation"].([]any)[0].(map[string]any)["seats"].([]any)[0].(map[string]any)[k]; has {
			t.Errorf("seat rows must not invent a %s", k)
		}
	}
}

func TestOrgSeats_SeatRowsCarryKind(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(seatsPath, seatsPage(tenSeatsFixture(), ""))
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, IncludeSeats: true})
	out := resultJSON(t, res)
	kinds := map[string]int{}
	for _, s := range out["seats"].([]any) {
		kinds[s.(map[string]any)["kind"].(string)]++
	}
	if kinds["board_seat"] != 5 || kinds["committee_seat"] != 5 {
		t.Errorf("seat kinds: want 5 board_seat / 5 committee_seat, got %v", kinds)
	}
}

func TestOrgSeats_ContactsUnscopedKeepsAllAndZeroContactsAddsFallback(t *testing.T) {
	// Unscoped: every contact is kept, including one on an unrelated project.
	api := setupOrgSeatsTest(t)
	api.Respond(seatsPath, seatsPage(tenSeatsFixture()[:1], ""))
	api.Respond(resourcesPath, page([]string{
		keyContactDoc("kc1", "m-cncf", "p-cncf", "CNCF", "Representative/Voting Contact", "Active", "Vic", "Vote", "vic@x.org"),
		keyContactDoc("kc4", "m-other", "p-other", "Other", "Technical Contact", "Active", "Out", "Side", "out@x.org"),
	}, ""))
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, IncludeMembershipContacts: true})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	if n := len(out["membership_contacts"].([]any)); n != 2 {
		t.Errorf("unscoped call keeps every contact, got %d", n)
	}
	// Representation: p-cncf (voting contact + board seat) only; the technical
	// contact on p-other earns no row.
	if reps := out["representation"].([]any); len(reps) != 1 || reps[0].(map[string]any)["project_uid"] != uuidFor("p-cncf") {
		t.Errorf("representation must have one row for p-cncf, got %v", reps)
	}

	// Zero contacts: fallback sentence; membership_contacts is present and
	// empty (a stated answer, not an omission); representation carries the
	// board seats.
	api2 := setupOrgSeatsTest(t)
	api2.Respond(seatsPath, seatsPage(tenSeatsFixture()[:1], ""))
	api2.Respond(resourcesPath, page(nil, ""))
	res2, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, IncludeMembershipContacts: true})
	if res2.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res2))
	}
	out2 := resultJSON(t, res2)
	note, _ := out2["contacts_note"].(string)
	if !strings.Contains(note, "No key contact is indexed for this organization in scope") || !strings.Contains(note, "get_membership_key_contacts per membership is the fallback") {
		t.Errorf("zero contacts must add the fallback sentence, got %q", note)
	}
	if reps := out2["representation"].([]any); len(reps) != 1 || len(reps[0].(map[string]any)["seats"].([]any)) != 1 || len(reps[0].(map[string]any)["voting_contacts"].([]any)) != 0 {
		t.Errorf("representation must still pair the board seat with an empty voting_contacts list, got %v", reps)
	}
	if contacts, has := out2["membership_contacts"]; !has || contacts == nil || len(contacts.([]any)) != 0 {
		t.Errorf("membership_contacts must be present and empty when the flag is set and nothing came back, got %v (present=%t)", contacts, has)
	}
}

func TestOrgSeats_CategoryNarrowsCountsNotTheRepresentation(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(seatsPath, seatsPage(tenSeatsFixture(), ""))
	api.Respond(resourcesPath, page([]string{
		keyContactDoc("kc1", "m-cncf", "p-cncf", "CNCF", "Representative/Voting Contact", "Active", "Vic", "Vote", "vic@x.org"),
	}, ""))
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, Category: "Technical", IncludeMembershipContacts: true})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	// Counts follow the category filter.
	if out["seats_total"] != float64(5) || out["board_seats"] != float64(0) {
		t.Errorf("category=Technical must narrow the counts, got %v", out)
	}
	if byVoting := out["by_voting_status"].(map[string]any); byVoting["None"] != float64(5) || byVoting["Voting Rep"] != nil {
		t.Errorf("by_voting_status follows the category filter, got %v", byVoting)
	}
	// The pairing does not: the board seats on cncf and kubernetes stand
	// beside the voting contact, and every row carries its slug.
	reps := out["representation"].([]any)
	if len(reps) != 2 {
		t.Fatalf("representation must pair against every drained board seat, got %v", reps)
	}
	for _, rp := range reps {
		row := rp.(map[string]any)
		seats := row["seats"].([]any)
		switch row["project_uid"] {
		case uuidFor("p-cncf"):
			if len(seats) != 3 || row["project_slug"] != "cncf" || len(row["voting_contacts"].([]any)) != 1 {
				t.Errorf("cncf row must keep its three board seats, slug and voting contact under category=Technical: %v", row)
			}
			if first := seats[0].(map[string]any); first["last_name"] != "Alpha" {
				t.Errorf("seats must be ordered by committee, last name, first name regardless of the category filter; got %v first", first["last_name"])
			}
		case uuidFor("p-k8s"):
			if len(seats) != 2 || row["project_slug"] != "kubernetes" {
				t.Errorf("kubernetes row must keep its two board seats and slug: %v", row)
			}
		default:
			t.Errorf("unexpected row %v", row)
		}
	}
}

func TestOrgSeats_RepresentationSlugFromAnySeatAndNoRowWithoutProject(t *testing.T) {
	api := setupOrgSeatsTest(t)
	// Only a Technical seat on kubernetes (no board seat there), one board
	// seat with no project at all, and a voting contact on kubernetes plus
	// one with no project_uid.
	noProject := strings.Replace(strings.Replace(seatDoc("s30", "c-b", "Some Board", "Board", "p-none", "", "Zed", "Zero", "zed@x.org", "None", true), `"project_slug": "",`, "", 1), fmt.Sprintf(`"project_uid": %q,`, uuidFor("p-none")), "", 1)
	api.Respond(seatsPath, seatsPage([]string{tenSeatsFixture()[5], noProject}, ""))
	orphan := strings.Replace(keyContactDoc("kc9", "m-x", "p-x", "X", "Representative/Voting Contact", "Active", "Orp", "Han", "orphan@x.org"), fmt.Sprintf(`"project_uid": %q,`, uuidFor("p-x")), "", 1)
	api.Respond(resourcesPath, page([]string{
		keyContactDoc("kc3", "m-k8s", "p-k8s", "Kubernetes", "Representative/Voting Contact", "Active", "Kay", "Eight", "kay@x.org"),
		orphan,
	}, ""))
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, IncludeMembershipContacts: true})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	// Both contacts are returned as contacts...
	if n := len(out["membership_contacts"].([]any)); n != 2 {
		t.Errorf("every contact is listed, got %d", n)
	}
	// ...but only the one with a project earns a representation row, whose
	// slug comes from the Technical seat row.
	reps := out["representation"].([]any)
	if len(reps) != 1 {
		t.Fatalf("one row (kubernetes) expected: no row for a contact or seat without project_uid, got %v", reps)
	}
	row := reps[0].(map[string]any)
	if row["project_uid"] != uuidFor("p-k8s") || row["project_slug"] != "kubernetes" || len(row["seats"].([]any)) != 0 || len(row["voting_contacts"].([]any)) != 1 {
		t.Errorf("kubernetes row must carry the slug from a non-board seat row and an empty seats list: %v", row)
	}
}

func TestOrgSeats_RepresentationPairsOnVotingStatusNotOnlyBoard(t *testing.T) {
	api := setupOrgSeatsTest(t)
	// Project p-other has no board committee: a member-class roster of
	// category "Other" holds a Voting Rep seat, an Alternate Voting Rep seat
	// and an Observer seat; project p-k8s keeps its board.
	api.Respond(seatsPath, seatsPage([]string{
		seatDocVoting("o01", "c-mem", "Members", "Other", "p-other", "other", "Vera", "Rep", "vera@x.org", "None", "Voting Rep", true),
		seatDocVoting("o02", "c-mem", "Members", "Other", "p-other", "other", "Alt", "Rep", "alt@x.org", "None", "Alternate Voting Rep", true),
		seatDocVoting("o03", "c-mem", "Members", "Other", "p-other", "other", "Obs", "Watch", "obs@x.org", "None", "Observer", true),
		// A board seat with status None still pairs, by category.
		seatDocVoting("s09", "c-k8b", "K8s Board", "Board", "p-k8s", "kubernetes", "Hal", "Theta", "hal@x.org", "None", "None", true),
	}, ""))
	api.Respond(resourcesPath, page([]string{
		keyContactDoc("kc7", "m-other", "p-other", "Other", "Representative/Voting Contact", "Active", "Con", "Tact", "con@x.org"),
	}, ""))

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, IncludeMembershipContacts: true})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	out := resultJSON(t, res)
	// Summary counts are untouched by the pairing rule: one board seat, three
	// committee seats, voting statuses as stored.
	if out["board_seats"] != float64(1) || out["committee_seats"] != float64(3) {
		t.Errorf("summary counts must not change: %v", out)
	}
	byVoting := out["by_voting_status"].(map[string]any)
	if byVoting["Voting Rep"] != float64(1) || byVoting["Alternate Voting Rep"] != float64(1) || byVoting["Observer"] != float64(1) || byVoting["None"] != float64(1) {
		t.Errorf("by_voting_status as stored: %v", byVoting)
	}

	reps := out["representation"].([]any)
	if len(reps) != 2 {
		t.Fatalf("expected rows for p-other and p-k8s, got %v", reps)
	}
	for _, rp := range reps {
		row := rp.(map[string]any)
		seats := row["seats"].([]any)
		switch row["project_uid"] {
		case uuidFor("p-other"):
			if len(row["voting_contacts"].([]any)) != 1 {
				t.Errorf("the voting contact pairs on p-other: %v", row)
			}
			if len(seats) != 2 {
				t.Fatalf("Voting Rep and Alternate Voting Rep seats pair, the Observer does not: %v", seats)
			}
			statuses := map[string]bool{}
			for _, sr := range seats {
				seat := sr.(map[string]any)
				statuses[seat["voting_status"].(string)] = true
				if seat["kind"] != "committee_seat" {
					t.Errorf("kind stays the category label (committee_seat) for a seat paired on voting status: %v", seat)
				}
				if seat["committee_category"] != "Other" {
					t.Errorf("category as stored: %v", seat)
				}
			}
			if !statuses["Voting Rep"] || !statuses["Alternate Voting Rep"] || statuses["Observer"] {
				t.Errorf("paired statuses wrong: %v", statuses)
			}
			if row["project_slug"] != "other" {
				t.Errorf("slug from the seat rows: %v", row["project_slug"])
			}
		case uuidFor("p-k8s"):
			if len(seats) != 1 || seats[0].(map[string]any)["kind"] != "board_seat" || seats[0].(map[string]any)["voting_status"] != "None" {
				t.Errorf("a board seat pairs by category whatever its voting status: %v", seats)
			}
		default:
			t.Errorf("unexpected row %v", row)
		}
	}
}

func TestIsRepresentingVotingStatus(t *testing.T) {
	for status, want := range map[string]bool{
		"Voting Rep": true, "Alternate Voting Rep": true, " voting rep ": true, "ALTERNATE VOTING REP": true,
		"Observer": false, "Emeritus": false, "None": false, "": false, "Voting": false,
	} {
		if got := isRepresentingVotingStatus(status); got != want {
			t.Errorf("isRepresentingVotingStatus(%q) = %v, want %v", status, got, want)
		}
	}
	board := orgCommitteeSeat{Kind: seatKindBoard, VotingStatus: "None"}
	other := orgCommitteeSeat{Kind: seatKindCommittee, VotingStatus: "Observer"}
	if !representsOrganization(board) || representsOrganization(other) {
		t.Error("a board seat represents whatever its status; an Observer committee seat does not")
	}
}

func TestOrgSeats_ContactsQueryFailureIsAnError(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(seatsPath, seatsPage(tenSeatsFixture(), ""))
	api.RespondStatus(resourcesPath, http.StatusInternalServerError, `{"message":"search unavailable"}`)
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, IncludeMembershipContacts: true})
	if !res.IsError {
		t.Fatal("a failed contacts query must be an error result, not a silent omission")
	}
	text := allResultText(t, res)
	if !strings.Contains(text, "Failed to get membership key contacts") || !strings.Contains(text, "search unavailable") {
		t.Errorf("error text must name the contacts step and carry the upstream message, got %q", text)
	}
	if strings.Contains(text, "seats_total") {
		t.Error("seats must not be returned alongside the contacts error")
	}
}

func TestOrgSeats_ForbiddenSeatsMakesNoContactsRequest(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.RespondStatus(seatsPath, http.StatusForbidden, `{"message":"forbidden"}`)
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, IncludeMembershipContacts: true})
	if !res.IsError || !strings.Contains(allResultText(t, res), "organisation grant") {
		t.Errorf("403 on seats must return the org-grant message, got %q", allResultText(t, res))
	}
	if n := len(api.RequestsTo(resourcesPath)); n != 0 {
		t.Errorf("no contacts request after a forbidden seats call, made %d", n)
	}
}

func TestOrgSeats_ContactsDrainIsCapped(t *testing.T) {
	api := setupOrgSeatsTest(t)
	api.Respond(seatsPath, seatsPage(tenSeatsFixture()[:1], ""))
	for i := 0; i < participantMaxDrainPages+5; i++ {
		api.Respond(resourcesPath, page([]string{keyContactDoc("kc", "m", "p-cncf", "CNCF", "Billing Contact", "Active", "A", "B", "a@x.org")}, fmt.Sprintf("t%d", i)))
	}
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, IncludeMembershipContacts: true})
	if !res.IsError || !strings.Contains(allResultText(t, res), "page cap") {
		t.Errorf("contacts drain cap must be an error, got %q", allResultText(t, res))
	}
	if n := len(api.RequestsTo(resourcesPath)); n != participantMaxDrainPages {
		t.Errorf("must stop at exactly %d contact pages, made %d", participantMaxDrainPages, n)
	}
}

func TestOrgSeats_DescriptionCoversMembershipContacts(t *testing.T) {
	tool := listRegisteredTool(t, "get_org_committee_seats", RegisterGetOrgCommitteeSeats)
	for _, want := range []string{"include_membership_contacts", "contact of record", "never merged", "by_voting_status", "representation", "board, or Voting Rep / Alternate Voting Rep on any committee"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("description missing %q", want)
		}
	}
}

// familyOf returns n child project docs of root plus the expected family
// uids (root first, children in page order).
func familyOf(root string, n int) (docs []string, family []string) {
	family = []string{root}
	for i := 0; i < n; i++ {
		uid := fmt.Sprintf("%s-child-%03d", root, i)
		docs = append(docs, projectDoc(uid, fmt.Sprintf("child%03d", i), fmt.Sprintf("Child %03d", i), root, ""))
		family = append(family, uid)
	}
	return docs, family
}

func TestChunkStrings(t *testing.T) {
	in := []string{"a", "b", "c", "d", "e"}
	got := chunkStrings(in, 2)
	if len(got) != 3 || strings.Join(got[0], "") != "ab" || strings.Join(got[1], "") != "cd" || strings.Join(got[2], "") != "e" {
		t.Errorf("chunkStrings(5, 2): %v", got)
	}
	if got := chunkStrings(in, 5); len(got) != 1 || len(got[0]) != 5 {
		t.Errorf("exact fit must be one chunk: %v", got)
	}
	if got := chunkStrings(in, 10); len(got) != 1 || len(got[0]) != 5 {
		t.Errorf("size larger than input must be one chunk: %v", got)
	}
	if got := chunkStrings(nil, 3); got != nil {
		t.Errorf("empty input must yield no chunks: %v", got)
	}
	if got := chunkStrings(in, 0); len(got) != 1 || len(got[0]) != 5 {
		t.Errorf("size below one must yield the whole input: %v", got)
	}
}

func TestOrgSeats_LargeFamilyIsReadInChunks(t *testing.T) {
	api := setupOrgSeatsTest(t)
	// Root plus 89 children = 90 uids -> chunks of 40, 40, 10.
	docs, family := familyOf("p-big", 89)
	api.Respond(resourcesPath, page(docs, ""))
	fx := tenSeatsFixture()
	// Chunk 1: two pages. Chunk 2: two pages. Chunk 3: one page.
	api.Respond(seatsPath, seatsPage(fx[:2], "c1p2"))
	api.Respond(seatsPath, seatsPage(fx[2:4], ""))
	api.Respond(seatsPath, seatsPage(fx[4:6], "c2p2"))
	api.Respond(seatsPath, seatsPage(fx[6:8], ""))
	api.Respond(seatsPath, seatsPage(fx[8:], ""))

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, FoundationUID: "p-big"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	reqs := api.RequestsTo(seatsPath)
	if len(reqs) != 5 {
		t.Fatalf("expected five seats requests (3 chunks, two of them paged), got %d", len(reqs))
	}
	// project_uids partition the family in order across the first request of
	// each chunk; continuation pages repeat their chunk's uids.
	var seen []string
	for i, want := range [][]string{family[:40], family[:40], family[40:80], family[40:80], family[80:]} {
		if got := reqs[i].Query["project_uids"]; strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("request %d project_uids: want %d uids starting %s, got %d starting %s", i, len(want), want[0], len(got), firstOf(got))
		}
	}
	for _, i := range []int{0, 2, 4} {
		seen = append(seen, reqs[i].Query["project_uids"]...)
	}
	if strings.Join(seen, ",") != strings.Join(family, ",") {
		t.Error("the chunks' first requests must partition the family in order without gaps or repeats")
	}
	if reqs[1].Query.Get("page_token") != "c1p2" || reqs[3].Query.Get("page_token") != "c2p2" || reqs[0].Query.Get("page_token") != "" || reqs[2].Query.Get("page_token") != "" || reqs[4].Query.Get("page_token") != "" {
		t.Errorf("page tokens must be followed within each chunk and reset between chunks: %+v", reqs)
	}
	out := resultJSON(t, res)
	if out["seats_total"] != float64(10) || out["people"] != float64(9) || out["project_uids_in_scope"] != float64(90) {
		t.Errorf("merged summary must count every seat once across chunks: %v", out)
	}
}

func firstOf(s []string) string {
	if len(s) == 0 {
		return "(none)"
	}
	return s[0]
}

func TestOrgSeats_ChunkForbiddenFailsClosed(t *testing.T) {
	api := setupOrgSeatsTest(t)
	docs, _ := familyOf("p-big", 89)
	api.Respond(resourcesPath, page(docs, ""))
	api.Respond(seatsPath, seatsPage(tenSeatsFixture()[:3], ""))
	api.RespondStatus(seatsPath, http.StatusForbidden, `{"message":"forbidden"}`)
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, FoundationUID: "p-big"})
	if !res.IsError || !strings.Contains(allResultText(t, res), "organisation grant") {
		t.Errorf("a 403 on the second chunk must return the forbidden message, got %q", allResultText(t, res))
	}
	if n := len(api.RequestsTo(seatsPath)); n != 2 {
		t.Errorf("must stop at the failing chunk, made %d seats requests", n)
	}
}

func TestOrgSeats_SmallFamilyIsOneRequest(t *testing.T) {
	api := setupOrgSeatsTest(t)
	// Root plus 39 children = exactly the chunk size: one request, unchanged.
	docs, family := familyOf("p-mid", 39)
	api.Respond(resourcesPath, page(docs, ""))
	api.Respond(seatsPath, seatsPage(tenSeatsFixture(), ""))
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, FoundationUID: "p-mid"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	reqs := api.RequestsTo(seatsPath)
	if len(reqs) != 1 {
		t.Fatalf("a family at or under the chunk size must be one request, made %d", len(reqs))
	}
	if got := reqs[0].Query["project_uids"]; len(got) != orgSeatsProjectChunk || strings.Join(got, ",") != strings.Join(family, ",") {
		t.Errorf("the one request must carry every uid in order: %d uids", len(got))
	}
	if out := resultJSON(t, res); out["seats_total"] != float64(10) {
		t.Errorf("summary: %v", out)
	}
}

func TestOrgSeats_ChunkPageCapIsAnError(t *testing.T) {
	api := setupOrgSeatsTest(t)
	docs, _ := familyOf("p-big", 89)
	api.Respond(resourcesPath, page(docs, ""))
	// First chunk drains in one page; the second never ends.
	api.Respond(seatsPath, seatsPage(tenSeatsFixture()[:1], ""))
	for i := 0; i < orgSeatsMaxPages+5; i++ {
		api.Respond(seatsPath, seatsPage(tenSeatsFixture()[1:2], fmt.Sprintf("t%d", i)))
	}
	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, FoundationUID: "p-big"})
	if !res.IsError || !strings.Contains(allResultText(t, res), "page cap") {
		t.Errorf("the page cap applies per chunk and is an error, got %q", allResultText(t, res))
	}
	if n := len(api.RequestsTo(seatsPath)); n != 1+orgSeatsMaxPages {
		t.Errorf("must stop at the cap of the second chunk: made %d requests, want %d", n, 1+orgSeatsMaxPages)
	}
}

func TestOrgSeats_PersonSeatedAcrossChunksCountsOnce(t *testing.T) {
	api := setupOrgSeatsTest(t)
	docs, family := familyOf("p-big", 89) // chunks: [0:40], [40:80], [80:90]
	api.Respond(resourcesPath, page(docs, ""))
	// ann@x.org holds a board seat on a project of chunk 1 and a technical
	// seat on a project of chunk 3; bob@x.org one seat in chunk 2.
	api.Respond(seatsPath, seatsPage([]string{
		seatDoc("x01", "c-gb", "Governing Board", "Board", family[3], "chunk1-proj", "Ann", "Alpha", "ann@x.org", "Chair", true),
	}, ""))
	api.Respond(seatsPath, seatsPage([]string{
		seatDoc("x02", "c-sc", "Steering", "Technical", family[45], "chunk2-proj", "Bob", "Beta", "bob@x.org", "None", false),
	}, ""))
	api.Respond(seatsPath, seatsPage([]string{
		seatDoc("x03", "c-toc", "TOC", "Technical", family[85], "chunk3-proj", "Ann", "Alpha", "ANN@x.org", "None", false),
	}, ""))

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, FoundationUID: "p-big", IncludeSeats: true})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	// The three chunks must not overlap and must cover the family exactly.
	reqs := api.RequestsTo(seatsPath)
	if len(reqs) != 3 {
		t.Fatalf("expected three chunk requests, got %d", len(reqs))
	}
	seen := map[string]int{}
	var union []string
	for i, r := range reqs {
		uids := r.Query["project_uids"]
		wantLen := []int{40, 40, 10}[i]
		if len(uids) != wantLen {
			t.Errorf("chunk %d carries %d uids, want %d", i, len(uids), wantLen)
		}
		for _, u := range uids {
			seen[u]++
			union = append(union, u)
		}
	}
	for u, n := range seen {
		if n != 1 {
			t.Errorf("uid %s sent in %d chunks; chunks must not overlap", u, n)
		}
	}
	if strings.Join(union, ",") != strings.Join(family, ",") {
		t.Error("the union of the chunks must be the family, in order")
	}

	// One summary over the merged rows: the person counts once, both seats count.
	out := resultJSON(t, res)
	if out["seats_total"] != float64(3) || out["people"] != float64(2) || out["board_seats"] != float64(1) || out["committee_seats"] != float64(2) {
		t.Errorf("merged summary wrong: seats_total=%v people=%v board=%v committee=%v", out["seats_total"], out["people"], out["board_seats"], out["committee_seats"])
	}
	if out["editable"] != float64(1) || out["foundation_controlled"] != float64(2) || out["project_uids_in_scope"] != float64(90) {
		t.Errorf("merged arithmetic wrong: %v", out)
	}
	byProject := out["by_project"].(map[string]any)
	if byProject["chunk1-proj"] != float64(1) || byProject["chunk2-proj"] != float64(1) || byProject["chunk3-proj"] != float64(1) {
		t.Errorf("by_project must span every chunk: %v", byProject)
	}
	if rows := out["seats"].([]any); len(rows) != 3 || rows[0].(map[string]any)["committee_name"] != "Governing Board" {
		t.Errorf("rows must be merged and sorted once (committee name first): %v", rows)
	}
}

func TestOrgSeats_RepeatedFamilyUIDIsSentOnce(t *testing.T) {
	api := setupOrgSeatsTest(t)
	// The index echoes child p-k8s on both pages and the root as its own child.
	api.Respond(resourcesPath, page([]string{projectDoc("p-k8s", "kubernetes", "Kubernetes", "p-cncf", ""), projectDoc("p-cncf", "cncf", "CNCF", "p-cncf", "")}, "more"))
	api.Respond(resourcesPath, page([]string{projectDoc("p-k8s", "kubernetes", "Kubernetes", "p-cncf", ""), projectDoc("p-env", "envoy", "Envoy", "p-cncf", "")}, ""))
	api.Respond(seatsPath, seatsPage(tenSeatsFixture()[:3], ""))

	res, _, _ := handleGetOrgCommitteeSeats(context.Background(), stubCallToolRequest(), GetOrgCommitteeSeatsArgs{B2bOrgUID: testSFID, FoundationUID: "p-cncf"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", allResultText(t, res))
	}
	want := []string{"p-cncf", "p-k8s", "p-env"}
	if got := api.RequestsTo(seatsPath)[0].Query["project_uids"]; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("each family uid must be sent once, root first: want %v got %v", want, got)
	}
	if out := resultJSON(t, res); out["project_uids_in_scope"] != float64(3) {
		t.Errorf("project_uids_in_scope counts each uid once, got %v", out["project_uids_in_scope"])
	}
}

func TestDedupeStrings(t *testing.T) {
	got := dedupeStrings([]string{"a", "b", "a", "c", "b"})
	if strings.Join(got, "") != "abc" {
		t.Errorf("dedupeStrings keeps the first occurrence in order, got %v", got)
	}
	if got := dedupeStrings(nil); len(got) != 0 {
		t.Errorf("empty input yields empty output, got %v", got)
	}
}

func TestOrgSeats_DescriptionMentionsChunkedReads(t *testing.T) {
	tool := listRegisteredTool(t, "get_org_committee_seats", RegisterGetOrgCommitteeSeats)
	if !strings.Contains(tool.Description, "large foundations being read in several requests") {
		t.Error("description must state that large foundations are read in several requests")
	}
}
