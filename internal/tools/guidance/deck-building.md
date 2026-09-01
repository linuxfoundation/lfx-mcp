# LFX deck building — agent guidance

Read this before assembling or verifying numbers for customer-facing decks,
briefings and presentations (memberships, contributions, events, training,
certification, by organization). It builds on the semantic layer guidance
(read_lfx_semantic_layer_guidance) and the saved-query guidance
(read_lfx_saved_queries_guidance) - read those first.

## Rule 1: saved queries first

Every recurring deck KPI has a curated recipe. Check the
query_lfx_semantic_layer_saved_queries catalog and use the matching kpi_*
recipe before assembling an ad-hoc query. Saved queries are governed and
digit-stable: the same question re-run gives the same number, which is what
makes a deck survive verification. Only when no recipe matches, fall back to
explore_lfx_semantic_layer + query_lfx_semantic_layer.

- Saved queries take a where filter and limit only - no order_by; sort
  client-side.
- where filters must use one-hop <entity>__<dimension> names
  (project__foundation_slug); multi-hop paths are rejected.

## Deck lane → recipe mapping

- Memberships, tiers, dues, ranks per account →
  kpi_members_and_dues_by_account, kpi_membership_tier_split,
  kpi_new_members_by_year, kpi_membership_churn. Membership counts are
  deduplicated; dues sum by account.
- Contributor and maintainer leaderboards → kpi_contributions_by_org
  (volume), kpi_contributors_by_org (headcount), kpi_maintainers_by_org,
  kpi_contributors_by_project.
- Event registrations → kpi_event_registrations,
  kpi_event_registrations_by_org. Registrations count rows, not unique
  people - present "registrations", not "attendees".
- Training enrollments → kpi_training_enrollments,
  kpi_training_enrollments_by_org. edX enrollments carry no organization and
  land in the NULL account - present "attributed enrollments" and say so.
- Maintainer × contribution joins (top maintainers by contributions,
  maintainer share of work) → query_lfx_lens: the semantic layer cannot
  join maintainers to activity at person grain.

The catalog in the tool description is the routing surface; the deployed
manifest is the source of truth. A "saved query does not exist" error means
it is not deployed yet, not that the name is wrong.

## Reading saved-query results

The mechanics and reading contract (pair grain, rollup re-grouping,
headcounts NOT additive, the NULL-attribution row) live in
read_lfx_saved_queries_guidance - read it before running the recipes.

## Combined-entity slides ("IBM including Red Hat", "Amazon including AWS")

Group by account_rollup_name for the combined figure. The direction matters:
rollups fold subsidiaries INTO parents - Red Hat's rollup value is IBM, so
filtering rollup = 'Red Hat' finds almost nothing; filter or group by the
parent. Where the deck shows the parts, also present the named sub-entities
(IBM and Red Hat as separate membership counts). Verified live: the
Amazon-including-AWS dues and membership figures reproduce deck digits
exactly through the rollup.

## Reconciling with official series

Official membership-team series and LFX counts differ by definition, not by
error (a "new members" official figure can sit between the LFX gross count
and the count excluding quasi-associate tiers). State the definitional delta
next to the figure; do not force numbers to match and do not silently
substitute one for the other.

## Outside the semantic layer's scope

These have no governed lane. Try query_lfx_lens for them and label the
result as generated SQL, not a governed figure; if lens cannot answer, say
so rather than improvising a number:

- Meeting attendance by company over a period.
- Event sponsorship revenue totals (primary source: the events CRM).
- Working-group rosters and official project counts (owned by other teams).

## Presentation honesty

- Be honest about what each figure represents; never present a figure as
  something it is not.
- Re-run governed numbers before presenting; if a figure moved, the data
  moved - annotate the as-of date.

How figures are formatted and presented is the deck author's call.
