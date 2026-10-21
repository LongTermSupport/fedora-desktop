# Plan 00211: agent bus reports and questions rooms

**Status**: In Progress (Phase 1, the member side, in code and not deployed; the Phase 0 spike has not run; D9 needs the owner's sign-off; the other defaulted decisions are open to override)
**Created**: 2026-10-21
**Owner**: joseph
**Priority**: High

## Overview

Each agent team has one homeserver and one Matrix room today. Every ping, peer note, question
and status message goes through it, so when a team is busy the owner cannot follow it. The
owner asked for two more rooms per team, so that what the owner sees is quiet:

- a **reports room** that the owner only reads. Any agent posts there what the human should
  know (`pingbus report`), and nobody is woken.
- a **questions room** between the coordinator and the humans. Workers ask the coordinator on
  the main room, as now. The coordinator answers what it can, and asks only what it cannot, as
  one long-form, self-contained question (`pingbus ask-human`). It is woken for every human
  message in that room, and passes the answers to the agents that asked.

The questions room is a real invite-only room created by the team's admin account, not a
direct message, so other agents can be added to it later, as the owner wanted. The existing
play creates both rooms for every team, LXC teams included, and running it again changes
nothing. The rollout is upgrade-first:

- **Deploy 1** ships the member side to every member.
- **Deploy 2** creates the rooms and invites every coordinator, then waits for each to have
  joined the questions room. That join is the proof that the coordinator's running code
  understands the rooms; the leg names any that has not.

The design is [SPEC.md](SPEC.md). Its research is in SPEC §1.

## Goals

- Two rooms per team, created and kept in step by `play-agent-bus.yml` through the admin tool's
  `bootstrap`, for existing and new teams. No manual step, and a re-run changes nothing
  (SPEC §3, §5).
- A reports room that the homeserver keeps read-only for humans, and that wakes nobody
  (SPEC §3.2, §6).
- A questions room whose only members are the coordinators and the humans. Every human message
  in it, and every edit, wakes the coordinator with full `HUMAN` authority (SPEC §4, §6, §8).
- New commands `pingbus report`, `ask-human`, `thread` and `questions`, reusing the send gate,
  the duplicate window and the outbox (SPEC §7, §8).
- Skill and session-start text that teaches workers "questions go to the coordinator, never to
  a human", and coordinators "bubble up only what you cannot answer" (SPEC §10).
- Fail-fast rollout. Deploy 2 refuses, naming the member, while an LXC container is down or out
  of date, or a coordinator has not joined the questions room (SPEC §5.3).

## Non-Goals

- Role gates, the deployer, collaborator rooms, the reconciler and token custody: Plan 00207.
  This plan supersedes 00207's management room (SPEC-roles §11), and 00207 builds on this
  plan's room index (SPEC §2).
- Porting the bus to Go: Plan 00209. This plan extends v1 Python (D9, which needs the owner's
  sign-off).
- Enforcing that workers never write to humans. Here that is guidance; Plan 00207's
  hub-and-spoke gate enforces it (D7).
- Moving lifecycle notices (goodbye, farewell, disconnected) out of the main room (D8).
- Exporting the new rooms' history. That is Plan 00178's job, and it is told (Task 4.3).
- Room aliases (D11), and a cap on open questions (D10).

## Context & Background

- The current single-room design, room trust, power levels, the inbox re-validation, the
  coordinator role, the delivery path and the ccy-teams parser are cited file by file in
  SPEC §1.
- **Overlap check.** The dedupe scout read 123 live plans. It found one candidate covering the
  same work: Plan 00207's management room (its M6b) and channel rooms (its M6a). It recommended
  merging this plan into 00207. This plan stays separate, and that choice is D1:
  - 00207 waits on 25 owner decisions.
  - Its rooms are tied to deployer token custody and to a reconciler.
  - This request differs from 00207's §11: reports come from any agent, and questions are
    long-form rather than capped at 300 characters.
    SPEC §2 says how 00207 builds on this plan.
- **Design review.** The first version of the spec was reviewed and came back FIX-FIRST, with
  six blocking gaps and several significant ones. This version resolves them; the dispositions
  are in JOURNAL/.
- **Plan 00209** is on an unmerged branch, read at `1447d536`. Its protocol corpus (DESIGN §5.4)
  must carry this plan's additions (SPEC §10.3, §5.5, §6). That is for 00209's owner to add on
  its branch; the branch is not edited here.

## Tasks

TDD throughout: each failing test is committed before its code. SPEC §11.1 gives the order.

**Merge order is part of the design.** Phase 1 is merged to F44 and deployed (deploy 1) before
any Phase 2 code is merged. Phase 2's admin code must never be on F44 at deploy 1.

### Phase 0: spike (host)

- [ ] 🔄 **Task 0.1**: `spike.bash` in this folder (`plan_mode deploy`, `plan_require_host`;
  SPEC §11.2). It runs a throwaway Tuwunel process as the user, under the run directory, and
  removes it on cleanup. It records:
  - whether a restricted join works (room version 12, allowed by membership of the main room);
  - that a power-0 human is refused `m.room.message` when `events_default` is 10;
  - that a power-50 human is refused `m.reaction` at 100;
  - that a pending invite does no harm across syncs.
    Written (`spike.bash`, `spike_probe.py`) and queued in `meta-deploy.list`; not run yet.
- [ ] ⬜ **Task 0.2**: Findings journalled. If the restricted join fails, SPEC §3.1, §4.4 and
  §5.3 are amended to another way into the reports room before Phase 1 starts.

### Phase 1: member side (deploy 1)

- [x] ✅ **Task 1.1**: `protocol`: the index, the back-link, both power-level forms, the join
  rules, the report and question content and format checks, and room-aware `validate_event`
  with edits (SPEC §3.2, §4, §6, §7, §8.2). Done in code, not deployed: 418ae31c (the
  question index and attachment rules of §8.3 are in `protocol` too, pure).
- [x] ✅ **Task 1.2**: `inbox`: every stored item records its room, and re-validation passes the
  room on (SPEC §6). Also `rooms.json` (the side rooms) and `reports.json` (report send times,
  apart from the outbox gate). Done in code, not deployed.
- [x] ✅ **Task 1.3**: `syncer` (done in code, not deployed). SPEC §4.3-§4.5, §6, §8.3:
  - side-room trust;
  - the reports join on reading the index;
  - the invite rule;
  - the coordinator's filter, with gap fill;
  - the questions-room receive path;
  - `history --room`;
  - the attachment rules.
- [x] ✅ **Task 1.4**: A coordinator with `human_text: false` is refused where the role is
  assigned: `members.py` in an LXC entry, and `admin.add_member`. A sync never refuses it; the
  member leaves the questions invite pending, and `status` and the hook context name the
  reason (SPEC §4.5). Done in code, not deployed: `status` prints a `ROOM` line per side room.
- [x] ✅ **Task 1.5**: `limits`: `report_per_hour`, with its bounds (SPEC §7). Done in code, not
  deployed.
- [x] ✅ **Task 1.6**: `cli`: `report`, `ask-human`, `thread` (with `--ask` and `--close`),
  `questions`, and `history --room`. The `HUMAN` line gains an appended `ROOM` field, and the
  `REPLY` command changes for questions-room items (SPEC §7, §8). Done in code, not deployed.
- [x] ✅ **Task 1.7**: `envelope`: the `room:`, `question:` and `revises:` lines (SPEC §8.3).
  Done in code, not deployed.
- [x] ✅ **Task 1.8**: `hooks`: the role lines. The pingbus skill text (SPEC §10.1, §10.2):
  workers ask the coordinator, reports are never questions, the coordinator bubbles up only
  what it cannot answer and passes every answer on. Done in code, not deployed.
- [x] ✅ **Task 1.9**: A change to `helpers/pingbus/` needs the container version bump: the
  pre-commit gate requires it (CLAUDE/ContainerRules.md, "every other file the image bakes
  in"), so SPEC §1 and §5.6's "no bump" was wrong. Each Phase 1 commit that touches the image
  bumps it (3.99.13.0, container 2.78.0, first; then the patch digit per commit). SPEC §1 and
  §5.6 are corrected.
- [x] ✅ **Task 1.10**: Go, test-first: the `agent-bus list` parser skips `ROOM` lines, pinned
  by a test (SPEC §5.5, §11.1). `TestParseRosterSkipsRoomLines`.
- [x] ✅ **Task 1.11**: `docs/agent-bus-protocol.md` and `docs/agent-bus.md` (SPEC §10.3).
  The protocol doc's §2, §8.1, §9, §10, §12, §13 and §15; the owner's guide's three rooms.
- [ ] ⬜ **Task 1.12**: `acceptance.bash` with `--fake` and stage 1 legs S1-S2. `deploy.bash --stage 1` runs the **whole** `play-agent-bus.yml` and the ccy staging, then `acceptance.bash --stage 1`. Its closing message asks the owner to restart the teams' sessions (SPEC §5.3,
  §11.2). `ansible-playbook --syntax-check` only.
- [ ] ⬜ **Task 1.13**: Merge Phase 1 to F44, then queue `deploy.bash --stage 1` in
  `meta-deploy.list`. Journal the run once the owner has run it.

### Phase 2: admin side (deploy 2), after deploy 1 is accepted

- [ ] ⬜ **Task 2.1**: `admin`: `_ensure_side_rooms` (SPEC §5.1):
  - creating the rooms, or adopting an existing room after a crash;
  - the ID files, the index and the back-links;
  - power levels, join rules and topics;
  - human invites;
  - removed humans kicked from every room.
- [ ] ⬜ **Task 2.2**: `admin`: `_sync_questions_members`, called from `bootstrap`,
  `add_member`, `set_role` and `remove_member` (SPEC §5.1):
  - the questions room is created at the first coordinator;
  - kicks come before invites, and every coordinator is invited (again, when `leave`);
  - the open and closed forms.
    `remove_member` also kicks from reports.
- [ ] ⬜ **Task 2.3**: `health`: a read-only side-room drift check, which also reports
  coordinators not joined to questions (SPEC §5.4).
- [ ] ⬜ **Task 2.4**: `agent-bus list` `ROOM` lines, with the HUMAN and MEMBER lines unchanged
  (SPEC §5.5).
- [ ] ⬜ **Task 2.5**: `acceptance.bash` gains stage 2 (A1-A4) and `--after-owner` (A5), and
  `ACCEPTANCE-owner.md` is written. Both are committed before Task 2.6 (SPEC §11.2, §11.3).
- [ ] ⬜ **Task 2.6**: `deploy.bash --stage 2` with the legs of SPEC §5.3: pre-flight (LXC containers up
  and new), the play, the questions-join wait, then acceptance. `ansible-playbook --syntax-check` only. Merge, then
  queue in `meta-deploy.list`.

### Phase 3: the owner's view (Go)

- [ ] ⬜ **Task 3.1**: Test first, then the parser reads `ROOM` lines. On the board, each human's
  row shows their membership of each room, for example `in all rooms` or `questions: invited`
  (SPEC §11.1).

### Phase 4: acceptance and close

- [ ] ⬜ **Task 4.1**: The host runs deploy 2 through meta-deploy. The owner does the Element
  steps, then `acceptance.bash --after-owner` runs. The results are journalled.
- [ ] ⬜ **Task 4.2**: Turn the reply and edit events A5 recorded into anonymised fixtures, and
  re-run the attachment and edit tests against them (SPEC §8.3).
- [ ] ⬜ **Task 4.3**: A note in Plan 00178's PLAN.md that the new rooms exist. (Plan 00207's
  note was filed with this plan.)
