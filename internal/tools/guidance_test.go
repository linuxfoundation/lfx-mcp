// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"regexp"
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
		"Membership\n  counts as of a past date or by year",
		"are standard metrics, not lens questions",
		"no metric, dimension or\ncolumn names, keys, SQL or tool names",
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
		"omitted = EVERY row",
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
		"attendance\nRECORDS, not unique people",
		"'Individual - No Account'",
		"there is no account entity, so no rollup",
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
		// events/training/sponsorships are standard metrics now; the ad-hoc
		// shape of each cut stays here for the slices they lack
		"event_registrations,\nevent_sponsorships, speakers, training_enrollments and certifications cover",
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
		// the layer's own reach is one hop / one level, and it says so, with
		// the standard metrics as the any-depth route
		"REACH — how deep this layer's own dimensions go",
		"ONE HOP",
		"not their own acquisitions",
		"for the whole company at any depth use the standard\nmetric",
		"already resolved to the parent account FOR THAT PROJECT",
		// where each domain attaches
		"ATTACHMENT LEVELS",
		"legitimately\n  near-empty",
		// windows cut on Pacific days, layer-wide
		"Day boundaries are US-Pacific",
		// standard metric calls
		"STANDARD METRIC CALLS take uniform parameters",
		"this means INDIVIDUALS by contribution volume — run it, do not ask",
		"16. SOCIAL LISTENING. The standard metrics social_mentions (by total,\nproject, network, sentiment) and social_reach (by total, project) cover the\ncommon readings — prefer them",
		"Compose here only for a slice they lack",
		"stored as 'Twitter', not 'X'",
		"no filter means ALL of LF",
		"17. WHAT GOES IN THE ANSWER",
		"There is no free filter on a standard metric",
		"default combined",
		"default excluded",
		"start_date,\nend_date, period (day|week|month|quarter|year)",
		"there is no\nsince, until or as_of",
		"DEFAULTS are the plain reading",
		"applied block",
		"a WINDOW family counts between start_date and end_date",
		"an AT-DATE family (memberships, maintainers,\nproject_health, software_value) reports the state on end_date",
		"no lens call needed",
		"Every date is a UTC calendar day",
		"separate and combined cover a named node's tree and a company's subsidiaries\nat ANY depth",
		"maintainer_contributions (by=project or by=org",
		"PEOPLE is maintainer_contributions by=maintainer",
		"query_lfx_standard_metrics",
		"PREFER it over the",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("semantic layer guidance missing %q", want)
		}
	}
}

