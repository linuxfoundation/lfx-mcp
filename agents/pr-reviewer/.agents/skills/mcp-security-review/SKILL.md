---
name: mcp-security-review
description: >
  Security review for lfx-mcp pull requests. Use when a PR touches auth, tool
  registration or gating, token handling, a tool handler, an upstream client,
  config, or the chart. Applies a diff-aware, high-confidence,
  low-false-positive methodology (adapted from Anthropic's
  claude-code-security-review) to this server's durable threat anchors:
  inbound credential verification, registration-time gating, the two-class
  upstream authorization model, token plumbing and caching, write tools
  against production data, output-schema exposure, and secrets. Discovers the
  concrete guards from the code at review time; this skill carries the
  method, not an inventory.
allowed-tools: Read, Glob, Grep
---

# LFX MCP Server Security Review

This server is an **authorization gateway in front of production LFX data**,
and its callers are AI assistants driven by text the platform does not
control: a weakened boundary here is not a bug in one feature, it is a door
into every project's data. That fact sets the stakes for every security
judgment.

## Methodology

Run a focused, **diff-aware** review, not a whole-repo audit:

1. **Only new risk.** Assess what this PR introduces or weakens. Do not
   relitigate pre-existing issues the diff does not touch (at most a `nit`).
2. **Assume hostile input, report only what is real.** Flag only
   high-confidence, concretely exploitable findings: if you cannot trace a
   path from an attacker-controlled input to a sensitive sink, it is not
   `critical`/`high`.
3. **Three passes.**
   - *Context*: discover, from the code and `ARCHITECTURE.md` at review
     time, the guards this server relies on around the diff (token
     verification, registration gates, access checks, cache keying,
     stripped output fields). Never assume a guard exists; find it.
   - *Comparative*: does the change deviate from the guard patterns the
     surrounding code establishes?
   - *Assessment*: trace each input to its sink and confirm a guard sits on
     the path the data actually takes, not three functions away.
4. **Confidence-gate every finding** (1-10, report only >= 7). A few real
   findings beat a speculative list.
5. **Evidence, not vibes.** Each finding names the file and function, what
   the attacker controls, the boundary crossed, the concrete impact, and
   the fix.

## Durable threat anchors

These are the kinds of boundaries that make a diff security-relevant in this
server. They describe its shape, not its current line-level guards; verify
the concrete mechanism in the code each time.

- **Inbound credential verification.** Which tokens and keys the server
  accepts, how it validates them, and what identity, scopes, and claims it
  derives. Watch for weakened validation (audience, expiry, algorithms),
  widened acceptance, and verification errors echoed to unauthenticated
  callers.
- **Registration-time gating.** A caller's tools are decided once, from its
  token, at registration; for most tools nothing is checked again. A gate
  moved, a tool registered under a weaker access level, or a divergence
  between a tool's declared read-only behavior and what its handler does is
  a permission change with no handler diff.
- **The two-class upstream model.** Brokered tools must check access before
  proxying; pass-through tools must forward the caller's token, not a
  stronger one. The failure modes are a brokered tool losing its check, a
  new brokered tool shipping without one, and a tool drifting between
  classes. Confirm the class from `ARCHITECTURE.md`, then confirm the code
  honors it.
- **Token plumbing and caching.** Exchanged user tokens carry identity into
  access checks; machine and per-service tokens carry the server's own
  authority. Mis-deriving, mis-caching (keying a token to the wrong
  caller), over-scoping, logging, or forwarding the wrong token can
  authorize the wrong principal silently.
- **Write tools against production data.** Any tool that creates, updates,
  or deletes through an upstream. New destructive modes, bulk operations,
  removed preview or dry-run guards, and parameters that widen blast radius
  get the full trace-to-sink treatment.
- **Tool output as an exposure channel.** Typed output schemas are generated
  from handler types, so adding a struct field can publish an internal
  identifier to every MCP client; prompt-visible text (tool descriptions,
  error strings) can leak upstream details. Check what new data leaves the
  server, and to whom.
- **Tool input as an injection channel.** Tool parameters arrive from
  model-driven callers; anything interpolated into an upstream request
  (path, query, body, headers) needs the same encoding discipline the
  surrounding code establishes. An input that selects which upstream or
  host gets called is the sharpest version.
- **Secrets and config.** Client credentials, signing keys, API keys, and
  the env contract that configures them. None may appear in logs, traces,
  responses, or plaintext chart values; the hosted OAuth surface (metadata,
  advertised scopes, public URLs) is part of this boundary.

## What not to flag

Signal discipline keeps the reviewer trusted. Do not raise:

- Denial of service, resource exhaustion, or "add rate limiting" on their
  own.
- Mere lack of hardening or defense-in-depth with no concrete vulnerability.
- Outdated third-party dependencies (managed separately); a *new*
  dependency's risk belongs to the architecture lens.
- Theoretical race or timing issues with no practical exploit.
- Test-only files, Markdown, and docs.
- Log spoofing, regex-DoS, and missing audit logs.
- SSRF that only controls a path; it counts when the attacker controls host
  or protocol.

Precedents: UUIDs are unguessable and need no validation (an authorization
finding rests on a missing check, not on guessing an id); environment
variables and config are trusted inputs; logging URLs and non-PII is fine.

## Reporting

For each finding give the file and function, what the attacker controls, the
boundary crossed, the concrete impact on this server, and the fix. If the
diff does not touch an anchor above, do not invent a finding for it.
