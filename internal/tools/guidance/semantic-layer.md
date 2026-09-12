# LFX Semantic Layer & Lens — agent guidance

Read this once per session before your first explore_lfx_semantic_layer,
query_lfx_semantic_layer or query_lfx_lens call. Answer from fresh queries only.

A metric is measured; a dimension groups or filters it; entities link domains, so one
query spans domains by grouping on a shared dimension — you never write a join.
Dimension qualified_names are entity__field, prefix per metric — copy from explore, NEVER assemble.

## Routing

- When a family in the standard-metrics inventory answers the question,
  call query_lfx_standard_metrics directly; do not explore this layer or the
  lens first to see what is there. The families are governed metrics named
  in plain words (contributors by=org, memberships by=tier...). Inventory:
  read_lfx_standard_metrics_guidance.
- explore_lfx_semantic_layer discovers metrics, dimensions and stored values; query_lfx_semantic_layer runs the query. Explore first.
- query_lfx_lens (text-to-SQL): cross-domain joins no standard metric or
  dimension expresses — label its answers as generated SQL. Membership
  counts as of a past date or by year, social listening aggregates, event,
  training and health figures, and people rankings (top contributors, top
  maintainers) are standard metrics, not lens questions.
- Committee/board/ambassador rosters: committee tools. Meeting lists and one meeting's details: meeting tools. Counts of meetings and participants with the caller's visibility: count_lfx_resources and search_past_meeting_participants (recipe 12). Meeting TOTALS over a period (occurrences, scheduled minutes, unique attendees, attendances) are in this layer (recipe 12).
- How many projects a foundation or parent has: this layer's project metrics count the authoritative project directory; search_projects and count_lfx_resources count only projects onboarded into LFX v2 and can be lower — use the tools to resolve names and slugs, the layer for the number.
- Where this layer and the standard metrics read differently (both are
  right; say which one you used): dates are UTC calendar days on the
  standard metrics and the session clock on ad hoc windows here, so a window
  can differ by a day's activity; an unknown literal is zero rows here and a
  rejection with candidates there; maintainers here count the whole index
  unless filtered to LF projects, the family counts LF projects only; event
  registrations here group by registration date unless you pick the event
  start date, the family uses the event start date; speakers here include
  every proposal status unless filtered to Accepted, the family counts
  Accepted only; placeholder accounts ('Individual - No Account',
  'TI Account') appear as accounts here and are folded into the NULL
  account row there.

## Protocol

0. If a standard metric matches, prefer it (scope and window via its own
   parameters, recipe 15; never go ad hoc just to sort, filter or scope).
1. ALWAYS resolve names first: search_projects for project slugs ('k8s', 'korg',
   'ptproject' — stored slugs are not everyday names), search_b2b_orgs for org
   legal names (recipe 5). A name either tool returned in this session may be
   reused as is; never filter, scope or call a standard metric with a slug or
   an organization name that has not come back from them.
2. Discover: list_metrics(search) → get_dimensions → get_dimension_values before
   filtering on any value you have not seen in output.
3. Query; state population, exact date window, and what counts as one in answers.

## Query syntax

  metrics   (required) CSV, copied from explore
  group_by  dimension qualified_names; metric_time__year (or __quarter, __month,
            __week, __day) for trends
  where     one MetricFlow expression, dates yyyy-mm-dd:
            {{ Dimension('country__lf_region') }} = 'Europe' AND {{ TimeDimension('metric_time','DAY') }} >= '2024-01-01'
  order_by  '-metric' descending — NULL rows sort FIRST; re-sort client-side
  limit     optional (10-20 top-N, 50-100 breakdowns); omitted = EVERY row,
            and full rosters run to thousands — set one unless you need them all

Multiple metrics outer-join on their shared dimensions (the only valid group_by
set); missing sides show NULL. Name dimensions give ranked lists; bare entities
return raw IDs. current_* is active-only; total_contributors excludes bots.

## Scope

There is no separate project parameter; scope lives in where. Pick the slug
dimension by what the question names:

- A WHOLE FOUNDATION (CNCF, LF AI & Data, OpenSSF...):
  {{ Dimension('project__foundation_slug') }} = '<slug>' — the conformed lens:
  works on every metric family, counts each row once. NEVER use project_slug for
  a foundation: it matches only the foundation's catch-all bucket, a silent
  severe undercount on activities. The one exception is the umbrella itself: "The
  Linux Foundation" as a whole is LF-wide — no project filter at all (or group
  by foundation to show the split). The 'tlf' slug is the umbrella's own
  bucket, not the LF-wide scope; state which population you used.
