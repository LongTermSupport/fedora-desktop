# Plan 00125 — the two causes, and the evidence for each

Supporting document for [PLAN.md](PLAN.md). This holds the established facts and the
reasoning behind them; `PLAN.md` holds the task tree and current state, and `JOURNAL/`
holds the dated narrative of how each was arrived at.

Both causes were observed on run `35027739399` (`b81832cb`) and reproduced on
`34995414256` (`9b6c8d55`), so neither was new on the day the plan was filed.

## Cause A — the docs gate

Eight findings, all one shape. Eight tracked `.claude/rules/*.md` files link to
`../hooks-daemon/CLAUDE/DirectoryRoles.md`. `.claude/hooks-daemon/` is gitignored
(`.gitignore:53`, `.claude/.gitignore:3`), so in a clean checkout the target cannot exist.
`helpers/docs/link_check.py` already lists `.claude/hooks-daemon/` in `_EXCLUDE_PREFIX`,
but that excludes files **in** that tree from being scanned — it does not exempt links
**into** it from the existence check. The link is correct on an installed machine and
impossible in CI.

Those pointer files arrived in `0015c886` on 2026-08-31, five days *after* the last green
run, which was first read as meaning they cannot be the original breakage. **That
inference was wrong** (Task 1.2): CI only observes commits that are pushed, and nothing was
pushed to `F44` in those five days. `0015c886` is the *very next run* after `1fc1c5fe`, and
it failed on **docs alone** — 7 findings, all `target does not exist`, with all 203 helper
tests passing. Cause A is the original breakage and for eleven days it was the only one.

This is the one cause still open, because fixing it is Task 2.1's decision, not a repair.

### Task 2.1: two of the three recorded options are dead, and a fourth is alive

The three options were recorded as a genuine three-way choice. Checking them killed two —
and a round-4 review then found a fourth I had not considered, after I had already told the
owner only one remained. Both halves are below, because narrowing a decision too far is the
same error as leaving it too wide.

**The eight findings are all daemon-generated content, and the correspondence is exact.**
Eight of the fifteen tracked `.claude/rules/*.md` files carry
`<!-- hooks-daemon-rule-version: 1.0.0 -->`, and they are the same eight the gate reports.
Their source is the daemon's own installer,
`.claude/hooks-daemon/src/claude_code_hooks_daemon/install/directory_role_rules.py`, which
renders the link from config via `directory_roles_link()` and re-deploys the files through
`sync_directory_role_rules()`. Nothing this repository hand-wrote contributes a single
finding.

- **(c) is not durable, for a stronger reason than "it would regress".** The installer
  re-renders the link on every sync, so an edit is reverted at the next upgrade — and the
  daemon's `docs_qa` `rules-file-shape` check *enforces* the pointer-only contract, so the
  edit would also be fighting a check while it lasted. The daemon's module docstring
  records the design decision deliberately: `DirectoryRoles.md` is not seeded into the
  client tree because a normal client install "clones the WHOLE daemon repository into
  `.claude/hooks-daemon/`", so the target exists "at a fixed, predictable path the moment
  the daemon itself is installed". The link is not wrong. Its premise — *the daemon is
  installed* — is simply false in CI.

- **(b) is the shape `CLAUDE.md` prohibits, and it is prohibited by name.** "Missing
  Dependencies — Fail Fast, Fix in IaC" rules out exactly "make the script tolerate the
  missing tool (skip-if-absent, `|| true`, advisory-only mode)", and a link check that
  stops checking when the tree is absent is that, one level of indirection away. It would
  also pass on a genuinely broken link in the only environment that cannot tell.

**(a)** is therefore live: the daemon is a real dependency of the docs graph and CI does not
have it. `.github/workflows/` contains no reference to the daemon today, so this is an
addition rather than a repair. It is the owner's call, because it makes every QA run depend
on an external repository's installer — a cost the rules do not decide.

### (d) — and why "only (a) remains" was wrong

I told the owner (a) was the sole admissible option. It is not. **Exclude daemon-GENERATED
files from the link check**, keyed on the `hooks-daemon-rule-version` marker that identifies
exactly the 8 offenders; the 7 rule files this repo authored stay checked.

The objection I would have raised is that this is (b) wearing a hat. It is not, and the
distinction is the one `CLAUDE.md`'s rule actually turns on:

|                            | (b) conditional on the tree being present | (d) exclude daemon-generated files                    |
| -------------------------- | ----------------------------------------- | ----------------------------------------------------- |
| Depends on the environment | **yes** — checks here, skips in CI        | no — same everywhere                                  |
| Can pass a broken link     | yes, exactly where it cannot tell         | yes — but only in files we neither author nor can fix |
| Needs a network            | no                                        | no                                                    |

(b) is "check when convenient". (d) is a **scope** decision: this repository does not author
these files, cannot fix their links, and would have its edits re-rendered by
`sync_directory_role_rules()` if it tried. And it is not a new kind of judgement —
`link_check.py:214-223` already excludes `.claude/hooks-daemon/`, `.claude/ccy/`,
`.claude/skills/` and `.claude/agents/` on precisely that reasoning. The 8 rule files are
daemon-owned content that happens to be deployed outside the daemon's own directory; the
exclusion follows ownership rather than path.

