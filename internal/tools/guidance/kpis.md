# LFX KPI recipes — agent guidance

Read this before your first query_lfx_kpis call. A recipe is a governed
saved query: its metrics and grouping are fixed in the warehouse, so the
same question re-run gives the same figure. When one matches the question,
prefer it over the explore+query flow.

Answer three questions, then call once:

1. Which recipe? Pick from the inventory below by what it answers.
2. Which scope? foundation, project or org, resolved with search_projects /
   search_b2b_orgs. Omit all three for an LF-wide figure.
3. Which time? A FLOW recipe takes since/until on its own time axis; a
   SNAPSHOT recipe takes as_of. A trend on a SNAPSHOT recipe is one call per
   period with as_of at the period end - label those readings as snapshots.

Never rewrite a recipe as an ad-hoc query to sort, filter or scope it:
order_by, the scope parameters and where do that on the governed recipe.

## Calling

- saved_query is the recipe name; everything else is the slice. The
  parameters are identical for every recipe (the tool description lists
  them), and a parameter the recipe cannot honour returns an error naming
  the recipe's shape rather than silently ignoring it.
- foundation and project take stored slugs from search_projects ('k8s',
  'korg'), not everyday names. org takes the legal name from
  search_b2b_orgs.
- where is an extra MetricFlow filter ANDed with the scope parameters, on
  ONE-HOP names only: {{ Dimension('account__account_name') }} = 'Red Hat LLC'
  works; multi-hop paths like event_id__project__foundation_slug are
  rejected. Check literals with explore_lfx_semantic_layer's
  get_dimension_values first - an unknown literal returns zero rows, not an
  error.
- order_by takes the recipe's own result columns, - prefix for descending:
  order_by=-total_registrations with limit=15 is a top-15. limit has a
  ceiling 500 and, omitted, returns EVERY row - rosters run long, so set one
  unless you need the complete set.

## Inventory

Each entry: what it answers · result columns (qualified names, as order_by
needs them) · shape and time axis · notes.

- kpi_members_and_dues_by_account — current members and their annual dues
  per account. account__account_name, account__account_rollup_name,
  current_membership_count, current_membership_revenue · SNAPSHOT · additive
  across accounts; dues are list price on active terms, not cash.
- kpi_membership_tier_split — active members and dues per membership tier.
  asset_id__membership_tier, current_membership_count,
  current_membership_revenue · SNAPSHOT · tier literals differ per
  foundation, so scope with foundation first.
- kpi_new_members_by_year — new memberships per calendar year, by install
  date. metric_time__year, new_membership_count · FLOW, install date · the
  current year is year-to-date.
- kpi_membership_churn — memberships that ended with no renewal, per year.
  metric_time__year, churned_membership_count · FLOW, churn date · the
  current year is partial.
- kpi_contributions_by_org — code contribution volume per account.
  account__account_name, account__account_rollup_name,
  code_contribution_activities · FLOW, activity date · additive; the NULL
  account row is unattributed work.
- kpi_contributors_by_org — distinct code contributors per account.
  account__account_name, account__account_rollup_name, total_contributors ·
  FLOW, activity date · NOT additive - people span accounts, so there is no
  governed combined figure for a parent today.
- kpi_contributors_by_project — distinct code contributors per project,
  with its foundation. project__foundation_slug, project__foundation_name,
  project__slug, project__name, total_contributors · FLOW, activity date ·
  NOT additive across projects.
- kpi_maintainers_by_org — active maintainers per employer account.
  maintainer_key__account_name, active_maintainers · SNAPSHOT · NOT
  additive; the NULL row is unresolved employer; org not accepted yet (no
  parent lens on this recipe) - use where on maintainer_key__account_name
  (exact account name) and present the figure as per-account.
- kpi_event_registrations_by_org — accepted registrations per account.
  account__account_name, account__account_rollup_name, total_registrations ·
  FLOW, event start date · additive; rows are registrations, not people.
- kpi_training_enrollments_by_org — enrollments per account.
  account__account_name, account__account_rollup_name, total_enrollments ·
  FLOW, enrollment date · additive; edX rows carry no account and land in
  the NULL row.

Windows on kpi_event_registrations_by_org run on the EVENT start date, so
since/until mean "events in the window", matching the recipe's event-year
grouping.

## Organizations: the account and its parent

Every *_by_org recipe carries two organization columns. account_name is the
Salesforce account that holds the record; account_rollup_name is the parent
company it belongs to. Red Hat LLC is an account whose rollup is
International Business Machines Corporation; IBM's own direct business is
another account under the same rollup.

The org parameter always names the PARENT, and the rows come back per
account. org = a subsidiary's name returns only what rolls up to THAT
subsidiary - a small, plausible-looking answer that excludes the
subsidiary's own row, which sits under the top parent. Always name the TOP
parent; to see one subsidiary alone, filter account__account_name in where.

Combined "including subsidiaries" figures: a rollup grain is coming. Today
org = the top parent returns that parent's rows per account; for ADDITIVE
recipes (members and dues, contribution volume, registrations, enrollments)
sum those rows client-side and label it a client-side sum; for HEADCOUNT
recipes (contributors, maintainers) no combined figure is governed yet - say
so rather than summing.

kpi_maintainers_by_org has no parent lens at all yet: org is rejected on it.
Filter maintainer_key__account_name in where with the exact account name and
present the figure as per-account, not per parent.

The full account-vs-rollup doctrine (acronym trap, accounts that are their
own rollup, the NULL row) is in read_lfx_semantic_layer_guidance.

## Reading results

- *_by_org rows come per account with the parent alongside: read account
  rankings straight off the rows. A parent total is a client-side sum of the
  rows sharing a rollup name, and only for additive recipes - label it as
  one.
- The NULL account row is unattributed - never an organization, never folded
  into a parent. Report it as unattributed.
- SNAPSHOT readings are the state today. Historical as-of readings run high
  once they exist (departures before tracking began are not recorded); say
  so next to any historical figure.
- Every result carries compiled_sql; quote it when an auditor asks how a
  figure was produced.

## Errors

- A recipe name the server does not know is not deployed yet: fall back to
  explore_lfx_semantic_layer + query_lfx_semantic_layer and label the figure
  as not governed. Never retry the name.
- org on kpi_maintainers_by_org is rejected: that recipe has no
  parent-organization lens yet. Use where on maintainer_key__account_name.
- since/until on a SNAPSHOT recipe, as_of on a FLOW recipe, and an as_of
  other than today are rejected with the recipe's shape named. Correct the
  call; do not retry it unchanged.

Building a deck or briefing? Also read read_lfx_deck_building_guidance.