- A SINGLE PROJECT (k8s, pytorch...): activity_project_id__project_slug — the
  per-project surface (__project_slug and __segment_slug), whose DEFINITION
  is code contributions, bots excluded. Do NOT reconcile figures against other
  dashboards or pages: they differ by repo registration, member cleaning and
  curated lists; say what this figure covers, and PCC-style reporting is the
  reconciliation surface. For leaf projects the spine slug returns identical
  counts; for umbrella nodes project_slug is only the node's own bucket.
- SUBTREES AND HIERARCHY WALKS (umbrella nodes, "foundation to its projects"):
  activity_project_id__project_spine_slug — identical totals to foundation_slug
  (verified) and the surface PCC-style foundation rollups reconcile against.
  spine_hierarchy_level = 2 lists direct children
  ("Direct children of X": spine slug = X + level 2, group by project slug);
  on non-activity metrics a subtree is project__project_path LIKE '%/<slug>/%'.
- SUM METRICS (insertions, deletions): ALWAYS the spine filter — non-hierarchical
  filters inflate them severalfold. Walk-downs are flattened: counts only, never sums.
- ATTACHMENT LEVELS: memberships and event registrations attach at FOUNDATION
  level almost entirely, so a project-level filter on them is legitimately
  near-empty rather than a failed query — scope them by foundation and say the
  figure is foundation-level. Enrollments and maintainers attach below the
  foundation too.
- Domain scope dimensions: memberships asset_id__project_slug; maintainers
  maintainer_key__project_slug (ALWAYS + maintainer_key__is_lf_project = true, see
  recipe 11; foundations via
  project__foundation_slug — the cm_*_slug rollups are NOT project keys);
  health health_metric_key__foundation_slug. Events, registrations, sponsorships
  and training carry the conformed project entity — scope them with
  project__foundation_slug / project__slug (event_id__project_name also works but
  needs the EXACT stored display name).
- Twins exist (risc-v-international/riscv, cff/cloud-foundry,
  opensearch-foundation/opensearch-project): low total → group by the slug.
  Compare entities with IN (...) + group_by; never total across spine groups.
- REACH — how deep this layer's own dimensions go: the whole company at any
  depth is account__top_parent_name (account__account_id_path LIKE
  '%/<id>/%' for exact identity); account__account_rollup_name is ONE hop,
  for direct subsidiaries only. The whole subtree on any model is
  project__project_path LIKE '%/<slug>/%' (project__project_depth for
  levels); project__parent_project_slug is one level. Speakers and meeting
  attendance carry no account entity.

## Windows

Day boundaries are US-Pacific for every TimeDimension filter: a 'DAY' bound cuts
at midnight US Pacific, not UTC, so a window sits a few hours off a UTC one —
state the window, never claim an exact UTC calendar month.

Default is the trailing 12 months (the prior 365 complete days); state the concrete
dates and reuse them in any lens question. YTD needs AND metric_time <= today —
installs can be future-dated. Members as of date D: membership_count with
metric_time <= 'D' AND asset_id__end_date >= 'D' (end_date is never NULL;
open-ended terms carry a far-future placeholder); never churn_date, which
is derived from a different end column and undercounts. Today's actives
are current_membership_count; new members = new_membership_count by install date.

## Value discovery

An unknown filter literal returns zero rows, not an error — call
get_dimension_values before filtering on anything unseen. The "did you mean"
list an unknown dimension name returns is MetricFlow's fuzzy match and can omit
the right name — get_dimensions(search=) is authoritative. Stored spellings
surprise: 'Asia Pacific' never 'APAC'; 'Viet Nam', 'Türkiye' (ISO). Prefer country__*
over asset_id__billing_country (unnormalized free text). Zero rows = suspect
spelling and scope first; only then report absence.

## Worked recipes

1. BOTS. Bot exclusion is the default, built into contributor and activity
metrics (code volumes read noticeably higher with bots); bot_activities
(member_is_bot) is the explicit bot view.

2. ORG SHARES. Share of work = ACTIVITY VOLUMES, never headcounts. Compute on the
org-ATTRIBUTED base: filter activity_project_id__is_org_contribution = true — the
governed real-organization filter (no need to hand-exclude NULL rows and
'Individual - No Account') — and report the unattributed share separately (it
is large). Account-attributed rows are a SUPERSET of is_org_contribution; the
difference is exactly the Individual placeholder accounts, so a numerator
filtered on an account rollup sits inside this base.

