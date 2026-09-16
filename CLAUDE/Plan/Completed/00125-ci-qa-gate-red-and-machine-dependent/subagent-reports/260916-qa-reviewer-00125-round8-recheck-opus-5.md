# QA Re-check — Plan 00125 round 8 follow-up, `0b1623fa` on `F44`

> **Provenance.** Written by the round-8 re-check reviewer. `Write` was withheld from this
> session (tools: `Read`, `Bash`, `SendMessage`), so this file was authored with a quoted
> Bash heredoc. **Sixth consecutive round** in which a reviewer could not persist its own
> output.

**Scope.** A targeted re-check of round 8's three should-fix items and ten nits, not a
ninth full round. Nothing rounds 1–8 cleared was re-reviewed.

**Verdict**: **FIX-BEFORE-MERGE**. Two of the three findings are closed and verified with
controls that could have failed. The third is closed in `qa-helper-tests.bash` and
**invisible through `qa-all.bash`**, which is the invocation `CLAUDE/QA.md` mandates — so the
defect it names still happens end to end, measured. Plus one new over-claim, in the sentence
written to retract the last one.

**Tree.** Clean at start, clean at the end, `HEAD` unchanged at `0b1623fa` throughout, level
with `origin/F44`. `HEAD` did not move and the tree never went dirty except for two named
control fixtures that were created and removed inside single commands with `trap`-guarded
cleanup; `git status --short` was empty immediately after each. All scratch went to
`untracked/scratch/round8-recheck/`.

---

## Finding 1 — CLOSED, reproduced

Every part of the claim holds. Control run, not believed:

```
tests/helpers/qa_environment/test_zz_recheck_forge_fixture.py   (untracked, then deleted)
    print("✓ helper-tests: Ran 1 test in 1 module, 0 skipped")

$ ./scripts/qa-all.bash            rc=1
stderr:
  ✗ QA FAILED: helper-tests wrote to stdout, which is this suite's verdict stream
    A test printing here can forge or split a stage line. Send it to stderr.
  ✓ helper-tests: Ran 1 test in 1 module, 0 skipped
stdout: grep -c "helper-tests" → 0
```

- **The capture is real** and **the `[[ -s ]]` gate fires** — `qa-all.bash:179,190-195`. The
  forged stage line reached **0** occurrences of stdout; before the fix it reached the
  verdict stream `verdicts.py:43` parses.
- **The trap names every temp file.** Derived rather than eyeballed: the set of
  `TMP_*=$(mktemp)` assignments and the set named in the `trap` at `:37` are **equal**, ten
  each, difference empty in both directions. One `trap … EXIT` in the file.
- **Failure output order is readable** — the `✗` verdict first, the remedy second, the
  offending bytes third, all on stderr.
- **Nothing legitimate is tripped, and this is not a guess.** Measured **0 bytes** of stdout
  from the real suite on four separate runs. More usefully, the repo has already produced
  this exact leak once: Plan 00104 round 5 recorded *"`cli.main()` prints unconditionally in
  tests, so `qa-helper-tests.bash` output now interleaves four stray `COVERAGE:` lines after
  `OK`"* and fixed it with `contextlib.redirect_stdout` — i.e. the gate would have fired on a
  real historical case, and the repo's own verdict then was that it was a defect. The ~25
  `subprocess.run(` call sites under `tests/helpers` all capture today.

Nit on the message: *"Send it to stderr"* is the right remedy for a stray `print()` and the
wrong one for the other way a test can leak here — an uncaptured subprocess, whose fix is
`capture_output=True`. Two words.

## Finding 2 — the three statements are TRUE; a sixth over-claim entered with them

The three limbs, each measured against the real runner (fixtures under
`untracked/scratch/`, removed):

| statement | control | result |
| --- | --- | --- |
| write ordering defeats a mid-run forgery | test writes `tests=9999` + valid token mid-run | runner rc 0, file reads `tests=1` — **overwritten**; reader prints the truth |
| the token detects a file written by something that never read this run's `argv` | stale `token=some-other-run`, test calls `os._exit(0)` so the runner never writes | runner rc **0**, reader rc **1**, token-mismatch message — **detected** |
| a write landing after the runner's is detected by nothing | forgery registered via `atexit` | runner rc 0, reader rc **0**, `Ran 9999 tests in 1 module, 0 skipped` — **undetected**, exactly as documented |