**The mechanism is not the same shape, though, and that is (d)'s one real cost.**
`_EXCLUDE_PREFIX` excludes by PATH: the set it covers is visible by reading the tuple, and a
file cannot drift into it without moving. A marker-based exclusion is decided by file
CONTENT, so the excluded set is invisible until something counts it — and it would widen
silently the day any other file grew that marker. So (d) is admissible **with a coverage
line**: report how many files the marker excluded, the way `version-pins` reports
`COVERAGE: 9 of 9`. Without one it is a skip nobody can see, which is the family (b) belongs
to even though (d) does not.

So the real question for the owner is not which option is admissible but **which
relationship is true**: is the daemon a dependency this repo's CI should install (a), or is
its generated output simply not ours to audit (d)? Both are defensible. (a) costs a network
fetch per run; (d) means a genuinely broken daemon-authored link would go unreported here —
though the daemon's own `docs_qa` already checks those files.

**That cost, measured rather than asserted.** Two things were claimed to the owner without
checking, so both were checked:

- **Would it actually fix the gate?** Yes. The link target resolves:
  `.claude/rules/../hooks-daemon/CLAUDE/DirectoryRoles.md` normalises to a file that exists
  on an installed machine. Nothing else about the eight findings needs to change.
- **How heavy is it?** The installed tree here is 452M, but **336M of that is the daemon's
  own `untracked/` runtime state**, which a fresh clone does not carry. The repository is
  ~49M of history plus a working tree of similar order, and the `.venv` (16M) is built
  locally. So the honest figure for CI is *a shallow clone of one public GitHub repository*,
  not half a gigabyte.

Worth stating plainly for the decision: CI needs **one 12KB file** — `DirectoryRoles.md` —
and option (a) as originally phrased fetches a whole repository to get it. `--depth 1` is
the sane form. The dependency on an external repo's availability is real either way, and
that, not the byte count, is what the owner is actually weighing.

### The decision: not (a), and the owner's framing beat (d)

**Not (a).** The daemon is not installed in CI — "maybe later, but only if we decide it's
needed". The rule adopted instead is broader than the option as recorded: *we do not QA
another repo's files*, covering the daemon, the vendored Ansible roles, and whatever gets
vendored next.

That framing is **better than (d)**, and it removes (d)'s one real cost. (d) excluded whole
FILES by a content marker, so the excluded set was invisible and could widen silently the day
any other file grew that marker. The boundary excludes by resolved **target**: a
daemon-generated file's own *other* links are still checked, and the decision is by path, so
nothing can drift into it by having something written into it.

**Three outcomes, not two, and the third is what makes the second safe.**

| The target is…                    | Verdict                                                 |
| --------------------------------- | ------------------------------------------------------- |
| tracked by this repository        | checked as before; missing is a **failure**             |
| inside a declared vendored repo   | **warned on, never failed** — three outcomes below      |
| untracked, and vendored by nobody | **failure** — a link to something no clean checkout has |

The third row asks **trackedness, not ignoredness**, and the swap moved files in both
directions: a tracked file matching an ignore rule left that row, and a present, unignored,
never-committed file entered it. That second one is the finding — green here, red in a clean
checkout — and the earlier wording of this table described the question the swap replaced.

And a vendored target is still LOOKED AT, in the one case where looking means something —
the owner's refinement, which recovers exactly what a flat exemption was throwing away.
The three outcomes (`verified` / `unverifiable` / `broken`) are tabulated in
[`CLAUDE/QA.md`](../../QA.md), which owns the gate reference; what belongs here is why.

`broken` is the case a flat "not followed" discarded, and it is the useful one: the repo is
right there, so "cannot say" is false — the link is demonstrably wrong, which usually means
that repository moved the file and our generated pointers are stale. It warns rather than
fails because it is still not ours to fix, and because **the exit code must not depend on
what is installed**. What the gate SAYS may, and should: a machine that can see the repo can
say more about it, and saying more never flips a verdict. That distinction — verdict
machine-independent, detail machine-dependent — is the one the original defect blurred, and
it is worth stating because "make the gate machine-independent" taken literally would have
thrown the `broken` case away too.

Stopping at "ignored, so skip it" was the version I was about to ship, and it would have
quietly exempted a link into `untracked/` as well — trading a false failure for a silent
skip, which is this repository's recurring defect appearing inside the fix for an instance of
it. The owner caught it.

**The question asked is trackedness — and the first version asked something narrower.** It
asked `git check-ignore`, which is machine-independent and was chosen for exactly that, but
it answers "does `.gitignore` name this path", while the finding it raised said "target is
not tracked by this repository". Those come apart for a file sitting on one disk that was
never `git add`ed and matches no ignore rule: not ignored, so it passed there, and absent in
CI, so it failed. **Cause A's own shape, inside the classification built to remove it** —
round 10 reproduced it in a throwaway repo. Zero such links exist today, which is why it
survived a round.

So the check now asks `git ls-files`, plus the directories those files imply, since a link
to `docs/` is a link to something this repo plainly owns. The index ships in every clean
checkout, so the property `check-ignore` was picked for is kept — and the finding's wording
is finally what the code tests.

