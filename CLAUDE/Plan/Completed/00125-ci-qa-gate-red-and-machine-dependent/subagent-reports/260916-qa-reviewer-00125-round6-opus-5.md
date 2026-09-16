# QA Review — Plan 00125 round 6, `d17c1e46` and `4ada2342` on `F44`

> **Provenance.** Written by the round-6 reviewer itself. As in round 5, the `Write` tool was
> unavailable to this session (tools: `Read`, `Bash`, `SendMessage` only), so this file was
> created with a quoted heredoc. Content is the reviewer's full long-form report, unabridged.
> This is the third consecutive round in which a reviewer could not persist its own output —
> see the nit below.

**Verdict**: FIX-BEFORE-MERGE

**Scope note.** The review was dispatched against `d17c1e46`. `4ada2342` landed mid-review and
changes the masking figures again; both commits are reviewed here. Tree clean,
`HEAD == origin/F44`, nothing unpushed.

## Blocking

None. Nothing here breaks another user, loses data, leaks private information, or violates a
HARD RULE. The two reader defects below are latent: `grep -rnE "^(Ran [0-9]+ tests?|OK( \(|$)|FAILED( \(|$))" tests/helpers helpers`
returns nothing outside this suite's own fixtures, so no live decoy exists in the tested
population today.

## Should fix

### 1. The new awk is defeated by two orderings that real captures produce

`scripts/lib/qa-helper-summary.bash:37-39`.

The rule — *the `Ran` line most recently seen when the result line arrives* — is a genuine
improvement on "last wins", and it is right for the two orderings the new fixtures model. It
is wrong for two more, both measured against real `python3 -m unittest` processes piped
exactly as `qa-all.bash:157` pipes them.

**(a) A stdout decoy that carries its own result line.** The second awk clause reassigns
`answer` on *every* `^(OK|FAILED)` match, so the effective rule is "the `Ran` line preceding
the LAST result line". A decoy printing both wins:

```
$ cat test_decoy_small.py          # test_a prints "Ran 3 tests in a scenario" then "OK (skipped=99)"
$ python3 -m unittest test_decoy_small 2>&1 | grep -nE '^(Ran|OK|FAILED)'
3:Ran 1 test in 0.000s
5:OK
6:Ran 3 tests in a scenario
7:OK (skipped=99)

helper_test_summary -> [Ran 3 tests]   (truth: Ran 1 test)
helper_skip_count   -> [99]            (truth: 0)
```

Note what that costs the commit's own thesis. `:31-33` says the two readers "depend on one
fact about the format instead of two" — but on this single capture `helper_skip_count` takes
the *first* result line and `helper_test_summary` takes the *last*. They anchor on the same
regex in opposite directions, and they disagree.

**(b) A stderr decoy interleaved between unittest's two writes.** unittest writes `Ran N tests`
and `OK` as two separate line-buffered stderr writes. Anything else writing to stderr in that
window lands between them, and the "most recently seen" rule picks it. A test that leaves a
thread running is enough — reproducible 5 runs out of 5:

```
real 'Ran' line at 780, 'OK' at 784  -> 3 line(s) interleaved BETWEEN unittest's two writes
780: Ran 1 test in 0.001s
781: Ran 3 tests in a scenario
782: Ran 3 tests in a scenario
783:
784: OK
run 1..5: helper_test_summary -> [Ran 3 tests]      (truth: Ran 1 test)
```

**Fix**: take the `Ran` line preceding the **first** result line, not the last — `answer`
should be assigned once (`if (answer == "" && seen != "")`), which closes (a). (b) is not
closable by a tie-break rule; the honest response is to stop claiming the rule is exhaustive
and say which decoy shapes it does and does not survive.

### 2. `helper_skip_count`'s "can never precede" is false, and its new fixture covers only half the population

`scripts/lib/qa-helper-summary.bash:54-55` — *"a STDOUT decoy is block-buffered to a pipe and
lands after the result line, so it **can never precede** it."* Measured, it does precede. The
block buffer is 8192 bytes; a test that prints past it flushes mid-run:

```
--- BIG decoy (>8KB of stdout before it), piped ---
2:Ran 3 tests in a scenario
3:OK (skipped=99)
6:Ran 1 test in 0.000s
8:OK
```

