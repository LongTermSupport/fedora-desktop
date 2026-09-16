# Plan 00125: The QA workflow has been red for three weeks, and qa-all.bash answers differently per machine

**Status**: In Progress
**Created**: 2026-09-15
**Owner**: joseph
**Priority**: High

## Overview

The `QA` workflow's last green run on `F44` was **2026-08-26** (`1fc1c5fe`). Every run
since has failed. Sampling the last 100 runs on the branch: 90 failed, 0 succeeded, the
remainder cancelled.

`CLAUDE/QA.md` treats CI as the authority — it is the argument that a separate CI step
would be *"a divergence, not a belt-and-braces"*, and the reason `test-ccy-rootless-guard.bash`
was moved into `qa-all.bash` rather than left as a CI-only step. For three weeks that
authority has been reporting nothing, and because a red run looks the same as the
previous red run, no individual commit's failure was distinguishable from the standing one.

The deeper defect is not the redness. **`./scripts/qa-all.bash` returns a different verdict
on different machines** — green in the CCY container, red on a GitHub runner. A gate whose
answer depends on where it runs cannot gate anything, and "run QA before committing" has
been satisfiable locally while CI disagreed.

Plan 00049 is not at fault: its Decision Gate 2 (`.github/workflows/qa.yml`, QA-03) was
genuinely done and CI *was* green at its Batch 9. This is a regression that landed after.

## Goals

- The `QA` workflow is green on `F44`.
- `qa-all.bash` reaches the **same verdict** in the CCY container, on a host, and on a
  GitHub runner — or names precisely and by design which stages cannot run where, in a
  form that fails rather than passes when the reason stops applying.
- A future regression of this kind is visible on the commit that causes it, rather than
  three weeks later.

## Non-Goals

- Rewriting any gate's *substance*. This plan restores the signal; it does not re-open
  what the gates check.
- The missing-input gaps — the vault password file, and `qa-js.bash` exiting 2 on an absent
  `extensions/node_modules`. **Not a worktree-only population**: `CLAUDE/QA.md` names it as
  a worktree *and any checkout where `npm install` has not been run*, which includes a fresh
  clone. An IaC gap wanting its own plan; the cost, and why it bites here, in `FINDINGS.md`
- Running ccy itself in CI — Plan 00113 owns that.

## Context & Background

Two causes, both established with evidence before any code changed, and both written up in
full in **[FINDINGS.md](FINDINGS.md)** — the per-cause reasoning, the table of affected
tests, and the one diagnosis that turned out to be wrong and how it was corrected.

**Cause A — the docs gate.** Eight tracked `.claude/rules/*.md` files link into the
gitignored `.claude/hooks-daemon/` tree, so in a clean checkout the target cannot exist.
It is the original breakage (`0015c886`, 2026-08-31) and the only cause still open: the
fix is Task 2.1's decision, not a repair.

**Cause B — tests that read the machine they were written on.** Five at first, and a sixth
once the abort stopped hiding it. All six were defective **tests**, not production paths;
all six are fixed.

**The mechanism, and it hid Cause B rather than "all of it".** `qa-all.bash` runs 7 stages
that accumulate and 29 hard gates that `exit 1`. Cause A sits in an accumulating stage, so
it masked nothing — it went red and every gate behind it kept running. Cause B was in hard
gates, and the set behind the `helper-tests` abort **grew 5 → 11 → 25** while it was red, so
20 of them had never run once in CI. That is Task 4.1's answer and the argument for Task 4.3.

## Tasks

### Phase 1: Establish what is actually true

- [x] ✅ **Task 1.1**: `triage.bash` + `probe-qa-verdicts.bash` — runs `qa-all.bash` here,
  downloads the newest **completed** CI run's whole log (not `--log-failed`: a stage that
  *passed* on both machines over different inputs is the case that hid `js` for weeks),
  and puts them side by side. Four states per stage, because two of them are the point:
  `differs`, and `only-here`/`only-there` — a stage **absent** from one side never ran
  there, which is what the abort does and what no pass/fail comparison can show. Parsing
  and diffing live in `helpers/qa_environment/verdicts.py` (40 tests) rather than the
  script, per `helpers/CLAUDE.md`. It renders no verdict (R9) and points at
  `CLAUDE/QA.md`'s declared-dependency table. It declares neither `plan_require_host` nor
  `plan_require_container`, deliberately — `triage.bash:18-23` owns that argument. First
  run found six differences: the two declared ones,
  and four that were only this checkout being ahead of the compared commit — so it now says
  so rather than letting a reader chase them