3. ORG HEADCOUNTS run well below externally published headcounts (volumes
reconcile far more closely). State the caveat.

4. CONTRIBUTOR POPULATIONS. total_contributors = code-only, non-bot;
total_contributors_with_collaboration adds issues/docs/chat (say so); total_activities = any activity.

5. NAME DISCOVERY. Org/account names are stored FULL LEGAL names: IBM is
'International Business Machines Corporation'; Red Hat is 'Red Hat LLC' in account
dimensions, 'Red Hat' in activity-side organization_name. Resolve via search_b2b_orgs
FIRST; empty may be permission-filtering — value-search a distinctive token of
the legal name ('Machines') on account__account_rollup_name.

6. ACCOUNT vs ROLLUP ("including subsidiaries"). account__account_name is the
account holding the record; account__account_rollup_name is its parent, and
rollups fold subsidiaries INTO parents — so filtering the rollup on a
SUBSIDIARY's name returns only what rolls up to THAT subsidiary: a small,
plausible-looking answer that excludes the subsidiary's own row, which sits
under the top parent (Red Hat LLC is itself a rollup parent, while its own row
sits under International Business Machines Corporation). Always filter the TOP
parent, or group by account__top_parent_name; to see one subsidiary alone filter
account__account_name. Legal names do not contain the acronym:
value-searching 'IBM' finds accounts SPELLED with it, mostly regional or
stray accounts that are their OWN rollup and NOT folded under the parent —
present those separately rather than as part of it. Additive metrics (dues,
volumes) may be summed across accounts sharing a rollup; headcounts may NOT —
re-read them at rollup grain. No rollup dimension → sum named sub-entities
and list them. ANY DEPTH: account__top_parent_name folds every subsidiary
into the top parent (Red Hat LLC's own acquisitions land under IBM);
account__account_rollup_name is one hop — use it only for direct
subsidiaries. On ACTIVITIES the
account is already resolved to the parent account FOR THAT PROJECT before the
rollup applies, so a contribution row's Salesforce account can differ from the
account on the source record; organization_name stays the crowd.dev spelling
and the two vocabularies never mix in one answer.

7. TIER LITERALS differ per foundation ('Premier Membership' vs 'Premier Member') — get_dimension_values per foundation, never reuse.

8. HEALTH SCORES are daily snapshots: find the latest health-bearing date
({{ Dimension('health_metric_key__has_health_score_v2') }} = true), filter to
it, then aggregate; unfiltered grouping inflates severalfold. Categories
are the stored v2 band names (Excellent, Healthy, Fair, Concerning, Critical):
group by them, never by a threshold; the v2 category can be NULL on a scored
row, so scored projects are counted on the flag, never on the label. For
"current" or "today" health with no date, current_project_health_count,
current_avg_health_score and current_software_value (on
silver_fact_project_health_latest) read each project's own latest snapshot
without a pin — a different population from the pinned day's (each project on
its own date, so "current" is not "today's reading") — and the WHOLE
population, LF-hosted plus the open-source index, unless filtered on
project_health_latest_id__is_lf_project or
project_health_latest_id__foundation_slug; a bare current_* figure is never
"LF project health". Any dated question stays on the daily fact with the pin.

9. ECONOMIC VALUE = total_software_value / total_estimated_cost (COCOMO): non-additive
daily snapshots; totals can read low, never inflated.

10. RANKING CONTRIBUTORS BY CONTRIBUTIONS ("top contributors"). Unqualified,
this means INDIVIDUALS by contribution volume — run it, do not ask: the
standard metric contributions by=contributor (order_by
-code_contribution_activities, limit) returns people by display name with
the account the activity resolved to, scope switches included; "top
maintainers" is maintainer_contributions by=maintainer. Compose it here
(code_contribution_activities by activity_project_id__member_display_name,
spine-scoped, trailing 365 days) only for a slice the switches cannot
express. Display names are not identity keys — for identity-stable answers
use lens (member_id); say which. Offer the governed readings as follow-ups:
organizations by volume (contributions by=org) or headcount
(contributors by=org), projects by volume (contributions by=project) or
headcount (contributors by=project).