`print(flush=True)`, `python3 -u`, `PYTHONUNBUFFERED`, and a child process writing to the
inherited fd each do the same. "Never" is an absolute that one `print("X"*9000)` falsifies —
and it is load-bearing, because it is the justification offered for first-match.

Worse, the fixture added in this commit to cover result-shaped decoys
(`scripts/test-qa-helper-summary.bash:141-142`) tests the sub-case where the **real** result
line carries a count, and passes for that reason alone:

```
helper_skip_count 'Ran 5 ...\n\nOK (skipped=1)\nOK (skipped=99)'  -> 1    (fixture's shape, passes)
helper_skip_count 'Ran 1 ...\n\nOK\nOK (skipped=99)'              -> 99   (truth 0)
```

A run with **zero skips** prints a bare `OK`, bash `=~` finds no `skipped=` on it and scans on
into the decoy line. That is the majority shape — most suites skip nothing — and it is the
`AgentNotes` pattern verbatim: *the check covers the path the author had in mind, not the whole
population*.

**Fix**: match `skipped=` against the **first** result line only (`grep -m1`, or read the first
line of `$result`), and add the bare-`OK` fixture. Replace "can never precede" with the true
statement: a stdout decoy usually lands after the result line, and does not when the buffer
flushes first.

### 3. `FINDINGS.md:195` still says three where `:264` now says twenty

`4ada2342` upgraded *"Three gates in this plan had never run once in CI"* to *"20 of the 25 had
never run in CI even once"* at `:264` and left the older sentence standing at `:195`, seventy
lines above it in the same document and on the same subject. A reader meets three first. **20
is the verified figure** (derivation in the answers below); `:195`'s "three" describes
something different — the three that *failed* when unmasked — and should say so.

### 4. `helper_test_summary`'s docstring no longer describes the function

`scripts/lib/qa-helper-summary.bash:14-16` — *"or the word `passed` if unittest's **count
line** is absent"*. Since `d17c1e46` it also answers `passed` when the **result** line is
absent, because `answer` is only ever assigned inside the `^(OK|FAILED)` clause:

```
capture 'Ran 1464 tests in 0.42s' (no result line)
  before d17c1e46 -> [Ran 1464 tests in 0.42s]
  now             -> [passed]
```

An untested behaviour change: the fixture at `:179-180` covers result-line-without-`Ran`, and
nothing covers `Ran`-without-result-line. Production is safe — `qa-all.bash:173` calls
`helper_skip_count` next and it hard-fails on a missing result line — but the docstring now
understates where the function degrades, and the sibling readers disagree about whether that
capture is an error.

## Nits

- `scripts/lib/qa-helper-summary.bash:52` — *"a test writing to stderr is unbuffered"*. It is
  **line**-buffered: `sys.stderr.line_buffering` is `True` on Python 3.11.2 here. Immaterial to
  the outcome for whole lines, but this comment block exists specifically to state the
  buffering facts correctly.
- `FINDINGS.md:259` labels `497370ba` *"last red commit before the first fix"*. Literally true —
  `cedc9426` is the first fix (`failures=5` became `3` in its run) and `497370ba` is the last
  commit before it. But masking did **not** end there: `cedc9426` was itself still red at
  `helper-tests` (run `35034834651`), and the abort was not cleared until `390a9290`
  (run `35036018936`, where the abort moves to `panel-sections`). The count also first reaches
  25 at `2987caf6`, not at `497370ba`. `497370ba` holds the figure because it is the last commit
  to *touch* `qa-all.bash` before the fix — saying that would make the choice self-justifying
  and remove the misreading.
- The journal entry at `JOURNAL/00125-Journal-26-09-16.md` (02:48) says *"Masking continued
  until the first fix (`cedc9426`)"*. Masking continued **through** `cedc9426` and ended at
  `390a9290`. Journals are append-only, so nothing to change; noted so it is not cited forward.
- `CLAUDE/QA.md:22` still has round 5's awkward wrap (`A missing` ending the line).
- Third consecutive round in which the reviewer's `Write` tool was withheld and the report had
  to be shell-authored. Round 5 flagged it as a tooling gap and it was not acted on. That
  belongs in the plan or as its own issue, not in a fourth commit message.

## Answers to the five questions

