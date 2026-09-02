# LFX Semantic Layer & Lens — agent guidance

Read this once per session before your first explore_lfx_semantic_layer,
query_lfx_semantic_layer or query_lfx_lens call. Answer from fresh queries only.

A metric is measured; a dimension groups or filters it; entities link domains, so one
query spans domains by grouping on a shared dimension — you never write a join.
Dimension qualified_names are entity__field, prefix per metric — copy from explore, NEVER assemble.

## Routing

- explore_lfx_semantic_layer discovers metrics, dimensions and stored values; query_lfx_semantic_layer runs the query. Explore first.
- query_lfx_kpis answers common questions with fixed recipes (kpi_*). When one
  matches, PREFER it over the explore+query flow. Inventory:
  read_lfx_kpi_guidance. Decks: read_lfx_deck_building_guidance.
- query_lfx_lens (text-to-SQL): only maintainer×contribution joins ("top
  maintainers by contributions", maintainer share of work), social listening, and
  questions no metric family expresses — label its answers as generated SQL.
- Committee/board/ambassador rosters: committee tools. Meetings: meeting tools.
  Neither is in this layer or lens's lane (recipe 12).

## Protocol

0. If a kpi_* recipe matches, prefer it (scope and window via its own uniform
   parameters, recipe 15; never go ad hoc just to sort, filter or scope).
1. Resolve scope: search_projects for slugs ('k8s', 'korg', 'ptproject' — stored
   slugs are not everyday names); search_b2b_orgs for org legal names (recipe 5).
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
- A SINGLE PROJECT (k8s, pytorch...): activity_project_id__project_slug — matches
  what that project's Insights page shows (__project_slug and __segment_slug are
  the page-parity surfaces). For leaf projects the spine slug returns identical
  counts; for umbrella nodes project_slug is only the node's own bucket.
- SUBTREES AND HIERARCHY WALKS (umbrella nodes, "foundation to its projects"):
  activity_project_id__project_spine_slug — identical totals to foundation_slug
  (verified) and the surface PCC-style foundation rollups reconcile against.
  spine_hierarchy_level = 2 lists direct children
  ("Direct children of X": spine slug = X + level 2, group by project slug).
- SUM METRICS (insertions, deletions): ALWAYS the spine filter — non-hierarchical
  filters inflate them 2-4x. Walk-downs are flattened: counts only, never sums.
- Domain scope dimensions: memberships asset_id__project_slug; maintainers
  maintainer_key__project_slug (+ is_lf_project = true; foundations via
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

## Windows

Default is the trailing 12 months (prior 365 complete UTC days); state the concrete
dates and reuse them in any lens question. YTD needs AND metric_time <= today —
installs can be future-dated. Members as of date D: membership_count with metric_time
<= 'D' AND asset_id__end_date >= 'D'; today's actives are current_membership_count;
new members = new_membership_count by install date.

## Value discovery

An unknown filter literal returns zero rows, not an error — call
get_dimension_values before filtering on anything unseen. Stored spellings
surprise: 'Asia Pacific' never 'APAC'; 'Viet Nam', 'Türkiye' (ISO). Prefer country__*
over asset_id__billing_country (unnormalized free text). Zero rows = suspect
spelling and scope first; only then report absence.

## Worked recipes

1. BOTS. Bot exclusion is the Insights default, built into contributor and activity
metrics (code volumes read roughly 1.8x higher with bots); bot_activities
(member_is_bot) is the explicit bot view.

2. ORG SHARES. Share of work = ACTIVITY VOLUMES, never headcounts. Compute on the
org-ATTRIBUTED base: filter activity_project_id__is_org_contribution = true — the
governed real-organization filter (no need to hand-exclude NULL rows and
'Individual - No Account') — and report the unattributed share (roughly 40-70%)
separately. The 500-row cap makes exact percentiles over big pools unretrievable.

3. ORG HEADCOUNTS run 2-4x below externally published counts (volumes reconcile
to ~1-4%). State the caveat.

4. CONTRIBUTOR POPULATIONS. total_contributors = code-only, non-bot;
total_contributors_with_collaboration adds issues/docs/chat (say so); total_activities = any activity.

5. NAME DISCOVERY. Org/account names are stored FULL LEGAL names: IBM is
'International Business Machines Corporation'; Red Hat is 'Red Hat LLC' in account
dimensions, 'Red Hat' in activity-side organization_name. Resolve via search_b2b_orgs
FIRST; empty may be permission-filtering — value-search a token ('Machines').

6. ROLLUPS ("including subsidiaries"): account__account_rollup_name folds
subsidiaries INTO parents — Red Hat's rollup value is IBM, so filtering rollup =
'Red Hat' finds almost nothing; group by the rollup or filter the PARENT. No
rollup dimension → sum named sub-entities and list them.

7. TIER LITERALS differ per foundation ('Premier Membership' vs 'Premier Member') — get_dimension_values per foundation, never reuse.

8. HEALTH SCORES are daily snapshots: find the latest health-bearing date
(health_score_category IS NOT NULL), filter to it, then aggregate; unfiltered
grouping inflates ~8-9x. Bands: Critical <20, Unsteady 20-39.

9. ECONOMIC VALUE = total_software_value / total_estimated_cost (COCOMO): non-additive
daily snapshots; totals can read low, never inflated.

10. RANKING CONTRIBUTORS BY CONTRIBUTIONS ("top contributors"). Group
code_contribution_activities by activity_project_id__member_display_name
(+ organization_name), order by the metric descending. Display names are
not identity keys — for identity-stable answers use lens (member_id); say which.

11. MAINTAINERS. One project: maintainer_key__project_slug ('k8s' returns the
real roster; cm_project_grandparents_slug = 'k8s' returns ZERO — the cm_*
rollups hold foundation ancestry, verified live). Foundations:
project__foundation_slug. Always + maintainer_key__is_lf_project = true;
active = no end date; start_date has a
2000-01-01 sentinel — never trend on it. Maintainer-by-contribution questions
("top maintainers by contributions", maintainer share of work) are person-grain
joins this layer cannot express: use query_lfx_lens.

12. ROSTERS AND MEETINGS. search_committees → search_committee_members (paginate;
group-mode names: search_groups/search_group_members). Never infer a roster from
membership or event data. Meetings: search_meetings and search_past_meetings.
Aggregations those tools cannot express (e.g. meeting attendance by company over
a period) are not answerable with governed data today: try query_lfx_lens as a
best effort and disclose there is no canonical way to compute it.

13. REGIONS. country__* follows the person; organization_lf_region etc. follow the org's HQ.

14. EVENTS/TRAINING/SPONSORSHIPS BY ORG. The account entity spans registrations,
enrollments and sponsorships: group account__account_name (or the rollup, recipe
6). Registrations count rows, not people; edX enrollments land in the NULL bucket
— present "attributed enrollments". Sponsorships: total_sponsorship_revenue (USD)
and total_sponsorship_count include ALL tier types — filter
sponsorship__sponsorship_tier_type = 'package_tier' for package-only figures
('a_la_carte' and 'billing_adjustment' are the others).

15. KPI RECIPE CALLS take uniform parameters — foundation, project, org, by
(account | rollup), since/until on FLOW recipes, as_of on SNAPSHOT ones,
order_by on the recipe's own result columns, limit. where adds a filter and is
one-hop only (<entity>__<dimension>); multi-hop paths fail at parse time,
though ad-hoc queries accept both.

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

Prefer repeatable answers: KPI recipe > named metric > lens SQL — label anything below
the top rung; struggling, re-read the recipe BEFORE any query_lfx_lens fallback.
