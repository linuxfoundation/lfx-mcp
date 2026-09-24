// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// getPastMeetingText calls get_past_meeting against a stubbed upstream answer
// and returns the tool's error text. The handler reports an upstream failure
// as a returned error, which the SDK sends to the client as the text of an
// error result (see "Tool Error Responses" in AGENTS.md).
func getPastMeetingText(t *testing.T, status int, body string) string {
	t.Helper()
	api := setupMeetingLookupTest(t)
	api.RespondStatus("/itx/past_meetings/abc", status, body)

	res, _, err := handleGetPastMeeting(context.Background(), stubCallToolRequest(), GetPastMeetingArgs{UID: "abc"})
	if err == nil {
		t.Fatalf("expected the upstream failure as a returned error, got result %+v", res)
	}
	if res != nil {
		t.Fatalf("expected no result next to the error, got %+v", res)
	}
	return err.Error()
}

func TestGetPastMeeting_RefusalWithoutBodyShowsAccessMessage(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			got := getPastMeetingText(t, status, "")
			want := "Failed to get past meeting: " + accessDeniedMessage
			if got != want {
				t.Errorf("expected %q, got %q", want, got)
			}
		})
	}
}

func TestGetPastMeeting_RefusalWithoutCodeShowsAccessMessage(t *testing.T) {
	// The meeting service's 401/403 bodies require "code" as well as
	// "message"; without it Goa would report a validation error.
	for _, status := range []int{http.StatusForbidden, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			got := getPastMeetingText(t, status, `{"message":"denied by policy"}`)
			want := "Failed to get past meeting: " + accessDeniedMessage
			if got != want {
				t.Errorf("expected %q, got %q", want, got)
			}
		})
	}
}

func TestGetPastMeeting_ServiceAuthoredRefusalKept(t *testing.T) {
	got := getPastMeetingText(t, http.StatusForbidden, `{"code":"403","message":"only organizers"}`)
	want := "Failed to get past meeting: Forbidden: only organizers"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestGetPastMeeting_ServerErrorWithoutBodyUnchanged(t *testing.T) {
	got := getPastMeetingText(t, http.StatusInternalServerError, "")
	if !strings.Contains(got, "failed to decode response body") {
		t.Errorf("expected the decoding error to pass through, got %q", got)
	}
	if strings.Contains(got, accessDeniedMessage) {
		t.Errorf("a 500 must not show the access message, got %q", got)
	}
}