- [ ] ⬜ **Task 4.4**: The `qa-reviewer` agent over the full diff; resolve its findings.

## Decisions (defaulted; the owner may override any)

| #   | Decision                                                                           | Default, and why                                                                                                                                                                                                                                                                                                                                                                     |
| --- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| D1  | A new plan, or a phase of Plan 00207?                                              | A new plan. 00207 waits on 25 decisions and ties rooms to token custody; this plan ships alone and 00207 builds on it. The dedupe scout recommended merging                                                                                                                                                                                                                          |
| D2  | Reports room: read-only for humans by convention, or enforced?                     | Enforced by power levels (humans 0, agents 10). A human's post there would reach nobody, silently                                                                                                                                                                                                                                                                                    |
| D3  | How agents enter the reports room                                                  | A restricted join (members of the main room may join), made by each member's syncer on reading the index. The spike checks it                                                                                                                                                                                                                                                        |
| D4  | Questions room for a team with no coordinator?                                     | Created only when the team first has a coordinator. If it later has none, the room is closed (humans at power 0), so no message goes unread                                                                                                                                                                                                                                          |
| D5  | Threads or replies for answers?                                                    | Each question is a thread root. Thread replies, replies, a `Q<n>` prefix and unattached messages are all delivered                                                                                                                                                                                                                                                                   |
| D6  | Edits in the questions room                                                        | Delivered to the coordinator as a revision (`revises: <event>`), beside the original. Edits in the main room are dropped, as now                                                                                                                                                                                                                                                     |
| D7  | Workers posting in the questions room, or writing questions to humans              | No. Workers ask on the main room, and this is guidance only here; Plan 00207's gate enforces it                                                                                                                                                                                                                                                                                      |
| D8  | Move goodbye, farewell and disconnected notices to reports?                        | No. They stay in the main room, where the coordinator needs them                                                                                                                                                                                                                                                                                                                     |
| D9  | **Needs the owner's sign-off.** Extend v1 Python now, or wait for Plan 00209's Go? | Extend Python now. This departs from CLAUDE.md's rule that a standalone Python CLI is ported to Go when it is next reworked: four subcommands, multi-room sync and an inbox change are a rework of `pingbus`. The reason: Plan 00209 is porting the whole bus. The cost: a larger corpus for 00209 to carry. The alternative is to build this plan after 00209's member-client stage |
| D10 | Caps                                                                               | A report: at most 1500 bytes, and 6 an hour per member. A question: at most 8000 bytes, with `Context:` and `Question:` required. No cap on open questions, because it would block an urgent one                                                                                                                                                                                     |
| D11 | Room aliases?                                                                      | None. Members find the rooms through the index the admin writes                                                                                                                                                                                                                                                                                                                      |
| D12 | Rollout                                                                            | Two deploys: the member side, then a session restart, then the admin side. Deploy 2 has an LXC pre-flight check, and a wait for every coordinator's questions join that refuses, naming any not joined (an old one rejects; the next run invites again)                                                                                                                              |
| D13 | History visibility                                                                 | `shared` in both rooms, so a new coordinator reads the open questions                                                                                                                                                                                                                                                                                                                |
| D14 | Message types                                                                      | Reports are `m.notice` with no mentions, so they never notify. Questions are `m.text` mentioning the humans, so they notify                                                                                                                                                                                                                                                          |
| D15 | May the owner start a topic in the questions room unprompted?                      | Yes. It reaches the coordinator as an unattached `HUMAN` message                                                                                                                                                                                                                                                                                                                     |
| D16 | Reactions in the questions room                                                    | Closed (`m.reaction` 100), so Element does not offer them. An emoji is an ambiguous answer to a decision; a message is delivered and unambiguous                                                                                                                                                                                                                                     |

