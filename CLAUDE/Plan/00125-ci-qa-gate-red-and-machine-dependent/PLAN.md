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
- The worktree vault-password gap (`CLAUDE/Plan/00123-…/WORKTREE-QA-GAP.md`). A linked
  worktree is a third environment with its own missing input and its own decision.
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
gates, and **5 to 11 gates stood behind the `helper-tests` abort** while it was red, so
three of them had never run once in CI. That is Task 4.1's answer and the argument for Task 4.3.

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
  `CLAUDE/QA.md`'s declared-dependency table. It declares **neither** `plan_require_host`
  nor `plan_require_container`, deliberately; `triage.bash:18-23` owns that argument and
  this page does not restate it. First run found six differences: the two declared ones,
  and four that were only this checkout being ahead of the compared commit — so it now says
  so rather than letting a reader chase them
- [x] ✅ **Task 1.2**: **`0015c886`, 2026-08-31** — and the task's own premise was wrong.
  There was no further cause "between those dates" because there was nothing between them:
  no commit was pushed to `F44` in those five days, so CI observed nothing. `0015c886` is
  the next run after the last green, and it failed on **docs alone** (7 findings, every one
  `target does not exist`, all 203 helper tests passing). Cause A is the original breakage
  and was the only one for eleven days. Cause B arrived later and in two waves —
  `9a79dd77` (2026-09-11) added the DisplayLink pair; `cb88ec4e`, `ff63ac5d` and `b3f6e909`
  (all 2026-09-14) added the other three. Every one of them landed into an already-red run
- [x] ✅ **Task 1.3**: Diagnose the two `host_health` failures — **both determined,
  reproduced byte-exactly, and fixed.** Neither was the ledger. Both were **defective
  tests reading host state**, not production paths misbehaving; production was doing its
  job in each case.
  - `test_login_message` hardcoded `KERNEL` and `NOW` while `main` read
    `os.uname().release` and the real clock — `main` was the one entry point that could
    not be given the two facts every other function there takes as arguments. It now
    takes both as injection seams. **This test also carried a dated bomb**: the fixture
    stamp against the real clock meant it would have gone red on every machine on
    **2026-09-28**, CI or not
  - `test_handoff` relied on *"this container always has findings — no dkms, no systemd
    bus"*, which is true here and false on a runner (systemd as PID 1, no `/var/lib/dkms`,
    so both probes take their silent branches). It now supplies its own finding and
    asserts on that rather than on prose the host happened to produce
  - Both classes verified against an emulated runner — foreign kernel, future clock,
    working systemd, absent dkms — and pass

### Phase 2: The docs gate — decision required

- [ ] ⬜ **Task 2.1**: **DECISION GATE** — how a tracked file may reference an installed,
  gitignored tree. All 8 findings are daemon-generated: the 8 rule files carrying
  `hooks-daemon-rule-version` are exactly the 8 reported, rendered by the daemon's own
  installer. **(b)** is the skip-if-absent shape `CLAUDE.md` prohibits by name; **(c)** is
  re-rendered by `sync_directory_role_rules()` at the next upgrade. The link is not wrong —
  its premise, *the daemon is installed*, is false in CI. **Two live options** — my earlier
  claim that only (a) remained was wrong: **(a)** install the daemon in CI (a network fetch
  per run), or **(d)** exclude daemon-GENERATED files from the link check by their version
  marker — unconditional, no network, and the same ownership judgement
  `link_check.py:214-223` already makes for four other trees, though by CONTENT rather than
  PATH, so it needs a coverage line or the excluded set is invisible. The 7 repo-authored
  rule files stay checked either way. Full argument in `FINDINGS.md`. **The owner's call**:
  (a) treats the daemon as a dependency, (d) treats its output as not ours to audit

- [ ] ⬜ **Task 2.2**: Implement the chosen option; the docs gate passes in a clean checkout.

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
  drift gate aborts at `qa-all.bash:137` — before `helper-tests`. **Run BOTH
  `play-podfreeze.yml` and `play-lxcfreeze.yml`**, not either: each deploys its own binary
  and all three files changed. Runtime behaviour is unchanged. Detail in `FINDINGS.md`

### Phase 4: Make the next regression visible

- [x] ✅ **Task 4.1**: The identical-looking red run is only half of it, and the other half
  is worse. `qa-all.bash` **exits at the first failing hard gate**, so from the moment the
  DisplayLink pair began failing CI stopped executing every gate behind `helper-tests`:
  **5 at `9a79dd77`, growing to 11 by `b3f6e909`** as new gates were added behind a gate
  that could not pass. The suite did not merely stay red — *the number of checks actually
  running fell*, and nothing said so. Three compounding causes, written up in `FINDINGS.md`.
  **Demonstrated live three times while closing Phase 3** — each fix revealed the next gate
  that had never run once (journal, 23:38 and 23:52). The remedy is Task 4.3