Existence is checked FIRST and trackedness second. A typo'd link is both absent and
untracked, and `target does not exist` is the message that helps; a present-but-untracked
target fails here and fails in CI for the other reason. Same exit code, different sentence —
which is the distinction the whole gate turns on, applied to itself.

**Declared, not detected — and here is the honest limit.** In CI the vendored tree is simply
absent, so nothing on disk distinguishes it from a typo; the roots live in `_VENDORED_ROOTS`.
They are declared as PARENTS (`roles/vendor/`, not each role), so vendoring under an existing
root needs no code change. A first attempt made the declaration self-maintaining by walking
the tree for undeclared `.git` directories; it was written, it reported **eleven**, and every
one was a gitignored acceptance-run fixture under a completed plan. Every vendored repo is
gitignored, so a sweep either skips all of them or drags in every stray clone — the
population it can see is not the population that matters. What replaced it costs nothing and
only answers for a target that is already a finding: walk UP from that target, and if a
repository is nested there, say so in the finding. The instruction lands on the machine that
can see the repo, which is the machine the fix is made on.

**And the limit is worth stating plainly, because a comment here once hid it.** Nothing
checks that a declared root really is a vendored repository — adding one exempts every link
under it, and only review stops that. The comment used to claim otherwise, citing the
`check_vendored_declaration` that had been written, measured and deleted, leaving the
citation behind: a claim printed where a measurement belongs, in the paragraph answering the
obvious objection to a declared exemption list. What IS checked is the other direction — a
repository nobody declared gets its links reported.

Two shapes the declaration nearly missed, both caught by round 10. The roots carry a trailing
slash and the test was `startswith`, so a link to the ROOT ITSELF (`../hooks-daemon`) matched
no root — and missed the ignore branch too, because a directory-only rule cannot classify a
path git has never seen. "Parents, not leaves" did not cover the parent. The slash stays,
because dropping it would exempt `roles/vendor-extra/`; the test is now equal-or-under.

**Verified against a tree with no daemon**, which is the CI condition: same checker, **0
findings, 8 vendored links**. With the declaration removed in memory the same 8 come back as
findings, each naming `.claude/hooks-daemon` and pointing at `_VENDORED_ROOTS`; restored,
they go away again; and a typo in our own tree is still `target does not exist`. The
exemption is doing work, and it is not blindness.

## The counts are not in the text

The `helper-tests` stage line has to tell a clean machine from a blind one, because
`unittest` counts a SKIPPED test inside `testsRun` and `Ran 1464 tests` is byte-identical
either way. Four readers were written to scrape that count out of the run's captured
output. Every one of them was verified by hand, landed, and was then defeated:

| Reader                                 | Defeated by                                                                                      | Wrong answer         |
| -------------------------------------- | ------------------------------------------------------------------------------------------------ | -------------------- |
| `\(skipped=[0-9]+\)`                   | `OK (skipped=1, expected failures=1)` — unittest appends more inside the same bracket            | `0` for a real skip  |
| unscoped match over the whole capture  | bash `=~` takes the FIRST hit anywhere; this repo's own fixtures contain `skipped=41`            | the fixture's number |
| last match wins, merged `2>&1` capture | Python block-buffers stdout to a pipe at 8KB: a padded decoy flushes EARLY, an unpadded one LATE | `99` where truth `0` |
| last match wins, stderr alone          | an `atexit` handler printing to stderr runs AFTER unittest's summary                             | `99` where truth `0` |

The third and fourth are the instructive pair. Having measured that a small decoy lands
after the summary, "last match wins" looked forced; the padding case shows the ordering is
a function of how much the test printed, so **no first-or-last rule over a merged stream
can be correct**. Separating the streams then looked like a structural fix rather than a
fifth guess — but it rests on "unittest's summary is always last on stderr", and that is
simply false. Reproduced on the first attempt:

```
Ran 1 test in 0.000s      <- unittest's real summary
OK
Ran 3 tests in 9.999s     <- the atexit handler, after it
OK (skipped=99)
```

A test can write anything, to either stream, in any order. The text is therefore not a
source of truth for this, and no amount of match-rule cleverness changes that. The numbers
exist exactly once, in unittest's `TestResult` object, and that is what
`helpers/qa_environment/unittest_counts.py` reads — writing them to a file whose path the
caller supplies, so the payload never shares a channel with anything a test can reach.

Two secondary properties came free. The previous pair of readers had drifted into opposite
match rules and disagreed with each other about the same run; there is now one reader, so
they cannot. And a malformed counts file **fails** rather than reporting zero, because
"the reader went blind" and "the machine skipped nothing" must never print the same line —
which is the defect the whole exercise exists to remove, and the one every revision found
a new way to reintroduce.

**The same shape lived in 23 more places (Task 4.5).** 21 hard gates in `qa-all.bash` read
their case count with an unscoped `grep -oE 'passed: [0-9]+'`: `-o` prints every match, so a
second occurrence made the stage line two lines and `verdicts.py` read the first as the stage
and lost the rest.

**It took three sweeps, and the first two were both short — in the same way, for the same
reason.** Sweep one said "every other hard gate": it was 21 of the 29 non-merged gates, and
**2** of the remaining 8 had the same defect with a *different regex*. Sweep two, correcting
that, swept for the `||` fallback — and missed **2 more** that had no fallback at all because
they interpolated the child's whole capture directly. Sweep three enumerated all 29 and
classified how each derives its stage line, which is the only version that could have been
right:

