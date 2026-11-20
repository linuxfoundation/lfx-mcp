// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"encoding/json"
	"testing"
)

func TestIsMeetingRecordPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/itx/meetings":                 true,
		"/itx/meetings/":                true,
		"/itx/meetings/91550996118":     true,
		"/itx/meetings/91550996118/":    true,
		"/itx/meetings/1/registrants":   false,
		"/itx/meetings/1/registrants/2": false,
		"/itx/past_meetings/1":          false,
		"/itx/meetingsx":                false,
		"/committees/1":                 false,
	} {
		if got := isMeetingRecordPath(path); got != want {
			t.Errorf("isMeetingRecordPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestCleanMeetingRecord(t *testing.T) {
	in := `{"id":"1","public_link":"","password":"","duration":30,
	  "auto_email_reminder_time":0,"early_join_time_minutes":10,
	  "recurrence":{"type":0},"restricted":false,
	  "owner":{"username":"u","email":""},
	  "occurrences":[{"start_time":"","status":"available"}]}`
	var got map[string]any
	if err := json.Unmarshal(cleanMeetingRecord([]byte(in)), &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"public_link", "password", "auto_email_reminder_time", "recurrence"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s should be dropped, got %v", k, got[k])
		}
	}
	for k, want := range map[string]any{"id": "1", "duration": float64(30), "early_join_time_minutes": float64(10), "restricted": false} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v (kept)", k, got[k], want)
		}
	}
	if _, ok := got["owner"].(map[string]any)["email"]; ok {
		t.Error("nested empty owner.email should be dropped")
	}
	occ := got["occurrences"].([]any)[0].(map[string]any)
	if _, ok := occ["start_time"]; ok || occ["status"] != "available" {
		t.Errorf("occurrence not cleaned correctly: %v", occ)
	}
}

func TestCleanMeetingRecord_KeepsValidRecurrenceAndReminder(t *testing.T) {
	in := `{"auto_email_reminder_time":120,"recurrence":{"type":2,"weekly_days":"3"}}`
	var got map[string]any
	_ = json.Unmarshal(cleanMeetingRecord([]byte(in)), &got)
	if got["auto_email_reminder_time"] != float64(120) {
		t.Errorf("in-range reminder must be kept, got %v", got)
	}
	if rec, _ := got["recurrence"].(map[string]any); rec == nil || rec["weekly_days"] != "3" {
		t.Errorf("valid recurrence must be kept, got %v", got)
	}
}

func TestCleanMeetingRecord_NonObjectUnchanged(t *testing.T) {
	for _, in := range []string{`[]`, `not json`, `"x"`} {
		if got := string(cleanMeetingRecord([]byte(in))); got != in {
			t.Errorf("cleanMeetingRecord(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestIsMeetingRegistrantPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/itx/meetings/1/registrants":        true,
		"/itx/meetings/1/registrants/r1":     true,
		"/itx/meetings/1/registrants/r1/ics": false,
		"/itx/meetings/1/registrants/self":   true,
		"/itx/meetings/1":                    false,
		"/itx/past_meetings/1/registrants":   false,
	} {
		if got := isMeetingRegistrantPath(path); got != want {
			t.Errorf("isMeetingRegistrantPath(%q) = %v, want %v", path, got, want)
		}
	}
}
