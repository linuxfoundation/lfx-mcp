// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package lfxv2 provides client utilities for interacting with LFX v2 APIs, including OAuth2 token exchange.
package lfxv2

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	committeeservice "github.com/linuxfoundation/lfx-v2-committee-service/gen/committee_service"
	mailinglist "github.com/linuxfoundation/lfx-v2-mailing-list-service/gen/mailing_list"
	meetingservice "github.com/linuxfoundation/lfx-v2-meeting-service/gen/meeting_service"
	memberservice "github.com/linuxfoundation/lfx-v2-member-service/gen/membership_service"
	projectservice "github.com/linuxfoundation/lfx-v2-project-service/api/project/v1/gen/project_service"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	goahttp "goa.design/goa/v3/http"
)

// newRefusalTestClients starts a server that answers every request with the
// given status, Content-Type (omitted when empty) and body, and returns
// clients pointed at it.
func newRefusalTestClients(t *testing.T, status int, contentType, body string) *Clients {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	clients, err := NewClients(context.Background(), ClientConfig{APIDomain: srv.URL})
	if err != nil {
		t.Fatalf("NewClients: %v", err)
	}
	return clients
}

func getPastMeeting(clients *Clients) error {
	_, err := clients.Meeting.GetItxPastMeeting(context.Background(), &meetingservice.GetItxPastMeetingPayload{PastMeetingID: "x"})
	return err
}

