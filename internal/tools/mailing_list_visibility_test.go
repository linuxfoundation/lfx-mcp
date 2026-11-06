// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Mailing lists of one project, as the caller's relations to them make them:
// one the caller manages, one it audits, and one it only subscribes to.
const (
	mlWriterUID     = "150001"
	mlAuditorUID    = "150002"
	mlSubscriberUID = "150003"
)

// mailingListDoc is a groupsio_mailing_list query record; the query
// service's ID is the object ref.
func mailingListDoc(uid string) string {
	return fmt.Sprintf(`{"type": "groupsio_mailing_list", "id": "groupsio_mailing_list:%s", "data": {"uid": %q, "group_id": %s, "project_uid": "P1", "public": true}}`, uid, uid, uid)
}

// mailingListMemberDoc is a groupsio_member query record of list listUID.
func mailingListMemberDoc(uid, listUID, email string) string {
	return fmt.Sprintf(`{"type": "groupsio_member", "id": "groupsio_member:%s", "data": {"uid": %q, "mailing_list_uid": %q, "group_id": %s, "username": "user-%s", "first_name": "Sam", "last_name": "Subscriber", "email": %q, "project_uid": "P1"}}`, uid, uid, listUID, listUID, uid, email)
}

// mailingListMemberDocAs is a groupsio_member query record of list listUID
// with the given username and e-mail.
func mailingListMemberDocAs(uid, listUID, username, email string) string {
	return fmt.Sprintf(`{"type": "groupsio_member", "id": "groupsio_member:%s", "data": {"uid": %q, "mailing_list_uid": %q, "group_id": %s, "username": %q, "first_name": "Sam", "last_name": "Subscriber", "email": %q, "project_uid": "P1"}}`, uid, uid, listUID, listUID, username, email)
}

// ownMailingListFilters is the identity part of the filters_or clause for
// stubCallToolRequest's caller: username, then the e-mail claim as given and
// lowercased.
var ownMailingListFilters = []string{"username:" + stubCallerUsername, "email:" + stubCallerEmail, "email:" + strings.ToLower(stubCallerEmail)}

// mailingListRequestAs is stubCallToolRequest with the caller's username and
// e-mail claims replaced; an empty value removes the claim.
func mailingListRequestAs(username, email string) *mcp.CallToolRequest {
	req := stubCallToolRequest()
	delete(req.Extra.TokenInfo.Extra, "username")
	delete(req.Extra.TokenInfo.Extra, ClaimEmail)
	if username != "" {
		req.Extra.TokenInfo.Extra["username"] = username
	}
	if email != "" {
		req.Extra.TokenInfo.Extra[ClaimEmail] = email
	}
	return req
}

// listRelation is the relation request the rule asks for one list.
func listRelation(uid, relation string) string {
	return "groupsio_mailing_list:" + uid + "#" + relation
}

// memberQueries returns the groupsio_member queries the stub saw.
func mailingListMemberQueries(api *stubLFXAPI) []stubAPIRequest {
	var out []stubAPIRequest
	for _, r := range api.RequestsTo(resourcesPath) {
		if r.Query.Get("type") == mailingListMemberResourceType {
			out = append(out, r)
		}
	}
	return out
}

func TestSearchMailingListMembers_ManagersAndAuditorsGetTheList(t *testing.T) {
	for _, tc := range []struct {
		name    string
		listUID string
		grant   string
	}{
		{"writer", mlWriterUID, listRelation(mlWriterUID, "writer")},
		{"auditor", mlAuditorUID, listRelation(mlAuditorUID, "auditor")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMailingListTest(t)
			api.GrantRelations(tc.grant)
			doc := mailingListMemberDoc("gm-1", tc.listUID, "subscriber@example.test")
			api.Respond(resourcesPath, page([]string{doc}, ""))

			_, out, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{MailingListID: tc.listUID, Name: "Sam"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(out.Resources) != 1 || dataString(out.Resources[0].Data, "email") != "subscriber@example.test" {
				t.Fatalf("expected the member unchanged, got %+v", out.Resources)
			}
			// Both relations are asked: auditor does not include writer.
			bodies := api.AccessCheckBodies()
			if len(bodies) != 1 || !slices.Contains(bodies[0], listRelation(tc.listUID, "writer")) || !slices.Contains(bodies[0], listRelation(tc.listUID, "auditor")) {
				t.Errorf("expected one access-check asking writer and auditor, got %v", bodies)
			}
			q := mailingListMemberQueries(api)
			if len(q) != 1 || strings.Join(q[0].Query["tags_all"], ",") != "mailing_list_uid:"+tc.listUID || q[0].Query.Get("name") != "Sam" {
				t.Errorf("expected the one-list query as before, got %+v", q)
			}
			if _, ok := q[0].Query["filters_or"]; ok {
				t.Errorf("a shown list needs no narrowing clause, got %v", q[0].Query["filters_or"])
			}
		})
	}
}

