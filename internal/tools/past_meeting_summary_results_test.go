// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type summaryAccessCall struct {
	token    string
	requests []string
}

type fakeSummaryAccessChecker struct {
	calls   []summaryAccessCall
	results map[string]bool
	err     error
}

func (f *fakeSummaryAccessChecker) CheckAccess(_ context.Context, token string, requests []string) (map[string]bool, error) {
	f.calls = append(f.calls, summaryAccessCall{token: token, requests: append([]string(nil), requests...)})
	return f.results, f.err
}

func summaryResultDoc(t *testing.T, id string, data map[string]any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"type": pastMeetingSummaryResourceType, "id": id, "data": data})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func summaryResultData(id string, requiresApproval, approved bool) map[string]any {
	return map[string]any{
		"meeting_and_occurrence_id": id,
		"requires_approval":         requiresApproval,
		"approved":                  approved,
		"content":                   "Summary text",
		"edited_content":            "Edited summary text",
		"summary_title":             "Weekly sync",
		"zoom_webhook_event":        `{"summary_content":"Provider summary text"}`,
	}
}

func callSummaryResultHandler(ctx context.Context, t *testing.T, tool string) *mcp.CallToolResult {
	t.Helper()
	var res *mcp.CallToolResult
	var err error
	if tool == "search" {
		res, _, err = handleSearchPastMeetingSummaries(ctx, stubCallToolRequest(), SearchPastMeetingSummariesArgs{PageSize: 5})
	} else {
		res, _, err = handleGetPastMeetingSummary(ctx, stubCallToolRequest(), GetPastMeetingSummaryArgs{UID: "summary-1"})
	}
	if err != nil || res == nil || res.IsError {
		t.Fatalf("summary tool failed: err=%v, result=%+v", err, res)
	}
	return res
}

