# QA Review — Plan 00125 round 3, commit `29ceee97`

**Verdict**: FIX-BEFORE-MERGE

Reviewed `29ceee97` against `subagent-reports/260916-qa-reviewer-00125-round2-opus-5.md`.
`29ceee97` is HEAD and level with `origin/F44`. **The tree is not clean**:
`scripts/qa-all.bash` carries an uncommitted change that revises round-2 nit 1 a second
time. Both states are reviewed below.

## Per round-2 item

| Item | Verdict |
| ---- | ------- |
| SF1 — `PLAN.md` "helper-tests agree exactly" | **Real**, closed in `a28dd697` (not `29ceee97`). `PLAN.md:213-228` now names the commit and CI run, lists three declared differences, and uses the criterion's own *"or the disagreement is declared"* branch; `CLAUDE/QA.md:106-122` carries the declaration. No theatre. |
| SF2 — `no-run-summary` refuses a tool abort | **Real and sound.** Drove `TOOL_ABORT` against all seven `exit 2` messages (`qa-all.bash:33,42,51,61,70,79,96`) — **7 MATCH, 0 MISS**. No under-match. Anchored, so `  ERROR: Missing required tools (indented)` does not match; `ERROR: no helper tests found` (`qa-helper-tests.bash:30`) correctly does not match, and it exits 1 so a `QA FAILED` line exists anyway. It cannot swallow a stage verdict — `^ERROR:` and `^[✓✗⚠] ` are mutually exclusive, measured `False` over both populations. Over-match needs a string that does not exist in the repo (grep: the two producers are `qa-all.bash` only). Control fixture present at `test_verdicts.py:281`. One nit below. |
| SF3 — the two SC2119 comments | **Half real. The `podfreeze` comment's stated mechanism is false** — see Blocking 2. `lxcfreeze` is correct. |
| Nit 1 — skip extraction under-matches | **Real fix, incomplete — and round 2's own prescription carried the third hole.** Being re-fixed uncommitted right now; see Blocking 1. |
| Nit 2 — coverage cannot see its own blind spot | **Real, and it works.** Measured: a CI line with a malformed timestamp now reads `1 of 3 … 1 unrecognised` instead of vanishing at `2 of 2`. Delta 0 holds on **four** captures (both triage pairs, `20260915-235821` and `20260916-004433`), so the claim is more robust than the docstring's "a real capture". **One over-correction** — Should fix 1. |
| Nit 3 — "last wins" premise removed not answered | **Real.** `verdicts.py:122-127`. Verified independently: on both sides of `20260916-004433` the only multi-line stage is `patterns`, `['⚠ 15 file(s) …', '✓ 260 files OK']`, same order. |
| Nit 4 — invented journal timestamps | **Real, accurate, and correctly recorded.** `17307240` authored `00:35:01` and added entries stamped 00:35/00:37/00:39/00:41 — confirmed. New entry at the file's end, diff is additions only, 01:03/01:04 after 01:02 keeps monotonicity, and `## 01:04 · thought · — —` matches the file's own convention (line 24 has the same shape) and `PlanJournalling.md:66` (`—` for no REF). Not over-corrected: it admits the entry's own stamp is also invented and says why. |

Nothing was silently dropped. Nothing is theatre.

## Blocking

### 1. An uncommitted change to a QA gate is sitting in the tree — untested, unjournalled, unpushed

`git status --short` → ` M scripts/qa-all.bash`. The committed `29ceee97` has:

```bash
if [[ "$helper_out" =~ skipped=([0-9]+) ]]; then
```

The working tree has `qa-all.bash:169-174`, scoping the match to unittest's result line via
`grep -E '^(OK|FAILED)( \(|$)'`.

The committed version **is** blind in the way round 3's brief suspected, and it was measured:

```
capture                                                           committed  worktree
"Ran 5 tests / OK"                                                    0          0
"Ran 5 tests / OK (skipped=1)"                                        1          1
"Ran 5 tests / OK (skipped=1, expected failures=1)"                   1          1
"Ran 5 tests / FAILED (failures=2, skipped=3)"                        3          3
"VMTEST-CHECKS-DONE … skipped=41 / Ran 5 tests / OK (skipped=2)"     41          2
```

