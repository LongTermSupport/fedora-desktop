# Plan 00211: agent bus reports and questions rooms

**Status**: Not Started (spec written; waiting on the owner's review of the defaulted decisions D1-D15)
**Created**: 2026-10-21
**Owner**: joseph
**Priority**: High

## Overview

Each agent team has one homeserver and one Matrix room today. Every ping, peer note, question
and status goes through it, so when a team is busy the owner cannot follow it. The owner
asked for two more rooms per team, so the owner's view is quiet:

- a **reports room** the owner only reads, where any agent posts what the human should know
  (`pingbus report`), and nobody is woken;
- a **questions room** between the coordinator and the humans. Workers ask the coordinator on
  the main room, as now. The coordinator answers what it can, and asks only what it cannot
  as one long-form, self-contained question (`pingbus ask-human`). It is woken for every
  human message in that room and passes the answers to the agents that asked.

The questions room is a real invite-only room created by the team's admin, not a direct
message, so other agents can be added to it later, as the owner wanted. Both rooms are made
by the existing play for every team, LXC teams included, idempotently. The design is
[SPEC.md](SPEC.md), with its research in SPEC §1.

## Goals

- Two rooms per team, created and kept in step by `play-agent-bus.yml` through the admin
  tool's `bootstrap`, for existing and new teams, with no manual step (SPEC §3, §5).
- A reports room the homeserver keeps read-only for humans, which wakes nobody (SPEC §3.2, §6).
- A questions room where only the coordinator and the humans are members, and every human
  message wakes the coordinator with full `HUMAN` authority (SPEC §4, §6, §8).
- `pingbus report`, `ask-human`, `thread` and `questions`, reusing the send gate, the duplicate
  window and the outbox (SPEC §7, §8).
- Skill and session-start text that teaches workers "questions go to the coordinator, never
  to a human" and coordinators "bubble up only what you cannot answer" (SPEC §10).
- Upgrade first: no member is invited to a room its pingbus cannot read (SPEC §5.3).

## Non-Goals

- Role gates, the deployer, collaborator rooms, the reconciler and token custody: Plan 00207.
  This plan supersedes 00207's management room (SPEC-roles §11), and 00207 builds on its room
  index (SPEC §2).
- Porting the bus to Go: Plan 00209. This extends v1 Python (D9).
- Enforcing that workers never write to humans. That is guidance here, and Plan 00207's
  hub-and-spoke gate enforces it (D7).
- Moving lifecycle notices (goodbye, farewell, disconnected) out of the main room (D8).
- Exporting the new rooms' history: Plan 00178 reads only the main room, and is told so.
- Room aliases (D11).

## Context & Background

- The current single-room design, room trust, power levels, the coordinator role and the
  delivery path are cited file by file in SPEC §1.
- **Overlap check.** The dedupe scout read 123 live plans and found one same-deliverable
  candidate: Plan 00207's management room (its M6b) and channel rooms (its M6a). It
  recommended merging into 00207. This plan stays separate, and that choice is D1:
  - 00207 waits on 25 owner decisions;
  - its rooms are tied to deployer token custody and a reconciler;
  - this request differs from its §11: reports come from any agent, and questions are
    long-form, not capped at 300 characters.
    SPEC §2 says how 00207 builds on this plan. Plan 00161 is the bus itself; this extends it.
- **Plan 00209 (unmerged branch, read at `1447d536`).** Its protocol corpus (DESIGN §5.4) must
  carry the new types, content keys, output field and caps of SPEC §10.3 as requirements.
  This is for 00209's owner to add on its branch; it is not edited here.

## Tasks

TDD throughout. Each failing test is committed before its code (SPEC §11.1 gives the order).

### Phase 0: spike (host)

- [ ] ⬜ **Task 0.1**: A `triage.bash` in this folder (`plan_mode gather`, `plan_require_host`)
  against a throwaway team on the host's Tuwunel. It records:
  - whether a restricted join (room version 12, allowed by main-room membership) works;
  - that a power-0 human is refused `m.room.message` in a room with `events_default` 10;
  - the exact `m.relates_to` content Element sends for "Reply in thread" and "Reply", kept as
    test fixtures;
  - that an invite left pending does no harm across syncs.
- [ ] ⬜ **Task 0.2**: Findings journalled. If the restricted join fails, SPEC §3.1 and §4.4 are
  amended so `admin` invites each agent to the reports room on `add_member`, with upgrade first
  covering the rejection.

### Phase 1: member side (deploy 1, upgrade first)

- [ ] ⬜ **Task 1.1**: `protocol`: the index, the back-link, the reports power levels, the join
  rules, the report and question content and format checks (SPEC §4, §7, §8.2).
- [ ] ⬜ **Task 1.2**: `syncer`: side-room trust, the invite rule, the coordinator's filter, the
  questions-room receive path and the attachment rules (SPEC §4.3, §4.4, §6, §8.3).
- [ ] ⬜ **Task 1.3**: `limits`: `report_per_hour` and `max_open_questions` with bounds (SPEC §7,
  §8.2).
- [ ] ⬜ **Task 1.4**: `cli`: `report`, `ask-human`, `thread`, `questions`, `history --room`, and
  the `HUMAN` line's appended `ROOM` field and `REPLY` command (SPEC §7, §8).
- [ ] ⬜ **Task 1.5**: `envelope`: the `room:` and `question:` lines (SPEC §8.3).
- [ ] ⬜ **Task 1.6**: `hooks`: the role lines (SPEC §10.2).
- [ ] ⬜ **Task 1.7**: The pingbus skill (SPEC §10.1).
- [ ] ⬜ **Task 1.8**: `docs/agent-bus-protocol.md` and `docs/agent-bus.md` (SPEC §10.3).
- [ ] ⬜ **Task 1.9**: Confirm how a new zipapp reaches running and new ccy images
  (`play-claude-yolo.yml:342-360`, `claude-yolo:2096-2098`), and make the version bump
  `CLAUDE/ContainerRules.md` requires if one is needed.
- [ ] ⬜ **Task 1.10**: `deploy.bash` for deploy 1, queued in `meta-deploy.list`. Its closing
  message asks the owner to restart the teams' sessions. It is the software play only: no
  room is created.

### Phase 2: admin side (deploy 2)

- [ ] ⬜ **Task 2.1**: `admin`: `_ensure_side_rooms` (rooms, ID files, index, back-links, power
  levels, join rules, topics, human invites) in `bootstrap` (SPEC §5.1).
- [ ] ⬜ **Task 2.2**: `admin`: `_sync_questions_members` from `bootstrap`, `add_member`,
  `set_role`, `park_member` and `remove_member`, with kicks before invites; `remove_member` also
  kicks from reports (SPEC §4.4, §5.1).
- [ ] ⬜ **Task 2.3**: `health`: a read-only side-room drift report, including a coordinator that
  has not joined the questions room (SPEC §5.3).
- [ ] ⬜ **Task 2.4**: `agent-bus list` reports each human's and the coordinator's membership of
  each room.
- [ ] ⬜ **Task 2.5**: `ansible-playbook --syntax-check` of `play-agent-bus.yml` (no other
  Ansible run). Then `deploy.bash` gains deploy 2, which ends with `acceptance.bash`, and both
  are queued in `meta-deploy.list`.

### Phase 3: the owner's view (Go)

- [ ] ⬜ **Task 3.1**: Test first, then `src-go/internal/teams/board`: each human's row shows
  their membership of each room (`in all rooms`, or e.g. `questions: invited`) from
  `agent-bus list` (SPEC §11.1).

### Phase 4: acceptance

- [ ] ⬜ **Task 4.1**: `acceptance.bash` with `--fake`, legs A1-A5 and `--after-owner`, plus
  `ACCEPTANCE-owner.md` (SPEC §11.2, §11.3).
- [ ] ⬜ **Task 4.2**: The host runs through meta-deploy; the owner's Element steps; results
  journalled.
- [ ] ⬜ **Task 4.3**: A note in Plan 00178's PLAN.md that the new rooms exist. (Plan 00207's
  note that its management room is superseded is filed with this plan.)
