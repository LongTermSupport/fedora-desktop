# Plan 00104: Suspend aborts on dock unplug and never re-suspends

**Status**: In Progress
**Created**: 2026-09-08
**Owner**: joseph
**Priority**: High

## Overview

The laptop was suspended from the GNOME menu, undocked, and put in a rucksack. The suspend
was entered and then aborted three seconds later by the USB disconnect storm from unplugging
the dock. Nothing re-suspended it, so it ran at full load in a sealed bag for 25 minutes and
came out very hot, requiring a forced power-off.

The abort itself is ordinary — a wakeup event during the s2idle transition will do that. The
defect is that **nothing recovered from it**. `systemd-logind` handles the lid as an *event*,
not a *state*, so with the lid already closed there was no new transition to act on. The one
mechanism that would have caught it regardless of cause — GNOME's idle-suspend — is disabled
on both power sources, and the battery half of that is unmanaged drift rather than a
deliberate decision.

Because this machine only offers `s2idle` (no deep S3), "failed to suspend" means a running
CPU with peripherals partly live, which is why it produced real heat rather than merely
draining the battery.

Full evidence, with facts numbered F1–F17 and their sources: **[TRIAGE-EVIDENCE.md](TRIAGE-EVIDENCE.md)**.

## Goals

- The laptop reliably ends up asleep when it is closed and carried, whatever aborted the
  first suspend attempt.
- `sleep-inactive-battery-type` becomes IaC-managed, so the safety net cannot silently drift
  away again.
- The recovery behaviour is verified by reproducing the incident, not assumed from config.

## Non-Goals

- **Not** changing the docked-workstation behaviour. Lid closed on AC with external displays
  must continue **not** to suspend (F7/F8) — that is deliberate and relied upon.
- **Not** re-enabling idle-suspend on AC. `sleep-inactive-ac-type=nothing` is set on purpose
  by `play-prevent-ssh-suspend.yml` so inbound SSH survives (F11).
- **Not** pursuing hibernation. Kernel lockdown restricts it on this host (observed
  repeatedly in the incident journal) and enabling it is a separate concern.
- **Not** disarming USB/Thunderbolt wakeup as the primary fix — see Decision 2.

## Context & Background

The confirmed causal chain, and the two hypotheses (H1, H2) and three unverified premises
(P1–P3) that remain open, are recorded in [TRIAGE-EVIDENCE.md](TRIAGE-EVIDENCE.md). The most
consequential open item is **H1**: the same failure should occur with *no* suspend attempt at
all — simply undocking a lid-closed machine and walking away. If H1 holds, the aborted
suspend was a trigger rather than the root cause, and the fix must address the state change
rather than the resume path.

**Execution split.** Editing and committing happen in the CCY container; every probe, deploy
and acceptance run happens on the HOST (`CLAUDE/ContainerRules.md`). `triage.bash` enforces
this with `plan_require_host`. This is why the evidence is transcribed into a committed,
sanitised document rather than left in the gitignored run directory — the container cannot
read the host journal.

## Technical Decisions

Four decisions, with the reasoning and the options rejected, live in
**[DECISIONS.md](DECISIONS.md)** — extracted so this document stays lean.

| #   | Decision                                                     | Status                                           |
| --- | ------------------------------------------------------------ | ------------------------------------------------ |
| 1   | Restore the battery idle-suspend safety net                  | Setting stands; its **priority** superseded by 4 |
| 2   | Treat the AC->battery state change separately                | Open; gated on H1                                |
| 3   | Put the setting in `play-prevent-ssh-suspend.yml`            | **Superseded by 4** (reasoning survives)         |
| 4   | The goal is a durable suspend request, not a bounded failure | **Current**                                      |

## Tasks

### Phase 1: Capture the baseline (HOST)

- [x] ✅ **Task 1.1**: Establish the facts from the incident journal
  - [x] ✅ Transcribe the evidence, sanitised, into `TRIAGE-EVIDENCE.md`
  - [x] ✅ Confirm GNOME does not hold `handle-lid-switch`, so logind owns the lid (F9)
  - [x] ✅ Confirm `sleep-inactive-battery-type` is absent from the repo (F11)
- [ ] ⬜ **Task 1.2**: Run `./triage.bash` on the HOST to capture a reproducible baseline
  - [ ] ⬜ Confirm the report reproduces F4, F5, F7, F9, F10 from live state
  - [ ] ⬜ Note any divergence from `TRIAGE-EVIDENCE.md` in the JOURNAL

### Phase 2: Settle the open hypotheses (HOST)

