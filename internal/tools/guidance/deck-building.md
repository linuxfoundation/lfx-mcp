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
  metrics (new_members, membership_churn, contributors, contributions,
  maintainer_contributions), as_of on SNAPSHOT ones (memberships,
  maintainers; today only — a members-in-2024 or members-by-year slide is
  a query_lfx_lens question, labelled generated SQL), an order_by on the result columns (-
  prefix for descending), and a limit. subprojects=combined folds every
  project column of the result; a per-project slide needs
  subprojects=separate. The maintainer metrics take subsidiaries too: a
  company's maintainers as one distinct headcount is subsidiaries=combined.
- DEFAULTS are the headline reading: a project name alone is its whole tree
  as one figure, an organization name alone is that account, and a
  contribution metric with no since is the trailing 365 days. Most slides
  want the headline AND the breakdown - two calls, the second with
  subprojects=separate or subsidiaries=separate. Every result carries an
  applied block saying which scope and window ran; the caption under the
  figure comes from it.
- DEPTH: every standard metric covers a named node's tree at any depth
  within its foundation and a company's subsidiaries at any depth - nothing
  to caption. Only an explore + query slide is shallower (one hop on
  accounts, one level below a project), so keep hierarchy slides on the
  standard metrics.
- There is no free filter on a standard metric. A slide that needs one
  (a single tier, a single role) is an explore + query slide, labelled ad
  hoc.

## Deck lane → recipe mapping

- Memberships, tiers, dues, ranks per organization →
  memberships by=org, memberships by=tier, new_members by=year,
  membership_churn by=year; the headline count is memberships by=total.
  Membership counts are deduplicated; dues are the
  list price on active terms, on a different grain from the count.
- One-figure headlines ("how many contributors / maintainers does X have")
  → contributors, contributions, maintainers, memberships, each by=total
  (the default).
- "Top contributors" unqualified → individuals by contribution volume:
  contributions by=contributor, order_by=-code_contribution_activities,
  limit 10-20 (personal names); offer the organization and project
  leaderboards below as the follow-ups.
- Contributor and maintainer leaderboards → contributions by=org (volume),
  contributors by=org (headcount), contributions by=project,
  contributors by=project (a company's projects: add org to either),
  maintainers by=org, maintainers by=project. Named maintainers per project →
  maintainers by=maintainer (personal names: use it only where naming individuals is
  appropriate).
- Event registrations, training enrollments and sponsorships → no standard
  metric: compose them with explore + query (recipe 14 in
  read_lfx_semantic_layer_guidance) and label them as ad-hoc figures.
  Registrations count rows, not unique people - present "registrations", not
  "attendees"; edX enrollments carry no organization, so an org-scoped
  enrollment figure is a floor. Sponsorships:
  total_sponsorship_revenue (USD) / total_sponsorship_count, filtered
  sponsorship__sponsorship_tier_type = 'package_tier' for package-only
  revenue; group account__account_name or the rollup for sponsor rankings.
- Meeting attendance by company, committee or meeting type over a period →
  explore + query on attendees_count / invited_count (recipe 12 in
  read_lfx_semantic_layer_guidance), labelled ad hoc: attendances, not unique
  people; organizations are raw account names with no rollup, and the '' and
  'Individual - No Account' buckets are unattributed.
- Maintainer × contribution figures (contributions made by maintainers per
  project or organization, maintainer share of work) → maintainer_contributions
  by=project or by=org, over contributions for the same scope; "top
  maintainers by contributions" as named PEOPLE → maintainer_contributions
  by=maintainer, with the same project and org switches.

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

ANY DEPTH: combined walks the account hierarchy to the bottom, so a top
parent's figure covers its subsidiaries' own acquisitions as well; a slide
that wants DIRECT subsidiaries alone is an explore + query slide on
account__account_rollup_name, labelled as such. Headcount metrics
(contributors, maintainers, contributing_maintainers) are not additive across
accounts: take the parent figure from subsidiaries=combined, never from
summing rows.

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

- Working-group rosters and official project counts (owned by other teams).
- Membership counts as of a past date, or at year end by year: ask for
  memberships installed on or before the date and churning after it.

## Presentation honesty

- Be honest about what each figure represents; never present a figure as
  something it is not.
- Re-run governed numbers before presenting; if a figure moved, the data
  moved - annotate the as-of date.
- Caveats go on a slide only when they change how that slide's figure is
  read, in the audience's words — no field names, keys, SQL or tool names;
  the rest is working knowledge, kept in context.

How figures are formatted and presented is the deck author's call.
