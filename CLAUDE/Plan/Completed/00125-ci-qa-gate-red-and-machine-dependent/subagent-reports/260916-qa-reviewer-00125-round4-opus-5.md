# QA Review — Plan 00125 round 4, `ced20d08`

> **Provenance.** The reviewer could not write this file: the `Write` tool was disabled for
> its session and its own rules forbid Bash mutations, so it returned a **compressed**
> report in its completion message and its long-form version was never persisted. What
> follows is that message, transcribed verbatim in substance by the coordinator so the next
> round has something to reference. It is not the reviewer's own file, and the detail it
> says was dropped is genuinely gone.

**Verdict**: FIX-BEFORE-MERGE

Tree **clean**; round-3 Blocking 1 not repeated. HEAD has since moved past the review
target (`3138ebc2`, `355527c9`).

**CI already ran `ced20d08`** — run `35045164017`: sole failure `✗ docs: 8 findings`; all 37
stages ran; `✓ helper-summary-readers: passed: 16` green in CI first try; `helper-tests: 2 skipped` in CI against `1` local. The signal works.

**All six round-3 items are real fixes — none theatre, none narrowed.**

## Answers to the five questions put to the reviewer

1. **helper-summary lib/test — genuinely shared.** `test:38` and `qa-all.bash:22` source the
   same path. Hard-failing an unreadable capture is right and cannot redden a healthy run
   (the capture is only taken after exit 0; unittest always emits `^OK`/`^FAILED`, including
   the zero-test path). Missed shapes are unreachable. **But `helper_test_summary` (`lib:19`)
   still carries the round-3 defect** — unscoped `grep -oE 'Ran [0-9]+ tests?'`, and `-o`
   returns all matches, so a decoy makes the stage line **two lines** which `verdicts.py`
   half-drops. Untested, one function away, same docstring.
2. **`SYMBOL_BEARING` — no real line wrongly dropped or counted.** Delta 0 on the four
   captures *and* the real CI log (`38 of 39 … 0 unrecognised`); the `-1` case is gone (90
   brute-forced shapes, zero negatives). Residual: a **two-token** timestamp would vanish
   from *both* sides (a silent 100%), so the docstring's "whatever shape it has taken"
   overstates it. Over-matches exist (`Legend: ✓ …`, tab-indent) but no qa gate emits them.
3. **`link_check` exclusion is safe today** — set diff: exactly 1 line, 1 name
   (`qa-helper-summary.bash`), nothing added. A compound `source … && bash …gate.bash` line
   would be hidden; mitigated because the inventory is bidirectional, so a hidden
   *documented* gate fires loudly.
4. **Counts are off by one, three times.** `helper-tests` now owns **two** `exit 1` sites
   (`:160`, `:175`), so sites were counted rather than gates. Confirmed three ways (CI log,
   QA.md's 29 table rows, 29 `✓` prints): **29 hard gates not 30**; **27** behind the drift
   abort not 28; **26** behind helper-tests not 27. `37` total stages is *correct*, but
   `7+30` is not its decomposition — it is 7 accumulating gates → 8 named lines, plus 29
   hard. `25` at `29ceee97` is **right**. Also `:602` there is wrong (`:597`),
   `qa-deployed-drift.bash:194` → `:192`, `PLAN.md:208` "914 files" → 918.
5. **`podfreeze` comment is now true.** No `break` in the loop; `while [ "$#" -gt 0 ]`
   guarantees `$#`=0 at `:888`. The `lxcfreeze` hazard is genuinely live. Minor: "shifts
   every branch" — two branches exit instead.

## Also found

- `CLAUDE/QA.md:19`/`:41` still say thirty-five/twenty-eight (now 36/29) — drift this commit
  introduced; `link_check` checks names, not counts.
- `test-qa-helper-summary.bash:61` says the diagnostic "is captured rather than discarded" —
  `2>/dev/null` discards it.
- **Task 2.1 is not single-option.** `link_check.py:214-223` already excludes
  `.claude/hooks-daemon|ccy|skills|agents`; excluding the 8 daemon-*generated* rule files by
  their version marker (the 7 repo-authored ones stay checked) is unconditional,
  machine-independent and needs no network — materially different from (b).

## Clean

Fail-fast, stderr hygiene, modes and placement, version bumps (none owed), public-repo
safety, the Plan Commit Rule, append-only journal.

## Mechanical gates

`qa-all` PASS (918 files) · `plan-qa` PASS for 00125 (2 advisories, pre-existing) ·
`qa-helper-tests` PASS standalone · syntax-check not triggered (82 playbooks OK repo-wide) ·
extension gates not triggered.

## Task 5.3

Not dischargeable — the five should-fixes above. The host still needs **both** freeze plays
or drift aborts at `:137`, **27** gates short.
