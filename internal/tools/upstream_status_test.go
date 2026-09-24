// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	committeeservice "github.com/linuxfoundation/lfx-v2-committee-service/gen/committee_service"
	mailinglist "github.com/linuxfoundation/lfx-v2-mailing-list-service/gen/mailing_list"
	meetingservice "github.com/linuxfoundation/lfx-v2-meeting-service/gen/meeting_service"
	memberservice "github.com/linuxfoundation/lfx-v2-member-service/gen/membership_service"
	projectservice "github.com/linuxfoundation/lfx-v2-project-service/api/project/v1/gen/project_service"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	goahttp "goa.design/goa/v3/http"
)

// testUID is a synthetic v2 UID for the committee and project calls.
const testUID = "5f3c1a2e-8b4d-4c6e-9a1f-2b3c4d5e6f70"

// upstreamCall is one LFX v2 service call made against the stub API.
type upstreamCall struct {
	path string
	call func(ctx context.Context, clients *lfxv2.Clients) error
}

var (
	getPastMeetingCall = upstreamCall{
		path: "/itx/past_meetings/abc",
		call: func(ctx context.Context, clients *lfxv2.Clients) error {
			_, err := clients.Meeting.GetItxPastMeeting(ctx, &meetingservice.GetItxPastMeetingPayload{PastMeetingID: "abc"})
			return err
		},
	}
	getCommitteeCall = upstreamCall{
		path: "/committees/" + testUID,
		call: func(ctx context.Context, clients *lfxv2.Clients) error {
			uid := testUID
			_, err := clients.Committee.GetCommitteeBase(ctx, &committeeservice.GetCommitteeBasePayload{UID: &uid})
			return err
		},
	}
	getProjectCall = upstreamCall{
		path: "/projects/" + testUID,
		call: func(ctx context.Context, clients *lfxv2.Clients) error {
			uid := testUID
			_, err := clients.Project.GetOneProjectBase(ctx, &projectservice.GetOneProjectBasePayload{UID: &uid})
			return err
		},
	}
)

// upstreamError makes c against a stub API that answers with status and body,
// and returns the error the Goa client reports.
func upstreamError(t *testing.T, c upstreamCall, status int, body string) error {
	t.Helper()
	api := newStubLFXAPI(t)
	api.RespondStatus(c.path, status, body)
	err := c.call(api.Clients.WithMCPToken(context.Background(), stubMCPToken), api.Clients)
	if err == nil {
		t.Fatal("expected the upstream call to fail")
	}
	return err
}