`=~` takes the first match anywhere in the capture, so any earlier `skipped=<digits>` wins.
Reachability, measured rather than assumed:

- `qa-helper-tests.bash` runs one `python3 -m unittest` in non-verbose mode, so there is
  exactly one result line — multiple summary lines are not reachable, and `FAILED (…)` is
  unreachable because `qa-all.bash:150-154` exits first. Two of the three worries are fine
  in both versions.
- The third is live. Today's real capture is 32 lines and **line 2 is already a test leaking
  to the console**: `...container-watch: DBus emit skipped (CalledProcessError: no session
  bus?)`. It has no `=`, so the count is correct today. But the payload text exists in the
  repo: `tests/helpers/vmtest/test_transcript.py:26` (`skipped=41`) and `:42`
  (`VMTEST-CHECKS-DONE … skipped=0`). unittest does not buffer stdout, so one `print` of
  either fixture puts a wrong number on the line — and `skipped=0` would be silent in
  exactly the "reports identical over different populations" direction the line exists to
  stop.

So the working-tree edit is the right fix and the shape is sound (`if helper_result=$(…)` is
`set -e`-safe; the anchored `^OK` matches the real capture's line 32; GNU grep honours `$`
before `|`/`)`). It is not committed, not pushed, not journalled, and CLAUDE.md's
*"Always Push — GitHub Is the Backup"* applies. Commit it before anything else — and note
that once committed, the `01:03` journal entry and `29ceee97`'s commit message both describe
`[[ $helper_out =~ skipped=([0-9]+) ]]`, an implementation that will no longer exist.
Append-only means that needs a successor entry, not an edit.

### 2. `podfreeze:879`'s comment states a mechanism that does not hold at that call site

The comment claims `assert_on_host "$@"` *"would pass THIS TOOL's argv into the
container-marker slots, so `podfreeze somename` would test `-f somename`"*. At
`podfreeze:884` that is false. The arg loop at `podfreeze:813-866` is
`while [ "$#" -gt 0 ]` and every non-exiting branch `shift`s, so `$#` is 0 by the time
control reaches line 884. Measured:

```
podfreeze shape  (top-level, after the shift loop):     assert_on_host received 0 arg(s): []
lxcfreeze shape  (main "$@", parse_args shifts a copy): assert_on_host received 2 arg(s): [freeze somename]
```

`lxcfreeze:740` is inside `main()`, which is called `main "$@"` at `:780` and whose own
positionals survive `parse_args "$@"` — so **the `lxcfreeze` comment is exactly right and
the `podfreeze` one is a copy of it that does not apply.** Taking SC2119's advice at
`podfreeze:884` would be behaviourally identical, not a loosening.

The conclusion (keep it bare) is still correct, and `assert_on_host` does read `$1`/`$2`
(`freeze-common.bash:176-177`), so the attribution is sound. But a reader who tests the
claim will find it does not reproduce and may conclude the whole comment is FUD. Rewrite
`podfreeze`'s to say what is actually true there: `"$@"` is empty at that point, so the
advisory's remedy is meaningless rather than dangerous — and it *becomes* the `lxcfreeze`
hazard the moment the call moves inside a function or the loop stops shifting. Also drop
*"no caller supplies them"*: `scripts/test-freezelib.bash:917-940` supplies both, which is
the sentence before it.

## Should fix

### 1. The coverage denominator now counts a finding's own detail text — reachable, and the repo's own fixture already proves it

`SYMBOL_BEARING` (`verdicts.py:65`) is an unanchored `search` on the raw line, so any
`✓ `/`✗ `/`⚠ ` anywhere counts. Measured:

```
prose fixture (test_verdicts.py:69, "the gate prints ✓ when it is happy"):
  QA-VERDICTS-COVERAGE --here 0 of 1 symbol-prefixed line(s) (… 1 unrecognised) -> 0 stage(s)

a failing patterns run (qa-patterns.bash:362 emits `  ✗ <file>` per failure):
  QA-VERDICTS-COVERAGE --here 1 of 4 symbol-prefixed line(s) (… 2 unrecognised) -> 1 stage(s)
```

