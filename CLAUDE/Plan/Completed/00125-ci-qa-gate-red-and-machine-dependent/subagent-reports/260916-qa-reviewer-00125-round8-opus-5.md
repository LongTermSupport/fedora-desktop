# QA Review — Plan 00125 round 8, `9b366751` on `F44`

> **Provenance.** Written by the round-8 reviewer. `Write` was withheld from this session
> (tools: `Read`, `Bash`, `SendMessage` only), so this file was authored with a quoted Bash
> heredoc. **Fifth consecutive round** in which a reviewer could not persist its own output.
> This is no longer a one-off; it is a standing property of the reviewer harness.

**Verdict**: **FIX-BEFORE-MERGE**. **Task 5.3 is not dischargeable on this commit** — but
what is owed is three small, fully specified edits, not another design round.

**Tree.** Clean at start, clean at the end, `HEAD` unchanged at `9b366751` throughout, level
with `origin/F44`. Every result below is attributable to the review target. All scratch went
to `untracked/scratch/round8/`.

---

## Blocking

None. Nothing here breaks another user, loses data, leaks private information, or violates a
HARD RULE.

---

## Should fix

### 1. `helper-tests` is now the only hard gate whose child stdout flows into `qa-all.bash`'s own verdict stream — and `verdicts.py` parses that stream

`scripts/qa-all.bash:174-175`:

```bash
if ! bash "$SCRIPT_DIR/qa-helper-tests.bash" --counts-file "$TMP_HELPER_COUNTS" \
    --counts-token "$TMP_HELPER_TOKEN" 2>"$TMP_HELPER_ERR"; then
```

Only stderr is redirected. The child's **stdout is inherited**, so it lands in
`qa-all.bash`'s stdout — the machine-read verdict stream. `helpers/qa_environment/verdicts.py:43`:

```python
STAGE = re.compile(r"^(?P<symbol>[✓✗⚠]) (?P<name>[a-z0-9][a-z0-9-]*): (?P<detail>.*)$")
```

Measured — a helper test's `print()` reaches the runner process's stdout, already in
stage-line shape:

```
$ PYTHONPATH=... python3 -m helpers.qa_environment.unittest_counts --counts-file C \
      --counts-token t t_print   > out 2> err
$ cat out
✓ helper-tests: Ran 1 test in 1 modules, 0 skipped
```

**This is a regression introduced by this commit.** The previous revision captured it —
`helper_out="$(bash "$SCRIPT_DIR/qa-helper-tests.bash" 2>"$TMP_HELPER_ERR")"` — and printed
it to stderr only on failure. And it is the odd one out: of the **29** hard-gate subprocess
invocations in `qa-all.bash` (lines 119, 138, 174, 204, 221, 239, 262, 280, 296, 309, 324,
338, 352, 367, 381, 396, 408, 423, 438, 456, 473, 491, 506, 522, 539, 549, 567, 583, 597),
**28 capture the child's output and `:174` alone does not**. That asymmetry is the evidence
it is an oversight rather than a decision.

Reachability: measured **0 bytes** on stdout from the real suite today
(`bash scripts/qa-helper-tests.bash > out` → `stat -c%s out` = 0), so it is latent, not live.
But the guard that made it unreachable was removed, and what replaced it is an unenforced
precondition over 1,482 tests that any future test can break with one `print()` — which is
this plan's own thesis about what fails.

The comments at `:162` (*"nothing a test prints shares a channel with them"*) and `:166-167`
(*"The run's human-readable output all goes to stderr"*) are both true of the **counts**. They
are not true of the **line the counts become**: that line now shares a channel with anything a
test puts on stdout.

**Fix**: capture it as every sibling does, or `>/dev/null`. Better, make the precondition a
gate rather than a comment — the counts-file run's stdout must be empty, and it is one
`[[ -s ]]` to assert it.

### 2. The token claim is stated as a law in four places, and the sentence that states it is falsifiable in two lines

This is the fifth instance of the habit rounds 6 and 7 named. The sentence:

`scripts/lib/qa-helper-summary.bash:47-49`

