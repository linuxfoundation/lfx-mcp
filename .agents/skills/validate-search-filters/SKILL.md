---
name: validate-search-filters
description: Validate MCP search tool filter parameters against the live OpenSearch resources index and upstream indexer-contract documentation. Use after any filter bug report or indexer contract change to identify broken, missing, or incorrectly implemented filter parameters.
license: MIT
compatibility: Requires kubectl configured against the LFX v2 Kubernetes cluster (dev or prod) with aws-vault access. The OpenSearch cluster is an AWS-managed OpenSearch Service domain reachable only from within the cluster network — queries are tunnelled through the NATS box pod using kubectl exec.
---

Validate every search tool filter parameter in `internal/tools/` against the
live OpenSearch `resources` index and the upstream indexer-contract
documentation. Produce a per-filter verdict table and optionally apply fixes.

## Gotchas

- The OpenSearch cluster is AWS-managed and not port-forward accessible. All
  `curl` queries must be run via `kubectl exec` into the NATS box pod.
- Discover the pod name dynamically — never hardcode it. Use the label
  selector `app.kubernetes.io/component=nats-box,app.kubernetes.io/instance=lfx-platform`
  in namespace `lfx`.
- Discover the OpenSearch URL dynamically from the indexer deployment env var
  `OPENSEARCH_URL`. The index name is in `OPENSEARCH_INDEX` (currently
  `resources`).
- `tags` entries may have empty values (e.g. `"committee_uid:"`,
  `"project_uid:"`) — these are indexed but useless for filtering. A tag key
  is only valid evidence when at least one document has a non-empty value for
  it.
- Mixed old/new data means partial `parent_refs` coverage is expected on some
  resource types. A non-zero hit count on a prefix query is sufficient evidence
  that the mechanism works.
- `payload.Parent` in the query service is a single string. A tool that
  accepts both `project_uid` and `committee_uid` can only send one at a time.
- `handleSearchPastMeetingResource` is a shared handler used by
  `v1_past_meeting_participant`, `v1_past_meeting_summary`, and
  (if added) `v1_past_meeting_transcript`. Changes to its filter logic affect
  all three resource types simultaneously.
- When sampling documents, use `"size": 3` to keep output small. Use
  `_source` filtering to request only `tags` and `parent_refs` fields.

## Step 1 — Discover infrastructure

```bash
# Resolve the NATS box pod name.
NATS_POD=$(aws-vault exec lfx-prod -s -- kubectl --context=prod-lfx-v2 \
  get pod -n lfx \
  -l 'app.kubernetes.io/component=nats-box,app.kubernetes.io/instance=lfx-platform' \
  -o jsonpath='{.items[0].metadata.name}')

# Resolve OpenSearch URL and index from the indexer deployment.
OS_ENV=$(aws-vault exec lfx-prod -s -- kubectl --context=prod-lfx-v2 \
  get deploy -n lfx lfx-v2-indexer-service \
  -o jsonpath='{.spec.template.spec.containers[0].env}')
# Extract OPENSEARCH_URL and OPENSEARCH_INDEX from OS_ENV (jq or grep).

# Verify connectivity — halt if this returns an error or empty body.
aws-vault exec lfx-prod -s -- kubectl --context=prod-lfx-v2 \
  exec -n lfx "$NATS_POD" -- \
  curl -s "$OPENSEARCH_URL/"
```

If the connectivity check fails, stop and report: **OpenSearch unreachable —
check cluster access and aws-vault session.**

Substitute `lfx-prod` / `prod-lfx-v2` with `lfx-dev` / `dev-lfx-v2` if you
need to target the dev cluster instead.

## Step 2 — Enumerate search tools and their filter mappings

Read every `*Args` struct in `internal/tools/` and record how each filter
parameter is sent to the query service. The mechanisms are:

| Mechanism | Query service field | Index field |
|---|---|---|
| `payload.Parent = "<type>:<uid>"` | `Parent` | `parent_refs` |
| `payload.Tags = ["<key>:<value>"]` | `Tags` | `tags` |
| `payload.Filters = ["<field>:<value>"]` | `Filters` | top-level doc fields |
| `payload.Name = "<value>"` | `Name` | `name` (text search) |
| `payload.DateField` / `DateFrom` / `DateTo` | date range | date fields |

Current search tools and their structured filter parameters (update this list
if new tools are added):

| Tool | Resource type | Parameter | Mechanism | Sent as |
|---|---|---|---|---|
| `search_meetings` | `v1_meeting` | `committee_uid` | Parent (preferred) | `committee:<uid>` |
| `search_meetings` | `v1_meeting` | `project_uid` | Parent (fallback) | `project:<uid>` |
| `search_meeting_registrants` | `v1_meeting_registrant` | `meeting_id` | Parent (preferred) | `meeting:<id>` |
| `search_meeting_registrants` | `v1_meeting_registrant` | `committee_uid` | Parent (fallback) | `committee:<uid>` |
| `search_past_meetings` | `v1_past_meeting` | `project_uid` | Parent | `project:<uid>` |
| `search_past_meetings` | `v1_past_meeting` | `committee_uid` | Tag | `committee_uid:<uid>` |
| `search_past_meetings` | `v1_past_meeting` | `meeting_id` | Tag | `meeting_id:<id>` |
| `search_past_meeting_participants` | `v1_past_meeting_participant` | `meeting_id` | Tag | `meeting_id:<id>` |
| `search_past_meeting_summaries` | `v1_past_meeting_summary` | `meeting_id` | Tag | `meeting_id:<id>` |

