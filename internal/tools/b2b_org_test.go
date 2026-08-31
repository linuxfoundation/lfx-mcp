// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"strings"
	"testing"
)

// TestB2bOrgEmptyResultNote guards the empty-page disambiguation for
// search_b2b_orgs. B2B org records are access-controlled and the query
// service silently withholds what the caller cannot see, so a bare empty
// result reads as "the organization does not exist" - which sent agents off
// to wrong conclusions. The pagination token separates the two cases: it is
// built from the raw index page before access filtering, so empty-with-token
// means permissions, empty-without-token means no match.
func TestB2bOrgEmptyResultNote(t *testing.T) {
	t.Run("empty page with token reports a permissions gap", func(t *testing.T) {
		note := b2bOrgEmptyResultNote(0, true)
		for _, want := range []string{
			"do not have permission",
			"permissions gap, not missing data",
		} {
			if !strings.Contains(note, want) {
				t.Errorf("permissions note missing %q: %q", want, note)
			}
		}
	})

	t.Run("empty page without token reports no match with the access caveat", func(t *testing.T) {
		note := b2bOrgEmptyResultNote(0, false)
		if !strings.Contains(note, "No organizations matched") {
			t.Errorf("no-match note missing plain explanation: %q", note)
		}
		if !strings.Contains(note, "access-controlled") {
			t.Errorf("no-match note missing access caveat: %q", note)
		}
		if strings.Contains(note, "permissions gap") {
			t.Errorf("no-match note must not claim a permissions gap: %q", note)
		}
	})

	t.Run("non-empty page gets no note", func(t *testing.T) {
		if note := b2bOrgEmptyResultNote(3, true); note != "" {
			t.Errorf("expected no note for non-empty page, got %q", note)
		}
	})
}
