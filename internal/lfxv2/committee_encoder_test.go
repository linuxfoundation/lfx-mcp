// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	committeeservice "github.com/linuxfoundation/lfx-v2-committee-service/gen/committee_service"
	committeehttpclient "github.com/linuxfoundation/lfx-v2-committee-service/gen/http/committee_service/client"
)

// encodeUpdateMemberBody runs the generated update-committee-member request
// encoder with committeeRequestEncoder and returns the serialized body keys.
func encodeUpdateMemberBody(t *testing.T, p *committeeservice.UpdateCommitteeMemberPayload) map[string]json.RawMessage {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, "http://committee.test/committees/c/members/m", nil)
	if err != nil {
		t.Fatal(err)
	}
	encode := committeehttpclient.EncodeUpdateCommitteeMemberRequest(committeeRequestEncoder)
	if err := encode(req, p); err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("body is not a JSON object: %v\n%s", err, raw)
	}
	return fields
}

func strPtr(s string) *string { return &s }

func TestCommitteeRequestEncoder_UpdateMemberSendsEmailWhenHeld(t *testing.T) {
	fields := encodeUpdateMemberBody(t, &committeeservice.UpdateCommitteeMemberPayload{
		Version:     "1",
		UID:         "c",
		MemberUID:   "m",
		Email:       "member@example.test",
		AppointedBy: "Community",
		Status:      "Active",
	})
	if got := string(fields["email"]); got != `"member@example.test"` {
		t.Errorf("email: want %q, got %q", `"member@example.test"`, got)
	}
}

func TestCommitteeRequestEncoder_UpdateMemberOmitsEmptyEmail(t *testing.T) {
	ifMatch := "W/\"7\""
	fields := encodeUpdateMemberBody(t, &committeeservice.UpdateCommitteeMemberPayload{
		Version:     "1",
		IfMatch:     &ifMatch,
		UID:         "c",
		MemberUID:   "m",
		AppointedBy: "Community",
		Status:      "Active",
		FirstName:   strPtr("Ada"),
		JobTitle:    strPtr("Engineer"),
		Role: &struct {
			Name      string
			StartDate *string
			EndDate   *string
		}{Name: "Chair"},
		Voting: &struct {
			Status    string
			StartDate *string
			EndDate   *string
		}{Status: "Voting Rep"},
		Organization: &struct {
			ID      *string
			Name    *string
			Website *string
		}{ID: strPtr("org-1"), Name: strPtr("Example Org")},
	})
	if raw, ok := fields["email"]; ok {
		t.Errorf("email key must be absent when the payload holds no e-mail; got %s", raw)
	}
	// The other fields reach the wire as goa would send them.
	want := map[string]string{
		"appointed_by": `"Community"`,
		"status":       `"Active"`,
		"first_name":   `"Ada"`,
		"job_title":    `"Engineer"`,
	}
	for key, val := range want {
		if got := string(fields[key]); got != val {
			t.Errorf("%s: want %s, got %s", key, val, got)
		}
	}
	var role struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(fields["role"], &role); err != nil || role.Name != "Chair" {
		t.Errorf("role: want name Chair, got %s (%v)", fields["role"], err)
	}
	var voting struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(fields["voting"], &voting); err != nil || voting.Status != "Voting Rep" {
		t.Errorf("voting: want status \"Voting Rep\", got %s (%v)", fields["voting"], err)
	}
	var org struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(fields["organization"], &org); err != nil || org.ID != "org-1" || org.Name != "Example Org" {
		t.Errorf("organization: want id org-1 and name, got %s (%v)", fields["organization"], err)
	}
	// Optional fields the payload left nil stay absent, as goa's omitempty
	// tags make them.
	for _, key := range []string{"username", "last_name", "linkedin_profile"} {
		if raw, ok := fields[key]; ok {
			t.Errorf("%s: want absent, got %s", key, raw)
		}
	}
}

func TestCommitteeRequestEncoder_UpdateMemberKeepsHeaders(t *testing.T) {
	ifMatch := "W/\"7\""
	req, err := http.NewRequest(http.MethodPut, "http://committee.test/committees/c/members/m", nil)
	if err != nil {
		t.Fatal(err)
	}
	encode := committeehttpclient.EncodeUpdateCommitteeMemberRequest(committeeRequestEncoder)
	if err := encode(req, &committeeservice.UpdateCommitteeMemberPayload{
		Version: "1", IfMatch: &ifMatch, UID: "c", MemberUID: "m", AppointedBy: "Community", Status: "Active",
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if got := req.Header.Get("If-Match"); got != ifMatch {
		t.Errorf("If-Match: want %q, got %q", ifMatch, got)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type: want application/json, got %q", got)
	}
	if got := req.URL.Query().Get("v"); got != "1" {
		t.Errorf("query v: want 1, got %q", got)
	}
	// GetBody is what lets the transport replay the body on a retry; the
	// wrapped goa encoder sets it and the hook must not lose it.
	if req.GetBody == nil {
		t.Fatal("GetBody must be set")
	}
	replay, err := req.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(replay)
	if len(raw) == 0 {
		t.Error("GetBody replay is empty")
	}
}

// Other committee bodies are untouched: create-committee-member also carries
// a required e-mail string, and it must be serialized as goa serializes it.
func TestCommitteeRequestEncoder_OtherBodiesPassThrough(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://committee.test/committees/c/members", nil)
	if err != nil {
		t.Fatal(err)
	}
	encode := committeehttpclient.EncodeCreateCommitteeMemberRequest(committeeRequestEncoder)
	if err := encode(req, &committeeservice.CreateCommitteeMemberPayload{
		Version: "1", UID: "c", AppointedBy: "Community", Status: "Active",
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw, _ := io.ReadAll(req.Body)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("body: %v", err)
	}
	if got := string(fields["email"]); got != `""` {
		t.Errorf("create body must keep goa's serialization of an empty e-mail (%q), got %q", `""`, got)
	}
}
