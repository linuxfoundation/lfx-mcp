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
  (excluded|separate|combined, default combined), org + subsidiaries
  (excluded|separate|combined, default excluded), since/until on FLOW
  metrics, as_of on SNAPSHOT ones, an order_by on the result columns (-
  prefix for descending), and a limit. subprojects=combined folds every
  project column of the result; a per-project slide needs
  subprojects=separate. The maintainer metrics have no parent-company lens,
  so subsidiaries must be excluded there - name one employer with org.
- DEFAULTS are the headline reading: a project name alone is its whole tree
  as one figure, an organization name alone is that account, and a
  contribution metric with no since is the trailing 365 days. Most slides
  want the headline AND the breakdown - two calls, the second with
  subprojects=separate or subsidiaries=separate. Every result carries an
  applied block saying which scope and window ran; the caption under the
  figure comes from it.
- DEPTH: the contribution metrics cover a named node's tree at any depth
  within its foundation; the membership and maintainer metrics cover a
  foundation completely but reach an umbrella below foundation level only to
  its direct children - say so on a slide scoped to such an umbrella.
- There is no free filter on a standard metric. A slide that needs one
  (a single tier, a single role) is an explore + query slide, labelled ad
  hoc.

## Deck lane → recipe mapping

- Memberships, tiers, dues, ranks per organization →
  members_and_dues_by_org, membership_tiers, new_members_by_year,
  membership_churn_by_year. Membership counts are deduplicated; dues are the
  list price on active terms, on a different grain from the count.
- One-figure headlines ("how many contributors / maintainers does X have")
  → contributors, contributions, maintainers.
- "Top contributors" unqualified → individuals by contribution volume,
  recipe 10 of read_lfx_semantic_layer_guidance (ad hoc); offer the
  organization and project leaderboards below as the follow-ups.
- Contributor and maintainer leaderboards → contributions_by_org (volume),
  contributors_by_org (headcount), contributions_by_project,
  contributors_by_project (a company's projects: add org to either),
  maintainers_by_org, maintainers_by_project. Named maintainers per project →
  maintainer_roster (personal names: use it only where naming individuals is
  appropriate).
- Event registrations, training enrollments and sponsorships → no advertised
  standard metric: compose them with explore + query (recipe 14 in
  read_lfx_semantic_layer_guidance) and label them as ad-hoc figures.
  Registrations count rows, not unique people - present "registrations", not
  "attendees"; edX enrollments carry no organization, so an org-scoped
  enrollment figure is a floor. Sponsorships:
  total_sponsorship_revenue (USD) / total_sponsorship_count, filtered
  sponsorship__sponsorship_tier_type = 'package_tier' for package-only
  revenue; group account__account_name or the rollup for sponsor rankings.
- Maintainer × contribution joins (top maintainers by contributions,
  maintainer share of work) → query_lfx_lens: the semantic layer cannot
  join maintainers to activity at person grain.

The inventory in read_lfx_standard_metrics_guidance is the routing surface.

## Reading recipe results

The parameters and the reading contract (what each switch covers, headcounts
NOT additive, the NULL-attribution row) live in
read_lfx_standard_metrics_guidance - read it before running the metrics.

## Combined-entity slides ("IBM including Red Hat", "Amazon including AWS")

Name the TOP parent and set subsidiaries: separate lists the parts (IBM and
Red Hat as their own rows), combined returns the single folded row the slide
means. org alone, with the default subsidiaries=excluded, is that company by
itself - which is the right call for "Red Hat", and the wrong one for "IBM
including Red Hat".

ONE HOP: the parent link is a single hop, so a combined figure for a top
parent covers its DIRECT subsidiaries but not their own acquisitions -
disclose that next to the number, or ask query_lfx_lens for the whole chain
at any depth and label that slide as generated SQL. Headcount metrics (contributors,
maintainers) are not additive across accounts: take the parent figure from
subsidiaries=combined, never from summing rows, and for the maintainer
metrics - which have no parent-company lens - say no combined figure is
available.

STRAY SAME-COMPANY ACCOUNTS: a company can hold accounts that are their own
rollup parent (regional and research arms spelled with its name), and those
are folded into nothing. search_b2b_orgs for the company name before a
combined slide, and either name those accounts beside the figure or add them
deliberately - say which you did.

## Reconciling with official series

Official membership-team series and LFX counts differ by definition, not by
error (a "new members" official figure can sit between the LFX gross count
and the count excluding quasi-associate tiers). State the definitional delta
next to the figure; do not force numbers to match and do not silently
substitute one for the other.

Do not reconcile figures against other dashboards or pages either: those
surfaces differ by repo registration, member cleaning and curated lists, so a
mismatch is construction, not error. Caption the figure with exactly what it
covers (from the applied block), offer the breakdown and another window, and
leave PCC-style reporting as the reconciliation surface.

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
