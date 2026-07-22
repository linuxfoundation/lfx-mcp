---
name: agentic-comment-format
description: >-
  The exact format for the two verdict comments the agentic review posts on a
  lfx-mcp pull request: the needs-human verdict (escalation) and the
  agentic-check verdict (conductor). Use whenever you post either verdict. Defines the
  human presentation and the machine-readable markers that the deterministic apply
  step parses, so writer and reader stay in sync.
---

<!-- Copyright The Linux Foundation and each contributor to LFX. -->
<!-- SPDX-License-Identifier: MIT -->

# Agentic verdict comment format

Both agentic roles publish exactly one verdict comment on the pull request, via the
`add_issue_comment` tool. Each comment is two things at once: a clear, useful message
for the engineer, and a machine-readable marker that a deterministic workflow step
(`agentic-apply.yml`) parses to set labels and the commit status.

The markers are **load-bearing** — keep them exactly as written here or the
deterministic step stops working. The prose around them is yours to make genuinely
good to read.

Shared rules:

- **One comment per verdict.** Never split it across comments.
- **Markers are HTML comments** (`<!-- ... -->`) so they are invisible in the rendered
  view and never clutter what the engineer sees. The deterministic step greps for them.
- **Write for a busy engineer:** lead with the outcome, be specific, point at the code,
  and do not pad.

## Needs-human verdict (escalation judge)

Posted when the PR opens and again for each new head (the escalation re-runs per
push; the label it drives is sticky and add-only). When a human must sign off
before merge:

```
<!-- agentic:needs-human v1 -->
<!-- needs-human: yes -->
<!-- head: <full 40-char SHA of the head you judged> -->
### Needs a human before merge

**Why:** <one specific sentence: what a lead needs to know about and why>
```

When no human sign-off is required:

```
<!-- agentic:needs-human v1 -->
<!-- needs-human: no -->
<!-- head: <full 40-char SHA of the head you judged> -->
### No human sign-off required

<one specific sentence: what you checked and why this change is routine>
```

The `<!-- needs-human: yes -->` / `<!-- needs-human: no -->` line is the machine signal.
The deterministic step sets the sticky `needs-human` label when it is `yes`, and does
nothing when it is `no`. Do not set the label yourself, and write the marker exactly —
it is the only place the words `needs-human: yes|no` may appear in your comment.

The `<!-- head: ... -->` line binds your verdict to the exact head you judged: read
the PR's current head SHA immediately before posting and write all 40 characters.
The gate only honors a verdict whose `head:` equals the head it is about to
approve, so a stale verdict from an earlier push can never vouch for newer commits.

## Agentic-check verdict (conductor)

Posted after each review round. The baseline (first-round) check is authored
deterministically by the conductor workflow itself in this exact same format —
every non-nit finding as an `outstanding` row; later rounds come from the
reconcile agent. A human summary first, then one raw collapsed `<details>`
ledger — never wrap it in backticks or a code fence, which would render the
`<details>` element as literal text instead of collapsing it:

```
### Agentic review check — <✅ clean | ❌ N blocking>

<one or two lines: the state of the change and what remains to reach clean>

**Blocking**

| Severity | Finding | Next step |
| --- | --- | --- |
| high | <short finding> | <what a real fix needs> |

**Remaining tidiness:** <only when clean but a thread fails the gate's tidiness
rule: name every thread with no reply yet (nits and human threads included), and
say the gate approves once each is answered — fixed and said so, or replied with
the reason it stands>

**Handled well:** <one line on what the change got right, when there is something>

<details>
<summary>Machine ledger (conductor state)</summary>

<!-- agentic:check v1 -->
head: <full 40-char commit SHA of the head you judged>
clean: true|false
threads:
- id: <thread_node_id>, status: fixed|obsolete|outstanding|rebutted-valid|rebutted-invalid, severity: critical|high|should-fix|nit, reason: <one short sentence>

</details>
```

Rules the deterministic step depends on, so be exact:

- The machine block lives inside the collapsed `<details>` element exactly as
  shown, so the engineer reads the prose and the ledger stays out of the way —
  it is bookkeeping for the deterministic step and for later rounds, not part
  of the human message. Keep the summary line verbatim and put nothing after
  `</details>`.
- The block begins with the literal `<!-- agentic:check v1 -->` line (the
  parser reads from that line to the end of the comment, so the wrapper does
  not affect it).
- `head:` is the full commit SHA of the PR head you actually judged. The deterministic
  step sets the clean status on **that** commit, so a commit that lands after you post
  cannot inherit this verdict (it re-derives as not-yet-clean and the gate stays shut).
- `clean:` is `true` only when no thread blocks (none is `outstanding` or
  `rebutted-invalid`); otherwise `false`.
- One `- id:` line per thread you adjudicated, its four fields comma-separated —
  `id`, `status`, `severity`, then a one-sentence `reason` — in that order, all on
  the one line. The commas keep the fields scannable for both the engineer and the
  parser. **Exception:** an unaddressed nit gets no row at all (see below); the
  per-thread completeness rule applies to every other adjudicated thread.
- The **Blocking** table lists only the blocking rows and mirrors the block. When
  `clean: true` there are no blocking rows: drop the table and say plainly that it is
  clean.
- **Never** emit an `outstanding` or `rebutted-invalid` row for a nit: blocking
  rows flip `clean` to false, and a nit must not block. An unaddressed nit is
  prose only — the **Remaining tidiness** line — because the gate withholds its
  approving review while any thread has no reply, even on a clean change. This is
  the one exception to the one-row-per-adjudicated-thread rule above; an addressed
  nit (`fixed`/`obsolete`/`rebutted-valid`) still gets its row (and its reply) so
  the record shows why it cleared.

## What the deterministic step reads

`agentic-apply.yml` acts only on a comment authored by the `lfx-reviewer` machine
account (so a developer cannot forge a verdict). From that comment it:

- sets the sticky `needs-human` label when it sees `<!-- needs-human: yes -->`;
- from the `<!-- agentic:check v1 -->` block: validates the rows (shape, PR
  membership, consistency with `clean:`) and sets the `agentic-review/clean`
  commit status from `clean:`. It does not touch thread state — no automation
  token can — which is why your per-thread replies matter: they are the record,
  and the gate requires every thread to have one.

Nothing else you write is parsed, so the surrounding prose is entirely for the engineer.