**1 — Is the new awk correct for every ordering that can actually occur?** **No.** Two real
orderings defeat it; see finding 1. Taking the sub-questions in turn:

- **A FAILED result** — correct. `^(OK|FAILED)( \(|$)` matches `FAILED (failures=1)` via the
  paren branch and a bare `FAILED` via `$`. The new fixture at `:210-211` passes and I
  reproduced it.
- **A stderr decoy between unittest's two writes** — **not** correct. Reproducible 5/5 with a
  live process (finding 1b). It is not a formatting question and no tie-break rule fixes it.
- **A result line with no `Ran` line** — correct. `seen` stays `""`, `answer` is never
  assigned, awk's `END{print answer}` emits an empty line, and the `[[ =~ ]]` falls through to
  `printf 'passed'`. Verified: `helper_test_summary 'OK'` -> `[passed]`.
- **`set -euo pipefail` safety** — **safe**, and verified for all four paths: empty capture,
  no-match, match, and no-result-line. awk exits 0 in each, the command substitution succeeds,
  `rc=0`. If awk were missing, the assignment fails and `set -e` aborts, which is the right
  direction.

**2 — The masking figures, derived independently.** I re-derived every figure from
`git show <commit>:scripts/qa-all.bash` and from the CI logs themselves, without reference to
the plan's arithmetic. **At `4ada2342` all of them are right.**

| Claim | Verified |
| --- | --- |
| 5 masked at `9a79dd77` | yes |
| 11 at `b3f6e909` | yes |
| 25 at `497370ba` | yes |
| 26 behind `helper-tests` today | yes |
| 21 commits touched `qa-all.bash` in the window | yes |
| 20 of the 25 never ran in CI | yes |

On the two specific questions asked:

- **Is `9a79dd77` really the first commit where `helper-tests` was red?** Yes, with one
  nuance worth recording. Its own CI run (`34593573058`) was **cancelled**, so CI never
  completed a run at that SHA. But its parent `c44c56ce` (run `34592414281`) prints
  `✓ helper-tests: Ran 255 tests` and reaches the final summary with all five later gates
  green, and its child `201e115c` (run `34593675847`) prints `FAILED (failures=2)` and
  `✗ QA FAILED: helper unit tests`. `9a79dd77` is the commit that added
  `tests/helpers/displaylink_recovery/test_run_recovery.py`. The attribution is sound; the
  first run that *showed* it red was `201e115c`'s.
- **Is `497370ba` really the last before the fixes?** Not quite — see the nit above. But the
  **number** survives, and I checked this the way the question deserves rather than by trusting
  the endpoint: I computed the masked count at **every** commit that touched `qa-all.bash` in
  the window. It is strictly monotonic and never exceeds 25:

  ```
  9a79dd77=5  6bab5661=6  58529e4d=6  84abb932=7  acc16ed2=8  f5002614=9  d15b9984=10
  562351e1=11 7219fc06=12 72a3c923=13 ee501aaf=14 a60b9f9a=15 5d0afc88=16 89bbdc59=17
  55ad09ac=18 a32739ed=19 2e4329a7=20 17360c6f=21 5066569c=22 9b6c8d55=23 e7294705=24
  2987caf6=25    (497370ba and cedc9426 also read 25; neither touched qa-all.bash)
  ```

  So 25 is the maximum, not merely an endpoint reading. There is no earlier or later red commit
  with a higher figure, and the range is right.

**3 — Does "every gate added after a standing abort is born unexecuted" outrun its evidence?**
**No. It is supported, and it is now a count rather than a phrase.** Two things had to hold and
both do:

- *The abort stood continuously.* I pulled the logs of eleven completed runs spanning
  2026-09-11 to 2026-09-15 23:15 — `c44c56ce`, `201e115c`, `247ffa88`, `d4b46779`, `9cf465ef`,
  `37818e65`, `cb88ec4e`, `5767ab77`, `b3f6e909`, `9a4ab1f0`, `8af28800`, `f1645dde`,
  `9b6c8d55`, `497370ba`, `cedc9426`. Every one after `9a79dd77` aborts at
  `✗ QA FAILED: helper unit tests`. Not one got past it.