- [x] ✅ **Task 4.2**: `CLAUDE/QA.md` now carries *"The same command does not reach the same
  verdict everywhere"* — a table of each environment-dependent stage and what it needs,
  `deployed-drift` named as the shape to copy (it states its dependency and prints the
  reason it skipped), the abort-hides-the-rest consequence spelled out, and the two rules
  that would have prevented both defects found today: resolve paths relative to the file,
  never to a fixed absolute root; and exclude the whole `.ansible/` tree from discovery

- [ ] ⬜ **Task 4.3**: Make a gate's *absence* visible. `qa-all.bash` aborting at the first
  failure means one unfixable gate silently disables every gate after it — the mechanism
  behind Task 4.1, and not something documentation alone fixes. Options to weigh: run every
  gate and report all verdicts before exiting non-zero; or keep the abort but have CI
  compare the executed-gate list against the declared one and fail on a shrink. This is a
  structural change to the suite and affects local runs too, so it is the owner's call.
  **Narrowed:** the first option is not a new design — 7 of the 36 gates already work that
  way (`|| rc=$?`, `FAILED++`, reported together at the end) against 29 that `exit 1`.
  The question is whether to extend the existing design to those 29, not
  whether to invent it. That split is also why the two causes hid differently — `docs`
  accumulates and masked nothing; Cause B was in hard gates. See `FINDINGS.md`

- [x] ✅ **Task 4.4**: The `helper-tests` line now has a test, because it had been wrong
  twice in three revisions and every hand-check died with the session that ran it. Both
  readers moved to `scripts/lib/qa-helper-summary.bash`, sourced by `qa-all.bash`, driven by
  `scripts/test-qa-helper-summary.bash` (23 cases) which runs as its own gate — so the test
  exercises the shipped functions rather than a copy of the expression. Falsified against
  all three historical defects: the whole-capture match fails 6 cases, the closing-paren
  match 2, answering `0` for an unreadable capture 3. An unreadable capture now **fails**
  rather than reporting `0 skipped`, which was the same defect waiting to happen a fourth
  time. `helpers/docs/link_check.py` learned that a `source`d library is not a gate —
  otherwise the inventory would have demanded a row claiming a library checks something

### Phase 5: Close

- [x] ✅ **Task 5.1**: `./scripts/qa-all.bash` green locally — 918 files.
- [ ] ⬜ **Task 5.2**: The `QA` workflow green on `F44` — the run link is the evidence.
  **Blocked on Task 2.1 and nothing else.** Run `35041998528` (`29ceee97`) fails on
  `✗ QA FAILED: 8 errors in 918 files`, and all 8 are the docs findings. Every one of the
  other 35 stages passes, and the file count matches a local run exactly.
- [ ] ⬜ **Task 5.3**: `qa-reviewer` agent over the full diff.

## Success Criteria

- [ ] The `QA` workflow's most recent run on `F44` is a success, with the run identified.

- [x] Local `qa-all.bash` and the CI run agree on every stage, or the disagreement is
  declared in `CLAUDE/QA.md` and fails closed when its reason stops applying. Measured by
  `triage.bash`, not asserted, and measured at the **same commit on both machines** with a
  clean tree (`fa3cfe8e`, CI run `35040903213`): 36 stages each side, 37 of 38
  symbol-prefixed lines accounted for, **three** differences, every one declared —

  - `docs`: Cause A, the one open decision (Task 2.1);
  - `deployed-drift`: declared, and it prints its own reason on each machine;
  - `helper-tests`: `1 skipped` here against `2 skipped` on a runner — **the point, not a
    residue.** The two machines skip *different* tests, and until the skip count joined the
    line the two sides were byte-identical and read as `agree`. Declared in `CLAUDE/QA.md`.

  `js`, `bash`, `patterns` and `python` now agree exactly, which three of them did not
  before this plan.

- [x] Each of the tests has been classified as a defective test or a production path
  reading unowned host state, and fixed accordingly — all six were defective tests.

- [ ] The docs gate passes in a checkout with no hooks daemon installed.

- [ ] A deliberately introduced failure is distinguishable from the standing state.
  Partly met and worth stating precisely: it is distinguishable *now* in the sense that
  a new failure changes the run's output, because only one cause remains. It is not yet
  met in the sense Task 4.3 means — a gate that stops RUNNING is still invisible.

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