- [x] ✅ **Task 1.2**: **`0015c886`, 2026-08-31** — and the task's own premise was wrong:
  there was no further cause "between those dates" because no commit was pushed in those
  five days, so CI observed nothing. Cause A ran alone for eleven days; Cause B arrived in
  two later waves, every one landing into an already-red run. Commits and dates in
  `FINDINGS.md`
- [x] ✅ **Task 1.3**: Diagnose the two `host_health` failures — **both determined,
  reproduced byte-exactly, and fixed**, and verified against an emulated runner. Neither
  was the ledger. Both were **defective tests reading host state**, not production paths
  misbehaving. One of them carried a dated bomb that would have reddened every machine on
  2026-09-28. See `FINDINGS.md`, "The two `host_health` tests"

### Phase 2: The docs gate — decided, and out of the dependency business

- [x] ✅ **Task 2.1**: **DECIDED by the owner: not (a).** The daemon is not to be installed
  in CI — "maybe later, but only if we decide it's needed". The rule is *we do not QA another
  repo's files*, covering the daemon, the vendored roles, and whatever is vendored next. It
  also beat the recorded option (d): the exemption is on the resolved **target**, not on
  whole files by a content marker, so a generated file's own broken links still count

- [x] ✅ **Task 2.2**: `link_check.py` classifies a target three ways — tracked (checked),
  inside a declared vendored repo (warned on, never failed), neither (**a finding**). The
  third is what makes the second safe: exempting everything unowned would have covered a
  link into `untracked/` too, trading a false failure for a silent skip. A vendored target
  is still looked at: `verified` / `unverifiable` (repo absent) / `broken` (repo present,
  target gone — its own `⚠` line). The **exit code** is what must not depend on what is
  installed; the detail may, and should. The question asked is trackedness (`git ls-files`),
  which is what the finding has always claimed and is in every clean checkout. Roots are
  DECLARED, as parents, so vendoring under an existing root needs no code change.
  **Proved against a daemon-less tree**: 0 findings, 8 unverifiable; remove the declaration
  and the same 8 return as findings naming the nested repo. 26 cases

### Phase 3: The tests that read the machine they were written on

- [x] ✅ **Task 3.1**: The DisplayLink pair — the scan now excludes DRM connector types
  that carry **no physical display link** (`Virtual`, `Writeback`), for which the test's
  inference was never sound. A denylist on purpose: an unrecognised type is asserted
  against, not skipped, so a linkless type nobody has met yet surfaces as a failure rather
  than as a test that quietly stopped checking. Falsified four ways (journal, 23:30),
  including the requirement this task was written around: a real link type whose EDID
  reads zero still **fails**
- [x] ✅ **Task 3.2**: The dbus fallback test — the bus is now **injected** rather than
  arranged on the host, because the host cannot be arranged: the uid-derived candidate is
  by design not environment-controllable. Which branch `resolve_session_bus` picks is
  `session_bus`'s own question and was already settled hermetically in its suite; what
  belongs in the applier's suite is that `main` puts the resolved prefix in front of
  `gsettings` and exports nothing for that route. Falsified by mutating `_gsettings` to
  drop `bus.prefix` — the test fails, with the same assertion text CI produced
- [x] ✅ **Task 3.3**: The two `host_health` tests — done with Task 1.3, since the
  diagnosis and the fix were the same piece of work.
- [x] ✅ **Task 3.4**: **All five are defective tests.** Not one is a production path
  misbehaving; `git diff` for this phase touches `tests/` only. Each asserted something
  true of the machine it was written on rather than of the code: the container's absent
  systemd, the author's kernel and calendar date, a VM's virtual connector, a uid with no
  session. One real production **gap** was found and closed: `session_bus.current()` — the
  single function in that module that reads `os.environ` and `os.getuid()` instead of
  taking them as arguments — had **no test of its own**, and the only thing exercising it
  was the applier test that was really asking a different question. It now has four.
