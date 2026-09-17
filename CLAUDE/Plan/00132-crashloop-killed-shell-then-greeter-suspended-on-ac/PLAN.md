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

Two defects, one incident, and no detection of either: ten hours of runaway restarts
produced no signal anywhere.

Phases 1–4 are research and design and apply no fix. Phases 5–6 are the implementation,
handled by another agent. **Detection alone is not protection** — the report goes to a
logged-in human, and nobody was logged in; that is the incident's defining property. So
Phase 6 adds enforcement that acts without one.

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
- [x] ✅ **Task 2.3**: **Answered: no.** `session.conf` declares 1e9; the session broker runs at 1e14 and the system broker at 512 MiB — scope-dependent constants, not a reading of the XML. Editing `session.conf` would be an inert fix
- [x] ✅ **Task 2.4**: **Answered: no headroom figure exists to threshold against.** The 1e14 ceiling was never what broke; the binding limit is a per-peer receive share exposed through no flag, file or bus method. Quota-headroom monitoring rejected; the restart-rate proxy adopted

### Phase 3: Specify the defences (design, no implementation)

- [x] ✅ **Task 3.1**: **Specified.** One file, `/etc/dconf/db/gdm.d/NN-power`, setting `sleep-inactive-ac-type='nothing'` under `[org/gnome/settings-daemon/plugins/power]`, then `dconf update`. It belongs in `play-suspend-and-lid-policy.yml`, immediately before its `# ---- Verification, LAST ----` marker, which already owns host-scope power policy as root; `play-prevent-ssh-suspend.yml` is `become_user`-scoped and has no route to the `gdm` account. Read-back is `triage.bash` P3, which must move from `'suspend'` to `'nothing'`

