# Plan 00225: team liaison role

**Status**: Not Started (plan only; decisions LD1-LD10 carry coordinator defaults the owner may change)
**Created**: 2026-10-23
**Owner**: joseph
**Priority**: Medium

## Overview

The owner, dictated: "a coordinator is very very busy on a reasonably sized team ... it's too
busy to handle my human questions. because between one message from me and the next message
from me, it might get multiple [messages] from different agents. ... we need another role within
teams ... whose focus is ... human interface ... it's the one that would give me progress updates
and would ask me questions. and the coordinator would delegate to it when it needs to talk to me.
and it would mean that the coordinator thread then doesn't have to be checked for questions that
are actually for me. ... [it] would not do any work, would not do any development, would not do
any coordination, would not do any triage or anything, it would purely discuss things from my
point of view. Ideally it has read-only access to all of the relevant code ... primarily its goal
is just to ask questions and then to liaise with the coordinator and maybe with other team
members." The owner named the role **liaison**.

This plan adds `liaison` as a third team role, at most one per team. The coordinator keeps
running `pingbus ask-human`; with an active liaison, pingbus hands the question to the liaison
instead of posting it. The liaison asks the human, in the questions room or its own terminal,
and is the one woken for human messages there. It passes each answer back with `pingbus relay`,
which names the human's event. The coordinator's own pingbus fetches that event from the
homeserver and shows the human's verbatim words beside the liaison's text. So a relay never
counts as authority, and the HUMAN and PEER rules are unchanged. The liaison builds progress
updates from the reports room and the bus history, citing its sources. It cannot edit or commit:
the Plan 00222 guard denies it, and in ccy and LXC its checkout is mounted read-only. A team with
no liaison behaves exactly as it does today.

The design, with numbered requirements LZ0-LZ23, is [DESIGN.md](DESIGN.md).

## Goals

- `liaison` is a role, one per team, needing a coordinator, never mixed with another role in one
  session (DESIGN LZ1-LZ4).
- Human questions flow coordinator -> liaison -> human -> liaison -> coordinator, and the
  coordinator is woken for the questions room only as a fallback (LZ8-LZ11).
- A relay is a `PEER` text. The human's own event, checked by the receiver against the
  homeserver, is shown with it, and `pingbus verify-human` confirms it (LZ5-LZ7a).
- Progress updates come from the reports room, the bus history and the hand-offs, and each one
  says when it is from and what it is based on (LZ12-LZ14).
- The liaison cannot edit or commit: there is a guard in every runtime, and a read-only mount
  in ccy and LXC (LZ15-LZ20).
- With no liaison, or an inactive one, behaviour is byte for byte today's (LZ0, LZ10).

## Non-Goals

- A filesystem boundary for a host `cc` liaison (same uid as the owner; LZ19).
- A liaison shared across teams as one seat: one per team, though one session may hold the seat
  in several teams (DESIGN section 7, LD1).
- Timed progress digests (LD8), labels for the liaison (LZ22), relays to workers (LZ4).
- Editing Plan 00209's or 00222's files: requirements are handed over (Tasks 7.1, 7.2).

## Dependencies

- Plans 00196 and 00203 (roles, hot control), 00211 (rooms, `ask-human`; deploy 2 must be
  accepted before Phase 3), 00194 (live delivery, which the hand-off latency rests on).
- **Plan 00222 Phase 2** (the guard and `Decide`) must be merged before Phase 4; this plan
  extends it, never forks it.
- Plan 00221 (the tree) for LZ22's board line; until it lands, the flat board gets the role word.
- Plan 00224 (`notify-owner`) takes LZ21 as a requirement (Task 7.1).
- Plan 00209 (v2) takes DESIGN section 8 as requirements (Task 7.2).

## Tasks

Every build task is test first: the failing test is committed or recorded before the code.

### Phase 0: Facts

- [ ] ⬜ **Task 0.1**: `triage.bash` in this folder (host; throwaway container and unit, removed
  in an `EXIT` trap). It records:
  - which paths inside the checkout a ccy session (Claude Code with the hooks daemon) writes,
    found by launching one with the checkout `ro` and logging each `EROFS`;
  - whether `git status` and `git log` work on a `ro` checkout;
  - the same for an LXC unit with `BindReadOnlyPaths=`;
  - that a coordinator's token can `GET /rooms/{room}/event/{id}` in the questions room.
- [ ] ⬜ **Task 0.2**: Findings in `RESEARCH-facts.md`; LZ17 and LZ20's writable list is the
  measured one; a refuted fact changes DESIGN.md first.
- [ ] ⬜ **Task 0.3**: The owner answers LD1-LD10, or the defaults stand. Each is recorded in the journal.

### Phase 1: The role in the v1 protocol (upgrade-first)

- [ ] ⬜ **Task 1.0**: Golden tests for a team with no liaison: envelopes, wakes, `ask-human`
  posts, `--list` (LZ0). These must stay green through every later phase.
