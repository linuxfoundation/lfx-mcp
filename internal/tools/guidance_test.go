// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
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
		"read noticeably higher with bots",
		// org shares and headcounts (in words, never a ratio)
		"org-ATTRIBUTED",
		"Individual - No Account",
		"report the unattributed share separately (it\nis large)",
		"run well below externally published headcounts (volumes\nreconcile far more closely)",
		// name discovery and rollups
		"International Business Machines Corporation",
		"Red Hat LLC",
		"account__account_rollup_name",
		"subsidiaries INTO parents",
		// tiers, health, value
		"Premier Membership",
		"health_metric_key__has_health_score_v2') }} = true",
		"(Excellent, Healthy, Fair, Concerning, Critical)",
		"current_avg_health_score and current_software_value",
		"read each project's own latest snapshot\nwithout a pin — a different population from the pinned day's (each project on\nits own date, so \"current\" is not \"today's reading\")",
		"project_health_latest_id__is_lf_project",
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
		"meeting_occurrences (distinct occurrences held) and scheduled_meeting_minutes\n(sum of the scheduled duration",
		"unique_attendees (distinct PEOPLE who attended",
		"attendees_count (attendance RECORDS where the invitee attended",
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
		// DBT-2: the layer reaches the whole company and the whole subtree on
		// its own dimensions; the rollup is one hop, the parent slug one level,
		// and the two models without an account entity are named
		"REACH — how deep this layer's own dimensions go: the whole company at any\n  depth is account__top_parent_name",
		"account__account_rollup_name is ONE hop,\n  for direct subsidiaries only",
		"project__project_path LIKE '%/<slug>/%' (project__project_depth for\n  levels); project__parent_project_slug is one level",
		"Speakers and meeting\n  attendance carry no account entity",
		"ANY DEPTH: account__top_parent_name folds every subsidiary\ninto the top parent (Red Hat LLC's own acquisitions land under IBM)",
		"Employer: account__account_name /\naccount__top_parent_name",
		"account__top_parent_name for the whole company (recipe 6); speakers carry no\naccount entity",
		"on non-activity metrics a subtree is project__project_path LIKE '%/<slug>/%'",
		"cross-domain joins no standard metric or\n  dimension expresses",
		"already resolved to the parent account FOR THAT PROJECT",
		// where each domain attaches
		"ATTACHMENT LEVELS",
		"legitimately\n  near-empty",
		// windows cut on Pacific days, layer-wide
		"Day boundaries are US-Pacific",
		// standard metric calls
		"STANDARD METRIC CALLS take uniform parameters",
		"this means INDIVIDUALS by contribution volume — run it, do not ask",
		"The two meetup families are the\nexception to the uniform switches",
		"(the meetup families\nexcepted from DEPTH: no tree, subprojects a no-op)",
		"16. MEETUPS (Open Community Groups)",
		"a community IS a foundation:\nproject=cncf",
		"AVAILABILITY FIRST",
		"search is a free-text substring match",
		"say so and stop",
		"where={{ Dimension('meetup_group__city') }} = 'Austin'",
		"metrics=meetups,\nsearch='Austin') first (metrics is required on that action)",
		"is the Austin meetup scene growing",
		"Growth is the direction of BOTH series",
		"17. SOCIAL LISTENING. The standard metrics social_mentions (by total,\nproject, network, sentiment) and social_reach (by total, project) cover the\ncommon readings — prefer them",
		"Compose here only for a slice they lack",
		"stored as 'Twitter', not 'X'",
		"no filter means ALL of LF",
		"18. WHAT GOES IN THE ANSWER",
		"There is no free filter on a standard metric",
		"default combined",
		"default excluded",
		"start_date,\nend_date, period (day|week|month|quarter|year)",
		"there is no\nsince, until or as_of",
		"DEFAULTS are the plain reading",
		"applied block",
		"a WINDOW family counts between start_date and end_date",
		"an AT-DATE family (memberships, member_organizations,\npaying_member_organizations, maintainers, project_health, software_value)\nreports the state on end_date",
		"no lens call needed",
		"Every date is a UTC calendar day",
		"separate and combined cover a named node's tree and a company's subsidiaries\nat ANY depth. Results come back",
		"maintainer_contributions (by=project or by=org",
		"PEOPLE is maintainer_contributions by=maintainer",
		"query_lfx_standard_metrics",
		"call query_lfx_standard_metrics directly; do not explore this layer or the\n  lens first",
		"Where this layer and the standard metrics read differently (both are\n  right; say which one you used)",
		"the family counts LF projects only",
		"the family uses the event start date",
		"the family counts\n  Accepted only",
		"are folded into the NULL\n  account row there",
		"metric_time <= 'D' AND asset_id__end_date >= 'D' (end_date is never NULL",
		"The 'tlf' slug is the umbrella's own\n  bucket, not the LF-wide scope; state which population you used.",
		"An LF-wide total takes NO project; the\nfoundation's own slug (tlf) is one bucket, not the LF-wide scope.",
		"open-ended terms carry a far-future placeholder",
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
		// meetups: the two families, the community-is-a-foundation scope
		// rule, the switch exceptions, the caveats, and the four shapes
		"| meetups | window | total, community, region, group, city |",
		"| meetup_attendees | window | total, community, region, group, city |",
		"ALL history when start_date is omitted, unlike the activity families",
		"NULL region or city rows are chapters with none set",
		"absent rather than a zero row",
		"## Meetups (Open Community Groups)",
		"a community is a foundation",
		"org and\nsubsidiaries do not apply and are rejected",
		"subprojects is accepted but a\nNO-OP",
		"two years' rows do not sum\nto a two-year figure",
		"read ALL HISTORY when start_date is omitted",
		"attended CNCF meetups in 2024 vs 2025",
		"period=year, start_date=2024-01-01, end_date=2025-12-31",
		"Top 5 regions by meetup activity",
		"order_by=-meetups, limit=5",
		"The 3 least active groups",
		"order_by=meetups,\n  limit=3",
		"absent rather than zero",
		"Is the Austin meetup scene growing",
		"Meetups by city over time",
		// resolve names first, always
		"## Resolve names first — ALWAYS",
		"search_projects",
		"search_b2b_orgs",
		"has\nnot come back from them",
		"never a zero",
		"pick one or go back to the search tool",
		// the contract table and the removed parameters
		"## The contract",
		"| start_date | yyyy-mm-dd, a UTC calendar day | the family's window (below) |",
		"| end_date | yyyy-mm-dd, a UTC calendar day; for \"each year since X\" or \"through now\" leave it unset",
		"| period | day, week, month, quarter or year: adds a time dimension to by | none = no time series |",
		"There is no since, until or as_of",
		"rejected by the\nrequest schema as an unexpected property; the words are start_date and\nend_date",
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
		"An AT-DATE family (memberships, member_organizations,\n     paying_member_organizations, maintainers, project_health,\n     software_value) reports the state on end_date",
		"partial_last_period",
		"the state on a single day is end_date alone",
		"## The two kinds, with examples",
		"member_organizations, end_date=2022-12-31 (date-based; see Reading\n  results)",
		"a few percent above\n  the status-based figure; applied.definition says which",
		"today's roster members\n  with a code contribution in each period (the activity's account on by=org),\n  not the roster at that time",
		"an end_date other than today without period\n  is a rejection",
		"give applied.coverage when\n  the count is the answer",
		// defaults and the applied block
		"## Defaults and the applied block",
		"end_date defaults to today (UTC)",
		"includes_future_dated",
		"the trailing 365 days before end_date",
		"all history on new_members, membership_churn\nand the new_/lost_member_organizations families",
		"the trailing year on any\nday or week series",
		"runs from\nthe first row of data",
		"timezone (always UTC)",
		"definition (one sentence",
		"defaulted (the list of parameters the lens\nchose)",
		"truncated (limit cut\nrows off)",
		"`engine` is provenance for you and never goes in\nan answer",
		"Do not compare the figure with a number from\nanother dashboard",
		// the switches
		"## The switches, row by row",
		"| combined (default) | X plus everything under it, any depth | folded together: the project columns leave the result; the rows are whatever by groups",
		"| separate | X plus everything under it, any depth | as the metric groups them, one row each: the breakdown |",
		"| excluded | X's own bucket only, nothing under it |",
		"| excluded (default) | the Y account only |",
		"| combined | Y plus every subsidiary under it, any depth | folded together: the org columns leave the result; the rows are whatever by groups",
		"one\nrow per parent organization, resolved to the top of each chain",
		"MOST\nQUESTIONS WANT BOTH",
		"Never derive one from the other: distinct counts do not sum",
		// inventory: every family, kind and grouping list — derived below from
		// standardMetricNames and standardMetricGroupings, the single source
		"## Inventory",
		// the definitions and caveats that change how a figure is read
		"revenue is LIST PRICE, not dues billed — never divide one by the other",
		"membership_count, membership_revenue on any day but today and on a series",
		"region is provisional",
		"the churn date is the day AFTER the term ended",
		"known for a minority of contributors (applied.coverage)",
		"one row per GitHub identity (`handle`, a profile URL)",
		"Organizations from the enrichment vocabulary, not CRM accounts",
		"contributors = code, participants = anyone who did anything",
		// SM-3: the organization grain, firstness and the consortium
		"is member_organizations (paying-only: paying_member_organizations), never derived from these rows",
		"\"new logos\" (organizations new to the LF or returning after a lapse) is new_member_organizations",
		"lost organizations are lost_member_organizations",
		"Distinct organizations holding at least one active membership on end_date, status-based",
		"applied.firstness says arrival (no project), first in the foundation or first in the project, following the project switch",
		"an arrival with no project (first LF membership ever or a return after\na lapse), first in the foundation (or its consortium) for a root, first in\nthe project with excluded",
		"- new_member_organizations with a project below foundation level and\n  combined:",
		"Distinct organizations that departed in the window: a paid membership ended with nothing of any tier or price in force the day after, counted at that churn date and judged on that day, so a later return does not remove it",
		"The paying subset of member_organizations",
		"contributors = code, participants = anyone who did anything",
		"CONSORTIA. A JDF series and its '-fund' project",
		// V23: the four organization-grain definition sentences, verbatim from applied.definition
		"Distinct organizations that became members in the window, for the first time or returning after a lapse",
		"Distinct organizations holding at least one active membership with a list price above zero on end_date, status-based; list price, not dues billed",
		// V24: the ad hoc trap
		"the layer's first-membership flag counts membership rows, not organizations, and is_first_membership is the Salesforce 'New Business' opportunity type",
		// V25: participants' definition, visibly different from contributors'
		"Distinct non-bot people with any activity in the window: code, issues, reviews, comments, stars, forks, meeting invitations and attendance, training and exams, Hacker News",
		// V28: the applied block names scope and the fields it carries
		"engine, scope (one sentence: the project\nclause and the org clause as applied), note, firstness, consortium and\nconsortium_members",
		"the sentence under a figure comes from `scope`,\n`definition` and `defaulted`",
		// SM-3 worked call: one for the four families
		"member_organizations, by=foundation,\n  order_by=-member_organizations, limit=5",
		"the same parameters serve the paying_, new_\n  (add start_date) and lost_member_organizations families",
		"applied.consortium_members\nlists them; report the one figure and name the members",
		"excluded reads the\nnamed project alone; other families never merge one",
		"first-membership is defined for a single project or for a whole\n  foundation",
		"the organization families (member_, paying_member_,\n  new_member_, lost_member_organizations) count distinct organizations",
		"today's roster active in each period",
		"Maintainership as of the build (applied.definition says which roster this call read), contributions in the window",
		"category is the stored v2 band name (Excellent, Healthy, Fair, Concerning, Critical), never a threshold",
		"project_health_count, avg_project_health_score",
		"additive across projects, never across days",
		"totals read low, never inflated",
		"The window is the EVENT start date",
		"distinct people by email, not registrations: never sum them across rows",
		"Distinct people with an Accepted speaker status",
		"rejected and in-review proposals are excluded",
		"the edX branch carries no account",
		"neutral or unknown sentiment is in neither positive nor negative",
		"The sum counts a prolific author once per mention",
		// organizations: exact literals, the guard, the candidates
		"names its organization column `account`",
		"parent_org is the account's\ndirect parent, except on a parent leaderboard (no org, separate or combined)\nwhere it is the top of the chain; some by=org rows carry no parent column",
		"LITERALS ARE EXACT",
		"STRAY SAME-COMPANY ACCOUNT",
		"rejected,\nwith up to five candidates that do carry it",
		"carrying no data for the family ('IBM', 'Google', 'Microsoft')",
		"never present a candidate as the\ncaller's own choice",
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
		"never an\n  organization, never folded into a parent, never dropped",
		"its revenue is LIST PRICE (never a ratio\n  of the two)",
		"rows are GitHub identities",
		"Two identities sharing a display name are two\n  rows",
		"never as a contact list",
		"never mix\n  the two in one answer",
		"truncated=true means limit cut rows off",
		// worked calls, one per kind, a series, an org, a rejection
		"## Worked calls",
		"One figure, window: contributors, project=cncf, start_date=2025-01-01",
		"One figure, at-date: memberships, project=cncf",
		"A series, LF-wide: new_members, period=year, no project",
		"An org with subsidiaries: contributions, org=International Business\n  Machines Corporation, subsidiaries=combined",
		"A rejection: memberships, start_date=2020-01-01",
		// errors
		"## Errors",
		"read it and change the call, do\nnot retry the same one",
		"the message names the\n  replacement (since and until are start_date and end_date; as_of is\n  end_date)",
		"an org that matches no data-bearing account: 400 with candidates",
		"an\n  unknown project slug: 400 with candidates",
		"an order_by field that is not one of the result columns",
		"no snapshot on or before end_date",
		// round 9: date-free health disclosure; the tool reports the version
		"- project_health is read on the latest snapshot on or before end_date\n  (applied.snapshot_date); an end_date before the first v2 snapshot is a\n  rejection, not a zero; this family has no unpinned reading — each project's\n  own latest score with no common day is the semantic-layer route's current_*\n  health metrics",
		"applied.definition says which column this call read",
		"v2 snapshots have a short history",
		"In the answer: the figure, the scope, the snapshot day, and\n  the version applied.definition gives — one line",
		"the average normalized to a hundred-point scale on both engines; applied.definition says which column this call read",
		"snapshots have a short history).",
		// round 9: answer economy, the umbrella slug, placeholders, one lens query
		"when unsure\nwhether a note belongs, leave it out",
		"\"How many members\ndoes the LF have\" → member_organizations, no project: the figure, \"distinct\nmember organizations across the LF today\"",
		"leave project unset. A foundation's slug is a root in the project tree and,\non the membership families, that foundation's own programme: hosted\nfoundations are their own roots with their own slugs and are never folded\nin; combined adds only programmes the spine places under the root; LF-wide\nis no project.",
		"A search_projects hit for the foundation's name\nis not a reason to scope.",
		"- A series, LF-wide: new_members, period=year, no project",
		"report it as returned and name the\n  cause (a stray same-name account, say) as the caveat; do not re-issue it\n  with different filter logic",
		"a scored project without a stored maximum counts in the total but not in the average; v2 snapshots have a short history;",
		// round 7 addendum: preconditions, not background
		"stating the stored spelling here does not exempt\nit: k8s, cncf and tlf still come back from search_projects in-session",
		"Do not compute a\nstart_date by counting back N years from today",
		"report every row the series returned, or say how many rows\nthere are and which ones you show",
		// round 11: executive readings vs the layer's (Part E)
		"an organization with memberships on three projects counts three times",
		"count with\n  by=total, list with limit and order_by (applied.truncated says when the\n  list is partial)",
		"last N years, last N months, trailing quarter",
		"an organization that dropped one project but kept another still counts here and is not a lost member",
		"lf_region is provisional and lists China, India and\n  Japan beside Asia Pacific",
		"check-in data exists only for some registration sources, so an event whose source carries none shows zero attendees, not low attendance",
		"memberships without a\n  resolvable billing country; it is one row, never dropped and never folded\n  into a named country",
		// round 10: series order and flagged rows
		"A series arrives in period order, oldest first",
		"a figure that is not in a row does\n  not exist",
		"start_date=2020-01-01, period=year, no end_date: one row per year end",
		"the last one today's state with\n  partial_last_period set",
		"a future end_date reads the scheduled state on that day and sets includes_future_dated, it is not the to-date row",
		"A flagged row (partial_last_period,\nincludes_future_dated) is reported with the applied block's wording, never\ndropped",
		"do not trim the table to a rounder\nwindow when writing up",
		"Decide the lens query's\n  scope before issuing it, not after seeing the result",
		// verification exercise (round 5): the disclosed lines
		"means participants (any non-bot activity, stars and forks included)",
		"show the call shape\nonly",
		"only when the applied block sets the flag",
		"a computed start clips the first period",
		"a top-N cut hides subsidiaries a hand-sum would miss",
		"placeholder accounts such as\n  'Individual - No Account' are folded into it on by=org",
		"sits in the NULL account row with the placeholder learners",
		"a large share resolves to none and sits in the NULL account row",
		"candidates carry every word of the text you sent in\n  their name or their parent's and hold data for the family; a run-together\n  spelling (Redhat) may return none",
		"since, until, as_of, group_by, where: rejected by the request schema",
		"A future end_date does not move the default window\nstart",
		"each LF-hosted project's own latest snapshot row on or before end_date",
		"applied.snapshot_date is null",
		"an ad hoc query by registration date reads differently",
		"an ad hoc count over all statuses reads higher",
		"an ad hoc count over the whole maintainers index reads higher",
		"Choosing combined or separate to answer a broader question is a scope\nchoice the answer names in words (applied.scope carries the sentence)",
		"because a rollup was asked for; absent that, start from\n  excluded",
		"ONE lens query whose filter mirrors the family's stated definition",
		"subsidiaries=separate needs by=org and subprojects=separate needs\nby=project",
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