- *The 20 are genuinely new and genuinely never ran.* At `9a79dd77` the whole script declared
  only six hard gate names. The 20 added later (`ccy-token-mode`, `ccy-ssh-handling`,
  `ccy-selinux-verdict`, `ccy-gpu-device`, `ccy-host-hostname`, the four `vmtest-*`,
  `panel-sections`, `panel-contract`, the three `run-*`, `freezelib`, `lxcfreeze`, `podfreeze`,
  `host-health-login-snippet`, `vmtest-manifest`, `version-pins`) did not exist anywhere in the
  script beforehand, so none could have run ahead of the abort. I also checked the **other
  branches** rather than only `F44`, since the claim is about CI and CI ran elsewhere:
  `ccy-gpu-device-optional` (`34894567738`) and two `worktree-ccy-reboot-restore` runs
  (`35023882545`, `35028630201`) all abort at `helper unit tests` as well.

**4 — Is the coverage line for option (d) genuinely necessary, or would the bidirectional
inventory catch it?** **Necessary, and the inventory would not catch it. The reasoning at
`FINDINGS.md:89-96` is sound and not over-stated.**

The bidirectional inventory is `check_qa_gate_inventory_in` (`helpers/docs/link_check.py:165-202`).
It compares the **gates `qa-all.bash` invokes** against the **rows in `CLAUDE/QA.md`**. Its
subject is gates, not markdown files. A marker exclusion that silently widened would change
which `.md` files `in_scope` admits (`:226-249`) and would not touch either set the inventory
compares. It is structurally blind to this.

What exists already is the count: `main()` prints `"scanned": len(scoped)` and the stage line
reads `✓ docs: 71 files OK`. And `:370-377` refuses the **zero** case explicitly — *"found 0
in-scope markdown files — discovery is broken, not the tree"* — while saying nothing about the
**partial** case. That is the `AgentNotes` shape exactly, in the very file (d) would edit: a
guard against zero, none against 71 quietly becoming 63. The existing `scanned` number gives
the numerator; what `FINDINGS.md` asks for — how many the marker excluded — supplies the
denominator. Both halves are needed and the ask is the right size.

One thing the ask does not yet settle, and `FINDINGS.md` raised it in round 5 without
resolving: whether the exclusion is link-only or drops the file from `in_scope` wholesale. The
latter also removes anchor checking. Costless today (each of the 8 has one link and no
anchors), but it should be stated in Task 2.2's acceptance rather than decided at the keyboard.

**5 — Are the three corrected buffering comments true as written?** **Two of three are;
`helper_skip_count`'s new claim is false.**

| Claim | Site | Verdict |
| --- | --- | --- |
| unittest writes its summary to stderr | `:25` | true |
| a test's `print` goes to stdout, block-buffered to a pipe | `:25-26` | true |
| a decoy "appears AFTER the result line, **never** before it" | `:27` | **false** — flushes early past 8KB |
| "a test writing to stderr is **unbuffered**" | `:52` | imprecise — line-buffered |
| a stdout decoy "**can never precede**" the result line | `:54-55` | **false**, same reason |
| the same absolute restated in the fixture comments | `test-…:139`, `:195`, `:201` | **false** |

The measurement behind the commit was real; the generalisation from it is not. "Never" was
falsified by adding one `print("X" * 9000)` to the same probe — which is the same defect the
commit message describes catching in its own predecessor, one revision later.

## Checked and clean

- **Fail-fast** — no new `failed_when: false` / `ignore_errors: true`, no `|| true`, no
  skip-and-warn. `helper_skip_count` still hard-fails an unreadable capture rather than
  answering `0`, and `qa-all.bash:173-176` consumes that failure. The awk substitution aborts
  under `set -e` if awk is absent.
- **Stderr hygiene** — `helper_test_summary` puts only its payload on stdout;
  `helper_skip_count`'s diagnostic goes to stderr and the channel is asserted at
  `test-qa-helper-summary.bash:145-169`, not merely described.
- **Version bumps** — none owed. Across every file the whole plan has touched (`2ce0c7d5~1..HEAD`)
  there is no `files/var/local/claude-yolo/**`, no Dockerfile, entrypoint, patch script or
  deployed skill. The two commits under review touch only `scripts/lib/`, `scripts/` and this
  plan's markdown.
- **Public-repo safety** — scanned all 516 added lines across both commits for home paths,
  non-`example.com` emails, RFC 1918 addresses, `.local` hostnames, key blocks and token
  prefixes: no matches. The one `.local` hit is round 5's own report describing its scan.
  `scripts/test-secret-scan.bash`: `passed: 29 failed: 0`.
