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

Two causes are established. Both were observed on run `35027739399` (`b81832cb`) and
reproduced on `34995414256` (`9b6c8d55`), so neither is new today.

**Cause A — the docs gate, 8 findings, all one shape.** Eight tracked `.claude/rules/*.md`
files link to `../hooks-daemon/CLAUDE/DirectoryRoles.md`. `.claude/hooks-daemon/` is
gitignored (`.gitignore:53`, `.claude/.gitignore:3`), so in a clean checkout the target
cannot exist. `helpers/docs/link_check.py` already lists `.claude/hooks-daemon/` in
`_EXCLUDE_PREFIX`, but that excludes files in that tree from being **scanned** — it does
not exempt links **into** it from the existence check. The link is correct on an installed
machine and impossible in CI. Those pointer files arrived in `0015c886` on 2026-08-31,
five days *after* the last green run, so they are not the original breakage.

**Cause B — five helper unit tests that pass locally and fail on a runner.**

| Test                                                                                                                                      | Status       |
| ----------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| `tests/helpers/displaylink_recovery/test_run_recovery.py::TestEdidByteCountAgainstRealSysfs::test_a_connected_display_reports_edid_bytes` | fixed (T3.1) |
| `…::TestEdidByteCountAgainstRealSysfs::test_stat_disagrees_with_reading_which_is_the_whole_point`                                         | fixed (T3.1) |
| `tests/helpers/gnome/test_apply_enabled_extensions.py::TestMain::test_falls_back_to_dbus_run_session_without_a_bus`                       | fixed (T3.2) |
| `tests/helpers/host_health/test_handoff.py::TestTheHandoffCanBeSuppressedForTriage::test_the_findings_are_still_reported_either_way`      | fixed (T1.3) |
| `tests/helpers/host_health/test_login_message.py::TestTheEntryPointALoginShellCalls::test_it_exits_zero_and_prints_nothing_when_clean`    | fixed (T1.3) |

All five are **defective tests**, not production paths. Run `35034834651` (`cedc9426`)
confirms the first two fixes on a real runner: `failures=5` became `failures=3`, with the
two `host_health` entries gone and nothing else changed.

The DisplayLink pair deliberately asserts against real sysfs — its own docstring says a
tempfile cannot reproduce the defect. It scans every `card*-*` connector and asserts
"connected and advertising modes ⇒ has EDID bytes". That inference holds for a connector
with a physical display link and is simply **false for a `Virtual` connector**, whose modes
are invented by the driver and which has no monitor to read an EDID from. A runner is a VM
whose one connected connector is `card1-Virtual-1`. Production never looks at these at all
— `_drm_head_states()` globs `card*-DVI-I-*` — so the unsound inference is the test's own.

The dbus test's first diagnosis was **wrong and is corrected here**: it *does* isolate
`DBUS_SESSION_BUS_ADDRESS`, because `mock.patch.dict(..., clear=True)` unsets it (measured:
`None` inside the patch). The real cause is one candidate further down. `runtime_dirs`
always appends `/run/user/<uid>` and deliberately never drops it — it is the path derived
from who the process actually is, so no environment change can remove it. On a runner
(uid 1001, a live user session) that socket is reachable, `resolve_session_bus` returns
`source="runtime-socket"`, and the fallback the test names is never reached. It passed in
the container only because the container runs as a uid with no session.

The two `host_health` failures were diagnosed and fixed under Task 1.3.

## Tasks

### Phase 1: Establish what is actually true

- [ ] ⬜ **Task 1.1**: `triage.bash` for this plan — one read-only script that reports, for
  the current checkout: each `qa-all.bash` stage's verdict, and for every stage that
  differs from the last CI run, the specific input it read that CI does not have. The
  point is a per-stage machine-dependence answer, not a pass/fail.
