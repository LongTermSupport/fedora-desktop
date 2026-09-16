# QA Review — Plan 00125 round 7, `27d7b342` on `F44`

> **READ THIS FIRST — the design reviewed here has been superseded.**
> The review target is `27d7b342`, whose `helper-tests` stage line is produced by **scraping
> `unittest`'s stderr** with a "last match wins" rule. That scrape is **known broken** and is
> being replaced. The reason is finding 1 below: a test that registers an `atexit` handler
> writing to `sys.stderr` puts result-shaped text *after* unittest's summary, so the
> precondition the whole design rests on — *"on stderr, unittest's summary is always last"* —
> is false, and both readers answer `Ran 3 tests` / `99` where the truth is `Ran 1 test` / `0`.
> The replacement takes the counts from the `TestResult` object and writes them to a counts
> file, parsing no stream a test can write to. **Nothing in this document endorses the scrape.**
> Part 2 reviews the replacement design instead.

> **Provenance.** Written by the round-7 reviewer. `Write` was withheld from this session
> (tools: `Read`, `Bash`, `SendMessage` only), so this file was authored with a quoted Bash
> heredoc. Fourth consecutive round in which a reviewer could not persist its own output.

**Verdict**: **FIX-BEFORE-MERGE**. **Task 5.3 is not dischargeable on this commit.**

**Scope note.** The working tree moved under this review — see the process finding. Every code
claim below was re-verified against `git show 27d7b342:<path>`, not the working copy.

---

# Part 1 — `27d7b342` as committed

## Blocking

None. Nothing here breaks another user, loses data, leaks private information, or violates a
HARD RULE in the diff.

## Should fix

### 1. "On stderr, unittest's summary is ALWAYS last" is false — confirmed, not re-derived

`scripts/lib/qa-helper-summary.bash:26-27`, restated at `:39`, `:54`, at
`scripts/qa-all.bash:159-163`, at `scripts/test-qa-helper-summary.bash:187-196`, in the commit
message, and in `JOURNAL/00125-Journal-26-09-16.md` (03:10).

The owner reached this independently; recorded here for completeness with the three mechanisms
beyond `atexit` that also do it. All measured against live `python3 -m unittest` processes with
stderr captured alone (`2>&1 1>/dev/null`):

| Mechanism | Lands after the summary |
| --- | --- |
| `atexit` handler | yes — `Ran 3 tests in 9.9s` / `OK (skipped=99)` |
| `__del__` at interpreter shutdown | yes — `OK (skipped=99)` |
| non-daemon thread outliving the summary | yes — both lines |
| orphan subprocess inheriting stderr | yes — `OK (skipped=99)` |

`warnings`, `logging`, `tearDownModule` and the unraisable hook could not be made to produce a
*result-shaped* line, so they are not part of the finding. Round 6's finding 2 —
`helper_skip_count` scanning past a bare `OK` into a later decoy — was relocated from stdout to
stderr, not closed:

```
helper_skip_count 'Ran 1 test in 0.0s\n\nOK\nOK (skipped=99)\n'  -> 99   (truth 0)
```

Reachability on this commit was checked rather than assumed:
`grep -rnE "^(Ran [0-9]+ tests?|OK( \(|$)|FAILED( \(|$))" tests/helpers helpers` returns nothing
outside this suite's own fixtures, and no `atexit`/`__del__` stderr writer exists in
`tests/helpers`. Latent, not live — which is why this is *should fix* and not *blocking*.

### 2. The two readers can still describe different runs on one stderr capture

`helper_test_summary` takes the last `^Ran [0-9]+ tests? in`; `helper_skip_count` takes the last
`^(OK|FAILED)`. The rule is symmetrical; the **anchor** is not. A `Ran`-only decoy after the
summary splits them — measured from a live `atexit` process: `summary = Ran 3 tests` (the
decoy's run), `skip = 0` (unittest's run). Fixture form against the committed blob:

```
capture: Ran 1 test in 0.0s / (blank) / OK (skipped=1) / Ran 9 tests in 9s
  summary=[Ran 9 tests]  skip=[1]
```

The new fixture *"the two readers agree on the same capture"*
(`test-qa-helper-summary.bash:211-213`) pins only the shape where the last `Ran` and the last
result line belong to the same run.

### 3. `qa-helper-tests.bash` guards the zero case and is blind to the partial one — and the fix already exists twice in the same directory

**This one survives the redesign.** The counts file fixes the *parse*; it says nothing about the
*population*.

