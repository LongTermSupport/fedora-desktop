# Plan 00196: team coordinator role

**Status**: In Progress
**Created**: 2026-10-14
**Owner**: joseph
**Priority**: Medium

## Overview

A team has three kinds of member: normal agents (workers), humans, and one agent the owner
nominates as the **coordinator**. Today the team record knows `orchestrator` and `worker`; this
plan renames the first to `coordinator` (the word agents use naturally), allows at most one per
team, and makes the coordinator part of the heads-up traffic: a worker can address it with
`--to-coordinator` on `send` and `say`, and the coordinator is **copied automatically** on
everything a worker sends to a human (free text and pings), marked as a copy, without the worker
having to remember. A human may message the coordinator ("get this done") and the coordinator
dispatches to workers; humans can still message workers directly.

The copy needs no new wire kind. `pingbus` adds the coordinator to the `to` (and so to
`m.mentions`) of a worker's text or ping that already names a human. Every receiving check
already accepts that shape; the receiver derives "this is a copy" from `to` naming a listed
human beside it. The protocol stays version 1, in the same way `org` and `exclude` did: a
member built earlier reads a team record that writes `coordinator` as untrusted, so every member
takes the current pingbus before a team names a coordinator.

On the host side, `ccy-teams` shows the coordinator on the board and gains "Make coordinator" /
"Stop being coordinator" in the seat row menu, and bare `ccy --teams` (choose team, choose
seat) also offers the role. The role lives in the team record, so Quick Launch reuses the seat
(LAST_TEAMS) and the role comes back with it. The launcher is on the bash-limits ratchet and
must not grow: the logic goes into Go.

## Goals

- `coordinator` replaces `orchestrator` as the role name; an existing `orchestrator` entry
  reads as `coordinator` with no migration; writers write `coordinator`.
- `set-role` refuses a second coordinator and names the holder.
- `pingbus send` and `pingbus say` take `--to-coordinator`.
- A worker's text or ping to a human also reaches the coordinator, marked as a copy.
- Each agent learns its own role and the coordinator at session start; the pingbus skill gives
  short role guidance.
- `ccy-teams` board shows the coordinator; the seat row menu changes the role (sudo, asks first,
  names the current holder).
- Bare `ccy --teams` offers the role after team and seat, changing it through the same Go code
  path and using sudo only when it changed; Quick Launch's summary shows team, seat and role.
- Protocol spec, `docs/agent-bus.md` and usage texts agree with the code.

## Non-Goals

- A migration of team records (the old name reads as the new one).
- More than one coordinator, or a role beyond coordinator and worker.
- A new wire message kind, or a protocol version bump.
- The delivery envelope of Plan 00194; this plan only keeps its "kind/team/seat" naming compatible.
- Running Ansible or deploying; the host run goes through `meta-deploy.bash` when the owner decides.

## Tasks

### Phase 1: protocol, pingbus, agent-bus

- [ ] ⬜ **Task 1.1**: `protocol.py`: role `coordinator`; the team record accepts `orchestrator` as
  a legacy spelling read as `coordinator`; at most one coordinator in a record (red tests first)
- [ ] ⬜ **Task 1.2**: `agent_bus` registry/admin: writers write `coordinator`; `set-role` refuses a
  second coordinator naming the holder; legacy entries read as `coordinator`
- [ ] ⬜ **Task 1.3**: `pingbus send --to-coordinator` and `say --to-coordinator`; the old flag is
  renamed unless something on the host depends on it
- [ ] ⬜ **Task 1.4**: auto-copy: a worker's `say`/`send` that names a human also names the coordinator;
  the receiver marks the copy; spec in `docs/agent-bus-protocol.md` (§1, §5, §7, §8, §13, §15)
- [ ] ⬜ **Task 1.5**: session-start hook text states the agent's role and who the coordinator is;
  the pingbus skill gains role guidance
- [ ] ⬜ **Task 1.6**: `docs/agent-bus.md`, usage texts, README.host/README.podman updated

### Phase 2: ccy-teams and the launcher

- [ ] ⬜ **Task 2.1**: `ccy-teams` parses the role; board shows the coordinator
- [ ] ⬜ **Task 2.2**: seat row menu: "Make coordinator" / "Stop being coordinator" (sudo, asks first,
  names the current holder)
- [ ] ⬜ **Task 2.3**: bare `ccy --teams` offers the role after team and seat; sudo only on change
- [ ] ⬜ **Task 2.4**: Quick Launch (LAST_TEAMS) reuses the seat; the previous-configuration summary
  shows team, seat and role
- [ ] ⬜ **Task 2.5**: CCY version bump; launcher does not grow (`qa-bash-limits.bash`)

### Phase 3: verify and close

- [ ] ⬜ **Task 3.1**: touched Python tests and ruff, `scripts/qa-go.bash`, shellcheck,
  `scripts/qa-bash-limits.bash`, the ccy-teams/agent-bus/pingbus bash suites
- [ ] ⬜ **Task 3.2**: `qa-reviewer` agent run; owner decisions recorded; host run via `meta-deploy.bash`

## Success Criteria

- [ ] A team record naming `orchestrator` still validates and reads as `coordinator`
- [ ] A second coordinator is refused with the holder named
- [ ] A worker's `say` to a human reaches the coordinator marked as a copy, and the same event
  passes every receiving check unchanged
- [ ] Bare `ccy --teams` changes the role, and the next bare `ccy` offers the previous
  configuration with its role
- [ ] The launcher's code lines and statements did not grow

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00196-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed: (this commit)