| How the stage line is derived            | Gates  | Status                                   |
| ---------------------------------------- | ------ | ---------------------------------------- |
| `qa_gate_case_count`                     | 21     | fixed, sweep one                         |
| `qa_gate_detail`                         | 6      | 2 in sweep two, 4 in sweep three         |
| `helper_counts_summary` (from a file)    | 1      | Task 4.4                                 |
| composes its own line (`deployed-drift`) | 1      | never had the defect                     |
| **total**                                | **29** | 28 composed stage lines + deployed-drift |

The first version of that table was wrong three ways, and round 10 caught all three: it said
`qa_gate_detail` covered 4 gates when the code says 6 in two other places, it summed to 27
under a sentence claiming 29, and its last row was not a row at all — an unescaped `|` inside
`` `|| summary="OK"` `` broke the cell split, so the two gates it meant were invisible AND
already counted in the `qa_gate_detail` bucket. **An enumeration table that did not
enumerate, offered as the proof that the population had finally been enumerated.** Worth
leaving on the page: the numbers in the code were right every time, and the summary of them
was wrong every time, which is the whole argument for deriving a count rather than writing
one down.

Each sweep generalised exactly as far as the text it had been reading — for the pattern
string, then for the fallback operator — rather than for the defect, which is *a stage line
derived from an unscoped read of the capture*. `CLAUDE/AgentNotes.md` names the habit. The
lesson that transfers is not "sweep harder": it is that a sweep over a population nobody
enumerated is a measurement of what you happened to look at.

**`vmtest-manifest` was the one still emitting the defect on every run.** Three unconditional
`echo`s on its success path, interpolated whole, made a THREE-line stage line; `verdicts.py`
kept the first and dropped two coverage measurements. It is now three scoped reads joined
into one line — three rather than one chosen line, because each is a different measurement
and picking one would discard two on purpose where the old code discarded them by accident.

The worse of the two had never worked. `nokill-containerwatch` read
`[0-9]+ call site[s]? checked` from a gate whose single commit has only ever printed
`N container-watch file(s) clean` — **zero matches in its entire life**, with a `||` fallback
substituting the sentence `no forbidden kill call sites` on every run. A claim nothing had
verified, printed where a measurement belongs, six lines under a comment complaining that a
rule had been "written down beside the drift gate and never applied to this one". Both now use
`qa_gate_detail`, whose fallback is the deliberately useless `summary unreadable`: a fallback
that asserts something is worse than none, because it is indistinguishable from an answer. Round 4 found this in the helper-tests reader and it
was fixed there alone; the 21 copies were never looked at, and none was tested.

They now share `qa_gate_case_count`, scoped to the last matching LINE. A line and not a
match, because the 21 do not agree on a format and no anchor covers all of them:

| Shape                             | Example                                      |
| --------------------------------- | -------------------------------------------- |
| one space before `failed:`        | `passed: 29 failed: 0`                       |
| two spaces                        | `passed: 15  failed: 0`                      |
| three spaces                      | `passed: 187   failed: 0`                    |
| no `failed:` at all               | `passed: 20`                                 |
| prefixed with the gate's own name | `ccy selinux-verdict: passed: 14  failed: 0` |

It degrades to the word `passed` rather than to a number for the usual reason — a wrong count
reads as a measurement — though that path is DEFENSIVE: all 21 callers emit a count, and only
this library's own tests reach it. (`planlib-tests` prints `PASSED (library version 1.2.0)`
and was once cited as the live caller; it is not a caller at all, it has its own reader.)
**"A pure refactor: every stage line byte-identical" was written here and in `PLAN.md`, and
it is retracted.** What is true, measured: 21 of 21 case-count lines are byte-identical, old
expression against new, on the same captures. What was false is the scope of the claim —
three other stage lines changed:

```
nokill-containerwatch   no forbidden kill call sites  ->  3 container-watch file(s) clean
vmtest-manifest         (three lines)                 ->  one line, all three measurements
extension-compat        …Fedora 44 ships.             ->  …Fedora 44 ships
```

The first was described three clauses earlier in the same bullet that called the change pure.
The measurement was taken correctly and then carried forward across a population that had
moved — this plan's meta-defect, in the commit retracting two other instances of it.

**And the retraction over-claimed in its turn**, which round 10 caught: it said all three
changed *because they were broken*. Two were. `extension-compat`'s old reader matched exactly
one line of a seven-line capture, its `||` fallback never fired, and the change cost it a
full stop — not the shape `nokill` (zero matches, ever) or `vmtest-manifest` (a three-line
stage line, every run) were in. The count of three is right and the block above states each
change exactly; only the word generalising across them was wrong. Round 9's finding 2 was
that a retraction contained a fresh over-claim, and so did this one — which is the argument
for stating what each case IS rather than what the set has in common.

**A pattern and a gate that nothing compares will drift, and the drift is silent.** That is
what `nokill` was, for its whole life. So `test-qa-helper-summary.bash` now extracts every
`qa_gate_detail` pattern out of the real `qa-all.bash` and runs it against the real gate —
neither side a fixture. Re-introducing the dead `[0-9]+ call site[s]? checked` pattern fails
it, so the suite would have caught the defect the day it landed. A call site whose capture
variable is not registered there fails too, which is what stops the next pattern being added
uncoupled.

