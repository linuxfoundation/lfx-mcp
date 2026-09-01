# LFX Semantic Layer & Lens — agent guidance

Read this once per session before your first explore_lfx_semantic_layer,
query_lfx_semantic_layer or query_lfx_lens call. The data is fluid: trap sizes
below are ratios measured live against the deployed layer; answers always come
from fresh queries, never from remembered figures.

## Which tool answers what

- explore_lfx_semantic_layer — discover metrics, their dimensions, and stored
  dimension values. Always explore before querying an unfamiliar metric.
- query_lfx_semantic_layer — governed metric queries: contributions,
  contributors, memberships, revenue, events, sponsorships, registrations,
  education, maintainers, health, by country/region. Preferred for anything a
  named metric answers.
- query_lfx_semantic_layer_saved_queries — saved queries for common
  semantic layer questions (kpi_*). When one matches, PREFER it over the
  explore+query flow: same figure every run. Building a deck or briefing?
  Read read_lfx_deck_building_guidance.
- query_lfx_lens — text-to-SQL fallback. Only for maintainer-contribution
  joins at person grain, social listening, and questions no metric family can
  express. Label lens answers as generated SQL, less repeatable.
- Committee/board/ambassador rosters: the committee tools. Meetings: the
  meeting tools. Neither lives in this layer, and neither is query_lfx_lens's
  lane (recipe 15).

## Core concepts

  metric     the number being measured
  dimension  an attribute you group, filter or list by
  entity     the key that links domains — country, project, event, organization

Because domains share entities, one query can span them: list several metrics
and group by a dimension they share; the join path derives from the shared
entity. You never write a join.

Dimension qualified_names are entity__field. The prefix is the primary key of
the metric's own table, so it differs from metric to metric
(project__foundation_slug on one, activity_project_id__member_display_name on
another). Always copy the name from explore output; NEVER assemble one by
hand.

## Calling protocol

0. Check the saved-query catalog (query_lfx_semantic_layer_saved_queries):
   when a kpi_* recipe matches the question, prefer it and skip the flow
   below - add scope with its where filter (one-hop names, recipe 18).
1. Resolve scope first. Projects and foundations: search_projects, then use
   the returned slug — stored slugs are not everyday names (Kubernetes is
   'k8s', kernel is 'korg', the PyTorch segment is 'ptproject'). Organizations:
   search_b2b_orgs, then use the stored FULL LEGAL name (recipe 6).
2. Discover: list_metrics(search) by topic word, get_dimensions for the
   group_by/where surface, get_dimension_values before filtering on any value
   you have not seen in output.
3. Query with explicit filters (syntax below).
4. State your contract in the answer: population (which scope definition),
   exact date window, and counting rule (what counts as one).

## Query syntax

  metrics   (required) comma-separated metric names, copied from explore.
  group_by  (optional) dimension qualified_names, comma-separated.
  where     (optional) one MetricFlow filter expression:
              categorical  {{ Dimension('country__lf_region') }} = 'Europe'
              time         {{ TimeDimension('metric_time','DAY') }} >= '2024-01-01'
              dates yyyy-mm-dd.
  order_by  (optional) selected group_by or metric fields; - for descending.
  limit     (optional) ceiling 500. Use 10-20 for top-N, 50-100 for breakdowns.

Multiple metrics outer-join on their shared dimensions, the only dimensions
the query may group by. A group present in only one domain has NULL for the
other. Use a name dimension for ranked lists; entities themselves return raw
IDs. Add metric_time__year (or __quarter, __month, __week, __day) for trends.
order_by '-metric' sorts NULL rows FIRST — re-sort client-side before reading
a top-N.

Pre-filtered metrics: current_* is active-only and total_contributors
excludes bots. Do not repeat those conditions.

## Scope

There is no separate project parameter; scope lives in where.

Foundation scope, any domain: {{ Dimension('project__foundation_slug') }} =
'<slug>'. This is the conformed lens: the same filter works on every metric
family and counts each row once. NEVER scope a foundation with project_slug —
the same 'cncf' literal reaches ~40x fewer activities through project_slug
(its catch-all bucket): a silent undercount, not an error.
activity_project_id__project_spine_slug returns identical activity counts
(verified) and handles sub-foundation umbrella nodes and hierarchy walks —
spine_hierarchy_level = 2 lists a foundation's direct children. Keep the
spine filter for sum metrics: insertions and deletions inflate 2-4x under
non-hierarchical filters.

