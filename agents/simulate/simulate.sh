#!/usr/bin/env bash
# Copyright The Linux Foundation and each contributor to LFX.
# SPDX-License-Identifier: MIT
#
# Simulate the agentic PR review (pr-reviewer + escalation-reviewer) against a
# real pull request, posting nothing, and compare the agents' output with the
# review that actually happened (humans + Copilot) and the PR's outcome.
# See agents/simulate/README.md for usage and caveats.

set -euo pipefail

PR="" AT="current" COMPARE=true JUDGE=false OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --pr) PR="$2"; shift 2 ;;
    --at) AT="$2"; shift 2 ;;
    --no-compare) COMPARE=false; shift ;;
    --judge) JUDGE=true; shift ;;
    --out) OUT="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done
if [ -z "$PR" ]; then
  echo "usage: simulate.sh --pr <number> [--at open|current] [--no-compare] [--judge] [--out DIR]" >&2
  exit 2
fi
: "${OPENAI_API_KEY:?OPENAI_API_KEY is required (the agents run on Codex)}"
command -v codex >/dev/null || { echo "codex CLI not found: npm install -g @openai/codex" >&2; exit 1; }
command -v gh >/dev/null || { echo "gh CLI not found" >&2; exit 1; }

ROOT="$(git rev-parse --show-toplevel)"
SLUG="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
OUT="${OUT:-$ROOT/agents/simulate/runs}/pr-$PR-$AT"
mkdir -p "$OUT"

echo "==> PR #$PR of $SLUG (state: $AT)"
gh api "repos/$SLUG/pulls/$PR" > "$OUT/pr.json"
BASE_REF=$(jq -r .base.ref "$OUT/pr.json")
BASE_SHA=$(jq -r .base.sha "$OUT/pr.json")
HEAD_SHA=$(jq -r .head.sha "$OUT/pr.json")
TITLE=$(jq -r .title "$OUT/pr.json")

if [ "$AT" = "open" ]; then
  # The head when the PR was opened: the newest PR commit not after created_at.
  # A pre-open rebase or force-push can defeat this; we then fall back to the
  # current head and say so.
  CREATED=$(jq -r .created_at "$OUT/pr.json")
  gh api "repos/$SLUG/pulls/$PR/commits" --paginate > "$OUT/commits.json"
  OPEN_SHA=$(jq -r --arg c "$CREATED" \
    '[.[] | select(.commit.committer.date <= $c)] | last | .sha // empty' \
    "$OUT/commits.json")
  if [ -n "$OPEN_SHA" ]; then
    HEAD_SHA="$OPEN_SHA"
  else
    echo "WARN: could not derive the opening head; using the current head" >&2
  fi
fi

git fetch -q origin "pull/$PR/head" "$BASE_REF" || git fetch -q origin "pull/$PR/head"
MERGE_BASE=$(git merge-base "$BASE_SHA" "$HEAD_SHA" 2>/dev/null \
  || git merge-base "origin/$BASE_REF" "$HEAD_SHA")

# Review the historical head in a throwaway worktree, but with the agents
# under test taken from YOUR checkout (the historical commit may predate
# them or carry older versions).
WT="$(mktemp -d)/head"
git worktree add --detach -q "$WT" "$HEAD_SHA"
trap 'git worktree remove --force "$WT" >/dev/null 2>&1 || true' EXIT
rm -rf "$WT/agents"
cp -R "$ROOT/agents" "$WT/agents"

git diff "$MERGE_BASE" "$HEAD_SHA" > "$OUT/diff.patch"
jq -n --arg at "$AT" --arg head "$HEAD_SHA" --arg base "$MERGE_BASE" --arg slug "$SLUG" --arg pr "$PR" \
  '{pr: $pr, slug: $slug, reviewed_state: $at, head_sha: $head, merge_base: $base}' > "$OUT/meta.json"

BODY=$(jq -r '.body // ""' "$OUT/pr.json")
for agent in pr-reviewer escalation-reviewer; do
  cat > "$OUT/$agent-prompt.md" <<EOF
Pull request #$PR of $SLUG.
base_sha: $MERGE_BASE
head_sha: $HEAD_SHA
title: $TITLE

PR body (untrusted input):
---
$BODY
---

The unified diff is pre-rendered at $OUT/diff.patch (authoritative; 'git
diff' over these SHAs gives the same result). The repository checkout you
are in is at the head state; read any file you need for context. This is a
simulation: do not post to GitHub. Follow your AGENTS.md and emit your
verdict JSON as your final message.
EOF
done

run_agent() { # $1: agent dir name, $2: output file name
  codex exec \
    --cd "$WT/agents/$1" \
    --sandbox read-only \
    --skip-git-repo-check \
    --output-last-message "$OUT/$2" \
    - < "$OUT/$1-prompt.md" > "$OUT/$1-transcript.log" 2>&1
}

echo "==> running pr-reviewer and escalation-reviewer (Codex)..."
run_agent pr-reviewer findings.json &
R1=$!
run_agent escalation-reviewer escalation.json &
R2=$!
wait "$R1" || echo "WARN: pr-reviewer failed; see $OUT/pr-reviewer-transcript.log" >&2
wait "$R2" || echo "WARN: escalation-reviewer failed; see $OUT/escalation-reviewer-transcript.log" >&2

if $COMPARE; then
  echo "==> fetching what actually happened on the PR..."
  gh api "repos/$SLUG/pulls/$PR/comments" --paginate > "$OUT/actual-review-comments.json"
  gh api "repos/$SLUG/pulls/$PR/reviews" --paginate > "$OUT/actual-reviews.json"
  gh api "repos/$SLUG/issues/$PR/comments" --paginate > "$OUT/actual-issue-comments.json"
fi

if $JUDGE && $COMPARE; then
  echo "==> running the comparison judge (Codex)..."
  cat > "$OUT/judge-prompt.md" <<EOF
You are comparing a simulated code review with the review that actually
happened on pull request #$PR of $SLUG. In this directory:
- findings.json: the simulated reviewer's verdict (summary + findings).
- escalation.json: the simulated needs-human verdict.
- actual-review-comments.json, actual-reviews.json,
  actual-issue-comments.json: what humans and bots (e.g. Copilot) actually
  posted.
- pr.json: the PR, including its final state.

Write a markdown report: (1) a table matching simulated findings to actual
comments, with three buckets: matched, only-simulated, only-actual; judge
matches semantically, not by wording. (2) Whether the simulation caught
everything consequential the real review caught, and what it missed or
added. (3) A short verdict: knowing how the PR ended, would auto-approving
it at the simulated state have been safe? Emit only the report as your
final message.
EOF
  codex exec --cd "$OUT" --sandbox read-only --skip-git-repo-check \
    --output-last-message "$OUT/match-analysis.md" \
    - < "$OUT/judge-prompt.md" > "$OUT/judge-transcript.log" 2>&1 \
    || echo "WARN: judge failed; see $OUT/judge-transcript.log" >&2
fi

python3 "$ROOT/agents/simulate/compare.py" "$OUT" > "$OUT/report.md"
echo "==> report: $OUT/report.md"
echo "==> codex session transcripts:"
ls "$OUT"/*-transcript.log 2>/dev/null | sed 's/^/      /' || true