- [ ] ⬜ **Task 1.2**: Identify the commit that first turned CI red. The last green is
  `1fc1c5fe` (2026-08-26) and the `.claude/rules` pointers landed 2026-08-31, so at least
  one further cause existed between those dates. Name it; do not assume it is still live.
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
  gitignored tree. Options, with the argument for each recorded in `DECISIONS.md`:
  - **(a) Install the hooks daemon in CI before QA.** Most consistent with this repo's
    "Missing Dependencies — Fail Fast, Fix in IaC" rule: the daemon is a real dependency
    of the docs graph, so add the dependency rather than teach the check to tolerate its
    absence. Costs a CI step and makes CI depend on the daemon's installer.
  - **(b) Make the existence check conditional on the tree being present** — a link into a
    known installed tree is checked when the tree is there and not when it is not. Smaller,
    but it is the "skip and warn" shape `CLAUDE.md` prohibits, and it would pass on a
    genuinely broken link in exactly the environment that cannot check it.
  - **(c) Stop tracked files linking into the untracked tree** — the pointers name the
    topic file rather than deep-linking. Changes daemon-deployed content, which is
    replaced wholesale on upgrade, so it would regress at the next one.
- [ ] ⬜ **Task 2.2**: Implement the chosen option; the docs gate passes in a clean checkout.

### Phase 3: The five tests

- [x] ✅ **Task 3.1**: The DisplayLink pair — the scan now excludes DRM connector types
  that carry **no physical display link** (`Virtual`, `Writeback`), for which the test's
  inference was never sound. A denylist on purpose: an unrecognised type is asserted
  against, not skipped, so a linkless type nobody has met yet surfaces as a failure rather
  than as a test that quietly stopped checking. Falsified four ways — the pair still passes
  against this container's real `eDP-1`; reintroducing `os.path.getsize()` fails it 2/2; a
  `Virtual`-only tree skips and **names what it ignored**; and a `card1-DP-1` whose EDID
  reads zero still **fails**, which is the requirement this task was written around
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

### Phase 4: Make the next regression visible

- [ ] ⬜ **Task 4.1**: Decide and record why three weeks of red went unremarked, and what
  changes so it does not repeat. A red run that looks identical to the previous red run is
  the mechanism; any fix has to break that.
- [ ] ⬜ **Task 4.2**: `CLAUDE/QA.md` states which stages are environment-dependent and what
  each needs. Today the page asserts CI is the authority without qualification.

### Phase 5: Close

- [ ] ⬜ **Task 5.1**: `./scripts/qa-all.bash` green locally.
- [ ] ⬜ **Task 5.2**: The `QA` workflow green on `F44` — the run link is the evidence.
- [ ] ⬜ **Task 5.3**: `qa-reviewer` agent over the full diff.

## Success Criteria

- [ ] The `QA` workflow's most recent run on `F44` is a success, with the run identified.
- [ ] Local `qa-all.bash` and the CI run agree on every stage, or the disagreement is
  declared in `CLAUDE/QA.md` and fails closed when its reason stops applying.
- [x] Each of the five tests has been classified as a defective test or a production path
  reading unowned host state, and fixed accordingly.
- [ ] The docs gate passes in a checkout with no hooks daemon installed.
- [ ] A deliberately introduced failure is distinguishable from the standing state.

## Risks & Mitigations

- **Fixing the tests by weakening them.** The DisplayLink pair exists precisely because a
  tempfile-only suite passed while the defect was live; a skip that widens too far restores
  that hole. Mitigation: Task 3.1 requires the test to still fail on a real zero-read.
- **Option (b) in Task 2.1 passing on a genuinely broken link.** Mitigation: it is recorded
  as the weaker option, not the default.
- **Phase 3 turning into a rewrite of the helper suites.** Mitigation: Non-Goals — this
  plan restores the signal, it does not re-open what the gates check.

## Dependencies

- None blocking. Phase 3 Task 3.3 depends on Task 1.3.
- Plan 00113 (ccy CI runner) is adjacent, not a dependency: it concerns running ccy *in*
  CI, not the QA workflow that is red.

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
