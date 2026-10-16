# Plan 00205: ssh team members

**Status**: In Progress (research done; owner decisions open, nothing built)
**Created**: 2026-10-16
**Owner**: joseph
**Priority**: Medium

## Overview

ccy-teams joins an agent user inside an LXC container to a team by reaching into the container
with `lxc-attach` as root on the host (`helpers/agent_bus_lxc/members.py` run by
`play-agent-bus.yml`, the entry written by `ccy-teams lxc-add`). Its Checkout step reads the
container's rootfs from the host as root (`derive.ROOT_READ_PY`, `lxcfreeze.Lister.ReadCheckout`).
The owner already reaches these containers with plain `ssh` as the right user, and wants to know
whether team members could be reached over ssh instead. The central question: with ssh, does the
tooling need to know how the member is virtualised at all (LXC, VM, another physical host)?

The research answer is no. Every in-member step is "run as root" or "run as the agent user, with
this stdin", which ssh does for any machine. The handle's type is reported by the member itself
(`pingbus suggest-handle`). The three LXC-only parts (the host-side rootfs read, `lxc-info` state
and autostart, the "bridge of this host" route check) each have a generic replacement or drop
out. Options, trade-offs, the recommendation and the owner's open questions are in
[PROPOSAL-ssh-members.md](PROPOSAL-ssh-members.md).

## Goals

- A clear, owner-decidable design for team members reached over plain ssh, generic to any
  ssh-reachable machine, not to one install's setup.
- The impact on members.py, the ccy-teams LXC flow, token delivery, the keeper and pingbus in the
  member, `service: true` units, and ssh auth, each stated with options.
- After the owner decides: a build plan (tests first) for the chosen option.

## Non-Goals

- Changing any code, playbook or container in this research phase.
- Fixing the `btrfs:` `lxc.rootfs.path` refusal (`helpers/agent_bus_lxc/derive.py` lines
  131-134); another agent owns that fix.
- Cross-machine teams over WireGuard (Plan 00161 D46's later phase); this plan depends on it for
  members on other physical hosts.
- The seat broker (Plan 00197); this plan is shaped so the broker can take over token delivery.

## Tasks

### Phase 1: research and proposal

- [x] ✅ **Task 1.1**: Read the LXC member flow (members.py, derive.py, `lxcfreeze` lister,
  `teams/lxcadd`, `play-agent-bus.yml`, the kit READMEs, docs/agent-bus\*.md, Plans 00161, 00177,
  00197, 00203) and map each step to its ssh equivalent.
- [x] ✅ **Task 1.2**: [PROPOSAL-ssh-members.md](PROPOSAL-ssh-members.md): the virtualisation
  question, impact per area, lxc-attach versus ssh, options O1-O4, recommendation (O2), open
  questions.

### Phase 2: owner decisions

- [ ] ⬜ **Task 2.1**: The owner answers the proposal's section 6 (entry shape, who runs ssh,
  root in the member, `service: true` shape, unreachable absent members, WireGuard dependency,
  relation to 00197, handle type).

### Phase 3: build plan (after Phase 2)

- [ ] ⬜ **Task 3.1**: SPEC with numbered expectations for the chosen option; Fable review.
- [ ] ⬜ **Task 3.2**: Tasks for the build (tests first), the ccy-teams flow, docs
  (`docs/agent-bus-lxc-members.md`, the kit READMEs), host acceptance through `meta-deploy.bash`,
  and the `qa-reviewer` pass.

## Dependencies

- Related: Plan 00161 (team bus design, D46), Plan 00177 (LXC handle derivation, Task 9.7),
  Plan 00197 (seat broker), Plan 00203 (keeper, `service: true`), Plan 00116 (forwarded agent).

## Success Criteria

- [ ] The owner has chosen an option and answered the open questions.
- [ ] The build plan exists and covers a generic ssh-reachable member.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00205-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Research and proposal committed on `plan/ssh-team-members`.
