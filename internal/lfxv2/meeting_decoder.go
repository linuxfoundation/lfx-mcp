// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package lfxv2

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	goahttp "goa.design/goa/v3/http"
)

// meetingRecordRanges are the integer fields the generated meeting client
// range-checks on a meeting record. The service stores some of them as 0 when
// the setting is off (auto_email_reminder_time on a meeting without
// reminders), which the client would reject.
var meetingRecordRanges = map[string][2]int64{
	"auto_email_reminder_time": {120, 1440},
	"early_join_time_minutes":  {10, 60},
}

// meetingRecordDecoder wraps next so that successful responses for a single
// meeting record (create, get and update under /itx/meetings) or a meeting
// registrant are cleaned before the generated client validates them:
//
//   - empty strings are dropped at any depth. The client format-checks
//     optional fields such as public_link, password and the timestamps, and
//     the service returns "" for them where there is no value (a private
//     meeting has no public link), which fails the URI/UUID/date checks.
//   - the range-checked integers above are dropped when out of range.
//   - a recurrence with a type outside 1-3 (one-time meetings) is dropped.
//
// Dropping these turns them into absent optional fields, which is what they
// mean. Without it, a create that succeeded upstream is reported as a
// failure, and a private meeting cannot be read back for an update.
func meetingRecordDecoder(next func(*http.Response) goahttp.Decoder) func(*http.Response) goahttp.Decoder {
	return func(resp *http.Response) goahttp.Decoder {
		if resp.StatusCode < 200 || resp.StatusCode > 299 || resp.Request == nil || !(isMeetingRecordPath(resp.Request.URL.Path) || isMeetingRegistrantPath(resp.Request.URL.Path)) {
			return next(resp)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errDecoder{err: err}
		}
		resp.Body = io.NopCloser(bytes.NewReader(cleanMeetingRecord(body)))
		return next(resp)
	}
}

// isMeetingRecordPath reports whether path is /itx/meetings or
// /itx/meetings/{id}, not a sub-resource such as registrants.
func isMeetingRecordPath(path string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSuffix(path, "/"), "/itx/meetings")
	if !ok {
		return false
	}
	return rest == "" || (strings.HasPrefix(rest, "/") && !strings.Contains(rest[1:], "/"))
}

// isMeetingRegistrantPath reports whether path is
// /itx/meetings/{id}/registrants or /itx/meetings/{id}/registrants/{rid}.
func isMeetingRegistrantPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) >= 4 && len(parts) <= 5 && parts[0] == "itx" && parts[1] == "meetings" && parts[2] != "" && parts[3] == "registrants" && (len(parts) == 4 || parts[4] != "")
}

// cleanMeetingRecord applies the rules described on meetingRecordDecoder.
// A body that is not a JSON object is returned unchanged.
func cleanMeetingRecord(body []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var record map[string]any
	if err := dec.Decode(&record); err != nil {
		return body
	}
	for name, bounds := range meetingRecordRanges {
		if n, ok := record[name].(json.Number); ok {
			if v, err := n.Int64(); err != nil || v < bounds[0] || v > bounds[1] {
				delete(record, name)
			}
		}
	}
	if rec, ok := record["recurrence"].(map[string]any); ok {
		t, _ := rec["type"].(json.Number)
		if v, err := t.Int64(); err != nil || v < 1 || v > 3 {
			delete(record, "recurrence")
		}
	}
	dropEmptyStrings(record)
	out, err := json.Marshal(record)
	if err != nil {
		return body
	}
	return out
}

// dropEmptyStrings removes keys whose value is "" from v and every object
// nested inside it.
func dropEmptyStrings(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if s, ok := val.(string); ok && s == "" {
				delete(t, k)
				continue
			}
			dropEmptyStrings(val)
		}
	case []any:
		for _, item := range t {
			dropEmptyStrings(item)
		}
	}
}