- [x] ✅ **Task 3.5**: A **sixth**, of the same species, and it only became visible because
  Phase 3 removed the abort that was hiding it. `test-freezelib.bash`'s `assert_on_host`
  case drove the guard by *the suite happening to run inside a container* — and its `else`
  branch failed outright with *"this suite is not running in a container, so the guard
  cannot be driven"*. Deliberate fail-fast, and it made the gate impossible to pass on a
  runner. The guard ORs three signals and only one was injectable, so
  `freeze-common.bash` now reads the two marker **paths** from overridable variables
  (defaults unchanged, no tool sets them). All three signals are driven on any machine,
  and the **allow** direction is asserted for the first time — it could never be, because
  in a container the real marker files are there. Falsified both ways (journal, 23:52).
  **HOST, and more urgent than "the behaviour is identical" suggested.**
  An undeployed host's `qa-all.bash` is red **and stops 27 hard gates short**, because the
  drift gate aborts at the `qa-deployed-drift.bash` invocation in `qa-all.bash` — before
  `helper-tests`. **Run BOTH
  `play-podfreeze.yml` and `play-lxcfreeze.yml`**, not either: each deploys its own binary
  and all three files changed. Runtime behaviour is unchanged. Detail in `FINDINGS.md`

### Phase 4: Make the next regression visible

- [x] ✅ **Task 4.1**: The identical-looking red run is only half of it, and the other half
  is worse. `qa-all.bash` **exits at the first failing hard gate**, so from the moment the
  DisplayLink pair began failing CI stopped executing every gate behind `helper-tests`:
  **5 at `9a79dd77`, 11 by `b3f6e909`, 25 by `497370ba`** (`cedc9426~1`) — fivefold in four
  days, because every gate added in that window landed behind an abort already out of reach.
  The suite did not merely stay red — *the number of checks actually running fell*, and
  nothing said so. Three compounding causes, written up in `FINDINGS.md`. **Demonstrated
  live twice while closing Phase 3**: clearing the abort revealed a real failure in
  `panel-sections`, then in `freezelib`, each a gate that had never run once (journal, 23:38
  and 23:52). The remedy is Task 4.3

- [x] ✅ **Task 4.2**: `CLAUDE/QA.md` now carries *"The same command does not reach the same
  verdict everywhere"* — a table of each environment-dependent stage and what it needs,
  `deployed-drift` named as the shape to copy (it states its dependency and prints the
  reason it skipped), the abort-hides-the-rest consequence spelled out, and the two rules
  that would have prevented both defects found today: resolve paths relative to the file,
  never to a fixed absolute root; and exclude the whole `.ansible/` tree from discovery

- [ ] ⬜ **Task 4.3**: Make a gate's *absence* visible. `qa-all.bash` aborting at the first
  failure means one unfixable gate silently disables every gate after it — the mechanism
  behind Task 4.1. **Owner's call:** (1) run every gate and report all verdicts before
  exiting non-zero, or (2) keep the abort and have CI fail on a shrink in the executed-gate
  list. Measured since: a failing hard gate prints `✗ QA FAILED: <prose>`, which
  `verdicts.py` reads as a run summary, so it erases *itself* from the stage census — (2)
  cannot tell a failed gate from an absent one, and fixing that is most of (1)'s work.
  Recommendation: **(1)**. Costing in `FINDINGS.md`

- [x] ✅ **Task 4.4**: The `helper-tests` line no longer scrapes the run's output. Four
  readers that did were each defeated by a test printing unittest-shaped text, the last by
  an `atexit` handler writing after unittest's summary — see `FINDINGS.md`, "The counts are
  not in the text". `helpers/qa_environment/unittest_counts.py` (21 tests) takes both
  numbers from unittest's `TestResult` object and writes them to the path
  `qa-helper-tests.bash --counts-file` is given; the single reader in
  `scripts/lib/qa-helper-summary.bash` reads that file and **fails** rather than reporting
  zero when it cannot, driven by `scripts/test-qa-helper-summary.bash` as its own gate — case
  count in its stage line, not frozen here. Mutation-tested: 6 of the runner, 13 of the reader, all caught; the last case runs
  the real runner end to end. `qa-all.bash` captures that run's stdout and requires it
  EMPTY, since a `print()` in any test could otherwise forge a stage line