Domain-specific scope dimensions:

  memberships          asset_id__project_slug
  event registrations  registration_id__project_slug
  maintainers          maintainer_key__cm_project_grandparents_slug
  health               health_metric_key__foundation_slug

For maintainers, also add is_lf_project = true to exclude non-LF projects.

Events, speakers and sponsorships have NO slug dimension. Filter
event_id__project_name with the EXACT display name returned by
get_dimension_values, for example 'Cloud Native Computing Foundation (CNCF)'.
NEVER slice sponsorship metrics by asset_id__project_slug: that returns one
NULL row containing all sponsorships.

To compare several projects or foundations, filter the correct scope
dimension with IN (...) and group_by that same dimension. Never report a
total across spine groups. Zero rows means the literal is probably
misspelled; confirm it with get_dimension_values.

"The Linux Foundation" is ambiguous: the 'tlf' slug is the umbrella
foundation's own tree, NOT the whole portfolio. For LF-wide questions, run
the query unscoped (no foundation filter) or group by foundation. State which
population you used; the two differ by 3-4x on membership counts.

SURFACE RECONCILIATION: __project_slug and __segment_slug match what an
Insights project page shows for that slug: Insights scopes every page to one
segment and never walks hierarchies. __project_spine_slug matches
PCC-style foundation rollups used in executive reporting. Pick the scope by which
surface the caller must reconcile against.

Some foundations have twin Salesforce entities: risc-v-international/riscv,
cff/cloud-foundry, opensearch-foundation/opensearch-project. If a total looks
low, group_by the slug dimension and check for a twin.

## Value discovery

Call get_dimension_values before filtering on any value you have not already
seen in output. An unknown literal is not an error: the query succeeds and
returns zero rows, indistinguishable from the data genuinely being empty.
Stored spellings are not the everyday ones: lf_region is 'Asia Pacific',
never 'APAC'; country_name is 'Viet Nam', 'Korea, Republic of', 'Türkiye' —
ISO spellings. Prefer the country__* dimensions over
asset_id__billing_country, which is unnormalized free text holding both
'Viet Nam' and 'Vietnam' alongside 'na' and 'Untied States'.

## Worked examples

  CNCF contributors, last 12 months
    metrics   total_contributors
    where     {{ Dimension('project__foundation_slug') }} = 'cncf'
              AND {{ TimeDimension('metric_time','DAY') }} >= '<start>'
              AND {{ TimeDimension('metric_time','DAY') }} < '<today UTC>'

  Kubernetes code volume (slug resolved with search_projects)
    metrics   total_code_insertions
    where     {{ Dimension('activity_project_id__project_spine_slug') }} = 'k8s'

  Compare three foundations
    metrics   total_contributors
    group_by  activity_project_id__project_spine_slug
    where     {{ Dimension('activity_project_id__project_spine_slug') }} IN ('cncf','lf-ai-foundation','openssf')

  CNCF membership count
    metrics   current_membership_count
    where     {{ Dimension('asset_id__project_slug') }} = 'cncf'

  Foundation to its projects (walk-down)
    metrics   total_contributors
    group_by  activity_project_id__project_slug
    where     {{ Dimension('activity_project_id__project_spine_slug') }} = 'lf-ai-foundation'

The walk-down is flattened: all depths appear at leaf granularity, while an
intermediate node such as cncf shows only its directly attached activity. Use
it for counts only, never sums. "Direct children of X": filter the spine slug
to X, add {{ Dimension('activity_project_id__spine_hierarchy_level') }} = 2,
and group_by the project slug.

## Worked recipes and caveats

Consult these before concluding the semantic layer cannot answer, and always
before falling back to query_lfx_lens.