All three true as written. No live copy of *"only this run knows"* survives — the four
remaining hits are FINDINGS.md and the journal quoting the retracted claim, which is correct
use.

**But the replacement sentence over-claims, in four places, and its counter-example is in
the same paragraph.**

- `scripts/qa-all.bash:171-172` — *"anything that can find the file has the token too"*
- `scripts/lib/qa-helper-summary.bash:49-50` — *"anything able to find the file already has the token"*
- `helpers/qa_environment/unittest_counts.py:48-49` — *"anything that can find the file already has the token"*
- `CLAUDE/Plan/…/FINDINGS.md:173-174` — *"anything that can find the file already has the token"*

Each of those sentences is two lines below its own refutation: **"a hardcoded path"**, named
as a case the token detects. Something writing to a hardcoded path can find the file and does
**not** have the token — that is the token's entire remaining value. Read literally, the
sentence says the token buys nothing, which argues for deleting it.

This is the same failure mode as the previous five: measured on one member (a test reading
`argv`), stated over the population (anything that can find the file). It is the sixth, and
it is inside the retraction of the fifth.

**`CLAUDE/QA.md:140` is the one copy that is already right** — *"It is not a lock, because the
token rides in the same `argv` as the path"* states the mechanism and claims no population.
Fix: copy QA.md's shape, or add the clause — *"anything that reaches the file **by reading
`argv`** already has the token"*. Four sites, no code change.

One residual imprecision, reported for completeness and **not** counted as a defect:
`qa-helper-summary.bash:47-48` credits the token with detecting *"a counts file written by
something that never read this run's argv"*, and a **mid-run** foreign write is handled by
write ordering rather than by the token. Under the natural reading — *of the file the reader
actually sees* — the sentence is true, and no wrong conclusion follows either way.

## Finding 3 — half-closed again: the coverage exists and never reaches an operator

The mechanics are exactly as claimed, reproduced with a control:

```
tests/helpers/qa_environment/test_zz_recheck_untracked_fixture.py   (untracked, then deleted)

$ bash scripts/qa-helper-tests.bash --counts-file … --counts-token …     rc=0
stdout: 0 bytes
stderr: COVERAGE: 65 of 65 tracked helper test modules
          plus 1 UNTRACKED file(s), which run here and nowhere else:
            tests/helpers/qa_environment/test_zz_recheck_untracked_fixture.py
        Running 66 helper test module(s)...
        Ran 1483 tests …  OK (skipped=1)
counts: tests=1483 skipped=1 modules=66
```

Arithmetic correct, independently confirmed: `git ls-files` gives **65** tracked
`tests/helpers/**/test_*.py`; 66 discovered − 1 untracked = 65. On stderr, out of the counts,
out of the verdict stream — all as claimed.

**Now the same fixture through `qa-all.bash`, which is the only invocation `CLAUDE/QA.md:7,13`
permits** (*"ALWAYS and ONLY use this single command"*, *"NEVER use individual scripts
directly"*):

```
$ ./scripts/qa-all.bash            rc=0
stderr: 0 bytes
grep "COVERAGE\|UNTRACKED\|untracked" over BOTH streams → only version-pins' own line
✓ helper-tests: Ran 1483 tests in 66 modules, 1 skipped
✓ QA passed: 921 files checked
```

`qa-all.bash:178-183` cats `TMP_HELPER_ERR` **only inside the failure branch**, so on a pass
the whole of the child's stderr is discarded. Measured `0 bytes` of stderr on a clean run too.

So, stated plainly: **an untracked test runs, is counted, the gate passes, and the number
silently moves — 1482 → 1483 and 65 → 66 — with no signal anywhere on the operator's path.**
That is round 7's finding 3 verbatim, and the reason it was raised: *"the number it moves is
the one two machines are compared on"*. The fix made the information exist. It did not make
it arrive. Same on CI — run `35055525017` carries no COVERAGE line from this gate either.

This is also the rubric's *"a gate whose only visible output is a failure is indistinguishable
from a gate that is not running"*, which this repo has already paid for twice (Plan 00081, two
gates documented and unrun for months).