- [ ] ⬜ **Task 4.4**: The `qa-reviewer` agent over the full diff; resolve its findings.

## Decisions defaulted (the owner may override any)

| #   | Decision                                                      | Default, and why                                                                                                                                         |
| --- | ------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | A new plan, or a phase of Plan 00207?                         | New plan. 00207 waits on 25 decisions and ties rooms to token custody; this one ships alone and 00207 builds on it. The dedupe scout recommended merging |
| D2  | Reports room: humans read-only by convention, or enforced?    | Enforced by power levels (humans 0, agents 10). A human's post there would reach nobody, silently                                                        |
| D3  | How agents enter the reports room                             | A restricted join (main-room members may join), no per-agent invite. Invites instead if the spike fails                                                  |
| D4  | Create the questions room for a team with no coordinator?     | Yes, for every team; its topic says nobody reads it until there is one                                                                                   |
| D5  | Threads or replies for answers?                               | Each question is a thread root. Thread replies, replies, a `Q<n>` prefix and unattached messages are all delivered                                       |
| D6  | Edits in the questions room                                   | Dropped as `edit`, as in the main room; send a new message instead                                                                                       |
| D7  | Stop workers writing to humans?                               | Guidance only here; Plan 00207's gate enforces it                                                                                                        |
| D8  | Move goodbye, farewell and disconnected notices to reports?   | No, they stay in the main room; the coordinator needs them there                                                                                         |
| D9  | Extend v1 Python now, or wait for Plan 00209's Go?            | Extend v1 now: new subcommands of existing CLIs. The board change is Go                                                                                  |
| D10 | Caps                                                          | Report at most 1500 bytes and 6 an hour per member. Question at most 8000 bytes, with required `Context:` and `Question:`. At most 5 open questions      |
| D11 | Room aliases?                                                 | None; members find rooms through the admin-written index                                                                                                 |
| D12 | Rollout                                                       | Two deploys: member side, a session restart, then the admin side                                                                                         |
| D13 | History visibility                                            | `shared` for both, so a new coordinator reads the open questions                                                                                         |
| D14 | Message types                                                 | Reports are `m.notice` with no mentions (never notify). Questions are `m.text` mentioning the humans (notify)                                            |
| D15 | May the owner start a topic in the questions room unprompted? | Yes; it reaches the coordinator as an unattached `HUMAN` message                                                                                         |