1. DEFAULT WINDOW. When the asker gives no window, use the trailing 12 months
- the prior 365 complete UTC days - and say so:
{{ TimeDimension('metric_time','DAY') }} >= '<today minus 365 days>' AND
{{ TimeDimension('metric_time','DAY') }} < '<today>'. State the concrete
dates in the answer, and pass the same concrete dates into any
query_lfx_lens question so both lanes read the same window.

2. MEMBERS AS OF A DATE D (the PCC-parity recipe): metric membership_count
with where "{{ TimeDimension('metric_time','day') }} <= 'D' AND
{{ TimeDimension('asset_id__end_date','day') }} >= 'D'". Verified to
reproduce PCC Health Metrics' member counts exactly; today's active members
are current_membership_count. New members: new_membership_count by install
date - but any YTD/current-year count needs AND metric_time <= today,
because installs can be future-dated and the unbounded year bucket runs
high.

3. FOUNDATION SCOPE. {{ Dimension('project__foundation_slug') }} = '<slug>'
is the conformed scope: the same filter works on every metric family and
counts each row once. project_slug matches only a foundation's catch-all
bucket - a silent ~40x undercount on activities, never an error.
activity_project_id__project_spine_slug returns identical activity counts
(verified); spine_hierarchy_level = 2 lists a foundation's direct children.
Some families split across several Salesforce entities
(risc-v-international/riscv, cff/cloud-foundry,
opensearch-foundation/opensearch-project); if a total looks low, group by the
slug dimension and check for twins.

4. BOTS. Bot exclusion is the LFX Insights default and is built into the
contributor and activity metrics; adding {{
Dimension('activity_project_id__member_is_bot') }} = false yourself is
redundant but always safe. The gap this default closes is large - code
contributions measured roughly 1.8x higher with bots than without on a
large foundation. bot_activities is the explicit bot view when bot traffic
itself is the question.

5. ORG SHARES AND CONCENTRATION. Share of an org's work = ACTIVITY VOLUMES
(e.g. code_contribution_activities by organization), never contributor
headcounts. Compute shares on the org-ATTRIBUTED base of the same metric:
drop NULL organization rows (and 'Individual - No Account') from the
denominator and report the unattributed share separately - it is large
(roughly 40-70% depending on the metric). For concentration questions pull
the full grouped distribution and compute client-side. Always state the
population definition. PERCENTILE CAP: the query limit ceiling is 500 rows,
so a full per-person distribution over a large population is not
retrievable - for median/percentile claims over big pools, report only what
the top-500 slice actually bounds, or say the exact percentile needs ad-hoc
SQL. The total plus a top slice cannot recover a rank outside the slice.

6. NAME DISCOVERY. Org and account names are stored as FULL LEGAL names.
IBM is 'International Business Machines Corporation' - the string 'IBM' does
not appear in it. Resolve short names with search_b2b_orgs FIRST: a fuzzy
company search that returns the stored legal name. Its results are
permission-filtered, so an empty result can mean your identity cannot see
the org index, not that the org does not exist - then fall back to
discovering the name in this layer, which is trickier: a value search for
'IBM' misses the main account and finds only subsidiaries like
'Turbonomic, an IBM Company'. Search a distinctive token instead
('Machines'), or pull top values and scan. Red Hat is 'Red Hat LLC' in
account dimensions but 'Red Hat' in the activity-side organization_name.

7. CORPORATE FAMILIES AND ROLLUPS. For "including subsidiaries" questions
("IBM including Red Hat", "Amazon including AWS"), use the
account__account_rollup_name dimension where the metric carries it. THE
DIRECTION MATTERS: rollups fold subsidiaries INTO parents - Red Hat's rollup
value is IBM, so filtering rollup = 'Red Hat' finds almost nothing. Group by
the rollup, or filter it to the PARENT's name. Where no rollup dimension
exists, sum the named sub-entities explicitly and list the ones you
included.