### What the token does and does not do

Removing the scrape closed the stream, and the first draft then over-claimed about the file
that replaced it. The claim was *"a value only this run knows"*, in four places. It is
false: the token travels in the same `argv` as the path, so anything that finds the file **by
reading argv** already has the token. Note the qualifier — without it the sentence says the
token buys nothing, and contradicts the row below it: a hardcoded path finds the file and does
not have the token, which is exactly the case the token catches. Stated correctly, three
separate things are going on.

| Threat                                                | What stops it                            | Detected?         |
| ----------------------------------------------------- | ---------------------------------------- | ----------------- |
| A test writes the counts file DURING the run          | the runner's write lands after the suite | n/a — overwritten |
| A stale file, a concurrent run, a hardcoded path      | the token                                | yes               |
| A write landing AFTER the runner's (`atexit`, thread) | nothing                                  | **no**            |

So what actually defeats the accident the comments cited as motivation — this module's own
tests being collected by the runner they test — is **write ordering**, which no comment
mentioned. The token covers a real but different case. The third row is the honest gap.

This is the fifth time in this plan a measurement has been written down as a law: *"a pipe
cannot produce that ordering"*, *"can never precede"*, *"always last"*, *"no stream a test
can write to is parsed"*, and now *"only this run knows"*. Four were caught by review. The
pattern is not carelessness about the measurement — every one of those was measured
correctly — it is generalising from the case measured to the population, in a sentence
written immediately after the measurement, when the difference is least visible.

### The stage line and the verdict stream are the same channel

Removing the scrape also removed the capture around `qa-helper-tests.bash`, leaving its
stdout inherited — so it flowed into `qa-all.bash`'s own stdout, which is the stream
`verdicts.py` parses for `^[✓✗⚠] name: ` stage lines. Of the 29 hard-gate invocations, 28
capture their child's output and that one did not. Measured as reachable: a test doing

```python
print("✓ helper-tests: Ran 1 test in 1 module, 0 skipped")
```

puts a forged stage line into the machine-read verdict stream of the suite whose subject is
verdict lines that cannot be trusted.

It is now captured and **required to be empty**, which is the difference between a
precondition and a gate. Across a suite of that size "no test prints to stdout" held by
accident, and one `print()` would have ended it silently.

The count that used to sit in this sentence is gone, and deliberately. It had rotted by the
time round 11 read it — this was the surviving copy of the very sentence `CLAUDE/QA.md`
deleted for rotting twice, which makes it the third rot of one claim. The live number is in
the `helper-tests` stage line on every run, which is the only place a number that must be
re-measured to stay true belongs.

### No line-number citation into a file this plan edits

`jq -s` has sat at line 600, 608, 624, 626 and 609 across this plan's revisions — one of
those moves happened *while a reviewer was citing it*, from a two-line comment edit. A stale
line number still RESOLVES, so a wrong citation reads exactly like a verified one; a failed
`grep` for anchor text is a failure you can see. The three citations into `qa-all.bash` now
use unique anchors (`# Merge JSON from all checks`, `jq -s`, `# Final terse summary`,
`qa-deployed-drift.bash` — each verified to match exactly once). The seven citations into
files this plan does not touch keep their line numbers, because they have not moved and
churning them buys nothing.

## The two machines disagree on three stages, and each is declared

Measured by `triage.bash` at the same commit on both machines with a clean tree
(`fa3cfe8e`, CI run `35040903213`): 36 stages each side, 37 of 38 symbol-prefixed lines
accounted for.

| Stage            | Local         | CI              | Declared where                              |
| ---------------- | ------------- | --------------- | ------------------------------------------- |
| `docs`           | 71 files OK   | 8 findings      | Cause A above — the one open decision (2.1) |
| `deployed-drift` | skipped (CCY) | skipped (clean) | prints its own reason on each machine       |
| `helper-tests`   | 1 skipped     | 2 skipped       | `CLAUDE/QA.md`, machine-dependence table    |

`js`, `bash`, `patterns` and `python` now agree exactly, which three of them did not
before this plan. The `helper-tests` row is the deliverable rather than a residue: the two
machines skip *different* tests, and until the skip count joined the line the two sides
were byte-identical and read as `agree`.

### After the boundary landed: `docs` agrees on the verdict and differs only in what it says

That table is the state at `fa3cfe8e`. The `docs` row is now closed, and the way it closed
is the point. Same commit, CI run `35081847136`:

```
CI:    ✓ docs: 71 files OK (…) — VENDORED: 0 verified, 8 unverifiable (repo absent), 0 broken
local: ✓ docs: 71 files OK (…) — VENDORED: 8 verified, 0 unverifiable (repo absent), 0 broken
```

Mirror images, and the same `✓`. The gate's **exit code** no longer depends on what is
installed; its **sentence** does, and reports exactly what the machine could and could not
check. A CI run saying `0 verified, 8 unverifiable` is not claiming those eight links are
fine — it is saying it has no way to know, which is the honest answer and the one that was
missing for three weeks.