func TestRefusalAwareDecoder_RefusalWithoutServiceMessage(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{"403 empty body", http.StatusForbidden, "", ""},
		{"401 empty body", http.StatusUnauthorized, "", ""},
		{"403 whitespace", http.StatusForbidden, "", " \n\t"},
		{"403 html", http.StatusForbidden, "text/html", "<html>Forbidden</html>"},
		{"403 json without message", http.StatusForbidden, "application/json", `{"error":"denied"}`},
		{"403 json with empty message", http.StatusForbidden, "application/json", `{"code":"403","message":""}`},
		{"403 json array", http.StatusForbidden, "application/json", `[]`},
		{"401 json with blank message", http.StatusUnauthorized, "application/json", `{"code":"401","message":"  "}`},
		// The meeting service's 401/403 bodies require "code" as well as "message".
		{"403 json message without code", http.StatusForbidden, "application/json", `{"message":"denied by policy"}`},
		{"401 json message without code", http.StatusUnauthorized, "application/json", `{"message":"denied by policy"}`},
		{"403 json with null code", http.StatusForbidden, "application/json", `{"code":null,"message":"denied by policy"}`},
		{"403 json with numeric code", http.StatusForbidden, "application/json", `{"code":403,"message":"denied by policy"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := getPastMeeting(newRefusalTestClients(t, tc.status, tc.contentType, tc.body))
			if !errors.Is(err, ErrAccessRefused) {
				t.Fatalf("expected ErrAccessRefused, got %v", err)
			}
			if want := fmt.Sprintf("(HTTP %d)", tc.status); !strings.Contains(err.Error(), want) {
				t.Errorf("expected error to carry %q, got %q", want, err.Error())
			}
		})
	}
}

func TestRefusalAwareDecoder_KeepsServiceAuthoredForbidden(t *testing.T) {
	err := getPastMeeting(newRefusalTestClients(t, http.StatusForbidden, "application/json", `{"code":"403","message":"only organizers"}`))
	if errors.Is(err, ErrAccessRefused) {
		t.Fatalf("service-authored refusal must not be ErrAccessRefused: %v", err)
	}
	var forbidden *meetingservice.ForbiddenError
	if !errors.As(err, &forbidden) {
		t.Fatalf("expected *meetingservice.ForbiddenError, got %T: %v", err, err)
	}
	if forbidden.Message != "only organizers" {
		t.Errorf("expected message %q, got %q", "only organizers", forbidden.Message)
	}
}

func TestRefusalAwareDecoder_KeepsServiceAuthoredUnauthorized(t *testing.T) {
	err := getPastMeeting(newRefusalTestClients(t, http.StatusUnauthorized, "application/json", `{"code":"401","message":"token expired"}`))
	if errors.Is(err, ErrAccessRefused) {
		t.Fatalf("service-authored refusal must not be ErrAccessRefused: %v", err)
	}
	var unauthorized *meetingservice.UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("expected *meetingservice.UnauthorizedError, got %T: %v", err, err)
	}
	if unauthorized.Message != "token expired" {
		t.Errorf("expected message %q, got %q", "token expired", unauthorized.Message)
	}
}

func TestRefusalAwareDecoder_CommitteeMessageOnlyForbiddenKept(t *testing.T) {
	// The committee service's 403 bodies require only "message".
	getBrief := func(clients *Clients) error {
		_, err := clients.Committee.GetCurrentWeeklyBrief(context.Background(), &committeeservice.GetCurrentWeeklyBriefPayload{UID: "x"})
		return err
	}
	for _, body := range []string{`{"message":"members only"}`, `{"code":403,"message":"members only"}`} {
		err := getBrief(newRefusalTestClients(t, http.StatusForbidden, "application/json", body))
		var forbidden *committeeservice.ForbiddenError
		if !errors.As(err, &forbidden) || forbidden.Message != "members only" {
			t.Errorf("body %s: expected *committeeservice.ForbiddenError with the service message, got %T: %v", body, err, err)
		}
	}
	if err := getBrief(newRefusalTestClients(t, http.StatusForbidden, "", "")); !errors.Is(err, ErrAccessRefused) {
		t.Errorf("expected ErrAccessRefused for an empty body, got %v", err)
	}
}

func TestRefusalAwareDecoder_OtherDeclaredStatusesUnchanged(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			err := getPastMeeting(newRefusalTestClients(t, status, "", ""))
			if errors.Is(err, ErrAccessRefused) {
				t.Fatalf("status %d must not be ErrAccessRefused: %v", status, err)
			}
			if err == nil || !strings.Contains(err.Error(), "failed to decode response body: EOF") {
				t.Errorf("expected the decoding error, got %v", err)
			}
		})
	}
}

func TestRefusalAwareDecoder_SuccessUnchanged(t *testing.T) {
	err := getPastMeeting(newRefusalTestClients(t, http.StatusOK, "application/json", `{}`))
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestRefusalAwareDecoder_UndeclaredStatusUnchanged(t *testing.T) {
	uid := "00000000-0000-0000-0000-000000000000"
	for _, status := range []int{http.StatusForbidden, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			clients := newRefusalTestClients(t, status, "", "")
			_, err := clients.Project.GetOneProjectBase(context.Background(), &projectservice.GetOneProjectBasePayload{UID: &uid})
			if errors.Is(err, ErrAccessRefused) {
				t.Fatalf("undeclared status %d must not be ErrAccessRefused: %v", status, err)
			}
			if want := fmt.Sprintf("invalid response code %d", status); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("expected error containing %q, got %v", want, err)
			}
		})
	}
}

func TestIsRefusalWithoutServiceMessage_UndeclaredStatus(t *testing.T) {
	uid := "00000000-0000-0000-0000-000000000000"
	cases := []struct {
		name        string
		status      int
		contentType string
		body        string
		want        bool
	}{
		{"401 empty body", http.StatusUnauthorized, "", "", true},
		{"403 empty body", http.StatusForbidden, "", "", true},
		{"401 html", http.StatusUnauthorized, "text/html", "<html>Unauthorized</html>", true},
		{"401 json without message", http.StatusUnauthorized, "application/json", `{"error":"denied"}`, true},
		{"401 json with message", http.StatusUnauthorized, "application/json", `{"code":"401","message":"token expired"}`, false},
		{"403 json with message", http.StatusForbidden, "application/json", `{"code":"403","message":"only organizers"}`, false},
		{"409 empty body", http.StatusConflict, "", "", false},
		{"502 empty body", http.StatusBadGateway, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clients := newRefusalTestClients(t, tc.status, tc.contentType, tc.body)
			_, err := clients.Project.GetOneProjectBase(context.Background(), &projectservice.GetOneProjectBasePayload{UID: &uid})
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, e := range []error{err, fmt.Errorf("outer: %w", err)} {
				if got := IsRefusalWithoutServiceMessage(e); got != tc.want {
					t.Errorf("IsRefusalWithoutServiceMessage(%q) = %v, want %v", e.Error(), got, tc.want)
				}
			}
		})
	}
}

func TestIsRefusalWithoutServiceMessage_DeclaredStatus(t *testing.T) {
	if !IsRefusalWithoutServiceMessage(getPastMeeting(newRefusalTestClients(t, http.StatusForbidden, "", ""))) {
		t.Error("expected a declared 403 without a body to be a refusal without a service message")
	}
	if IsRefusalWithoutServiceMessage(getPastMeeting(newRefusalTestClients(t, http.StatusForbidden, "application/json", `{"code":"403","message":"only organizers"}`))) {
		t.Error("a declared 403 with a service message must not match")
	}
	if IsRefusalWithoutServiceMessage(getPastMeeting(newRefusalTestClients(t, http.StatusNotFound, "", ""))) {
		t.Error("a declared 404 without a body must not match")
	}
	if IsRefusalWithoutServiceMessage(nil) || IsRefusalWithoutServiceMessage(errors.New("invalid response code 401")) {
		t.Error("nil and plain errors must not match")
	}
}

// TestUpstreamStatus_FromClients reads the status from what the Goa clients
// actually return: a refusal without a service message (declared status), the
// service's typed error (declared status with a valid body), and Goa's
// invalid_response error (undeclared status).
func TestUpstreamStatus_FromClients(t *testing.T) {
	uid := "00000000-0000-0000-0000-000000000000"
	getProject := func(clients *Clients) error {
		_, err := clients.Project.GetOneProjectBase(context.Background(), &projectservice.GetOneProjectBasePayload{UID: &uid})
		return err
	}
	cases := []struct {
		name        string
		call        func(*Clients) error
		status      int
		contentType string
		body        string
		want        int
	}{
		{"meeting 401 empty body", getPastMeeting, http.StatusUnauthorized, "", "", http.StatusUnauthorized},
		{"meeting 401 service message", getPastMeeting, http.StatusUnauthorized, "application/json", `{"code":"401","message":"token expired"}`, http.StatusUnauthorized},
		{"meeting 403 empty body", getPastMeeting, http.StatusForbidden, "", "", http.StatusForbidden},
		{"meeting 403 service message", getPastMeeting, http.StatusForbidden, "application/json", `{"code":"403","message":"only organizers"}`, http.StatusForbidden},
		{"meeting 404 service message", getPastMeeting, http.StatusNotFound, "application/json", `{"code":"404","message":"past meeting not found"}`, http.StatusNotFound},
		// A declared status whose body the client cannot decode carries no
		// status; see UpstreamStatus.
		{"meeting 404 empty body", getPastMeeting, http.StatusNotFound, "", "", 0},
		{"meeting 500 service message", getPastMeeting, http.StatusInternalServerError, "application/json", `{"code":"500","message":"boom"}`, 0},
		{"project 401 undeclared", getProject, http.StatusUnauthorized, "", "", http.StatusUnauthorized},
		{"project 401 undeclared with message", getProject, http.StatusUnauthorized, "application/json", `{"message":"token expired"}`, http.StatusUnauthorized},
		{"project 403 undeclared", getProject, http.StatusForbidden, "", "", http.StatusForbidden},
		{"project 404 service message", getProject, http.StatusNotFound, "application/json", `{"code":"404","message":"project not found"}`, http.StatusNotFound},
		{"project 409", getProject, http.StatusConflict, "", "", 0},
		{"project 502", getProject, http.StatusBadGateway, "", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(newRefusalTestClients(t, tc.status, tc.contentType, tc.body))
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, e := range []error{err, fmt.Errorf("outer: %w", err)} {
				if got := UpstreamStatus(e); got != tc.want {
					t.Errorf("UpstreamStatus(%T %q) = %d, want %d", err, e.Error(), got, tc.want)
				}
			}
		})
	}
}

// TestUpstreamStatus_TypedErrorNames pins the Goa error names of every LFX v2
// client this server uses: the typed errors for 401, 403 and 404, and a
// sample of the others, which carry no status.
func TestUpstreamStatus_TypedErrorNames(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"committee NotFound":    {&committeeservice.NotFoundError{Message: "x"}, http.StatusNotFound},
		"committee Forbidden":   {&committeeservice.ForbiddenError{Message: "x"}, http.StatusForbidden},
		"committee Conflict":    {&committeeservice.ConflictError{Message: "x"}, 0},
		"mailing list NotFound": {&mailinglist.NotFoundError{Message: "x"}, http.StatusNotFound},
		"meeting NotFound":      {&meetingservice.NotFoundError{Message: "x"}, http.StatusNotFound},
		"meeting Unauthorized":  {&meetingservice.UnauthorizedError{Message: "x"}, http.StatusUnauthorized},
		"meeting Forbidden":     {&meetingservice.ForbiddenError{Message: "x"}, http.StatusForbidden},
		"meeting BadRequest":    {&meetingservice.BadRequestError{Message: "x"}, 0},
		"member NotFound":       {memberservice.MakeNotFound(errors.New("x")), http.StatusNotFound},
		"member BadRequest":     {memberservice.MakeBadRequest(errors.New("x")), 0},
		"project NotFound":      {&projectservice.NotFoundError{Message: "x"}, http.StatusNotFound},
		"project Conflict":      {&projectservice.ConflictError{Message: "x"}, 0},
		"query NotFound":        {&querysvc.NotFoundError{Message: "x"}, http.StatusNotFound},
		"query InternalError":   {&querysvc.InternalServerError{Message: "x"}, 0},
		"undeclared 404":        {goahttp.ErrInvalidResponse("svc", "m", http.StatusNotFound, `{"message":"x"}`), http.StatusNotFound},
		"decoding error":        {goahttp.ErrDecodingError("svc", "m", errors.New("EOF")), 0},
		"plain 404 text":        {errors.New("invalid response code 404"), 0},
		"nil":                   {nil, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if got := UpstreamStatus(tc.err); got != tc.want {
				t.Errorf("UpstreamStatus = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestHasServiceMessage(t *testing.T) {
	cases := []struct {
		body  string
		extra []string
		want  bool
	}{
		{"", nil, false},
		{"  \n", nil, false},
		{"<html>Forbidden</html>", nil, false},
		{"[]", nil, false},
		{"null", nil, false},
		{`"message"`, nil, false},
		{`{"code":"403"}`, nil, false},
		{`{"message":""}`, nil, false},
		{`{"message":"   "}`, nil, false},
		{`{"message":42}`, nil, false},
		{`{"message":"only organizers"}`, nil, true},
		{`{"code":"403","message":"only organizers"}`, nil, true},
		{`{"message":"denied"}`, []string{"code"}, false},
		{`{"code":null,"message":"denied"}`, []string{"code"}, false},
		{`{"code":403,"message":"denied"}`, []string{"code"}, false},
		{`{"code":"403","message":""}`, []string{"code"}, false},
		{`{"code":"","message":"denied"}`, []string{"code"}, true},
		{`{"code":"403","message":"denied"}`, []string{"code"}, true},
	}
	for _, tc := range cases {
		if got := hasServiceMessage([]byte(tc.body), tc.extra...); got != tc.want {
			t.Errorf("hasServiceMessage(%q, %q) = %v, want %v", tc.body, tc.extra, got, tc.want)
		}
	}
}
