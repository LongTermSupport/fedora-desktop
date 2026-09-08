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

### Decision 4: The goal is a durable suspend request, not a bounded failure — **supersedes Decision 1's framing**

**Context**: Decisions 1 and 2 both framed the problem as *recovering from* a failed suspend,
and argued over how long the machine may stay awake before a safety net catches it. The
operator rejected the premise outright:

> "there is no acceptable bag time — if i say suspend it should go for suspend. something as
> trivial as unplugging the power should not derail that."

That is the better frame, and it changes the design. The defect is not "the recovery is slow";
it is **that an explicit suspend request was silently not honoured**. A bounded idle timeout
answers the wrong question — it makes the failure shorter rather than making the request hold.

**Decision**: Three layers, in priority order.

1. **Remove the trigger.** F17 shows the AC adapter and both USB-C power-delivery ports ship
   armed as wakeup sources. Pulling the mains therefore fires a wake event, and one landing
   during the s2idle transition aborts the suspend. Disarm them by udev rule — waking a
   laptop is what the lid and the power button are for.
2. **Make the request durable.** A `system-sleep` hook re-issues the suspend when a resume
   happens within 30s of one with the lid still closed. This covers *any* wake source, not
   just the one we know about.
3. **Defence in depth.** GNOME idle-suspend on battery, as originally proposed — demoted from
   primary to backstop.

**What this reverses**: Decision 1 rejected the `system-sleep` hook as "the narrowest option,
covering strictly less". Under the old framing that was true. Under the correct framing the
hook is the layer that actually delivers the requirement, and the idle timeout is the one
covering less — it cannot honour a request, only curtail the consequences of dropping it.
Decision 1's *choice* of setting stands as layer 3; its *reasoning about priority* does not.

**Date**: 2026-09-08

### Decision 1: Restore the battery idle-suspend safety net as the primary fix

**Context**: Something must bound the damage from *any* failure to stay suspended, not just
this one trigger.

**Options considered**:

- **A — `sleep-inactive-battery-type=suspend` in the playbook.** One managed setting.
  Restores GNOME's own default (F11). Catches every variant regardless of what aborted the
  suspend, bounded by the idle timeout. **Known limitation, not an advantage**: while an
  inbound SSH session is established, `ssh-suspend-guard` holds a *block*-mode sleep
  inhibitor (F12), which disables this fix entirely for as long as the session lasts. An
  earlier revision of this plan credited that interaction to Option A as a benefit, which
  inverts what F12 actually says.
- **B — Disarm wakeup on the dock's USB tree.** Treats one trigger. `3-6` is not a stable
  identifier — it enumerated as a hub during the incident and as a keyboard afterwards (F5).
  Leaves every other wakeup source unhandled.
- **C — A `system-sleep` post-resume hook that re-suspends if the lid is still closed.**
  Custom code, and the *narrowest* of the three: it only fires after a resume, so it does
  nothing for H1, where no suspend was ever attempted.

**Decision**: **A**, as the primary fix. It is the smallest change, it restores a default
rather than inventing behaviour, and it is the only one of the three whose coverage does not
depend on which trigger fired. C was the first approach considered in session and is recorded
here because it is the tempting one and it is wrong as a primary: it is more code than A and
covers strictly less.

**Date**: 2026-09-08

### Decision 2: Treat the AC→battery state change separately, and gate it on H1

**Context**: A is bounded by the idle timeout, so a worst case still leaves the machine awake
in a bag for that timeout. Reacting to the actual state change would cut it to seconds.

**Options considered**:

- **A udev rule on `SUBSYSTEM=="power_supply"`** that, on AC going offline with the lid
  closed, triggers a oneshot unit that suspends. Event-driven, covers H1 and the incident
  case alike.
- **Shortening `sleep-inactive-battery-timeout`** instead. No new units, but it is a blunt
  trade against normal on-battery desk use.