11. MAINTAINERS. One project: maintainer_key__project_slug ('k8s' returns the
real roster; cm_project_grandparents_slug = 'k8s' returns ZERO — the cm_*
rollups hold foundation ancestry, verified live). Foundations:
project__foundation_slug. Employer: account__account_name /
account__top_parent_name. ALWAYS add maintainer_key__is_lf_project = true: the
maintainers model also holds maintainers of non-LF projects crowd.dev tracks
(about half the rows) and they carry a project slug too, so a slug filter alone
does not exclude them; the maintainer standard metrics have this filter
built in. Active = no end date; start_date has a
2000-01-01 sentinel — never trend on it. As of date D: total_maintainers where
maintainer_key__start_date <= 'D' AND (end_date IS NULL OR end_date >= 'D');
since/until on start_date is meaningless, readings before tracking began run
high, and a trend is one as-of reading per period. Maintainer×contribution
figures are not in this layer: contributions made by maintainers per project
or per organization, and the maintainer share of work, are the standard
metric maintainer_contributions (by=project or by=org; the share is over
contributions for the same scope); "top maintainers by contributions" as
PEOPLE is maintainer_contributions by=maintainer.

12. ROSTERS AND MEETINGS. search_committees → search_committee_members (paginate;
group-mode names: search_groups/search_group_members). Never infer a roster from
membership or event data. Meeting LISTS and one meeting's details:
search_meetings and search_past_meetings. Meeting TOTALS over a period are in
this layer on two models. OCCURRENCES (one row per meeting occurrence):
meeting_occurrences (distinct occurrences held) and scheduled_meeting_minutes
(sum of the scheduled duration — join and leave times are not recorded, so say
"scheduled minutes", never time spent); meeting_and_occurrence_id__has_attendance
= true keeps only occurrences with at least one attendee; time axis metric_time
= the meeting date. ATTENDANCE (one row per invitee per meeting occurrence):
unique_attendees (distinct PEOPLE who attended — identity is the LF user id,
else the e-mail), attendees_count (attendance RECORDS where the invitee attended
— one person at ten meetings counts ten), invited_count (invited records),
unverified_attendee_count; time axis metric_time = the meeting date. Say
"attendees" only for unique_attendees and "attendances" for attendees_count;
attendance rate is attendees_count over invited_count on the same slice
(walk-ins attend uninvited, so the rate can exceed 1 on a small slice). SLICES
on the attendance model: primary_key__meeting_type (carries both a 'None'
literal and NULL — both are untyped), primary_key__committee_type,
primary_key__committee_name, primary_key__meeting_name, primary_key__invitee_role,
primary_key__invitee_voting_status. PROJECTS on both models through the
conformed entity: project__foundation_slug for a foundation, project__slug for
one project, project__project_path LIKE '%/<slug>/%' for a subtree; an
occurrence shared by several projects is attributed to one of them.
ORGANIZATIONS (attendance model only): primary_key__account_name is the
invitee's account as the source spelled it — there is no account entity, so no rollup,
no subsidiaries, and recipe 6's acronym trap applies; two buckets are not
companies: '' (no account) and 'Individual - No Account' — report both as
unattributed. No standard metric covers meetings: compose them here with these
names. TWO ROUTES, TWO DEFINITIONS. search_past_meetings returns a paged listing. count_lfx_resources provides meeting counts and search_past_meeting_participants with count_only=true provides participant counts; both count what the caller's identity may see, the same visibility as LFX Self Serve, and say whether the count is complete; this layer's meeting models count every meeting in the warehouse with no per-caller visibility. The two differ by design and neither is wrong. Cite a tool figure as 'meetings (or participants) visible to you' and a layer figure as 'all meetings in the warehouse'; never reconcile one against the other; prefer the tools for a project's or committee's own meeting list, details and caller-visible counts, and the layer for totals over a period — occurrences, scheduled minutes, attendees, attendances — LF-wide or by foundation, project subtree, company, committee type or meeting type. The meeting tools take date_from and date_to, inclusive, with date-only values read as UTC day boundaries; the standard metrics say the same thing as start_date and end_date. Seats for an organisation come from get_org_committee_seats, complete for the scope and gated on the organisation grant; the committee models in this layer carry no per-user access and are not the route for an organisation's seats.

13. REGIONS. country__* follows the person; organization_lf_region etc. follow the org's HQ.