The comment at `qa-helper-tests.bash:118` cites the right precedent and then diverges from it:
`qa-version-pins.bash:191` puts `COVERAGE: 9 of 9` on **stdout**, inside its stage line, which
is why you can see it. This one cannot do that — finding 1's new gate would reject it. The two
fixes collide, which is why the visible fix has to be in `qa-all.bash`.

**Fix** — the one that fits the design: the counts file already carries `modules=`; have
`qa-helper-tests.bash` write the tracked total alongside it and let the stage line read
`Ran 1482 tests in 65 of 65 tracked modules, 1 skipped`, with the untracked count appended
when non-zero. Alternatively `qa-all.bash` re-emits the `COVERAGE`/`UNTRACKED` lines from the
captured stderr. **Do not** make an untracked discovered test a hard failure — that
contradicts `qa-discovery.bash:200-202`, which keeps `find` as the discovery source
deliberately *"so a brand-new, not-yet-`git add`ed script is still gated"*.

---

## The ten nits — all closed; two carry residue

| nit | state | evidence |
| --- | --- | --- |
| 1 — duplicate detection tests "has a value" not "seen" | **closed** | `seen_*` flags at `qa-helper-summary.bash:95,103,111,119`. `token=` then `token=<real>` → `duplicate token=`, rc 1. `tests=` then `tests=5` → `duplicate tests=`, rc 1. Same for `skipped`, `modules`. Single-occurrence path unharmed: `Ran 1 test in 1 module, 0 skipped` and `Ran 1482 tests in 65 modules, 1 skipped` both rc 0; a lone `tests=` still fails the digit check, correctly |
| 2 — empty file misdiagnosed as a token mismatch | **closed** | `:78-82`, before the token check. Zero-byte file → `… is empty — the runner did not write it` |
| 3 — "narrower"/"broader" inverted | **closed** | `:57-58` now reads *"claiming a guarantee BROADER than they had"* |
| 4 — `subTest` caveat reads stale | **closed and accurate** | `QA.md:158-161`. Verified rather than inherited: `subTest` occurs **69** times across 21 files (≈70 ✓); the three `skipTest` sites (`test_run_recovery.py:138,240,254`) are each at method top level, not inside any `with self.subTest(...)` — read each |
| 5 — mutation figure 8 → 13 | **closed** | `PLAN.md:208` reads 13, agreeing with the commit message |
| 6 — `in 1 modules` | **closed** | `module_noun` at `:166-169`; three fixtures updated; measured `Ran 1 test in 1 module` |
| 7 — git prelude triplicated | **closed, with a wrong number** | see below |
| 8 — coverage failure exits 1, siblings exit 2 | **closed** | control: a `find` shim narrowing the walk → **rc 2**, 63 missed files listed, COVERAGE line correctly absent (aborts first) |
| 9 — `PLAN.md` at 17,993 bytes | **closed** | **17,961** bytes |
| 10 — `wasSuccessful()` comment misplaced | **closed** | now above the `return` at `unittest_counts.py:126-128`; the `write_text` gets its own ordering comment |

### Nit 7's residue — two small things

**a. The comment says three; it was four.** `qa-discovery.bash:207-208`: *"Extracted when the
**third** caller arrived: **three** verbatim copies…"*. Measured at the parent commit —
`git show 9b366751:scripts/qa-discovery.bash | grep -n "git not found"` → lines **206, 274,
340, 380**: **four** copies, four callers. The commit message for `0b1623fa` gets it right
(*"my function had made it a **fourth** verbatim copy"*), so the code comment contradicts its
own commit message. Round 8's nit 7 counted three — it missed the copy inside
`qa_tracked_shell_scripts` itself — and the fix inherited the miscount. One word, twice.

