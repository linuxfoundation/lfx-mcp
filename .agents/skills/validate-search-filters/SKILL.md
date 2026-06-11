---
name: validate-search-filters
description: Validate MCP search tool filter parameters against the live OpenSearch resources index and upstream indexer-contract documentation. Use after any filter bug report or indexer contract change to identify broken, missing, or incorrectly implemented filter parameters.
license: MIT
compatibility: Requires kubectl configured against the LFX v2 Kubernetes cluster (dev or prod). The OpenSearch cluster is an AWS-managed OpenSearch Service domain reachable only from within the cluster network — queries are tunnelled through the NATS box pod using kubectl exec.
---

Validate every filter parameter across all tools that use the query service SDK
in `internal/tools/` against the live OpenSearch `resources` index and the
upstream indexer-contract documentation. Produce a per-filter verdict table
and optionally apply fixes.

## Gotchas

- The OpenSearch cluster is AWS-managed and not port-forward accessible. All
  `curl` queries must be run via `kubectl exec` into the NATS box pod.
- Discover the pod name dynamically — never hardcode it. Use the label
  selector `app.kubernetes.io/component=nats-box,app.kubernetes.io/instance=lfx-platform`
  in namespace `lfx`.
- Discover the OpenSearch URL dynamically from the indexer deployment env var
  `OPENSEARCH_URL`. The index name is in `OPENSEARCH_INDEX` (currently
  `resources`). Combine them into `OPENSEARCH_BASEURL` as shown in Step 1.
- `tags` entries may have empty values (e.g. `"committee_uid:"`,
  `"project_uid:"`) — these are indexed but useless for filtering. A tag key
  is only valid evidence when at least one document has a non-empty value for
  it.
- Mixed old/new data means partial `parent_refs` coverage is expected on some
  resource types. A non-zero hit count on a prefix query is sufficient evidence
  that the mechanism works.
- `payload.Parent` in the query service is a single string. A tool that
  accepts both `project_uid` and `committee_uid` can only send one at a time.
- `handleSearchPastMeetingParticipants` and `handleSearchPastMeetingSummaries`
  are dedicated handlers — each owns its own filter logic independently.
- **Prefer count-only queries over random sampling.** A handful of random
  documents proves nothing — you can get lucky and see the right fields while
  90% of the corpus has them missing. Always run `"size": 0` prefix count
  queries first. Only pull sample documents (`"size": 3`) as a secondary
  debugging aid when a count is zero or surprising (e.g. to understand what
  fields are actually present on that resource type).

## Step 1 — Discover infrastructure

```bash
# Resolve the NATS box pod name.
NATS_POD=$(kubectl get pod -n lfx \
  -l 'app.kubernetes.io/component=nats-box,app.kubernetes.io/instance=lfx-platform' \
  -o jsonpath='{.items[0].metadata.name}')

# Resolve OpenSearch URL and index from the indexer deployment env vars.
OPENSEARCH_URL=$(kubectl get deploy -n lfx lfx-v2-indexer-service \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="OPENSEARCH_URL")].value}')
OPENSEARCH_INDEX=$(kubectl get deploy -n lfx lfx-v2-indexer-service \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="OPENSEARCH_INDEX")].value}')

# Build the base URL used in all search queries below.
OPENSEARCH_BASEURL="$OPENSEARCH_URL/$OPENSEARCH_INDEX"

# Verify connectivity — halt if this returns an error or empty body.
# Use --max-time 15 to prevent the curl from hanging and triggering OOMKill (exit 137).
kubectl exec -n lfx "$NATS_POD" -- \
  curl -s --max-time 15 "$OPENSEARCH_URL/"
```

If the connectivity check fails, stop and report: **OpenSearch unreachable —
check cluster access.**

Substitute the kubectl context as needed to target dev vs. prod.

## Step 2 — Enumerate search tools and their filter mappings

**Do not rely solely on the reference table below — always grep the codebase
first** to find every file that calls `QueryResources`. The table may be out of
date if new tools have been added since it was last updated.

```bash
grep -rEn "QueryResources|QueryResourcesPayload" internal/tools/ | grep -v "_test.go"
```

For each file that appears, read the handler and record how each filter
parameter is sent to the query service. The mechanisms are:

| Mechanism | Query service field | Index field |
|---|---|---|
| `payload.Parent = "<type>:<uid>"` | `Parent` | `parent_refs` |
| `payload.Tags = ["<key>:<value>"]` | `Tags` | `tags` |
| `payload.Filters = ["<field>:<value>"]` | `Filters` | top-level doc fields |
| `payload.FiltersAll = ["<field>:<value>"]` | `FiltersAll` | top-level doc fields (AND semantics) |
| `payload.Name = "<value>"` | `Name` | `name` (text search) |
| `payload.DateField` / `DateFrom` / `DateTo` | date range | date fields |