func TestSearchMailingListMembers_UnmanagedListReadsOnlyOwnRecords(t *testing.T) {
	// The stub ignores filters_or, so each page carries a stranger's record
	// next to the caller's: the narrowed query is what keeps the stranger's
	// record from being read, and the second check drops it from the page.
	for _, tc := range []struct {
		name string
		doc  string
	}{
		{"own by e-mail", mailingListMemberDocAs("gm-1", mlSubscriberUID, "someone-else", "stub.user@example.test")},
		{"own by e-mail in another casing", mailingListMemberDocAs("gm-1", mlSubscriberUID, "", "STUB.USER@EXAMPLE.TEST")},
		{"own by username", mailingListMemberDocAs("gm-1", mlSubscriberUID, stubCallerUsername, "other-address@example.test")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMailingListTest(t)
			// A subscriber is a member, and so a viewer, of the list:
			// neither shows its member list.
			api.GrantRelations(listRelation(mlSubscriberUID, "member"), listRelation(mlSubscriberUID, "viewer"))
			api.Respond(resourcesPath, page([]string{tc.doc, mailingListMemberDoc("gm-2", mlSubscriberUID, "stranger@example.test")}, ""))

			_, out, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{MailingListID: mlSubscriberUID, Name: "Sam"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			q := mailingListMemberQueries(api)
			if len(q) != 1 {
				t.Fatalf("expected one member query, got %d", len(q))
			}
			if got := q[0].Query["filters_or"]; !slices.Equal(got, ownMailingListFilters) {
				t.Errorf("the query must be narrowed to the caller's identity, got filters_or %q", got)
			}
			if got := strings.Join(q[0].Query["tags_all"], ","); got != "mailing_list_uid:"+mlSubscriberUID {
				t.Errorf("the list tag stays, got %q", got)
			}
			if q[0].Query.Get("name") != "Sam" {
				t.Errorf("name runs within the caller's own records, got %q", q[0].Query.Get("name"))
			}
			if len(out.Resources) != 1 || out.Resources[0].ID != "groupsio_member:gm-1" {
				t.Fatalf("expected only the caller's own record, got %+v", out.Resources)
			}
			if all := everything(t, nil, out, nil); strings.Contains(all, "stranger@example.test") {
				t.Errorf("another member's record must not be returned:\n%s", all)
			}
		})
	}
}