- [ ] 🔄 **Task 2.1**: Test **H1** — does anything re-evaluate on an AC change?
  - [x] ✅ Build the probe: `./triage.bash --watch-power` records logind/upower/kernel
    output across a live mains unplug and replug. No dock needed — with no external
    displays the lid branch is decided purely by AC state, so the mains cord is a
    sufficient and far less disruptive test than docking.
  - [x] ✅ Run it, lid **open**, while working. Result: `systemd-logind` logged **nothing**
    across a full unplug/replug, verified against a positive control (F16).
  - [x] ✅ H1's **mechanism** confirmed and recorded as F16; H1 itself updated.
  - [ ] ⬜ **Deferred** (needs an idle moment, not a working session): the lid-closed half
    — close the lid on AC, unplug, and observe whether it ever suspends
- [ ] ⬜ **Task 2.2**: Settle **P1** — does logind count evdi/DisplayLink outputs as displays?
  - [x] ✅ Establish how to measure it: logind's `Docked` property and the connected-output
    count are both readable, and `./triage.bash` now captures them (F14). `systemctl show`
    does **not** expose the `Handle*` settings — that route is a dead end.
  - [ ] ⬜ Re-run `./triage.bash` with the dock **attached** and compare against the detached
    baseline in F14 (`Docked: false`, 1 connected output)

### Phase 3: Implement the fix (CCY container: edit + commit)

Implements Decision 4's three layers. Superseded Decision 3's placement: the setting no
longer goes in `play-prevent-ssh-suspend.yml`, because layers 1 and 2 need a home that owns
suspend policy, and splitting the three layers across two plays would be worse than moving
one file.

- [x] ✅ **Task 3.1**: Give suspend policy a playbook that actually runs

  - [x] ✅ `git mv` the unimported `optional/hardware-specific/play-laptop-lid-power-management.yml`
    → `imports/play-suspend-and-lid-policy.yml`. `playbook-main.yml:3-4` forbids importing
    anything from `imports/optional/`, so importing it in place was not an option.
  - [x] ✅ Import it from `playbook-main.yml`, adjacent to `play-prevent-ssh-suspend.yml`
  - [x] ✅ Update `docs/playbooks.md` — move the entry out of the optional catalogue

- [x] ✅ **Task 3.2**: Layer 1 — disarm power-delivery wakeup sources (F17)

  - [x] ✅ `files/etc/udev/rules.d/99-suspend-wakeup-policy.rules`, matching on
    `SUBSYSTEM`+`KERNEL` so it cannot drift onto the wrong device
  - [x] ✅ Deliberately leave USB/Thunderbolt armed — waking from an attached keyboard is
    wanted, and layer 2 covers an abort from that direction

