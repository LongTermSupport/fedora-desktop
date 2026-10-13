# Plan 00186: Claude sessions inside LXC containers get the reboot warning and restore

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Low

Addresses #55.

## Overview

Claude sessions run in three places: as host `cc` sessions, in ccy (podman) containers, and
inside LXC system containers. The reboot and restore machinery from Plan 00135 knows only the
first two (#55). `ccy-sessions` finds sessions through the host user's tmux server, so a
session under an LXC container's own tmux is never listed. `reboot`, `reboot-with-update`
and `shutdown-with-update` therefore give it no warning, no "a restore will follow" or
"leave a handoff", and no cancellation, and the machine goes down under it mid-turn.
`ccy_restore_sessions` records and restarts only host tmux sessions. `lxcfreeze` pauses LXC
containers without telling the agents inside.

#55 leaves four questions open. Discovery: `lxc exec <ct> -- tmux list-sessions`, a
registry file inside the container, or the hooks daemon's session sidecars. Signalling: the
project's hooks-daemon CLI run inside the container, refusing the reboot when it cannot run,
as the ccy route does. Restore: whether, by whom, and how the opt-in reads per container.
Freeze: whether `lxcfreeze` warns first. The issue says "Not urgent; for later", so this plan
follows Plan 00135 and its first phase is research, not code.

## Goals

- A reboot or shutdown warns every Claude session inside a running LXC container, and
  withdraws the warning on cancel.
- A container whose daemon CLI cannot run refuses the reboot before any project is warned.
- Restore follows an opt-in decided per container.

## Non-Goals

- Changing the ccy or host `cc` paths beyond adding the LXC source.
- Docker or Podman inside LXC (Plan 00127).

## Tasks

### Phase 1: Research and decisions

- [ ] ⬜ **Task 1.1**: `triage.bash`: running LXC containers, which have tmux sessions running
  `claude`, and where each project's hooks-daemon CLI is inside them.
- [ ] ⬜ **Task 1.2**: Write the options for discovery, signalling, restore and freeze;
  owner decides each.

### Phase 2: Build (tests first)

- [ ] ⬜ **Task 2.1**: Discovery and signalling in `ccy-sessions`' reboot paths, in the style
  of `scripts/test-ccy-sessions-reboot.bash`.
- [ ] ⬜ **Task 2.2**: Restore per the decided opt-in; `lxcfreeze` per the freeze decision.
- [ ] ⬜ **Task 2.3**: Docs, CCY version bump, `deploy.bash`, `acceptance.bash`, an entry in
  `meta-deploy.bash`; `qa-all.bash`; the `qa-reviewer` agent.

## Success Criteria

- [ ] A reboot with a Claude session in an LXC container warns it, and a cancel withdraws it.
- [ ] That session's restore follows the decided opt-in.
- [ ] A test in the style of `test-ccy-sessions-reboot.bash` covers it.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00186-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #55.
