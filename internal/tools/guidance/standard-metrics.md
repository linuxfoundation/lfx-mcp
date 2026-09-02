# LFX standard metrics — agent guidance

Read this before your first query_lfx_standard_metrics call. A standard
metric is a governed recipe: its metrics and grouping are fixed in the
warehouse, so the same question re-run gives the same figure. When one
matches the question, prefer it over the explore+query flow, and never
rewrite it as an ad-hoc query just to sort, filter or scope it — order_by,
the scope parameters and where do that on the governed recipe.

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
(order_by=-total_registrations with limit=15 is a top-15). limit runs 1..500
and, omitted, returns EVERY row — rosters run long, so set one unless you
need the complete set.

## Inventory

Result columns are given in the vocabulary the results come back in.

| Metric | What it answers | Shape · time axis | Result columns | Notes |
|---|---|---|---|---|
| members_and_dues_by_org | Current members and their annual dues per organization | SNAPSHOT | account, parent_org, current_membership_count, current_membership_revenue | Additive across accounts; dues are the list price on active terms, not cash collected |
| membership_tiers | Active members and dues per membership tier | SNAPSHOT | asset_id__membership_tier, current_membership_count, current_membership_revenue | Tier literals differ per foundation, so scope with project (a foundation slug) first |
| new_members_by_year | New memberships per calendar year, by install date | FLOW · install date | metric_time__year, new_membership_count | The current year is year-to-date |
| membership_churn_by_year | Memberships that ended with no renewal, per calendar year | FLOW · churn date | metric_time__year, churned_membership_count | The current year is partial and not comparable to a completed one |
| contributions_by_org | Code contribution volume per organization | FLOW · activity date | account, parent_org, code_contribution_activities | Additive; the NULL account row is unattributed work |
| contributors_by_org | Distinct code contributors per organization | FLOW · activity date | account, parent_org, total_contributors | NOT additive - people span accounts, so a parent figure comes from subsidiaries=combined, never from summing rows |
| contributors_by_project | Distinct code contributors per project, with its foundation | FLOW · activity date | foundation, foundation_name, project, project_name, total_contributors | NOT additive across projects; subprojects=combined folds every project column away, leaving one row |
| maintainers_by_org | Active maintainers per employer | SNAPSHOT | account, active_maintainers | NOT additive; the NULL row is an unresolved employer; no parent-company lens yet, so subsidiaries must be none |
| event_registrations_by_org | Accepted registrations per organization | FLOW · EVENT start date | account, parent_org, total_registrations | Additive; rows are registrations, not people; a window means "events in the window" |
| training_enrollments_by_org | Training and certification enrollments per organization | FLOW · enrollment date | account, parent_org, total_enrollments | Additive; edX rows carry no account and land in the NULL row, so this is a platform coverage gap, not unknown employers |

## Organizations: account and parent_org

Every *_by_org metric names its organization column `account`: the account
that holds the record. Most carry a second column, `parent_org`, the company
that account rolls up to (maintainers_by_org is the exception — it has no
parent lens yet, so it returns `account` alone). Red Hat LLC is an account
whose parent_org is International Business Machines Corporation, and IBM's
own direct business is another account under the same parent. So:

- org = 'Red Hat LLC' (subsidiaries none) → Red Hat's own rows.
- org = 'Red Hat LLC', subsidiaries separate → Red Hat plus every account
  that rolls up to it, one row each.
- org = 'International Business Machines Corporation', subsidiaries combined
  → one row, IBM with Red Hat and its other subsidiaries folded in.

ONE HOP: the parent link is a single hop, so a combined figure for a top
parent covers its DIRECT subsidiaries but not their own acquisitions — an
account that rolls up to Red Hat LLC is not folded into IBM. Disclose that
next to any combined figure.

Headcount metrics (contributors, maintainers) are not additive across
accounts: never sum their rows into a parent figure — ask for the combined
row instead. maintainers_by_org has no parent-company lens yet, so it has no
combined row at all; say so rather than summing.

The full account-vs-rollup doctrine (the acronym trap, accounts that are
their own parent, the NULL row) is in read_lfx_semantic_layer_guidance.

## Projects and subprojects

project takes ONE slug, and that slug may name a project, a foundation or an
umbrella node — you are not asked to know which. With subprojects separate or
combined the scope covers the node's own bucket, a whole foundation, and an
umbrella's direct children.

DEPTH: a foundation is covered completely; an umbrella node BELOW foundation
level reaches its direct children only, so a grandchild project's rows are
missing from an umbrella subtree. Say so when the umbrella you scoped has
grandchildren. subprojects=none is the node's own bucket alone, which for a
foundation is its catch-all bucket, not the foundation's projects.

## Where each domain attaches

Memberships and event registrations attach at FOUNDATION level almost
entirely: a project filter on those metrics is legitimately near-empty, not
a failed query — scope them with the foundation's slug and say the figure is
foundation-level. Enrollments and maintainers attach below the foundation as
well; contribution and contributor metrics resolve on each activity's own
project.

## Reading results

- *_by_org rows come per account with the parent alongside: read account
  rankings straight off the rows, and take parent figures from
  subsidiaries=combined rather than from a client-side sum.
- The NULL account row is unattributed — never an organization, never folded
  into a parent. Report it as unattributed.
- Activities carry a parent-resolved account: a contribution row's account is
  already resolved to the parent account for that project before parent_org
  applies, so it can differ from the account on the source record. The
  crowd.dev spelling of a company ('Red Hat') is a different vocabulary from
  the Salesforce account name ('Red Hat LLC'); never mix the two in one
  answer.
- SNAPSHOT readings are the state today. Historical as-of readings, once they
  exist, run high (departures before tracking began are not recorded); say so
  next to any historical figure.
- Every result carries compiled_sql; quote it when an auditor asks how a
  figure was produced.

## Errors

Each rejection names the rule and the fix; correct the call rather than
retrying it unchanged.

- unknown metric: the message lists the valid names.
- since/until on a SNAPSHOT metric, as_of on a FLOW metric, or an as_of
  other than today.
- subsidiaries other than none on maintainers_by_org: that metric has no
  parent-company lens yet. Name one employer with org, or filter
  maintainer_key__account_name in where (the filter still uses the warehouse
  name, even though the result column comes back as account), and present the
  figure as per-account.
- an unknown subprojects/subsidiaries value, a date that is not yyyy-mm-dd,
  or a limit outside 1..500.
- A recipe the server does not know is not deployed yet: fall back to
  explore_lfx_semantic_layer + query_lfx_semantic_layer and label the figure
  as not governed. Never retry the name.

Building a deck or briefing? Also read read_lfx_deck_building_guidance.
