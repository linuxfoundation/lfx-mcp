// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	goa "goa.design/goa/v3/pkg"
)

// update_committee_member is a fetch-merge-put. These tests pin the request
// body it sends, byte for byte where it matters, against two committee
// service contracts:
//
//   - today's: the update body must carry an e-mail (an omitted one is "email
//     is missing from body", an empty one fails the e-mail format);
//   - after linuxfoundation/lfx-v2-committee-service#226: the member GET omits
//     the e-mail, an e-mail present on the body must be well formed, and an
//     omitted one is restored server-side from the stored record.
//
// The rule that satisfies both: send the e-mail when the tool holds one (the
// caller gave it, or the GET returned it) and leave the key out otherwise.

const (
	updateMemberCommitteeUID = "0f7a2c58-6d3e-4b1a-9c2e-1a2b3c4d5e6f"
	updateMemberUID          = "9b8c7d6e-5f4a-4b3c-8d2e-0f1e2d3c4b5a"
	updateMemberEmail        = "member@example.test"
	updateMemberETag         = `W/"7"`
)

// memberContract is how a fake committee service validates the e-mail on an
// update body.
type memberContract int

const (
	contractToday memberContract = iota
	contractAfter226
)

func (c memberContract) String() string {
	if c == contractToday {
		return "today"
	}
	return "after226"
}

// validateMemberUpdateEmail applies one contract's e-mail rule to a decoded
// update body, with the same format check goa generates for both services.
func validateMemberUpdateEmail(c memberContract, body map[string]json.RawMessage) error {
	raw, present := body["email"]
	if !present {
		if c == contractToday {
			return goa.MissingFieldError("email", "body")
		}
		return nil
	}
	var email string
	if err := json.Unmarshal(raw, &email); err != nil {
		return err
	}
	return goa.ValidateFormat("body.email", email, goa.FormatEmail)
}

// memberRecord is the member JSON the fake GET returns; withEmail is false
// for the post-#226 GET, which omits the field.
func memberRecord(withEmail bool) string {
	email := ""
	if withEmail {
		email = fmt.Sprintf(`"email":%q,`, updateMemberEmail)
	}
	return fmt.Sprintf(`{"uid":%q,"committee_uid":%q,%s"username":"ada","first_name":"Ada","last_name":"Lovelace","job_title":"Engineer","appointed_by":"Community","status":"Active","role":{"name":"Chair"},"voting":{"status":"Voting Rep"},"organization":{"id":"org-1","name":"Example Org","website":"https://example.test"}}`,
		updateMemberUID, updateMemberCommitteeUID, email)
}

// stubMemberUpdateService points the committee tools at a fake whose member
// GET returns the record (with or without an e-mail) and whose PUT validates
// the body under the given contract, answering 200 with the stored record
// merged with the body or 400 with the validation message.
func stubMemberUpdateService(t *testing.T, c memberContract, getReturnsEmail bool) *stubLFXAPI {
	t.Helper()
	api := setupCommitteeTest(t)
	path := fmt.Sprintf("/committees/%s/members/%s", updateMemberCommitteeUID, updateMemberUID)
	api.Handle(http.MethodGet, path, func(stubAPIRequest) stubAPIResponse {
		return stubAPIResponse{Status: http.StatusOK, Body: memberRecord(getReturnsEmail), Header: map[string]string{"ETag": updateMemberETag}}
	})
	api.Handle(http.MethodPut, path, func(r stubAPIRequest) stubAPIResponse {
		var body map[string]json.RawMessage
		if err := json.Unmarshal(r.Body, &body); err != nil {
			return stubAPIResponse{Status: http.StatusBadRequest, Body: fmt.Sprintf(`{"message":%q}`, err.Error())}
		}
		if err := validateMemberUpdateEmail(c, body); err != nil {
			return stubAPIResponse{Status: http.StatusBadRequest, Body: fmt.Sprintf(`{"message":%q}`, err.Error())}
		}
		var stored map[string]json.RawMessage
		_ = json.Unmarshal([]byte(memberRecord(true)), &stored)
		for k, v := range body {
			stored[k] = v
		}
		merged, _ := json.Marshal(stored)
		return stubAPIResponse{Status: http.StatusOK, Body: string(merged)}
	})
	return api
}