8. TIER LITERALS differ per foundation ('Premier Membership' vs 'Premier
Member'; CNCF has 'Platinum Membership' and no Diamond tier) -
get_dimension_values per foundation, never reuse literals across foundations.

9. HEALTH SCORES are daily snapshots. Aggregate only after filtering to the
latest HEALTH-BEARING date: first find it with metrics=project_health_count,
group_by=metric_time__day, order_by='-metric_time__day', limit=1, with filter
{{ Dimension('health_metric_key__health_score_category') }} IS NOT NULL (the
fact shares dates with the software-value feed, so the unqualified max can be
a day with no health scores); then filter
{{ TimeDimension('metric_time','day') }} = that date in the real query.
Unfiltered category grouping counts a project once per day and per category
it passed through (~8-9x inflation). Category bands: Critical <20, Unsteady
20-39, then Stable/Healthy above.

10. SOFTWARE / ECONOMIC VALUE. Questions about economic value, economic
impact or the dollar value of code = total_software_value and
total_estimated_cost (COCOMO model). They are non-additive daily snapshots:
each project contributes its latest row in the queried window, so a project
whose latest row lacks a value snapshot contributes nothing - totals can
read low, never inflated.

11. ORG HEADCOUNT CAVEAT. Contributor headcounts by organization run 2-4x
below externally published counts (conservative member-to-org attribution);
contribution volumes reconcile to ~1-4%. State the caveat when reporting.

12. CONTRIBUTOR POPULATIONS. total_contributors = code contributions only,
non-bot (the Insights default) - use it unless non-code participants are
explicitly wanted, then total_contributors_with_collaboration (adds
issues/docs/chat) and say so. Neither counts passive activity (stars,
forks); total_activities is the any-activity volume.

13. PERSON-GRAIN CONTRIBUTOR RANKINGS ("top contributors"). Group
code_contribution_activities by activity_project_id__member_display_name
(add activity_project_id__organization_name for affiliation), scoped and
windowed as usual, order_by the metric descending. Display names are labels,
not identity keys - two people can share one, and some records carry partial
names. For identity-stable person answers (dedup, cross-project identity,
linking to profiles), fall back to query_lfx_lens, whose member_id is the
stable key. Say which lane you used.

14. MAINTAINERS. Scope with maintainer_key__cm_project_grandparents_slug
plus {{ Dimension('maintainer_key__is_lf_project') }} = true. Active = no
recorded end date. start_date carries a 2000-01-01 sentinel on most
historical records (start unknown) - never build a trend on it.
Person-grain maintainer x activity joins are not expressible here: that is
query_lfx_lens's lane.

15. BOARDS, TOCs, TACs, AMBASSADORS, MEETINGS. Governance and committee
rosters (who sits on a board, who chairs a TOC/TAC, how many ambassadors a
foundation has) live in the committee tools - search_committees to find the
body, then search_committee_members with its committee_uid, paginating until
page_token is absent (group-terminology deployments name these
search_groups/search_group_members, with group_uid). They are NOT in this
layer and not query_lfx_lens's lane. Member records carry name, organization,
role and voting status but no country. Never infer a roster from membership
tiers or event data - fabricated board seats have been reported as fact.
Meetings likewise: schedules, occurrences, registrants, attendance and
summaries live in the meeting tools (search_meetings, search_past_meetings
and friends). Meeting ATTENDANCE aggregated by company over a period is
not answerable with governed data today at any grain - say so rather than
improvising a number.

16. REGION LENSES. country__* dimensions follow the person (contributor);
organization-side region analysis uses the organization_* dimensions
(organization_lf_region etc.) - the organization's HQ country, not its
contributors' countries.

17. EVENTS AND TRAINING BY ORG. Event registration and training enrollment
metrics carry account__ dimensions - group by account__account_name (or
account__account_rollup_name, recipe 7) for by-organization slices.
Registrations count rows, not unique people; edX training enrollments carry
no organization and land in the NULL bucket - present "attributed
enrollments" and say so.

18. SAVED-QUERY FILTERS are one-hop only: where clauses on
query_lfx_semantic_layer_saved_queries accept <entity>__<dimension> names
(project__foundation_slug), never multi-hop paths
(event_id__project__foundation_slug fails at parse time). Ad-hoc queries
accept both.

## Honesty rules

- Zero rows: suspect filter spelling and scope first, verify the stored
  value with get_dimension_values, and only then report absence.
- Prefer repeatable answers: saved query > named metric > lens SQL. Label
  anything below the top rung.
- State population, window and counting rule with every figure.
