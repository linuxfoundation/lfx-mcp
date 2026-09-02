# LFX standard metrics — agent guidance

Read this before your first query_lfx_standard_metrics call. A standard
metric is a governed recipe: its metrics, its grouping and its built-in
filters are fixed, and a caller chooses only the slice, so the same question
re-run gives the same figure. When one matches the question, prefer it over
the explore+query flow, and never rewrite it as an ad-hoc query just to sort,
filter or scope it — order_by, the scope parameters and where do that on the
governed recipe.

## Resolve names first — ALWAYS

project takes a stored project slug from search_projects; org takes an
organization's legal name, in its stored spelling, from search_b2b_orgs.
Stored spellings are not the everyday ones ('k8s', 'ptproject', 'Red Hat
LLC'). A name either tool has returned in this session may be reused as is;
what is never acceptable is passing a slug or an organization name that has
not come back from them. An unknown literal returns zero rows, not an error,
so a guess reads exactly like missing data.

## The contract

Answer four questions, then call once.

1. WHICH METRIC? Pick from the inventory below by what it answers.
2. WHICH SCOPE? project, org, both, or neither for an LF-wide figure.
3. WHAT DOES THE NAME COVER? Two switches, and their defaults differ on
   purpose — a subsidiary is a different company ("Red Hat" means Red Hat),
   a subproject is part of its project ("CNCF" means all of CNCF).
   - subprojects: none | separate | combined, DEFAULT separate.
   - subsidiaries: none | separate | combined, DEFAULT none.
   - none = the named thing's own rows only.
   - separate = it plus everything under it, rows as the metric defines them.
   - combined = it plus everything under it folded into ONE row. Without
     org, subsidiaries=combined gives one row per parent organization;
     subprojects=combined folds every project column of the result away, so
     it changes the shape of any metric that reports per project — and does
     nothing but filter on a metric that has no project column.
4. WHICH TIME? A FLOW metric takes since/until (yyyy-mm-dd) on its own time
   axis; a SNAPSHOT metric takes as_of, and today is the only as_of
   available. A trend on a SNAPSHOT metric is one call per period once
   history exists — label such readings as snapshots.

The rest: where adds a MetricFlow filter ANDed with the scope parameters, on
ONE-HOP names only ({{ Dimension('account__account_name') }} = 'Red Hat LLC'
works; a multi-hop path like event_id__project__foundation_slug is rejected);
check literals with explore_lfx_semantic_layer's get_dimension_values first.
order_by takes the result columns as they come back, - prefix for descending
(order_by=-total_contributors with limit=15 is a top-15). limit runs 1..500
and, omitted, returns EVERY row — rosters run long, so set one unless you
need the complete set.

## Inventory

Result columns are given in the vocabulary the results come back in.

| Metric | What it answers | Shape · time axis | Result columns | Caveat |
|---|---|---|---|---|
| members_and_dues_by_org | Current memberships and their list-price dues per organization | SNAPSHOT | account, parent_org, current_membership_count, current_membership_revenue | The two figures sit on different grains — never divide one by the other |
| membership_tiers | Current memberships and dues per membership tier | SNAPSHOT | tier, current_membership_count, current_membership_revenue | Tier literals differ per foundation, so scope with a foundation slug first |
| new_members_by_year | Memberships sold as new business, per year of installation | FLOW · install date | year, new_membership_count | A lapsed account that rejoins counts again; the current year is year-to-date |
| membership_churn_by_year | Memberships that ended without renewal, per year of churn | FLOW · churn date | year, churned_membership_count | The churn date is the day AFTER the term ended, so a term ending on 31 December counts in the following year |
| contributions_by_org | Code contribution volume per organization | FLOW · activity date | account, parent_org, code_contribution_activities | Additive; bots excluded; the NULL account row is unattributed work |
| contributors_by_org | Distinct code contributors per organization | FLOW · activity date | account, parent_org, total_contributors | A distinct-person count: never sum the rows |
| contributors_by_project | Distinct code contributors per project | FLOW · activity date | project, project_name, total_contributors | A distinct-person count: never sum the rows, across projects least of all |
| maintainers_by_org | Active maintainers per employer, LF projects only | SNAPSHOT | account, active_maintainers | A distinct-person count; the NULL row is maintainers with no resolved employer; no parent-company lens, so subsidiaries must be none |
| maintainers_by_project | Active maintainers per project, LF projects only | SNAPSHOT | foundation, project, project_name, active_maintainers | A distinct-person count; a person maintaining two projects counts once in each |
| maintainer_roster | Active maintainers by name, employer and role, per project (LF projects only) | SNAPSHOT | project, project_name, maintainer, account, role, active_maintainers | One row per person, project, employer and role; the metric column reads 1 on every row |

The LF-project filter is built into all three maintainer metrics: the
maintainers model also holds maintainers of non-LF projects, and they carry a
project slug too, so nothing here needs to exclude them by hand.

## Organizations: account and parent_org

Every *_by_org metric names its organization column `account`: the account
that holds the record, as Salesforce spells it. Most carry a second column,
`parent_org`, the company that account rolls up to (maintainers_by_org is the
exception — it has no parent lens, so it returns `account` alone). Red Hat
LLC is an account whose parent_org is International Business Machines
Corporation, and IBM's own direct business is another account under the same
parent. So:

- org = 'Red Hat LLC' (subsidiaries none) → Red Hat's own rows.
- org = 'Red Hat LLC', subsidiaries separate → Red Hat plus every account
  that rolls up to it, one row each.
- org = 'International Business Machines Corporation', subsidiaries combined
  → one row, IBM with Red Hat and its other subsidiaries folded in.

ONE HOP: the parent link is a single hop, so a combined figure for a top
parent covers its DIRECT subsidiaries but not their own acquisitions — an
account that rolls up to Red Hat LLC is not folded into IBM. Disclose that
next to any combined figure.

STRAY SAME-COMPANY ACCOUNTS: a company can also hold accounts that are their
own rollup parent — regional and research arms spelled with the company's
name — and those are folded into nothing. Before presenting a company figure,
search_b2b_orgs for the company's name and check for such accounts; if there
are any, name them next to the figure or add them to it deliberately, and say
which you did.

Headcount metrics (contributors, maintainers) are not additive across
accounts: never sum their rows into a parent figure — ask for the combined
row instead. maintainers_by_org has no parent-company lens, so it has no
combined row at all; say so rather than summing.

The full account-vs-rollup doctrine (the acronym trap, the NULL row) is in
read_lfx_semantic_layer_guidance.

## Projects and subprojects

project takes ONE slug, and that slug may name a project, a foundation or an
umbrella node — you are not asked to know which.

DEPTH depends on the domain, because the two families reach a subtree
differently:

- The contribution metrics (contributions_by_org, contributors_by_org,
  contributors_by_project) walk the project hierarchy, so with subprojects
  separate or combined they cover a named node's tree AT ANY DEPTH within its
  foundation — grandchildren included. In contributors_by_project the rows are
  the projects that carry the activity, and the named node's own bucket is one
  of them, so the table covers the whole subtree with nothing missing. The rows
  are still distinct people per project and never sum to a subtree total — take
  that from subprojects=combined.
- The membership and maintainer metrics reach a FOUNDATION completely, but an
  umbrella node BELOW foundation level reaches its direct children only, so a
  grandchild's rows are missing from an umbrella subtree. Say so when the node
  you scoped is an umbrella below foundation level.

subprojects=none is the node's own bucket alone, which for a foundation is
its catch-all bucket, not the foundation's projects.

Memberships attach at FOUNDATION level: a project-level filter on the
membership metrics is legitimately near-empty rather than a failed query —
scope them with the foundation's slug and say the figure is foundation-level.
Maintainers attach below the foundation as well, and contribution metrics
resolve on each activity's own project.

## Reading results

- *_by_org rows come per account with the parent alongside: read account
  rankings straight off the rows, and take parent figures from
  subsidiaries=combined rather than from a client-side sum.
- The NULL account row is unattributed work, and the NULL employer row is a
  maintainer whose employer was not resolved — never an organization, never
  folded into a parent. Report them as unattributed.
- Membership counts and membership revenue are different grains: the count is
  distinct project-account pairs with an active term, and the revenue is the
  LIST PRICE of the active membership assets, not the dues actually billed.
  Present them side by side, never as a ratio.
- maintainer_roster rows are NAMES, not identities: two maintainers who share
  a name are two rows that read as one person, and the same person appears
  once per project, employer and role. `maintainer` is a personal name —
  present it only where naming individuals is appropriate, and never as a
  contact list.
- Activities carry a parent-resolved account: a contribution row's account is
  already resolved to the parent account for that project before parent_org
  applies, so it can differ from the account on the source record. The
  crowd.dev spelling of a company ('Red Hat') is a different vocabulary from
  the Salesforce account name ('Red Hat LLC'); never mix the two in one
  answer.
- SNAPSHOT readings are the state as of the last warehouse build, not a live
  reading and not a historical one — date them by the build, and label
  historical as-of readings, once they exist, as snapshots that run high
  (departures before tracking began are not recorded).
- FLOW windows cut on US-Pacific day boundaries: since and until are days in
  US Pacific time, so a window sits a few hours off a UTC one. Say which
  window you used; do not present a figure as an exact UTC-calendar month.
- A FLOW window also drops rows with no usable timestamp, so an all-time
  figure can exceed the sum of its windows.
- Every result carries compiled_sql; quote it when an auditor asks how a
  figure was produced.

## Errors

Each rejection names the rule and the fix; correct the call rather than
retrying it unchanged.

- unknown metric: the message lists the valid names.
- since/until on a SNAPSHOT metric, as_of on a FLOW metric, or an as_of
  other than today.
- subsidiaries other than none on a metric with no parent-company lens
  (maintainers_by_org, maintainers_by_project, maintainer_roster): name one
  employer with org and present the figure as per-account, not per parent.
- an unknown subprojects/subsidiaries value, a date that is not yyyy-mm-dd,
  or a limit outside 1..500.
- an order_by field that is not one of the result columns: the message lists
  the columns you can order by.

Building a deck or briefing? Also read read_lfx_deck_building_guidance.