- [x] ✅ **Task 3.3**: Layer 2 — re-issue an aborted suspend

  - [x] ✅ `files/usr/lib/systemd/system-sleep/resuspend-aborted-suspend`: re-suspends when
    a resume lands within 10s of the suspend **and** the lid is still closed (window derived
    from F1's measured ~3s abort; ACPI is read before logind to avoid a stale-cache race)
  - [x] ✅ Async via `systemd-run` — an inline `systemctl suspend` deadlocks, because systemd
    waits for the hook before completing the resume
  - [x] ✅ Attempt cap (3) so a persistent waker cannot drive a hot suspend/resume loop

- [x] ✅ **Task 3.4**: Layer 3 — battery idle-suspend as backstop

  - [x] ✅ `sleep-inactive-battery-type=suspend`, gated on a `gsettings get` **probe**
    (`gnome_power_schema.rc == 0`), not on the profile. The profile still decides whether the
    probe runs at all — that is a policy question ("should this host have a GUI?"), not a
    measurable fact, so it is a legitimate use of it. A failing probe on a non-server host
    now **fails the run** rather than silently skipping.
  - [x] ✅ `sleep-inactive-ac-type` left untouched

- [x] ✅ **Task 3.5**: Make the play safe on every host it now runs on

  - [x] ✅ Preflight block establishes measured preconditions: sleep capability, upower
    presence, the systemd sleep-hook directory. The GNOME schema probe sits with the
    layer-3 task it gates, **not** in preflight, so both the probe and its hard fail land
    beside the thing that could not be set.
  - [x] ✅ `meta: end_host` (not `end_play`) when the host cannot sleep — per-host, so one
    non-suspending machine cannot cancel the policy for the rest of the group
  - [x] ✅ The sleep-hook-directory precondition moved into preflight, so that abort happens
    before anything is written rather than part-way through
  - [x] ✅ Verification runs **last**, so a failing check can never abort the run before
    layers 2 and 3 are installed — a verification must not be able to prevent the fix it
    verifies

- [x] ✅ **Task 3.6**: Verify layer 1 applied, rather than asserting it

  - [x] ✅ `helpers/suspend_wakeup/` — tested helper enumerating the power_supply devices the
    udev rule targets; prints `COVERAGE: n of m`, and a host with none passes and says so
  - [x] ✅ Unit tests, stdlib `unittest`, covering both the pure verdict and the sysfs read;
    `./scripts/qa-helper-tests.bash` green. No count recorded here — counts go stale, the
    gate is the source of truth
  - [x] ✅ Runs as a task after `meta: flush_handlers`, so it executes on **every real** run —
    as a handler it only ran on the run that changed the rules file. It is skipped under
    `--check`, where sysfs has not been written and the answer would be meaningless

### Phase 4: Verify against the real failure (HOST)

- [x] ✅ **Task 4.1**: Deploy the playbook change on the HOST
  - [x] ✅ `ok=18 changed=6 failed=0 skipped=2`; `COVERAGE: 3 of 3 power-delivery devices disarmed`, against `0 of 3` captured before the run
  - [x] ✅ All three layers confirmed on the live system independently of the play's own
    report: the three wakeup attributes read `disabled`, the hook is installed root-owned
    0755, `sleep-inactive-battery-type` is `'suspend'` and the AC sibling is untouched
  - [x] ✅ No reboot required — the logind drop-in was already correct, so
    `warn-reboot-required` did not fire
- [ ] ⬜ **Task 4.2**: Reproduce the original incident and confirm recovery
  - [ ] ⬜ Suspend from the menu, unplug the dock within ~3s, leave the lid closed
  - [ ] ⬜ Confirm from the journal that the machine returns to sleep, and that a
    `PM: suspend entry` is followed by sustained journal silence
  - [ ] ⬜ Confirm the docked workstation case still does **not** suspend on lid close
- [x] ✅ **Task 4.3**: Run the `qa-reviewer` agent over the full plan diff (required)
  - [x] ✅ Eight rounds. Rounds 5-8 each returned no blocking findings; round 8 returned no
    should-fix findings either and confirmed behaviour preservation at bytecode level with
    a negative control. Reports in `subagent-reports/`

## Dependencies

- Does **not** modify `play-prevent-ssh-suspend.yml` — an earlier revision of this plan said
  it would, but the implementation went into `play-suspend-and-lid-policy.yml` instead
  (Decision 4). That play still owns the deliberate AC-side setting (F11) and must not be
  disturbed by this work.
- Interacts with `ssh-suspend-guard`'s block-mode sleep inhibitor (F12): while an inbound SSH
  session is established, **no** sleep can fire — including layer 2's re-suspend and layer 3's
  idle-suspend. The layer 2 hook passes `--collect` so that refusal cannot leave a failed
  transient unit wedging every later retry.

## Success Criteria

- [ ] Reproducing the incident leaves the machine **asleep**, verified from the journal
- [x] `sleep-inactive-battery-type` is set by a playbook and survives a re-run — second run
  is `ok=15 changed=0`, no handlers fired, `COVERAGE: 3 of 3` still holds
- [ ] Lid close while docked on AC still does not suspend (Non-Goal preserved)
- [ ] `./triage.bash` runs clean on the HOST and its report agrees with `TRIAGE-EVIDENCE.md`
- [ ] QA passes (`./scripts/qa-all.bash`) — **blocked, not passed**: `qa-all.bash` exits at
  `qa-python`'s rc=2 (ruff pin `0.16.0` vs `0.16.3` installed) and never reaches the five
  later gates. Those five were run individually and are green. Needs its own plan
- [x] `qa-reviewer` run over the plan diff with no BLOCK or FIX-BEFORE-MERGE findings —
  eight rounds; rounds 5-8 all clean of blockers, round 8 clean of should-fixes too

## Risks & Mitigations

| Risk                                                                             | Impact | Probability | Mitigation                                                                                                                             |
| -------------------------------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Enabling battery idle-suspend interrupts long unattended on-battery work         | M      | M           | `ssh-suspend-guard` already blocks sleep during SSH sessions (F12); the timeout stays at the existing 900s rather than being shortened |
| A `block` SSH inhibitor left held by a stuck session re-creates the bag scenario | H      | L           | Phase 2 should check whether the guard can hold a lock with no live session; note in JOURNAL if so                                     |
| P1 wrong → the fix targets the wrong logind branch                               | M      | M           | Task 2.2 settles which branch applies before Decision 2 is closed                                                                      |
| Reproduction test overheats the laptop again                                     | M      | L           | Reproduce on a desk, lid closed but not enclosed, and abort by opening the lid                                                         |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00104-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed with sanitised triage evidence and re-runnable `triage.bash`