- [ ] ⬜ **Task 1.1**: `protocol.py`: the `liaison` role, at most one, needing a coordinator;
  the sender rules of LZ4 (refused on send, dropped `role` on receipt); the `relays` and
  `handoff` content keys and their grammar. Tests over every role and verb pair.
- [ ] ⬜ **Task 1.2**: `docs/agent-bus-protocol.md`: the role, the keys, and the receiver's
  check (LZ5, LZ6). The contract test is extended.

### Phase 2: The member side

- [ ] ⬜ **Task 2.1**: The syncer:

  - the liaison's rooms: main, questions woken, and the reports timeline read (LZ9, LZ12);
  - the coordinator's quiet questions items and its keeper's `liaison_relay_s` wake;
  - LZ10's active test, from the cached status.

  Tests use a fake clock and the fake homeserver.

- [ ] ⬜ **Task 2.2**: `pingbus`:

  - `say --to-liaison`;
  - `ask-human` hand-off routing with `--update` and `--direct`, and `ask-human --handoff`;
  - `relay` (with `--terminal`);
  - `verify-human`, `handoffs` and `history --room reports`;
  - the hand-off timeout notice (LZ5, LZ7, LZ8, LZ11).

  The `relays` refusal tests come first.

- [ ] ⬜ **Task 2.3**: The envelope's `relays:` block, checked on the homeserver, with every
  `NOT VERIFIED` reason: an unlisted sender, a redacted event, another team's room, an agent's
  event, an unknown ID (LZ6).

- [ ] ⬜ **Task 2.4**: `hooks.py` role lines (LZ23). The pingbus skill gets a Liaison section:
  LZ7's rule, LZ14's update shape, and the hand-off flow.

