// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The guidance tools replaced the explore tool's help action: tool results
// carry no byte budget, and tool descriptions/results are the only MCP
// surfaces that reach the model in every client. These tests pin the content
// the same way TestDoctrineHelp pinned the help action — if a token
// disappears, a failure pattern that produced wrong answers in the evals
// loses its recipe.

// TestGuidanceDescriptions_ShortAndFunctional keeps the guidance tools'
// descriptions small: their job is to be found and read, not to compete with
// the query tools for routing budget.
func TestGuidanceDescriptions_ShortAndFunctional(t *testing.T) {
	for name, desc := range map[string]string{
		"read_lfx_semantic_layer_guidance":   semanticLayerGuidanceDescription,
		"read_lfx_deck_building_guidance":    deckBuildingGuidanceDescription,
		"read_lfx_standard_metrics_guidance": standardMetricsGuidanceDescription,
	} {
		if got := len(desc); got > 400 {
			t.Errorf("%s description is %d bytes; guidance descriptions stay short (<=400)", name, got)
		}
		if !strings.Contains(desc, "Read") {
			t.Errorf("%s description should instruct when to read it", name)
		}
	}
	// The semantic layer guidance is shared by explore and query; its
	// description names the tools it covers so one read is understood to be
	// enough.
	for _, want := range []string{"explore_lfx_semantic_layer", "query_lfx_semantic_layer", "query_lfx_lens", "once per session"} {
		if !strings.Contains(semanticLayerGuidanceDescription, want) {
			t.Errorf("semantic layer guidance description missing %q", want)
		}
	}
}

// TestGuidanceTools_RegisterReadOnly checks both tools register under their
// gateable names with read-only annotations.
func TestGuidanceTools_RegisterReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register func(*mcp.Server)
	}{
		{"read_lfx_semantic_layer_guidance", RegisterSemanticLayerGuidance},
		{"read_lfx_deck_building_guidance", RegisterDeckBuildingGuidance},
		{"read_lfx_standard_metrics_guidance", RegisterStandardMetricsGuidance},
	} {
		tool := listRegisteredTool(t, tc.name, tc.register)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s must carry ReadOnlyHint", tc.name)
		}
	}
}

