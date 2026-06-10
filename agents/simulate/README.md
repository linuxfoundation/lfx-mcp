# Agentic review simulator

Test the [dual-agent PR review](../pr-reviewer/AGENTS.md) against real pull
requests without posting anything. The simulator runs the `pr-reviewer` and
`escalation-reviewer` agents (Codex CLI, read-only sandbox) on a PR's diff,
computes the verdict the deterministic gate WOULD reach, and compares it
with what actually happened on the PR: the human and Copilot comments, the
approvals, and the merge outcome.

## Run it locally

Requirements: `codex` (`npm install -g @openai/codex`), `gh` (authenticated),
`jq`, `python3`, and `OPENAI_API_KEY` in the environment.

```bash
# Replay PR 17 as it looked when it was opened, with semantic matching of
# simulated vs actual comments:
./agents/simulate/simulate.sh --pr 17 --at open --judge

# Quick shadow of an open PR's current state, structural comparison only:
./agents/simulate/simulate.sh --pr 42
```

Results land in `agents/simulate/runs/pr-<n>-<state>/`: `report.md` is the
human-readable outcome; `findings.json`, `escalation.json`, the Codex
session transcripts (`*-transcript.log`: every message and command of each
agent's run), and the fetched PR data sit next to it. To watch an agent
think while a run is in progress: `tail -f` its transcript. The `runs/`
directory is for local output only; do not commit it.

## What a run does

1. Fetches the PR and resolves the head to review. `--at open` picks the
   newest commit not after the PR's creation time; a pre-open force-push can
   defeat that heuristic, and the script then falls back to the current head
   with a warning.
2. Checks the head out in a throwaway worktree, with `agents/` overlaid from
   YOUR checkout, so the agents under test are the ones you are editing, not
   whatever the historical commit had.
3. Runs both agents in parallel via `codex exec --sandbox read-only`, each
   booted in its own directory per the cwd-as-identity design, with the
   pre-rendered diff and the PR title/body as the brief.
4. Fetches the PR's actual inline comments, reviews, and conversation
   (Copilot's comments arrive like any other author's and are compared the
   same way).
5. `compare.py` writes `report.md`: the simulated gate decision
   (clean + `needs-human: false` = WOULD APPROVE), the findings, what
   actually happened, and file-overlap signals. With `--judge`, a third
   Codex run matches the two comment sets semantically (matched /
   only-simulated / only-actual) and judges whether auto-approval at that
   state would have been safe, knowing how the PR ended.

## CI shadow mode

`.github/workflows/agentic-review-shadow.yml` runs the same script:

- On every PR (once merged to the default branch): reviews the current head
  and publishes the report to the job summary plus an artifact. Permissions
  are `read` only; it cannot comment, label, or approve. If the
  `OPENAI_API_KEY` secret is not configured the job skips politely.
- Via *Run workflow* (`workflow_dispatch`): replays any PR number, at its
  opening state by default, with the judge on.

## Caveats

- The Codex CLI flags (`exec`, `--sandbox read-only`,
  `--output-last-message`, prompt on stdin via `-`) should be re-verified on
  the first run with a real key; tune `simulate.sh` if the CLI has drifted.
- A replayed escalation verdict applies today's guidelines to yesterday's
  PR; differences can be the guidelines improving, not the agent erring.
- The gate's third condition (all threads resolved) has no analog in a
  simulation and is reported as out of scope.