func TestSearchMailingListMembers_UnscopedReadsOwnSubscriptions(t *testing.T) {
	api := setupMailingListTest(t)
	api.Respond(resourcesPath, page([]string{
		mailingListMemberDocAs("gm-1", mlSubscriberUID, stubCallerUsername, "stub.user@example.test"),
		mailingListMemberDocAs("gm-2", mlWriterUID, "", stubCallerEmail),
		mailingListMemberDoc("gm-3", mlAuditorUID, "stranger@example.test"),
	}, ""))

	_, out, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{Name: "Sam"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := len(api.RequestsTo(accessCheckPath)); n != 0 {
		t.Errorf("an unscoped search names no list to check, got %d access-checks", n)
	}
	q := mailingListMemberQueries(api)
	if len(q) != 1 {
		t.Fatalf("expected one member query, got %d", len(q))
	}
	if got := q[0].Query["filters_or"]; !slices.Equal(got, ownMailingListFilters) {
		t.Errorf("the query must be narrowed to the caller's identity, got filters_or %q", got)
	}
	if _, ok := q[0].Query["tags_all"]; ok || q[0].Query.Get("name") != "Sam" {
		t.Errorf("expected no scope tag and the name filter, got %+v", q[0].Query)
	}
	var ids []string
	for _, r := range out.Resources {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != "groupsio_member:gm-1,groupsio_member:gm-2" {
		t.Errorf("expected the caller's own subscriptions only, got %v", ids)
	}
}

func TestSearchMailingListMembers_NoIdentityReadsNothingElse(t *testing.T) {
	// A caller with neither username nor e-mail can be shown no record of a
	// list they do not manage or audit: the query is not sent, and the
	// result is the ordinary empty page.
	for _, tc := range []struct {
		name   string
		args   SearchMailingListMembersArgs
		grants []string
		lists  []string
	}{
		{"unmanaged list", SearchMailingListMembersArgs{MailingListID: mlSubscriberUID}, []string{listRelation(mlSubscriberUID, "member")}, nil},
		{"no scope", SearchMailingListMembersArgs{Name: "Sam"}, nil, nil},
		{"project with no shown list", SearchMailingListMembersArgs{ProjectUID: "P1"}, []string{listRelation(mlSubscriberUID, "member")}, []string{mailingListDoc(mlSubscriberUID)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMailingListTest(t)
			api.GrantRelations(tc.grants...)
			if tc.args.ProjectUID != "" {
				api.Respond(resourcesPath, page(tc.lists, ""))
			}
			api.Respond(resourcesPath, page([]string{mailingListMemberDoc("gm-1", mlSubscriberUID, "stranger@example.test")}, ""))
			res, out, err := handleSearchMailingListMembers(context.Background(), mailingListRequestAs("", ""), tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n := len(mailingListMemberQueries(api)); n != 0 {
				t.Errorf("no member query may be sent, got %d", n)
			}
			if len(out.Resources) != 0 || len(out.Warnings) == 0 {
				t.Errorf("expected an empty page with the search warning, got %+v", out)
			}
			if all := everything(t, res, out, nil); strings.Contains(all, "stranger@example.test") {
				t.Errorf("nothing may be returned:\n%s", all)
			}
		})
	}
}

func TestSearchMailingListMembers_FailsClosed(t *testing.T) {
	t.Run("relation check fails", func(t *testing.T) {
		api := setupMailingListTest(t)
		api.FailAccessCheck(http.StatusInternalServerError)
		api.Respond(resourcesPath, page([]string{mailingListMemberDoc("gm-1", mlWriterUID, "subscriber@example.test")}, ""))
		_, out, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{MailingListID: mlWriterUID})
		if err == nil || err.Error() != mailingListVisibilityUnavailableMessage || len(out.Resources) != 0 {
			t.Fatalf("expected the unavailable message and no records, got %v %+v", err, out)
		}
		if n := len(mailingListMemberQueries(api)); n != 0 {
			t.Errorf("no member query after a failed check, got %d", n)
		}
	})
	t.Run("project list lookup fails", func(t *testing.T) {
		api := setupMailingListTest(t)
		api.GrantRelations(listRelation(mlWriterUID, "writer"))
		api.RespondStatus(resourcesPath, http.StatusInternalServerError, `{"message":"boom"}`)
		_, _, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{ProjectUID: "P1"})
		if err == nil || err.Error() != mailingListVisibilityUnavailableMessage {
			t.Fatalf("expected the unavailable message, got %v", err)
		}
	})
}

func TestSearchMailingListMembers_ProjectIsNarrowedToShownLists(t *testing.T) {
	api := setupMailingListTest(t)
	api.GrantRelations(listRelation(mlWriterUID, "writer"), listRelation(mlAuditorUID, "auditor"), listRelation(mlSubscriberUID, "member"))
	api.Respond(resourcesPath, page([]string{mailingListDoc(mlWriterUID), mailingListDoc(mlAuditorUID)}, "more"))
	api.Respond(resourcesPath, page([]string{mailingListDoc(mlSubscriberUID)}, ""))
	// The page carries a stranger's record of the subscriber's list, which
	// the narrowing would not return: the second check drops it. The
	// caller's own record on that list stays.
	api.Respond(resourcesPath, page([]string{
		mailingListMemberDoc("gm-1", mlWriterUID, "one@example.test"),
		mailingListMemberDoc("gm-2", mlAuditorUID, "two@example.test"),
		mailingListMemberDoc("gm-3", mlSubscriberUID, "three@example.test"),
		mailingListMemberDocAs("gm-4", mlSubscriberUID, stubCallerUsername, "stub.user@example.test"),
	}, ""))

	_, out, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{ProjectUID: "P1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lookups := api.RequestsTo(resourcesPath)
	if len(lookups) != 3 || lookups[0].Query.Get("type") != mailingListResourceType || lookups[0].Query.Get("parent") != "project:P1" || lookups[1].Query.Get("page_token") != "more" {
		t.Fatalf("expected the paged list lookup on the project parent, got %+v", lookups)
	}
	q := mailingListMemberQueries(api)
	if len(q) != 1 {
		t.Fatalf("expected one member query, got %d", len(q))
	}
	if got, want := q[0].Query["filters_or"], append([]string{"mailing_list_uid:" + mlWriterUID, "mailing_list_uid:" + mlAuditorUID}, ownMailingListFilters...); !slices.Equal(got, want) {
		t.Errorf("the query must be narrowed to the shown lists and the caller, got filters_or %q", got)
	}
	if got := strings.Join(q[0].Query["tags_all"], ","); got != "project_uid:P1" {
		t.Errorf("the project tag stays, got %q", got)
	}
	var emails []string
	for _, r := range out.Resources {
		emails = append(emails, dataString(r.Data, "email"))
	}
	if strings.Join(emails, ",") != "one@example.test,two@example.test,stub.user@example.test" {
		t.Errorf("expected the shown lists' members and the caller's own record, got %v", emails)
	}
	if all := everything(t, nil, out, nil); strings.Contains(all, "three@example.test") {
		t.Errorf("another member of the withheld list must not be returned:\n%s", all)
	}
}

func TestSearchMailingListMembers_ProjectListUIDFromDataOrID(t *testing.T) {
	// One list record carries only the object ref as its ID, the other only
	// data.uid: both name the list by its bare uid.
	const idOnlyUID, dataOnlyUID = "180001", "180002"
	api := setupMailingListTest(t)
	api.GrantRelations(listRelation(idOnlyUID, "writer"), listRelation(dataOnlyUID, "auditor"))
	api.Respond(resourcesPath, page([]string{
		fmt.Sprintf(`{"type": "groupsio_mailing_list", "id": "groupsio_mailing_list:%s", "data": {"group_id": %s, "project_uid": "P1"}}`, idOnlyUID, idOnlyUID),
		fmt.Sprintf(`{"type": "groupsio_mailing_list", "data": {"uid": %q, "group_id": %s, "project_uid": "P1"}}`, dataOnlyUID, dataOnlyUID),
	}, ""))
	api.Respond(resourcesPath, page([]string{
		mailingListMemberDoc("gm-1", idOnlyUID, "one@example.test"),
		mailingListMemberDoc("gm-2", dataOnlyUID, "two@example.test"),
	}, ""))

	_, out, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{ProjectUID: "P1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bodies := api.AccessCheckBodies()
	want := []string{listRelation(idOnlyUID, "writer"), listRelation(idOnlyUID, "auditor"), listRelation(dataOnlyUID, "writer"), listRelation(dataOnlyUID, "auditor")}
	if len(bodies) != 1 || !slices.Equal(bodies[0], want) {
		t.Errorf("expected the relations of both bare uids, got %v", bodies)
	}
	q := mailingListMemberQueries(api)
	if len(q) != 1 {
		t.Fatalf("expected one member query, got %d", len(q))
	}
	if got, want := q[0].Query["filters_or"], append([]string{"mailing_list_uid:" + idOnlyUID, "mailing_list_uid:" + dataOnlyUID}, ownMailingListFilters...); !slices.Equal(got, want) {
		t.Errorf("expected the query narrowed to both lists and the caller, got filters_or %q", got)
	}
	if len(out.Resources) != 2 {
		t.Errorf("expected both members, got %d", len(out.Resources))
	}
}

func TestSearchMailingListMembers_ProjectWithNoShownListReadsOwnRecords(t *testing.T) {
	for _, tc := range []struct {
		name   string
		grants []string
		lists  []string
	}{
		{"no shown list", []string{listRelation(mlSubscriberUID, "member")}, []string{mailingListDoc(mlSubscriberUID)}},
		{"no visible list", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMailingListTest(t)
			api.GrantRelations(tc.grants...)
			api.Respond(resourcesPath, page(tc.lists, ""))
			api.Respond(resourcesPath, page([]string{
				mailingListMemberDocAs("gm-1", mlSubscriberUID, stubCallerUsername, "stub.user@example.test"),
				mailingListMemberDoc("gm-2", mlSubscriberUID, "stranger@example.test"),
			}, ""))
			_, out, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{ProjectUID: "P1", Name: "Sam"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			q := mailingListMemberQueries(api)
			if len(q) != 1 || !slices.Equal(q[0].Query["filters_or"], ownMailingListFilters) || strings.Join(q[0].Query["tags_all"], ",") != "project_uid:P1" {
				t.Fatalf("expected one query narrowed to the caller within the project, got %+v", q)
			}
			if len(out.Resources) != 1 || out.Resources[0].ID != "groupsio_member:gm-1" {
				t.Errorf("expected only the caller's own record, got %+v", out.Resources)
			}
		})
	}
}

func TestSearchMailingListMembers_ProjectRefusals(t *testing.T) {
	t.Run("list lookup beyond its page cap", func(t *testing.T) {
		api := setupMailingListTest(t)
		api.GrantRelations()
		for i := 0; i < peopleLookupMaxPages; i++ {
			api.Respond(resourcesPath, page([]string{mailingListDoc(fmt.Sprint(160000 + i))}, fmt.Sprint("p", i)))
		}
		_, _, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{ProjectUID: "P1"})
		if err == nil || err.Error() != mailingListLookupCapRefusal {
			t.Fatalf("expected the lookup-cap refusal, got %v", err)
		}
		if len(mailingListMemberQueries(api)) != 0 || len(api.RequestsTo(accessCheckPath)) != 0 {
			t.Error("a refused search must check and query nothing further")
		}
	})
	t.Run("shown lists beyond one clause", func(t *testing.T) {
		api := setupMailingListTest(t)
		var docs, grants []string
		for i := 0; i <= peopleFilterChunk; i++ {
			uid := fmt.Sprint(170000 + i)
			docs = append(docs, mailingListDoc(uid))
			grants = append(grants, listRelation(uid, "writer"))
		}
		api.GrantRelations(grants...)
		api.Respond(resourcesPath, page(docs, ""))
		_, _, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{ProjectUID: "P1"})
		if err == nil || err.Error() != mailingListFilterCapRefusal || len(mailingListMemberQueries(api)) != 0 {
			t.Fatalf("expected the filter-cap refusal and no member query, got %v", err)
		}
	})
	t.Run("shown lists and identity beyond one clause", func(t *testing.T) {
		// The lists alone fit the clause; with the caller's identity
		// terms they do not.
		api := setupMailingListTest(t)
		var docs, grants []string
		for i := 0; i < peopleFilterChunk-len(ownMailingListFilters)+1; i++ {
			uid := fmt.Sprint(170000 + i)
			docs = append(docs, mailingListDoc(uid))
			grants = append(grants, listRelation(uid, "writer"))
		}
		api.GrantRelations(grants...)
		api.Respond(resourcesPath, page(docs, ""))
		_, _, err := handleSearchMailingListMembers(context.Background(), stubCallToolRequest(), SearchMailingListMembersArgs{ProjectUID: "P1"})
		if err == nil || err.Error() != mailingListFilterCapRefusal || len(mailingListMemberQueries(api)) != 0 {
			t.Fatalf("expected the filter-cap refusal and no member query, got %v", err)
		}
	})
}

func TestGetMailingListMember_ManagersAndAuditorsGetTheRecord(t *testing.T) {
	for _, grant := range []string{listRelation(fullViewMailingListID, "writer"), listRelation(fullViewMailingListID, "auditor")} {
		t.Run(grant, func(t *testing.T) {
			api := setupMailingListTest(t)
			api.GrantRelations(grant)
			api.Respond(fullViewMailingListMemberPath, fullViewMailingListMemberRecord)
			res, _, err := handleGetMailingListMember(context.Background(), stubCallToolRequest(), GetMailingListMemberArgs{MailingListID: fullViewMailingListID, MemberID: fullViewMailingListMemberID})
			if err != nil || res.IsError || !strings.Contains(allResultText(t, res), "subscriber@example.test") {
				t.Fatalf("expected the record, got %v %s", err, allResultText(t, res))
			}
		})
	}
}

// missingMailingListMemberText is the text a caller with full view gets for
// a member that does not exist: the service's 404 through friendlyAPIError.
func missingMailingListMemberText(t *testing.T) string {
	t.Helper()
	api := setupMailingListTest(t)
	api.RespondStatus(fullViewMailingListMemberPath, http.StatusNotFound, `{"name":"NotFound","message":"member not found"}`)
	missing, _, _ := handleGetMailingListMember(fullViewCtx(), stubCallToolRequest(), GetMailingListMemberArgs{MailingListID: fullViewMailingListID, MemberID: fullViewMailingListMemberID})
	if !missing.IsError {
		t.Fatal("expected an error for the missing member")
	}
	return allResultText(t, missing)
}

func TestGetMailingListMember_OwnRecordOnAnyList(t *testing.T) {
	// fullViewMailingListMemberRecord is subscriber-1's, at
	// subscriber@example.test.
	for _, tc := range []struct {
		name            string
		username, email string
	}{
		{"by e-mail", "someone-else", "subscriber@example.test"},
		{"by e-mail in another casing", "", "Subscriber@Example.TEST"},
		{"by username", "subscriber-1", "other-address@example.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMailingListTest(t)
			api.GrantRelations(listRelation(fullViewMailingListID, "member"), listRelation(fullViewMailingListID, "viewer"))
			api.Respond(fullViewMailingListMemberPath, fullViewMailingListMemberRecord)
			res, _, err := handleGetMailingListMember(context.Background(), mailingListRequestAs(tc.username, tc.email), GetMailingListMemberArgs{MailingListID: fullViewMailingListID, MemberID: fullViewMailingListMemberID})
			if err != nil || res.IsError || !strings.Contains(allResultText(t, res), "subscriber@example.test") {
				t.Fatalf("expected the caller's own record, got %v %s", err, allResultText(t, res))
			}
		})
	}
}