`git show 27d7b342:scripts/qa-helper-tests.bash:24-29`:

```bash
mapfile -t test_files < <(find tests/helpers -type f -name 'test_*.py' | sort)

if [[ "${#test_files[@]}" -eq 0 ]]; then
    echo "ERROR: no helper tests found — expected tests/helpers/**/test_*.py" >&2
    exit 1
fi
```

`mapfile`'s exit status is its own; `find`'s and `sort`'s are discarded, and `pipefail` does not
reach inside a process substitution. Measured:

```
$ mapfile -t a < <(find /nonexistent-xyz /workspace/tests/helpers -name "test_*.py" 2>/dev/null); echo "rc=$? count=${#a[@]}"
rc=0 count=65
```

A partly-failed discovery yields a shorter module list, unittest runs it, the script exits 0,
and the stage line reports a smaller `Ran N tests` with no signal anywhere. An under-match is
silent, so it can never appear as a failure. The `-eq 0` guard refuses only the empty case.

This is the defect Plan 00076 fixed in `scripts/qa-bash.bash:54,67` (`qa_tracked_shell_scripts`,
`✗ bash: discovery missed N tracked shell script(s)`) and Plan 00081 fixed in
`scripts/qa-python.bash:95,108`. `scripts/qa-discovery.bash:202,336` already exports the
primitives. The third discovery site in the same directory never got the generalisation —
`AgentNotes.md`'s *"a lesson written down beside the thing it fixed, never generalised"*,
verbatim, and in the gate this plan has spent seven rounds on.

Not hypothetical: discovery is by `find`, not `git ls-files`, so an untracked
`tests/helpers/**/test_*.py` joins the population. During this review one did, and the reported
count moved 1464 → 1465 → 1479 with no change to the commit.

**Fix**: a `qa_tracked_helper_tests` cross-check alongside its two siblings, and a
`COVERAGE: n of m` line rather than coverage implied by the list's length.

### 4. `PLAN.md:176` still says "three" where `FINDINGS.md:196` now says "two"

The commit reconciled `FINDINGS.md`'s "three gates never ran" to the measured "20 of the 25",
and `:196-197` / `:270` now say clearing the abort *"surfaced real failures in **two** of
them — `panel-sections`, then `freezelib`"*. That figure is correct.

`PLAN.md:176` still reads *"**Demonstrated live three times while closing Phase 3** — each fix
revealed the next gate **that had never run once**"*. The journal's third member is `js`
(`JOURNAL/00125-Journal-26-09-16.md:94`), and `js` is one of the seven **accumulating** stages
(`FINDINGS.md:238`, `qa-all.bash:87`) — it ran on every CI run. The 23:39 journal entry records
it as a file-count disagreement (`✓ js: 10 files OK` local vs `8` in CI), not an unmasking. The
clause is false for one of its three.

Same shape as the drift this commit fixed: one document narrowed, its sibling left standing.

## Nits

- `qa-all.bash:163` — *"On failure both streams are shown, **in stream order**."* They are shown
  as the whole of stdout (`:166`) then the whole of stderr (`:167`), which is not chronological.
  Observed in the live failure. Say "stdout first, then the whole of stderr".
- The constraint that `qa-helper-tests.bash`'s progress line must stay on **stdout** is recorded
  only in `qa-all.bash:160` and the library header. `qa-helper-tests.bash:38`'s `echo` says
  nothing about it, and `CLAUDE/StderrHygiene.md` would ordinarily send progress to stderr. A
  future hygiene sweep moves it into the parse stream with no warning.
- `helper_skip_count:67-69` pipes `awk '/re/{answer=$0} END{if (answer != "") print answer}'`
  into `grep -E` with the same regex. The `grep` is a pure exit-status adapter and the
  `answer != ""` test is then redundant — two processes for one decision.
- `CLAUDE/QA.md:22` still carries round 5's awkward wrap (`A missing` ending the line). Round 6
  flagged it; unactioned.
- The new journal entry restates "On stderr unittest's summary is always last". Journals are
  append-only — nothing to change, noted so it is not cited forward.
- **Fourth consecutive round with `Write` withheld from the reviewer.** Rounds 5 and 6 both
  flagged it.

## Checked and clean

