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
		"search_past_meeting_participants with count_only=true provides participant RECORD counts — not people and not attendances",
		"one person can hold more than one record at one occurrence",
		"its listing's people field is the de-duplicated people",
		"Both count tools count what the caller's identity may see",
		"and say whether the count is complete",
		"An unscoped long-window count can time out: scope by project or committee and read month windows",
		"On participant records, an unknown date_field or date_field=start_time returns a silent 0",
		"created_at is the record's creation, not the meeting's",
		"For a meeting-date window use search_past_meeting_participants with a project or committee and date_from/date_to",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("recipe 12 missing %q", want)
		}
	}
	if strings.Contains(text, "count_lfx_resources, search_past_meetings and search_past_meeting_participants count") {
		t.Error("recipe 12 must not promise counts or completeness from search_past_meetings")
	}
}

func TestGuidanceMeetingAccountCoverage(t *testing.T) {
	text := strings.Join(strings.Fields(semanticLayerGuidance), " ")
	for _, want := range []string{
		"Speakers carry no account entity; meeting attendance does",
		"ORGANIZATIONS (attendance model only): the account entity",
		"account__top_parent_name for the whole company at any depth",
		"account__account_name for one account (recipe 6)",
		"primary_key__account_name, the spelling as stored",
		"account__account_name NULL (no account, or a stored account that does not resolve)",
		"account__is_placeholder_account = true",
		"report both, and treat a company figure as a floor",
		"The occurrences model carries no account: distinct meetings a company's people attended has no named metric — query_lfx_lens, labelled generated SQL",
		"company (attendance only)",
		"never reconcile one against the other",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("meeting guidance missing %q", want)
		}
	}
	for _, gone := range []string{
		"Speakers and meeting attendance carry no account entity",
		"there is no account entity, so no rollup",
		"count_only=true provides participant counts",
	} {
		if strings.Contains(text, gone) {
			t.Errorf("meeting guidance still claims %q", gone)
		}
	}
}
