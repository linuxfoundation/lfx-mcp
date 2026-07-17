---
name: upgrade-maintenance
description: Upgrade all Go dependencies, fix payload/contract changes introduced by upstream LFX V2 services, and run a query service argument review pass. Use when upgrading dependencies, after upstream service contract changes, or for periodic maintenance of the lfx-mcp codebase.
license: MIT
---

# Upgrade Maintenance

Perform a full upgrade maintenance pass on the lfx-mcp codebase. This covers
three sequential phases: dependency upgrades, upstream contract fixes, and a
query service argument review.

## Phase 1 — Upgrade all dependencies

### Step 1.1 — Upgrade the Go toolchain version

Compare the local toolchain version against the `go` directive in `go.mod` and
update if the local toolchain is newer:

```bash
# Extract the version from the local toolchain (e.g. "go1.24.3").
LOCAL=$(go version | awk '{print $3}')        # e.g. go1.24.3
GOMOD=$(grep '^go ' go.mod | awk '{print $2}') # e.g. 1.23.0

echo "Local toolchain: $LOCAL"
echo "go.mod go directive: $GOMOD"
```

If the local toolchain is newer, update `go.mod`:

```bash
# Updates the go directive to match the local toolchain.
go mod tidy -go=${LOCAL#go}
```

Also update any other files that pin the Go version:

- `Dockerfile` — `FROM golang:X.Y.Z` or `FROM golang:X.Y-alpine` base image.
- `.github/workflows/*.yml` — `go-version:` fields in GitHub Actions steps.
- `Makefile` — any hardcoded `GO_VERSION` variable.
- `AGENTS.md` / `README.md` — version references in prerequisite tables.

After updating, verify the build still compiles with the new toolchain:

```bash
make build
```

If any deprecated language features or removed stdlib symbols cause errors,
fix them before continuing.

### Step 1.2 — Upgrade Go module dependencies

```bash
# Upgrade all direct dependencies to their latest minor/patch versions.
go get -u ./...

# Tidy the module graph (removes unused indirect deps, syncs go.sum).
go mod tidy
```

After running, inspect `go.mod` for version changes. The most impactful upgrades
to watch are the **`github.com/linuxfoundation/lfx-v2-*` packages** — they
regularly add, rename, or remove Goa-generated struct fields, which directly
affects tool handler code in `internal/tools/` and client wiring in
`internal/lfxv2/client.go`.

All currently connected LFX services can be discovered from `go.mod`:

```bash
grep 'linuxfoundation/lfx-v2-' go.mod
```

Two categories of service are present, with different upgrade patterns:

- **Goa CRUD services** (all `lfx-v2-*` packages except query): each exposes
  standard resource CRUD operations via a Goa-generated HTTP client. The
  upgrade risks are: (1) new RPCs added → `NewClient` gains extra positional
  arguments; (2) response struct fields added/removed/renamed → tool handlers
  and view mapping functions need updating.
- **Query service** (`lfx-v2-query-service`): a generic search interface; its
  payload struct (`Parent`, `Tags`, `Filters`) is stable but the string values
  sent (tag keys, parent-ref prefixes, resource type names) are driven by
  upstream indexer contracts, reviewed in Phase 2.
- **MCP SDK** (`github.com/modelcontextprotocol/go-sdk`): less common; only
  minor/patch bumps expected. A major bump could change `mcp.AddTool`
  signatures, annotation fields, or content types.

### Step 1.2.1 — Update the OTel semconv import version

`go.opentelemetry.io/otel/semconv` is **not** a separate Go module. The version
in the import path (e.g. `semconv/v1.40.0`) is an OTel semantic-convention spec
version that is a subdirectory **inside** the `go.opentelemetry.io/otel` module.

**Why this matters**: instrumentation packages (e.g. `otelhttp`) always import
the semconv sub-package they were written against. When that version differs from
the one imported in our own code, the OTel SDK emits a startup error:

```text
conflicting Schema URL: https://opentelemetry.io/schemas/1.41.0 and
https://opentelemetry.io/schemas/1.40.0
```

**Rule**: after upgrading `go.opentelemetry.io/otel`, always update our semconv
import paths to the **highest-numbered sub-package present** inside the new otel
version. That is deterministically the latest spec version shipped with the
module, and it is the version the upgraded instrumentation packages will register.

Find it with:

```bash
OTEL_VER=$(grep 'go.opentelemetry.io/otel v' go.mod | grep -v '//' | awk '{print $2}' | head -1)
ls $(go env GOMODCACHE)/go.opentelemetry.io/otel@${OTEL_VER}/semconv/ \
  | grep '^v[0-9]' | sort -V | tail -1
# e.g. "v1.41.0"
```

