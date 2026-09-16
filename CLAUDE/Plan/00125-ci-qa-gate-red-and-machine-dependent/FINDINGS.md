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

**Task 4.5 is the same shape, unfixed.** 21 other hard gates in `qa-all.bash` read their
case count with `grep -oE 'passed: [0-9]+'`, unscoped: `-o` prints every match, so a
second occurrence makes the stage line two lines and `verdicts.py` reads the first as the
stage and loses the rest. It is not fixed in passing because those gates do not agree on a
format — `passed: N` alone, one/two/three spaces before `failed:`, and two prefixed with
the gate's own name — so a shared reader is a design task, not an extraction.

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

### The freeze library's host guard

`assert_on_host` ORs three container signals, and the test drove it by the suite
*happening* to run inside a container — with an `else` branch that failed outright rather
than skipping. A deliberate fail-fast choice, and also what made the gate impossible to
satisfy on a runner. Only `$container` was injectable; the two marker paths now are too.

**The host consequence, and why "the behaviour is identical" understated it.**
`qa-deployed-drift.bash:219` covers `files/home/.local/lib/freeze/*` and compares with
`cmp -s`, so a comment-only change drifts. Its abort is `qa-all.bash:138`, which sits
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
| hard gate               | 29    | `exit 1` immediately; everything declared after it never runs        |
| missing-tool abort      | 7     | `exit 2`; same effect, and prints no `QA FAILED` line                |

7 + 29 = 36 gates, which print **37** stage names: `qa-bash.bash` emits both `bash` and
`shellcheck`, so the seven accumulating gates account for eight named lines.

**Counted from the RUN, not from the source.** Counting `exit 1` occurrences is a proxy for
counting gates and it is not a sound one — a single gate may own more than one abort, and
`helper-tests` now owns two (its own failure, and the unreadable-capture failure Task 4.4
added). That proxy produced a wrong number here three times. The stage names a real run
prints are the ground truth, and `helpers/qa_environment/verdicts.py` already parses them:

```python
acc = {"bash","shellcheck","python","patterns","ansible","ansible-syntax","js","docs"}
hard = [n for n in stage_order if n not in acc]     # -> 29
hard[hard.index("helper-tests")+1:]                 # -> 26 behind it
```

**These counts are as of this plan's HEAD, and this plan moved them.** Numbers describing
what CI *was* masking are the counts at the masked commit and are labelled as such below.

The seven accumulating stages are `bash`, `python`, `patterns`, `ansible`,
`ansible-syntax`, `js` and `docs`; they merge into one JSON document and are reported
together by `qa-all.bash:610-627`.

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
question is whether to extend it to the other 29, not whether to invent it. The repo's own
recurring lesson applies to the plan that is documenting it: the right answer already
existed one directory over — in this case, sixty lines up.