`qa-patterns.bash:362` is `jq -r '.failures[] | "  ✗ \(.file)\n    \(.error)"'` — indented,
so the old anchored denominator excluded it and the new one counts one phantom
`unrecognised` per failing file. `unrecognised` was added in round 1 to mean *"the parser
lost a stage line"*; it now also means *"a gate printed a bullet"*, so the fix traded a
false 100% for a false non-zero. Latent today — the `⚠ patterns: 15 partial` and `UNPARSED`
lists use `printf '    %s\n'` with no symbol, and all four real captures show zero
over-counted lines.

`test_prose_mentioning_a_tick_is_not_a_stage` is literally the control fixture for this and
passes without noticing, because it asserts `.stages` only.

Fix: keep the raw line but require the symbol at the start of the payload — a permissive
optional prefix (BOM/tab-fields/ANSI) anchored at `^`, rather than a free `search`. That
keeps the blind spot closed (a malformed timestamp still lands in the denominator) while
rejecting indentation. While there: the coverage line still says *"symbol-prefixed line(s)"*
for a count that is now symbol-*bearing*, and `SYMBOL_LINE`'s docstring at `:56-57` still
carries the "under-match" sentence that now belongs to `SYMBOL_BEARING`.

Related, and not reachable in today's gate output but structural: numerator and denominator
are now computed over **different texts**, so `matched + summary <= symbol_lines` is no
longer an invariant. `"✓\x1b[0m js: 8 files OK"` yields `unrecognised = -1` (measured). No
qa gate colours its symbols, so this is a robustness note, not a live defect.

### 2. The host deploy instruction is now wrong, and the consequence is the failure mode this plan is about

`29ceee97` edits `files/home/.local/bin/podfreeze` and `files/home/.local/bin/lxcfreeze`.
`qa-deployed-drift.bash:29` scopes `SRC_DIR` to `files/home/.local/bin` and compares with
`cmp -s` (`:181`), so a comment-only change drifts. Its abort is `qa-all.bash:127-131`,
**before** `helper-tests` — the host's `qa-all.bash` goes red and stops ~26 gates short
until deployed, which is Task 4.1's mechanism pointed at the workstation.

`PLAN.md:163-166` and the `00:41` handoff say the fix is *"`tasks/deploy-freeze-lib.yml` …
via either freeze play"* / *"Running either freeze play clears it"*. That was true when only
the shared library had changed. It is no longer: `play-podfreeze.yml:75` deploys only
`podfreeze` and `play-lxcfreeze.yml:91` only `lxcfreeze`. **Both plays are now required**,
and `29ceee97`'s commit message and journal entry say nothing about the host at all — the
commit immediately after the handoff written to stop precisely this.

Mitigating: `qa-deployed-drift.bash:194` prints `owning_play "$name"` per drifted file, so
the gate itself names the right play. The stale part is the plan's own instruction. Correct
`PLAN.md` and add a successor journal entry.

### 3. The line that has now been wrong twice, and revised three times, still has no test

`grep -rln 'helper_skipped\|helper-tests:' tests/ scripts/test-*.bash helpers/` → nothing.
The skip count went blind-to-skips (round 1) → blind-to-`expected failures=` (round 2) →
blind-to-an-earlier-match (round 3, uncommitted), and every revision was verified by hand.
The same commit added three tests for `TOOL_ABORT` in `verdicts.py`; the bash half of the
same instrument got none. The journal claims *"driven against all five shapes unittest can
emit"* — plausible as triage, but there is nothing in the repo to re-run, and the
implementation now shipping in the tree is a different one that no committed artefact drives.

`playbooks/CLAUDE.md`'s "Complex Logic → TDD Helper" is in stone and this is text parsing.
The cheapest honest fix is a `scripts/test-qa-all-helper-skips.bash` in the shape of the 22
existing `scripts/test-*.bash` gates, driving the five shapes plus the earlier-match case,
wired into `qa-all.bash` like its siblings. Round 2 prescribing the exact expression is what
let the third hole through — a prescription is not a test.

## Nits

- **A tool abort's identity is discarded; only a count survives.** `parse` counts
  `abort_lines` and drops the message, so the coverage line says `1 tool abort` and the
  table shows every other stage as `only-there` with nothing saying *which* tool was
  missing. For an instrument whose subject is machine dependence, "semgrep" is the finding.
  The capture file is on disk, so a human can look — hence a nit, not a should-fix.