All four call sites are otherwise correct: `qa_tracked_shell_scripts:224-225`,
`qa_tracked_playbook_candidates:283-284`, `qa_tracked_python_files:340-341`,
`qa_tracked_helper_tests:371-372`. Each dropped `git_probe` from its `local` list (no longer
used) and each still passes `"$repo_root"`. Body byte-identical to the four it replaced;
`exit 2` still exits the script from inside the nested call. No behaviour change.

**b. A doc comment was orphaned by the insertion.** `qa-discovery.bash:198-202` —
*"Populate `QA_TRACKED_SHELL_FILES` with every TRACKED shell script… This is the yardstick…"*
— now sits directly above `qa_require_git_checkout`'s own header at `:203`, and
`qa_tracked_shell_scripts()` at `:223` has no header comment at all. The new function went in
between a comment and the function it describes.

---

## Also noted (no action required)

- **The `COVERAGE: n of m` ratio is tautological.** After the `missed` guard exits 2,
  tracked ⊆ discovered, so `n` = |discovered| − |untracked| ≡ |tracked| = `m`. The line can
  only ever print `m of m`. Not wrong — `m` is stated as a number, which was the ask, and the
  untracked count is stated separately — but the ratio itself carries no independent signal.
- **A `\n`-only counts file** is 1 byte, so `[[ ! -s ]]` misses it and it falls through to the
  token-mismatch message that nit 2 removed for the zero-byte case. Unreachable — `mktemp`
  creates zero bytes and nothing writes a bare newline.
- **`qa-all.bash:33`** — *"leaking the seven above"*. The trap now protects ten temp files; a
  replaced trap would leak all ten. Positionally still true, understated.

## Checked and clean

- **Fail-fast**: no new `failed_when`, `ignore_errors`, `|| true` or `set +e` in the diff. The
  new untracked branch warns and continues, which is deliberate and correct here (see finding
  3's fix note) — but it is the branch whose signal never arrives.
- **Stderr hygiene**: `qa-helper-tests.bash` stdout measured **0 bytes** with and without
  `--counts-file`; `test-qa-helper-summary.bash` stderr **0 bytes** on a pass;
  `test_unittest_counts` stdout **0 bytes**. The reader's payload is on stdout, every
  diagnostic on stderr.
- **Modes**: `100755` for the three executables, `100644` for the library, `qa-discovery.bash`
  and the Python file — unchanged.
- **Plan Commit Rule**: tree clean, level with `origin/F44`. Code, `PLAN.md`, `FINDINGS.md`,
  the journal and round 7's report all in `0b1623fa`. Journal append-only (67 added, **0**
  removed). `CLAUDE/Plan/README.md:37` index row present. `PLAN.md` header *In Progress* over
  six open tasks; nothing ticked that was not done; Task 5.3 correctly still `⬜`.