func TestPastMeetingSummaryResults_ApprovalStates(t *testing.T) {
	for _, tool := range []string{"search", "get"} {
		t.Run(tool, func(t *testing.T) {
			for _, tc := range []struct {
				name         string
				requires     any
				approved     any
				meetingID    any
				checkerMode  string
				granted      bool
				missingGrant bool
				checkError   bool
				withheld     bool
				wantCall     bool
			}{
				{name: "approved", requires: true, approved: true, meetingID: "meeting-1"},
				{name: "no approval needed", requires: false, approved: false, meetingID: "meeting-1"},
				{name: "missing requires approval", approved: false, meetingID: "meeting-1"},
				{name: "non boolean requires approval", requires: "true", approved: false, meetingID: "meeting-1"},
				{name: "pending organizer", requires: true, approved: false, meetingID: "meeting-1", granted: true, wantCall: true},
				{name: "pending non organizer", requires: true, approved: false, meetingID: "meeting-1", withheld: true, wantCall: true},
				{name: "missing approved", requires: true, meetingID: "meeting-1", withheld: true, wantCall: true},
				{name: "non boolean approved", requires: true, approved: "true", meetingID: "meeting-1", withheld: true, wantCall: true},
				{name: "check error with partial grant", requires: true, approved: false, meetingID: "meeting-1", granted: true, checkError: true, withheld: true, wantCall: true},
				{name: "missing grant", requires: true, approved: false, meetingID: "meeting-1", missingGrant: true, withheld: true, wantCall: true},
				{name: "nil checker", requires: true, approved: false, meetingID: "meeting-1", checkerMode: "nil", withheld: true},
				{name: "typed nil checker", requires: true, approved: false, meetingID: "meeting-1", checkerMode: "typed nil", withheld: true},
				{name: "typed nil upstream checker", requires: true, approved: false, meetingID: "meeting-1", checkerMode: "upstream typed nil", withheld: true},
				{name: "missing meeting id", requires: true, approved: false, granted: true, withheld: true},
				{name: "empty meeting id", requires: true, approved: false, meetingID: "", granted: true, withheld: true},
				{name: "non string meeting id", requires: true, approved: false, meetingID: 1.0, granted: true, withheld: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					api := setupMeetingLookupTest(t)
					checker := &fakeSummaryAccessChecker{results: map[string]bool{"v1_past_meeting:meeting-1#organizer": tc.granted}}
					if tc.missingGrant {
						checker.results = nil
					}
					if tc.checkError {
						checker.err = errors.New("synthetic upstream failure: meeting-1 " + stubExchangedToken)
					}
					switch tc.checkerMode {
					case "nil":
						meetingConfig.AccessChecker = nil
					case "typed nil":
						meetingConfig.AccessChecker = (*fakeSummaryAccessChecker)(nil)
					case "upstream typed nil":
						meetingConfig.AccessChecker = (*lfxv2.AccessCheckClient)(nil)
					default:
						meetingConfig.AccessChecker = checker
					}

					data := summaryResultData("meeting-1", true, false)
					for key, value := range map[string]any{"requires_approval": tc.requires, "approved": tc.approved, "meeting_and_occurrence_id": tc.meetingID} {
						if value == nil {
							delete(data, key)
						} else {
							data[key] = value
						}
					}
					api.Respond(resourcesPath, page([]string{summaryResultDoc(t, "summary-1", data)}, "next-page"))
					var logs bytes.Buffer
					ctx := WithLogger(context.Background(), slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
					res := callSummaryResultHandler(ctx, t, tool)
					out := resultJSON(t, res)
					resource := out
					if tool == "search" {
						resources := out["resources"].([]any)
						if len(resources) != 1 || out["page_token"] != "next-page" {
							t.Fatalf("record and pagination must remain: %v", out)
						}
						if len(res.Content) != 2 || !strings.HasPrefix(res.Content[0].(*mcp.TextContent).Text, "WARNING: some results on this page") {
							t.Errorf("existing page warning must remain: %v", res.Content)
						}
						resource = resources[0].(map[string]any)
					}
					if resource["ID"] != "summary-1" || resource["Type"] != pastMeetingSummaryResourceType {
						t.Errorf("resource envelope changed: %v", resource)
					}
					delete(data, "zoom_webhook_event")
					if tc.withheld {
						delete(data, "content")
						delete(data, "edited_content")
						data["content_withheld"] = "awaiting organizer approval"
					}
					if got := resource["Data"]; !reflect.DeepEqual(got, data) {
						t.Errorf("result Data: got %v, want %v", got, data)
					}

					if tc.wantCall {
						want := []summaryAccessCall{{token: stubExchangedToken, requests: []string{"v1_past_meeting:meeting-1#organizer"}}}
						if !reflect.DeepEqual(checker.calls, want) {
							t.Errorf("access calls: got %v, want %v", checker.calls, want)
						}
					} else if len(checker.calls) != 0 {
						t.Errorf("unexpected access calls: %v", checker.calls)
					}
					assertExchangedAuth(t, api.LastRequest())
					if tc.checkError {
						if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "failed to check summary organizer access") {
							t.Errorf("expected tool warning: %s", logs.String())
						}
						for _, omitted := range []string{"meeting-1", stubExchangedToken, stubMCPToken, "synthetic upstream failure"} {
							if strings.Contains(logs.String(), omitted) {
								t.Errorf("warning includes upstream details: %s", logs.String())
							}
						}
					}
				})
			}
		})
	}
}