- [ ] ⬜ **Task 2.5**: The container version bump that `helpers/pingbus/` changes need, and the
  `pingbus version` marker raised (00211 Task 1.9's rule). **CCY version bump.**

### Phase 3: The admin side

- [ ] ⬜ **Task 3.1**: `agent-bus set-role ... liaison` and `add-member --role liaison`:
  - LZ1, LZ2 and LZ3's refusals, naming the holder or the other team;
  - the refusal while a running member reports an older pingbus (DESIGN section 9).
- [ ] ⬜ **Task 3.2**: `_sync_questions_members` invites the liaison and kicks a former one;
  `check-rooms` reports a liaison that has not joined (offline means `OFFLINE`, never a
  failure). `members.py` accepts `role: liaison` for an LXC entry.

### Phase 4: The guard (after Plan 00222 Phase 2 merges)

- [ ] ⬜ **Task 4.1**: `Decide` table tests, then the code:
  - LZ15's liaison set, with the git-write parser's cases (each verb, `-C dir`, `env` prefixes,
    a quoted commit message);
  - LZ16's coordinator `AskUserQuestion` rule, for an active and an inactive liaison;
  - LZ3's single-role session.
- [ ] ⬜ **Task 4.2**: `hooks.json`'s matcher gains `Edit|Write|NotebookEdit|Bash` only for the
  guard entry, and a test pins it. The deny texts are compared byte for byte.

### Phase 5: Read-only code per runtime

- [ ] ⬜ **Task 5.1**: `ccy-teams internal launch-mounts` (Go): the `ro` workspace and the
  writable paths measured in Task 0.1, for a liaison launch; nothing for any other role. Tests on
  a fixture membership. The launcher appends its output in place of the workspace `-v`, adding
  no line (`qa-bash-limits` proves it), and `scripts/test-ccy-agent-bus.bash` pins the
  arguments first, red. **CCY version bump.**
- [ ] ⬜ **Task 5.2**: LZ18: the board's `relaunch for read-only` state and the role-notice text,
  in Go with tests.
- [ ] ⬜ **Task 5.3**: The LXC member phase of `play-agent-bus.yml`. Tests on the rendered unit
  come first. The unit gets `BindReadOnlyPaths=` and the `BindPaths=` lines for a liaison and
  none otherwise; a role change restarts the unit and the play says so (LZ20).
- [ ] ⬜ **Task 5.4**: For `cc --teams` and `agent-bus-claude` launched as the liaison, Go computes
  `--disallowedTools Edit Write NotebookEdit` (LZ19). The launchers do not grow.

### Phase 6: Host UX (Go)

- [ ] ⬜ **Task 6.1**: `ccy-teams`:

  - the parser reads `liaison`;
  - the board's liaison line, on the tree when Plan 00221 has landed (LZ22);
  - "Make liaison" and "Stop being liaison", with the refusals;
  - the role offered in bare `ccy --teams` and `cc --teams`, and in Quick Launch's summary.

  Picker tests come first. **CCY version bump** (ccy-teams ships with ccy).

### Phase 7: Handoffs to other plans

- [ ] ⬜ **Task 7.1**: A task line in Plan 00224's PLAN.md: its gate accepts `liaison` (LZ21). Its
  owner adds it, through the coordinator.
- [ ] ⬜ **Task 7.2**: DESIGN section 8 goes to Plan 00209's owner seat as a design note, through
  the coordinator. This plan edits none of 00209's files.
- [ ] ⬜ **Task 7.3**: Plan 00207's SPEC-roles and PLAN: "liaison room" renamed "collaborator
  room", in one docs-only commit, so the word has one meaning.

### Phase 8: Docs, deploy, acceptance

- [ ] ⬜ **Task 8.1**: `docs/agent-bus.md`: the liaison, how to seat one, what a relay proves, and
  what the read-only mount covers per runtime.
- [ ] ⬜ **Task 8.2**: `deploy.bash` (`play-agent-bus.yml`, `play-claude-yolo.yml`) and
  `acceptance.bash` with a coverage line, both queued in `meta-deploy.list`. On a throwaway team:
  - the stage-1 upgrade check;
  - `set-role` refused while a member is old;
  - a hand-off and its relay, with the envelope showing `checked on the homeserver`;
  - a relay naming an agent's event, refused on send;
  - a forged raw relay, shown `NOT VERIFIED`;
  - the liaison's `Edit` and `git commit` denied;
  - a write to the `ro` ccy checkout failing;
  - a team with no liaison identical to before;
  - the liaison stopped: `ask-human` posting directly;
  - hand-off latency recorded.
- [ ] ⬜ **Task 8.3**: Adversarial review of each phase's diff until MERGE, then `qa-reviewer`.

## Decisions (coordinator defaults; the owner may change any)

| ID   | Decision                                             | Default                                                                                                                       |
| ---- | ---------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| LD1  | One liaison per team, or one per human across teams  | **One per team** (DESIGN section 7); one session may hold the seat in several teams                                           |
| LD2  | A liaison without a coordinator                      | Refused at assignment (LZ2)                                                                                                   |
| LD3  | The coordinator loses `AskUserQuestion`              | Yes, only while a liaison is active in its team (LZ16)                                                                        |
| LD4  | The coordinator's `ask-human` with an active liaison | Handed to the liaison; `--direct` only after the hand-off timeout or with an inactive liaison (LZ8, LZ11)                     |
| LD5  | An answer typed in the liaison's terminal            | Information only, `relays: terminal`; a decision that needs authority is answered in the questions room (LZ7a)                |
| LD6  | Who owns `notify-owner`                              | Both may; the liaison is told to use it and the coordinator to hand it over (LZ21)                                            |
| LD7  | Read access beyond the liaison's own checkout        | None at first; other repositories by a later plan if needed                                                                   |
| LD8  | Progress updates                                     | When the human asks, or when the coordinator hands one over; no timed digests (LZ13)                                          |
| LD9  | Timers                                               | `liaison_offline_s` 600, `liaison_relay_s` 900, `liaison_handoff_s` 1800, per team in `agent_bus_teams`                       |
| LD10 | v1 member side in Python                             | Yes, as 00211 D9's interim (pingbus and admin are Python until v2); every new piece outside them (guard, mounts, board) is Go |

## Risks & Mitigations

| Risk                                                | Impact | Probability | Mitigation                                                                                                                      |
| --------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------- |
| The extra hop's latency slows an urgent question    | M      | M           | Live delivery wakes the idle liaison at once; a hand-off timeout and the inactive fallback (LZ10, LZ11); latency measured (8.2) |
| A stale or wrong summary misleads the owner         | M      | M           | `as of` time, source IDs and "no news since" in every update; it asks rather than guesses (LZ14)                                |
| A relay is taken as the human's authority           | H      | L           | Relays are `PEER`; the human's verbatim event is checked by the receiver, not the liaison; `verify-human` (LZ5-LZ7)             |
| The liaison is offline, and questions go unanswered | M      | M           | Inactive after `liaison_offline_s`: the old route comes back, the coordinator is woken, nothing blocks (LZ10)                   |
| The `ro` mount breaks a session's own writes        | M      | M           | Task 0.1 measures them; writable per-session dirs for exactly those (LZ17)                                                      |
| A host `cc` liaison writes through Bash             | L      | L           | Stated as guard-only (LZ19); ccy or LXC for a liaison with a boundary                                                           |
| Old members distrust a record naming `liaison`      | H      | M           | Upgrade-first; `set-role` refuses while a running member is old (DESIGN section 9)                                              |

## Success Criteria

- [ ] A coordinator's `ask-human` reaches the human through the liaison. The answer comes back
  to the coordinator as a relay, with the human's verbatim words marked `checked on the homeserver`.
- [ ] A relay naming a non-human event is refused on send, and a forged one is shown
  `NOT VERIFIED`.
- [ ] The liaison cannot `Edit`, `Write` or commit; in ccy and LXC a Bash write to the checkout
  fails.
- [ ] The coordinator is not woken for questions-room messages while the liaison is active, and
  is woken when it is not.
- [ ] A team with no liaison behaves as before; the golden tests pass.
- [ ] `qa-changed.bash` passes on each commit, every review ends at MERGE, and `qa-reviewer`
  passes.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00225-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan and design filed (plan only); reviews in `subagent-reports/`.