14. EVENTS/TRAINING/SPONSORSHIPS. The standard metrics event_registrations,
event_sponsorships, speakers, training_enrollments and certifications cover
the common readings (by total, event, org, course) — prefer them. Compose
here only for a slice they lack. METRICS: total_registrations counts
ACCEPTED registrations only; total_enrollments counts enrollment records only
(the source table is mostly other lifecycle events, and the metric filters
them out) — neither needs a status filter of your own. Sponsorships:
total_sponsorship_revenue (USD) and total_sponsorship_count include ALL tier
types — filter sponsorship__sponsorship_tier_type = 'package_tier' for
package-only figures ('a_la_carte' and 'billing_adjustment' are the others).
ACCOUNT LENS: the account entity spans registrations, sponsorships and
enrollments — group or filter account__account_name, or
account__top_parent_name for the whole company (recipe 6); speakers carry no
account entity. ATTACHMENT: all three attach at foundation level, so
scope them with project__foundation_slug; a leaf project's own slug returns NOTHING,
which is the attachment, not missing data. TIME AXES: registrations
carry two — registration_id__event_start_date, where a window means "events
in the window" (what the standard metric uses), and metric_time, the sign-up
date; pick the one the question means and say which. Enrollments use
metric_time. PER EVENT ad hoc: total_registrations by event_id__event_name +
registration_id__event_start_date__year. PER COURSE ad hoc: total_enrollments
by enrollment_id__course_name + enrollment_id__product_type. FLOORS: edX
enrollments carry no account and land in the NULL bucket, and a share of
registrations has no account either, so every org-scoped figure here is a
floor — present "attributed registrations/enrollments" and say so.

15. STANDARD METRIC CALLS take uniform parameters on every family — metric,
by, project + subprojects (excluded|separate|combined, default combined), org
+ subsidiaries (excluded|separate|combined, default excluded), start_date,
end_date, period (day|week|month|quarter|year), order_by, limit; there is no
since, until or as_of. The families: memberships, member_organizations,
new_members, new_member_organizations, lost_member_organizations,
paying_member_organizations, membership_churn, contributors, contributions,
contributing_organizations, participants, maintainers,
maintainer_contributions, project_health,
software_value, event_registrations, event_sponsorships, speakers,
training_enrollments, certifications, social_mentions, social_reach,
meetups, meetup_attendees; their groupings (by) are in
read_lfx_standard_metrics_guidance. The two meetup families are the
exception to the uniform switches: they take project only — org and
subsidiaries are rejected, and subprojects is accepted but a no-op, since
chapters attach at the foundation (recipe 16). by left out is the
first listed, and the scope supplies the other axis (by=project with org =
that company's projects; by=org with project = that project's companies).
period adds a time dimension to by: by=org with period=month is one row per
organization per month; without period the by grouping remains.
Two kinds: a WINDOW family counts between start_date and end_date;
an AT-DATE family (memberships, member_organizations,
paying_member_organizations, maintainers, project_health, software_value)
reports the state on end_date, and with
period the state at each period end. maintainers is the exception: today's
roster only; with period, one row per period of today's maintainers active
in it, not the roster at that time. "Members at the end of 2022" and
"members at each year end" are memberships with end_date, or with start_date
+ period=year; no lens call needed. An LF-wide total takes NO project; the
foundation's own slug (tlf) is one bucket, not the LF-wide scope. Every date is a UTC calendar day;
end_date defaults to today. The switches say what a name covers: excluded =
that project or account alone, separate = it and everything under it one row
each (the breakdown), combined = the hierarchy folded together (the project
or account columns leave the result; any other by grouping keeps its rows,
by=total is one figure). The
DEFAULTS are the plain reading: a project name alone is its whole tree as ONE figure, an
organization name alone is that account, and an activity family with no
start_date is the trailing 365 days; every result carries an applied block
saying which scope, dates and definition ran. A briefing usually wants the
headline and the breakdown — two calls. DEPTH: on every standard metric,
separate and combined cover a named node's tree and a company's subsidiaries
at ANY depth. Results come back in the same words (account, parent_org, project,
foundation, period), and order_by takes them (the meetup families
excepted from DEPTH: no tree, subprojects a no-op).
There is no free filter on a standard metric: a slice the switches, the dates and
the period cannot express is an explore + query question, and its answer is
labelled ad hoc.