- [x] ✅ **Task 4.5**: 21 other hard gates each inlined `grep -oE 'passed: [0-9]+'`; `-o`
  prints every match, so an earlier `passed: <digits>` made the stage line TWO lines —
  round 4's defect in 21 untested copies. They now share `qa_gate_case_count`, scoped to the
  last matching LINE because the 21 disagree on a format. **Swept three times, short twice**:
  for the pattern text (missed 2 with a different regex, one of them `nokill`, which had
  matched NOTHING since it landed), then for the `||` fallback (missed 2 that interpolated
  the whole capture — `vmtest-manifest` emitted a THREE-line stage line every run, losing two
  measurements to `verdicts.py`). All 29 hard gates are enumerated now. **Not a pure
  refactor**, and that claim is retracted: 21 of 21 case-count lines are byte-identical, and
  3 changed — two of them broken, one losing a stray full stop. 26 cases, 8 read out of
  `qa-all.bash` and run against the real gate with a `COVERAGE: n of m`, so a pattern cannot
  drift from its gate again (see `FINDINGS.md`)

### Phase 5: Close

- [x] ✅ **Task 5.1**: `./scripts/qa-all.bash` green locally — 920 files.
- [x] ✅ **Task 5.2**: **GREEN — run `35076071578`, commit `7e85ff49`**, the first success on
  `F44` in three weeks. `✓ QA passed: 920 files checked`, no failing stage. The docs line
  reads `VENDORED: 0 verified, 8 unverifiable (repo absent), 0 broken` — CI says it could not
  check those 8 rather than skipping them quietly, which is the whole design. And
  `helper-tests` reports `2 skipped` there against `1` locally on the same commit, with
  `65 tracked` agreeing: the machine-dependence this plan exists to expose, visible in a
  PASSING stage, where no pass/fail comparison could ever have found it
- [ ] ⬜ **Task 5.3**: `qa-reviewer` agent over the full diff.

## Success Criteria

- [x] The `QA` workflow's most recent run on `F44` is a success — a condition, not a frozen
  run. First green after three weeks: `35076071578` (`7e85ff49`); every run since passed too.

- [x] Local `qa-all.bash` and the CI run agree on every stage, or the disagreement is
  declared in `CLAUDE/QA.md` and fails closed when its reason stops applying. Measured by
  `triage.bash` at the **same commit on both machines**: three differences, every one
  declared, and `helper-tests` differing by skip count is **the point, not a residue** —
  until that count joined the line the two sides were byte-identical and read as `agree`.
  Commits, run IDs and the evidence table are in `FINDINGS.md`.

- [x] Each of the tests has been classified as a defective test or a production path
  reading unowned host state, and fixed accordingly — all six were defective tests.

- [x] The docs gate passes in a checkout with no hooks daemon installed — CI run
  `35081847136`; the two machines' `VENDORED:` counts are mirror images. See `FINDINGS.md`

- [ ] A deliberately introduced failure is distinguishable from the standing state.
  Partly met, stated precisely: the standing state is GREEN on both machines now, so any
  new failure changes the run's output. Not met in Task 4.3's sense — a gate that stops
  running is invisible, and a gate that FAILS erases its own stage line.

## Risks & Mitigations

- **Fixing the tests by weakening them.** The DisplayLink pair exists precisely because a
  tempfile-only suite passed while the defect was live; a skip that widens too far restores
  that hole. Mitigation: Task 3.1 requires the test to still fail on a real zero-read.
- **Option (d) leaving a genuinely broken daemon link unreported here.** Mitigation: those
  files are checked by the daemon's own `docs_qa`, and (d) is scoped by ownership marker so
  the 7 repo-authored rule files stay covered.
- **Phase 3 turning into a rewrite of the helper suites.** Mitigation: Non-Goals — this
  plan restores the signal, it does not re-open what the gates check.

## Dependencies

- None blocking. Phase 3 Task 3.3 depends on Task 1.3.
- Plan 00113 (ccy CI runner) is adjacent, not a dependency: it concerns running ccy *in*
  CI, not the QA workflow that is red.

## Delivery & Milestones

Every commit is `Plan 00125: …` on `F44`; `git log --oneline --grep 'Plan 00125'` is the
list, and `JOURNAL/` carries what each one found. Repeating it here only creates a second
copy to keep in step.

Remaining: Task 2.1 (decision), 2.2 (its implementation), 4.3 (decision), 5.2 (follows 2.2),
5.3 (`qa-reviewer`).
