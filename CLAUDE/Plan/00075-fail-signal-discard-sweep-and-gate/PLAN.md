# Plan 00075: Discarded failure signals — sweep the repo and build the gate

**Status**: In Progress
**Created**: 2026-08-17
**Owner**: joseph
**Priority**: High

## Overview

A single defect class produced two separate incidents in one session, one of them
user-facing and launch-blocking. The shape is:

> **A command's failure is silently converted into data, and that data is then
> used to make a decision or reported to the user as fact.**

The failure is not that something broke. It is that the breakage was *laundered
into a confident wrong answer*, which is strictly worse than an error: an error
sends you to the real cause, a wrong answer sends you somewhere else entirely.

This plan sweeps the repo for the class and — the actual point — builds tooling
that stops it being written again. The owner's framing is **"defence before
fix"**: a gate that fails a build, not a reviewer who might notice.

## The two incidents

**1. A GitHub outage reported as a configuration error** (`ssh-handling.bash`,
fixed in CCY 3.36.0):

```bash
token_user=$(GH_TOKEN="$GH_TOKEN" gh api user --jq .login 2>/dev/null)
if [ -n "$token_user" ] && [ "$token_user" != "$GITHUB_USERNAME" ]; then
```

GitHub answered 502. `gh` wrote the JSON error body to **stdout** and exited
non-zero. The status was discarded, so the error body became "the account name",
and ccy refused to launch while telling the user to go and edit `localhost.yml`.
The mapping was correct; GitHub was down.

**2. `$?` read after `fi`** (`Plan 00074/prototype.bash`, fixed):

```bash
if out="$(grep -ai "$pattern" "$file")"; then ... fi
rc=$?      # ALWAYS 0 — the status of the `if` statement, not of its condition
```

Every legitimate "no match" was reported as `grep failed (exit 0)`.

Both were caught by accident. That is the problem this plan exists to fix.

## Goals

- Find every live instance of the class in repo-owned code, with evidence.
- Add a **gate that fails** — not an advisory — covering the mechanically
  detectable shapes.
- Close the gap that today's protection has: the hooks daemon's
  `error_hiding_blocker` fires at *write* time for an *agent*. It does nothing
  about code already in the tree, and nothing about a human with an editor.

## Non-Goals

- Not a general bash-quality crusade. One defect class, done properly.
- Not rewriting working code for style. A finding needs a concrete answer to
  *"what does this believe when the command fails?"* or it is not a finding.
- Not weakening any existing gate to make a cleanup pass smaller.

## Tasks

### Phase 1: Sweep 🔄

- [ ] 🔄 **Task 1.1**: `files/var/local/claude-yolo/` — the ccy wrapper and libs
- [ ] 🔄 **Task 1.2**: `files/home/.local/bin/` and other deployed scripts
- [ ] 🔄 **Task 1.3**: `scripts/`, `helpers/`, `playbooks/`, plan-local scripts —
  including **QA gates that cannot fail**, which is the highest-value target: a
  gate that reports PASS on unchecked code is the same defect class applied to
  the safety net itself
- [ ] ⬜ **Task 1.4**: Triage all findings — BLOCKING / SERIOUS / MINOR, with the
  false positives explicitly dismissed rather than silently dropped

### Phase 2: Gate

- [ ] 🔄 **Task 2.1**: Establish what today's tooling actually catches, with
  evidence, and the exact gap
- [ ] ⬜ **Task 2.2**: Real shellcheck numbers per severity and per SC code — the
  decision input for whether the gate can be raised today or needs a cleanup
  pass first
- [ ] ⬜ **Task 2.3**: Implement the gate. Semgrep rules in
  `.semgrep/bash-conventions.yml` and/or a raised shellcheck bar in
  `qa-bash.bash`, wired into `qa-all.bash` so it fails a build
- [ ] ⬜ **Task 2.4**: Prove the gate catches BOTH incidents — check the exact
  pre-fix code in as a fixture and confirm the gate rejects it. A gate that does
  not catch the bug that motivated it is theatre

### Phase 3: Fix and close

- [ ] ⬜ **Task 3.1**: Fix the BLOCKING and SERIOUS findings
- [ ] ⬜ **Task 3.2**: Record the class in `CLAUDE/AgentNotes.md` — it is now a
  known repo gotcha, not a one-off
- [ ] ⬜ **Task 3.3**: `./scripts/qa-all.bash`, then the `qa-reviewer` agent

## Technical Decisions

### Decision 1: The gate must fail a build, not advise

**Context**: The repo already has advisory signal that did not prevent either
incident.
**Decision**: whatever ships must be wired into `qa-all.bash` as a hard failure.
An advisory that everyone learns to scroll past is worse than nothing, because it
creates the impression of coverage.
**Date**: 2026-08-17

## Success Criteria

- [ ] Both incident code samples are rejected by the new gate, proven with a
  fixture
- [ ] Every BLOCKING finding fixed or explicitly accepted with a recorded reason
- [ ] `./scripts/qa-all.bash` fails on a reintroduction of the class
- [ ] No existing gate weakened to accommodate the new one

## Risks & Mitigations

| Risk                                                             | Impact | Probability | Mitigation                                                                                     |
| ---------------------------------------------------------------- | ------ | ----------- | ---------------------------------------------------------------------------------------------- |
| The precise shape is not statically detectable without noise     | H      | M           | Gate the mechanical subset hard; leave judgement cases to `qa-reviewer` and say which is which |
| Tightening shellcheck surfaces hundreds of pre-existing findings | M      | M           | Get real counts before deciding; stage the raise per-code if needed                            |
| A noisy gate gets disabled by whoever it annoys                  | H      | M           | Prefer few high-confidence rules over broad coverage                                           |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00075-Journal-YY-MM-DD.md. -->

- Motivating fixes already shipped: CCY 3.36.0 (`b770441`), Plan 00074
  `prototype.bash`
