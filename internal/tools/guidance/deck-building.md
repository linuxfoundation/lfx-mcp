# LFX deck building — agent guidance

Read this before assembling or verifying numbers for customer-facing decks,
briefings and presentations (memberships, contributions, events, training,
certification, by organization). It builds on the semantic layer guidance
(read_lfx_semantic_layer_guidance) and the standard metrics guidance
(read_lfx_standard_metrics_guidance) - read those first.

## Rule 1: governed standard metrics first

Every recurring deck figure has a curated recipe behind
query_lfx_standard_metrics. Check the inventory in
read_lfx_standard_metrics_guidance and use the matching metric before
assembling an ad-hoc query. They are governed and digit-stable: the same
question re-run gives the same number, which is what makes a deck survive
verification. Only when no metric matches, fall back to
explore_lfx_semantic_layer + query_lfx_semantic_layer.

- ALWAYS resolve names first: project slugs from search_projects,
  organization names from search_b2b_orgs. A name they returned this session
  may be reused as is; never put a guessed slug or organization name in a
  deck query.
- Slice a metric with its own parameters: project + subprojects
  (none|separate|combined, default separate), org + subsidiaries
  (none|separate|combined, default none), since/until on FLOW metrics, as_of
  on SNAPSHOT ones, an order_by on the result columns (- prefix for
  descending), and a limit. subprojects=combined folds every project column of
  the result. maintainers_by_org has no parent-company lens, so subsidiaries
  must be none there - name one employer with org, or filter
  maintainer_key__account_name in where.
- where adds a filter on top, one-hop <entity>__<dimension> names only
  (account__account_name); multi-hop paths are rejected.

## Deck lane → recipe mapping

- Memberships, tiers, dues, ranks per organization →
  members_and_dues_by_org, membership_tiers, new_members_by_year,
  membership_churn_by_year. Membership counts are deduplicated; dues sum by
  account.
- Contributor and maintainer leaderboards → contributions_by_org (volume),
  contributors_by_org (headcount), maintainers_by_org,
  contributors_by_project.
- Event registrations → event_registrations_by_org, windowed on the event
  start date. Registrations count rows, not unique people - present
  "registrations", not "attendees".
- Training enrollments → training_enrollments_by_org. edX enrollments carry
  no organization and land in the NULL account - present "attributed
  enrollments" and say so.
- Event sponsorships → no governed metric yet: query total_sponsorship_revenue
  (USD) / total_sponsorship_count ad hoc. Filter
  sponsorship__sponsorship_tier_type = 'package_tier' for package-only
  revenue; group account__account_name or the rollup for sponsor rankings.
- Maintainer × contribution joins (top maintainers by contributions,
  maintainer share of work) → query_lfx_lens: the semantic layer cannot
  join maintainers to activity at person grain.

The inventory in read_lfx_standard_metrics_guidance is the routing surface;
the deployed manifest is the source of truth. A "does not exist" error from
the server means the recipe is not deployed yet, not that the name is wrong.

## Reading recipe results

The parameters and the reading contract (what each switch covers, headcounts
NOT additive, the NULL-attribution row) live in
read_lfx_standard_metrics_guidance - read it before running the metrics.

## Combined-entity slides ("IBM including Red Hat", "Amazon including AWS")

Name the TOP parent and set subsidiaries: separate lists the parts (IBM and
Red Hat as their own rows), combined returns the single folded row the slide
means. org alone, with the default subsidiaries=none, is that company by
itself - which is the right call for "Red Hat", and the wrong one for "IBM
including Red Hat".

ONE HOP: the parent link is a single hop, so a combined figure for a top
parent covers its DIRECT subsidiaries but not their own acquisitions -
disclose that next to the number. Headcount metrics (contributors,
maintainers) are not additive across accounts: take the parent figure from
subsidiaries=combined, never from summing rows, and for maintainers_by_org -
which has no parent-company lens yet - say no combined figure is available.

## Reconciling with official series

Official membership-team series and LFX counts differ by definition, not by
error (a "new members" official figure can sit between the LFX gross count
and the count excluding quasi-associate tiers). State the definitional delta
next to the figure; do not force numbers to match and do not silently
substitute one for the other.

Do not reconcile figures against Insights pages or collections either: those
surfaces differ by repo registration, member cleaning and curated
collections, so a mismatch is construction, not error. PCC-style reporting is
the reconciliation surface.

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