`helper-tests` stays in the table on purpose: `2 skipped` in CI against `1 skipped` locally,
still declared, still the deliverable. This satisfies the plan's fourth success criterion —
the docs gate passing in a checkout with no hooks daemon installed.

## Cause B — tests that read the machine they were written on

Five at first; a sixth surfaced once the abort stopped hiding it.

| Test                                                                                                                                      | Status       |
| ----------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| `tests/helpers/displaylink_recovery/test_run_recovery.py::TestEdidByteCountAgainstRealSysfs::test_a_connected_display_reports_edid_bytes` | fixed (T3.1) |
| `…::TestEdidByteCountAgainstRealSysfs::test_stat_disagrees_with_reading_which_is_the_whole_point`                                         | fixed (T3.1) |
| `tests/helpers/gnome/test_apply_enabled_extensions.py::TestMain::test_falls_back_to_dbus_run_session_without_a_bus`                       | fixed (T3.2) |
| `tests/helpers/host_health/test_handoff.py::TestTheHandoffCanBeSuppressedForTriage::test_the_findings_are_still_reported_either_way`      | fixed (T1.3) |
| `tests/helpers/host_health/test_login_message.py::TestTheEntryPointALoginShellCalls::test_it_exits_zero_and_prints_nothing_when_clean`    | fixed (T1.3) |
| `scripts/test-freezelib.bash` — the `assert_on_host` case                                                                                 | fixed (T3.5) |

All six are **defective tests**, not production paths misbehaving. Run `35034834651`
(`cedc9426`) confirmed the first pair of fixes on a real runner: `failures=5` became
`failures=3`, with the two `host_health` entries gone and nothing else changed.

### The DisplayLink pair

It deliberately asserts against real sysfs — its own docstring says a tempfile cannot
reproduce the defect, which is true: sysfs binary attributes report `st_size 0` with
content present, so a tempfile-only suite passes just as happily with `os.path.getsize()`.

It scanned every `card*-*` connector and asserted "connected and advertising modes ⇒ has
EDID bytes". That inference holds for a connector with a **physical display link**, where
the EDID comes from the monitor across it, and is simply false for a `Virtual` connector,
whose modes are invented by the driver and which has no monitor to read from. A runner is
a VM whose one connected connector is `card1-Virtual-1`. Production never looks at these
at all — `_drm_head_states()` globs `card*-DVI-I-*` — so the unsound inference was the
test's alone.

### The dbus fallback

The first diagnosis was **wrong**, and it is corrected here. The test *does* isolate
`DBUS_SESSION_BUS_ADDRESS`: `mock.patch.dict(..., clear=True)` unsets it, measured as
`None` inside the very patch the test uses. The real cause is one candidate further down.
`runtime_dirs` always appends `/run/user/<uid>` and deliberately never drops it — it is
the path derived from who the process actually is, so no environment change can remove it.
On a runner (uid 1001, a live user session) that socket is reachable,
`resolve_session_bus` returns `source="runtime-socket"`, and the fallback the test names
is never reached. It passed in the container only because the container runs as a uid with
no session.

### The two `host_health` tests, one of which was a dated bomb

`test_login_message` hardcoded `KERNEL` and `NOW`, while `main` read `os.uname().release`
and the real clock. Every other function in that module already takes those two facts as
arguments; `main` was the single entry point that could not be given them, so the test had
nothing to hold still and asserted against whatever machine ran it. Both are injection
seams now.

It also carried a **dated bomb**. Its fixture stamp was compared against the real clock,
so the assertion had an expiry: on **2026-09-28** it would have gone red on every machine,
CI or not, with nothing in the diff to point at. A test that reads the host is usually
found by moving it to another host — this one would have been found by waiting.

`test_handoff` relied on *"this container always has findings — no dkms, no systemd
bus"*. True here, false on a runner: systemd is PID 1 and `/var/lib/dkms` is absent, so
both probes take their silent branches and the prose the assertion expected is never
produced. It now supplies its own finding and asserts on that rather than on whatever the
host happened to say.

Both classes were verified against an emulated runner — foreign kernel, future clock,
working systemd, absent dkms — and pass.

### The freeze library's host guard

`assert_on_host` ORs three container signals, and the test drove it by the suite
*happening* to run inside a container — with an `else` branch that failed outright rather
than skipping. A deliberate fail-fast choice, and also what made the gate impossible to
satisfy on a runner. Only `$container` was injectable; the two marker paths now are too.

**The host consequence, and why "the behaviour is identical" understated it.**
`qa-deployed-drift.bash:219` covers `files/home/.local/lib/freeze/*` and compares with
`cmp -s`, so a comment-only change drifts. Its abort is the `qa-deployed-drift.bash` invocation in `qa-all.bash`, which sits
*before* `helper-tests` — so an undeployed host has a red `qa-all.bash` that **stops 27
hard gates short** (derived from the stage names a real run prints, not from `exit 1`
lines). That is this plan's own Task 4.1 mechanism aimed at the owner's workstation, and
`CLAUDE.md` makes a local `qa-all.bash` the pre-commit requirement, so it is not cosmetic.