Only `Parent`, `Tags`, `Filters`, and `FiltersAll` are structural filters that
map to indexed fields — these are the ones to validate. `Name` and date fields
are query-time text/range operations and do not need index field verification.

Reference table of known tools and their structured filter parameters (verify
against the grep output above before trusting this):

| Tool | Resource type | Parameter | Mechanism | Sent as |
|---|---|---|---|---|
| `search_projects` | `project` | `parent_uid` | Parent | `project:<uid>` |
| `search_committees` | `committee` | `project_uid` | Parent | `project:<uid>` |
| `search_committee_members` | `committee_member` | `committee_uid` | Tag | `committee_uid:<uid>` |
| `search_committee_members` | `committee_member` | `project_uid` | Tag | `project_uid:<uid>` |
| `search_mailing_lists` | `groupsio_mailing_list` | `project_uid` | Parent | `project:<uid>` |
| `search_mailing_list_members` | `groupsio_member` | `mailing_list_id` | Tag | `mailing_list_uid:<id>` |
| `search_mailing_list_members` | `groupsio_member` | `project_uid` | Tag | `project_uid:<uid>` |
| `search_meetings` | `v1_meeting` | `committee_uid` | Parent (preferred) | `committee:<uid>` |
| `search_meetings` | `v1_meeting` | `project_uid` | Parent (fallback) | `project:<uid>` |
| `search_meeting_registrants` | `v1_meeting_registrant` | `meeting_id` | Parent (preferred) | `meeting:<id>` |
| `search_meeting_registrants` | `v1_meeting_registrant` | `committee_uid` | Parent (fallback) | `committee:<uid>` |
| `search_past_meetings` | `v1_past_meeting` | `project_uid` | Parent | `project:<uid>` |
| `search_past_meetings` | `v1_past_meeting` | `committee_uid` | Tag | `committee_uid:<uid>` |
| `search_past_meetings` | `v1_past_meeting` | `meeting_id` | Tag | `meeting_id:<id>` |
| `search_past_meeting_participants` | `v1_past_meeting_participant` | `past_meeting_id` | Parent (preferred) | `past_meeting:<meeting_and_occurrence_id>` |
| `search_past_meeting_participants` | `v1_past_meeting_participant` | `project_uid` | Parent (fallback) | `project:<uid>` |
| `search_past_meeting_summaries` | `v1_past_meeting_summary` | `past_meeting_id` | Parent (preferred) | `past_meeting:<meeting_and_occurrence_id>` |
| `search_past_meeting_summaries` | `v1_past_meeting_summary` | `project_uid` | Parent (fallback) | `project:<uid>` |
| `search_members` | `project_membership` | `project_uid` | FiltersAll | `project_uid:<uid>` |
| `search_members` | `project_membership` | `b2b_org_uid` | FiltersAll | `b2b_org_uid:<uid>` |
| `search_members` | `project_membership` | `tier_uid` | FiltersAll | `tier_uid:<uid>` |
| `search_members` | `project_membership` | `tier_name` | FiltersAll | `tier_name:<name>` |
| `search_members` | `project_membership` | `status` | FiltersAll | `status:Active` (hardcoded default) |
| `get_membership_key_contacts` | `key_contact` | `membership_uid` | FiltersAll | `membership_uid:<uid>` |
| `search_b2b_orgs` | `b2b_org` | *(none — Name only)* | — | — |

## Step 3 — Fetch indexer contracts

Fetch the indexer-contract documentation for each resource type. The contracts
define the canonical set of tag keys and parent_ref prefixes each service
publishes. This information is **required** for the Step 6 report — every
filter parameter must be cross-referenced against the contract.

Known contract URLs:

- `v1_meeting`, `v1_meeting_registrant`, `v1_past_meeting`,
  `v1_past_meeting_participant`, `v1_past_meeting_transcript`,
  `v1_past_meeting_summary`:
  https://github.com/linuxfoundation/lfx-v2-meeting-service/blob/main/docs/indexer-contract.md
- `project`:
  https://github.com/linuxfoundation/lfx-v2-project-service/blob/main/docs/indexer-contract.md
- `committee`, `committee_member`:
  https://github.com/linuxfoundation/lfx-v2-committee-service/blob/main/docs/indexer-contract.md
- `groupsio_mailing_list`, `groupsio_member`:
  https://github.com/linuxfoundation/lfx-v2-mailing-list-service/blob/main/docs/indexer-contract.md

Fetch each URL and extract the **Tags** table and **Parent References** table
for each resource type. Record which tag keys and parent_ref prefixes the
contract defines. If a URL 404s or has no contract doc, note it and continue —
treat those filters as "no contract definition" in the report.

## Step 4 — Count hits in the live index

For each filter parameter, run a **count-only query** (`"size": 0`) via the
NATS box. This is the primary evidence step. Use the `$NATS_POD` and
`$OPENSEARCH_BASEURL` variables set in Step 1.