// TestGuidanceHandlers_ReturnTheDocuments checks the handlers hand back the
// embedded documents verbatim.
func TestGuidanceHandlers_ReturnTheDocuments(t *testing.T) {
	res, _, err := handleSemanticLayerGuidance(context.Background(), nil, GuidanceArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resultText(t, res); got != semanticLayerGuidance || len(got) < 5000 {
		t.Errorf("semantic layer guidance result is not the embedded document (len %d)", len(got))
	}
	res, _, err = handleDeckBuildingGuidance(context.Background(), nil, GuidanceArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resultText(t, res); got != deckBuildingGuidance || len(got) < 2000 {
		t.Errorf("deck building guidance result is not the embedded document (len %d)", len(got))
	}
	res, _, err = handleStandardMetricsGuidance(context.Background(), nil, GuidanceArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resultText(t, res); got != standardMetricsGuidance || len(got) < 1500 {
		t.Errorf("standard metric guidance result is not the embedded document (len %d)", len(got))
	}
}

// TestSemanticLayerGuidanceContent pins the doctrine: every recipe verified
// against the live layer during the August 2026 evals, plus the failure
// modes the 2026-08-31 post-deploy eval rounds surfaced (rollup direction,
// person-grain rankings, events/training account dimensions, LF-wide scope,
// one-hop standard metric recipe filters).
func TestSemanticLayerGuidanceContent(t *testing.T) {
	text := semanticLayerGuidance
	for _, want := range []string{
		// windows and membership parity
		"trailing 12 months",
		"asset_id__end_date",
		"current_membership_count",
		"future-dated",
		// scoping and hierarchy
		"project__foundation_slug",
		"spine_hierarchy_level = 2",
		"risc-v-international/riscv",
		"There is no separate project parameter",
		"conformed lens",
		"asset_id__project_slug",
		"carry the conformed project entity",
		"event_id__project_name",
		"maintainer_key__project_slug",
		"returns ZERO",
		"health_metric_key__foundation_slug",
		"counts only, never sums",
		"__segment_slug",
		"PCC-style foundation rollups",
		`"Direct children of X"`,
		// LF-wide scope ambiguity (round-2 eval divergence)
		"'tlf' slug is the umbrella",
		// syntax
		"metric_time__year",
		"yyyy-mm-dd",
		"ceiling 500",
		// value discovery
		"'Asia Pacific'",
		"Viet Nam",
		"asset_id__billing_country",
		"zero rows",
		// bots
		"member_is_bot",
		"bot_activities",
		"roughly 1.8x",
		// org shares and headcounts
		"org-ATTRIBUTED",
		"Individual - No Account",
		"2-4x",
		// name discovery and rollups
		"International Business Machines Corporation",
		"Red Hat LLC",
		"account__account_rollup_name",
		"subsidiaries INTO parents",
		// tiers, health, value
		"Premier Membership",
		"Critical <20, Unsteady",
		"total_software_value",
		"COCOMO",
		// populations, maintainers, regions, person grain
		"total_contributors_with_collaboration",
		"2000-01-01 sentinel",
		"maintainer_key__is_lf_project",
		"organization_lf_region",
		"activity_project_id__member_display_name",
		"not identity keys",
		// governance and meetings routing
		"search_committees",
		"search_committee_members",
		"Never infer a roster",
		"search_past_meetings",
		"not answerable with governed data today",
		"disclose there is no canonical way",
		// events/training/sponsorships account entities and tiers
		"account__account_name",
		"NULL bucket",
		"sponsorship__sponsorship_tier_type",
		"'package_tier'",
		// governed org-attribution filter, and the account-attributed superset
		"activity_project_id__is_org_contribution = true",
		"SUPERSET of is_org_contribution",
		// account vs rollup doctrine, in full, once
		"is its parent",
		"returns only what rolls up to THAT subsidiary",
		"Red Hat LLC is itself a rollup parent",
		"Always filter the TOP\nparent",
		"value-searching 'IBM' finds",
		"their OWN rollup",
		"headcounts may NOT",
		// the fuzzy did-you-mean trap
		"get_dimensions(search=) is authoritative",
		// as-of maintainers
		"As of date D: total_maintainers where",
		"one as-of reading per period",
		// events/training/sponsorships have no standard metric, so the
		// ad-hoc shape of each cut lives here in full
		"ACCEPTED registrations",
		"enrollment records only",
		"scope them with project__foundation_slug",
		"leaf project's own slug returns NOTHING",
		"registration_id__event_start_date",
		"and metric_time, the sign-up\ndate",
		"event_id__event_name",
		"enrollment_id__course_name",
		"enrollment_id__product_type",
		"every org-scoped figure here is a\nfloor",
		// other surfaces are never a reconciliation target
		"is code contributions, bots excluded",
		"Do NOT reconcile figures against other\n  dashboards or pages",
		"PCC-style reporting is the\n  reconciliation surface",
		// worked examples stay live-verified
		"## Worked examples (verified live)",
		// resolve-first: a guessed slug or account name is a silent wrong answer
		"ALWAYS resolve names first",
		"has not come back from them",
		// the rollup is one hop, and activities are parent-resolved first
		"ONE HOP",
		"not\ntheir own acquisitions",
		"already resolved to the parent account FOR THAT PROJECT",
		// where each domain attaches
		"ATTACHMENT LEVELS",
		"legitimately\n  near-empty",
		// windows cut on Pacific days, layer-wide
		"Day boundaries are US-Pacific",
		// standard metric calls
		"STANDARD METRIC CALLS take uniform parameters",
		"this means INDIVIDUALS by contribution volume — run it, do not ask",
		"There is no\nfree filter on a standard metric",
		"default combined",
		"default excluded",
		"contributors, contributions, contributions_by_org",
		"maintainers, maintainers_by_org",
		"DEFAULTS are the plain reading",
		"applied block",
		"maintainers_by_project, maintainer_roster",
		"contribution metrics cover a named node's tree at ANY depth",
		"only to its DIRECT\nchildren",
		"The maintainer metrics have no\nparent-company lens",
		"query_lfx_standard_metrics",
		"PREFER it over the",
		"read_lfx_deck_building_guidance",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("semantic layer guidance missing %q", want)
		}
	}
}

// TestDeckBuildingGuidanceContent pins the deck workflow: standard metric recipes
// first, the pair-grain reading contract, rollup direction, and the honest
// out-of-scope list.
func TestDeckBuildingGuidanceContent(t *testing.T) {
	text := deckBuildingGuidance
	for _, want := range []string{
		"read_lfx_semantic_layer_guidance",
		"query_lfx_standard_metrics",
		"members_and_dues_by_org",
		"contributors_by_org",
		"maintainers_by_project",
		"maintainer_roster",
		"There is no free filter on a standard metric",
		"an order_by on the result columns",
		"read_lfx_standard_metrics_guidance",
		// resolve-first, the two switches, and the one-hop disclosure
		"ALWAYS resolve names first",
		"never put a guessed slug",
		"subprojects\n  (excluded|separate|combined, default combined)",
		"subsidiaries\n  (excluded|separate|combined, default excluded)",
		"DEFAULTS are the headline reading",
		"the headline AND the breakdown",
		"applied block",
		"→ contributors, contributions, maintainers",

		"ONE HOP",
		"separate lists the parts",
		"combined returns the single folded row",
		"never from summing rows",
		"\"Top contributors\" unqualified → individuals by contribution volume",
		"The maintainer metrics have no parent-company lens",
		"subprojects=combined folds every\n  project column of the result",
		// depth, and the accounts a rollup never folds in
		"at any depth\n  within its foundation",
		"only to\n  its direct children",
		"STRAY SAME-COMPANY ACCOUNTS",
		// events, training and sponsorships have no advertised standard metric
		"no advertised\n  standard metric: compose them with explore + query",
		"Do not reconcile figures against other dashboards or pages",
		"offer the breakdown and another window",
		"PCC-style reporting as the reconciliation surface",
		"Meeting attendance by company",
		"total_sponsorship_revenue",
		"'package_tier'",
		"definitional delta",
		"Be honest about what each figure represents",
		"label the\nresult as generated SQL",
		"maintainers to activity at person grain",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("deck building guidance missing %q", want)
		}
	}
}

// TestStandardMetricsGuidanceContent pins the contract that only lives here: resolve
// names before calling, the two switches and their opposite defaults, the
// inventory with what each standard metric answers and the columns it returns, the
// account/parent_org reading with its one-hop limit, where each domain
// attaches, and the rejections.
func TestStandardMetricsGuidanceContent(t *testing.T) {
	text := standardMetricsGuidance
	for _, want := range []string{
		// resolve names first, always
		"Resolve names first — ALWAYS",
		"search_projects",
		"search_b2b_orgs",
		"has\nnot come back from them",
		"zero rows, not an error",
		// the two switches and their opposite defaults
		"subprojects: excluded | separate | combined, DEFAULT combined",
		"subsidiaries: excluded | separate | combined, DEFAULT excluded",
		"\"TOP CONTRIBUTORS\" with nothing more said means INDIVIDUALS",
		"without\n   asking which reading was meant",
		"## The switches, row by row",
		"| combined (default) | X plus everything under it, any depth | folded into ONE row",
		"| separate | X plus everything under it, any depth | as the metric groups them, one row each: the breakdown",
		"| excluded | X's own bucket only, nothing under it |",
		"| excluded (default) | the Y account only |",
		"| combined | Y plus every account rolling up to it | folded into ONE row",
		"MOST QUESTIONS WANT BOTH",
		"Never derive one from the other",
		"## Defaults and the applied block",
		"trailing 365 days",
		"`defaulted`, the list of parameters the lens chose",
		"offer what a reader most often wants next",
		"Do\nnot compare the figure with a number from another dashboard",
		// scope totals
		"| contributors | Distinct code contributors over the scope, ONE figure",
		"| contributions | Code contribution volume over the scope, ONE figure",
		"| maintainers | Active maintainers over the scope, ONE figure",
		"a subsidiary is a different company",
		"a subproject is part of its project",
		"folded into ONE row",
		"one row per parent organization",
		// the shape rule
		"A FLOW metric takes since/until",
		"a SNAPSHOT metric takes as_of",
		"folds every project column of the result away",
		"one call per period",
		// how to call
		"There is no free-form filter",
		"labelled as such",
		"1..500",
		"order_by takes the result columns as they come back",
		"WHAT YOU CAN ORDER BY",
		"minus any column\nthe call folds away",
		"a\ntop-N per project needs subprojects=separate first",
		// inventory: the ten names, what each answers, its result columns
		"members_and_dues_by_org",
		"membership_tiers",
		"new_members_by_year",
		"membership_churn_by_year",
		"contributions_by_org",
		"contributors_by_org",
		"contributors_by_project",
		"maintainers_by_org",
		"maintainers_by_project",
		"maintainer_roster",
		"account, parent_org, current_membership_count, current_membership_revenue",
		"tier, current_membership_count",
		"year, new_membership_count",
		"year, churned_membership_count",
		"account, parent_org, code_contribution_activities",
		"project, project_name, total_contributors",
		"| account, active_maintainers |",
		"foundation, project, project_name, active_maintainers",
		"project, project_name, maintainer, account, role, active_maintainers",
		// the caveats that change how a figure is read
		"Tier literals differ per foundation",
		"rejoins counts again",
		"the day AFTER the term ended",
		"never sum the rows",
		"LF-project filter is built into every maintainer metric",
		// organizations: account, parent_org, the one hop, the stray accounts
		"names its organization column `account`",
		"as Salesforce spells it",
		"`parent_org`, the company",
		"the maintainer metrics\nare the exception",
		"ONE HOP",
		"not their own acquisitions",
		"ask query_lfx_lens, which walks the account hierarchy recursively",
		"STRAY SAME-COMPANY ACCOUNTS",
		"never sum their rows into a parent figure",
		// projects: how deep each family reaches, and that reaching the whole
		// subtree is coverage, never permission to add the rows up
		"AT ANY DEPTH",
		"grandchildren included",
		"covers the whole subtree with nothing missing",
		"never sum to a subtree total",
		"reaches its direct children only",
		"Memberships attach at FOUNDATION level",
		// reading results
		"never an organization",
		"different grains",
		"rows are NAMES, not identities",
		"`maintainer` is a personal name",
		"already resolved to the parent account for that project",
		"never mix the two in one",
		"as of the last warehouse build",
		"US-Pacific day boundaries",
		"compiled_sql",
		// errors
		"unknown metric: the message lists the valid names",
		"subsidiaries other than excluded on a metric with no parent-company lens",
		"an order_by field that is not one of the result columns",
		"read_lfx_deck_building_guidance",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("standard metric guidance missing %q", want)
		}
	}
}

// TestGuidanceNamesNoOtherSurface pins a product decision: the guidance
// describes exactly what a figure covers and offers the breakdown or another
// window, and never sends the model to compare with, or explain away, a
// number on another LFX surface by name.
func TestGuidanceNamesNoOtherSurface(t *testing.T) {
	for name, text := range map[string]string{
		"semantic layer guidance":  semanticLayerGuidance,
		"deck building guidance":   deckBuildingGuidance,
		"standard metric guidance": standardMetricsGuidance,
		"tool description":         standardMetricsDescription,
	} {
		if strings.Contains(text, "Insights") {
			t.Errorf("%s names Insights; describe what the figure covers instead", name)
		}
	}
}
