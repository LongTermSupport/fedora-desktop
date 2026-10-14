# Plan 00197: agent bus seat broker daemon

**Status**: In Progress (design written; Fable review and owner decisions next)
**Created**: 2026-10-14
**Owner**: joseph
**Priority**: High

## Overview

A seat on a team is created today by whoever holds host sudo: the `ccy` launcher (on the host)
runs `sudo agent-bus add-member`, and ccy-teams does the same from its menu. An LXC container's
agent has no host sudo, so it joins only through a hand-written `agent_bus_lxc_members` entry
in `localhost.yml` and a root play run. The owner wants any place that can reach the team's
homeserver to ask for a seat itself: inside an LXC container, `ccy --teams` works the same as
it does on the desktop with podman.

The broker is a host daemon beside the homeserver that owns seat changes. A client asks for a
seat; the owner approves it in the team's Element room or in ccy-teams; only then does the
broker mint the seat, and the client collects its token with a secret it alone holds (the
shape of `gh auth login`'s device flow). Reaching the broker is never enough: a network
allowlist names addresses, not who is asking, and every process in every container on the
bridge can reach it.

Seats stay runtime state, not declared config. The registry under `/var/lib/agent-bus/<team>/`
and the homeserver's database already survive a reboot. The broker never writes
`localhost.yml`: a network-reachable service must not edit the file that drives root Ansible
runs. It keeps an append-only audit log beside the registry (who asked, what the owner approved
or refused, when). `localhost.yml` keeps declared config only: teams, their humans, admitted
ranges, the approval policy.

## Decisions (owner, 2026-10-14)

- D1: every new container and checkout needs the owner's approval. Keep the door open to a
  declared IP range that is approved without asking (a later policy option, not built now).
- D2: the broker handles seats and roles only (create, change role, remove). Creating a team
  stays a desktop action, `ccy-teams create`, because it changes the homeserver itself.

## Goals

- `ccy --teams` inside an LXC container asks the broker for a seat and starts once the owner
  approves; no `localhost.yml` edit, no root play run per member.
- One code path for seat changes: ccy-teams and the launcher on the desktop use the broker too.
- The trust boundary holds: no seat without the owner's approval; the token only to the
  requester; a refused or expired request leaves nothing behind.
- Hot-adding a running ccy session (Plan 00177's hot-add design) is the same flow started from
  ccy-teams.

## Non-Goals

- Creating or deleting teams through the broker (D2).
- Automatic approval by IP range (D1 keeps it open; not built here).
- Installing the member kit inside a container: the repository that provisions the container
  does that, as today.

## Related

- Backing up `localhost.yml` to the private config repo automatically is GitHub issue #104,
  not this plan.

## Tasks

### Phase 1: Design

- [x] ✅ **Task 1.1**: [DESIGN.md](DESIGN.md): the API (request, poll, approve, refuse, revoke), the pairing
  code and request secret, where it listens (only the bridge interfaces a team admits), the
  service user and what it may touch (the registration secret, the registry; not root), the
  approval surfaces (Element room message from the team's admin account, ccy-teams list), the
  audit log, expiry, rate limits, and how `agent_bus_lxc_members` is retired. The handle is
  derived from the checkout by ccy's rule (owner direction). Its section 13 holds 13 owner
  decisions; its section 14 is the task list that replaces Phases 2 and 3 once reviewed.
- [ ] ⬜ **Task 1.2**: Fable adversarial review of the design; findings fixed.

### Phase 2: Build (Go, spec first, tests first)

- [ ] ⬜ **Task 2.1**: SPEC with numbered expectations; the broker in `src-go`, its systemd
  unit and service user installed by `play-agent-bus.yml`.
- [ ] ⬜ **Task 2.2**: The client side: `ccy --teams` in a container, and ccy-teams on the host,
  through the broker.
- [ ] ⬜ **Task 2.3**: The approval surfaces: the Element message and the ccy-teams list.

### Phase 3: Cut-over

- [ ] ⬜ **Task 3.1**: LXC members join through the broker; `agent_bus_lxc_members` and the
  `ccy-teams lxc-add` action retired.
- [ ] ⬜ **Task 3.2**: Host acceptance through `meta-deploy.bash`; qa-reviewer over the plan.

## Success Criteria

- [ ] In an LXC container with the kit, `ccy --teams` gets a seat after one approval, with no
  config edit and no play run.
- [ ] A request the owner did not approve never yields a seat or a token.
- [ ] Seats, roles and the audit log survive a reboot.

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