Then update every `semconv/v*` import in the codebase to use that version:

```bash
# Find all semconv imports.
grep -rn 'semconv/v' internal/ cmd/ --include='*.go'
```

Update each import path in-place (e.g. `semconv/v1.40.0` → `semconv/v1.41.0`).
The attributes and helper functions are additive across minor spec versions, so
the rename is always safe for a minor/patch upgrade.

After updating, verify the build and confirm the schema-URL conflict is gone:

```bash
make build && ./scripts/test_server.sh 2>&1 | grep -i 'conflicting\|schema'
# Should produce no output.
```

### Step 1.3 — Verify the build compiles

```bash
make build
```

If the build fails, read the compiler errors before proceeding. Common causes
after Goa CRUD service upgrades:

- **`NewClient` has too few arguments** — a service added new RPCs. Every Goa
  CRUD service wires its `NewClient` call in `internal/lfxv2/client.go`. Find
  the new method names on the generated HTTP client (look in the
  `gen/http/<service>/client/client.go` file of the upgraded module) and pass
  them as the additional positional arguments. Use `nil` for encoder-function
  arguments on endpoints you don't call (e.g. multipart upload endpoints).
- **Struct field no longer exists** — a service removed a Goa response field.
  Find all usages in `internal/tools/` and `internal/lfxv2/` with `grep`.
  If the code was a deliberate workaround for a known upstream bug (look for
  comments referencing a Jira ticket like `// see LFXV2-XXXX`), the workaround
  can likely be deleted entirely now that the upstream fix is in. If it was
  real business logic, update accordingly.
- **Renamed or removed struct fields in the MCP SDK** — rare, but check
  `ToolAnnotations`, handler signatures, and content type constructors.

Fix all compilation errors, then re-run `make build` until clean.

### Step 1.4 — Run the integration test suite

```bash
./scripts/test_server.sh
```

If tests fail, diagnose from the JSON-RPC output. Most failures after LFX
service upgrades are field-shape mismatches in tool response JSON. Fix and
re-run until all tests pass.

---

## Phase 2 — Fix upstream LFX V2 service contract changes

Upstream Goa CRUD services ship indexer contracts documenting their tag keys,
parent_ref prefixes, and resource field shapes. The query service uses these
values as string literals in tool handlers. When a service changes its
contract, those string literals and view mapping functions may need updating.

**The most common pattern** is a Goa response struct field being removed or
renamed. Any code in `internal/tools/` that references the field will fail to
compile — the build error from Phase 1.3 is the signal. Less visibly, **new
fields are silently dropped** by the explicit `to*View` mapping functions that
filter upstream response structs into MCP output (e.g. `toB2bOrgMembershipView`,
`toMembershipView`, `toKeyContactView`).

### Step 2.1 — Discover which services changed and what moved

For each upgraded Goa CRUD service (everything except query), diff its response
struct fields between the old and new version. The old and new versions come
from `git diff go.mod`. Use `$(go env GOMODCACHE)` to locate modules:

```bash
MODCACHE=$(go env GOMODCACHE)
SVC=github.com/linuxfoundation/lfx-v2-SERVICENAME
OLD=vX.Y.Z   # from git diff go.mod
NEW=vX.Y.Z+1

diff \
  <(find "${MODCACHE}/${SVC}@${OLD}" -name '*.go' \
      | xargs grep -h -A60 'type.*Response.*struct\|type.*Result.*struct' 2>/dev/null \
      | grep -E '^\s+[A-Z]\w+\s') \
  <(find "${MODCACHE}/${SVC}@${NEW}" -name '*.go' \
      | xargs grep -h -A60 'type.*Response.*struct\|type.*Result.*struct' 2>/dev/null \
      | grep -E '^\s+[A-Z]\w+\s') \
  | grep '^[<>]'
```

Run this for every upgraded `lfx-v2-*` service (skip the query service).

Also check for stale workarounds anywhere in `internal/`:

```bash
grep -rn 'LFXV2-\|// Mask\|// Strip\|// workaround' internal/
```

### Step 2.2 — Act on each finding

Apply the appropriate fix for each change found:

**Removed field** (compile error): delete all references in `internal/tools/`
and `internal/lfxv2/`. If the code was a workaround (Jira comment present),
delete the entire workaround block. If it was real business logic, update the
handler to work without the field.

**New field on a response struct used by a `to*View` mapper**: decide whether
the field is useful to MCP clients. If yes, add it to both the `*View` struct
(with a `json:"..."` tag) and the `to*View` function. If not, add a comment on
the view struct explaining why it was excluded.

