# LFX deck building — agent guidance

Read this before assembling or verifying numbers for customer-facing decks,
briefings and presentations (memberships, contributions, events, training,
certification, by organization). It builds on the semantic layer guidance
(read_lfx_semantic_layer_guidance) and the KPI guidance
(read_lfx_kpi_guidance) - read those first.

## Rule 1: KPI recipes first

Every recurring deck KPI has a curated recipe. Check the query_lfx_kpis
inventory and use the matching kpi_* recipe before assembling an ad-hoc
query. Recipes are governed and digit-stable: the same question re-run gives
the same number, which is what makes a deck survive verification. Only when
no recipe matches, fall back to explore_lfx_semantic_layer +
query_lfx_semantic_layer.

- Slice a recipe with its own parameters: foundation, project, org,
  since/until on FLOW recipes, as_of on SNAPSHOT ones,
  an order_by on the recipe's own result columns (- prefix for descending),
  and a limit. org names the TOP parent and returns its rows per account; a
  recipe with no parent-organization lens (kpi_maintainers_by_org) rejects
  org - filter maintainer_key__account_name in where instead.
- where adds a filter on top, one-hop <entity>__<dimension> names only
  (account__account_name); multi-hop paths are rejected.

## Deck lane → recipe mapping

- Memberships, tiers, dues, ranks per account →
  kpi_members_and_dues_by_account, kpi_membership_tier_split,
  kpi_new_members_by_year, kpi_membership_churn. Membership counts are
  deduplicated; dues sum by account.
- Contributor and maintainer leaderboards → kpi_contributions_by_org
  (volume), kpi_contributors_by_org (headcount), kpi_maintainers_by_org,
  kpi_contributors_by_project.
- Event registrations → kpi_event_registrations_by_org, windowed on the
  event start date. Registrations count rows, not unique people - present
  "registrations", not "attendees".
- Training enrollments → kpi_training_enrollments_by_org. edX enrollments
  carry no organization and land in the NULL account - present "attributed
  enrollments" and say so.
- Event sponsorships → no kpi_* recipe yet: query total_sponsorship_revenue
  (USD) / total_sponsorship_count ad hoc. Filter
  sponsorship__sponsorship_tier_type = 'package_tier' for package-only
  revenue; group account__account_name or the rollup for sponsor rankings.
- Maintainer × contribution joins (top maintainers by contributions,
  maintainer share of work) → query_lfx_lens: the semantic layer cannot
  join maintainers to activity at person grain.

The inventory in the tool description is the routing surface; the deployed
manifest is the source of truth. A "recipe does not exist" error means it is
not deployed yet, not that the name is wrong.

## Reading recipe results

The parameters and the reading contract (org names the top parent, headcounts
NOT additive, the NULL-attribution row) live in read_lfx_kpi_guidance - read
it before running the recipes.

## Combined-entity slides ("IBM including Red Hat", "Amazon including AWS")

The direction matters: rollups fold subsidiaries INTO parents, so org = a
subsidiary's name returns only what rolls up to THAT subsidiary - a small,
plausible-looking answer that excludes the subsidiary's own row, which sits
under the top parent. Always name the TOP parent; to see one subsidiary
alone, filter account__account_name in where.

A rollup grain is coming. Today org = the top parent returns that parent's
rows per account; for ADDITIVE recipes (members and dues, contribution
volume, registrations, enrollments) sum those rows client-side and label it
a client-side sum; for HEADCOUNT recipes (contributors, maintainers) no
combined figure is governed yet - say so rather than summing. Where the deck
shows the parts, present the named sub-entities (IBM and Red Hat as separate
membership counts).

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
- Working-group rosters and official project counts (owned by other teams).

## Presentation honesty

- Be honest about what each figure represents; never present a figure as
  something it is not.
- Re-run governed numbers before presenting; if a figure moved, the data
  moved - annotate the as-of date.

How figures are formatted and presented is the deck author's call.