// TestSemanticLayerGuidanceCarriesNoDataRatio pins that the semantic-layer
// guidance quotes no data-derived ratio or share (a bots multiplier, an
// unattributed-share range, a headcount multiplier, a reconciliation
// percentage): such a figure is a measurement of one day's warehouse that
// gets quoted as if it were the answer. Ratios are words; a figure a caller
// needs is read at request time into applied.coverage or not at all.
func TestSemanticLayerGuidanceCarriesNoDataRatio(t *testing.T) {
	ratio := regexp.MustCompile(`\b\d+(\.\d+)?(-\d+(\.\d+)?)?\s?(x\b|%)|~\d`)
	for _, m := range ratio.FindAllString(semanticLayerGuidance, -1) {
		t.Errorf("semantic layer guidance carries the data ratio %q; say it in words", m)
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

// TestGuidanceHealthIsV2ByName pins the health vocabulary: the layer is
// undeclaring the v1 category dimension, so the guidance names only the v2
// one, and categories are the stored band names, never a score threshold.
func TestGuidanceHealthIsV2ByName(t *testing.T) {
	v1 := regexp.MustCompile(`health_score_category\b[^_]|Stable|Unsteady|Critical <|\b20-39\b`)
	for name, text := range map[string]string{
		"semantic layer guidance":  semanticLayerGuidance,
		"standard metric guidance": standardMetricsGuidance,
		"tool description":         standardMetricsDescription,
	} {
		if m := v1.FindString(text); m != "" {
			t.Errorf("%s still carries the v1 health vocabulary: %q", name, m)
		}
	}
}

// TestGuidanceCarriesNoCalendarDate pins Josep's rule for disclosures: a
// sentence says how a thing reads now and what it changes to, and the tool
// reports which state applied; a calendar date or a deployment week makes
// the sentence undecidable on the day it names. Years are allowed only as
// worked-call parameters, MetricFlow date literals, quoted example questions
// and the layer's stored sentinel value.
func TestGuidanceCarriesNoCalendarDate(t *testing.T) {
	year := regexp.MustCompile(`\b20\d\d\b`)
	allowed := regexp.MustCompile(`_date=|'20\d\d-\d\d-\d\d'|"[^"]*20\d\d[^"]*"|2000-01-01 sentinel|in 20\d\d"`)
	for name, text := range map[string]string{
		"semantic layer guidance":  semanticLayerGuidance,
		"standard metric guidance": standardMetricsGuidance,
	} {
		for n, line := range strings.Split(text, "\n") {
			if year.MatchString(line) && !allowed.MatchString(line) {
				t.Errorf("%s line %d carries a calendar date outside an example: %q", name, n+1, line)
			}
		}
	}
	for _, banned := range []string{"week of", "DBT-1 deployment lands", "from that deployment", "count them"} {
		if strings.Contains(standardMetricsGuidance, banned) {
			t.Errorf("standard metric guidance still says %q", banned)
		}
	}
}

// TestCombinedFoldsAHierarchyNotTheResult pins the Copilot round-3 fix: a
// combined fold removes the project or org columns and keeps whatever the by
// grouping produces; only by=total is one figure. No client text may say
// combined is "one row" or "one figure", which had callers reading a valid
// by=org breakdown as a wrong shape.
func TestCombinedFoldsAHierarchyNotTheResult(t *testing.T) {
	tool := listRegisteredTool(t, "query_lfx_standard_metrics", RegisterStandardMetrics)
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"standard metrics schema":   string(raw),
		"standard metrics guidance": standardMetricsGuidance,
		"semantic layer guidance":   semanticLayerGuidance,
	} {
		for _, banned := range []string{
			"folded into ONE figure", "folded into ONE row", "folded into one row",
			"those folded into one row", "one figure is combined",
		} {
			if strings.Contains(text, banned) {
				t.Errorf("%s still says %q: combined folds a hierarchy, not the result", name, banned)
			}
		}
	}
	for _, want := range []string{"the project columns leave the result", "the org columns leave the result"} {
		if !strings.Contains(string(raw), want) || !strings.Contains(standardMetricsGuidance, want) {
			t.Errorf("schema and guidance must both say %q", want)
		}
	}
}
