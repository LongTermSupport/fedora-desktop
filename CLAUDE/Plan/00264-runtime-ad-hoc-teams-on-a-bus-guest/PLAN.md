# Plan 00264: runtime ad hoc teams on a bus guest

**Status**: Not Started
**Created**: 2026-11-24
**Owner**: joseph
**Priority**: Medium

## Overview

The owner's question Q61: "starting an ad-hoc team must not need a whole IaC action; teams fluid,
cheap to create and tear down", with several teams per bus. Today a team exists only because
host_vars declares it and `play-teamchat.yml` approves and applies it; `ccy-teams create` edits the
declaration and runs the play.

This plan designs how `teamchat-admin` creates, approves and removes a team at runtime on an
existing bus guest, and what stays in Ansible. The design is
[DESIGN.md](DESIGN.md); this file holds the decisions it needs and the work it implies once they
are made. Its key finding: `team apply` is already a root, idempotent, fail-fast runtime verb, so
the work is around it (the declaration's source, approval, a port range with its SELinux label,
the timers, and a clean teardown), not a new apply.

It fits the v2 architecture of [Plan 00209](../00209-agent-bus-v2-in-go/PLAN.md)
([DESIGN-v2-C-host.md](../00209-agent-bus-v2-in-go/DESIGN-v2-C-host.md)) and the install path of
[Plan 00246](../00246-agent-bus-v2-landing-and-cross-network/INSTALL-PATH.md); it changes none of
their decisions (DC1 one homeserver per team, DC4 file-based reads, DC11 per-team locks, DC18 the
approval gate, C10 password sudo for changes).

## Goals

- A runtime team is created, renamed (display name) and removed on a bus guest with no play run.
- Creation inside an owner-approved policy needs no password; outside it, the owner approves.
- Every request and verdict is auditable on the bus guest.
- Removing a team, or purging and re-creating one, releases members' seats with no manual
  `seats abandon` while a notice or tombstone reaches the member machine (closes the issue #386
  gap for declared teams too).
- A live runtime team is never judged unknown by `knownBy` (DESIGN.md 5.4).
- A play run never removes, stops or re-declares a runtime team.

## Non-Goals

- Creating the bus guest, or any declared team, at runtime.
- A resident daemon or socket on the bus guest.
- Changing a team id in place (a rename is display only; DESIGN.md 4.3).
- Element profiles for runtime teams.

## Context & Background

DESIGN.md section 1 is the current lifecycle, cited by path: the play's steps, what `team approve|apply|remove|seal|rotate-ca` make, and how a member machine learns of a team
(place-issued, `desired.json`, the seat sweep, `seats abandon`'s `knownBy` on branch
`ci/batch-386-397-398`).

## Decisions for the owner

The questions are in [DESIGN.md section 10](DESIGN.md#10-questions-for-the-owner): who may
request; what the policy pre-approves; limits (team cap, port range, memory floor); the remove
default and the notice grace; ephemeral or carried on reprovision; adoption into host_vars;
offline members; a runtime id prefix; timers always on. Recommended option: **O2, a
policy-bounded request spool** (DESIGN.md section 9).

## Tasks

### Phase 0: Decide

- [ ] ⬜ **Task 0.1**: The owner answers DESIGN.md section 10 and picks an option; record each
  answer in the journal and in a Technical Decisions entry here.

### Phase 1: Teardown signals (needed by every option)

- [ ] ⬜ **Task 1.1**: Area A specifies the `team-closing` host notice (DESIGN-v2-A-protocol.md).
- [ ] ⬜ **Task 1.2**: `team remove` posts it, waits the bounded grace, and replaces the status
  record with `removed.json` (the tombstone); `team apply` of the same id replaces the tombstone.
- [ ] ⬜ **Task 1.3**: The member link records the notice as a tombstone in the seat store; the
  sweep drops the team from `desired.json` with a farewell and releases, and releases without
  logout any seat whose CA fingerprint matches a tombstone.
- [ ] ⬜ **Task 1.4**: `knownBy` consults tombstones first and reads the status record's presence,
  not its currency (DESIGN.md 5.4); tests over every row of that table.

### Phase 2: IaC groundwork

- [ ] ⬜ **Task 2.1**: Host vars for the runtime port range and the policy; `declare check` refuses a
  declared port inside the range; the play labels the range `teamchat_port_t` once.
- [ ] ⬜ **Task 2.2**: The requesters group, the spool directories (tmpfiles), the request path and
  service units printed by `teamchat-admin units print`, the policy file.
- [ ] ⬜ **Task 2.3**: The timers rule becomes "declared present, or runtime teams allowed"; after a
  base unit or member program change the play runs `team reapply --origin runtime`.

### Phase 3: Runtime verbs

- [ ] ⬜ **Task 3.1**: The request decode, the policy judgement, and `done/<id>.json` verdicts.
- [ ] ⬜ **Task 3.2**: The port allocator under a host-wide allocation lock, with the team cap and
  memory floor.
- [ ] ⬜ **Task 3.3**: `team requests` (the oneshot): create, rename, remove; `team approve --request <id>` for parked requests; `origin: runtime` in the stamp and status record.
- [ ] ⬜ **Task 3.4**: `declare check` refuses a host_vars id held by a runtime team;
  `ccy-teams adopt`.

### Phase 4: ccy-teams

- [ ] ⬜ **Task 4.1**: The board shows runtime teams as "runtime" (not "decide") with requester and
  pending requests; `ccy-teams create --runtime` files a request instead of running the play.

### Phase 5: Proof

- [ ] ⬜ **Task 5.1**: The VM-lab scenario of DESIGN.md section 8, with a plan-local
  `acceptance.bash` and its coverage line.
- [ ] ⬜ **Task 5.2**: `qa-reviewer` over the plan's diff.

## Dependencies

- Builds on: Plan 00209 (v2 design), Plan 00246 (install path, firewall zones X30 section 1).
- Relates to: issue #386 and `ccy-teams seats abandon` (branch `ci/batch-386-397-398`).

## Success Criteria

- [ ] The owner has answered every question in DESIGN.md section 10.
- [ ] In the lab, a runtime team is created, joined, removed and re-created with no play run, and the
  member's seats are released by the notice or the tombstone alone.
- [ ] A play run leaves running runtime teams untouched and re-applies them after a base unit change.
- [ ] QA passes (`./scripts/qa-changed.bash`, then CI's `qa-all.bash`).

## Risks & Mitigations

| Risk                                                      | Impact | Probability | Mitigation                                                                  |
| --------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------- |
| The policy is drawn loosely and "inside" is not safe      | H      | M           | default to the tightest policy (no humans, no trust, bridge network only)   |
| Many homeservers exhaust the bus guest's memory           | M      | M           | team cap and free-memory floor, refused by name                             |
| IaC and runtime drift (play stops a runtime team's timer) | H      | L           | the timers rule and the untouched-runtime rule are tested in the lab        |
| An offline member keeps a dead seat                       | L      | M           | the tombstone in the notice; manual `seats abandon` stays the fallback (Q7) |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00264-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Design written for the owner's decision (this commit).