Re-read the handler code to verify this table is current before proceeding.

## Step 3 — Fetch indexer contracts

Fetch the indexer-contract documentation for each resource type and extract
the **Tags** table and **Parent References** table.

Known contract URLs:

- `v1_meeting`, `v1_meeting_registrant`, `v1_past_meeting_participant`,
  `v1_past_meeting_transcript`, `v1_past_meeting_summary`:
  https://github.com/linuxfoundation/lfx-v2-meeting-service/blob/main/docs/indexer-contract.md

Fetch each URL and record which tag keys and parent_ref prefixes the contract
defines for each resource type.

## Step 4 — Sample the live index

For each resource type, run the following queries via the NATS box. Replace
`$NATS_POD`, `$OPENSEARCH_URL`, and `$INDEX` with the values from Step 1.

**Sample tags and parent_refs from 3 documents:**

```bash
aws-vault exec lfx-prod -s -- kubectl --context=prod-lfx-v2 \
  exec -n lfx "$NATS_POD" -- \
  curl -s -X GET "$OPENSEARCH_URL/$INDEX/_search" \
  -H 'Content-Type: application/json' \
  -d '{
    "size": 3,
    "_source": ["tags", "parent_refs", "object_type"],
    "query": {
      "term": { "object_type": "<RESOURCE_TYPE>" }
    }
  }'
```

**Check whether a specific tag key has any non-empty values:**

```bash
aws-vault exec lfx-prod -s -- kubectl --context=prod-lfx-v2 \
  exec -n lfx "$NATS_POD" -- \
  curl -s -X GET "$OPENSEARCH_URL/$INDEX/_search" \
  -H 'Content-Type: application/json' \
  -d '{
    "size": 1,
    "_source": ["tags"],
    "query": {
      "bool": {
        "must": [
          { "term": { "object_type": "<RESOURCE_TYPE>" } },
          { "prefix": { "tags": "<TAG_KEY>:" } }
        ],
        "must_not": [
          { "term": { "tags": "<TAG_KEY>:" } }
        ]
      }
    }
  }'
```

**Check whether a specific parent_ref prefix exists:**

```bash
aws-vault exec lfx-prod -s -- kubectl --context=prod-lfx-v2 \
  exec -n lfx "$NATS_POD" -- \
  curl -s -X GET "$OPENSEARCH_URL/$INDEX/_search" \
  -H 'Content-Type: application/json' \
  -d '{
    "size": 1,
    "_source": ["parent_refs"],
    "query": {
      "bool": {
        "must": [
          { "term": { "object_type": "<RESOURCE_TYPE>" } },
          { "prefix": { "parent_refs": "<PREFIX>:" } }
        ]
      }
    }
  }'
```

Record the `total.value` from each response. A non-zero value confirms the
key/prefix is present in the index.

## Step 5 — Build the truth table

Cross-reference: tool parameter → mechanism → index evidence. Assign a
verdict to each filter parameter:

- ✅ **Works** — the tag key or parent_ref prefix exists in the index with
  non-empty values, matching what the tool sends.
- ⚠️ **Broken** — the tool sends the wrong mechanism (e.g. tag when it should
  be parent_ref), or uses a key/prefix that does not appear in the index.
- ❌ **Not indexed** — the data is not present in the index at all for this
  resource type; the parameter should be removed from the tool.

## Step 6 — Report findings

Emit a structured markdown report grouped by tool:

```markdown
## <tool_name> (resource type: <type>)

| Parameter | Mechanism | Sent as | Index evidence | Verdict |
|---|---|---|---|---|
| committee_uid | Parent | committee:<uid> | parent_refs prefix "committee:" — N hits | ✅ Works |
| project_uid | Parent | project:<uid> | parent_refs prefix "project:" — 0 hits | ⚠️ Broken |
```

After the table, state explicitly:
- Which filters are confirmed working.
- Which are broken and why (wrong mechanism, wrong key name, etc.).
- Which should be removed because the data is not indexed.

## Step 7 — Apply fixes (optional)

Only proceed if explicitly instructed to fix. Apply the correct pattern for
each broken filter:

- **Tag → parent_ref**: change `payload.Tags` to
  `payload.Parent = "<type>:<uid>"`.
- **Wrong tag key**: use the key that actually appears in the index.
- **Not indexed**: remove the parameter from the args struct and handler.
- **Shared handler impact**: if the fix is in `handleSearchPastMeetingResource`,
  verify that the change is correct for all resource types that use it
  (`v1_past_meeting_participant`, `v1_past_meeting_summary`, and any others).

After applying fixes, run `make build` to confirm compilation succeeds.

## Step 8 — Verify fixes

Re-run the targeted `curl` queries from Step 4 against the corrected
mechanism to confirm non-zero results. Report before/after hit counts for
each fixed filter.