- **Public-repo safety**: added lines scanned — no non-`example.com` email, no home path
  (the two `/home/` hits are the repo's own `files/home/.local/` tree), no RFC 1918 address,
  no `.local` hostname, no key block, no token prefix.
- **Version bumps** — none owed: no `files/var/local/claude-yolo/**`, Dockerfile, entrypoint,
  patch script or deployed skill in the diff.
- **Doc drift** — one gap, folded into finding 3: `CLAUDE/QA.md:163-169` still describes the
  cross-check as one-directional and does not mention the `COVERAGE` line or the untracked
  direction at all.

## Mechanical gates

| gate | result |
| --- | --- |
| `./scripts/qa-all.bash` | **exit 0** — `✓ QA passed: 920 files checked`; `✓ helper-tests: Ran 1482 tests in 65 modules, 1 skipped`; `✓ helper-counts-reader: passed: 32`; stderr 0 bytes |
| `scripts/test-qa-helper-summary.bash` | rc 0, `passed: 32 failed: 0` (matches `PLAN.md`'s updated figure) |
| `python3 -m unittest tests.helpers.qa_environment.test_unittest_counts` | rc 0, `Ran 18 tests in 0.129s` / `OK` |
| `scripts/qa-helper-tests.bash` (no `--counts-file`) | rc 0, stdout 0 bytes, `COVERAGE: 65 of 65`, `Ran 1482 tests` / `OK (skipped=1)` |
| `scripts/qa-helper-tests.bash --counts-file` | rc 0, `tests=1482 skipped=1 modules=65`, token echoed back |
| `hooks-daemon plan-qa --sweep` | exit 1, **0 block / 2 advise** — `path-existence` on 00046 and `journal-freshness` naming 13 other plans. Both pre-existing, neither names 00125 |
| `shellcheck -x` (5 changed shell files) | clean |
| `ruff check` (2 Python files) | clean |
| `ansible-playbook --syntax-check` | **not triggered** — no playbook in the diff. `qa-all`'s repo-wide stage ran anyway: 82 playbooks OK |
| `qa-helper-tests.bash` as a conditional gate | **triggered** by the `helpers/` + `tests/helpers/` change — run, green, both ways |
| `check_extension_compat` / `extensions` ESLint | **not triggered** — no extension metadata or JS in the diff. Both ran inside `qa-all`: 5 extensions, 11 JS files |

### CI run `35055525017` at `0b1623fa`

Finished **failure**, and the expectation holds exactly: the only two `✗` lines are

```
✗ docs: 8 finding(s) across 71 files
✗ QA FAILED: 8 errors in 920 files
```

Cause A alone — Task 2.1, the user's open decision. The deliverable is still produced on a
runner rather than argued for:

```
local : ✓ helper-tests: Ran 1482 tests in 65 modules, 1 skipped
CI    : ✓ helper-tests: Ran 1482 tests in 65 modules, 2 skipped
```

Same tests, same modules, skip count alone diverging. `✓ helper-counts-reader: passed: 32`
green on the runner. 920 files both sides.

---

## Task 5.3 — not dischargeable on `0b1623fa`, and closer than last round

Findings 1 and 2's substance are closed and I reproduced all three of the lead's controls
rather than believing them; every one came out as described. The ten nits are all closed, and
two of them I re-derived rather than inherited (the `subTest` claim, the exit-2 control).

What is owed is smaller than last round and none of it is a design question:

1. **Surface the coverage through `qa-all.bash`** — finding 3. This is the one that matters:
   the defect round 7 named is still reproducible end to end on the mandated path, measured
   above, and `CLAUDE/QA.md:163-169` documents only the old half.
2. **One clause, four sites** — the sixth over-claim, with `CLAUDE/QA.md:140` as the model.
3. **"three" → "four", twice**, and re-seat the orphaned comment in `qa-discovery.bash`.

The honest summary of the shape this plan keeps hitting: round 8's finding 3 asked for the
coverage to be *reported* rather than implied, and it now is — into a stream the operator's
only sanctioned command throws away on success. The information was produced and not
delivered, which is one step short of the class this whole plan exists to remove.

---

# Addendum — three questions answered, and the tree moved again

## The tree moved

`HEAD` is still `0b1623fa`. Five files are now modified in the working tree:

```
 M CLAUDE/Plan/00125-…/FINDINGS.md
 M helpers/qa_environment/unittest_counts.py
 M scripts/lib/qa-helper-summary.bash
 M scripts/qa-all.bash
 M scripts/qa-helper-tests.bash
 M tests/helpers/qa_environment/test_unittest_counts.py
```

To be fair about the promise: it was *"I will not edit until your report lands"*, the report
landed, and the edits began after. That is the promise kept on its terms. But re-tasking the
reviewer afterwards re-opens the review, and this is now the **third consecutive round** in
which reviewer and implementer share one checkout. Round 7 suggested a worktree for one of
the two; it has not been done.

**Nothing in the report above is affected** — every measurement was taken against the clean
`0b1623fa`, and this addendum labels anything measured against the worktree.

The in-flight edits implement my finding 2 in all four sites and finding 3 end to end
(`tracked=` key, `--tracked-modules`, gate passes it, reader renders it). They are
**unreviewed**, and one of them is a defect — below.

## 1. Item 3 — agreed, and stderr-plus-failure-only is not defensible

**Does an operator or a cross-machine comparison ever see `m`? No.** Measured at `0b1623fa`:
the only `COVERAGE` string in a successful `qa-all.bash` run is `version-pins`', and CI run
`35055525017` is the same. `qa-all.bash` cats `$TMP_HELPER_ERR` only in the failure branch,
so on a green run the line is written and discarded.

One correction to your framing, because it matters for the fix: the stage line does **not**
carry `n`. `modules=` is `len(args.modules)` — *modules run*, i.e. `|discovered|`. In the
clean case run = n = m = 65 so the three coincide; under my control they diverged (run 66,
m 65). So the line carried "how many ran" and never "how many should have".

I cannot argue the stderr path is defensible, and here is why the steelman fails. *"A
developer adding tests runs the script directly and sees it"* — but `CLAUDE/QA.md:7,13`
forbid direct invocation, and more decisively the **consumer of `m` is not that developer**:
it is the two-machine comparison, which reads `verdicts.py`'s parse of `qa-all`'s **stdout**.
On the one path the number exists to serve, it was unreachable by construction.

The in-repo precedent is stronger than I put it in the report, and it is in this very file:
`qa-all.bash:125-131` and `:144-147` both carry *"a gate whose only visible output is a
failure is indistinguishable from a gate that is not running"* — written twice, for the
no-kill gate and the drift gate, and not applied to the third gate sixteen lines below. Same
shape as `qa-python.bash` inheriting `qa-bash.bash`'s discovery defect six lines away.

### The wording — keep yours, not mine

You asked for my wording; you have since written one, and **yours is better than the one I
was going to propose.** For the record, mine was:

```
Ran 1483 tests in 66 of 65 tracked modules (1 UNTRACKED), 1 skipped
```

Yours: `Ran 1483 tests in 66 modules (65 tracked), 1 skipped`. Keep yours, for three reasons
I did not weigh properly:

- **One shape, always.** Mine needs a conditional suffix that fires only in the rare case —
  a second rendering path that will almost never be exercised, which is a mild instance of
  this plan's own concern. Yours renders identically whatever the numbers are.
- **The cross-machine diff stays clean.** With a conditional parenthetical, two machines can
  differ by the *presence* of a clause rather than by a number, which is a noisier diff than
  `66 modules (65 tracked)` vs `65 modules (65 tracked)`.
- **`66 of 65` is ambiguous** about which number is the denominator; `(65 tracked)` labels it.

The one thing mine had that yours does not is that it *shouts* — `(1 UNTRACKED)` names the
anomaly where `(65 tracked)` requires a reader to notice two numbers differ. That is the
right trade: the names cannot go in the stage line anyway, and they are already on stderr
where a failing run shows them.

And to reaffirm what I said in the report: **do not make `modules != tracked` a failure.**
`qa-discovery.bash:200-202` keeps `find` as the discovery source deliberately so a
not-yet-`git add`ed test is still gated. Visibility was the gap; it is now closed.

### But `--tracked-modules` defaults, and only the fallback is tested

`helpers/qa_environment/unittest_counts.py:137` (worktree):

```python
tracked = args.tracked_modules if args.tracked_modules is not None else len(args.modules)
```

If `qa-helper-tests.bash` ever stops passing the flag — a rename, a refactor, an arg-order
slip — the runner silently synthesises `tracked == modules`, the stage line reads
`N modules (N tracked)` for ever, and the untracked signal is gone. **No test fails, no gate
fails, nothing anywhere notices.** A value that looks measured and is synthesised is worse
than an absent key, and this module's own docstring already states the rule: *"an absent key
and a zero must not look alike, because one means a clean run and the other means the reader
has gone blind."*

It is worse than a documented fallback, because of which side is covered. Measured over
`tests/helpers/qa_environment/test_unittest_counts.py` (worktree): every `main()`-level case
— `:151, 172, 186, 205, 243, 313, 358, 373` — asserts `modules=1\ntracked=1`, i.e. exercises
the **default**. `tracked != modules` is asserted only where `counts_text` is called directly
(`:97`). **No test drives `main()` with `--tracked-modules` supplied**, which is the only way
the gate ever calls it. The primary path is untested and the fallback is what the suite pins.

Fix, two lines:

- delete the default and make `--tracked-modules` **required** — a caller that forgets it
  then fails at argparse instead of being quietly answered;
- add one `main()`-level case passing `--tracked-modules 7` over one module and asserting
  `modules=1\ntracked=7`, so the wire the gate actually uses is exercised end to end.

Optional third: have the reader refuse `tracked > modules` as malformed. Past the missed
guard that is impossible, so it can only mean the gate and the runner disagree about the
population — which is the class of disagreement this library exists to catch.

## 2. The jq citation — two spans, and the spans are the wrong tool

The sentence has two verbs, so no single span covers it. At `0b1623fa`:

| what | where | why |
| --- | --- | --- |
| *"merge into one JSON document"* | **624-643** | `jq -s` on 624 is the merge (`-s` is what slurps; without it `.[0]`..`.[6]` address nothing); 643 is the seven operands and the redirect, which is what makes it *seven* |
| *"and are reported together"* | **645-655** | `# Final terse summary` and the single `✓ QA passed` / `✗ QA FAILED` line. **This is what the masking argument turns on** — seven stages, one verdict — and no span ending at 643 contains it |

So `610-627` is wrong at both ends, round 8's `608-627` is right about the merge and silent
about the report, and the correct answer is a pair: **624-643 and 645-655**.

Except it is not, because by the time you read this it has already moved. Measured:

| revision | `jq -s` | `# Final terse summary` |
| --- | --- | --- |
| `27d7b342` | 600 | 621 |
| `9b366751` | 608 | 629 |
| `0b1623fa` | 624 | 645 |
| worktree, right now | **626** | **647** |

Four values in four revisions of the same sentence's referent — and the fourth moved while
this review was running, from a two-line comment edit. Any number I give you is wrong on
commit.

## 3. Anchors — agreed, for the `qa-all.bash` citations only

The argument is this plan's own and it is correct: a stale line number **still resolves**, so
a wrong citation reads exactly like a verified one — a clean result indistinguishable from a
blind one. A stale anchor greps to nothing, which is a failure you can see. The table above
is the empirical case: three of my four values would have read as confident and been wrong.

**Scope: convert the three `qa-all.bash` citations, leave the other seven.** Round 8's audit
found every unstable citation pointing into `qa-all.bash` and every stable one pointing into
a file this plan does not touch. Converting the stable seven is churn that buys nothing.
Instead state the governing rule, so future citations are decided rather than guessed:
**no line-number citation into a file this plan edits.**

The three, all verified **unique** in `qa-all.bash` at both `0b1623fa` and in the worktree
(1 occurrence each): `jq -s`, `# Merge JSON from all checks`, `# Final terse summary`, and
`qa-deployed-drift.bash` for the other two.

Suggested replacement for `FINDINGS.md:355-357`:

> The seven accumulating stages are `bash`, `python`, `patterns`, `ansible`,
> `ansible-syntax`, `js` and `docs`. The `jq -s` invocation under
> `# Merge JSON from all checks` in `qa-all.bash` slurps their seven JSON files into one
> document, and the `# Final terse summary` block below it collapses the result into a
> single `✓ QA passed` / `✗ QA FAILED` line — which is why one red accumulating stage never
> stopped the other six being reported.

That says both verbs, and both anchors fail loudly if the code moves out from under them.

For `PLAN.md:164` and `FINDINGS.md:283`, `qa-all.bash:138` becomes *"the
`qa-deployed-drift.bash` invocation in `qa-all.bash`"* — which also reads better, because the
point was always *which gate aborts first*, not which line it is on.

## Still owed

1. `--tracked-modules`: drop the default, add the `main()`-level case.
2. The three citation conversions.
3. `qa-discovery.bash:207-208` — "three"/"third" → "four"/"fourth", and re-seat the orphaned
   `qa_tracked_shell_scripts` comment.
4. `CLAUDE/QA.md:163-169` still describes the cross-check as one-directional.

The in-flight finding-2 and finding-3 edits are unreviewed and should be a round's target,
not assumed good — including the new `tracked=` key's effect on the counts-file format
contract, which is now five keys with a sixth consumer.