- **Plan Commit Rule** — tree clean, `HEAD == origin/F44`, 0 commits ahead. Journal
  append-only (107 added, 0 removed across both commits). `CLAUDE/Plan/README.md:37` index row
  present. Both commits carry plan state alongside the change they describe.
- **Placement in the IaC graph** — no new play, no new script, no new abstraction. Every change
  is an edit to the file that already owns the concern. Nothing to relocate, nothing misplaced.
- **Document sizes** — `PLAN.md` is 17,755 bytes, under the 18,000 advisory, and I found no
  place where accuracy was traded for it: every number in it is derived and correct, and the
  narrowing in `4ada2342` (5 -> 11 -> 25) is shorter *and* more accurate than what it replaced.
  `FINDINGS.md` is 18,041; `plan-qa --sweep` does not flag it.
- **Modes and lint** — git records `100644` for the library and `100755` for the test script.
  `shellcheck -x` clean on both.

## Mechanical gates

- **`./scripts/qa-all.bash`**: **PASS** — exit 0, `✓ QA passed: 918 files checked`,
  `helper-tests: Ran 1464 tests, 1 skipped`, `helper-summary-readers: passed: 23`,
  `docs: 71 files OK`, `version-pins: … COVERAGE: 9 of 9`.
- **`hooks-daemon plan-qa --sweep`**: exit 1, **0 block / 2 advise**, both pre-existing and
  unrelated to 00125 — `path-existence` on `00046-localhost-yml-leak-guard`, and
  `journal-freshness` naming 13 other plans.
- **`ansible-playbook --syntax-check`**: **not triggered** — neither commit touches a playbook.
  `qa-all`'s `ansible-syntax` stage ran repo-wide regardless: 82 playbooks OK.
- **`scripts/qa-helper-tests.bash`** standalone: **PASS** — `Ran 1464 tests … OK (skipped=1)`.
- **`scripts/test-qa-helper-summary.bash`** standalone: **PASS** — `passed: 23 failed: 0`.
- **`tests.helpers.qa_environment.test_verdicts`**: `Ran 40 tests … OK` — `PLAN.md:76`'s figure
  confirmed.
- **`helpers.gnome.check_extension_compat`** and **`extensions/` eslint**: **not triggered** —
  no `extensions/` metadata or extension JS in either commit. compat ran inside `qa-all`
  anyway: 5 extensions OK.

## Task 5.3 — not dischargeable

A straight answer, as asked. Findings 1, 2 and 3 are defects in the work this round was sent to
check, and findings 1 and 2 are in the file whose entire purpose is to stop this class of
defect. What stands between 5.3 and done:

1. **Finding 1** — the awk's two live counter-orderings (`qa-helper-summary.bash:37-39`), and
   the fixtures for them.
2. **Finding 2** — `:54-55`'s "can never precede", and `helper_skip_count`'s bare-`OK` blind
   spot with the fixture that exposes it.
3. **Finding 3** — `FINDINGS.md:195` three versus `:264` twenty.
4. **Finding 4** — `qa-helper-summary.bash:14-16`'s docstring, and a fixture for the
   `Ran`-without-result-line capture.

The nits (`:52` "unbuffered", the `497370ba` label, `CLAUDE/QA.md:22`) are not blockers.

**What is worth saying about the trend**, since five rounds have each found defects in the
previous round's fixes. The *numbers* have converged: at `4ada2342` every figure in `PLAN.md`
and `FINDINGS.md` is independently derivable and correct, including the range endpoint, which I
checked by computing all 21 intermediate values rather than the two named ones. That part is
done. What has **not** converged is the habit of stating a measured observation as a universal
law — "a pipe cannot produce that ordering", "can never precede", "the only ordering that is
real". Each of those was measured once, correctly, and then generalised without being attacked.
Round 5 diagnosed exactly this in round 4 and then committed two more instances of it in the
same file. The remaining fixes are small; the discipline that would have caught them is to try
to falsify each absolute before writing it down, which for finding 2 took one `print("X" * 9000)`.

Tasks 2.1, 2.2, 4.3 and 5.2 are open by design and nothing in this review changes that.