**Both freeze plays are required, not either.** `tasks/deploy-freeze-lib.yml` deploys only
`freeze-common.bash`. The two binaries are deployed by their own plays —
`play-podfreeze.yml:75` and `play-lxcfreeze.yml:91` — and each play merely *includes* the
shared-library task. All three files changed in this plan, so running one play leaves the
other binary drifted and the gate still red. `qa-deployed-drift.bash:192` prints the owning
play per drifted file, so the gate names the right play itself; what was stale was this
plan's own instruction, which said "either freeze play".

## The mechanism that kept all of it invisible

`qa-all.bash` exits at the first failing hard gate. **5 hard gates stood behind the
`helper-tests` abort when it first went red (`9a79dd77`), 11 by `b3f6e909`, and 25 by
`497370ba`** (`cedc9426~1`, the high-water mark). 26 behind it today, though today it
passes. A gate that cannot pass in
an environment therefore does not merely stay red — it stops every gate behind it from
running at all, and the number of checks actually executing falls with nothing reporting
it. **20 of the 25 gates behind the abort had never run once in CI** by the time the first
fix landed, and clearing it surfaced real failures in two of them — `panel-sections`, then
`freezelib` — one at a time, as each fix let the run reach one gate further.

**Two, not three.** A third case was recorded alongside them at the time and repeated into
`PLAN.md`: `js` reporting 10 files locally against 8 in CI. That is a real divergence and it
is fixed, but `js` is one of the seven *accumulating* stages, so it ran on every CI run and
was never masked by the abort. Counting it made the unmasking look more frequent than it
was — the opposite direction from the undercount this plan is about, and worth naming for
that reason.

That is Task 4.1's answer and the argument for Task 4.3.

Three causes compounded to keep it that way:

1. **The first red was a gate that cannot pass in CI by construction** — a link into a
   gitignored tree. It was never a regression anyone could fix by fixing code, so nobody did.
2. **A permanently-red run carries no information**, so each later regression joined it
   invisibly. Red → red is not an event.
3. **Local `qa-all.bash` was green throughout**, and `CLAUDE.md` names it the pre-commit
   requirement — so the contributor's own signal said green every single time.

### The suite already contains both designs, and that is why the two causes hid differently

Counted mechanically, `qa-all.bash` runs its stages two ways:

| Design                  | Count | Behaviour on failure                                                 |
| ----------------------- | ----- | -------------------------------------------------------------------- |
| jq-merged, accumulating | 7     | `\|\| rc=$?`, `FAILED++`, **run continues**; all reported at the end |
| hard gate               | 30    | `exit 1` immediately; everything declared after it never runs        |
| missing-tool abort      | 7     | `exit 2`; same effect, and prints no `QA FAILED` line                |

7 + 30 = 37 gates, which print **38** stage names: `qa-bash.bash` emits both `bash` and
`shellcheck`, so the seven accumulating gates account for eight named lines. (These counts
move whenever a gate is added, which is why the block below derives them from a run rather
than restating them — this plan added `docs-exit-codes` and every one of them shifted by one.)

**Counted from the RUN, not from the source.** Counting `exit 1` occurrences is a proxy for
counting gates and it is not a sound one — a single gate may own more than one abort, and
`helper-tests` now owns two (its own failure, and the unreadable-capture failure Task 4.4
added). That proxy produced a wrong number here three times. The stage names a real run
prints are the ground truth, and `helpers/qa_environment/verdicts.py` already parses them:

```python
acc = {"bash","shellcheck","python","patterns","ansible","ansible-syntax","js","docs"}
hard = [n for n in stage_order if n not in acc]     # -> 30
hard[hard.index("helper-tests")+1:]                 # -> 27 behind it
```

**These counts are as of this plan's HEAD, and this plan moved them.** Numbers describing
what CI *was* masking are the counts at the masked commit and are labelled as such below.

The seven accumulating stages are `bash`, `python`, `patterns`, `ansible`,
`ansible-syntax`, `js` and `docs`; they merge into one JSON document and are reported
together by `qa-all.bash`'s `# Merge JSON from all checks` block (the `jq -s`
invocation) and its `# Final terse summary` block — the second is where the seven become
one verdict line, which is what the masking argument turns on.

**This is the explanation the plan was missing.** The two causes were masked differently
because they fell on opposite sides of that line:

