# Plan 00060: stderr hygiene coding standard

**Status**: Complete
**Created**: 2026-07-15
**Completed**: 2026-07-15
**Owner**: joseph
**Priority**: Medium

## Overview

A generated `gh-<alias>()` wrapper in `play-github-cli-multi.yml` echoed its
"Switching to <user>..." status line to **stdout**, so any consumer capturing
`$(gh-<alias> … --json)` got human chatter mixed into the JSON and `jq` broke —
but only when that account was not the box's default. The one-word fix is to
redirect the status line to stderr (`>&2`), keeping the wrapper's stdout pure.

That bug is an instance of a general class: **informational / diagnostic /
progress / prompt output emitted on stdout by a function or script whose stdout
is (or plausibly is) captured as a value.** This plan (a) applies the immediate
gh fix, (b) audits the repo for other members of the same class, and (c) makes
"echo diagnostics to stderr when appropriate" a first-class, documented coding
standard so the pattern is caught in review going forward.

## Goals

- Fix the `gh-<alias>` wrapper stdout pollution (`>&2` on the status line).
- Audit the repo (bash executables, generated bash includes, playbook shell
  blocks, `scripts/`, `helpers/`) for other stdout-pollution / "should be
  stderr" bugs; classify HIGH / MEDIUM / NOT and fix the real ones.
- Add a documented coding standard: diagnostics → stderr; stdout is reserved for
  a command/function's real captured output. Wire it into the project docs so it
  is discoverable and enforced in review.

## Non-Goals

- Rewriting help/status/report commands whose entire purpose is to print for
  humans (their stdout is not captured — not bugs).
- Adding an automated QA/semgrep gate for this in the same plan (note it as a
  possible follow-up; do not build it here unless it is cheap and reliable).

## Context & Background

- CCY container: edit + commit only; deploy on HOST. The gh aliases are generated
  by `playbooks/imports/play-github-cli-multi.yml` into
  `~/.bashrc-includes/gh-aliases.inc.bash`.
- The user already hardened their own consumer (`backfill.bash`) and documented
  the requirement as "R13" downstream; this plan fixes the source and generalises
  the rule.
- Interactive-script UX rules already live in `CLAUDE/InteractiveScripts.md`
  (rule 08 already says "prompts/diagnostics → stderr; machine output → stdout").
  The new standard should reference/extend that, not duplicate it.

## Tasks

### Phase 1: Immediate gh fix

- [x] ✅ **Task 1.1**: Redirect the `gh-<alias>` wrapper "Switching to …" status
  line to stderr in `play-github-cli-multi.yml`
- [x] ✅ **Task 1.2**: Run QA (`./scripts/qa-all.bash`) and fix findings
- [x] ✅ **Task 1.3**: Commit the gh fix (Plan 00060), reference the plan — `f2c7f93`, pushed

### Phase 2: Repo-wide stderr-hygiene audit

- [x] ✅ **Task 2.1**: Dispatch audit agent over bash executables, generated
  includes, playbook shell blocks, `scripts/`, `helpers/`
- [x] ✅ **Task 2.2**: Triage results — confirm HIGH/MEDIUM findings, reject
  false positives (help/status/report commands). **Result: 0 real bugs.** The
  repo is already disciplined — every value-returning shell/Python function
  routes chatter to stderr. The wrapper fix in Phase 1 was the only real
  finding. `gh-switch()` (line 712) prints `Switching to …` on stdout but emits
  no captured value (nothing does `x=$(gh-switch …)`), so the StderrHygiene
  carve-out applies — redirecting it would *contradict* the standard, so it is
  deliberately left as-is.
- [x] ✅ **Task 2.3**: Apply fixes for confirmed findings — none needed (0
  confirmed findings); no code change in this phase

### Phase 3: Coding standard

- [x] ✅ **Task 3.1**: Author the "stderr hygiene" standard (diagnostics → stderr,
  stdout = captured output), cross-referencing `CLAUDE/InteractiveScripts.md`
  rule 08 — `CLAUDE/StderrHygiene.md`
- [x] ✅ **Task 3.2**: Wire it into the CLAUDE docs index + a first-class Critical
  Rules subsection; back-link from InteractiveScripts.md rule 08
- [x] ✅ **Task 3.3**: Commit the standard (Plan 00060) — `62ac5c5`, pushed

## Dependencies

- None. Builds on the hooks-daemon upgrade already committed + pushed this
  session.

## Success Criteria

- [x] `gh-<alias>` wrapper stdout is pure (status line on stderr) — `f2c7f93`
- [x] Audit complete; every confirmed finding fixed or explicitly deferred with
  reason — 0 confirmed findings; `gh-switch` deferred by the carve-out
- [x] Coding standard documented and indexed — `62ac5c5`
- [x] QA passes (`./scripts/qa-all.bash`) — exit 0

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00060-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Recovery cron: 73605707 (session-only failsafe)
- `f2c7f93` — Phase 1: gh-<alias> wrapper status line → stderr (the real bug); QA green
- `62ac5c5` — Phase 3: `CLAUDE/StderrHygiene.md` standard + CLAUDE.md first-class rule + index row + InteractiveScripts.md back-link
- Phase 2 audit (background agent): 0 real bugs — repo already disciplined; `gh-switch` correctly left on stdout per the carve-out
