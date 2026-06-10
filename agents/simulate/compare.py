#!/usr/bin/env python3
# Copyright The Linux Foundation and each contributor to LFX.
# SPDX-License-Identifier: MIT
"""Build the agentic-review simulation report.

Reads the output directory simulate.sh produced (agent verdicts, PR
metadata, and what was actually posted on the PR) and writes a markdown
report to stdout: the simulated verdict including the gate's would-approve
decision, what actually happened, and structural side-by-side signals.
Semantic matching of comments is the optional Codex judge's job
(match-analysis.md); this script stays deterministic.
"""

import json
import re
import sys
from pathlib import Path

BLOCKING = ("critical", "high", "should-fix")
MAXLEN = 240


def read_json(path, default=None):
    if not path.exists():
        return default
    try:
        return json.loads(path.read_text())
    except json.JSONDecodeError:
        return default


def read_agent_json(path):
    """Agent verdicts arrive as the model's final message; tolerate code
    fences or stray prose around the JSON object."""
    if not path.exists():
        return None
    text = path.read_text().strip()
    text = re.sub(r"^```(?:json)?\s*", "", text)
    text = re.sub(r"\s*```$", "", text)
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        match = re.search(r"\{.*\}", text, re.S)
        if match:
            try:
                return json.loads(match.group(0))
            except json.JSONDecodeError:
                return None
    return None


def clip(text, limit=MAXLEN):
    text = " ".join((text or "").split()).replace("|", "\\|")
    return text if len(text) <= limit else text[: limit - 3] + "..."


def main():
    out = Path(sys.argv[1])
    meta = read_json(out / "meta.json", {})
    pr = read_json(out / "pr.json", {})
    findings_doc = read_agent_json(out / "findings.json")
    escalation = read_agent_json(out / "escalation.json")

    lines = []
    add = lines.append

    title = pr.get("title", "")
    add(f"# Agentic review simulation: PR #{meta.get('pr', '?')} of {meta.get('slug', '?')}")
    add("")
    add(f"**{clip(title, 120)}**")
    add("")
    add(f"- Reviewed state: `{meta.get('reviewed_state', '?')}` "
        f"(head `{meta.get('head_sha', '?')[:12]}`, merge base `{meta.get('merge_base', '?')[:12]}`)")
    add(f"- PR status now: {pr.get('state', '?')}"
        + (f", merged {pr.get('merged_at')}" if pr.get("merged_at") else ""))
    add("")

    # Simulated verdict ----------------------------------------------------
    add("## Simulated verdict")
    add("")
    findings = (findings_doc or {}).get("findings", [])
    blocking = [f for f in findings if f.get("severity") in BLOCKING]
    needs_human = bool((escalation or {}).get("needs-human"))

    if findings_doc is None or escalation is None:
        missing = [n for n, d in (("findings.json", findings_doc),
                                  ("escalation.json", escalation)) if d is None]
        add(f"**NO VERDICT**: could not parse {', '.join(missing)} "
            "(see the transcript logs).")
    elif not blocking and not needs_human:
        add("**Gate: WOULD APPROVE** (review clean, `needs-human` false). "
            "Thread resolution, the gate's third condition, has no analog in "
            "a simulation.")
    else:
        reasons = []
        if blocking:
            reasons.append(f"{len(blocking)} blocking finding(s)")
        if needs_human:
            reasons.append(f"needs-human: {clip((escalation or {}).get('reason', ''), 160)}")
        add(f"**Gate: WOULD NOT APPROVE** ({'; '.join(reasons)}).")
    add("")

    if escalation is not None:
        verdict = "true" if needs_human else "false"
        reason = clip(escalation.get("reason", ""), 200)
        add(f"- Escalation verdict: `needs-human: {verdict}`"
            + (f" : {reason}" if reason else ""))
    if findings_doc is not None:
        add(f"- Reviewer summary: {clip(findings_doc.get('summary', ''), 600)}")
    add("")

    if findings:
        add("| severity | location | finding |")
        add("| --- | --- | --- |")
        for f in findings:
            loc = f.get("file", "?")
            if f.get("line"):
                loc += f":{f['line']}"
            add(f"| {f.get('severity', '?')} | `{loc}` | {clip(f.get('comment', ''))} |")
    elif findings_doc is not None:
        add("No findings.")
    add("")

    # What actually happened ----------------------------------------------
    inline = read_json(out / "actual-review-comments.json")
    reviews = read_json(out / "actual-reviews.json")
    issue_comments = read_json(out / "actual-issue-comments.json")

    if inline is not None:
        add("## What actually happened")
        add("")
        approvals = [r for r in (reviews or []) if r.get("state") == "APPROVED"]
        if approvals:
            who = ", ".join(sorted({a.get("user", {}).get("login", "?") for a in approvals}))
            add(f"- Approved by: {who}")
        add(f"- Inline review comments: {len(inline)}; reviews: {len(reviews or [])}; "
            f"conversation comments: {len(issue_comments or [])}")
        add("")
        if inline:
            add("| author | location | comment |")
            add("| --- | --- | --- |")
            for c in inline:
                login = c.get("user", {}).get("login", "?")
                loc = c.get("path", "?")
                if c.get("line") or c.get("original_line"):
                    loc += f":{c.get('line') or c.get('original_line')}"
                add(f"| {login} | `{loc}` | {clip(c.get('body', ''))} |")
            add("")
        bodies = [r for r in (reviews or []) if (r.get("body") or "").strip()]
        if bodies:
            add("Review summaries:")
            add("")
            for r in bodies:
                add(f"- **{r.get('user', {}).get('login', '?')}** ({r.get('state', '?')}): "
                    f"{clip(r.get('body', ''))}")
            add("")

        # Side-by-side signals ---------------------------------------------
        add("## Side-by-side signals")
        add("")
        sim_files = {f.get("file") for f in findings if f.get("file")}
        act_files = {c.get("path") for c in inline if c.get("path")}
        add(f"- Files flagged by both: {sorted(sim_files & act_files) or 'none'}")
        add(f"- Only simulated: {sorted(sim_files - act_files) or 'none'}")
        add(f"- Only actual: {sorted(act_files - sim_files) or 'none'}")
        add("")
        analysis = out / "match-analysis.md"
        if analysis.exists():
            add("## Judge analysis (semantic matching)")
            add("")
            add(analysis.read_text().strip())
            add("")
        else:
            add("File overlap is a coarse signal; rerun with `--judge` for "
                "semantic matching of the comment sets.")
            add("")

    transcripts = sorted(p.name for p in out.glob("*-transcript.log"))
    if transcripts:
        add("---")
        add("")
        add("Codex session transcripts: "
            + ", ".join(f"`{t}`" for t in transcripts)
            + " (next to this report; in CI, inside the workflow artifact).")
        add("")

    print("\n".join(lines))


if __name__ == "__main__":
    main()
