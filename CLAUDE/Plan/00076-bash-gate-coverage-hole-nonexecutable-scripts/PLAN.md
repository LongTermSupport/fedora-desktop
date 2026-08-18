# Plan 00076: The bash gates cannot see 27 of the repo's scripts

**Status**: In Progress
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

- [x] ✅ **Task 1.1**: Add shebang-based discovery to `qa-bash.bash`, independent
  of the execute bit, so all 27 enter `bash -n` + shellcheck
- [x] ✅ **Task 1.2**: Add a **coverage assertion** — enumerate tracked files
  whose first line is a shell shebang and fail if any is absent from the
  discovered set. Zero-file detection already exists; this is the missing
  "discovered *fewer than exist*" case
- [x] ✅ **Task 1.3**: Prove it fails — add a mode-644 extensionless script with a
  known defect, confirm it is discovered, then restore the old `-executable`
  discovery in a copy and confirm `rc=2` naming all 27

### Phase 2: Make semgrep's coverage mode-independent too

Semgrep will not read a shebang without the owner execute bit, so it cannot see
these files at all. Task 2.1 ruled out the execute bit as the answer — see the
revised Decision 1 — so the gate scans a mirror instead.

- [x] ✅ **Task 2.1**: Confirm, per file, what mode the owning play deploys —
  derived from the playbooks, not assumed. **25 deploy `0755`; `colours` and
  `ps1-prompt` deploy `0644` because they are sourced libraries.** That two-file
  exception is what invalidates the `chmod` approach
- [x] ✅ **Task 2.2**: Scan a temp mirror (repo-relative path + `.bash` suffix)
  instead of changing any file mode; map findings and `.paths.scanned` back to
  real repo paths
- [x] ✅ **Task 2.3**: Verify semgrep now scans them — `.paths.scanned` is
  asserted to contain every discovered file, with a negative control (a file
  over `--max-target-bytes`) proving the assertion fires

### Phase 3: Clear the findings

- [x] ✅ **Task 3.1**: SC2155 ×19 — each one triaged, not blanket-rewritten
- [x] ✅ **Task 3.2**: The remaining 15 (SC1090 ×6, SC2034 ×3, SC2054 ×3,
  SC2120 ×2, SC1007 ×1)
- [x] ✅ **Task 3.3**: The semgrep findings the mirror exposed — **17**, not the
  28 the hand-applied census predicted. One was live: the ccy daily update check
  reported "up to date ()" against a version nothing had read (→ CCY 3.38.0)
- [x] ✅ **Task 3.4**: `./scripts/qa-all.bash` green with the widened discovery

### Phase 4: The second hole, found by the first — files semgrep cannot parse

`.paths.scanned` is not proof of analysis. Semgrep lists a file it could not
**parse** as scanned, returns zero findings, and exits 0; the reason appears only
in `.errors[]`, which the gate ignored. `ftp-camera` (2,475 lines) was in exactly
that state — and so were `rclone-tail` and `rclone-cache-status`, which the
**old** gate scanned too. All three pass `bash -n`.

- [x] ✅ **Task 4.1**: Gate on whole-file `Syntax error` (0% analysed), report
  `PartialParsing` with the affected files (the rest of the file *was* analysed).
  Both counts printed on every run, pass or fail
- [x] ✅ **Task 4.2**: Make the three exceptions explicit, named and
  **self-expiring** — `SEMGREP_CANNOT_PARSE` in `qa-patterns.bash` fails if an
  unlisted file cannot be parsed, *and* fails if a listed file starts parsing
- [x] ✅ **Task 4.3a**: `ftp-camera` — cause found by leave-one-out over complete
  top-level chunks (truncation bisect is invalid; a prefix cut mid-function is
  unparseable in its own right). A **one-line `case … esac` nested inside a
  `while` inside a command substitution**; the same `case` across lines parses.
  Fixed, file removed from the exception list, and the first finding it then
  reported was a real defect at line 704
- [ ] ⬜ **Task 4.3b**: `rclone-tail` and `rclone-cache-status` — cause **not**
  established. Each reduces to two segments either of which fixes the file, so it
  is an interaction. The shared embedded-Python heredoc inside an `if` condition
  was the obvious suspect and **parses in isolation** — dead end, not a cause
- [ ] ⬜ **Task 4.4**: The 11 `PartialParsing` files. The recurring construct is
  `${var:-(text)}` — parentheses inside a default value — which is my own idiom
  from Plan 00075's error-reporting fixes; also extglob and some `$(( ))`

### Phase 5: Review

- [ ] ⬜ **Task 5.1**: The `qa-reviewer` agent over the plan's full diff

## Dependencies

- Follows Plan 00075 (the gate this plan found the hole in). 00075's Task 4.1
  raised the shellcheck bar; this plan makes that raise apply to the whole repo.

## Technical Decisions

### Decision 1: Make both gates mode-independent — change no file modes (REVISED)

**Context**: Two independent mechanisms exclude these files: `qa-bash.bash`'s
`-executable` discovery branch, and semgrep's refusal to read a shebang without
the owner execute bit.
**Options**: (a) `chmod +x` the files — restores semgrep, but leaves coverage
dependent on a mode bit any future commit can drop. (b) Shebang discovery only —
fixes `qa-bash.bash`, leaves semgrep blind. (c) Shebang discovery **plus** a temp
mirror for semgrep, with a coverage assertion on each gate.
**Decision**: (c). The original decision was "(a) and (b) together", on the
premise that the repo modes were simply wrong. **Task 2.1 disproved the premise**:
`/var/local/colours` and `/var/local/ps1-prompt` are deployed `0644` on purpose —
they are sourced libraries — so `chmod +x` could never cover them, nor any future
sourced library. A mechanism that cannot cover its own category is not the
mechanism. The mirror makes coverage depend on what a file is, which is Goal 1
stated exactly.
**Date**: 2026-08-18

## Success Criteria

- [x] Every tracked shell script is in the discovered set, asserted by the gate
- [x] `qa-all.bash` fails if a shell script is added that the discovery misses
- [x] `.paths.scanned` from semgrep includes the ccy launcher
- [x] 0 gating (error/warning) shellcheck findings repo-wide
- [x] The reported file count matches the real one — both gates now say 153
- [x] No file is reported as passing that the analyser did not read — parse
      failures are gating, and the exception list cannot rot
- [ ] `qa-reviewer` agent run over the plan's full diff

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
- `86c4238` — the census: the bash gates cannot see 27 of the repo's scripts
- `644f7a7`, `df1deba`, `bf9b28f`, `86abbe0` — shellcheck 34 → 0 and the
  `|| true` / capture classes cleared in the hidden set, ahead of the widening
- Discovery widened, shared between both gates, and coverage asserted on each;
  both now report 153 files where they reported 125
