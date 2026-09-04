# LFX standard metrics — agent guidance

Read this before your first query_lfx_standard_metrics call, and again
whenever a call surprises you. A standard metric is a governed recipe: its
metrics, its grouping and its built-in filters are fixed, and a caller
chooses only the grouping (by), the scope and the dates, so the same
question re-run gives the same figure. When one matches the question, prefer
it over the explore+query flow, and never rewrite it as an ad-hoc query just
to sort, filter or scope it — order_by, the scope switches and the dates do
that on the governed recipe. A standard metric reaches a project's tree and a
company's subsidiaries AT ANY DEPTH, which the explore+query flow does not
(see "Organizations" and "Projects" below).

## Resolve names first — ALWAYS

project takes a stored project slug from search_projects; org takes an
organization's legal name, in its stored spelling, from search_b2b_orgs.
Stored spellings are not the everyday ones ('k8s', 'ptproject', 'Red Hat
LLC'). A name either tool has returned in this session may be reused as is;
what is never acceptable is passing a slug or an organization name that has
not come back from them. The lens guards both: an unknown slug, or an
organization name that matches no account carrying data, is a rejection
with up to five candidates — never a zero. Pick from the candidates or go
back to the search tool; never pass the everyday name again.

## The contract

Every family takes the same parameters, and nothing per family:

| Parameter | Meaning | Default |
|---|---|---|
| metric | the family (inventory below) | required |
| by | exactly one grouping from the family's list | the family's first (total) |
| project | one slug from search_projects, exact | none = LF-wide |
| subprojects | excluded, separate or combined: what the project name covers | combined |
| org | one stored legal account name from search_b2b_orgs, exact | none |
| subsidiaries | excluded, separate or combined: what the org name covers | excluded |
| start_date | yyyy-mm-dd, a UTC calendar day | the family's window (below) |
| end_date | yyyy-mm-dd, a UTC calendar day | today (UTC) |
| period | day, week, month, quarter or year: one row per period | none = one figure |
| order_by | result columns, - prefix for descending | none |
| limit | maximum rows; the result says whether it was cut | none = every row |

There is no since, until or as_of: a call that names one is rejected with
the word to use instead. There is no free-form filter.

Answer four questions, then call once.

1. WHICH METRIC, GROUPED HOW? Pick the family from the inventory by what it
   answers, then its grouping with by: a one-figure question is by=total; a
   "which ... most" or "per ..." question is by=org, by=project, by=tier,
   by=country, by=event and so on, as the family offers. Left out, by is the
   family's first grouping. The scope supplies the other axis, so the two
   combine: "which CNCF projects does IBM work on" is contributions
   by=project with project=cncf, org=<IBM's legal name>,
   subprojects=separate; "which companies contribute to Kubernetes" is
   contributions by=org with project=k8s. "TOP CONTRIBUTORS" with nothing
   more said means INDIVIDUALS ranked by volume: contributions
   by=contributor with order_by=-code_contribution_activities and a limit —
   run it, do not ask which reading was meant. "Top maintainers" likewise is
   maintainer_contributions by=maintainer. The other readings each have a
   governed metric — name them as follow-ups, do not ask first: top
   organizations by volume = contributions by=org, by headcount =
   contributors by=org; top projects likewise by=project.
2. WHICH SCOPE? project, org, both, or neither for an LF-wide figure. A
   family whose model carries no account (speakers, project_health,
   software_value, social_mentions, social_reach) rejects org.
3. WHAT DOES THE NAME COVER? Two switches, and their defaults differ on
   purpose — a subsidiary is a different company ("Red Hat" means Red Hat),
   a subproject is part of its project ("CNCF" means all of CNCF).
   - subprojects: excluded | separate | combined, DEFAULT combined.
   - subsidiaries: excluded | separate | combined, DEFAULT excluded.
4. WHICH DATES? Two kinds of family, and the same three parameters on both.
   - A WINDOW family counts what happened between start_date and end_date
     inclusive. period adds one row per period, column `period` = the
     period's first day.
   - An AT-DATE family reports the state on end_date. With period, one row
     per period end from start_date to end_date; if end_date falls inside a
     period the last row is the state on end_date and the applied block says
     partial_last_period. start_date without period on an at-date family is
     a rejection: the state on a single day is end_date alone.

## The two kinds, with examples

WINDOW: new_members (install date), membership_churn (churn date),
contributors, contributions, contributing_organizations, participants,
maintainer_contributions (activity date), event_registrations,
event_sponsorships, speakers (the EVENT's start date), training_enrollments,
certifications (enrollment date), social_mentions, social_reach (mention
date).

- "How many contributors did CNCF have in 2025" → contributors,
  project=cncf, start_date=2025-01-01, end_date=2025-12-31.
- "Contributions per month this year" → contributions, start_date=<1 Jan>,
  period=month. Each row is one month; the last one is month-to-date.
- "New members per year" → new_members, period=year (by=year still works
  this release and means the same).

AT-DATE: memberships (the active roster on a day), maintainers (today's
roster), project_health and software_value (a daily snapshot).

- "How many members does CNCF have" → memberships, project=cncf. Today's
  state, status-based.
- "How many members did the LF have at the end of 2022" → memberships,
  end_date=2022-12-31. Any day but today is read date-based: installed on
  or before the day and not ended by it; the applied block's definition
  says so, and it reads a few percent above the status-based current count.
- "Members at each year end since 2020" → memberships,
  start_date=2020-01-01, end_date=2025-12-31, period=year: six rows, each
  the state on 31 December.
- "Maintainers over time" → maintainers, period=year: people on TODAY's
  roster with a code contribution in each year. The roster itself has no
  honest history, so maintainers with an end_date other than today is a
  rejection that says exactly this.
- project_health and software_value are read on the latest snapshot on or
  before end_date (applied.snapshot_date); the snapshot history is days
  deep, so an end_date before it is a 404, not a zero.

## Defaults and the applied block

end_date defaults to today (UTC); an explicit future end_date is honoured
and flagged includes_future_dated. start_date defaults to the family's
window: the trailing 365 days before end_date on the activity, event,
training and social families; all history on new_members and
membership_churn; the trailing year on any day or week series. A month,
quarter or year series on an at-date family with no start_date runs from
the first row of data.

Every result carries an `applied` block: metric, by, kind (window or
at-date), project, subprojects, org, subsidiaries, start_date, end_date,
period, timezone (always UTC), definition (one sentence: which stored
definition produced the figure), defaulted (the list of parameters the lens
chose), partial_last_period, includes_future_dated, truncated (limit cut
rows off), snapshot_date, coverage, engine. Read the scope and the window
from it, never from the request you think you sent. The sentence under a
figure comes from `definition` and `defaulted`. `engine` is provenance for
you and never goes in an answer. Then offer what a reader most often wants
next — the breakdown, another window, the series. Do not compare the figure
with a number from another dashboard.

## The switches, row by row

For a named project X:

| subprojects | rows read | rows returned |
|---|---|---|
| combined (default) | X plus everything under it, any depth | folded into ONE row: the project columns leave the result |
| separate | X plus everything under it, any depth | as the metric groups them, one row each: the breakdown |
| excluded | X's own bucket only, nothing under it | as the metric groups them |

For a named organization Y:

| subsidiaries | rows read | rows returned |
|---|---|---|
| excluded (default) | the Y account only | as the metric groups them |
| separate | Y plus every subsidiary under it, any depth | one row each, with parent_org alongside |
| combined | Y plus every subsidiary under it, any depth | folded into ONE row: the org columns leave the result |

With no org named, subsidiaries=combined on a by=org reading is one row per
parent organization, resolved to the top of each chain. MOST QUESTIONS WANT
BOTH the headline and the breakdown — two calls, combined then separate.
Never derive one from the other: distinct counts do not sum.

## Inventory

Result columns are given in the vocabulary the results come back in. A
series adds `period` in front; an at-date series adds `period_end` too.

| Family | Kind | by | What it answers | Result columns | Definition and caveat |
|---|---|---|---|---|---|
| memberships | at-date | total, org, tier, project, country, region | Memberships and list-price dues on end_date | [account, parent_org / tier / project, project_name / country / region,] current_membership_count, current_membership_revenue (membership_count, membership_revenue on any day but today and on a series) | Distinct project-account pairs with an active term; revenue is LIST PRICE, not dues billed — never divide one by the other; memberships attach at foundation level; country and region are the account's billing country and its LF region (region is provisional) |
| new_members | window | total, org, project | Memberships sold as new business, by install date | [account, parent_org / project, project_name,] new_membership_count | A lapsed account that rejoins counts again; all history unless start_date |
| membership_churn | window | total, org, project | Memberships that ended without renewal, by churn date | [...,] churned_membership_count | The churn date is the day AFTER the term ended, so a term ending on 31 December counts in the following year; zero-revenue and quasi-associate memberships excluded |
| contributors | window | total, org, project, country, region | Distinct code contributors | [account, parent_org / project, project_name / country / region,] total_contributors | Distinct people, bots excluded: never sum rows; country and region follow the PERSON and are known for about a third of contributors — the NULL row is the rest |
| contributions | window | total, org, project, contributor, type, platform, org_region | Code contribution volume | [account, parent_org / project, project_name / handle, contributor, account / type / platform / org_region,] code_contribution_activities | Additive, bots excluded; by=contributor is one row per GitHub identity (`handle`, a profile URL) with the display name and the resolved account; org_region is the LF region of the employer's HQ |
| contributing_organizations | window | total, project | Distinct organizations credited with a code contribution | [project, project_name,] total_contributing_organizations | Organizations from the enrichment vocabulary, not CRM accounts; a distinct count: never sum rows |
| participants | window | total, org, project | Distinct people with a code contribution OR a collaboration activity | [...,] total_contributors_with_collaboration | A superset of contributors (issues, comments, reviews count); distinct people: never sum rows |
| maintainers | at-date | total, org, project, maintainer | Active maintainers today (LF projects only); with period, today's roster active in each period | [account / foundation, project, project_name / project, project_name, maintainer, account, role,] active_maintainers | Distinct people; the NULL account row is maintainers with no resolved employer; today only unless period; by=maintainer has no series |
| maintainer_contributions | window | total, org, project, maintainer | Code contributions by people on the CURRENT maintainer roster of the activity's project | [...,] maintainer_contributions[, contributing_maintainers] | Maintainership as of the build, contributions in the window; maintainer_contributions is additive, contributing_maintainers a distinct count; "share of work" is this over contributions for the same scope and window |
| project_health | at-date | total, foundation, category, population | Projects with a health score and their mean score, on the latest snapshot on or before end_date | [foundation / category / population,] project_health_count, avg_project_health_score | LF-hosted projects unless by=population (rows lf_hosted and index); the mean is of project scores: never sum or re-average rows |
| software_value | at-date | total, foundation, population | COCOMO software value, each project's latest row on or before end_date, summed | [foundation / population,] total_software_value | USD; additive across projects at a date, never across days; a project whose latest row is a health-only day contributes nothing, so totals read low, never inflated |
| event_registrations | window | total, event, org | Accepted registrations of events starting in the window, and the distinct people behind them | [event / account, parent_org,] total_registrations, total_unique_registrants, total_checked_in_attendees | The window is the EVENT start date; registrants and attendees are distinct people by email: never sum them across rows; by=org is the registrant's account, NULL = unattributed |
| event_sponsorships | window | total, org, event | Sponsorship revenue and count of events starting in the window | [account, parent_org / event,] total_sponsorship_revenue, total_sponsorship_count | Additive; USD; all tier types |
| speakers | window | total, event | Accepted speakers of events starting in the window | [event,] total_speakers | Distinct people with an Accepted speaker status (Sessionize proposals accepted, Bevy listed speakers); rejected and in-review proposals are excluded; no org scope |
| training_enrollments | window | total, org, course | Enrollments and enrolled users by enrollment date | [account, parent_org / course,] total_enrollments, total_enrolled_users | Platform data only (TI + edX), so lifetime totals read below the official trained figure; the edX branch carries no account and sits in the NULL account row; enrolled users is a distinct count |
| certifications | window | total, org | Completed certifications by enrollment date | [account, parent_org,] total_certifications | Additive; counted at enrollment time; the edX branch has no account |
| social_mentions | window | total, project, network, sentiment | Social listening mentions, distinct authors and sentiment by mention date | [project, project_name / network / sentiment,] social_listening_mentions, social_listening_unique_authors, social_listening_positive_mentions, social_listening_negative_mentions | Mention counts are additive; unique_authors is a distinct count; neutral or unknown sentiment is in neither positive nor negative; no org scope |
| social_reach | window | total, project | Potential reach of the mentions by mention date | [project, project_name,] social_listening_total_author_followers, social_listening_avg_author_followers | The sum counts a prolific author once per mention; the average is per mention; NULL follower counts excluded; no org scope |

## Organizations: account and parent_org

Every by=org reading names its organization column `account`: the account
that holds the record, as the CRM spells it. Most carry a second column,
`parent_org`: with an org named it is the account's direct parent; without
one (a parent leaderboard) it is the top of the account's chain. ANY DEPTH:
separate and combined walk the account hierarchy to the bottom, so there is
nothing to disclose about depth on these figures. (In the explore+query flow
the same reading is account__account_rollup_name, which is one hop,
labelled ad hoc.)

LITERALS ARE EXACT. org takes the stored legal name — 'International
Business Machines Corporation', 'Red Hat LLC', 'Google LLC', 'Microsoft
Corporation' — and the everyday name is often a STRAY SAME-COMPANY ACCOUNT
carrying no memberships and no activity ('IBM', 'Google', 'Microsoft').
Those used to return zero; now they are rejected, and the rejection lists
up to five data-bearing candidates with their active memberships and
trailing-year contributions, the parent legal name first. Pick one; never
sum the stray accounts' rows into a parent figure, and never present a
candidate as the caller's own choice.

## Projects and subprojects

project takes one slug and covers, by default, its whole tree AT ANY DEPTH
(grandchildren included) folded into one figure. The default is coverage,
never permission to add rows up: a subprojects=separate table of distinct
counts covers the whole subtree with nothing missing, and its rows still
never sum to a subtree total — that is a reason to prefer the standard
metric, not something to caption. Memberships attach at FOUNDATION level, so
a leaf project's own memberships are zero by attachment, not by data loss.
An unknown slug is rejected with candidate slugs and names; 'kubernetes' is
not a slug, 'k8s' is.

## What goes in the answer

Everything in this document is working knowledge: it shapes the call and
how you read the result. The ANSWER carries the figure, one line on what it
covers (from the applied block: scope, window or date, definition), and
only the caveats that change how THIS figure is read — an unattributed row,
a distinct count that must not be summed, a partial last period, a
future-dated end, maintainership read as of today, a date-based membership
reading — in the reader's words: "ranked by GitHub identity", not a note on
display names and keys. Column names, keys, engines, SQL and the other tools
stay out unless the reader asks how a figure was made. Keep the rest in
context: vocabulary, grains and hierarchy depth are said only when they
answer the question or change the conclusion. Then offer the breakdown,
another window or the series.

## Reading results

- Start from the `applied` block: it is the scope, the dates and the
  definition that ran, and the sentence under the figure comes from it.
- Every date is a UTC calendar day, on both engines; say "UTC" only when a
  reader compares with another clock.
- `period` is the first day of each period; on an at-date series
  `period_end` is the day the state was read on. partial_last_period is set
  on both kinds and means the last row is to-date, not a full period.
- by=org rows come per account with the parent alongside: read account
  rankings straight off the rows, and take parent figures from
  subsidiaries=combined rather than from a client-side sum.
- The NULL account row is unattributed work, and the NULL employer row is a
  maintainer whose employer was not resolved — never an organization, never
  folded into a parent. Report them as unattributed. The NULL country or
  region row is people whose country is not known.
- Membership counts and membership revenue are different grains: the count is
  distinct project-account pairs with an active term, and the revenue is the
  LIST PRICE of the active membership assets, not the dues actually billed.
  Present them side by side, never as a ratio. On any day but today, and on
  a series, the reading is date-based and the columns are membership_count
  and membership_revenue; say "as of <date>" and that it is the date-based
  count.
- contributions by=contributor and maintainer_contributions by=maintainer
  rows are GitHub identities: `handle` is the stored identity (a profile
  URL), `contributor` or `maintainer` the display name, `account` the one
  the activity resolved to. Two identities sharing a display name are two
  rows. Personal names: present them only where naming individuals is
  appropriate, never as a contact list. maintainers by=maintainer is the
  roster: one row per person, project, employer and role.
- A contribution row's account is already resolved to the parent account
  for that project before parent_org applies, so it can differ from the
  source record. The enrichment spelling of a company ('Red Hat') is a
  different vocabulary from the CRM account name ('Red Hat LLC'); never mix
  the two in one answer.
- A window drops rows with no usable timestamp, so an all-time figure can
  exceed the sum of its windows.
- truncated=true means limit cut rows off: say "top N", not "all".
- Every result carries compiled_sql; quote it only when someone asks how a
  figure was produced.

## Worked calls

- One figure, window: contributors, project=cncf, start_date=2025-01-01,
  end_date=2025-12-31 → one row; the answer says "distinct code
  contributors to CNCF and its projects in 2025".
- One figure, at-date: memberships, project=cncf → today's active
  memberships; add end_date=2024-12-31 for the year-end state (date-based).
- A series: new_members, project=tlf, period=year → one row per year of
  installation, the current year to date.
- An org with subsidiaries: contributions, org=International Business
  Machines Corporation, subsidiaries=combined → IBM and everything under it
  as one figure; subsidiaries=separate for the table with parent_org.
- A rejection: memberships, start_date=2020-01-01 → "start_date needs period
  for an at-date metric; the state on a single day is end_date alone" — add
  period=year for the series, or drop start_date for one day.

## Errors

Every rejection names the rule and the fix; read it and change the call, do
not retry the same one.

- an unknown metric, or a grouping the family does not offer: the message
  lists the valid names or groupings.
- since, until or as_of: the message names start_date or end_date.
- start_date without period on an at-date family; an end_date other than
  today on maintainers without period; period on a family and grouping
  with no series yet.
- an org that matches no data-bearing account: 400 with candidates (see
  Organizations); an unknown project slug: 400 with candidates.
- org on a family whose model carries no account.
- an order_by field that is not one of the result columns (the message
  lists them, minus any column the call folds away).
- a 404 on project_health or software_value: no snapshot on or before
  end_date.