## Dependencies

- Builds on Plans 00161 (the bus), 00194 (live delivery) and 00196 (the coordinator).
- Plan 00207 M6a and M6b should build on this plan's index and questions room, not alongside
  them.
- Plan 00209's corpus must add this plan's protocol additions before its stage 2.

## Success Criteria

- [ ] Every team, LXC teams included, has a reports room and a questions room after one
  `play-agent-bus.yml` run, and a second run reports no change.
- [ ] A worker's `pingbus report` appears in the reports room in Element, wakes no session and
  raises no notification. The owner cannot post there.
- [ ] A coordinator's `ask-human` appears as one thread in the questions room and notifies the
  owner. The owner's reply in the thread reaches the coordinator's session as `HUMAN` with its
  `Q<n>`.
- [ ] A worker's token cannot read, join or post in the questions room (server-refused).
- [ ] A role change moves the questions-room membership to the new coordinator.
- [ ] Touched suites, `qa-changed.bash` and the `qa-reviewer` agent pass.

## Risks & Mitigations

| Risk                                                     | Impact | Probability | Mitigation                                                                                             |
| -------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------ |
| Tuwunel does not support restricted joins                | M      | L           | Spike first (Task 0.1). The fallback is invites, which upgrade first already makes safe                |
| An old coordinator rejects its questions invite          | M      | M           | Two deploys (D12). `bootstrap` re-invites on every apply and health names the coordinator              |
| A container that is down misses deploy 1                 | M      | M           | The play says it was skipped. A4 checks each LXC member's pingbus version before deploy 2 is accepted  |
| Coordinators over-ask, and the questions room gets noisy | M      | M           | Format checks, the open cap, duplicate refusal and the skill's "bubble up only what you cannot answer" |
| Element's thread behaviour differs from the spec         | L      | M           | Fixtures captured from Element in Task 0.1. Every form is delivered, attached or not                   |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00211-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan and spec filed (design only)
