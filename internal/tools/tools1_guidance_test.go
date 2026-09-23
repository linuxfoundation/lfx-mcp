// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"strings"
	"testing"
)

func TestTools1GuidanceRoutesDirectoryProjectCounts(t *testing.T) {
	_, routing, found := strings.Cut(semanticLayerGuidance, "## Routing\n")
	if !found {
		t.Fatal("guidance is missing its routing section")
	}
	routing, _, found = strings.Cut(routing, "\n## Protocol")
	if !found {
		t.Fatal("guidance is missing its protocol section after routing")
	}
	const want = "How many projects a foundation or parent has: this layer's project metrics count the authoritative project directory; search_projects and count_lfx_resources count only projects onboarded into LFX v2 and can be lower — use the tools to resolve names and slugs, the layer for the number."
	if !strings.Contains(strings.Join(strings.Fields(routing), " "), want) {
		t.Error("routing must distinguish directory project counts from onboarded v2 project lookups")
	}
}

// TestTools1GuidanceDistinguishesCountsFromPagedListings pins the tool
// contracts in recipe 12: the listing route is not a count/completeness API.
func TestTools1GuidanceDistinguishesCountsFromPagedListings(t *testing.T) {
	text := strings.Join(strings.Fields(semanticLayerGuidance), " ")
	for _, want := range []string{
		"TWO ROUTES, TWO DEFINITIONS.",
		"search_past_meetings returns a paged listing.",
		"count_lfx_resources provides meeting counts",
		"search_past_meeting_participants with count_only=true provides participant counts",
		"both count what the caller's identity may see",
		"and say whether the count is complete",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("recipe 12 missing %q", want)
		}
	}
	if strings.Contains(text, "count_lfx_resources, search_past_meetings and search_past_meeting_participants count") {
		t.Error("recipe 12 must not promise counts or completeness from search_past_meetings")
	}
}