## Dependencies

- Builds on Plans 00161 (the bus), 00194 (live delivery) and 00196 (the coordinator).
- Plan 00207 M6a and M6b should build on this plan's index and questions room, not run
  alongside them.
- Plan 00209's corpus must include this plan's protocol additions before its stage 2.

## Success Criteria

- [ ] Every team, LXC teams included, has a reports room, and every team with a coordinator has a
  questions room, after deploy 2. A further play run reports no change.
- [ ] A worker's `pingbus report` appears in the reports room in Element, wakes no session and
  raises no notification. The owner cannot post there.
- [ ] A coordinator's `ask-human` appears as one thread in the questions room and notifies the
  owner. The owner's reply in the thread, and an edit of it, reach the coordinator's session as
  `HUMAN` with the question's `Q<n>`.
- [ ] A worker's token cannot read, join or post in the questions room: the server refuses each.
- [ ] A role change moves the questions-room membership to the new coordinator, and the new
  coordinator's `pingbus questions` shows any answer not yet passed on.
- [ ] Deploy 2 refuses, naming the member, while an LXC container is down or a coordinator's
  running pingbus is old.
- [ ] ccy-teams works unchanged through deploy 2, before Phase 3.
- [ ] Touched suites, `qa-changed.bash` and the `qa-reviewer` agent pass.

## Risks & Mitigations

| Risk                                                     | Impact | Probability | Mitigation                                                                                |
| -------------------------------------------------------- | ------ | ----------- | ----------------------------------------------------------------------------------------- |
| Tuwunel does not support restricted joins                | M      | L           | Spike first (Task 0.1); if it fails, another way into reports is settled before Phase 1   |
| An old coordinator rejects its questions invite          | M      | M           | Deploy 2's questions-join wait and health name it; the next play run invites it again     |
| A container that is down misses deploy 1                 | M      | M           | The stage-1 acceptance and the deploy-2 pre-flight both refuse, naming it                 |
| Admin code reaches F44 before deploy 1                   | H      | L           | The merge order is stated in Tasks. Task 1.13 merges Phase 1 alone                        |
| Coordinators over-ask, and the questions room gets noisy | M      | M           | Format checks, duplicate refusal, and the skill's "bubble up only what you cannot answer" |
| Element's thread or edit shape differs from the spec     | L      | M           | Every form is delivered, attached or not. A5's recorded events become fixtures (Task 4.2) |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00211-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan and spec filed (design only): c9f11737
- Spec revised after the FIX-FIRST design review
