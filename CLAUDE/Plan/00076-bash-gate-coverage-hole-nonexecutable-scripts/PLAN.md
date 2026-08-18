# Plan 00076: The bash gates cannot see 27 of the repo's scripts

**Status**: Not Started
**Created**: 2026-08-18
**Owner**: joseph
**Priority**: High

## Overview

`./scripts/qa-all.bash` reports `✓ bash: 125 files OK`. There are **152** bash
scripts in this repo. The 27 it does not mention are not passing — they are
**never examined**, by `bash -n`, by shellcheck, or by semgrep.

The cause is that every gate identifies bash by **filename extension or file
mode**, and these 27 files have neither a `.sh`/`.bash` extension nor an execute
bit:

- `scripts/qa-bash.bash` discovers `*.sh`/`*.bash` by name, **or** `-type f -executable` plus a shebang sniff. A mode-644 extensionless script is in
  neither branch.
- semgrep is worse, and for a reason no one would guess: for a file with no
  recognised extension it detects the language by reading the shebang, and
  `target_manager.py:1024` returns `None` unless the file has
  `S_IRUSR | S_IXUSR`. **No execute bit, no shebang read, no language, no scan.**

This is Plan 00075's defect class applied to the safety net: a gate that reports
a clean pass over files it never opened. `qa-bash.bash` already guards against
discovering **zero** files ("A discovery that finds nothing is a BROKEN GATE, not
a clean repo") — but nothing guards against discovering *most* of them.

## The evidence

Measured, not estimated (`shellcheck -S style` over the 27 files):

| Severity            | Count  |
| ------------------- | ------ |
| **error + warning** | **34** |
| info                | 62     |
| style               | 2      |

The gating 34, by code — note the top entry:

| Code       | Count  | What it is                                          |
| ---------- | ------ | --------------------------------------------------- |
| **SC2155** | **19** | `local x=$(cmd)` — **this repo's own defect class** |
| SC1090     | 6      | non-constant source                                 |
| SC2034     | 3      | unused variable                                     |
| SC2054     | 3      | missing comma in array                              |
| SC2120     | 2      | function references unpassed arguments              |
| SC1007     | 1      | remove space after `=` or quote                     |

**SC2155 ×19 is the finding.** Plan 00075 Task 4.1 raised the shellcheck bar from
`error` to `warning` *specifically because SC2155 is the discarded-failure-signal
class*, and fixed the 12 instances it could see. Nineteen more were sitting in
files the raised gate structurally cannot open. The bar was raised over a
keyhole.

Worst offenders: `files/home/.local/bin/nord` (16), then
`files/var/local/claude-yolo/claude-yolo` (5) — the ccy launcher, 140 KB, the
largest script in the repo, run on every session on every machine, and **the file
where the incident that started Plan 00075 happened**.

All 27 pass `bash -n`, so nothing is currently broken at parse level.

### The census above counted only shellcheck — the real total is ~92

`qa-patterns.bash` (semgrep) is blinded by the *same* execute-bit rule, so its
rules were never applied to these files either. Re-measured by extracting each
rule's own `pattern-regex` and `pattern-not-regex` from
`.semgrep/bash-conventions.yml` and applying them directly, so the count comes
from the shipped rules rather than a hand-written approximation:

| Rule                           | Hidden findings |
| ------------------------------ | --------------- |
| `bash-error-hiding-pipe-echo`  | 22              |
| `bash-error-hiding-or-true`    | 16              |
| `bash-capture-discards-status` | 14              |
| `bash-test-discards-status`    | 6               |
| **semgrep total**              | **58**          |

**58 semgrep + 34 shellcheck ≈ 92**, not 34. `bash-error-hiding-or-true` matters
most of those: it has **no `FAIL-FAST-OK` escape by design**, because it mirrors
the write-time blocker — every one of its 16 needs rewriting into an explicit
reporting form, not annotating.

Recording the correction rather than quietly restating the number: the original
34 was measured with one tool and presented as the size of the job, which is the
same shape of error this plan is about.

## Goals

- Make gate coverage independent of file mode and filename. A script is bash
  because of what it *is*, not because of a permission bit.
- Make under-coverage **fail loudly**, the way zero-coverage already does.
- Clear the 34 gating findings, SC2155 first.

## Non-Goals

- Not gating `info`/`style` — that decision was made in Plan 00075 Task 4.1 on
  measured grounds and is not reopened here.
- Not a rewrite of any of the 27 scripts. Fix the findings, nothing else.
- Not touching the `.j2` template that the census deliberately excludes: a Jinja
  template is not a shell script.

## Tasks

### Phase 1: Stop the gate reporting coverage it does not have

- [ ] ⬜ **Task 1.1**: Add shebang-based discovery to `qa-bash.bash`, independent
  of the execute bit, so all 27 enter `bash -n` + shellcheck
- [ ] ⬜ **Task 1.2**: Add a **coverage assertion** — enumerate tracked files
  whose first line is a shell shebang and fail if any is absent from the
  discovered set. Zero-file detection already exists; this is the missing
  "discovered *fewer than exist*" case
- [ ] ⬜ **Task 1.3**: Prove it fails — add a mode-644 extensionless script with a
  known defect, confirm `rc=1` naming it, then remove it

### Phase 2: Restore the file modes so semgrep can see them too

Semgrep's shebang detection cannot be worked around from the rule side; it needs
the execute bit. These files are **deployed** `0755` by their plays, so the repo
mode is simply wrong — this is mode hygiene, not a workaround.

- [ ] ⬜ **Task 2.1**: Confirm, per file, that the owning play deploys it as an
  executable before setting `+x` — derived from the playbooks, not assumed
- [ ] ⬜ **Task 2.2**: Set the execute bit and commit the mode change
- [ ] ⬜ **Task 2.3**: Verify semgrep now scans them (`.paths.scanned` must list
  each one — a finding count of zero proves nothing on its own)

### Phase 3: Clear the findings

- [ ] ⬜ **Task 3.1**: SC2155 ×19 — each one triaged, not blanket-rewritten. This
  is the class; a discarded status here is a live bug candidate
- [ ] ⬜ **Task 3.2**: The remaining 15 (SC1090 ×6, SC2034 ×3, SC2054 ×3,
  SC2120 ×2, SC1007 ×1)
- [ ] ⬜ **Task 3.3**: `./scripts/qa-all.bash` green with the widened discovery,
  then the `qa-reviewer` agent

## Dependencies

- Follows Plan 00075 (the gate this plan found the hole in). 00075's Task 4.1
  raised the shellcheck bar; this plan makes that raise apply to the whole repo.

## Technical Decisions

### Decision 1: Fix discovery *and* the file modes, not either alone

**Context**: Two independent mechanisms exclude these files.
**Options**: (a) `chmod +x` only — restores semgrep and the existing executable
branch, but leaves coverage silently dependent on a mode bit that any future
commit can drop. (b) Shebang discovery only — fixes `qa-bash.bash`, but semgrep
still cannot read a shebang without `+x`, so the pattern rules stay blind.
**Decision**: both. (b) makes coverage robust; (a) is required for semgrep and is
correct on its own terms, since these files deploy as executables.
**Date**: 2026-08-18

## Success Criteria

- [ ] Every tracked shell script is in the discovered set, asserted by the gate
- [ ] `qa-all.bash` fails if a shell script is added that the discovery misses
- [ ] `.paths.scanned` from semgrep includes the ccy launcher
- [ ] 0 gating (error/warning) shellcheck findings repo-wide
- [ ] The reported file count matches the real one

## Risks & Mitigations

| Risk                                                      | Impact | Probability | Mitigation                                                                        |
| --------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------- |
| 34 findings across 27 unfamiliar scripts introduces a bug | H      | M           | Triage per finding; SC2155 fixes are mechanical but each is read in context first |
| A mode change breaks a play that assumes `0644`           | M      | L           | Task 2.1 derives the expected mode from the playbooks before changing anything    |
| Widened discovery pulls in files that are not really bash | L      | M           | Discovery keys on a shell shebang, and `.j2` templates are excluded explicitly    |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00076-Journal-YY-MM-DD.md. -->

- Found while widening Plan 00075's semgrep rules: the launcher reported zero
  findings, and the zero turned out to mean "never opened"