func TestGetMailingListMember_NotShownReadsAsNotFound(t *testing.T) {
	want := missingMailingListMemberText(t)
	for _, tc := range []struct {
		name string
		req  *mcp.CallToolRequest
		// fetches is whether the record is fetched to be matched: a
		// caller with no identity has nothing to match it against.
		fetches bool
	}{
		{"another member's record", stubCallToolRequest(), true},
		{"caller without identity", mailingListRequestAs("", ""), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupMailingListTest(t)
			api.GrantRelations(listRelation(fullViewMailingListID, "member"), listRelation(fullViewMailingListID, "viewer"))
			api.Respond(fullViewMailingListMemberPath, fullViewMailingListMemberRecord)
			hidden, _, err := handleGetMailingListMember(context.Background(), tc.req, GetMailingListMemberArgs{MailingListID: fullViewMailingListID, MemberID: fullViewMailingListMemberID})
			if err != nil || !hidden.IsError {
				t.Fatalf("expected an error result, got %v %s", err, allResultText(t, hidden))
			}
			if got := allResultText(t, hidden); got != want {
				t.Errorf("a member not shown must read as a missing one:\n got %q\nwant %q", got, want)
			}
			if n := len(api.RequestsTo(fullViewMailingListMemberPath)); (n != 0) != tc.fetches {
				t.Errorf("record fetches: %d, want fetched=%v", n, tc.fetches)
			}
		})
	}
}

