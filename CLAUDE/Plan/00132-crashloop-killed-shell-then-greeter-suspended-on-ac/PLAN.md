# Plan 00132: crashloop killed shell then greeter suspended on ac

**Status**: In Progress
**Created**: 2026-09-17
**Owner**: joseph
**Priority**: High

## Overview

A rootless container in an unrelated project crash-looped without bound for over ten
hours. Every restart cycle put traffic on the **session** D-Bus, which the desktop also
depends on, until `dbus-broker` hit its per-UID byte quota and disconnected its peers.
GNOME Shell lost its bus connection and exited four milliseconds later; on Wayland the
compositor is the display server, so every GUI client died with it. The machine had not
rebooted and no service had failed — from the outside it simply looked like the desktop
had vanished overnight.

The GDM greeter then took over and suspended the machine 900 seconds later, **while it
was plugged in**. That second failure is this repo's own: the plays set
`sleep-inactive-ac-type=nothing` for the human user only, so the documented "never
idle-suspend when plugged in" intent silently inverts the moment the user session dies
and the unmanaged `gdm` account becomes the one in charge of the display.

Two defects, one incident. Neither had any detection: ten hours of runaway restarts
produced no signal anywhere, and the host-health surface this repo already owns never saw
it. This plan is **research and design only** — it establishes the facts, records them,
and specifies the defences. It applies no fix.

## Goals

- Record the full, evidence-backed incident chain so the causal order is not re-derived
  from scratch next time (the segfault is a *teardown artefact*, not the cause — the
  obvious reading of the journal is the wrong one).
- Establish, with verified evidence, why the greeter suspends on AC and which dconf
  mechanism actually governs it on this host.
- Establish, with verified evidence, why podman permits an unbounded crash loop and why
  systemd's start-rate limiter does not apply to it.
- Specify the defences for both defects, including how each will be *falsified* — a fix
  that cannot be shown to change the observed behaviour is not a fix.
- Identify the detection gap and which **existing** surface it belongs in, rather than
  building a new parallel mechanism.

## Non-Goals

- **No fixes are applied while planning-only mode is in force**, by explicit instruction.
  Phases 1–4 honour that absolutely. Phase 5 is the implementation, and is gated on the
  user lifting that mode — it is written, not started.