- [x] ✅ **Task 3.2**: **Specified** in [research/greeter-power-policy.md](research/greeter-power-policy.md#the-read-back-specification-task-32) — the exact task to add to `play-prevent-ssh-suspend.yml`, why it must cross the same bus as the write, and why it compares against `'nothing'` with its quotes

- [x] ✅ **Task 3.3**: **Specified** in [research/detection-gap.md](research/detection-gap.md#the-confirmed-course-of-action-task-33). Extend **plan 00055's** watchdog (2-minute tick, attribution, report, notification), **not** `host-health-collect` — daily, and delivers at login, which this incident precedes. Signal: `RestartCount` delta per tick (≥10) plus an absolute floor (~1,000) for a loop already running at start-up. Measured separation: offender 124,873, next-highest 19, all others 0

- [x] ✅ **Task 3.4**: **Recorded.** Greeter: the read-back in Task 3.1 must move from `'suspend'` to `'nothing'` — a file that exists while the read-back still says `'suspend'` is a failed fix, not an applied one. Detection: the four-row falsification table in [research/detection-gap.md](research/detection-gap.md#how-it-gets-falsified-task-34), whose first row must be executed **while the loop is still live**

- [x] ✅ **Task 3.5**: **Recorded** in the 26-09-17 journal (09:34, with the 09:35 correction)
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

- [x] ✅ **Task 5.1**: **Ported.** `helpers/containerwatch/crashloop.py` is a pure module — no subprocess, no engine, so every branch is unit-testable — driven from `cli.py`'s scan. Tests were written first. Both gates carry the running-gate from Task 5.5. Podman **and** Docker are sampled, and `engine_coverage()` makes the report state which engines a verdict actually covers, so LXC is named as uncheckable rather than silently absent. A crash-loop finding gets its own allowlist matcher: the shared process-shaped one matches `cmd` with fnmatch, and `fnmatch("", "*")` is True, so an entry tuning one container's CPU noise would otherwise have muted every crash-loop alarm on the host. **Still open, carried into Task 5.3:** 00055's L3 panel/notification delivery leg remains unverified, and this defence depends on it

- [x] ✅ **Task 5.2**: **True positive captured and no longer losable.** `detect-crashloop.bash` ran against the live loop: 13 containers, **1 flagged**, **0 false positives**, both conditions firing independently; the nearest borderline case (19 lifetime restarts) correctly cleared. Evidence in [research/detection-gap.md](research/detection-gap.md#the-true-positive-captured). Done out of order because it was the only perishable step

- [x] ✅ **Task 5.3**: **Deployed, and the read-back proves it.** The greeter reads `'nothing'`; `triage.bash --expect-greeter nothing` exits 0 and `--expect-greeter suspend` exits 1, so the assertion distinguishes the fixed state from the broken one rather than merely being green. Deploying found two defects `--syntax-check` cannot see: `ansible_managed` is undefined in this repo (the copy task failed outright), and `become_user: gdm` inherited the invoking user's pyenv shim, which `gdm` cannot execute (rc 126, reported as a JSON deserialisation error naming neither the account nor the path)

- [x] ✅ **Task 5.6**: **`dconf update` is now gated on staleness, because the compile is not a no-op.** Applying the fix cost a live desktop: the compile rewrites **every** database under `/etc/dconf/db` — including `local` and `site`, which a logged-in user's profile stacks — so a change confined to `gdm.d` still notified the user's session, and GNOME Shell segfaulted handling it. The backtrace is unambiguous: `g_settings_backend_invoke_closure` → `settings_backend_path_changed` → `g_settings_real_change_event` → `update_clock()` in `libgnome-desktop-4`, reached through a `VOID__STRINGv` marshal. The crash is upstream and not ours; how often we hand it the opportunity is. `helpers/dconf/staleness.py` (tests first) compares each compiled database against the drop-ins it was built from, so the play still self-heals a database gone stale by any route — which `when: <copy> is changed` would not — while staying silent on a run that changed nothing

- [x] ✅ **Task 5.4**: **Added.** `play-prevent-ssh-suspend.yml` now reads the AC suspend key back as the target user across that user's own session bus — the same bus the write crossed, since a read over a different bus would vouch for a value the desktop never sees — and fails the run unless it is `'nothing'`, quotes included

- [x] ✅ **Task 5.5**: **Loop stopped and the true negative obtained.** `podman stop` was definitive (policy `unless-stopped`); churn fell from ~50 starts/40s to **zero** and stayed there. Final count 131,377. The re-run **found a defect**: `RestartCount` is cumulative, so the absolute test kept firing on a container already dealt with — a permanent false alarm. Fixed by gating the absolute test on the container actually running; the count is still reported, just not flagged. Detector now exits 0. **Task 5.1 must port the running-gate, not just the thresholds**

### Phase 6: Teeth — enforcement, because detection is not protection

Detection alone **would not have saved the session**: the report goes to a logged-in human
and nobody was logged in. Specified in
[research/detection-gap.md](research/detection-gap.md#teeth-detection-alone-would-not-have-saved-the-session).

- [ ] ⬜ **Task 6.1**: Automatic `podman stop` at **100 restarts within a rolling window** (windowed, not cumulative). ~85,000 restarts exhaust the quota, so 100 is **0.1% of the way to failure** and trips ~10 hours early
- [ ] ⬜ **Task 6.2**: Home it in **plan 00079**, not 00055 — whose D3 is reporting-only and gated. An orderly, signal-free `podman stop` is not what D3 rejected, but it still does not belong in that tree
- [ ] ⬜ **Task 6.3**: Probe whether `cgroup_manager = "cgroupfs"` removes the `libpod-*.scope` churn. **Hypothesis, not a recommendation** — must not reach a play before a read-back proves it moves the behaviour
- [ ] ⬜ **Task 6.4**: UID containment recorded as the structural option — the broker accounts per-UID and podman runs as the desktop's own UID. Strongest, most disruptive; recorded, not proposed

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
- [x] No fix was applied to the host or the playbooks **while planning-only mode was in force** — every change here landed after that mode was lifted and worktree execution was authorised. Restated rather than left ticked: as originally worded this criterion became false the moment Phase 5 shipped, and a tick against a false statement is worse than an unticked box

## Risks & Mitigations

| Risk                                                                                                       | Mitigation                                                                                                                    |
| ---------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| ~~Two earlier risks: an "inert" `gdm.d` drop-in, and a threshold set against an unknown quota~~            | **Both closed** — one disproven, one avoided. Tasks 2.1 and 2.4                                                               |
| Enforcement stops a container a human wanted running                                                       | Windowed threshold of 100 with ~1000x headroom to failure; `podman stop` is reversible and leaves the project's config intact |
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
