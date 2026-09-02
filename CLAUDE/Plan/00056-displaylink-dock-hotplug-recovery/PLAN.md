# Plan 00056: displaylink dock hotplug recovery

**Status**: Dormant (blocked on Task 2.3: HOST deploy against a real DisplayLink dock, and on `gh` account access for issue #28)
**Created**: 2026-07-01
**Owner**: joseph
**Priority**: Medium

## Overview

This resurrects [GitHub issue #28](https://github.com/LongTermSupport/fedora-desktop/issues/28),
which was closed as "resolved: faulty monitor HDMI input port" but explicitly left
the door open: *"Reopen if a head wedges again with no hardware confound in
play."* The problem has recurred: unplugging and replugging the USB-C
DisplayLink dock confuses GNOME Shell / mutter (Wayland), and recovery
currently requires a full logout or reboot — which is highly disruptive
mid-session.

Issue #28's own diagnosis identified **two stacked host-side software
problems**, not a hardware fault this time:

1. **evdi/DisplayLink stack wedge** — a video head reports `connected` but
   never gets its EDID read; survives a service restart, dock power-cycle, and
   USB re-enumeration; historically only cleared by a full reboot (reloads the
   `evdi` kernel module).
2. **mutter monitor-manager state corruption** — hotplug churn leaves
   `meta_monitor_manager_get_logical_monitor_from_number` /
   `meta_workspace_get_work_area_for_monitor` assertions failing in the
   gnome-shell journal, and GNOME Settings > Displays refuses to apply *any*
   layout change ("Changes Cannot be Applied") until a logout/login.

The current playbook (`playbooks/imports/optional/hardware-specific/play-displaylink.yml`)
only has a suspend/resume watchdog (`displaylink-resume-check.sh`) that runs
`systemctl restart displaylink-driver.service` — which issue #28 already
established is **insufficient** (it restarts DisplayLinkManager but does not
reload the `evdi` kernel module, and does nothing for mutter's corrupted
state).

This plan's job is to research (online, ultracode multi-agent pass) whether a
genuinely reboot-free — or at least logout-free — recovery is possible, and if
so, implement it as an IaC-managed udev rule / systemd service / helper script
in the DisplayLink playbook.

## Goals

- Determine, with sourced evidence, whether the evdi/USB-side wedge can be
  recovered without a reboot (module reload feasibility on Wayland, USB
  unbind/rebind reset).
- Determine whether the mutter monitor-manager state corruption can be cleared
  without a full logout (Wayland has no X11-style "Alt+F2, r" shell restart).
- Implement whatever reboot-free/logout-free recovery is genuinely achievable,
  as an Ansible-managed udev rule + systemd service + helper script triggered
  on dock hotplug, replacing/extending the current service-restart-only
  watchdog.
- If a full fix is not possible, document precisely which half of the problem
  remains unresolved and why, so the next person doesn't re-litigate settled
  ground.

## Non-Goals

- Hardware replacement/troubleshooting (issue #28 already ruled out dock,
  cables, and this time is not chasing a monitor input-port fault).
- General DisplayLink driver upgrades unrelated to hotplug recovery.

## Context & Background

- Live diagnostics gathered 2026-07-01 (this system): mutter 50.1, gnome-shell
  50.2, kernel 7.0.12, evdi 1.14.16 (DKMS installed for 3 kernels), displaylink
  1.14.16-2. `/sys/class/drm/` currently shows **4** DisplayLink `DVI-I`
  connectors (card2-5) though the dock only has 2 physical video heads —
  card4/card5 are disconnected zombies from an earlier hotplug cycle that
  created fresh virtual DRM devices without cleaning up the old ones. This
  matches the playbook's own comment: *"EVDI recreates virtual DRM devices
  with new IDs, breaking `~/.config/monitors.xml` references."*
- Kernel log (`journalctl -k`) shows DisplayLinkManager cycling the two active
  heads (open/connect/disconnect/close) repeatedly during normal operation,
  including "Double connect - replacing ..." warnings — this project's
  DisplayLinkManager appears to reconnect somewhat aggressively even without a
  physical unplug, which may be a contributing factor to state churn.
- GitHub issue #28 is currently **closed**; reopening it requires a GitHub
  account with write access to `LongTermSupport/fedora-desktop` (the `gh`
  session's active account, `<gh-account>`, is pull-only on this repo) — blocked,
  see Notes.

## Research Findings

A 5-angle web research pass (6 agents, 550k tokens, 132 tool calls) produced a
clear, well-sourced answer:

**The evdi/USB wedge is reboot-free fixable.** In ascending order of
intrusiveness: restart `displaylink-driver.service`; force the dock's own USB
device to re-enumerate via the sysfs `authorized` toggle (does not disturb
sibling devices on the hub); reload the `evdi` kernel module — but only when no
DRM client (i.e. no active compositor session) holds `/dev/dri/cardN` open,
confirmed by evdi's own upstream issue tracker
([DisplayLink/evdi#540](https://github.com/DisplayLink/evdi/issues/540)).

**The mutter monitor-manager corruption is NOT reboot-free (or even
logout-free) fixable, on any current or near-future GNOME.** This is a
structural, upstream-acknowledged limitation, not a gap in this project's
tooling:

- `Meta.restart()` explicitly refuses under Wayland (`Meta.is_wayland_compositor()`
  guard, traced to a 2015 commit) — the mechanism that made X11's "Alt+F2, r"
  work does not exist for Wayland sessions.
- [GNOME/gnome-shell#5634](https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/5634)
  ("rework Shell's architecture to allow restarting under Wayland without
  killing spawned apps") is still open years after filing — no fix has landed.
- GNOME 50 (shipped by Fedora 44, this system) removed the X11 session backend
  from Mutter entirely — there is no more X11 fallback either.
- The live `org.gnome.Mutter.DisplayConfig` D-Bus interface (read directly from
  mutter's `main` branch XML) exposes only `GetResources`, `ApplyConfiguration`,
  `ApplyMonitorsConfig`, `GetCurrentState`, etc. — no reset/reprobe method.
- The exact assertions this system's earlier wedge hit
  (`meta_monitor_manager_get_logical_monitor_from_number`,
  `meta_workspace_get_work_area_for_monitor`) are independently reproduced
  across GNOME 41 through 50 in multiple upstream/downstream trackers
  ([mutter#1979](https://gitlab.gnome.org/GNOME/mutter/-/issues/1979),
  [mutter#3402](https://gitlab.gnome.org/GNOME/mutter/-/issues/3402),
  [gnome-shell#6001](https://gitlab.gnome.org/GNOME/gnome-shell/-/issues/6001),
  [Launchpad #2117277](https://bugs.launchpad.net/ubuntu/+source/mutter/+bug/2117277)).
  One specific NULL-logical-monitor crash pathway was fixed upstream
  (mutter MR !4549, landed before this system's 50.1), but maintainers
  characterize the underlying API problem (many callers, no redesign) as
  unresolved in general.

Full per-angle findings with URLs and confidence levels are in the workflow
transcript (run `wf_672f1753-dd8`); the summary above captures every
high-confidence claim.

## Design Decision: automate the fixable half only, never the unfixable half

**Decision**: implement automated recovery for the evdi/USB wedge (silent,
no session impact). For confirmed mutter corruption, only **notify** the user
that a logout is needed — never automate the logout itself.

**Context**: the first implementation draft included an opt-in
(`displaylink_recovery_force_logout`, default `false`) automated
`loginctl terminate-session` escalation for the mutter-corruption case, reasoning
that gating it behind a default-off flag made it safe.

**Why reversed**: the user rejected this immediately and correctly. A finding
of "no reboot-free fix exists" is something to *report*, not something to
route around by building a mechanism that kills every app in a session based on
a heuristic `journalctl` grep — regardless of whether it defaults on or off.
This is exactly the class of hard-to-reverse, session-visible action this
project's "Executing actions with care" principle says must stay a human
decision, not something IaC quietly offers as a toggle. The
`Action.FORCE_LOGOUT` enum member, `--force-logout` CLI flag, and
`_force_logout()` executor were removed entirely (not just defaulted off) —
see `helpers/displaylink_recovery/recovery.py` and its test suite.

**How to apply**: when a future recovery mechanism turns out to be genuinely
undoable/reboot-required, the correct scope is diagnosis + clear user
notification. Do not build an automated escalation path for it, opt-in or not.

## Tasks

### Phase 1: Research (ultracode multi-agent)

- [x] ✅ **Task 1.1**: Launch parallel research across 5 angles: mutter
  hotplug-corruption bug/fix status, evdi reload-without-reboot feasibility,
  USB unbind/rebind reset technique, Wayland-safe gnome-shell state reset, and
  community-reported workarounds for this class of problem.
- [x] ✅ **Task 1.2**: Synthesize findings into a ranked set of candidate
  recovery mechanisms with confidence levels (see Research Findings above).

### Phase 2: Design & Implement

- [x] ✅ **Task 2.1**: Implement staged evdi/USB recovery as a TDD'd helper
  (`helpers/displaylink_recovery/{recovery.py,run_recovery.py}` + 13 unit
  tests), deployed via `play-displaylink.yml`: a udev rule
  (`99-displaylink-dock-recovery.rules`) triggers a oneshot systemd service on
  dock USB add; the existing suspend/resume watchdog now calls the same
  tested logic instead of its old bash-only restart-and-hope script (removed).
  Mutter corruption is detected via recent-journal grep and only ever
  triggers a desktop notification — no automated logout (see Design Decision).
- [x] ✅ **Task 2.2**: Run QA: `./scripts/qa-all.bash` — passes (348 files);
  `./scripts/qa-helper-tests.bash` — passes (162 tests incl. the 13 new ones).
- [ ] 🔄 **Task 2.1-rework**: the sysfs-EDID-byte-count wedge heuristic in
  `helpers/displaylink_recovery/recovery.py` (`status=="connected" and edid_bytes==0`) is **confirmed wrong** by the 2026-07-01 live incident (see
  `live-incident-2026-07-01.md`) — a healthy, actively-in-use connector
  (`card3`) also reads 0 EDID bytes in steady state. Must be replaced with the
  kernel-log connect-sequence signal (`Connector state: connected` never
  followed by `Edid property set`), which the same incident's real log lines
  confirm as accurate. **Not yet deployed anywhere — caught in dry-run before
  reaching the live host, so no harm done, but do not deploy until reworked.**
- [ ] ⬜ **Task 2.3**: (On HOST) deploy the playbook and test against a real
  dock unplug/replug cycle.

### Phase 3: Close the loop

- [ ] ⬜ **Task 3.1**: Reopen/comment on GitHub issue #28 with the recurrence
  and the fix (or documented non-fix) — needs a GitHub account with write
  access; currently blocked on the active `gh` identity.

## Success Criteria

- [x] The evdi/USB-side wedge recovers without requiring a reboot (automated,
  silent). The mutter monitor-manager corruption does **not** have a
  reboot-free (or logout-free) fix — confirmed with sourced evidence above;
  the correct behaviour is notification, not automation.
- [x] `./scripts/qa-all.bash` passes for all playbook/script changes.
- [ ] Issue #28 reflects the current state (blocked on `gh` account access).

## Notes & Updates

### 2026-07-01

- Plan resurrected from the closed GitHub issue #28 stub
  (`untracked/Plan/displaylink-dead-head/PLAN.md`) after the user reported the
  wedge recurring on dock unplug/replug.
- Gathered live diagnostics (see Context).
- Attempted to reopen issue #28 — blocked: active `gh` account (`<gh-account>`) has
  only `pull` permission on `LongTermSupport/fedora-desktop`. Switching to a
  different stored `gh` identity was correctly denied by the sandbox as
  credential exploration beyond the current task's scope. **User action
  needed**: either switch the active `gh` account to one with write access
  (e.g. `gh auth switch --user <account>`) or reopen/comment on the issue
  manually.
- Launched and completed a 5-angle research workflow (Task 1.1/1.2) — see
  Research Findings.
- Implemented the evdi/USB recovery pipeline (Task 2.1), including a first
  draft with an opt-in automated forced-logout escalation for mutter
  corruption — the user rejected this on sight ("we can't fix mutter... so
  instead of confirming that, you're going to build an automated logout into
  the playbook?? hmm fuck no"). Removed entirely; see Design Decision section.
  This is the kind of correction worth remembering beyond this plan — recorded
  as a feedback memory too.
- QA and the helper test suite both pass. Deployment + real hardware test
  (Task 2.3) and the issue #28 update (Task 3.1) remain.
- Non-durable hourly failsafe recovery cron `953badbb` created for this
  session.
- **Live incident, ~15:20–15:26**: the wedge recurred for real, mid-session,
  while this plan was in progress — full blow-by-blow evidence (baseline
  state, two failed remediation attempts with exact kernel-log timestamps, the
  refcount-21 pre-flight check before attempting module reload) recorded in
  `live-incident-2026-07-01.md`. Key outcomes: service restart alone had no
  effect (matches issue #28); a scoped USB re-authorize on `/sys/bus/usb/devices/4-1.2`
  genuinely re-enumerated the device but the wedged head still never
  re-probed, proving the stuck state lives inside the `evdi` module itself, not
  USB enumeration; this also caught a real bug in the recovery helper's
  detection heuristic (flagged above as Task 2.1-rework) before it was ever
  deployed. Module-reload attempt (`modprobe -r evdi`) in progress at time of
  this note, result pending.
