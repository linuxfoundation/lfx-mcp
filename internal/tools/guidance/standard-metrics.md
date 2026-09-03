# LFX standard metrics — agent guidance

Read this before your first query_lfx_standard_metrics call. A standard
metric is a governed recipe: its metrics, its grouping and its built-in
filters are fixed, and a caller chooses only the grouping (by) and the
slice, so the same question re-run gives the same figure. When one matches the question, prefer it over
the explore+query flow, and never rewrite it as an ad-hoc query just to sort,
filter or scope it — order_by and the scope parameters do that on the
governed recipe. A standard metric reaches a project's tree and a company's
subsidiaries AT ANY DEPTH, which the explore+query flow does not (see
"Organizations" and "Projects" below).

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

1. WHICH METRIC, GROUPED HOW? Pick the metric from the inventory below by
   what it answers, then its grouping with `by`: a one-figure question is
   by=total; a "which ... most" or "per ..." question is by=org, by=project,
   by=tier or by=year, as the metric offers; by=maintainer is the roster by
   name. Left out, by is the metric's first grouping. The scope supplies the
   other axis, so both parameters combine: "which CNCF projects does IBM
   work on" is contributions by=project with project=cncf, org=IBM,
   subprojects=separate, and "which companies contribute to Kubernetes" is
   contributions by=org with project=k8s — no ad-hoc query needed.
   "TOP CONTRIBUTORS" with nothing more said means INDIVIDUALS ranked by
   contribution volume: contributions by=contributor with
   order_by=-code_contribution_activities and a limit — run it, do not ask
   which reading was meant. "Top maintainers" likewise means
   maintainer_contributions by=maintainer. Both take the scope switches, so
   "top maintainers in CNCF from IBM" is project=cncf, org=<IBM's legal
   name>, subsidiaries=combined. The other common readings each have a
   governed metric — name them as follow-ups, do not ask first: top
   organizations by volume = contributions by=org, by headcount =
   contributors by=org; top projects by volume = contributions by=project,
   by headcount = contributors by=project.
2. WHICH SCOPE? project, org, both, or neither for an LF-wide figure.
3. WHAT DOES THE NAME COVER? Two switches, and their defaults differ on
   purpose — a subsidiary is a different company ("Red Hat" means Red Hat),
   a subproject is part of its project ("CNCF" means all of CNCF).
   - subprojects: excluded | separate | combined, DEFAULT combined.
   - subsidiaries: excluded | separate | combined, DEFAULT excluded.
4. WHICH TIME? A FLOW metric takes since/until (yyyy-mm-dd) on its own time
   axis; a SNAPSHOT metric takes as_of, and today is the only as_of
   available. SNAPSHOT: memberships and maintainers — "members since 2024"
   is not a question they answer; use new_members for arrivals. FLOW:
   new_members (install date), membership_churn (churn date), contributors,
   contributions and maintainer_contributions (activity date). A PAST
   membership count — "how many members did CNCF have in 2024", "members at
   year end by year" — is a query_lfx_lens question: ask it for memberships
   whose install date is on or before the date and whose churn date is
   after it, one row per year-end for a series, and label the answer as
   generated SQL. Do not approximate it from new_members and
   membership_churn. A past maintainers count has no lane yet.

## The switches, row by row

Each switch answers two separate questions: what the named thing COVERS, and
how the rows COME BACK.

| subprojects | project X covers | rows come back |
|---|---|---|
| combined (default) | X plus everything under it, any depth | folded into ONE row: the figure for the whole tree |
| separate | X plus everything under it, any depth | as the metric groups them, one row each: the breakdown |
| excluded | X's own bucket only, nothing under it | as the metric groups them |

| subsidiaries | org Y covers | rows come back |
|---|---|---|
| excluded (default) | the Y account only | as the metric groups them |
| separate | Y plus every subsidiary under it, any depth | as the metric groups them, one row each: the breakdown |
| combined | Y plus every subsidiary under it, any depth | folded into ONE row: the figure for the group |

Reading the tables:

- combined and separate read the SAME rows; only the shape differs. On a
  reading with no project column (by=total, by=org, by=tier, by=year)
  subprojects=combined and subprojects=separate return the same result, and
  the switch only decides whether the tree or the own bucket is covered.
- subprojects=combined folds every project column of the result away, so on
  by=project and by=maintainer it turns the table into one row. That is what the default does: a project
  name alone is ONE figure for its whole tree.