> THE TOKEN IS A CLOBBER DETECTOR, NOT A LOCK. The counts path travels in `argv`, so a test
> can read it; the token means the caller notices, **because a file written by anything other
> than the run it asked for carries the wrong value or none.**

Restated at `scripts/qa-all.bash:171` (*"a value **only this run knows**"*),
`helpers/qa_environment/unittest_counts.py:39-40` (*"the caller passes a value **only it
knows**"*) and `CLAUDE/QA.md:136-137` (*"passes a value **only that run knows**"*).

**What falsifies it.** The token travels in the same `argv` as the path, so anything that can
learn the path has already learned the token. Measured, from inside a test:

```
ARGV-SEEN: ['/workspace/helpers/qa_environment/unittest_counts.py', '--counts-file',
            '.../counts', '--counts-token', 'qa-all-12345-1700000000000000000', 't_argv']
```

And what actually defeats a clobber is **write ordering**, not the token. Two runs, identical
forging test, differing only in when it writes:

| forgery lands | counts file the reader finds | reader |
| --- | --- | --- |
| mid-run (before `main`'s `write_text`) | `tests=1` — the runner overwrote it | rc=0, `Ran 1 test in 1 modules, 0 skipped` (correct) |
| from `atexit` (after `main`'s `write_text`) | `token=<correct>` `tests=9999` | **rc=0, `Ran 9999 tests in 1 modules, 0 skipped`** |

So the guard's real scope is: *a file written by something that never read this run's argv*.
That is a genuine and worthwhile guard — a stale file, a concurrent run, a hardcoded path —
and the mechanism is a real improvement. But the accident the comments name as the motivation
(*"this module's own tests are themselves collected by the runner they test"*) reaches the
file through `sys.argv`, and therefore reproduces the token exactly. What saves that case is
the runner writing last, which no comment mentions.

**Fix** — four sentences, no code change:

- say what it detects: *a counts file written by something that did not read this run's `argv`*;
- say what actually defeats an in-run clobber: *the runner's own write lands after the suite,
  so anything written during the run is overwritten*;
- say what nothing detects: *a write that lands after the runner's, e.g. from `atexit`*;
- drop *"only this run knows"* in all three copies — every test in the run knows it.

The token generation itself (`qa-all-$$-$(date +%s%N)`, `qa-all.bash:173`) is **adequate** for
the purpose that survives: it only needs to be unique per run, and PID + nanoseconds is. It is
not unpredictable, but nothing in the corrected claim requires it to be.

### 3. Round 7's finding 3 is closed in one direction only, and the `COVERAGE: n of m` half was not done

The cross-check works, and I verified it rather than trusting it. Control — a `find` shim that
narrows the walk, run against the real `scripts/qa-helper-tests.bash`:

```
rc=1
ERROR: discovery missed 63 tracked helper test file(s):
    tests/helpers/containerwatch/test_cli.py
    ...
  These are tests this gate would have reported a pass over without
  running. Fix the discovery — do not untrack the files to silence it.
counts file: ABSENT
```

That closes the `mapfile`/`find` hole properly, in the same shape as
`qa-bash.bash:54-73` and `qa-python.bash:95-114`.

What is not closed is the other direction. `scripts/qa-helper-tests.bash:98-109` computes
`missed` = tracked ∖ discovered and nothing else. An **untracked**
`tests/helpers/**/test_*.py` is discovered, run, and counted, and no tracked file is missing,
so the gate passes and the number silently changes. That is not hypothetical: it is the exact
perturbation that moved round 7's own measurements **1464 → 1465 → 1479** with no change to
the commit under review, and the number it moves is the one this whole plan exists to compare
across two machines.

Round 7 asked for two things — the cross-check *and* `COVERAGE: n of m` "rather than coverage
implied by the list's length". The stage line reports `in 65 modules`, which is the `n`; there
is no `m` anywhere in the output. Today `n == m` (65 tracked helper tests, 65 modules run,
verified), so the line is right — but an operator cannot tell that from the line.

In fairness: both sibling gates share the one-directional shape, so this is a repo-wide
pattern rather than something this commit got worse. It is still the half of the finding that
was asked for and not delivered.

**Fix**: print `COVERAGE: 65 of 65 tracked helper test modules`, or list `discovered ∖ tracked`
as a warning — one `printf`.

---

## Nits

1. **"carries a key twice" is not what the code checks.** `qa-helper-summary.bash:41` promises
   a file that "carries a key twice" is refused. The guards at `:72`, `:79`, `:86`, `:93` test
   `[[ -n "$tests" ]]` — *already non-empty*, not *already seen*. Measured:

   ```
   tests=
   tests=5
   skipped=0
   modules=1
     -> rc=0  "Ran 5 tests in 1 modules, 0 skipped"
   ```

   Same for a duplicated `token=` whose first occurrence is empty. The suite's case
   (`test-qa-helper-summary.bash:226-231`) uses `tests=3` / `tests=4`, both non-empty, so it
   cannot see this. Unreachable from the runner, which never writes an empty value — but it is
   a stated guarantee the code does not have, in the file whose subject is exactly that.
   Fix: four `seen_*` flags, or set the variable to a sentinel.

2. **The empty-file diagnostic misdiagnoses the one case the header says is reachable.** The
   token check runs before the digit checks (`:111`), so the `os._exit(0)` case — named at
   `:53-55` as *reachable* — reports:

   ```
   helper_counts_summary: <path> carries token '', expected tok — the file was
     written by something other than the run that asked for it
   ```

   when the truth is that **nothing wrote it at all**. The two have different remedies, which
   is the argument the suite itself makes at `:298-300` for distinguishing them. Add an
   explicit empty-file branch before the token check.

3. **`qa-helper-summary.bash:50-51` inverts its own lesson.** *"It stops an accident, not an
   attempt — which is the honest claim, and the previous four revisions each failed by stating
   a **narrower** guarantee than they had."* They failed by stating a **broader** one. Stating
   a narrower guarantee than you have is harmless; that is the whole point of the sentence.

4. **`CLAUDE/QA.md:151` is true but will read as stale.** *"There are no `subTest` skips in
   `tests/helpers` today"* — correct: the three conditional skip sites
   (`tests/helpers/displaylink_recovery/test_run_recovery.py:138`, `:240`, `:254`) are none of
   them inside a `subTest` block, verified by reading each. But `subTest` itself appears at
   roughly **70** sites across `tests/helpers`, so the next person to grep will conclude the
   caveat is already false. Say *"no skip is raised inside a `subTest` block"*.

5. **`PLAN.md:208` carries an intermediate mutation figure.** It says *"6 mutations of the
   runner, **8** of the reader, all caught"*. The commit message says **13** of the reader, and
   the journal shows why — 8 at `03:5x` (`JOURNAL/00125-Journal-26-09-16.md:621-625`) plus
   *"5 further mutations of the reader"* after the token and module additions (`:733`). The
   plan was not re-touched after the second round of mutation testing. 8 + 5 = 13; the plan
   should say 13.

6. **`in 1 modules`.** `qa-helper-summary.bash:135-139` singularises `test`/`tests` and not
   `module`/`modules`, and two cases pin the ungrammatical form
   (`test-qa-helper-summary.bash:129`, `:140`). Either singularise both or neither.

7. **The git prelude is now triplicated verbatim.** `qa-discovery.bash:205-214`, `:339-348`
   and `:376-385` are the same eleven lines three times. A `qa_require_git_checkout` would
   make the third copy an argument for extraction rather than a third copy.

8. **Exit-code inconsistency with the siblings.** `qa-helper-tests.bash:108` exits **1** on a
   coverage failure; `qa-bash.bash:72` and `qa-python.bash:113` exit **2** for the identical
   condition, and `CLAUDE/QA.md:22-23` reserves `2` for "cannot verify". `qa-all.bash` collapses
   both to 1, so nothing observable turns on it — but the three sites should agree.

9. **`PLAN.md` is 17,993 bytes** — seven bytes under the 18,000 advisory. The next sentence
   trips it.

10. **`unittest_counts.py:113-115`.** The `wasSuccessful()` comment sits above the
    `write_text` call it does not describe; the line it explains is the `return` at `:120`.

---

## Checked and clean

- **Hole B — the owner's claim holds, and is stronger than stated.** Four routes probed, all
  fail closed, none reaches a green stage line:

  | route | runner rc | counts file | reader |
  | --- | --- | --- | --- |
  | module does not import (`_FailedTest`) | 1 | written, `tests=1 errors` | n/a — gate already aborted |
  | a test calls `os._exit(0)` | **0** | zero bytes | refused |
  | module-level `sys.exit(0)` at import (not an `ImportError`) | **0** | zero bytes | refused |
  | module-level `raise unittest.SkipTest` | 1 | zero bytes | refused |

  The two rows where the *runner* exits 0 are the interesting ones — `set -e` sees success and
  the reader is what stops the run. Hole D is doing real work, on more than the case it was
  written for.
- **Hole A — closed.** `CLAUDE/QA.md:148-152` corrected; `counts_text` does not clamp and
  `test_a_skip_count_above_the_test_count_is_rendered_faithfully` plus
  `test-qa-helper-summary.bash:139-141` pin it from both sides.
- **Hole D — verified end to end** against the real shape (`mktemp`-style pre-created empty
  file), not just as a fixture.
- **Reader parsing, probed rather than reasoned about.** Refused: CRLF (`tests=5\r`), a key
  with leading whitespace, a value containing `=`, an empty file, a path that is a directory,
  a dangling symlink, a file with no `token=` when one is expected, an unknown key, a negative
  count, trailing text. Accepted correctly: keys in any order, a trailing blank line, no
  trailing newline, `tests=1` singularised. The `|| [[ -n "$key" ]]` final-line guard works and
  is covered.
- **Counts file vs the run.** `modules=len(args.modules)` — names requested, and discovery is
  `find … | sort`, which cannot emit a duplicate, so it cannot double-count. `wasSuccessful()`
  rather than `failures or errors`, with the unexpected-success rationale stated and tested
  (`test_an_unexpected_success_fails_the_run_as_unittest_itself_would`). The write happens
  after `runner.run(suite)` returns, so it describes the population that ran.
- **Discovery edge cases, measured in a synthetic repo.** A **tracked symlinked** test is in
  the yardstick (`[[ -f ]]` follows the link) but invisible to `find -type f` → the gate
  hard-fails rather than silently dropping it. A **tracked-but-deleted-from-worktree** file is
  correctly excluded by the same guard, matching the siblings.
- **Naming.** `unittest_counts`, `counts_text`, `helper_counts_summary`,
  `qa_tracked_helper_tests`, and the stage rename `helper-summary-readers` →
  `helper-counts-reader` all say what the thing does. No hygiene/management/support words.
- **Placement in the IaC graph.** No new play, no new script, no new gate, no new abstraction.
  The one new function went into `scripts/qa-discovery.bash` beside its two siblings; the
  reader stayed in the library that already owned it; the runner went into the existing
  `helpers/qa_environment/` package. Nothing is anywhere new.
- **`helpers/CLAUDE.md`.** Stdlib only (`argparse`, `pathlib`, `sys`, `unittest`). No
  `__init__.py` under `helpers/qa_environment/` or `tests/helpers/qa_environment/`. No
  `subprocess` in the helper; the test's one call passes explicit `check=False` with the
  return code asserted on the following lines. Test mirrors the helper path exactly.
- **Fail-fast.** No new `|| true`, `failed_when`, `ignore_errors` or `set +e` anywhere in the
  diff. The single new `2>/dev/null` is `test-qa-helper-summary.bash:103`, inside `refuses()`,
  with the rc captured and checked and the channel asserted separately at `:262-314` — the
  probe-then-check pattern, correctly applied. `qa-all.bash:190-193` consumes the reader's
  failure. Multi-command blocks keep `set -euo pipefail`.
- **Stderr hygiene.** `qa-helper-tests.bash` stdout measured at **0 bytes**; progress and
  unittest's output on stderr; the reader's payload on stdout with every diagnostic on stderr,
  asserted rather than assumed. (See should-fix 1 for the one channel that is not contained.)
- **Version bumps** — none owed. No `files/var/local/claude-yolo/**`, Dockerfile, entrypoint,
  patch script or deployed skill in the diff.
- **Public-repo safety** — 1,846 added lines scanned: no non-`example.com` email, no home path,
  no RFC 1918 address, no `.local` hostname, no key block, no token prefix. The only `/home/`
  hit is the repo's own `files/home/.local/` tree.
- **Plan Commit Rule** — code, `PLAN.md`, `FINDINGS.md`, the journal and round 7's report all
  landed in one commit. Tree clean, level with `origin/F44`. Journal **append-only** (162
  added, 0 removed). `CLAUDE/Plan/README.md:37` index row present. `PLAN.md` header says
  *In Progress* over six genuinely open tasks — nothing ticked that was not done.
- **Task 4.5 was filed, not dropped.** The `grep -oE 'passed: [0-9]+'` count is **21**,
  verified. Filing it rather than fixing 21 gates inside a commit about one of them is the
  right call. Note for the lead: **4.5 is open and is NOT blocked on the user**, unlike 2.1,
  2.2, 4.3 and 5.2.
- **Every number this commit touched, re-derived from the real run** rather than accepted:
  38 emitted stage lines, **37** distinct names (only `patterns` duplicated), **36** gates,
  **26** stage names after `helper-tests`, 27 short of the drift gate, 7 + 29 = 36, 920 files,
  65 tracked helper tests = 65 modules run, 18 tests in `test_unittest_counts`, 29 cases in
  `test-qa-helper-summary.bash`. Line citations: `qa-all.bash:138` is the drift gate's `if !`
  (correct); `qa-all.bash:610-627` is the jq merge program including its file list (correct —
  round 7's `613-620` was wrong both before and after).
- **Modes and lint** — `100644` for the library, the discovery file and both Python files;
  `100755` for the three executables. `shellcheck -x` clean on all five shell files; `ruff
  check` clean on both Python files.

---

## Mechanical gates

| gate | result |
| --- | --- |
| `./scripts/qa-all.bash` | **exit 0**, `✓ QA passed: 920 files checked`. `✓ helper-tests: Ran 1482 tests in 65 modules, 1 skipped`; `✓ helper-counts-reader: passed: 29` |
| `scripts/qa-helper-tests.bash` (no `--counts-file`) | rc 0, stdout **0 bytes**, stderr `Ran 1482 tests in 20.771s` / `OK (skipped=1)` |
| `scripts/qa-helper-tests.bash --counts-file` | rc 0, file reads `tests=1482` / `skipped=1` / `modules=65` |
| `scripts/qa-helper-tests.bash --bogus` / `--counts-file` with no value | rc 2, usage error on stderr |
| `scripts/test-qa-helper-summary.bash` | `passed: 29 failed: 0` |
| `python3 -m unittest tests.helpers.qa_environment.test_unittest_counts` | `Ran 18 tests in 0.127s` / `OK` |
| `hooks-daemon plan-qa --sweep` | exit 1, **0 block / 2 advise**, both pre-existing and unrelated (`path-existence` on 00046; `journal-freshness` naming 13 other plans) |
| `shellcheck -x` (5 changed shell files) | clean |
| `ruff check` (2 Python files) | clean |
| `ansible-playbook --syntax-check` | **not triggered** — no playbook in the diff. `qa-all`'s repo-wide stage ran anyway: 82 playbooks OK |
| `qa-helper-tests.bash` as a conditional gate | **triggered** by the `helpers/` + `tests/helpers/` change — run, green |
| `check_extension_compat` / `extensions` ESLint | **not triggered** — no extension metadata or JS in the diff. Both ran inside `qa-all`: 5 extensions, 11 JS files |

### CI run `35053479172` at `9b366751`

Finished: **failure**, and the only `✗` is Cause A.

```
✗ docs: 8 finding(s) across 71 files
✗ QA FAILED: 8 errors in 920 files
```

Everything else green, including **all 26 gates behind `helper-tests`**. The deliverable is
produced on a runner, not asserted:

```
local : ✓ helper-tests: Ran 1482 tests in 65 modules, 1 skipped
CI    : ✓ helper-tests: Ran 1482 tests in 65 modules, 2 skipped
```

Same test count, same module count, **only the skip count differs** — which is precisely the
signal the plan set out to create, and the module count now proves the two machines collected
the same population rather than leaving it inferred. `✓ helper-counts-reader: passed: 29` green
on a runner first try. File counts match exactly, 920 both sides.

---

## Task 5.3 — not dischargeable on `9b366751`, and close

Round 7 set the bar: *"a round must review the replacement and come back clean, with Holes A–D
closed and finding 3 addressed."* Measured against that bar:

- **Hole A** — closed.
- **Hole B** — closed, and the claim that the silent-green form is unreachable **holds**; I
  probed four routes including two where the runner itself exits 0, and all fail closed.
- **Hole C** — the mechanism is closed as far as it can be; the **claim about it** is not
  (should-fix 2).
- **Hole D** — closed, verified end to end.
- **Finding 3** — closed in the direction that mattered, with a control that could have
  failed; the `COVERAGE: n of m` half is outstanding (should-fix 3).
- **Finding 4** — closed. `PLAN.md:176-179` now says "twice" and names `js` as the
  accumulating stage that was never masked.

So it does not come back clean. But nothing outstanding is a design question: a redirect on
one line, four sentences of claim correction across four files, one `printf`, and the nits.
**A ninth full round is not warranted** — a targeted re-check of those three edits is enough
to discharge 5.3.

**What is genuinely done, said plainly.** Abandoning the scrape was right, and this is the
first revision in five that did not need a sixth match rule, because there is no match rule
left to get wrong. The counts come off the `TestResult` object, the two disagreeing readers
are one, the runner's own tests fire a padded-stdout decoy, a stderr decoy and an `atexit`
decoy at a real subprocess and assert *both* that the counts are right and that the attack
reached the streams — so the case cannot pass by the attack failing. Round 7's finding 3 got
the generalisation the repo had failed to make twice, in the third site, with a control that
reports 63 missed files. Every load-bearing number in the two plan documents was re-derived
here from a real run and all of them are right, including the two line citations that were
wrong before. And the central deliverable is demonstrated on two machines rather than argued
for: 1482 tests, 65 modules, 1 skip here against 2 on a runner.

**What has still not converged** is the same habit round 6 and round 7 each named, now in its
fifth instance. *"A pipe cannot produce that ordering"*, *"can never precede"*, *"always
last"*, *"no stream a test can write to is parsed"*, and now *"a file written by anything
other than the run it asked for carries the wrong value or none."* Each was true of the thing
measured and stated about a wider population than was measured. The fifth is the mildest of
the five and the mechanism behind it is sound — but it took two lines to falsify, which is the
same cost as the previous four.

---

# Addendum — follow-up questions, and a tree that moved

## PROCESS DEFECT: the working tree was mutated during this review, after two explicit promises that it would not be

The dispatch said *"I will not touch the working tree while you run"*, and the follow-up
said *"The tree is still clean at `9b366751` and I will not touch it until you report. If you
see it move, that is a defect and I want to hear about it."* It moved, mid-addendum:

```
$ git status --short
 M scripts/lib/qa-helper-summary.bash
 M scripts/qa-all.bash
 M scripts/qa-helper-tests.bash
```

`HEAD` is still `9b366751`. All three modifications are **implementations of this round's own
findings**, landing while the round was still running:

| worktree change | this report's item |
| --- | --- |
| `qa-all.bash` — `TMP_HELPER_OUT`, child stdout captured, `[[ -s ]]` gate, trap extended | should-fix 1 |
| `qa-all.bash:170-176`, `qa-helper-summary.bash:44-56` — token claim rewritten to "did NOT read this run's argv" / "WRITE ORDERING" | should-fix 2 |
| `qa-helper-tests.bash:113-135` — `COVERAGE: n of m` plus the untracked direction | should-fix 3 |
| `qa-helper-tests.bash:108-110` — coverage failure now `exit 2` | nit 8 |
| `qa-helper-summary.bash:41` — "carries a key twice" → "repeats a key"; empty file gets its own message | nits 1, 2 |
| `qa-helper-summary.bash:52-53` — "narrower" → "BROADER" | nit 3 |

The fixes look right on a read, and I am not asking for them to be reverted. Two things
follow that do matter:

1. **Nothing in the main report above is affected.** Every finding was measured before the
   tree moved, and the two re-checks this addendum was asked for were deliberately run against
   `git show 9b366751:<path>`, not the worktree. The citation analysis below is against
   `FINDINGS.md`, which is unmodified.
2. **The fixes are unreviewed.** They were not in the reviewed commit and no round has looked
   at them. In particular the new `[[ -s "$TMP_HELPER_OUT" ]]` gate and the new `COVERAGE`
   line are new gate behaviour, and the `COVERAGE` line goes to **stderr**, where
   `verdicts.py` will not see it — which may or may not be intended, and is exactly the sort
   of thing a round should check rather than assume.

This is the second consecutive round in which a reviewer and an implementer shared one
checkout. Round 7 said a worktree for one of the two would remove it; it was not done, and it
happened again.

## The `qa-all.bash:610-627` citation — both of us are wrong, and in different places

`FINDINGS.md:312`: *"they merge into one JSON document **and are reported together** by
`qa-all.bash:610-627`."*

Derived independently from the committed blob:

| line | content | role |
| --- | --- | --- |
| 604 | `# Merge JSON from all checks` | the section's own anchor |
| 608 | `jq -s \` | **`-s` is what performs the merge** — without slurp, `.[0]`..`.[6]` address nothing |
| 609 | `--arg status "$STATUS" \` | binds `$status`, which line 611 *inside the cited range* dereferences |
| 610–626 | the jq program literal | the shape of the merged document |
| 627 | `}' "$TMP_BASH" … "$TMP_DOCS" > "$JSON_OUT"` | the seven operands — what makes it *seven* stages |
| 630–638 | `TOTAL=…`; `✓ QA passed: $TOTAL files checked` / `✗ QA FAILED: $NERRORS errors in $TOTAL files` | the **single joint verdict line** |

**Why 610 is wrong under every reading.** Line 610 is `    '{` — the opening of a shell
single-quoted string whose command word is on 608. Extracted verbatim, 610–627 is not a
command; it begins mid-token. Your re-derivation of **608** is correct, and for a stronger
reason than adjacency: `-s` on 608 is the merging, and the sentence's first verb is "merge".

**But the span is also wrong at the other end, which neither of us caught.** The sentence has
two verbs. The *merge* is 608–627. "Reported **together**" — the seven producing one verdict
line rather than seven — happens at **630–638**, and that is the behaviour the very next
paragraph leans on: *"`✗ docs` followed by 28 passing stages and only then `✗ QA FAILED`"*
(`FINDINGS.md:317-320`). No span ending at 627 covers it.

So:

- if "reported together" means *collected into one JSON document* → **608–627**;
- if it means *speak with one verdict line*, which is what the surrounding argument about
  masking actually turns on → **608–638**, or better two citations: merge 608–627, joint
  verdict 630–638.

I would write the second. `610-627` is wrong at both ends; `608-627` is right about the merge
and silent about the report.

## On stating it as an anchor instead — yes, but target it

Measured base rate across the ten source-line citations the two plan documents carry:

| citation | verdict |
| --- | --- |
| `link_check.py:214-223` (×2) | correct — `_EXCLUDE_PREFIX`, exactly that span |
| `triage.bash:18-23` | correct — the R2 comment block, exactly that span |
| `qa-deployed-drift.bash:219` | correct — the `files/home/.local/lib/freeze/*` pair |
| `qa-deployed-drift.bash:192` | correct — `owning_play "$name" >&2` |
| `play-podfreeze.yml:75`, `play-lxcfreeze.yml:91` | correct — the `src:` line of each deploy task |
| `qa-all.bash:138` (×2) | correct **today**; round 7 measured it wrong by three at `27d7b342`, repaired by coincidence when this commit shifted lines +3 |
| `qa-all.bash:610-627` | **wrong**, third wrong value in three commits |

Seven right on purpose, two right by luck, one wrong — and **every unstable one points into
`qa-all.bash`**, the single file this plan edits on nearly every commit. The others point into
files the plan does not touch and they have not moved. So the recommendation is not "stop
using line numbers"; it is "stop using them for `qa-all.bash`".

**The reason it is worth changing, in this plan's own terms.** A stale line number still
*resolves*. `613` pointed at `"patterns": .[2]`; `610` points at `'{`. Both are real lines in
a real file, so a wrong citation reads exactly like a verified one — a clean result
indistinguishable from a blind one, which is this plan's subject, reproduced in the plan's own
citations. An anchor fails the other way round: `grep -n 'jq -s' scripts/qa-all.bash` returns
a line or it returns nothing, and nothing is a failure you can see.

There is in-repo precedent for the principle: `check_qa_gate_inventory_in`
(`helpers/docs/link_check.py`) does not record where the gates are, it *derives* them from
`qa-all.bash` and compares both directions against `CLAUDE/QA.md`'s rows. Plan documents are
outside that gate's scope, but a greppable anchor gets most of the benefit for nothing.

Both candidate anchors are unique in the file at `9b366751` (verified: 1 occurrence each, as
is `jq -s`). Suggested replacement for `FINDINGS.md:310-312`:

> The seven accumulating stages are `bash`, `python`, `patterns`, `ansible`,
> `ansible-syntax`, `js` and `docs`; the `jq -s` invocation under
> `# Merge JSON from all checks` in `qa-all.bash` merges them into one JSON document, and the
> `# Final terse summary` block below it reports all seven in a single verdict line.

Same for `qa-all.bash:138` in `PLAN.md:164` and `FINDINGS.md:238` — "the
`qa-deployed-drift.bash` invocation in `qa-all.bash`" cannot rot. Leave the other five alone;
they are stable and precise.

## The two round-7 carry-overs, re-checked against the commit

Both run against `git show 9b366751:<path>`, so neither is contaminated by the worktree.

- **EXIT traps — clean at the commit.** Exactly one `trap … EXIT` per script and none in the
  sourced library: `qa-all.bash:36` (naming all nine temp files), `qa-helper-tests.bash:67`
  (scoped inside the no-`--counts-file` branch, so it cannot fire on a caller-supplied path),
  `test-qa-helper-summary.bash:57`. `qa-helper-summary.bash` and `qa-discovery.bash` have
  zero. No clobber anywhere.
- **The progress line is on stderr at the commit.**
  `git show 9b366751:scripts/qa-helper-tests.bash` line 118 is
  `echo "Running ${#modules[@]} helper test module(s)..." >&2`, and a live run measured
  **0 bytes** on stdout with the line present once on stderr. Round 7's nit — that the
  constraint "must stay on stdout" was recorded only in a comment — is not merely resolved,
  it is **inverted**: nothing parses that stream any more, so stderr is now the correct home
  and `CLAUDE/StderrHygiene.md` and the code agree. Note this is what made should-fix 1
  possible to miss: the *child's* hygiene became perfect at the same moment the *parent*
  stopped containing it.

## Why the redesign exists — the four falsifications, cited

Round 7's completion notice falsified *"on stderr, unittest's summary is always last"* four
ways, not one, each measured against a live `python3 -m unittest` with stderr captured alone:

| mechanism | lands after the summary |
| --- | --- |
| `atexit` handler | yes — `Ran 3 tests in 9.9s` / `OK (skipped=99)` |
| `__del__` at interpreter shutdown | yes — `OK (skipped=99)` |
| non-daemon thread outliving the summary | yes — both lines |
| orphan subprocess inheriting stderr | yes — `OK (skipped=99)` |

Four independent mechanisms, no shared cause beyond "the interpreter is still alive and
something else owns the fd". That is the evidence that mattered: a single counter-example
invites a fifth match rule, four disjoint ones establish that the population is not
constrained at all — which is the actual argument for taking the numbers off the `TestResult`
object. It belongs in `FINDINGS.md`'s "The counts are not in the text" table, which currently
records only the `atexit` row.