- Not changing the deliberate AC/battery asymmetry for the human user. That asymmetry is
  documented as intentional in `play-suspend-and-lid-policy.yml` ("do NOT 'make them
  consistent'") and is out of scope.
- Not fixing the third-party project whose container crash-looped. That code is outside
  this repository. This plan covers only the host's resilience to such a loop.
- Not patching mutter/cogl. The SEGV is upstream and is a consequence, not a cause.

## Supporting Documents

| Document                                                                                       | Contents                                                                                                                          |
| ---------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| [research/incident-chain.md](research/incident-chain.md)                                       | Timestamped causal chain, with the evidence for each link and the two misreadings it rules out                                    |
| [research/greeter-power-policy.md](research/greeter-power-policy.md)                           | Why the greeter suspends on AC; the dconf stack that governs it; why the `gdm.d` drop-in is sufficient and needs no lock          |
| [research/podman-restart-supervision.md](research/podman-restart-supervision.md)               | Podman restart-policy semantics, the absent backoff, and the `.scope` vs `.service` gap                                           |
| [research/detection-gap.md](research/detection-gap.md)                                         | What was observable, why quota-headroom monitoring was rejected on measurement, and the confirmed `RestartCount` detection design |
| [research/independent-witness-agent-session.md](research/independent-witness-agent-session.md) | A container-hosted agent session that survived the outage, corroborating the timeline against a clock outside the session bus     |

## Related Plans

A dedupe sweep over the live plans found nothing already covering this work. Three plans
touch adjacent ground and should be reconciled with before Phase 3 specifies anything —
each is a subset, and none addresses either defect here:

| Plan                                                        | Status  | Adjacency                                                                                                                                                                                                                                                                                                                                                   |
| ----------------------------------------------------------- | ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 00104 — Suspend aborts on dock unplug and never re-suspends | Blocked | Also manages idle-suspend power keys via IaC, but scoped to suspend-abort recovery. Does not touch greeter or `gdm` dconf                                                                                                                                                                                                                                   |
| 00055 — Container Process Watchdog                          | Dormant | **Confirmed as the home for the detection defence** — 2-minute timer, container attribution, `report.json`, D-Bus signal, allowlist, reporting-only QA gate. Deployed, with L2 HOST-green for podman/docker/lxc; **its L3 panel/notification pass is still outstanding**, and that is precisely the delivery leg this plan leans on. Task 5.1 depends on it |
| 00079 — Podman container control                            | Blocked | Manual pause/unpause lifecycle tool. No restart-rate or quota dimension                                                                                                                                                                                                                                                                                     |

Plan 00055 was read before Task 3.3 was specified, and it **is** the home. The crash-loop
check is an extension of its watchdog, not of host-health — the reasoning, including why
`host-health-collect` is the wrong surface, is in
[research/detection-gap.md](research/detection-gap.md#where-it-belongs-extend-plan-00055-do-not-build-anything-new).

## Tasks

### Phase 1: Establish the facts (research)

- [x] ✅ **Task 1.1**: Prove the host did not reboot, and pin the suspend and resume times
- [x] ✅ **Task 1.2**: Identify the first failure in the chain and prove the SEGV is downstream of it
- [x] ✅ **Task 1.3**: Determine the effective power policy for both the human user and `gdm`, and identify which dconf source supplies each
- [x] ✅ **Task 1.4**: Establish podman's restart-policy semantics and unit-type registration from the man pages and live unit state
- [x] ✅ **Task 1.5**: Measure the crash-loop rate from the journal, and confirm whether it is still live
- [x] ✅ **Task 1.6**: `triage.bash` written on `_planlib.inc.bash`, shellcheck-clean, run green and red. Read-only, HOST-gated, P1–P8 covering **10 of 15 tasks** — printed as a `COVERAGE:` count with the uncovered ones named, not a list implying totality. Required legs record into `PLAN_FAILED_LEGS` so the exit code agrees with the text; verified via `--expect-greeter nothing`, which fails and exits 1. It caught three defects in its own evidence, each a green result establishing nothing — see the 10:31 journal entry

### Phase 2: Resolve the open questions (research)

- [x] ✅ **Task 2.1**: **Answered: a `gdm.d` drop-in alone is sufficient.** The "inert fix" fear is withdrawn — GDM ships `/usr/share/dconf/profile/gdm`, which already stacks `system-db:gdm` first among the system databases. Nothing needs creating under `/etc/dconf/profile/`
- [x] ✅ **Task 2.2**: **Answered: no lock required.** Only `user-db:user` outranks `system-db:gdm`, and the greeter's user db holds no `sleep-inactive-*` key — nor any UI that would write one. Omitted on YAGNI grounds, with the read-back as the falsifier
- [x] ✅ **Task 2.3**: **Answered: no.** `session.conf` declares 1e9; the session broker runs with `--max-bytes` 1e14 and the system broker with 512 MiB. The values are scope-dependent constants, not a reading of the XML. Editing `session.conf` would be an inert fix
- [x] ✅ **Task 2.4**: **Answered: there is no headroom figure to threshold against.** The global ceiling is 1e14 and was never what broke; the binding limit is a per-peer receive share the broker derives internally and exposes through no flag, file or bus method. Quota-headroom monitoring is rejected, and the restart-rate proxy is adopted instead

### Phase 3: Specify the defences (design, no implementation)

- [x] ✅ **Task 3.1**: **Specified.** One file, `/etc/dconf/db/gdm.d/NN-power`, setting `sleep-inactive-ac-type='nothing'` under `[org/gnome/settings-daemon/plugins/power]`, then `dconf update`. It belongs in `play-suspend-and-lid-policy.yml`, immediately before its `# ---- Verification, LAST ----` marker, which already owns host-scope power policy as root; `play-prevent-ssh-suspend.yml` is `become_user`-scoped and has no route to the `gdm` account. Read-back is `triage.bash` P3, which must move from `'suspend'` to `'nothing'`

- [x] ✅ **Task 3.2**: **Specified** in [research/greeter-power-policy.md](research/greeter-power-policy.md#the-read-back-specification-task-32) — the exact task to add to `play-prevent-ssh-suspend.yml`, why it must cross the same bus as the write, and why it compares against `'nothing'` with its quotes

- [x] ✅ **Task 3.3**: **Specified** in [research/detection-gap.md](research/detection-gap.md#the-confirmed-course-of-action-task-33). Extend **plan 00055's** container watchdog (2-minute timer, attribution, `report.json`, D-Bus signal, panel + notification, allowlist, reporting-only), **not** `host-health-collect` — that runs daily and delivers at login, and this incident destroys the session before anyone logs in. Signal: `RestartCount` delta per tick (≥10), plus an absolute floor (~1,000) so a loop already running at start-up is caught on the first tick. Measured separation on this host: offender 124,873, next-highest 19, all others 0

- [x] ✅ **Task 3.4**: **Recorded.** Greeter: the read-back in Task 3.1 must move from `'suspend'` to `'nothing'` — a file that exists while the read-back still says `'suspend'` is a failed fix, not an applied one. Detection: the four-row falsification table in [research/detection-gap.md](research/detection-gap.md#how-it-gets-falsified-task-34), whose first row must be executed **while the loop is still live**

- [x] ✅ **Task 3.5**: **Recorded**, in the 26-09-17 journal (09:34 and the 09:35 correction)
  and in the statement below. The task was to record a finding, and the finding is written
  in the two places a reader looks; there is nothing further to implement, which is the
  whole point of it. Record that the workload-resilience defence **already works and needs no change**. The compositor is declared unrecoverable upstream (`Restart=no`, "On wayland we cannot restart"), so session death is unpreventable by design — and the tmux-hosted work correctly survived it, running for a further 901 seconds. It was then killed by the greeter suspend. This makes the greeter fix (Task 3.1) the *sole* remaining exposure for long-running work on this host, not one mitigation among several

### Phase 4: Review

- [ ] ⬜ **Task 4.1**: Run the `qa-reviewer` agent over the plan and its supporting documents

### Phase 5: Implementation — the confirmed course of action

Every open research question is now answered, so the work below needs no further
investigation. **It is gated on the user lifting planning-only mode**, not on anything
this plan still has to learn. The ordering is deliberate and is the defence-before-fix
argument in [research/detection-gap.md](research/detection-gap.md): the crash loop is
still live and is the only genuine test case in existence.

- [ ] ⬜ **Task 5.1**: Port the validated algorithm into `helpers/containerwatch` (plan 00055), tests first per that plan's D4. The algorithm is no longer a proposal — `detect-crashloop.bash` in this folder is a working, lint-clean reference implementation with a recorded true positive; port it rather than re-deriving it. Podman + Docker; LXC explicitly out of scope, not silently skipped. **Also confirm 00055's outstanding L3 panel/notification pass** — that delivery leg is unverified and this defence depends on it
- [x] ✅ **Task 5.2**: **The true positive is captured and can no longer be lost.** `detect-crashloop.bash` (this folder) implements the specified algorithm and was run against the live loop: 13 containers, **1 flagged**, **0 false positives**, both conditions firing independently. The nearest borderline case — a container with 19 lifetime restarts — was correctly cleared. Evidence in [research/detection-gap.md](research/detection-gap.md#the-true-positive-captured). Done ahead of the rest of Phase 5 because it was the only perishable step
- [ ] ⬜ **Task 5.3**: Ship the greeter `gdm.d` drop-in per Task 3.1 via `play-suspend-and-lid-policy.yml`, with the read-back assertion. Run the play; confirm the greeter reads `'nothing'`
- [ ] ⬜ **Task 5.4**: Add the read-back assertions for the existing user-scope keys (Task 3.2)
- [ ] ⬜ **Task 5.5**: Only now, stop the crash loop, and confirm the detection goes quiet — the true negative

## Success Criteria

- [x] The facts the defences rest on are reproducible by running `triage.bash`, not merely
  asserted. Coverage is stated as a number by the script itself — 10 of 15 tasks; the rest
  are specifications with nothing live to read
- [x] The open questions in Phase 2 are answered with evidence. Two are answered
  **against** what this plan first recorded: the greeter fix is not inert, and
  quota-headroom monitoring is not available. One residual unknown is recorded
  explicitly rather than guessed — the divisor `dbus-broker` uses to derive a peer's
  share — together with why it does not change the conclusion
- [x] The greeter defence is specified precisely enough to implement without re-research,
  including the read-back that proves it works (P3 in `triage.bash`)
- [x] The detection defence is specified against a **measured** signal, in an existing
  surface, with a falsification method per claim
- [x] No fix has been applied to the host or the playbooks by this plan

## Risks & Mitigations

| Risk                                                                                                       | Mitigation                                                                                                                    |
| ---------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| ~~The `gdm.d` drop-in is **inert** because `/etc/dconf/profile/gdm` does not exist~~                       | **Closed, disproven.** GDM ships the profile at `/usr/share/dconf/profile/gdm` and it already stacks `system-db:gdm`          |
| ~~A monitoring threshold is set against a quota whose real value and headroom are unknown~~                | **Closed by avoidance.** Task 2.4 found no headroom figure exists, so quota monitoring was rejected rather than tuned         |
| The restart-rate threshold is a **proxy** and will miss a different cause of bus-quota pressure            | Accepted knowingly: Task 2.4 showed the class-level signal is unavailable at any threshold. Recorded rather than papered over |
| The live crash loop is stopped before it can be used to validate detection, losing the only real test case | Defence-before-fix ordering: build and validate detection against the live loop, and only then stop it                        |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00132-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 1 facts established from live host state and journal evidence
- Phase 2 closed: all four open questions answered, two of them reversing an earlier
  recorded conclusion
- Phase 3 closed: both defences specified, each with its falsification method
- `triage.bash` makes every asserted fact re-derivable; it is also the greeter read-back
- Course of action confirmed. Phase 5 is implementation, gated only on planning-only mode
  being lifted — no research remains