- MOST QUESTIONS WANT BOTH. "How is CNCF doing" wants the CNCF figure and the
  per-project table; "IBM's contribution" wants IBM's figure and the parts.
  Make two calls: the default for the headline, then subprojects=separate or
  subsidiaries=separate for the breakdown. Never derive one from the other.
- Without org, subsidiaries=combined gives one row per parent organization —
  a parent-company leaderboard, each company resolved to the top of its
  chain; subsidiaries=separate lists the accounts with that top parent
  alongside. maintainers by=org carries no parent column, so there it is the
  one distinct total; a company's maintainers are org + subsidiaries=combined.
- excluded is for "X itself", "excluding subprojects", "the umbrella's own
  repos". Never for a plain question.

## Defaults and the applied block

A plain question gets the default reading without any parameter beyond the
name: the whole project tree as one figure, that one account, and on the
contribution metrics the trailing 365 days. The lens applies those defaults
itself when a parameter is omitted, and every result carries an `applied`
block — metric, by, project, subprojects, org, subsidiaries, since, until,
as_of,
`defaulted`, the list of parameters the lens chose, and `engine`. Read it and
state what it says: "distinct code contributors across the CNCF project tree,
last 12 months", not "CNCF contributors". `engine` is provenance for you
(semantic_layer, or warehouse when the scope needed the lens's predefined
statement); it never goes in an answer.

Windows: since omitted on a contribution metric is the trailing 365 days;
pass since for any other period, and an explicit early since (2000-01-01)
for all time. The membership metrics are a history and read all time by
default; the maintainer metrics are snapshots and take no window.

Then offer what a reader most often wants next: the breakdown
(subprojects=separate or subsidiaries=separate) and a different window. Do
not compare the figure with a number from another dashboard or page; say
what this one covers and leave it there.

There is no free-form filter. A slice the switches and the window cannot
express (one membership tier, one maintainer role, one country) is an
explore_lfx_semantic_layer + query_lfx_semantic_layer question, and its
answer is an ad-hoc figure, labelled as such — never presented as the
governed one. order_by takes the result columns as they come back, - prefix
for descending (order_by=-total_contributors with limit=15 is a top-15).
WHAT YOU CAN ORDER BY: exactly the Result columns the inventory lists for
that metric — its metric name and its grouping columns — minus any column
the call folds away: under the default subprojects=combined the project
columns are gone, and under subsidiaries=combined the org columns are, so a
top-N per project needs subprojects=separate first. A by=total reading has
only its metric to order by. Anything else is rejected, and the message lists the
orderable columns. limit is optional and, omitted, returns EVERY row —
rosters run long, so set one unless you need the complete set.

## Inventory

Result columns are given in the vocabulary the results come back in.

