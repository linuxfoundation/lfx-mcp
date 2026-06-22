# Copilot Instructions — LFX MCP Server

## What This Repository Does

This is the **LFX MCP Server** — a Model Context Protocol (MCP) implementation in Go that exposes the Linux Foundation's LFX platform as MCP tools for AI agents. It uses the official MCP Go SDK and supports JSON-RPC 2.0 over stdio (dev) and Streamable HTTP (production) transports.

**Key facts**: ~25 Go source files, single binary, no database, stateless HTTP mode. Go module version in `go.mod`, MCP SDK v1.6+, built with `ko` for container images. Deployed to Kubernetes via Helm.

---

## CI Rules (what breaks the build)

Two workflows run on every PR (`.github/workflows/`):

1. **`license-header-check.yml`** — every tracked source file must begin with the license header.
2. **`mega-linter.yml`** — MegaLinter Go flavor v9 (config: `.mega-linter.yml`).

### Requirements for new/modified files

**License header** — always include as the first lines:

```go
// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT
```

For YAML, shell, and Makefile use `#` comment syntax. Missing headers are the #1 cause of CI failure.

**Package doc comment** — every `.go` file must have `// Package <name> ...` immediately above the `package` declaration. The `GO_REVIVE` linter enforces the `package-comments` rule.

**YAML formatting** — lines must be ≤120 characters (config: `.yamllint`). Helm templates in `charts/lfx-mcp/templates/` are excluded from YAML linting.

**Other active MegaLinter checks:** `BASH_SHELLCHECK`, `DOCKERFILE_HADOLINT`, `ACTION_ACTIONLINT`, `REPOSITORY_GITLEAKS`, `MARKDOWN_MARKDOWNLINT`.

**Disabled/error-only (will NOT fail CI):** `GO_GOLANGCI_LINT`, `YAML_PRETTIER`, `REPOSITORY_TRIVY`, `REPOSITORY_CHECKOV`, `REPOSITORY_DEVSKIM`, `SPELL_CSPELL`, `SPELL_LYCHEE`, `COPYPASTE_JSCPD`.

---

## Project Layout

```
cmd/lfx-mcp-server/main.go    — Entry point, config, flag parsing, tool registration
internal/tools/                — All MCP tool implementations (one file per tool/domain)
internal/tools/scopes.go       — Scope constants (ScopeRead, ScopeManage)
internal/tools/helpers.go      — Shared utilities for tool handlers
internal/auth/                 — JWT verification, API-key verification
internal/lfxv2/                — LFX V2 API client (token exchange, slug resolver, access checks)
internal/serviceapi/           — Generic HTTP client for downstream service APIs
internal/otel/                 — OpenTelemetry initialization
charts/lfx-mcp/               — Helm chart (deployment, ingress, service, PDB)
Makefile                       — Build automation (targets: build, test, check, clean, etc.)
.mega-linter.yml               — MegaLinter config
.yamllint                      — YAML lint rules (max line length: 120)
.ko.yaml                       — ko builder config with ldflags
Dockerfile                     — Multi-stage build (Chainguard base images)
AGENTS.md                      — Detailed developer guide (canonical; CLAUDE.md is a symlink)
ARCHITECTURE.md                — System architecture with Mermaid diagrams
```

---

## Adding or Modifying Tools

Each tool lives in `internal/tools/<domain>.go`. The pattern is:

1. Define an args struct with `json` + `jsonschema` tags.
2. Write a `Register<ToolName>(server *mcp.Server)` function that calls `mcp.AddTool`.
3. Write a `handle<ToolName>` function implementing the logic.
4. Register the tool in `cmd/lfx-mcp-server/main.go` inside `newServer()`, gated on `canRead` or `canManage`.
5. Add the tool name to the `defaultTools` slice (also in `main.go`) if it should be enabled by default.

Tool annotations: always set `ReadOnlyHint: true` for read tools. Write tools must explicitly set `DestructiveHint`.

---

## Key Conventions

- **Package comments**: Every `.go` file needs a `// Package <name> ...` comment (revive enforces this).
- **Error constant**: Use `const errKey = "error"` for structured logging error keys.
- **Logging**: Use `slog` (Go stdlib). Debug-only logs use `slog.Debug(...)`.
- **No wrapper functions for scope enforcement** — tool gating is done inline in `newServer()`.
- **JSON schema generation**: The MCP SDK auto-generates schemas from struct tags; no manual schema files.
- **`schemaCache`**: A package-level cache shared across per-request server instances; do not duplicate it.

---

## Common Pitfalls

- Forgetting the license header on new files is the #1 cause of CI failure.
- New `.go` files without a package doc comment will fail the revive `package-comments` rule.
- The `defaultTools` list in `main.go` controls which tools are enabled; adding a Register call without adding the name to `defaultTools` means the tool is never active.
- Helm chart templates (`charts/lfx-mcp/templates/`) are excluded from YAML linting via regex in `.mega-linter.yml`.

---

## Trust These Instructions

These instructions are validated and current. Only perform additional exploration if the information above is incomplete or produces errors. For detailed architecture, tool patterns, and environment variable reference, consult `AGENTS.md` in the repo root.
