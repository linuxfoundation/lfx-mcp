# LFX Semantic Layer & Lens — agent guidance

Read this once per session before your first explore_lfx_semantic_layer,
query_lfx_semantic_layer or query_lfx_lens call. Answer from fresh queries only.

A metric is measured; a dimension groups or filters it; entities link domains, so one
query spans domains by grouping on a shared dimension — you never write a join.
Dimension qualified_names are entity__field, prefix per metric — copy from explore, NEVER assemble.

## Routing

- explore_lfx_semantic_layer discovers metrics, dimensions and stored values; query_lfx_semantic_layer runs the query. Explore first.
- query_lfx_standard_metrics answers common questions with governed metrics
  named in plain words (contributors_by_org, members_and_dues_by_org...). When
  one matches, PREFER it over the explore+query flow: it also reaches a
  company's subsidiaries and a project's tree at ANY depth, which this layer
  does not (see REACH under Scope). Inventory:
  read_lfx_standard_metrics_guidance. Decks: read_lfx_deck_building_guidance.
- query_lfx_lens (text-to-SQL): person-grain maintainer rankings ("top
  maintainers by contributions"), social listening, any-depth hierarchy
  questions no standard metric expresses, and questions no metric family
  expresses — label its answers as generated SQL.
- Committee/board/ambassador rosters: committee tools. Meetings: meeting tools.
  Neither is in this layer or lens's lane (recipe 12).

## Protocol

0. If a standard metric matches, prefer it (scope and window via its own
   parameters, recipe 15; never go ad hoc just to sort, filter or scope).
1. ALWAYS resolve names first: search_projects for project slugs ('k8s', 'korg',
   'ptproject' — stored slugs are not everyday names), search_b2b_orgs for org
   legal names (recipe 5). A name either tool returned in this session may be
   reused as is; never filter, scope or call a standard metric with a slug or
   an organization name that has not come back from them.
2. Discover: list_metrics(search) → get_dimensions → get_dimension_values before
   filtering on any value you have not seen in output.
3. Query; state population, exact date window, and what counts as one in answers.

## Query syntax

  metrics   (required) CSV, copied from explore
  group_by  dimension qualified_names; metric_time__year (or __quarter, __month,
            __week, __day) for trends
  where     one MetricFlow expression, dates yyyy-mm-dd:
            {{ Dimension('country__lf_region') }} = 'Europe' AND {{ TimeDimension('metric_time','DAY') }} >= '2024-01-01'
  order_by  '-metric' descending — NULL rows sort FIRST; re-sort client-side
  limit     ceiling 500 (10-20 top-N, 50-100 breakdowns); omitted = EVERY row,
            and full rosters run to thousands — set one unless you need them all

Multiple metrics outer-join on their shared dimensions (the only valid group_by
set); missing sides show NULL. Name dimensions give ranked lists; bare entities
return raw IDs. current_* is active-only; total_contributors excludes bots.

## Scope

There is no separate project parameter; scope lives in where. Pick the slug
dimension by what the question names:

- A WHOLE FOUNDATION (CNCF, LF AI & Data, OpenSSF...):
  {{ Dimension('project__foundation_slug') }} = '<slug>' — the conformed lens:
  works on every metric family, counts each row once. NEVER use project_slug for
  a foundation: it matches only the foundation's catch-all bucket, a silent ~40x
  undercount on activities.
- A SINGLE PROJECT (k8s, pytorch...): activity_project_id__project_slug — the
  per-project surface (__project_slug and __segment_slug), whose DEFINITION
  is code contributions, bots excluded. Do NOT reconcile figures against other
  dashboards or pages: they differ by repo registration, member cleaning and
  curated lists; say what this figure covers, and PCC-style reporting is the
  reconciliation surface. For leaf projects the spine slug returns identical
  counts; for umbrella nodes project_slug is only the node's own bucket.
- SUBTREES AND HIERARCHY WALKS (umbrella nodes, "foundation to its projects"):
  activity_project_id__project_spine_slug — identical totals to foundation_slug
  (verified) and the surface PCC-style foundation rollups reconcile against.
  spine_hierarchy_level = 2 lists direct children
  ("Direct children of X": spine slug = X + level 2, group by project slug).
- SUM METRICS (insertions, deletions): ALWAYS the spine filter — non-hierarchical
  filters inflate them 2-4x. Walk-downs are flattened: counts only, never sums.
- ATTACHMENT LEVELS: memberships and event registrations attach at FOUNDATION
  level almost entirely, so a project-level filter on them is legitimately
  near-empty rather than a failed query — scope them by foundation and say the
  figure is foundation-level. Enrollments and maintainers attach below the
  foundation too.
- Domain scope dimensions: memberships asset_id__project_slug; maintainers
  maintainer_key__project_slug (ALWAYS + maintainer_key__is_lf_project = true, see
  recipe 11; foundations via
  project__foundation_slug — the cm_*_slug rollups are NOT project keys);
  health health_metric_key__foundation_slug. Events, registrations, sponsorships
  and training carry the conformed project entity — scope them with
  project__foundation_slug / project__slug (event_id__project_name also works but
  needs the EXACT stored display name).
- "The Linux Foundation": the 'tlf' slug is the umbrella foundation's own tree,
  NOT the portfolio. LF-wide = unscoped or grouped by foundation; state which
  population you used (they differ 3-4x on memberships).
- Twins exist (risc-v-international/riscv, cff/cloud-foundry,
  opensearch-foundation/opensearch-project): low total → group by the slug.
  Compare entities with IN (...) + group_by; never total across spine groups.
- REACH — how deep this layer's own dimensions go, and where to go instead:
  ACCOUNTS: account__account_rollup_name is ONE hop (an account's direct
  parent), so filtering or grouping on it covers a company's DIRECT
  subsidiaries only. PROJECTS: project__foundation_slug covers a foundation
  completely; project__parent_project_slug covers a node below foundation
  level ONE level down (its direct children); only the activity spine
  (activity_project_id__project_spine_slug) reaches any depth, and only on
  activities. When the question means the whole company or the whole
  subtree, that is a standard metric (subsidiaries / subprojects separate or
  combined walk to the bottom on every recipe) or, for a shape no standard
  metric has, query_lfx_lens. Use this layer's one-hop and one-level
  dimensions when the question asks exactly for direct subsidiaries or
  direct children, and say so in the answer only then.

## Windows

Day boundaries are US-Pacific for every TimeDimension filter: a 'DAY' bound cuts
at midnight US Pacific, not UTC, so a window sits a few hours off a UTC one —
state the window, never claim an exact UTC calendar month.

Default is the trailing 12 months (the prior 365 complete days); state the concrete
dates and reuse them in any lens question. YTD needs AND metric_time <= today —
installs can be future-dated. Members as of date D: membership_count with metric_time
<= 'D' AND asset_id__end_date >= 'D'; today's actives are current_membership_count;
new members = new_membership_count by install date.

## Value discovery

An unknown filter literal returns zero rows, not an error — call
get_dimension_values before filtering on anything unseen. The "did you mean"
list an unknown dimension name returns is MetricFlow's fuzzy match and can omit
the right name — get_dimensions(search=) is authoritative. Stored spellings
surprise: 'Asia Pacific' never 'APAC'; 'Viet Nam', 'Türkiye' (ISO). Prefer country__*
over asset_id__billing_country (unnormalized free text). Zero rows = suspect
spelling and scope first; only then report absence.

## Worked recipes

1. BOTS. Bot exclusion is the default, built into contributor and activity
metrics (code volumes read roughly 1.8x higher with bots); bot_activities
(member_is_bot) is the explicit bot view.

2. ORG SHARES. Share of work = ACTIVITY VOLUMES, never headcounts. Compute on the
org-ATTRIBUTED base: filter activity_project_id__is_org_contribution = true — the
governed real-organization filter (no need to hand-exclude NULL rows and
'Individual - No Account') — and report the unattributed share (roughly 40-70%)
separately. Account-attributed rows are a SUPERSET of is_org_contribution; the
difference is exactly the Individual placeholder accounts, so a numerator
filtered on an account rollup sits inside this base. The 500-row cap makes
exact percentiles over big pools unretrievable.

3. ORG HEADCOUNTS run 2-4x below externally published counts (volumes reconcile
to ~1-4%). State the caveat.

4. CONTRIBUTOR POPULATIONS. total_contributors = code-only, non-bot;
total_contributors_with_collaboration adds issues/docs/chat (say so); total_activities = any activity.

5. NAME DISCOVERY. Org/account names are stored FULL LEGAL names: IBM is
'International Business Machines Corporation'; Red Hat is 'Red Hat LLC' in account
dimensions, 'Red Hat' in activity-side organization_name. Resolve via search_b2b_orgs
FIRST; empty may be permission-filtering — value-search a distinctive token of
the legal name ('Machines') on account__account_rollup_name.

6. ACCOUNT vs ROLLUP ("including subsidiaries"). account__account_name is the
account holding the record; account__account_rollup_name is its parent, and
rollups fold subsidiaries INTO parents — so filtering the rollup on a
SUBSIDIARY's name returns only what rolls up to THAT subsidiary: a small,
plausible-looking answer that excludes the subsidiary's own row, which sits
under the top parent (Red Hat LLC is itself a rollup parent, while its own row
sits under International Business Machines Corporation). Always filter the TOP
parent, or group by the rollup; to see one subsidiary alone filter
account__account_name. Legal names do not contain the acronym:
value-searching 'IBM' finds accounts SPELLED with it, mostly regional or
stray accounts that are their OWN rollup and NOT folded under the parent —
present those separately rather than as part of it. Additive metrics (dues,
volumes) may be summed across accounts sharing a rollup; headcounts may NOT —
re-read them at rollup grain. No rollup dimension → sum named sub-entities
and list them. ONE HOP: the parent link is a single hop, so an "including
subsidiaries" figure built here covers a top parent's DIRECT subsidiaries but
not their own acquisitions (an account rolling up to Red Hat LLC is not
folded into IBM) — for the whole company at any depth use the standard
metric (subsidiaries=combined), and build it here only when the question
asks for direct subsidiaries alone. On ACTIVITIES the
account is already resolved to the parent account FOR THAT PROJECT before the
rollup applies, so a contribution row's Salesforce account can differ from the
account on the source record; organization_name stays the crowd.dev spelling
and the two vocabularies never mix in one answer.

7. TIER LITERALS differ per foundation ('Premier Membership' vs 'Premier Member') — get_dimension_values per foundation, never reuse.

8. HEALTH SCORES are daily snapshots: find the latest health-bearing date
(health_score_category IS NOT NULL), filter to it, then aggregate; unfiltered
grouping inflates ~8-9x. Bands: Critical <20, Unsteady 20-39.

9. ECONOMIC VALUE = total_software_value / total_estimated_cost (COCOMO): non-additive
daily snapshots; totals can read low, never inflated.

10. RANKING CONTRIBUTORS BY CONTRIBUTIONS ("top contributors"). Unqualified,
this means INDIVIDUALS by contribution volume — run it, do not ask. Group
code_contribution_activities by activity_project_id__member_display_name
(+ organization_name), order by the metric descending, spine-scoped, trailing
365 days. Display names are not identity keys — for identity-stable answers
use lens (member_id); say which. Offer the governed readings as follow-ups:
organizations by volume (contributions_by_org) or headcount
(contributors_by_org), projects by volume (contributions_by_project) or
headcount (contributors_by_project).

11. MAINTAINERS. One project: maintainer_key__project_slug ('k8s' returns the
real roster; cm_project_grandparents_slug = 'k8s' returns ZERO — the cm_*
rollups hold foundation ancestry, verified live). Foundations:
project__foundation_slug. ALWAYS add maintainer_key__is_lf_project = true: the
maintainers model also holds maintainers of non-LF projects crowd.dev tracks
(about half the rows) and they carry a project slug too, so a slug filter alone
does not exclude them; the maintainer standard metrics have this filter
built in. Active = no end date; start_date has a
2000-01-01 sentinel — never trend on it. As of date D: total_maintainers where
maintainer_key__start_date <= 'D' AND (end_date IS NULL OR end_date >= 'D');
since/until on start_date is meaningless, readings before tracking began run
high, and a trend is one as-of reading per period. Maintainer×contribution
figures are not in this layer: contributions made by maintainers per project
or per organization, and the maintainer share of work, are the standard
metrics maintainer_contributions_by_project / _by_org (over contributions
for the same scope); "top maintainers by contributions" as PEOPLE is a
person-grain join only query_lfx_lens can make.

12. ROSTERS AND MEETINGS. search_committees → search_committee_members (paginate;
group-mode names: search_groups/search_group_members). Never infer a roster from
membership or event data. Meetings: search_meetings and search_past_meetings.
Aggregations those tools cannot express (e.g. meeting attendance by company over
a period) are not answerable with governed data today: try query_lfx_lens as a
best effort and disclose there is no canonical way to compute it.

13. REGIONS. country__* follows the person; organization_lf_region etc. follow the org's HQ.

14. EVENTS/TRAINING/SPONSORSHIPS BY ORG. No standard metric covers these —
compose them here. METRICS: total_registrations counts ACCEPTED registrations
only; total_enrollments counts enrollment records only (the source table is
mostly other lifecycle events, and the metric filters them out) — neither
needs a status filter of your own. Sponsorships: total_sponsorship_revenue
(USD) and total_sponsorship_count include ALL tier types — filter
sponsorship__sponsorship_tier_type = 'package_tier' for package-only figures
('a_la_carte' and 'billing_adjustment' are the others). ACCOUNT LENS: the
account entity spans all three — group or filter account__account_name, or
account__account_rollup_name for the parent (recipe 6). ATTACHMENT: all three
attach at foundation level, so scope them with project__foundation_slug; a
leaf project's own slug returns NOTHING, which is the attachment, not missing
data. TIME AXES: registrations carry two — registration_id__event_start_date,
where a window means "events in the window", and metric_time, the sign-up
date; pick the one the question means and say which. Enrollments use
metric_time. PER EVENT: total_registrations by event_id__event_name +
registration_id__event_start_date__year. PER COURSE: total_enrollments by
enrollment_id__course_name + enrollment_id__product_type. FLOORS: edX
enrollments carry no account and land in the NULL bucket, and a share of
registrations has no account either, so every org-scoped figure here is a
floor — present "attributed registrations/enrollments" and say so.

15. STANDARD METRIC CALLS take uniform parameters — metric, project +
subprojects (excluded|separate|combined, default combined), org + subsidiaries
(excluded|separate|combined, default excluded), since/until on FLOW metrics,
as_of on SNAPSHOT ones, order_by, limit. The sixteen:
members_and_dues_by_org, membership_tiers, new_members_by_year,
membership_churn_by_year, contributors, contributions, contributions_by_org,
contributions_by_project, contributors_by_org, contributors_by_project,
maintainers, maintainers_by_org, maintainers_by_project, maintainer_roster,
maintainer_contributions_by_project, maintainer_contributions_by_org. The
switches say what a name covers: excluded = that project or account alone,
separate = it and everything under it one row each (the breakdown), combined
= folded into one row (subprojects=combined folds every project column of
the result). The DEFAULTS are the plain reading: a project name alone is its
whole tree as ONE figure, an organization name alone is that account, and a
contribution metric with no since is the trailing 365 days; every result
carries an applied block saying which scope and window ran. A briefing
usually wants the headline and the breakdown — two calls. DEPTH: on every
standard metric, separate and combined cover a named node's tree and a
company's subsidiaries at ANY depth — deeper than this layer's own
dimensions reach (REACH, above). Results come back in the same words
(account, parent_org, project, foundation, year), and order_by takes them.
There is no free filter on a standard metric: a slice the switches and the
window cannot express is an explore + query question, and its answer is
labelled ad hoc.

16. WHAT GOES IN THE ANSWER. These recipes are working knowledge. The answer
is the figure, one line on what it covers, and only the caveats that change
how that figure is read; vocabulary, grain and timezone notes stay in context
unless they answer the question asked or would change the conclusion.

## Worked examples (verified live)

Top CNCF member orgs by dues:
  metrics=current_membership_count,current_membership_revenue
  group_by=account__account_name order_by=-current_membership_revenue limit=20
  where={{ Dimension('project__foundation_slug') }} = 'cncf'

Kubernetes contributor trend by month:
  metrics=total_contributors group_by=metric_time__month
  where={{ Dimension('activity_project_id__project_slug') }} = 'k8s' AND {{ TimeDimension('metric_time','DAY') }} >= '2026-03-01'

Org share of PyTorch code activity (recipe 2):
  metrics=code_contribution_activities group_by=activity_project_id__organization_name
  order_by=-code_contribution_activities limit=50
  where={{ Dimension('activity_project_id__project_spine_slug') }} = 'pytorch' AND {{ Dimension('activity_project_id__is_org_contribution') }} = true

Prefer repeatable answers: standard metric > named metric > lens SQL — label
anything below the top rung; struggling, re-read the recipe BEFORE any
query_lfx_lens fallback.