// TestFriendlyAPIError_UpstreamStatus pins the text friendlyAPIError gives
// each upstream status, for errors produced by the real Goa clients and, for
// clients without a declared status here, by their typed errors.
func TestFriendlyAPIError_UpstreamStatus(t *testing.T) {
	const op = "failed to get resource"
	accessDenied := "Failed to get resource: " + accessDeniedMessage
	unauthorized := "Failed to get resource: Unauthorized (HTTP 401)"

	cases := []struct {
		name string
		err  func(t *testing.T) error
		want string
	}{
		// 401: the status only, with or without a service message.
		{"401 declared without message", func(t *testing.T) error {
			return upstreamError(t, getPastMeetingCall, http.StatusUnauthorized, "")
		}, unauthorized},
		{"401 declared with message", func(t *testing.T) error {
			return upstreamError(t, getPastMeetingCall, http.StatusUnauthorized, `{"code":"401","message":"token expired"}`)
		}, unauthorized},
		{"401 undeclared without message", func(t *testing.T) error {
			return upstreamError(t, getProjectCall, http.StatusUnauthorized, "")
		}, unauthorized},
		{"401 undeclared with message", func(t *testing.T) error {
			return upstreamError(t, getCommitteeCall, http.StatusUnauthorized, `{"message":"token expired"}`)
		}, unauthorized},

		// 403: unchanged.
		{"403 declared without message", func(t *testing.T) error {
			return upstreamError(t, getPastMeetingCall, http.StatusForbidden, "")
		}, accessDenied},
		{"403 declared with message", func(t *testing.T) error {
			return upstreamError(t, getPastMeetingCall, http.StatusForbidden, `{"code":"403","message":"only organizers"}`)
		}, "Failed to get resource: Forbidden: only organizers"},
		{"403 undeclared with message", func(t *testing.T) error {
			return upstreamError(t, getProjectCall, http.StatusForbidden, `{"message":"no"}`)
		}, accessDenied},

		// 404: the access message, from every client.
		{"404 meeting NotFound", func(t *testing.T) error {
			return upstreamError(t, getPastMeetingCall, http.StatusNotFound, `{"code":"404","message":"past meeting not found"}`)
		}, accessDenied},
		{"404 committee NotFound", func(t *testing.T) error {
			return upstreamError(t, getCommitteeCall, http.StatusNotFound, `{"message":"committee not found"}`)
		}, accessDenied},
		{"404 project NotFound", func(t *testing.T) error {
			return upstreamError(t, getProjectCall, http.StatusNotFound, `{"code":"404","message":"project not found"}`)
		}, accessDenied},
		{"404 mailing list NotFound", func(*testing.T) error {
			return &mailinglist.NotFoundError{Message: "mailing list not found"}
		}, accessDenied},
		{"404 member NotFound", func(*testing.T) error {
			return memberservice.MakeNotFound(errors.New("membership not found"))
		}, accessDenied},
		{"404 query NotFound", func(*testing.T) error {
			return &querysvc.NotFoundError{Message: "resource not found"}
		}, accessDenied},
		{"404 undeclared", func(*testing.T) error {
			return goahttp.ErrInvalidResponse("project-service", "get-one-project-base", http.StatusNotFound, `{"message":"not found"}`)
		}, accessDenied},

		// Every other status: unchanged.
		{"500 declared", func(t *testing.T) error {
			return upstreamError(t, getPastMeetingCall, http.StatusInternalServerError, `{"code":"500","message":"boom"}`)
		}, "Failed to get resource: InternalServerError: boom"},
		{"502 undeclared", func(*testing.T) error {
			return goahttp.ErrInvalidResponse("project-service", "get-one-project-base", http.StatusBadGateway, "")
		}, "Failed to get resource: [project-service get-one-project-base]: invalid response code 502"},
		{"400 declared", func(*testing.T) error {
			return &meetingservice.BadRequestError{Message: "bad uid"}
		}, "Failed to get resource: BadRequest: bad uid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err(t)
			if got := friendlyAPIError(op, err); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
			// Wrapping by the caller does not change a status-mapped answer.
			if tc.want == accessDenied || tc.want == unauthorized {
				if got := friendlyAPIError(op, fmt.Errorf("outer: %w", err)); got != tc.want {
					t.Errorf("wrapped: got  %q\nwant %q", got, tc.want)
				}
			}
		})
	}
}

// errorLogRecords runs handler with a request logger that writes JSON to a
// buffer, and returns the ERROR records it wrote.
func errorLogRecords(t *testing.T, handler func(ctx context.Context) error) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("username", "test-user")
	if err := handler(WithLogger(context.Background(), logger)); err == nil {
		t.Fatal("expected a tool error")
	}
	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if rec["level"] == "ERROR" {
			records = append(records, rec)
		}
	}
	return records
}

// TestUnauthorized_LoggedWithRequestContext pins that a 401 reaches the
// server log as one ERROR record on the request's own logger, carrying the
// caller's identity and the upstream error text, including for the meeting
// service's typed 401, whose Error() is blank.
func TestUnauthorized_LoggedWithRequestContext(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(t *testing.T)
		call      func(ctx context.Context) error
		wantTool  string
		wantInLog string
	}{
		{
			name: "get_project undeclared 401",
			setup: func(t *testing.T) {
				setupProjectTest(t).RespondStatus("/projects/"+testUID, http.StatusUnauthorized, "")
			},
			call: func(ctx context.Context) error {
				_, _, err := handleGetProject(ctx, stubCallToolRequest(), GetProjectArgs{UID: testUID})
				return err
			},
			wantTool:  "Failed to get project: " + unauthorizedMessage,
			wantInLog: "invalid response code 401",
		},
		{
			name: "get_past_meeting typed 401 with a service message",
			setup: func(t *testing.T) {
				setupMeetingLookupTest(t).RespondStatus(pastMeetingPath, http.StatusUnauthorized, `{"code":"401","message":"token expired"}`)
			},
			call: func(ctx context.Context) error {
				_, _, err := handleGetPastMeeting(ctx, stubCallToolRequest(), GetPastMeetingArgs{UID: "past-1"})
				return err
			},
			wantTool:  "Failed to get past meeting: " + unauthorizedMessage,
			wantInLog: "Unauthorized: token expired",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			var toolErr error
			records := errorLogRecords(t, func(ctx context.Context) error {
				toolErr = tc.call(ctx)
				return toolErr
			})
			if got := toolErr.Error(); got != tc.wantTool {
				t.Errorf("tool error: got  %q\nwant %q", got, tc.wantTool)
			}
			if len(records) != 1 {
				t.Fatalf("expected one ERROR record, got %d: %v", len(records), records)
			}
			if records[0]["username"] != "test-user" {
				t.Errorf("expected the request's username on the record, got %v", records[0]["username"])
			}
			if text, _ := records[0]["error"].(string); !strings.Contains(text, tc.wantInLog) {
				t.Errorf("expected %q in the record's error, got %q", tc.wantInLog, text)
			}
		})
	}
}

