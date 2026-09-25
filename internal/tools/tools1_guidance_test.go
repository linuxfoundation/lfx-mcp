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

func TestGuidanceCommitteeCountsAndSources(t *testing.T) {
	text := strings.Join(strings.Fields(semanticLayerGuidance), " ")
	for _, want := range []string{
		"page to the end to read every row",
		"Count members with count_lfx_resources type=committee_member",
		"one committee parent=committee:<uid>",
		"one project tags_all project_uid:<uid>",
		"committee members carry no project parent: parent=project:<uid> counts 0",
		"one organisation tags_all organization_id:<SFID>",
		"A filter on a field the record lacks also counts 0",
		"filters_all takes data fields (organization.name), tags_all takes tags (organization_name:, organization_id:, committee_category:)",
		"Read the complete flag; an incomplete count is a lower bound",
		"count each candidate organisation with count_lfx_resources (type=committee_member, tags_all organization_id:<SFID>, plus committee_category:Board for board seats)",
		"seats visible to you in LFX v2",
		"An open LF-wide ranking over every organisation is query_lfx_lens as the last resort, labelled generated SQL over the warehouse copy of the v1 committee records",
		"active seats only (end date empty or in the future), LF staff and unaffiliated seats set aside",
		"never combine or reconcile the two",
		"This layer has no committee metric: committees appear only as slices on the meeting models",
		"meeting_and_occurrence_id__committee_name / meeting_and_occurrence_id__committee_type on occurrences",
		"primary_key__committee_name / primary_key__committee_type on attendance",
		"on occurrences: meeting_and_occurrence_id__committee_type, meeting_and_occurrence_id__committee_name",
		"this layer carries no committee seats",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("committee guidance missing %q", want)
		}
	}
	for _, gone := range []string{"this layer's committee and maintainer models", "the committee models in this layer"} {
		if strings.Contains(text, gone) {
			t.Errorf("committee guidance still claims %q", gone)
		}
	}
}

func TestGuidanceRepresentationWithoutOrganizationGrant(t *testing.T) {
	for name, guidance := range map[string]string{
		"semantic layer":   semanticLayerGuidance,
		"standard metrics": standardMetricsGuidance,
	} {
		t.Run(name, func(t *testing.T) {
			text := strings.Join(strings.Fields(guidance), " ")
			for _, want := range []string{
				"Without the organisation grant, get_org_committee_seats refuses",
				"contacts either",
				"search_members → get_membership_key_contacts",
				"search_committee_members or count_lfx_resources, both \"visible to you\"",
				"a refusal is the gate, never 'no seats'",
				"an empty side is what you can see, not an absence",
			} {
				if !strings.Contains(text, want) {
					t.Errorf("no-grant branch missing %q", want)
				}
			}
		})
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

func TestMemberSinceGuidance(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		wants      []string
	}{
		{
			"routing", semanticLayerGuidance,
			[]string{
				"How long an organisation has been a member / member since: search_members summary=true",
				"one row per organisation and project, its visible membership records read whole when complete=true",
				"a summary the tool reports incomplete is partial; re-read with the organisation's uid as the scope",
			},
		},
		{
			"standard metrics", standardMetricsGuidance,
			[]string{
				"Member since is the earliest start across the organisation's membership records on that project",
				"read whole by search_members summary=true when complete=true, with the current term, its tier and end date alongside",
				"b2b_org_uid to the organisation's own record from search_b2b_orgs",
				"Results cover the caller's visible records, as in LFX Self Serve",
				"memberships family of the standard metrics counts memberships in a window or on a date and never yields a first date",
				"A summary the tool reports incomplete is partial; re-read with the organisation's uid as the scope, never present it as whole",
				"Cite first_start as recorded",
				"terms list shows gaps, so disclose a lapse and return rather than presenting it as uninterrupted \"member since\"",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := strings.Join(strings.Fields(tc.text), " ")
			for _, want := range tc.wants {
				if !strings.Contains(text, want) {
					t.Errorf("member since guidance missing %q", want)
				}
			}
		})
	}
	if !strings.Contains(standardMetricsGuidance, "not an absence.\n\nMember since") {
		t.Error("member since guidance must follow the representation paragraph")
	}
}