- **Setting `HandleLidSwitchDocked=suspend`**. Rejected outright — it would suspend the
  docked workstation on lid close, which the Non-Goals forbid.

**Decision**: Deferred to a decision gate in Phase 3, **after** H1 and P1 are settled on the
host. Building a udev rule for a state transition that has not been demonstrated would be
speculative; if H1 is refuted, Phase 1 alone may be sufficient. Recording the option now so
the gate has something concrete to accept or reject.

**Date**: 2026-09-08

### Decision 3: The setting goes in `play-prevent-ssh-suspend.yml` — ⚠️ SUPERSEDED by Decision 4

> **Superseded.** Its *reasoning* holds and is why the move happened at all — a
> drift-prevention setting must live in a playbook that actually runs. Its *conclusion* does
> not: once Decision 4 added a udev rule and a `system-sleep` hook, the work needed a play
> that owns suspend policy, and `play-prevent-ssh-suspend.yml` is not that. The outcome is
> `imports/play-suspend-and-lid-policy.yml`, which satisfies Decision 3's actual requirement
> (imported by `playbook-main.yml`) without putting a udev rule in an SSH play.

**Context**: Two playbooks could plausibly own `sleep-inactive-battery-type` — the lid/power
one by topic, or the one that already sets its AC sibling.

**The deciding fact**: `play-laptop-lid-power-management.yml` is **imported by no playbook**.
It lives under `imports/optional/hardware-specific/` and `playbook-main.yml` does not pull it
in, so it only runs if invoked by hand. `play-prevent-ssh-suspend.yml` **is** imported, at
`playbook-main.yml:8`.

**Decision**: `play-prevent-ssh-suspend.yml`. Putting a drift-prevention setting into a
playbook that never runs would recreate exactly the failure being fixed — the setting would
be nominally IaC-managed and still absent from the host. Topical tidiness loses to actually
being deployed. It also puts the battery setting beside the AC sibling it must not disturb,
where the next reader sees both together.

**Follow-on**: that `play-laptop-lid-power-management.yml` is unimported is itself a latent
problem — the lid config it deploys (F7) is on this host but nothing would restore it. Out of
scope here; worth its own plan.

**Date**: 2026-09-08

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
    a resume lands within 30s of the suspend **and** the lid is still closed
  - [x] ✅ Async via `systemd-run` — an inline `systemctl suspend` deadlocks, because systemd
    waits for the hook before completing the resume
  - [x] ✅ Attempt cap (3) so a persistent waker cannot drive a hot suspend/resume loop
- [x] ✅ **Task 3.4**: Layer 3 — battery idle-suspend as backstop
  - [x] ✅ `sleep-inactive-battery-type=suspend`, guarded by `provisioning_profile != 'server'`
  - [x] ✅ `sleep-inactive-ac-type` left untouched

### Phase 4: Verify against the real failure (HOST)

- [ ] ⬜ **Task 4.1**: Deploy the playbook change on the HOST
- [ ] ⬜ **Task 4.2**: Reproduce the original incident and confirm recovery
  - [ ] ⬜ Suspend from the menu, unplug the dock within ~3s, leave the lid closed
  - [ ] ⬜ Confirm from the journal that the machine returns to sleep, and that a
    `PM: suspend entry` is followed by sustained journal silence
  - [ ] ⬜ Confirm the docked workstation case still does **not** suspend on lid close
- [ ] ⬜ **Task 4.3**: Run the `qa-reviewer` agent over the full plan diff (required)

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
- [ ] `sleep-inactive-battery-type` is set by a playbook and survives a re-run
- [ ] Lid close while docked on AC still does not suspend (Non-Goal preserved)
- [ ] `./triage.bash` runs clean on the HOST and its report agrees with `TRIAGE-EVIDENCE.md`
- [ ] QA passes (`./scripts/qa-all.bash`)
- [ ] `qa-reviewer` run over the plan diff with no BLOCK or FIX-BEFORE-MERGE findings

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
