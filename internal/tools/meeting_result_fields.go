// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

// meetingResultDeniedFields lists the keys removed from meeting records
// before they are returned to the caller. Joining a meeting stays in LFX
// Self Serve, which issues join links through its own endpoint, so the
// direct call link and call passcode add nothing to a read-only tool
// result. The recording password is documented upstream as no longer used.
// Matching is exact and case-sensitive: the join-page password fields
// (`password` on meetings, `meeting_password` on past meetings) are kept on
// purpose because LFX Self Serve shows them to signed-in viewers.
var meetingResultDeniedFields = map[string]struct{}{
	"join_url":           {},
	"passcode":           {},
	"recording_password": {},
	"host_key":           {},
}

// trimMeetingResultFields removes the denied keys from a decoded
// query-service record at any depth. It walks maps and slices in place and
// returns the (possibly mutated) value; any other type is returned untouched.
func trimMeetingResultFields(data any) any {
	switch v := data.(type) {
	case map[string]any:
		for key, value := range v {
			if _, denied := meetingResultDeniedFields[key]; denied {
				delete(v, key)
				continue
			}
			v[key] = trimMeetingResultFields(value)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = trimMeetingResultFields(item)
		}
		return v
	default:
		return data
	}
}