// TestStandardMetricsGuidanceContent pins what the standard-metric guidance
// must say: resolve names before calling, the contract table with the three
// date parameters and the removed ones, the two kinds with their examples,
// the defaults and the applied block, the two switches and their opposite
// defaults, the inventory with every family and grouping, the exact-literal
// rules and what a guard's candidates mean, how to read results, the worked
// calls and the rejections. No absolute figure anywhere (a figure in the
// guidance goes stale and gets quoted), and function, not rationale.
func TestStandardMetricsGuidanceContent(t *testing.T) {
	text := standardMetricsGuidance
	for _, want := range []string{
		// resolve names first, always
		"## Resolve names first — ALWAYS",
		"search_projects",
		"search_b2b_orgs",
		"has\nnot come back from them",
		"never a zero",
		"never pass the everyday name again",
		// the contract table and the removed parameters
		"## The contract",
		"| start_date | yyyy-mm-dd, a UTC calendar day | the family's window (below) |",
		"| end_date | yyyy-mm-dd, a UTC calendar day | today (UTC) |",
		"| period | day, week, month, quarter or year: one row per period | none = one figure |",
		"There is no since, until or as_of",
		"rejected with\nthe word to use instead",
		"There is no free-form filter",
		// the four questions
		"\"TOP CONTRIBUTORS\" with nothing\n   more said means INDIVIDUALS",
		"run it, do not ask which reading was meant",
		"subprojects: excluded | separate | combined, DEFAULT combined",
		"subsidiaries: excluded | separate | combined, DEFAULT excluded",
		"a subsidiary is a different company",
		"a subproject is part of its project",
		"rejects org",
		// the two kinds
		"A WINDOW family counts what happened between start_date and end_date\n     inclusive",
		"An AT-DATE family reports the state on end_date",
		"partial_last_period",
		"the state on a single day is end_date alone",
		"## The two kinds, with examples",
		"end_date=2022-12-31. Any day but today is read date-based",
		"reads a few percent above the status-based current count",
		"start_date=2020-01-01, end_date=2025-12-31, period=year",
		"people on TODAY's\n  roster with a code contribution in each year",
		"The roster itself has no\n  honest history",
		"v2 health\n  score, whose snapshots start on 2026-08-25",
		"withheld under a coverage threshold",
		"applied.coverage says how many, out of the LF-hosted\n  projects with a health row on the snapshot day",
		// defaults and the applied block
		"## Defaults and the applied block",
		"end_date defaults to today (UTC)",
		"includes_future_dated",
		"the trailing 365 days before end_date",
		"all history on new_members and\nmembership_churn",
		"the trailing year on any day or week series",
		"runs from\nthe first row of data",
		"timezone (always UTC)",
		"definition (one sentence",
		"defaulted (the list of parameters the lens\nchose)",
		"truncated (limit cut\nrows off)",
		"`engine` is provenance for\nyou and never goes in an answer",
		"Do not compare the figure\nwith a number from another dashboard",
		// the switches
		"## The switches, row by row",
		"| combined (default) | X plus everything under it, any depth | folded into ONE row",
		"| separate | X plus everything under it, any depth | as the metric groups them, one row each: the breakdown |",
		"| excluded | X's own bucket only, nothing under it |",
		"| excluded (default) | the Y account only |",
		"| combined | Y plus every subsidiary under it, any depth | folded into ONE row",
		"one row per\nparent organization",
		"MOST QUESTIONS WANT\nBOTH",
		"Never derive one from the other: distinct counts do not sum",
		// inventory: every family, kind and grouping list — derived below from
		// standardMetricNames and standardMetricGroupings, the single source
		"## Inventory",
		// the definitions and caveats that change how a figure is read
		"revenue is LIST PRICE, not dues billed — never divide one by the other",
		"membership_count, membership_revenue on any day but today and on a series",
		"region is provisional",
		"The churn date is the day AFTER the term ended",
		"known for about a third of contributors",
		"one row per GitHub identity (`handle`, a profile URL)",
		"Organizations from the enrichment vocabulary, not CRM accounts",
		"A superset of contributors",
		"today's roster active in each period",
		"Maintainership as of the build, contributions in the window",
		"v2 score; snapshots start on 2026-08-25; a subset of LF-hosted projects (applied.coverage)",
		"category is the stored v2 band name (Excellent, Healthy, Fair, Concerning, Critical), never a threshold",
		"the average is v2 from the DBT-1 deployment (week of 2026-09-07) onward, v1 before",
		"The family reports the count of scored projects and their\n  mean score",
		"before that day the layer's\n  average still reads the v1 score",
		"project_health_count, avg_project_health_score",
		"additive across projects at a date, never across days",
		"totals read low, never inflated",
		"The window is the EVENT start date",
		"distinct people by email: never sum them across rows",
		"Distinct people with an Accepted speaker status",
		"rejected and in-review proposals are excluded",
		"the edX branch carries no account",
		"neutral or unknown sentiment is in neither positive nor negative",
		"The sum counts a prolific author once per mention",
		// organizations: exact literals, the guard, the candidates
		"names its organization column `account`",
		"ANY DEPTH:\nseparate and combined walk the account hierarchy to the bottom",
		"LITERALS ARE EXACT",
		"STRAY SAME-COMPANY ACCOUNT",
		"the rejection lists\nup to five data-bearing candidates",
		"the parent legal name first",
		"never present a\ncandidate as the caller's own choice",
		// projects
		"AT ANY DEPTH\n(grandchildren included)",
		"never sum to a subtree total",
		"Memberships attach at FOUNDATION level",
		"'kubernetes' is\nnot a slug, 'k8s' is",
		// what goes in the answer
		"## What goes in the answer",
		"only the caveats that change how THIS figure is read",
		"in the reader's words",
		"Column names, keys, engines, SQL and the other tools\nstay out",
		// reading results
		"## Reading results",
		"Every date is a UTC calendar day, on both engines",
		"`period` is the first day of each period",
		"`period_end` is the day the state was read on",
		"never an organization, never\n  folded into a parent",
		"Present them side by side, never as a ratio",
		"rows are GitHub identities",
		"Two identities sharing a display name are two\n  rows",
		"never as a contact list",
		"never mix\n  the two in one answer",
		"truncated=true means limit cut rows off",
		// worked calls, one per kind, a series, an org, a rejection
		"## Worked calls",
		"One figure, window: contributors, project=cncf, start_date=2025-01-01",
		"One figure, at-date: memberships, project=cncf",
		"A series: new_members, project=tlf, period=year",
		"An org with subsidiaries: contributions, org=International Business\n  Machines Corporation, subsidiaries=combined",
		"A rejection: memberships, start_date=2020-01-01",
		// errors
		"## Errors",
		"read it and change the call, do\nnot retry the same one",
		"since, until or as_of: the message names start_date or end_date",
		"an org that matches no data-bearing account: 400 with candidates",
		"an unknown project slug: 400 with candidates",
		"an order_by field that is not one of the result columns",
		"no snapshot on or before\n  end_date",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("standard metric guidance missing %q", want)
		}
	}
	// Every family's inventory row, in the guidance's own table shape, from the
	// one list the description and the schema tests use too: a grouping
	// added on one surface and not the other fails here.
	for _, name := range standardMetricNames {
		row := "| " + name + " | " + standardMetricKinds[name] + " | " + standardMetricGroupings[name] + " |"
		if !strings.Contains(text, row) {
			t.Errorf("standard metric guidance missing inventory row %q", row)
		}
	}
	// Nothing from the old contract survives in the document as a live
	// instruction (as_of, since and until are named only as the words a
	// rejection replaces).
	for _, gone := range []string{"since/until", "takes as_of", "FLOW", "SNAPSHOT", "US-Pacific", "display name — \"top", "one-hop"} {
		if strings.Contains(text, gone) {
			t.Errorf("standard metric guidance still says %q, which the contract removed", gone)
		}
	}
}

// TestStandardMetricsGuidanceCarriesNoFigure pins that the guidance quotes
// no absolute figure: a number in the guidance goes stale the day after it
// is written and gets quoted as if it were the answer. Dates, parameter
// counts, HTTP statuses and the 365-day default are the only numbers.
func TestStandardMetricsGuidanceCarriesNoFigure(t *testing.T) {
	allowed := regexp.MustCompile(`^(20\d\d(-\d\d(-\d\d)?)?|365|400|404|1|2|3|4|5|31|01)$`)
	// A date is one token, not three: consume yyyy-mm-dd before bare numbers.
	for _, match := range regexp.MustCompile(`\b\d{4}-\d\d-\d\d\b|\b\d[\d,.]*\b`).FindAllString(standardMetricsGuidance, -1) {
		match = strings.TrimRight(match, ".,")
		if !allowed.MatchString(match) {
			t.Errorf("standard metric guidance carries the figure %q; figures go stale and get quoted", match)
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
		"standard metric guidance": standardMetricsGuidance,
		"tool description":         standardMetricsDescription,
	} {
		if strings.Contains(text, "Insights") {
			t.Errorf("%s names Insights; describe what the figure covers instead", name)
		}
	}
}