// firstBlock returns the text of a tool result's first content block.
func firstBlock(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || res.IsError || len(res.Content) < 2 {
		t.Fatalf("expected a partial result with a warning block, got %+v", res)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

// Partial-result warnings describe an upstream failure the same way as
// friendlyAPIError, after their own prefix.
func TestPartialResultWarnings_UpstreamStatus(t *testing.T) {
	t.Run("get_past_meeting recording 401", func(t *testing.T) {
		api := setupMeetingLookupTest(t)
		api.Respond(pastMeetingPath, pastMeetingBody)
		api.RespondStatus(resourcesPath, http.StatusUnauthorized, "")
		api.Respond(resourcesPath, emptyResourcePage)
		res, _, err := handleGetPastMeeting(context.Background(), stubCallToolRequest(), GetPastMeetingArgs{UID: "past-1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got, want := firstBlock(t, res), "WARNING: past meeting recording unavailable - Unauthorized (HTTP 401)"; got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("get_past_meeting transcript 403", func(t *testing.T) {
		api := setupMeetingLookupTest(t)
		api.Respond(pastMeetingPath, pastMeetingBody)
		api.Respond(resourcesPath, singleResourcePage("v1_past_meeting_recording", "rec-1", `{"uid": "rec-1"}`))
		api.RespondStatus(resourcesPath, http.StatusForbidden, "")
		res, _, err := handleGetPastMeeting(context.Background(), stubCallToolRequest(), GetPastMeetingArgs{UID: "past-1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got, want := firstBlock(t, res), "WARNING: past meeting transcript unavailable - "+accessDeniedMessage; got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("get_project settings 404", func(t *testing.T) {
		api := setupProjectTest(t)
		api.Respond("/projects/"+testUID, `{"uid": "`+testUID+`"}`)
		api.RespondStatus("/projects/"+testUID+"/settings", http.StatusNotFound, `{"code":"404","message":"settings not found"}`)
		res, _, err := handleGetProject(context.Background(), stubCallToolRequest(), GetProjectArgs{UID: testUID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got, want := firstBlock(t, res), "WARNING: project settings unavailable - "+accessDeniedMessage; got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})
	t.Run("get_committee settings 401", func(t *testing.T) {
		api := setupCommitteeTest(t)
		api.Respond("/committees/"+testUID, `{"uid": "`+testUID+`"}`)
		api.RespondStatus("/committees/"+testUID+"/settings", http.StatusUnauthorized, "")
		res, _, err := handleGetCommittee(context.Background(), stubCallToolRequest(), GetCommitteeArgs{UID: testUID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got, want := firstBlock(t, res), "WARNING: committee settings unavailable - Unauthorized (HTTP 401)"; got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})
}

// TestErrorTextHandler pins that the server-side tool logger writes a Goa
// typed error with a blank Error() as its upstreamErrorText, whether it is
// passed on the record or bound with With, and leaves other errors as they are.
func TestErrorTextHandler(t *testing.T) {
	var buf bytes.Buffer
	ctx := WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)))
	logger := newToolLogger(ctx, nil)

	typed := fmt.Errorf("outer: %w", &meetingservice.NotFoundError{Message: "past meeting not found"})
	logger.With("bound", typed).ErrorContext(ctx, "typed", "error", typed)
	logger.ErrorContext(ctx, "plain", "error", errors.New("dial tcp: connection refused"))

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 records, got %d: %s", len(lines), buf.String())
	}
	var first, second map[string]any
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lines[1], &second); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"bound", "error"} {
		if got, want := first[key], "outer: NotFound: past meeting not found"; got != want {
			t.Errorf("%s: got %q, want %q", key, got, want)
		}
	}
	if got, want := second["error"], "dial tcp: connection refused"; got != want {
		t.Errorf("plain error: got %q, want %q", got, want)
	}
}