**New field on a response struct not covered by a view mapper** (returned
directly as JSON): no action needed — the field will appear automatically.

**`NewClient` argument count mismatch** (compile error): already handled in
Step 1.3. Included here as a reminder that it originates from new RPCs in a
Goa CRUD service, not from a contract doc change.

**New field on an upstream payload struct (create/update handlers)**: the
compiler does not require exhaustive struct literal initialization, so new
fields are silently zeroed and never sent. Two patterns exist in
`internal/tools/` and both must be checked after any Goa CRUD service upgrade:

- *Pattern 1 — MCP args adapter with field renaming*: an adapter function that
  delegates to another handler using a struct literal with explicit field
  mappings (e.g. `handleCreateGroupMemberMode` →
  `CreateCommitteeMemberArgs{CommitteeUID: args.GroupUID, ...}`). These use
  struct literals because the field names differ between source and destination
  types, so a type conversion is not possible. A new field on the destination
  type will be silently zeroed.

  Note: adapters where source and destination types are structurally identical
  (same field names and types) should use a direct type conversion
  (`DestType(args)`) instead of a struct literal — the compiler then enforces
  exhaustiveness and this check is not needed for those.

- *Pattern 2 — Upstream payload builder*: a handler that constructs an upstream
  Goa payload struct directly, typically in two parts: a baseline struct literal
  seeded from current state, then a block of `if args.X != nil { payload.X = …
  }` overrides. A new upstream payload field can be silently dropped from
  either part.

To find candidates needing review:

```bash
# Find adapter functions that still use struct literals (Pattern 1).
# Type-conversion adapters (e.g. CreateCommitteeArgs(args)) are safe to skip.
grep -n 'func handle.*Mode\b' internal/tools/*_write.go internal/tools/*.go 2>/dev/null
# Then for each, check if the body uses a struct literal (not a type conversion).
grep -A5 'func handle.*Mode\b' internal/tools/*_write.go internal/tools/*.go 2>/dev/null | grep 'Args{'

# Find upstream payload struct literals (Pattern 2).
grep -n 'Payload{$\|Payload{' internal/tools/*_write.go
```

For each match, compare the fields in the struct literal against the current
field list of the destination type (check the upgraded module in
`$(go env GOMODCACHE)`). For every field present on the destination type but
absent from the literal, decide: forward it (add the mapping), or add a comment
explaining why it is intentionally omitted.

**Tag key, parent_ref prefix, or resource type name changed** (query service
string literals): update the affected string literals in `internal/tools/`.
Cross-reference against the service's indexer contract to confirm the new
value. Contracts live at:
`https://github.com/linuxfoundation/lfx-v2-SERVICENAME/blob/main/docs/indexer-contract.md`

The filter-to-mechanism mapping for query service payloads:

| Mechanism                               | Query service field | Index field          |
|-----------------------------------------|---------------------|----------------------|
| `payload.Parent = "<type>:<uid>"`       | `Parent`            | `parent_refs`        |
| `payload.Tags = ["<key>:<value>"]`      | `Tags`              | `tags`               |
| `payload.Filters = ["<field>:<value>"]` | `Filters`           | top-level doc fields |

### Step 2.3 — Verify

After all fixes, run:

```bash
make build && ./scripts/test_server.sh
```

Both must pass before proceeding to Phase 3.

---

## Phase 3 — Query service argument review

Run the full `validate-search-filters` skill to cross-validate every filter
parameter in every tool against the live OpenSearch index and the now-updated
contracts.

> **Instruction to the agent**: load and execute the `validate-search-filters`
> skill now. Complete all steps (1 through 6) and produce the full report.
> Only apply fixes (Step 7) if explicitly instructed by the user.

The output of the validate-search-filters skill drives any remaining filter
corrections. If Step 7 fixes are applied there, re-run `make build` and
`./scripts/test_server.sh` again after.

---

## Phase 4 — Final verification and wrap-up

### Step 4.1 — Full quality check

```bash
make check
```

Fix any lint or vet errors reported.

### Step 4.2 — Full build and test

```bash
make build && ./scripts/test_server.sh
```

Both must complete without errors before the maintenance pass is considered
done.

### Step 4.3 — Summarise changes

Produce a short plain-text summary of:

1. Which dependencies were upgraded, noting any Goa CRUD service bumps and
   what structural changes (new RPCs, removed/added fields) required fixes.
2. Which view mapping functions were updated and what new fields were included
   or explicitly excluded.
3. The final validate-search-filters report (verdict table, any remaining ⚠️
   items).
4. Any items that could not be fixed automatically and require human follow-up.

This summary is suitable for a PR description or Jira comment.