func TestGetMailingListMember_CheckFailureFailsClosed(t *testing.T) {
	api := setupMailingListTest(t)
	api.FailAccessCheck(http.StatusBadGateway)
	api.Respond(fullViewMailingListMemberPath, fullViewMailingListMemberRecord)
	res, _, _ := handleGetMailingListMember(context.Background(), stubCallToolRequest(), GetMailingListMemberArgs{MailingListID: fullViewMailingListID, MemberID: fullViewMailingListMemberID})
	if !res.IsError || strings.TrimSpace(allResultText(t, res)) != mailingListVisibilityUnavailableMessage {
		t.Fatalf("expected the unavailable message, got %s", allResultText(t, res))
	}
	if strings.Contains(allResultText(t, res), "subscriber@example.test") {
		t.Error("no record may be returned with the failure")
	}
}

func TestCountLFXResources_MailingListMembersFollowTheRule(t *testing.T) {
	// The refusal names the allowed form without saying it is what LFX Self
	// Serve shows: this rule is stricter than Self Serve's screen.
	const refusal = "Error: counting groupsio_member is available only with parent=groupsio_mailing_list:<uid> for a mailing list you manage or audit."
	for _, tc := range []struct {
		name    string
		grant   string
		args    CountLFXResourcesArgs
		allowed bool
		// checks is whether the gate asks access-check: a parent that
		// names no list is refused before any relation is checked.
		checks bool
	}{
		{"writer parent", listRelation(mlWriterUID, "writer"), CountLFXResourcesArgs{Parent: "groupsio_mailing_list:" + mlWriterUID, Tags: []string{"status:normal"}}, true, true},
		{"auditor parent", listRelation(mlAuditorUID, "auditor"), CountLFXResourcesArgs{Parent: "groupsio_mailing_list:" + mlAuditorUID}, true, true},
		{"subscriber parent", listRelation(mlSubscriberUID, "member"), CountLFXResourcesArgs{Parent: "groupsio_mailing_list:" + mlSubscriberUID}, false, true},
		{"no parent", listRelation(mlWriterUID, "writer"), CountLFXResourcesArgs{TagsAll: []string{"mailing_list_uid:" + mlWriterUID}}, false, false},
		{"project parent", listRelation(mlWriterUID, "writer"), CountLFXResourcesArgs{Parent: "project:P1"}, false, false},
		{"empty list uid", listRelation(mlWriterUID, "writer"), CountLFXResourcesArgs{Parent: "groupsio_mailing_list:"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := setupCountTest(t)
			api.GrantRelations(tc.grant)
			api.Respond(countPath, `{"count": 3, "has_more": false}`)
			args := tc.args
			args.Type = mailingListMemberResourceType
			res, _, _ := handleCountLFXResources(context.Background(), stubCallToolRequest(), args)
			counted := len(api.RequestsTo(countPath))
			if checked := len(api.RequestsTo(accessCheckPath)) != 0; checked != tc.checks {
				t.Errorf("access-check called: %v, want %v", checked, tc.checks)
			}
			if tc.allowed {
				if res.IsError || counted != 1 {
					t.Fatalf("expected the count, got %s", allResultText(t, res))
				}
				return
			}
			if !res.IsError || strings.TrimSpace(allResultText(t, res)) != refusal || counted != 0 {
				t.Fatalf("expected the refusal and no count, got %s (%d counts)", allResultText(t, res), counted)
			}
		})
	}
	t.Run("relation check fails", func(t *testing.T) {
		api := setupCountTest(t)
		api.FailAccessCheck(http.StatusInternalServerError)
		res, _, _ := handleCountLFXResources(context.Background(), stubCallToolRequest(), CountLFXResourcesArgs{Type: mailingListMemberResourceType, Parent: "groupsio_mailing_list:" + mlWriterUID})
		if !res.IsError || strings.TrimSpace(allResultText(t, res)) != mailingListVisibilityUnavailableMessage || len(api.RequestsTo(countPath)) != 0 {
			t.Fatalf("expected the unavailable message, got %s", allResultText(t, res))
		}
	})
}

func TestSearchMailingListMembersDescribesTheRule(t *testing.T) {
	const want = "Without LFX-wide access, you get the members of mailing lists you manage or audit and your own subscriptions on other lists."
	tool := listRegisteredTool(t, "search_mailing_list_members", RegisterSearchMailingListMembers)
	if !strings.Contains(tool.Description, want) {
		t.Errorf("description missing %q", want)
	}
	if n := len(tool.Description); n > schemaDescriptionBudget {
		t.Errorf("description is %d bytes, over the %d budget", n, schemaDescriptionBudget)
	}
	for _, banned := range []string{"Insights", "%", "staff"} {
		if strings.Contains(tool.Description, banned) {
			t.Errorf("description must not contain %q", banned)
		}
	}
}