> **Note:** These queries omit `track_total_hits: true`, so `hits.total.value`
> may be capped at 10,000 on very large indices. This is intentional — an
> approximate count is sufficient to confirm a field is populated. If a count
> comes back at exactly 10,000, treat it as "≥10,000 hits" rather than a
> precise figure.

**Count documents where a specific tag key has non-empty values (last 45 days):**

```bash
kubectl exec -n lfx "$NATS_POD" -- \
  curl -s --max-time 15 -X GET "$OPENSEARCH_BASEURL/_search" \
  -H 'Content-Type: application/json' \
  -d '{
    "size": 0,
    "query": {
      "bool": {
        "must": [
          { "term": { "object_type": "<RESOURCE_TYPE>" } },
          { "prefix": { "tags": "<TAG_KEY>:" } },
          { "range": { "updated_at": { "gte": "now-45d" } } }
        ],
        "must_not": [
          { "term": { "tags": "<TAG_KEY>:" } }
        ]
      }
    }
  }'
```

**Count documents where a specific parent_ref prefix exists (last 45 days):**

```bash
kubectl exec -n lfx "$NATS_POD" -- \
  curl -s --max-time 15 -X GET "$OPENSEARCH_BASEURL/_search" \
  -H 'Content-Type: application/json' \
  -d '{
    "size": 0,
    "query": {
      "bool": {
        "must": [
          { "term": { "object_type": "<RESOURCE_TYPE>" } },
          { "prefix": { "parent_refs": "<PREFIX>:" } },
          { "range": { "updated_at": { "gte": "now-45d" } } }
        ]
      }
    }
  }'
```

Record the `total.value` from each response. A non-zero count confirms the
key/prefix is present in recently indexed data. If the count is zero but the
resource type has older data, note it as "not seen in last 45 days" rather
than immediately marking it broken.

**Only when a count is zero or surprising**, pull a small sample to understand
what fields are actually present on that resource type:

```bash
kubectl exec -n lfx "$NATS_POD" -- \
  curl -s --max-time 15 -X GET "$OPENSEARCH_BASEURL/_search" \
  -H 'Content-Type: application/json' \
  -d '{
    "size": 3,
    "_source": ["tags", "parent_refs", "object_type"],
    "query": {
      "bool": {
        "must": [
          { "term": { "object_type": "<RESOURCE_TYPE>" } },
          { "range": { "updated_at": { "gte": "now-45d" } } }
        ]
      }
    }
  }'
```

## Step 5 — Build the truth table

Cross-reference: tool parameter → mechanism → contract definition → index
evidence. Assign a verdict to each filter parameter:

- ✅ **Works** — the tag key or parent_ref prefix exists in the index with
  non-empty values, matching what the tool sends, and the contract defines it.
- ⚠️ **Broken** — the tool sends the wrong mechanism (e.g. tag when it should
  be parent_ref), or uses a key/prefix that does not appear in the index.
- ❌ **Not indexed** — the data is not present in the index at all for this
  resource type; the parameter should be removed from the tool.
- ⚠️ **No contract definition** — the filter works in the live index but is
  not listed in the indexer-contract doc; flag for follow-up.

## Step 6 — Report findings

Emit a structured markdown report grouped by tool. Each row must include a
"Contract" column that cross-references the indexer-contract documentation
fetched in Step 3 — state whether the contract defines the tag key or
parent_ref prefix used by the tool, and if so, whether the tool's mechanism
matches what the contract specifies.

```markdown
## <tool_name> (resource type: <type>)

| Parameter | Mechanism | Sent as | Contract | Index evidence | Verdict |
|---|---|---|---|---|---|
| committee_uid | Parent | committee:<uid> | ✅ parent_ref `committee:` | parent_refs prefix "committee:" — N hits | ✅ Works |
| project_uid | Parent | project:<uid> | ✅ parent_ref `project:` | parent_refs prefix "project:" — 0 hits | ⚠️ Broken |
| meeting_id | Tag | meeting_id:<id> | ⚠️ not in contract | tag key "meeting_id:" — N hits | ⚠️ Review |
```

After the table, state explicitly:
- Which filters are confirmed working and match the contract.
- Which are broken and why (wrong mechanism, wrong key name, etc.).
- Which should be removed because the data is not indexed.
- Which have no contract definition (tag/parent_ref not listed in the
  indexer-contract doc) — flag these for follow-up even if they appear to work
  in the live index, since undocumented fields may be removed without notice.

## Step 7 — Apply fixes (optional)

Only proceed if explicitly instructed to fix. Apply the correct pattern for
each broken filter:

- **Tag → parent_ref**: change `payload.Tags` to
  `payload.Parent = "<type>:<uid>"`.
- **Wrong tag key**: use the key that actually appears in the index.
- **Not indexed**: remove the parameter from the args struct and handler.

After applying fixes, run `make build` to confirm compilation succeeds.

## Step 8 — Verify fixes

Re-run the aggregation count queries from Step 4 against the corrected
mechanism to confirm non-zero results. Report before/after hit counts for
each fixed filter.
