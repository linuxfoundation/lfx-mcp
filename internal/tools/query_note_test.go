// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"strings"
	"testing"
)

// TestAccessFilteredEmptyNote guards the empty-page disambiguation shared by
// every query-service-backed search tool. Indexed resource types are
// access-controlled and the query service silently withholds what the caller
// cannot see, so a bare empty result reads as "the record does not exist" -
// which sent agents off to wrong conclusions (observed live on b2b_org). The
// pagination token separates the two cases asymmetrically: it is built from
// the raw index page before access filtering and emitted only when that page
// was full, so empty-with-token PROVES withheld matches, while
// empty-without-token proves nothing (a partial final raw page can be
// entirely withheld without a token) and must only claim "nothing visible".
func TestAccessFilteredEmptyNote(t *testing.T) {
	t.Run("empty page with token reports a permissions gap", func(t *testing.T) {
		note := accessFilteredEmptyNote("organizations", 0, true)
		for _, want := range []string{
			"No organizations are visible",
			"do not have permission",
			"permissions gap, not missing data",
		} {
			if !strings.Contains(note, want) {
				t.Errorf("permissions note missing %q: %q", want, note)
			}
		}
	})

	t.Run("empty page without token claims only no visible matches", func(t *testing.T) {
		note := accessFilteredEmptyNote("meetings", 0, false)
		if !strings.Contains(note, "No meetings are visible") {
			t.Errorf("no-visible note missing plain explanation: %q", note)
		}
		if !strings.Contains(note, "permission to view") {
			t.Errorf("no-visible note missing access caveat: %q", note)
		}
		// A partial final raw page emits no token even when fully withheld,
		// so this branch must never assert that nothing matched.
		if strings.Contains(note, "matched") {
			t.Errorf("no-visible note must not claim nothing matched: %q", note)
		}
		if !strings.Contains(note, "not proof of absence") {
			t.Errorf("no-visible note must warn against reading absence: %q", note)
		}
		if strings.Contains(note, "permissions gap") {
			t.Errorf("no-visible note must not claim a proven permissions gap: %q", note)
		}
	})

	t.Run("non-empty page gets no note", func(t *testing.T) {
		if note := accessFilteredEmptyNote("projects", 3, true); note != "" {
			t.Errorf("expected no note for non-empty page, got %q", note)
		}
	})
}
