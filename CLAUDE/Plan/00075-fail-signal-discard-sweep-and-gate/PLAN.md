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

### Phase 1: Sweep ✅

- [x] ✅ **Task 1.1**: `files/var/local/claude-yolo/` — the ccy wrapper and libs
- [x] ✅ **Task 1.2**: `files/home/.local/bin/` and other deployed scripts
- [x] ✅ **Task 1.3**: `scripts/`, `helpers/`, `playbooks/`, plan-local scripts —
  including **QA gates that cannot fail**, which is the highest-value target: a
  gate that reports PASS on unchecked code is the same defect class applied to
  the safety net itself
- [x] ✅ **Task 1.4**: Triage all findings — BLOCKING / SERIOUS / MINOR, with the
  false positives explicitly dismissed rather than silently dropped

### Phase 2: Gate ✅

- [x] ✅ **Task 2.1**: Establish what today's tooling actually catches, with
  evidence, and the exact gap. Found `bash-error-hiding-pipe-echo` had **never
  matched anything real** — `pattern: $CMD || echo $MSG` binds a bare command
  word, so all 16 live sites were invisible and the zero was read as clean
- [x] ✅ **Task 2.2**: Real shellcheck numbers per severity and per SC code —
  125 files: 0 error / 26 warning / 79 info / 0 style; SC2155 ×16 is this class
- [x] ✅ **Task 2.3**: Implement the gate — `bash-status-after-block` and
  `bash-capture-discards-status` added, `bash-error-hiding-pipe-echo` repaired,
  shellcheck made **required** in `qa-bash.bash` (absent ⇒ exit 2, no free pass)
- [x] ✅ **Task 2.4**: Prove the gate catches BOTH incidents — the exact pre-fix
  code is checked in as annotated fixtures in `.semgrep/bash-conventions.bash`,
  and `qa-patterns.bash` runs `semgrep --test` before scanning, so a rule that
  stops matching is a hard failure rather than a quiet zero

### Phase 3: Fix and close 🔄

- [x] ✅ **Task 3.1**: Fix the BLOCKING and SERIOUS findings
- [x] ✅ **Task 3.2**: Record the class in `CLAUDE/AgentNotes.md` — it is now a
  known repo gotcha, not a one-off
- [x] ✅ **Task 3.3**: `./scripts/qa-all.bash` (green, 444 files), then the
  review. The `qa-reviewer` agent signalled idle twice without producing a
  report, so the review was carried out directly — the requirement is that the
  review happens, not that a particular agent performs it. It found four more
  live instances of the class, all shipped as CCY 3.37.0

### Phase 4: Staged widening — owner's call, deliberately not in the gate commit

Both are mechanical but bulky, and mixing a refactor into the commit that
introduces a gate makes the gate unreviewable. Neither is a blocker.

- [x] ✅ **Task 4.1**: Raise the `qa-bash.bash` shellcheck bar from `error` to
  `warning`. 18 findings fixed (SC2155 ×12, SC2034 ×5, SC2088 ×1), gate raised,
  and the raise **proved to fail** by injecting a fresh SC2155 and confirming
  `rc=1` naming the file and line. `info`/`style` stay advisory on purpose —
  79 findings dominated by SC2016/SC2012, where gating trades signal for noise.
  Two were live bugs, not style: `docker-in-lxc` built its container name from a
  `git remote` capture that silently became empty outside a repo, and provisioned
  against an empty `lxc-info` IP

- [x] ✅ **Task 4.2**: Widen `bash-capture-discards-status` to `fedora-install/**`
  — the highest-stakes tree in the repo (it repartitions a disk and handles
  LUKS). 8 sites triaged as correctly handled and annotated; **1 real bug fixed**:
  `prompt_luks_passphrase()` skipped BOTH passphrase checks when
  `cryptsetup status` could not report, returning it as though verified.
  Also **repaired the rule itself** — its `$` anchored straight after the closing
  paren, so any trailing comment evaded it and the `FAIL-FAST-OK` exclusion was
  decorative. Pinned with a fixture case

- [ ] ⬜ **Task 4.3**: Widen to `scripts/**` — 15 findings, concentrated in two
  status-report scripts. Deliberately staged: probing and printing "not
  available" is their design, but not all are benign — `nvidia-status.bash`
  reports *"no MOK keys enrolled"* when `mokutil` is simply absent, sending
  someone to re-enrol a key they already have. Each site needs a "could not
  check" state distinct from "absent", which is per-site design rather than a
  mechanical edit

- [ ] ⬜ **Task 4.4**: Report upstream to the hooks-daemon: `lint_on_edit` honours
  neither a per-handler `options.exclude_paths` nor the project-wide
  `daemon.exclude_paths` (both tried, neither works), so a deliberately-invalid
  test fixture cannot be exempted and prints a wall of findings on every edit.
  Also `.claude/init.sh:310` — `resolve_venv_python` failing returns 0, a live
  instance of this plan's own class. Neither may be patched locally: both are
  overwritten on daemon upgrade

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