- **`capture_problem` still returns the marker `no-run-summary` for a state that is now "no
  terminal line".** `_PROBLEM_DETAIL` was correctly rewritten; the marker string was not.
  Only `test_verdicts.py:287` consumes it.
- **A missing result line and a genuinely zero skip count are still indistinguishable.** In
  the working-tree version, a `grep` miss leaves `helper_skipped=0` and prints `0 skipped`.
  Same shape as the pre-existing `|| helper_summary="passed"` at `:155`. Unreachable while
  unittest's format holds, but on this specific line "clean vs blind" is the whole point.

## Checked and clean

- **`TOOL_ABORT` correctness** — 7/7 exit-2 messages match, no under-match, anchored, cannot
  shadow a stage line; control fixture present; the only over-matching strings do not exist
  in the repo.
- **Append-only and journal grammar** — additions only after line 671, times monotonic
  (01:02 → 01:03 → 01:04), headers match the file's own and `PlanJournalling.md:57-67`. The
  00:37/00:39/00:41-vs-`00:35:01` claim is accurate.
- **`compare`/`differences`/`_report` and the `terminal_lines` property** — no behaviour
  change beyond the intended one; `Parsed` is frozen and has no consumer outside
  `probe-qa-verdicts.bash:135` and its tests.
- **Version bumps** — nothing under `files/var/local/claude-yolo/**`, no playbook,
  Dockerfile, entrypoint or skill; none owed. The two freeze tools are
  `files/home/.local/bin/**`, which the drift gate covers (Should fix 2) rather than a
  version LABEL.
- **Public-repo safety** — `29ceee97` adds only `podfreeze somename` / `lxcfreeze somename`
  placeholders, commit hashes and times. No names, hosts, container names, emails or
  secrets.
- **Plan Commit Rule for `29ceee97` itself** — the journal moved with the code in the same
  commit, and Task 5.3 correctly stays unticked. The exposure is the uncommitted
  `qa-all.bash` (Blocking 1).
- **Fail-fast** — no new `failed_when`/`ignore_errors`; no skip-and-continue introduced.

## Mechanical gates

Run against the **dirty** tree, so the green below includes the uncommitted `qa-all.bash`
edit.

- `scripts/qa-all.bash`: **PASS** — `QA passed: 914 files checked`; `shellcheck: 171` and
  `patterns: 15 partial` standing. `jq` on `/tmp/qa-results.json` confirms exactly two
  SC2119 and no SC2120: `lxcfreeze:740`, `podfreeze:884`.
- `hooks-daemon plan-qa --sweep`: **PASS for 00125** — 2 findings, 0 block, both
  pre-existing and unrelated (00046 path reference; journal-freshness on 13 other plans).
- `ansible-playbook --syntax-check`: **not triggered** — no playbook in `29ceee97`;
  `qa-ansible-syntax` ran repo-wide inside `qa-all` (82 playbooks OK).
- `scripts/qa-helper-tests.bash` (**required** — `helpers/` and `tests/helpers/` changed):
  **PASS** — `Ran 1456 tests`, `OK (skipped=1)`.
- `test-freezelib` / `podfreeze` / `lxcfreeze` (**required** — freeze tools changed):
  **PASS** — 236 / 187 / 76, inside `qa-all`.
- `helpers.gnome.check_extension_compat`, `extensions` ESLint: **not triggered** — no
  `metadata.json`, no extension JS in the diff; both ran inside `qa-all` regardless.

## Task 5.3

**Not dischargeable yet.** Four things stand between it and done, in order: the working tree
must be committed and pushed so there is a fixed diff to review at all (Blocking 1);
`podfreeze:879`'s comment states a mechanism that does not hold (Blocking 2); the deploy
instruction will send the user to one play when two are needed (Should fix 2); and the
skip-extraction line reaches its third revision with no regression coverage (Should fix 3).
Separately, Phase 5 cannot close while Tasks 2.1, 2.2, 4.3 and 5.2 are open — but 5.3 itself
only needs a round that comes back clean over a committed tree.