16. MEETUPS (Open Community Groups) are the community-run chapters on
ocgroups.dev — not LF conferences (recipe 14) and not project meetings
(recipe 12). The governed shapes are standard metrics (recipe 15): meetups
and meetup_attendees by community, region, group or city, with period for
time, scoped with the foundation's slug (a community IS a foundation:
project=cncf). Come here only for a slice those cannot express — a FILTER
on one city, region or group with a trend over time. AVAILABILITY FIRST:
this domain is being rolled out, so before any meetup call run list_metrics
with search='meetup' (search is a free-text substring match, not a fixed
list — 'meetup' is the intended term); if meetups and meetup_attendees are
not both returned, the data is not available yet — say so and stop, do not
substitute a query_lfx_lens guess or a different metric. METRICS: meetups
(events on their start date, additive) and meetup_attendees (distinct
people; never sum rows across years). DIMENSIONS: meetup_group__city,
meetup_group__region, meetup_group__group_name,
meetup_group__community_slug, and metric_time__year on the event start
date. CITY OVER TIME ("is the Austin meetup scene growing"):
  metrics=meetups,meetup_attendees group_by=metric_time__year
  where={{ Dimension('meetup_group__city') }} = 'Austin'
  order_by=metric_time__year
— the city is the chapter's home city, so verify the literal with
get_dimension_values(dimension=meetup_group__city, metrics=meetups,
search='Austin') first (metrics is required on that action), and scope
with meetup_group__community_slug only if the question names a foundation.
Read the trend from full years; call the current year year-to-date and do
not extrapolate it. Growth is the direction of BOTH series — more events
with fewer distinct people is a different story from fewer events with
more people, and the answer says which.

17. SOCIAL LISTENING. The standard metrics social_mentions (by total,
project, network, sentiment) and social_reach (by total, project) cover the
common readings — prefer them. Compose here only for a slice they lack:
language, keyword, share of voice across foundations, trends at a grain the
period switch does not give. The model: mentions of a project across social
and web platforms, one row per mention. METRICS: social_listening_mentions,
social_listening_positive_mentions and social_listening_negative_mentions
(the rest are neutral), social_listening_unique_authors, and reach as
social_listening_total_author_followers (the authors' follower counts summed;
NULL for older records and platforms without follower data, so a floor) and
social_listening_avg_author_followers. SCOPE: mention_key__project_slug is
the project a mention resolved to — a leaf's own mentions;
project__foundation_slug covers a foundation, project__project_path LIKE
'%/<slug>/%' a subtree. Never let scope default: no filter means ALL of LF, and a
foundation filter nobody asked for is a silent undercount — say which scope
ran. SLICES: mention_key__social_network (stored as 'Twitter', not 'X';
'Reddit', 'Bluesky', 'News', 'Podcasts', 'DEV', 'Hacker News', 'LinkedIn',
'YouTube', 'Github', 'TikTok' — copy from get_dimension_values),
mention_key__sentiment ('positive' | 'neutral' | 'negative'),
mention_key__language, mention_key__keyword, metric_time (mention time) for
trends. SHARE OF VOICE: mentions by project__foundation_slug or
mention_key__project_slug over one window, shares computed from the rows.
The feed is young — group by metric_time__year before comparing years.
Free-text feeds (titles, bodies, URLs, per-author lists) are not metrics;
they are the one social question that still goes to query_lfx_lens.

18. WHAT GOES IN THE ANSWER. These recipes are working knowledge. The answer
is the figure, one line on what it covers, and only the caveats that change
how that figure is read, in the reader's words: no metric, dimension or
column names, keys, SQL or tool names unless asked how it was made. Grain,
vocabulary and timezone notes stay in context.

## Worked examples (verified live)

Top CNCF member orgs by dues:
  metrics=current_membership_count,current_membership_revenue
  group_by=account__account_name order_by=-current_membership_revenue limit=20
  where={{ Dimension('project__foundation_slug') }} = 'cncf'

Kubernetes contributor trend by month:
  metrics=total_contributors group_by=metric_time__month
  where={{ Dimension('activity_project_id__project_slug') }} = 'k8s' AND {{ TimeDimension('metric_time','DAY') }} >= '2026-03-01'

Kubernetes mentions by network this year (recipe 17):
  metrics=social_listening_mentions,social_listening_positive_mentions,social_listening_negative_mentions
  group_by=mention_key__social_network order_by=-social_listening_mentions
  where={{ Dimension('mention_key__project_slug') }} = 'k8s' AND {{ TimeDimension('metric_time','DAY') }} >= '2026-01-01'

Org share of PyTorch code activity (recipe 2):
  metrics=code_contribution_activities group_by=activity_project_id__organization_name
  order_by=-code_contribution_activities limit=50
  where={{ Dimension('activity_project_id__project_spine_slug') }} = 'pytorch' AND {{ Dimension('activity_project_id__is_org_contribution') }} = true

Prefer repeatable answers: standard metric > named metric > lens SQL — label
anything below the top rung; struggling, re-read the recipe BEFORE any
query_lfx_lens fallback.