- **Cause A (`docs`) is an accumulating stage.** It has been red since 2026-08-31 and
  masked nothing at all — every gate behind it kept running. That is why the current CI log
  shows `✗ docs` followed by 28 passing stages and only then `✗ QA FAILED` (run
  `35041998528`, counted from that run's own output).

- **Cause B landed in hard gates**, and the number that matters is the one at the commits
  where masking actually happened — **not** `29ceee97`, which is this plan's own round-2
  commit, by which point the test fixes had landed, `helper-tests` passed
  (`✓ Ran 1456 tests, 2 skipped`) and the run reached its final summary. Nothing was masked
  there. Counting behind that abort measured a real property of the script and attributed it
  to a commit that had stopped exhibiting it. Behind `helper-tests` at the commits that were
  actually red:

  | Commit     | Date       | Hard gates masked |                                                                              |
  | ---------- | ---------- | ----------------- | ---------------------------------------------------------------------------- |
  | `9a79dd77` | 2026-09-11 | **5**             | helper-tests first goes red                                                  |
  | `b3f6e909` | 2026-09-14 | **11**            |                                                                              |
  | `497370ba` | 2026-09-15 | **25**            | `cedc9426~1` — the high-water mark, immediately before this plan's first fix |

  The masked set **grew fivefold in four days**, because 21 commits touched `qa-all.bash` in
  that window and every gate they added landed behind an abort that already could not be
  reached. That is the property worth stating, and it is measured rather than asserted:
  **every gate added after a standing abort is born unexecuted.** 20 of the 25 had never run
  in CI even once by the time the first fix landed.

  25 was therefore the right number and the wrong commit — it is `497370ba`'s figure, not
  `29ceee97`'s. Clearing the abort unmasked `panel-sections`, then `freezelib`, one at a
  time.

It also narrows **Task 4.3**. Its option (1) — run every gate, report all verdicts, exit
non-zero at the end — is not a new design to weigh: it is the design already in force for
seven stages of this same script, and the one the final summary was written for. The
question is whether to extend it to the other 30, not whether to invent it. The repo's own
recurring lesson applies to the plan that is documenting it: the right answer already
existed one directory over — in this case, sixty lines up.

### The `qa-js.bash` gap is not worktree-only, and it sits in front of this plan's deliverable

`PLAN.md`'s Non-Goal described this as a linked-worktree gap. The document it cites says
otherwise two sentences away: `CLAUDE/QA.md`'s machine-dependence table names the missing
input as absent in *"a linked worktree, **and any checkout where `npm install` has not been
run**"* — which includes a fresh clone. Two statements of one population, disagreeing.

Measured rather than assumed:

- No playbook installs the deps. `grep -rniE 'extensions.*(npm|node_modules)|npm.*extensions'`
  over `playbooks/ tasks/ roles/ files/` returns nothing.
- The CCY Dockerfile **cannot** supply them: it carries no node stage, and
  `extensions/node_modules` is under the bind-mounted project directory, so anything a `RUN`
  created there would be covered by the mount at start-up.
- CI is green only because `.github/workflows/qa.yml` runs `npm ci` in `extensions/`.
- `extensions/node_modules` is gitignored, so it survives in a working checkout once someone
  has run `npm ci` and is absent in every fresh one.

Why it matters here rather than in the abstract: where the deps are missing, `qa-js.bash`
exits 2 and `qa-all.bash` aborts **before `qa-docs.bash` is invoked at all**. The gate this
plan exists to fix is then never executed by the mandated command — Task 4.1's mechanism,
live, in front of this plan's own deliverable.

By `CLAUDE.md`'s missing-dependency rule this is an IaC gap to close, not a runtime
condition to document. It is not closed here because the remedy is a real decision — whether
extension dev tooling belongs in the desktop provision at all, or in a separate bootstrap —
and that is the owner's, not this plan's. Flagged, costed, and left for its own plan.

### A failing hard gate erases ITSELF from the census, not just the gates behind it

This was assumed to be a masking problem about *subsequent* gates. It is worse than that,
and the difference decides Task 4.3.

A hard gate that fails prints prose, not a stage line:

```bash
echo "✗ QA FAILED: helper unit tests" >&2
exit 1
```

`verdicts.STAGE` is `^(?P<symbol>[✓✗⚠]) (?P<name>[a-z0-9][a-z0-9-]*): ` and `RUN_SUMMARY` is
`^[✓✗⚠] QA (?:passed|FAILED):`. That line is a RUN SUMMARY. Measured rather than reasoned —
`verdicts.parse()` over a three-line sample ending in that abort:

```
stages recorded: ['bash', 'nokill-containerwatch']
is a stage recorded for the FAILING gate?  False
```

All **31** gate aborts in `qa-all.bash` have this shape (32 `exit 1` sites, less the final
summary; `helper-tests` owns three of them, which is how 31 lines cover 29 gates). And the
prose is not even the gate's name: `helper unit tests` against the stage `helper-tests`,
`secret scanner unit tests` against `secret-scan-tests`, `plan-script library regression tests` against `planlib-tests`, `the panel and the status document producer disagree`
against `panel-contract`. No reader could map one to the other.

**This makes option (2) unworkable as written.** "Have CI compare the executed-gate list
against the declared one and fail on a shrink" assumes a failed gate is distinguishable from
an absent one. It is not: both produce no stage line. The shrink detector would report the
gate that failed as a gate that never ran — a wrong sentence about the one event it exists
to describe — and it would do so on every red run, which is every run it matters on.

Making the failure lines stage-shaped is the prerequisite for option (2), and it is most of
option (1)'s work: the 30 hard gates share one shape,

```bash
if ! foo_out="$(bash "$SCRIPT_DIR/gate.bash" 2>&1)"; then … exit 1; fi
foo_summary=$(qa_gate_case_count "$foo_out")
printf '✓ gate-name: %s\n' "$foo_summary"
```

so the edit is the same mechanical one at each: emit `✗ gate-name: <reason>`, increment
`FAILED`, and put the `✓` line in an `else`. The seven `exit 2` missing-tool aborts stay
aborts — a suite that cannot run its tools has nothing to accumulate.

**Recommendation, and still the owner's call:** option (1). Option (2) costs the same edit
and then adds a comparison on top of it, and what it would buy — "a gate vanished" — falls
out of option (1) for free, because every gate then prints a line whether it passed or
failed.
