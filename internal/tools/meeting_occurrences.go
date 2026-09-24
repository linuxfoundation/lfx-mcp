// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"encoding/json"
	"time"
)

// meetingSearchNow is the clock search_meetings uses to decide which
// occurrences have ended. Tests replace it to pin the request time.
var meetingSearchNow = time.Now

// upcomingOccurrenceLimit is how many upcoming occurrences a meeting search
// result keeps per meeting when the request sets no start_time range.
const upcomingOccurrenceLimit = 5

// meetingEndBuffer is how long after its scheduled end an occurrence still
// counts as not ended. It matches LFX Self Serve (isOccurrencePast and
// hasMeetingEnded in packages/shared/src/utils/meeting.utils.ts) and the
// meeting service's occurrence calculator.
const meetingEndBuffer = 40 * time.Minute

// occurrenceNote is the warnings line search_meetings adds when it shortened
// at least one meeting's occurrence list.
const occurrenceNote = "Occurrence lists in these meetings keep only occurrences that have not ended: those starting within date_from/date_to when the range filters a start time, otherwise the next upcoming ones. occurrences_omitted counts the rest per meeting; get_meeting returns the full list."

// occurrenceWindow is the start_time range a search requested. A nil bound is
// open.
type occurrenceWindow struct {
	from, to *time.Time
}

// set reports whether the window has at least one bound.
func (w occurrenceWindow) set() bool {
	return w.from != nil || w.to != nil
}

// contains reports whether t falls inside the window, bounds inclusive.
func (w occurrenceWindow) contains(t time.Time) bool {
	if w.from != nil && t.Before(*w.from) {
		return false
	}
	if w.to != nil && t.After(*w.to) {
		return false
	}
	return true
}

// occurrenceWindowFor returns the occurrence window for a search. It applies
// only when the search filters on a start time: start_time (the default
// date_field) or occurrences.start_time, which matches a meeting when any of
// its occurrences starts in the range. Either way the occurrences are fitted
// to the range the caller asked about. The
// bounds are parsed the way the query service parses them: RFC 3339, or a
// date-only value meaning 00:00:00Z for date_from and 23:59:59Z for date_to.
// A bound that does not parse is treated as absent.
func occurrenceWindowFor(args SearchMeetingsArgs) occurrenceWindow {
	if args.DateFrom == "" && args.DateTo == "" {
		return occurrenceWindow{}
	}
	switch args.DateField {
	case "", "start_time", "occurrences.start_time":
	default:
		return occurrenceWindow{}
	}
	return occurrenceWindow{
		from: parseOccurrenceBound(args.DateFrom, false),
		to:   parseOccurrenceBound(args.DateTo, true),
	}
}

// parseOccurrenceBound parses one date bound; see occurrenceWindowFor.
func parseOccurrenceBound(s string, isEnd bool) *time.Time {
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil
	}
	if isEnd {
		d = time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, time.UTC)
	}
	return &d
}

// occurrenceStart returns an occurrence's start_time, and false when it is
// missing or does not parse as RFC 3339.
func occurrenceStart(occ map[string]any) (time.Time, bool) {
	s, ok := occ["start_time"].(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// occurrenceDuration returns an occurrence's duration, falling back to the
// meeting's duration and then to zero. Durations are in minutes.
func occurrenceDuration(occ, meeting map[string]any) time.Duration {
	for _, v := range []any{occ["duration"], meeting["duration"]} {
		if m, ok := minutes(v); ok {
			return time.Duration(m * float64(time.Minute))
		}
	}
	return 0
}

// minutes reads a JSON number as a float.
func minutes(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// occurrenceEnded reports whether an occurrence has ended at now: its start
// plus its duration plus meetingEndBuffer has passed.
func occurrenceEnded(start time.Time, duration time.Duration, now time.Time) bool {
	return now.After(start.Add(duration + meetingEndBuffer))
}

// fitMeetingOccurrences shortens each meeting's occurrence list to the
// occurrences that fit the search, and returns how many it left out across
// the page. Occurrences that have ended are left out. With a window set it
// keeps the occurrences starting inside it; otherwise it keeps the first
// upcomingOccurrenceLimit remaining occurrences. Kept occurrences are the
// original values, in index order. An entry whose start cannot be read is
// kept and never counted as left out.
//
// A meeting whose list was shortened gets the kept list and an
// occurrences_omitted count; a meeting with nothing left out, or with no
// occurrence list, is not modified.
func fitMeetingOccurrences(resources []searchResource, w occurrenceWindow, now time.Time) int {
	total := 0
	for _, r := range resources {
		occs, ok := r.Data["occurrences"].([]any)
		if !ok || len(occs) == 0 {
			continue
		}
		kept := make([]any, 0, len(occs))
		omitted := 0
		for _, o := range occs {
			occ, isMap := o.(map[string]any)
			start, hasStart := time.Time{}, false
			if isMap {
				start, hasStart = occurrenceStart(occ)
			}
			switch {
			case !hasStart:
				kept = append(kept, o)
			case occurrenceEnded(start, occurrenceDuration(occ, r.Data), now):
				omitted++
			case w.set() && !w.contains(start):
				omitted++
			case !w.set() && len(kept) >= upcomingOccurrenceLimit:
				omitted++
			default:
				kept = append(kept, o)
			}
		}
		if omitted > 0 {
			r.Data["occurrences"] = kept
			r.Data["occurrences_omitted"] = omitted
			total += omitted
		}
	}
	return total
}