func TestPastMeetingSummaryResults_BatchesDistinctMeetings(t *testing.T) {
	api := setupMeetingLookupTest(t)
	checker := &fakeSummaryAccessChecker{results: map[string]bool{
		"v1_past_meeting:meeting-1#organizer": false,
		"v1_past_meeting:meeting-2#organizer": true,
	}}
	meetingConfig.AccessChecker = checker
	api.Respond(resourcesPath, page([]string{
		summaryResultDoc(t, "summary-1", summaryResultData("meeting-1", true, false)),
		summaryResultDoc(t, "summary-2", summaryResultData("meeting-2", true, false)),
		summaryResultDoc(t, "summary-3", summaryResultData("meeting-1", true, false)),
	}, "next-page"))

	res := callSummaryResultHandler(context.Background(), t, "search")
	want := []summaryAccessCall{{token: stubExchangedToken, requests: []string{
		"v1_past_meeting:meeting-1#organizer", "v1_past_meeting:meeting-2#organizer",
	}}}
	if !reflect.DeepEqual(checker.calls, want) {
		t.Errorf("expected one batched call with distinct requests: got %v, want %v", checker.calls, want)
	}
	out := resultJSON(t, res)
	resources := out["resources"].([]any)
	if len(resources) != 3 || out["page_token"] != "next-page" {
		t.Fatalf("page changed: %v", out)
	}
	for i, resource := range resources {
		data := resource.(map[string]any)["Data"].(map[string]any)
		if _, has := data["zoom_webhook_event"]; has {
			t.Error("provider payload must be absent from every record")
		}
		if _, has := data["content"]; has != (i == 1) {
			t.Errorf("access result applied to wrong record: %v", data)
		}
		if _, has := data["edited_content"]; has != (i == 1) {
			t.Errorf("edited text access result applied to wrong record: %v", data)
		}
		if i != 1 && data["content_withheld"] != "awaiting organizer approval" {
			t.Errorf("pending record missing withholding note: %v", data)
		}
	}
}

func TestPastMeetingSummaryResults_PageWithoutPending(t *testing.T) {
	api := setupMeetingLookupTest(t)
	checker := &fakeSummaryAccessChecker{}
	meetingConfig.AccessChecker = checker
	api.Respond(resourcesPath, page([]string{
		summaryResultDoc(t, "summary-1", summaryResultData("meeting-1", true, true)),
		summaryResultDoc(t, "summary-2", summaryResultData("meeting-2", false, false)),
	}, ""))
	res := callSummaryResultHandler(context.Background(), t, "search")
	if len(checker.calls) != 0 {
		t.Errorf("page without pending records must not check access: %v", checker.calls)
	}
	resources := resultJSON(t, res)["resources"].([]any)
	if len(resources) != 2 {
		t.Fatalf("records removed: %v", resources)
	}
	for _, resource := range resources {
		data := resource.(map[string]any)["Data"].(map[string]any)
		if _, has := data["zoom_webhook_event"]; has || data["content"] != "Summary text" {
			t.Errorf("unexpected result: %v", data)
		}
	}
}

func TestPastMeetingSummaryResults_TokenFailureWithholds(t *testing.T) {
	api := setupMeetingLookupTest(t)
	checker := &fakeSummaryAccessChecker{}
	cfg := &MeetingConfig{Clients: api.Clients, AccessChecker: checker}
	data := summaryResultData("meeting-1", true, false)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	// Missing MCP-token context exercises token failure without an upstream call.
	cfg.preparePastMeetingSummaryResults(context.Background(), logger, []*querysvc.Resource{{Data: data}})
	if _, has := data["content"]; has {
		t.Error("content must be withheld after token failure")
	}
	if _, has := data["edited_content"]; has {
		t.Error("edited content must be withheld after token failure")
	}
	if _, has := data["zoom_webhook_event"]; has {
		t.Error("provider payload must be removed after token failure")
	}
	if data["content_withheld"] != "awaiting organizer approval" || len(checker.calls) != 0 {
		t.Errorf("unexpected failure handling: data=%v calls=%v", data, checker.calls)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("expected warning: %s", logs.String())
	}
}

func TestPastMeetingSummaryResults_ParticipantGetterUnchanged(t *testing.T) {
	api := setupMeetingLookupTest(t)
	checker := &fakeSummaryAccessChecker{}
	meetingConfig.AccessChecker = checker
	data := summaryResultData("meeting-1", true, false)
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	api.Respond(resourcesPath, singleResourcePage(pastMeetingParticipantResourceType, "participant-1", string(body)))
	res, _, err := handleGetPastMeetingParticipant(context.Background(), stubCallToolRequest(), GetPastMeetingParticipantArgs{UID: "participant-1"})
	if err != nil || res == nil || res.IsError {
		t.Fatalf("participant getter failed: %v, %+v", err, res)
	}
	if got := resultJSON(t, res)["Data"]; !reflect.DeepEqual(got, data) || len(checker.calls) != 0 {
		t.Errorf("participant data must remain untouched: data=%v, calls=%v", got, checker.calls)
	}
}
