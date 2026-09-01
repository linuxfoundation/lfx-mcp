# LFX saved queries — agent guidance

Read this before your first query_lfx_semantic_layer_saved_queries call.
Saved queries answer common semantic layer questions with a fixed recipe:
the same question re-run gives the same figure. When one matches the
question, prefer it over the explore+query flow.

## Catalog (name: use case)

- kpi_members_and_dues_by_account: current members and dues by account,
  as-of today (active terms)
- kpi_new_members_by_year: new members per year, YTD-bounded
- kpi_membership_tier_split: active members and dues by tier (tier literals
  differ per foundation)
- kpi_membership_churn: churned memberships per year (churn-date axis;
  current year partial)
- kpi_maintainers_by_org: active maintainers by employer on LF projects
- kpi_contributors_by_project: distinct code contributors per project
  (never sum rows - people span projects)
- kpi_contributions_by_org: code contribution VOLUME by organization
- kpi_contributors_by_org: contributor HEADCOUNT by organization (not
  additive - people span accounts)
- kpi_event_registrations: accepted registrations per event (rows, not
  people)
- kpi_event_registrations_by_org: registrations by organization
- kpi_training_enrollments: enrollments per course/certification (TI+edX
  scope, not the lifetime trained headline)
- kpi_training_enrollments_by_org: enrollments by organization (edX
  enrollments all land in the NULL account)

The tool description lists the current names; the deployed manifest is the
source of truth. If the server reports a saved query does not exist, it is
not deployed yet - fall back to query_lfx_semantic_layer, never retry.

## Mechanics

- Each recipe's metrics and grouping are fixed: the only inputs are an
  optional where filter and a limit (default 100, ceiling 500).
  There is no order_by - sort client-side.
- where uses MetricFlow syntax on ONE-HOP names only:
  {{ Dimension('project__foundation_slug') }} = '<slug>' works;
  multi-hop paths like event_id__project__foundation_slug are rejected.
- Check filter literals with explore_lfx_semantic_layer's
  get_dimension_values first: an unknown literal returns zero rows, not an
  error.

## Reading results

- *_by_org / *_by_account rows come at the (account_name,
  account_rollup_name) PAIR grain - never read the rollup column as a
  ranking directly. For rollup-grain rankings, sum rows sharing the rollup
  value client-side (additive metrics only; for contributor headcount,
  re-query grouped by rollup alone - people span accounts).
- A NULL account row is unresolved attribution, usually the largest row -
  never present it as an organization; report it as unattributed.
- Rollups fold subsidiaries INTO parents (Red Hat's rollup value is IBM):
  filter or group by the parent, never the subsidiary.

Building a deck or briefing? Also read read_lfx_deck_building_guidance.
