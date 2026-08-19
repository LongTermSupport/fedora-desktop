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

Headline: **34 gating shellcheck findings**, of which **SC2155 ×19** — this
repo's own defect class, in files Plan 00075's raised bar structurally could not
open. The bar was raised over a keyhole. Adding semgrep's blind spot took the
census to **~92**, and Phase 3 then found the semgrep half was 17 rather than the
28 predicted.

Full per-code and per-rule tables, the worst offenders, and both corrections are
in `JOURNAL/00076-Journal-26-08-19.md` ("Relocated from PLAN.md") — moved there
when Phase 6 pushed this document past its size advisory. All 27 files pass
`bash -n`, so nothing was ever broken at parse level.

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
  reported was a real defect at line 704.
  **Corrected in review (Task 5.2)**: this recovered less than it claimed. The
  file went from a whole-file `Syntax error` to `PartialParsing` — but the
  surviving range is lines **585-2486**, so **1,902 of its 2,486 lines are still
  unanalysed**. It moved from 0% to 23% covered, not to covered. The old report
  could not show this, because it printed a file list with no sizes

- [x] ✅ **Task 4.3c**: `ftp-camera`'s 1,902-line gap — **it costs nothing, and
  the "per rule" framing was wrong**. Semgrep parses a file only when a rule's
  regex has ALREADY matched in it: across ten measured (file, rule) cells,
  matches 0 ⇒ no parse ⇒ no error, matches ≥1 ⇒ parse ⇒ error. The rules that
  looked like they coped had never been asked. And a probe regex returned **293
  findings against 293 raw occurrences**, 262 of them inside the 585-2486 range —
  because every rule here is `pattern-regex` and the regex engine reads raw text.
  The gap becomes real only if an AST `pattern:` rule is ever added

- [ ] ⬜ **Task 4.5**: **Per-rule coverage is invisible, and it is not uniform.**
  The gate prints `✓ patterns: 157 files OK`, which is the **union** and reads as
  "every rule ran on 157 files". Only one did:

  | rule                           | files it saw | blind to |
  | ------------------------------ | ------------ | -------- |
  | `bash-error-hiding-or-true`    | 157          | 0        |
  | `bash-error-hiding-pipe-echo`  | 123          | 34       |
  | `bash-status-after-block`      | 123          | 34       |
  | `bash-capture-discards-status` | **90**       | **67**   |
  | `bash-test-discards-status`    | 90           | 67       |

  `bash-capture-discards-status` — the rule for the exact class Plan 00075 exists
  to catch — is blind to 67 of 157 files. The narrowing is deliberate and recorded
  in the rule; it is the *pass line* that does not say so.

  **AWAITING A DECISION.** Options measured, not estimated:

  - **A — one scan per rule.** 18.4s vs 4.3s for the combined scan (+14s on a
    gate that currently takes 17.7s). Reports what actually happened.
  - **B — parse `paths.include` and match the globs in the gate.** No extra scan.
    The stated blocker for this was "needs a YAML parser = a host/CI dependency
    decision" — **that blocker does not hold**: `qa-ansible-syntax.bash` already
    requires ansible, CI installs it explicitly, and ansible ships PyYAML. But B
    *models* semgrep's targeting instead of measuring it, and a glob matcher that
    drifts from semgrep's semantics would report coverage the scan does not have
    — which is the defect this plan is about.
  - **C — `semgrep --time`'s `match_times`.** **Ruled out by measurement.** A `0`
    means both "not targeted" and "targeted, matched nothing": `capture-discards`
    reads 0 on a `helpers/` file it never saw *and* on a `files/` file it did.

- [x] ✅ **Task 4.3b**: `rclone-tail` and `rclone-cache-status` — cause **found**,
  and it cost 4 hidden findings. The earlier attempts failed because the harness
  was unreliable: they bisected while running rules whose regexes matched
  nothing, so the error appeared and vanished for reasons unrelated to the edit.
  Re-run with a probe rule that always matches, leave-one-out named one function
  in each file, and both reduce to the same construct — a heredoc fed **directly
  to an `if` condition** with `then` on the line after the terminator. Valid bash,
  unparseable by tree-sitter-bash. Feeding it to a command substitution instead
  fixes both, and is better bash anyway. `SEMGREP_CANNOT_PARSE` is now **empty**