- **The trap (Q3) — clean, and the generalisation was checked too.** Exactly one `trap … EXIT`
  in `qa-all.bash:35`. It names all eight `mktemp` files (`:25-31`, `:34`). No trap in the
  sourced library. Armed before every early `exit 2` (`:43,52,61,71,80,89,106`) and every
  `exit 1`, so nothing leaks on any exit path. `/tmp/qa-results.json` is a documented output,
  not a leak. Repo-wide sweep for the same shape: `qa-bash.bash` (one real trap; the second hit
  is a comment), `files/home/.local/bin/vmtest:1403` (inside `cmd_freshness_status`, a separate
  dispatch from `cmd_run`'s `teardown_run` at `:1186` — no overlap),
  `files/home/.local/bin/ftp-camera:2196-2197` (different signal sets, cleared with
  `trap - EXIT INT TERM`). **No clobber anywhere.**
  **A cheap mechanical check**: a scope-blind `grep -c 'trap .* EXIT'` per file yields those 3
  candidates and 0 real ones — 100% false positives, so it is not gateable as written. A useful
  version must be scope-aware (two EXIT traps in the same function or at top level with no
  `trap - EXIT` between). Worth an advisory in `qa-bash.bash`; `shellcheck` does not flag it.
- **Q5, diagnostics — nothing lost, verified against a real failure.** The live `qa-all.bash`
  failure printed `$helper_out` and then the whole of `$TMP_HELPER_ERR` to stderr; the full
  `ImportError` traceback reached the operator intact. On the success path the stdout capture is
  discarded exactly as the old `2>&1` capture was — no regression.
- **Q2, contamination of the parse stream.** On paths that exit 0, `python3` is the only stderr
  writer: the `ERROR: no helper tests found` at `:27` exits 1, and a failed `cd` aborts under
  `set -e`. A `find` permission error would land in the stream but is not result-shaped (its
  real cost is finding 3). Tests in the live suite *do* write to stderr — `--there:`, `--here:`,
  `container-watch: DBus emit skipped`, `ResourceWarning` — none result-shaped.
- **Round 6 finding 4 resolved.** `helper_test_summary 'Ran 1464 tests in 0.42s'` →
  `Ran 1464 tests` again, so `:29-31`'s docstring describes the function once more.
- **Round 6's "unbuffered" nit resolved** — the claim is gone from `:54-56`.
- **Round 6's `497370ba` label resolved.** `git rev-parse cedc9426~1` =
  `497370ba5780150547c36d0588b8d268bcb19941`. The new label claims the high-water *value* and
  the position, neither of which overstates.
- **The CI divergence is genuinely produced, not asserted.** CI at `27d7b342` prints
  `✓ helper-tests: Ran 1464 tests, 2 skipped`; the tracked-only local run prints
  `Ran 1464 tests` / `OK (skipped=1)`. The counts come from **different tests**: locally (uid 0)
  `test_run_recovery.py:138` skips with *"running as root: chmod 000 does not block the read"*;
  a runner (non-root, virtual-only DRM) takes `:240` and `:254`. Exactly three conditional skip
  sites exist, matching `CLAUDE/QA.md:122`.
- **Fail-fast** — no new `|| true`, `failed_when`, `ignore_errors`, `2>/dev/null` or `set +e`.
  `helper_skip_count` hard-fails an unreadable capture and `qa-all.bash:183-186` consumes the
  failure; an empty stderr capture fails closed.
- **Version bumps** — none owed across `2ce0c7d5~1..HEAD`: no `files/var/local/claude-yolo/**`,
  Dockerfile, entrypoint, patch script or deployed skill.
- **Public-repo safety** — 477 added lines scanned: no non-`example.com` email, no home path, no
  RFC 1918 address, no `.local` hostname, no key block, no token prefix.
  `test-secret-scan.bash`: `passed: 29 failed: 0`.
- **Plan Commit Rule** — at the reviewed commit the tree was clean and level with `origin/F44`.
  Journal append-only (62 added, 0 removed). `CLAUDE/Plan/README.md:37` index row present.
- **Placement in the IaC graph** — no new play, no new script, no new abstraction; every change
  edits the file that already owns the concern.
- **Sizes** — `PLAN.md` 17,761 B (under the 18,000 advisory); `FINDINGS.md` 18,253 B, not
  flagged by the sweep.
- **Modes and lint** — `100644` library, `100755` for the three executables; `shellcheck -x`
  clean on all four. **Naming** — no jargon in the new identifiers.

## Mechanical gates

- **`./scripts/qa-all.bash`** — **RED, and not at the gate the dispatch predicted.** It aborts
  at `helper-tests` (`✗ QA FAILED: helper unit tests`) on an `ImportError` from
  `tests/helpers/qa_environment/test_unittest_counts.py:44` — an **untracked** file that
  appeared at 03:19–03:20 from a concurrent session, not from `27d7b342`. The abort stops **26**
  hard gates short. The `deployed-drift` gate expected to be red **skipped cleanly**:
  `✓ deployed-drift: skipped (CCY container — no deployed copies to compare)`. That expectation
  is wrong: the drift gate self-skips in a container and only blocks on an undeployed *host*.
- **Tracked-only reconstruction of HEAD** (64 test files, `git ls-files`): `Ran 1464 tests` /
  `OK (skipped=1)`, rc=0 — matching the commit message exactly, both readers correct.
- **`scripts/test-qa-helper-summary.bash`** — `passed: 23 failed: 0`.
- **`tests.helpers.qa_environment.test_verdicts`** — `Ran 40 tests in 0.007s` / `OK`.
- **`scripts/test-secret-scan.bash`** — `passed: 29 failed: 0`.
- **`hooks-daemon plan-qa --sweep`** — exit 1, **0 block / 2 advise**, both pre-existing and
  unrelated (`path-existence` on 00046; `journal-freshness` naming 13 other plans).
- **`ansible-playbook --syntax-check`** — not triggered, no playbook in the diff. `qa-all`'s
  repo-wide `ansible-syntax` stage ran anyway: 82 playbooks OK.
- **`check_extension_compat` / `extensions` ESLint** — not triggered. compat ran inside
  `qa-all`: 5 extensions OK.
- **CI run `35050838327` at `27d7b342`** — **failed, and the failure is Cause A alone**:
  `✗ docs: 8 finding(s) across 71 files`, every one
  `.claude/rules/*.md → ../hooks-daemon/CLAUDE/DirectoryRoles.md — target does not exist`, then
  `✗ QA FAILED: 8 errors in 918 files`. All 35 other stages pass, **including every one of the
  26 behind `helper-tests`** — independent confirmation that the unmasking worked.

---

# Part 2 — the replacement design (counts file from the `TestResult` object)

Reviewed as a design, from the owner's description plus measurement. Verdict on the design:
**sound in its core claim, with four specific holes to close before it ships.**

## Confirmed: the result object is authoritative, and it matches the text exactly

One run with every category present, comparing `TextTestRunner`'s own text against the object:

```
text  : Ran 7 tests in 0.000s
        FAILED (failures=1, errors=1, skipped=2, expected failures=1, unexpected successes=1)
object: testsRun=7  skipped=2  failures=1  errors=1
        expectedFailures=1  unexpectedSuccesses=1  wasSuccessful()=False
```

- **`result.testsRun` includes skips** — yes, confirmed. The premise holds.
- **`len(result.skipped)` equals the text's `skipped=`** — yes, exactly, **including a subTest
  skip**, which the text also counts. `expectedFailures` and `unexpectedSuccesses` are separate
  lists and do **not** contaminate either figure.
- **Use `result.wasSuccessful()` for the exit code, not `failures or errors`.** An unexpected
  success alone gives `FAILED (unexpected successes=1)` and `wasSuccessful()=False` while
  `failures` and `errors` are both empty. Getting this wrong turns a red suite green.

## Hole A — `testsRun` and `skipped` are not commensurable, so the stage line can read as nonsense

Measured: one test method with three subTest skips.

```
text  : Ran 1 test in 0.000s / OK (skipped=3)
object: testsRun=1  skipped=3
stage line would read: "Ran 1 test, 3 skipped"
```

`testsRun` counts test **methods**; `skipped` counts skip **events**, and a subTest skip adds an
entry without adding to `testsRun`. The counts file is faithful — unittest's own text says the
same — but `CLAUDE/QA.md:121-125`'s argument that *"the four machine shapes give four distinct
counts (0, 1, 2, 3)"* silently assumes the skip count is bounded by the test count. It is not.
Today there are no subTest skips in `tests/helpers`, so it is a documentation correction rather
than a live defect, but it belongs in the same commit as the redesign.

## Hole B — an unimportable module is reported as a test that ran

Measured against a module that does not exist:

```
text  : Ran 1 test in 0.000s / FAILED (errors=1)
object: testsRun=1  errors=1  wasSuccessful=False
```

unittest substitutes a `_FailedTest` placeholder, so a module that fails to import **inflates
`testsRun` by one** and is indistinguishable in the counts from a real test. This is not
hypothetical — it is exactly what happened in this very review (1464 → 1465). The counts file
cannot tell the operator that a machine ran 1464 real tests and one import failure while the
other ran 1465 real ones. **Record `len(result.errors)` and the module count in the counts file
too**, and have the stage line carry the module count; otherwise the redesign reproduces the
original blindness one level up.

## Hole C — the counts file is writable by every test in the suite, because the path is in `argv`

The design's claim is *"no stream a test can write to is parsed at all"*. True of streams. But
the **file** is not protected, and passing its path on argv hands it to every test in-process:

```
$ python3 -c '<test prints sys.argv>' --counts-file /path/to/COUNTS
    a test can read sys.argv: ['-c', '--counts-file', '/path/to/COUNTS']
```

This is a much smaller target than "any test that prints" — it is a genuine, large improvement —
but it is not the structural immunity the design claims, and there is a concrete accidental
path: **`tests/helpers/qa_environment/test_unittest_counts.py` is itself discovered and run by
the runner it tests.** A case that invokes the runner with a defaulted or mis-parameterised path
clobbers the production counts file from inside the production run.

Cheapest fixes, in order: have `qa-all.bash` generate a nonce, pass it in, and **require the
counts file to carry it back** (turns a clobber into a hard failure rather than a wrong number);
and/or pass an already-open write fd rather than a path, scrubbing it from `sys.argv` and the
environment before the suite loads.

## Hole D — a hard exit produces an absent-or-empty counts file and exit code 0

Measured:

```
test calls os._exit(0)
  -> atexit does NOT run, the runner never returns, nothing is written
  -> interpreter exit code: 0
```

`set -e` in `qa-helper-tests.bash` sees success. If `qa-all.bash` pre-creates the file with
`mktemp`, the reader meets a **zero-byte file that exists** — the Plan 00067 rclone shape
exactly, where existence is mistaken for "generated". **The reader must refuse absent, empty,
short or malformed content and fail the gate**, never degrade to `0`. That is the same rule
`helper_skip_count` already gets right today (`:62-64`) and it must survive the rewrite; it is
the one property of the current design that is worth carrying over verbatim.

## Also note

- Finding 3 above (partial discovery) is **not** addressed by the counts file. The counts
  describe the modules that ran; nothing asserts which modules should have.
- `helpers/` is stdlib-only by rule (`helpers/CLAUDE.md`) — the new runner must not import
  anything else. TDD ordering applies: the test file before the source file.
- If the new module's tests add a conditional skip, `CLAUDE/QA.md:122`'s "three conditional
  skips … counts (0, 1, 2, 3)" must be re-derived.

---

# Part 3 — the load-bearing numbers, so they can be corrected in the same commit

Two kinds of number live in these documents, and mixing them up is how rounds 4–6 each
generated drift while fixing drift.

## (a) Current-state numbers — these MOVE if the gate set changes

| Site | Figure | Today | Moves when |
| --- | --- | --- | --- |
| `CLAUDE/QA.md:19` | total gates | thirty-six | any gate added or removed |
| `CLAUDE/QA.md:19-20` | merged / separate | 7 / 29 | a hard gate is added → 7 / 30 |
| `CLAUDE/QA.md:21-22` | named lines | 8 names from the 7; **37 names for 36 gates** | any gate added → 38 / 37 |
| `CLAUDE/QA.md:33-77` | the gate table | one row per gate | **machine-enforced**, see below |
| `CLAUDE/QA.md:73` | the `test-qa-helper-summary.bash` row's text | "the readers behind this suite's own `helper-tests` line" | the file becomes a counts-file reader |
| `CLAUDE/QA.md:122` | conditional skips | three; counts 0,1,2,3 | a new conditional skip anywhere in `tests/helpers` — and see Hole A |
| `PLAN.md:60-64` | stages / hard gates | 7 / 29 | hard gate added |
| `PLAN.md:164` | `qa-all.bash:137` | correct **today, by luck** — see below | any edit to `qa-all.bash` above line 137 |
| `PLAN.md:164` | "stops **27** hard gates short" | 27 | a hard gate added after the drift gate → 28 |
| `PLAN.md:192-193` | "7 of the 36 … against 29" | 7 / 36 / 29 | hard gate added |
| `PLAN.md:201` | `test-qa-helper-summary.bash` "(23 cases)" | 23 | the suite is repurposed |
| `FINDINGS.md:191` | "**26** behind it today" | 26 | a hard gate added after `helper-tests` → 27 |
| `FINDINGS.md:214-221` | design table 7 / 29 / 7, and "7 + 29 = 36 … print **37**" | | hard gate added |
| `FINDINGS.md:223-226` | "`helper-tests` now owns **two**" aborts | two | a counts-file-unreadable abort makes it three |
| `FINDINGS.md:229-233` | the snippet's `# -> 29` and `# -> 26 behind it` | | hard gate added |
| `FINDINGS.md:238` | `qa-all.bash:137` | correct today, by luck | any edit above line 137 |
| `FINDINGS.md:305` | `qa-all.bash:613-620` | **already wrong** | — |
| `FINDINGS.md:273-278` | "seven stages … the other 29" | | hard gate added |

**The one that is machine-enforced.** `check_qa_gate_inventory_in`
(`helpers/docs/link_check.py:165-202`) derives the gate set from `qa-all.bash` and compares it
to the **rows** of `CLAUDE/QA.md`, **in both directions**. So renaming
`test-qa-helper-summary.bash` fails the docs gate **twice** (new name has no row; old row names
a gate nobody runs), and adding a gate fails it once. Everything else in the table above is
prose and fails silently.

**The two line-number citations, measured.** The drift gate's `if !` was at `qa-all.bash:134`
before this commit and is at `:137` after. `PLAN.md:164` and `FINDINGS.md:238` both said `:137`
*before* the commit, so they were wrong by three and this commit's `+3` shift made them right
**by coincidence**. The next edit above line 137 breaks both again. `FINDINGS.md:305`'s
`qa-all.bash:613-620` for the jq merge is wrong now and was wrong before: the merge is
**`600-619`** (it was `590-609`), and line `613` lands on `"patterns": .[2]` while `620` is
blank.

## (b) Run-pinned historical numbers — these must NOT move

Verified correct and correctly labelled; do not "update" them when the gate count changes.

- `PLAN.md:173`, `FINDINGS.md:257-267` — 5 / 11 / 25, 20 of the 25, 21 commits. Round 6
  re-derived all 21 intermediate values; monotonic, maximum 25.
- `PLAN.md:213-215` — run `35041998528` (`29ceee97`), "8 errors in 918 files", "the other **35**
  stages". **Verified**: at `29ceee97` the suite had 35 gates / 36 stage names, because the
  `helper-summary-readers` gate did not yet exist. 36 − docs = 35. Correct.
- `FINDINGS.md:247` — "`✗ docs` followed by **28** passing stages" for the same run.
  **Verified**: 28 hard gates followed docs at that commit. Correct.
- `PLAN.md:225` — `fa3cfe8e`, run `35040903213`, "36 stages each side, 37 of 38".
- `PLAN.md:211` — 918 files; still 918 in CI at `27d7b342`.

---

# Process finding

The working tree was mutated throughout this review by a concurrent session. Timeline:
`git status` clean at start → two untracked files at 03:19–03:20 → helper test count
1464 → 1465 → 1479 across three runs → by the end all four reviewed scripts were `M`, with
`scripts/lib/qa-helper-summary.bash` defining `helper_counts_summary` instead of the two
functions under review, and a new `$TMP_HELPER_COUNTS` in `qa-all.bash`.

Consequence: **no local `qa-all.bash` result from this round is attributable to `27d7b342`**,
and a reviewer and an implementer sharing one checkout will keep producing that. A worktree for
one of the two would remove it.

# Task 5.3 — not dischargeable

On `27d7b342`: findings 1 and 2 are the scrape, now superseded by the owner's own decision;
finding 3 survives the redesign untouched; finding 4 is live doc drift. Before 5.3 can
discharge, a round must review the **replacement** and come back clean, with Holes A–D closed
and finding 3 addressed.

**What is genuinely done, and worth saying plainly.** The stream separation in `27d7b342` was
the right call and it deleted a whole class rather than out-guessing it — every stdout attack
rounds 3–6 found is unreachable by construction and none could be resurrected. The trap is
correct on every exit path, and the rest of the repo has none of the same shape. The masking
figures are stable and two of them were re-verified here against the runs they are pinned to.
Round 6's findings 3 and 4 and two of its three nits are properly closed. The `1 skipped` /
`2 skipped` divergence is real and was verified to come from two *different* skip sites rather
than coincidence — that is the plan's central deliverable and it works. Abandoning the scrape
for the result object is the right call and one revision earlier than the previous four.

What has still not converged is the habit round 6 named: a measurement taken once, correctly,
and then written down as a law. "A pipe cannot produce that ordering", "can never precede",
"always last", and now "no stream a test can write to is parsed at all" — the fourth is nearly
true, and Hole C is the gap between nearly and actually.
