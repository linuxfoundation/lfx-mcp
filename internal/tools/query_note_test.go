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
// pagination token separates the two cases: it is built from the raw index
// page before access filtering, so empty-with-token means permissions,
// empty-without-token means no match on this page.
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

	t.Run("empty page without token reports no match with the access caveat", func(t *testing.T) {
		note := accessFilteredEmptyNote("meetings", 0, false)
		if !strings.Contains(note, "No meetings matched") {
			t.Errorf("no-match note missing plain explanation: %q", note)
		}
		if !strings.Contains(note, "permission to view") {
			t.Errorf("no-match note missing access caveat: %q", note)
		}
		if strings.Contains(note, "permissions gap") {
			t.Errorf("no-match note must not claim a permissions gap: %q", note)
		}
	})

	t.Run("non-empty page gets no note", func(t *testing.T) {
		if note := accessFilteredEmptyNote("projects", 3, true); note != "" {
			t.Errorf("expected no note for non-empty page, got %q", note)
		}
	})
}