| Metric | by | What it answers | Shape · time axis | Result columns | Caveat |
|---|---|---|---|---|---|
| memberships | total | Current memberships and their list-price dues over the scope, ONE figure | SNAPSHOT | current_membership_count, current_membership_revenue | The headline for "how many members does X have"; memberships attach at foundation level; the two figures sit on different grains — never divide one by the other |
| memberships | org | Current memberships and their list-price dues per organization | SNAPSHOT | account, parent_org, current_membership_count, current_membership_revenue | The two figures sit on different grains — never divide one by the other |
| memberships | tier | Current memberships and dues per membership tier | SNAPSHOT | tier, current_membership_count, current_membership_revenue | Tier literals differ per foundation, so scope with a foundation slug first |
| new_members | year | Memberships sold as new business, per year of installation | FLOW · install date | year, new_membership_count | A lapsed account that rejoins counts again; the current year is year-to-date |
| membership_churn | year | Memberships that ended without renewal, per year of churn | FLOW · churn date | year, churned_membership_count | The churn date is the day AFTER the term ended, so a term ending on 31 December counts in the following year |
| contributors | total | Distinct code contributors over the scope, ONE figure | FLOW · activity date | total_contributors | The headline for "how many contributors does X have"; trailing 365 days unless since is given |
| contributors | org | Distinct code contributors per organization | FLOW · activity date | account, parent_org, total_contributors | A distinct-person count: never sum the rows |
| contributors | project | Distinct code contributors per project | FLOW · activity date | project, project_name, total_contributors | A distinct-person count: never sum the rows, across projects least of all; the default folds it to one row — subprojects=separate for the table |
| contributions | total | Code contribution volume over the scope, ONE figure | FLOW · activity date | code_contribution_activities | Additive; bots excluded; trailing 365 days unless since is given |
| contributions | org | Code contribution volume per organization | FLOW · activity date | account, parent_org, code_contribution_activities | Additive; bots excluded; the NULL account row is unattributed work |
| contributions | project | Code contribution volume per project | FLOW · activity date | project, project_name, code_contribution_activities | Additive; bots excluded; with org it is "which projects does this company work on"; the default folds it to one row — subprojects=separate for the table |
| contributions | contributor | Code contribution volume per person, by display name — "top contributors" | FLOW · activity date | contributor, account, code_contribution_activities | Names are not identity keys; account is the one the activity resolved to; set order_by=-code_contribution_activities and a limit |
| maintainers | total | Active maintainers over the scope, ONE figure (LF projects only) | SNAPSHOT | active_maintainers | The headline for "how many maintainers does X have" |
| maintainers | org | Active maintainers per employer, LF projects only | SNAPSHOT | account, active_maintainers | A distinct-person count; the NULL row is maintainers with no resolved employer; subsidiaries=combined folds a company's maintainers into one distinct headcount |
| maintainers | project | Active maintainers per project, LF projects only | SNAPSHOT | foundation, project, project_name, active_maintainers | A distinct-person count; a person maintaining two projects counts once in each; subprojects=separate for the table |
| maintainers | maintainer | Active maintainers by name, employer and role, per project (LF projects only) | SNAPSHOT | project, project_name, maintainer, account, role, active_maintainers | One row per person, project, employer and role; the metric column reads 1 on every row; subprojects=separate keeps the project on each row |
| maintainer_contributions | total | Code contributions made by active maintainers over the scope, ONE figure | FLOW · activity date | maintainer_contributions, contributing_maintainers | Maintainership is as of the build (current roster of the activity's project), the contributions are in the window; maintainer_contributions is additive, contributing_maintainers is a distinct-person count |
| maintainer_contributions | org | Code contributions made by active maintainers, per organization | FLOW · activity date | account, parent_org, maintainer_contributions, contributing_maintainers | Same definition per employer account; the NULL row is unattributed work; "maintainer share of work" is this figure over contributions for the same scope and window |
| maintainer_contributions | project | Code contributions made by active maintainers, per project | FLOW · activity date | project, project_name, maintainer_contributions, contributing_maintainers | Same definition per project; the default folds it to one row — subprojects=separate for the table |
| maintainer_contributions | maintainer | Code contributions made by active maintainers, per maintainer by display name — "top maintainers" | FLOW · activity date | maintainer, account, maintainer_contributions | Names are not identity keys; maintainership as of the build; set order_by=-maintainer_contributions and a limit |

The LF-project filter is built into every maintainer headcount metric: the
maintainers model also holds maintainers of non-LF projects, and they carry a
project slug too, so nothing here needs to exclude them by hand.
maintainer_contributions counts a contribution when its author is on
the CURRENT maintainer roster of the project the activity belongs to;
by=maintainer ranks those people by name.

## Organizations: account and parent_org

Every by=org reading names its organization column `account`: the account
that holds the record, as Salesforce spells it. Most carry a second column,
`parent_org`: with an org named it is the account's direct parent; without
one (a parent leaderboard) it is the top of the account's chain.
maintainers by=org returns `account` alone. Red Hat LLC is an account whose
parent is International Business Machines Corporation, and IBM's own direct
business is another account under the same parent. So:

- org = 'Red Hat LLC' (subsidiaries excluded) → Red Hat's own rows.
- org = 'Red Hat LLC', subsidiaries separate → Red Hat plus every account
  under it, one row each.
- org = 'International Business Machines Corporation', subsidiaries combined
  → one row, IBM with Red Hat, Red Hat's own acquisitions and every other
  subsidiary folded in.

ANY DEPTH: separate and combined walk the account hierarchy to the bottom,
so a combined figure for a top parent covers its subsidiaries' acquisitions
too, and a parent leaderboard folds each chain into its top company. There is
nothing to disclose about depth on these figures. Only if a question asks for
DIRECT subsidiaries alone is that a different reading: explore + query with
account__account_rollup_name, which is that one hop, labelled ad hoc.

STRAY SAME-COMPANY ACCOUNTS: a company can also hold accounts that are their
own rollup parent — regional and research arms spelled with the company's
name — and those are folded into nothing. Before presenting a company figure,
search_b2b_orgs for the company's name and check for such accounts; if there
are any, name them next to the figure or add them to it deliberately, and say
which you did.

Headcount metrics (contributors, maintainers, contributing_maintainers) are
not additive across accounts: never sum their rows into a parent figure — ask
for the combined row instead, on the maintainer metrics too.

The full account-vs-rollup doctrine (the acronym trap, the NULL row) is in
read_lfx_semantic_layer_guidance.

## Projects and subprojects

project takes ONE slug, and that slug may name a project, a foundation or an
umbrella node — you are not asked to know which.

DEPTH is the same on every metric: with subprojects separate or combined a
standard metric covers the named node's tree AT ANY DEPTH within its
foundation — grandchildren included — on the membership and maintainer
metrics as much as on the contribution ones. In contributors by=project
with subprojects=separate the rows are the projects that carry the activity,
and the named node's own bucket is one of them, so the table covers the whole
subtree with nothing missing. The rows are still distinct people per project
and never sum to a subtree total — take that from by=total, or from
by=project under the default subprojects=combined. (The explore+query flow is shallower: its conformed
project entity reaches a foundation completely but a node below foundation
level only to its direct children; that is a reason to prefer the standard
metric, not something to caption.)

subprojects=excluded is the node's own bucket alone, which for a foundation
is its catch-all bucket, not the foundation's projects.

Memberships attach at FOUNDATION level: a project-level filter on the
membership metrics is legitimately near-empty rather than a failed query —
scope them with the foundation's slug and say the figure is foundation-level.
Maintainers attach below the foundation as well, and contribution metrics
resolve on each activity's own project.

## What goes in the answer

Everything in this document is working knowledge: it shapes the call and
how you read the result. The ANSWER carries the figure, one line on what it
covers (from the applied block), and only the caveats that change how THIS
figure is read — an unattributed row, a distinct count that must not be
summed, a partial year, maintainership read as of today — in the reader's
words: "ranked by the name on the record", not a note on display names and
identity keys. Column names, keys, engines, SQL and the other tools stay
out unless the reader asks how a figure was made. Keep the rest in
context: vocabulary, grains, timezone edges and hierarchy depth are said
only when they answer the question or change the conclusion. Then offer
the breakdown or another window.

## Reading results

- Start from the `applied` block: it is the scope and window that ran, and
  the sentence under the figure comes from it.
- by=org rows come per account with the parent alongside: read account
  rankings straight off the rows, and take parent figures from
  subsidiaries=combined rather than from a client-side sum.
- maintainer_contributions rows: maintainership is the roster as of the
  build, the contributions are the window's — a new maintainer carries
  their whole year. "Share of work" is maintainer_contributions over
  contributions for the same scope and window, two calls.
- The NULL account row is unattributed work, and the NULL employer row is a
  maintainer whose employer was not resolved — never an organization, never
  folded into a parent. Report them as unattributed.
- Membership counts and membership revenue are different grains: the count is
  distinct project-account pairs with an active term, and the revenue is the
  LIST PRICE of the active membership assets, not the dues actually billed.
  Present them side by side, never as a ratio.
- maintainers by=maintainer, contributions by=contributor and
  maintainer_contributions by=maintainer rows are NAMES, not identities: two
  people who share a name read as one row, one person under two spellings
  as two, and the roster lists a person once per project, employer and
  role. In the answer that is one plain clause ("by the name on the record,
  so a shared name can merge two people"), not a data note. `account` on
  the rankings is the account the activity resolved to; NULL is
  unattributed. `maintainer` and `contributor` are personal names — present
  them only where naming individuals is appropriate, never as a contact
  list.
- A contribution row's account is already resolved to the parent account
  for that project before parent_org applies, so it can differ from the
  source record. The crowd.dev spelling of a company ('Red Hat') is a
  different vocabulary from the Salesforce account name ('Red Hat LLC');
  never mix the two in one answer.
- SNAPSHOT readings are the state as of the last warehouse build, not a live
  reading and not a historical one — date them by the build.
- FLOW windows cut on US-Pacific day boundaries: since and until are days in
  US Pacific time, so a window sits a few hours off a UTC one. Say which
  window you used; do not present a figure as an exact UTC-calendar month.
- A FLOW window also drops rows with no usable timestamp, so an all-time
  figure can exceed the sum of its windows.
- Every result carries compiled_sql; quote it only when someone asks how a
  figure was produced.

## Errors

Each rejection names the rule and the fix; correct the call rather than
retrying it unchanged.

- unknown metric, or a `by` the metric does not offer: the message lists
  the valid names or groupings.
- since/until on a SNAPSHOT metric, as_of on a FLOW metric, or an as_of
  other than today (a past membership count is a query_lfx_lens question).
- an org that matches no account (separate or combined): the name was not
  the stored legal name — resolve it with search_b2b_orgs.
- an unknown subprojects/subsidiaries value, a date that is not yyyy-mm-dd,
  or a limit below 1.
- an order_by field that is not one of the result columns: the message lists
  the columns you can order by.
