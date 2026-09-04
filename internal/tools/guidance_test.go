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
		"the snapshot history is days\n  deep",
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
		// inventory: every family, kind and grouping list
		"## Inventory",
		"| memberships | at-date | total, org, tier, project, country, region |",
		"| new_members | window | total, org, project |",
		"| membership_churn | window | total, org, project |",
		"| contributors | window | total, org, project, country, region |",
		"| contributions | window | total, org, project, contributor, type, platform, org_region |",
		"| contributing_organizations | window | total, project |",
		"| participants | window | total, org, project |",
		"| maintainers | at-date | total, org, project, maintainer |",
		"| maintainer_contributions | window | total, org, project, maintainer |",
		"| project_health | at-date | total, foundation, category, population |",
		"| software_value | at-date | total, foundation, population |",
		"| event_registrations | window | total, event, org |",
		"| event_sponsorships | window | total, org, event |",
		"| speakers | window | total, event |",
		"| training_enrollments | window | total, org, course |",
		"| certifications | window | total, org |",
		"| social_mentions | window | total, project, network, sentiment |",
		"| social_reach | window | total, project |",
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
		"LF-hosted projects unless by=population (rows lf_hosted and index)",
		"never sum or re-average rows",
		"additive across projects at a date, never across days",
		"totals read low, never inflated",
		"The window is the EVENT start date",
		"distinct people by email: never sum them across rows",
		"Sessionize rows count proposal SUBMITTERS whether accepted or not",
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