- [x] ✅ **Task 4.4**: The `PartialParsing` files (now 14) — **no cause needed**:
  measured to cost nothing while the ruleset is all `pattern-regex`. The gate
  reports them as "outside the parse tree", not "not analysed", and names the
  condition under which they would start to matter

### Phase 5: Review

- [x] ✅ **Task 5.1**: Review of the plan's full diff. Three attempts to delegate
  it died on API 529s, so it was done inline instead — the gate machinery read
  end to end against the defeat-vectors list, and the shipped-script changes
  against their call sites
- [x] ✅ **Task 5.2**: Findings fixed. Two were real, and both are this plan's own
  defect class committed inside the fix for it:
  - **Mirror path collision.** `dir/foo` and `dir/foo.bash` both mirror to
    `dir/foo.bash`; the second `cp` overwrites the first and the merged map keeps
    one value for the key. The lost file is then neither scanned **nor** missed
    by the assertion, which reads the map's values — it ceases to exist and the
    gate reports a pass over it. None exists today; it is now a hard failure
    naming both files, with a negative control confirming `rc=2`
  - **"Analysed EXCEPT for some ranges" hid its own magnitude.** A bare file list
    makes a 3-line gap and a 1,902-line gap look identical. The report now prints
    the union of skipped lines against the file's length — which is how the
    Task 4.3a overstatement above was caught
- [x] ✅ **Task 5.3**: Defeat-vectors checked and recorded rather than assumed —
  `#!` with a space (none present), CRLF shebangs (none), tracked symlinks (one,
  `scripts/vault` → a vendored directory, correctly skipped by the `-f` test),
  non-bash shebangs (none). The first three would be invisible today because
  discovery and the assertion share one predicate; see the journal

### Phase 6: Land the rewritten helpers on the host

The Task 4.3b fix changed how `query_stats` / `query_cache` emit their result, so
it is a behaviour change in two production scripts, not a parser appeasement.

- [ ] ⬜ **Task 6.1**: `deploy.bash` — runs `play-rclone.yml`. Until it does,
  `qa-deployed-drift.bash` fails on the host by design. Carries Plan 00072's
  in-flight guard: the play restarts the mounts, which can lose
  cached-but-not-yet-uploaded data. **HOST action**
- [ ] ⬜ **Task 6.2**: `acceptance.bash` — exercises the **deployed** scripts,
  not a copy of the construct. Fails on `parse failed` (the captured Python
  output not reaching the caller) and reports **INCONCLUSIVE**, not PASS, when
  the RC does not answer and the rewritten success path was never reached.
  **HOST action**

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
- [x] The reported file count matches the real one — both gates now say 154
- [x] No file is reported as passing that the analyser did not read — parse
  failures are gating, and the exception list cannot rot
- [x] Two files cannot claim one mirror path, so no discovered script can drop
  out of both the scan and the assertion
- [x] The partial-parse report states its own magnitude — skipped lines against
  file length — so a 1,902-line gap cannot read like a 3-line one
- [x] Full-diff review done and its findings fixed (inline; delegation died on
  API 529s three times)
- [x] `SEMGREP_CANNOT_PARSE` is empty — no file is exempt from the ruleset, and
  the 4 findings the two exemptions were hiding are fixed
- [x] The partial-parse report states what the gap COSTS, not only how big it is
  — measured at zero for an all-`pattern-regex` ruleset, with the condition that
  would change that named on the line

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
- `SEMGREP_CANNOT_PARSE` emptied: one construct (heredoc fed straight to an `if`
  condition) explained both entries, and clearing it surfaced 4 real `|| echo`
  violations the blackout had been hiding