// updateMemberBody returns the single PUT the handler made, decoded.
func updateMemberBody(t *testing.T, api *stubLFXAPI) (stubAPIRequest, map[string]json.RawMessage) {
	t.Helper()
	var puts []stubAPIRequest
	for _, r := range api.Requests() {
		if r.Method == http.MethodPut {
			puts = append(puts, r)
		}
	}
	if len(puts) != 1 {
		t.Fatalf("want exactly one PUT, got %d (%+v)", len(puts), api.Requests())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(puts[0].Body, &body); err != nil {
		t.Fatalf("PUT body is not a JSON object: %v\n%s", err, puts[0].Body)
	}
	return puts[0], body
}

// callUpdateMember runs the update through the committee-named tool or its
// group-named twin with the same arguments.
func callUpdateMember(t *testing.T, asGroups bool, args UpdateCommitteeMemberArgs) *mcp.CallToolResult {
	t.Helper()
	var (
		res *mcp.CallToolResult
		err error
	)
	if asGroups {
		res, _, err = handleUpdateGroupMemberMode(fullViewCtx(), stubCallToolRequest(), UpdateGroupMemberArgs{
			GroupUID:        args.CommitteeUID,
			MemberUID:       args.MemberUID,
			Email:           args.Email,
			AppointedBy:     args.AppointedBy,
			Status:          args.Status,
			FirstName:       args.FirstName,
			LastName:        args.LastName,
			JobTitle:        args.JobTitle,
			LinkedinProfile: args.LinkedinProfile,
			Role:            args.Role,
			Voting:          args.Voting,
			Organization:    args.Organization,
		})
	} else {
		res, _, err = handleUpdateCommitteeMember(fullViewCtx(), stubCallToolRequest(), args)
	}
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return res
}

// The matrix: each contract, each tool name, the three e-mail cases. Every
// cell must succeed except the one neither contract accepts: no e-mail held
// against today's service, which requires one (and today's GET always
// returns it, so that cell does not occur in practice).
func TestUpdateCommitteeMember_EmailMatrixAcrossContracts(t *testing.T) {
	status := "Inactive"
	callerEmail := "new.address@example.test"
	emptyEmail := ""
	cases := []struct {
		name            string
		getReturnsEmail bool
		callerEmail     *string
		wantEmail       string // "" means the key must be absent
	}{
		{name: "caller supplies e-mail", getReturnsEmail: true, callerEmail: &callerEmail, wantEmail: callerEmail},
		{name: "caller supplies e-mail, GET has none", getReturnsEmail: false, callerEmail: &callerEmail, wantEmail: callerEmail},
		{name: "GET returned e-mail, caller gave none", getReturnsEmail: true, callerEmail: nil, wantEmail: updateMemberEmail},
		{name: "neither held", getReturnsEmail: false, callerEmail: nil, wantEmail: ""},
		// An empty e-mail from the caller is not an address the tool holds:
		// the key is dropped, as the encoder's comment states.
		{name: "caller supplies empty e-mail", getReturnsEmail: true, callerEmail: &emptyEmail, wantEmail: ""},
	}
	for _, c := range []memberContract{contractToday, contractAfter226} {
		for _, asGroups := range []bool{false, true} {
			for _, tc := range cases {
				name := fmt.Sprintf("%s/groups=%t/%s", c, asGroups, tc.name)
				t.Run(name, func(t *testing.T) {
					api := stubMemberUpdateService(t, c, tc.getReturnsEmail)
					res := callUpdateMember(t, asGroups, UpdateCommitteeMemberArgs{
						CommitteeUID: updateMemberCommitteeUID,
						MemberUID:    updateMemberUID,
						Email:        tc.callerEmail,
						Status:       &status,
					})

					put, body := updateMemberBody(t, api)
					assertExchangedAuth(t, put)
					if got := put.Header.Get("If-Match"); got != updateMemberETag {
						t.Errorf("If-Match: want %q, got %q", updateMemberETag, got)
					}
					if tc.wantEmail == "" {
						if raw, ok := body["email"]; ok {
							t.Errorf("email key must be absent when no e-mail is held; got %s", raw)
						}
					} else if got := string(body["email"]); got != fmt.Sprintf("%q", tc.wantEmail) {
						t.Errorf("email: want %q, got %s", tc.wantEmail, got)
					}
					// The merge carried the GET's other fields and the caller's change.
					for key, want := range map[string]string{
						"status":       `"Inactive"`,
						"appointed_by": `"Community"`,
						"first_name":   `"Ada"`,
						"last_name":    `"Lovelace"`,
						"job_title":    `"Engineer"`,
						"username":     `"ada"`,
					} {
						if got := string(body[key]); got != want {
							t.Errorf("%s: want %s, got %s", key, want, got)
						}
					}
					var org struct {
						ID string `json:"id"`
					}
					if err := json.Unmarshal(body["organization"], &org); err != nil || org.ID != "org-1" {
						t.Errorf("organization id must survive the merge, got %s", body["organization"])
					}

					wantError := tc.wantEmail == "" && c == contractToday
					if res.IsError != wantError {
						t.Fatalf("IsError: want %t, got %t: %s", wantError, res.IsError, resultText(t, res))
					}
				})
			}
		}
	}
}

// Against the post-#226 service a partial update (no e-mail anywhere) is the
// regression this change exists for; the PUT must be accepted. With the
// request-encoder hook removed, the pinned client sends "email":"" and this
// test fails with the service's 400.
func TestUpdateCommitteeMember_PartialUpdateSucceedsAfter226(t *testing.T) {
	title := "Principal Engineer"
	api := stubMemberUpdateService(t, contractAfter226, false)
	res := callUpdateMember(t, false, UpdateCommitteeMemberArgs{
		CommitteeUID: updateMemberCommitteeUID,
		MemberUID:    updateMemberUID,
		JobTitle:     &title,
	})
	if res.IsError {
		t.Fatalf("partial update rejected: %s", resultText(t, res))
	}
	_, body := updateMemberBody(t, api)
	if raw, ok := body["email"]; ok {
		t.Errorf("email key must be absent; got %s", raw)
	}
	if got := string(body["job_title"]); got != `"Principal Engineer"` {
		t.Errorf("job_title: got %s", got)
	}
}

// Against today's service nothing changes: the GET returns the e-mail and
// the PUT carries it.
func TestUpdateCommitteeMember_TodayStillSendsStoredEmail(t *testing.T) {
	title := "Principal Engineer"
	api := stubMemberUpdateService(t, contractToday, true)
	res := callUpdateMember(t, false, UpdateCommitteeMemberArgs{
		CommitteeUID: updateMemberCommitteeUID,
		MemberUID:    updateMemberUID,
		JobTitle:     &title,
	})
	if res.IsError {
		t.Fatalf("update rejected: %s", resultText(t, res))
	}
	_, body := updateMemberBody(t, api)
	if got := string(body["email"]); got != fmt.Sprintf("%q", updateMemberEmail) {
		t.Errorf("email: want the GET's address, got %s", got)
	}
}

// A GET failure still stops the update before any PUT.
func TestUpdateCommitteeMember_GetFailureMakesNoPut(t *testing.T) {
	api := setupCommitteeTest(t)
	path := fmt.Sprintf("/committees/%s/members/%s", updateMemberCommitteeUID, updateMemberUID)
	api.RespondStatus(path, http.StatusNotFound, `{"message":"not found"}`)
	status := "Inactive"
	res, _, err := handleUpdateCommitteeMember(context.Background(), stubCallToolRequest(), UpdateCommitteeMemberArgs{
		CommitteeUID: updateMemberCommitteeUID, MemberUID: updateMemberUID, Status: &status,
	})
	if err != nil || !res.IsError {
		t.Fatalf("want a tool error, got err=%v IsError=%v", err, res != nil && res.IsError)
	}
	if got := len(api.RequestsTo(path)); got != 1 {
		t.Fatalf("want exactly the GET, got %d requests to %s", got, path)
	}
	if api.Requests()[0].Method != http.MethodGet {
		t.Fatalf("the one request must be the GET, got %s", api.Requests()[0].Method)
	}
}

// A field this client's generated type does not know about must refuse the
// update rather than merge the GET's known fields over it and PUT, silently
// erasing the unknown one. See lfx-mcp#196.
func TestUpdateCommitteeMember_UnknownFieldOnGetRefusesWrite(t *testing.T) {
	api := setupCommitteeTest(t)
	path := fmt.Sprintf("/committees/%s/members/%s", updateMemberCommitteeUID, updateMemberUID)
	api.Handle(http.MethodGet, path, func(stubAPIRequest) stubAPIResponse {
		body := strings.TrimSuffix(memberRecord(true), "}") + `,"brand_new_field":"future value"}`
		return stubAPIResponse{Status: http.StatusOK, Body: body, Header: map[string]string{"ETag": updateMemberETag}}
	})
	api.Handle(http.MethodPut, path, func(stubAPIRequest) stubAPIResponse {
		t.Fatal("PUT must not happen when the preceding GET is refused")
		return stubAPIResponse{}
	})

	status := "Inactive"
	res, _, err := handleUpdateCommitteeMember(context.Background(), stubCallToolRequest(), UpdateCommitteeMemberArgs{
		CommitteeUID: updateMemberCommitteeUID, MemberUID: updateMemberUID, Status: &status,
	})
	if err != nil || !res.IsError {
		t.Fatalf("want a tool error, got err=%v IsError=%v", err, res != nil && res.IsError)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "brand_new_field") {
		t.Errorf("error should name the unknown field, got: %s", text)
	}
	if got := len(api.RequestsTo(path)); got != 1 {
		t.Fatalf("want exactly the GET, got %d requests to %s", got, path)
	}
}
