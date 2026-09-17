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
- Identify the detection gap and where it belongs in the existing host-health surface,
  rather than as a new parallel mechanism.

## Non-Goals

- **No fixes are applied by this plan.** Planning only, by explicit instruction.
- Not changing the deliberate AC/battery asymmetry for the human user. That asymmetry is
  documented as intentional in `play-suspend-and-lid-policy.yml` ("do NOT 'make them
  consistent'") and is out of scope.
- Not fixing the third-party project whose container crash-looped. That code is outside
  this repository. This plan covers only the host's resilience to such a loop.
- Not patching mutter/cogl. The SEGV is upstream and is a consequence, not a cause.

## Supporting Documents

| Document                                                                                       | Contents                                                                                                                      |
| ---------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| [research/incident-chain.md](research/incident-chain.md)                                       | Timestamped causal chain, with the evidence for each link and the two misreadings it rules out                                |
| [research/greeter-power-policy.md](research/greeter-power-policy.md)                           | Why the greeter suspends on AC; dconf scope findings; why the obvious fix would be inert                                      |
| [research/podman-restart-supervision.md](research/podman-restart-supervision.md)               | Podman restart-policy semantics, the absent backoff, and the `.scope` vs `.service` gap                                       |
| [research/detection-gap.md](research/detection-gap.md)                                         | What was observable, what existing surface should have caught it, and the D-Bus accounting interface found to be available    |
| [research/independent-witness-agent-session.md](research/independent-witness-agent-session.md) | A container-hosted agent session that survived the outage, corroborating the timeline against a clock outside the session bus |

## Related Plans

A dedupe sweep over the live plans found nothing already covering this work. Three plans
touch adjacent ground and should be reconciled with before Phase 3 specifies anything —
each is a subset, and none addresses either defect here:

| Plan                                                        | Status  | Adjacency                                                                                                                                                                       |
| ----------------------------------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 00104 — Suspend aborts on dock unplug and never re-suspends | Blocked | Also manages idle-suspend power keys via IaC, but scoped to suspend-abort recovery. Does not touch greeter or `gdm` dconf                                                       |
| 00055 — Container Process Watchdog                          | Dormant | Reporting-only detection of container processes harming the host, for CPU-pinned processes. Nearest existing home for crash-loop detection — check before building anything new |
| 00079 — Podman container control                            | Blocked | Manual pause/unpause lifecycle tool. No restart-rate or quota dimension                                                                                                         |

Plan 00055 in particular is worth reading before Task 3.3: if it already establishes a
container-observation mechanism, the crash-loop check may be an extension of that rather
than of host-health.

## Tasks

### Phase 1: Establish the facts (research)

- [x] ✅ **Task 1.1**: Prove the host did not reboot, and pin the suspend and resume times
- [x] ✅ **Task 1.2**: Identify the first failure in the chain and prove the SEGV is downstream of it
- [x] ✅ **Task 1.3**: Determine the effective power policy for both the human user and `gdm`, and identify which dconf source supplies each
- [x] ✅ **Task 1.4**: Establish podman's restart-policy semantics and unit-type registration from the man pages and live unit state
- [x] ✅ **Task 1.5**: Measure the crash-loop rate from the journal, and confirm whether it is still live
- [ ] ⬜ **Task 1.6**: Write `triage.bash` on `_planlib.inc.bash` so every fact above is re-derivable on demand rather than quoted from one session

### Phase 2: Resolve the open questions (research)

- [ ] ⬜ **Task 2.1**: Determine empirically whether creating `/etc/dconf/profile/gdm` plus a `gdm.d` db changes the greeter's *effective* value — read back as `gdm` and compare. This is the question that decides Phase 3's whole approach
- [ ] ⬜ **Task 2.2**: Determine whether a dconf **lock** is required, or whether a db entry alone is sufficient given `gdm` has no competing user-dconf value for this key
- [ ] ⬜ **Task 2.3**: Determine whether `dbus-broker`'s per-UID `--max-bytes` quota derives from the `session.conf` XML limits. The launcher's man page hedges ("Nearly all of the configuration attributes are supported") and names none, so this is unverified either way
- [ ] ⬜ **Task 2.4**: Read the actual per-UID quota and current headroom via the D-Bus accounting interface, to establish what a monitoring threshold would even be measured against

### Phase 3: Specify the defences (design, no implementation)

- [ ] ⬜ **Task 3.1**: Specify the greeter power-policy change: exact files, the play it belongs in, and the read-back assertion that proves it took effect
- [ ] ⬜ **Task 3.2**: Specify a read-back assertion for the *existing* user-scope power keys. Neither play currently re-reads what it set, which is why this gap survived
- [ ] ⬜ **Task 3.3**: Specify the crash-loop detection defence as an extension of the existing host-health surface, not a new parallel mechanism
- [ ] ⬜ **Task 3.4**: Record the falsification method for each defence — what observation would show it does *not* work
- [ ] ⬜ **Task 3.5**: Record that the workload-resilience defence **already works and needs no change**. The compositor is declared unrecoverable upstream (`Restart=no`, "On wayland we cannot restart"), so session death is unpreventable by design — and the tmux-hosted work correctly survived it, running for a further 901 seconds. It was then killed by the greeter suspend. This makes the greeter fix (Task 3.1) the *sole* remaining exposure for long-running work on this host, not one mitigation among several

### Phase 4: Review

- [ ] ⬜ **Task 4.1**: Run the `qa-reviewer` agent over the plan and its supporting documents

## Success Criteria

- [ ] Every factual claim in the supporting documents is reproducible by running
  `triage.bash`, not merely asserted
- [ ] The open questions in Phase 2 are answered with evidence, or explicitly recorded as
  still-unknown with the reason
- [ ] The greeter defence is specified precisely enough to implement without re-research,
  including the read-back that proves it works
- [ ] No fix has been applied to the host or the playbooks by this plan

## Risks & Mitigations

| Risk                                                                                                                                    | Mitigation                                                                                             |
| --------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| The obvious greeter fix (`gdm.d` file alone) is **inert** — `/etc/dconf/profile/gdm` does not exist, so the db may never be read        | Task 2.1 verifies by read-back before anything is specified as the fix                                 |
| A monitoring threshold is set against a quota whose real value and headroom are unknown, producing either false alarms or false comfort | Task 2.4 measures the real accounting figures first                                                    |
| The live crash loop is stopped before it can be used to validate detection, losing the only real test case                              | Defence-before-fix ordering: build and validate detection against the live loop, and only then stop it |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00132-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 1 facts established from live host state and journal evidence
