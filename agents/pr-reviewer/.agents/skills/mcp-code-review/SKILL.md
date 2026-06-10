---
name: mcp-code-review
description: >
  How to judge the implementation of an lfx-mcp pull request: the general
  quality dimensions (correctness, error handling, tests, performance,
  readability, code truthfulness) and how to hold the diff to the repo's
  documented standards. Use on every PR that changes code, however small;
  this is the reviewer's line-level lens. Security has its own skill
  (mcp-security-review).
allowed-tools: Read, Glob, Grep
---

# LFX MCP Server Code Review

Judge the implementation the way a senior reviewer the team trusts would:
thorough yet pragmatic, catching real issues while respecting the author's
time. Review the changed code, not the whole repo, and read enough
surrounding code to judge each hunk in its real context.

## The house standards

The repo defines its own standards; hold the diff to them, and name the
documented source in any standards finding. They live in the repo's
documentation (`CLAUDE.md` / the root `AGENTS.md`, `ARCHITECTURE.md`, and
what they point to): code layout, the error model, how tools are registered
and described, test expectations, license headers. Read the parts relevant
to the diff before judging, every run, because the standards belong to the
repo and move with it.

Enforcement runs in both directions: code that violates a documented standard
is a finding, and a documented standard the code has visibly outgrown is a
finding against the docs. If a documented convention is wrong for this
specific change, say so explicitly and explain the trade, rather than
silently waiving or silently enforcing it.

## Quality dimensions

Run these on the changed code, scaled to the size of the change:

- **Correctness**: does it do what it claims? Unthreaded or uncancelled
  contexts, ignored errors, races, cache-keying mistakes.
- **Error handling**: failures follow the repo's documented error model and
  are neither silently swallowed nor leaked to MCP clients; error paths
  clean up what they opened.
- **Tests**: new or changed behavior has tests that assert real behavior,
  not that a mock was called; missing tests on gating, token, or
  tool-contract code is at least `should-fix`.
- **Performance**: no unbounded fan-out or fetch against an upstream; no
  blocking call without a deadline; caches bounded.
- **Readability and structure**: the change reads like the surrounding code
  and respects the repo's layout; names say what a thing is or does;
  duplicated logic that wants a shared helper is a finding when it traps the
  next editor.
- **Code truthfulness**: comments, docs, tool descriptions, and the PR
  description match what the code actually does; a tool description that
  oversells or undersells its handler misleads every model that reads it.

## Writing findings

- **Specific and actionable.** Name the exact file and line, explain *why*
  it is a problem here (not just what), and show what a fix looks like. When
  the diff violates a pattern, point at the working pattern in the
  surrounding code rather than describing an abstract ideal.
- **Pragmatic.** Substance over style: leave to the linters what the linters
  own, do not propose rewrites of a sound approach, and do not suggest
  change for its own sake; working, readable code needs no improvement.
  Pre-existing issues the diff does not touch are at most a `nit`.
- **Know your limits.** Distinguish "this is wrong" from "this might be a
  problem depending on context", and say which one you mean. When a judgment
  depends on something you cannot see (a peer repo's contract, an upstream
  API's behavior), state the dependency in the finding instead of guessing.
- **Credit what is good.** When the change handles something well (a tricky
  edge case, a careful token-cache fix), say so in the verdict's summary; it
  shows the review was real and reinforces the pattern.

Severities come from `AGENTS.md`; this skill decides what is a finding, not
the ladder.
