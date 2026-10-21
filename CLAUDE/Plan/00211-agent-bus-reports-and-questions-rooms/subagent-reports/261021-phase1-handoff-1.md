# Plan 00211 Phase 1: handoff 1

The first Phase 1 implementer stopped at its context budget after Task 1.1. This is where the
work stands and the design the next agent should follow, so it does not have to re-derive it.

## Done (branch `agent-a8f8e58fd75acf6cd-d331941f`)

- Merged the spec branch (fb4c77b1). Filed the review as `261021-review-00211-opus.md`.
- Step 0 (edb72d95): the re-review's R1-R3 applied to SPEC.md and PLAN.md, and journalled.
  - R1: no beacon, no "every coordinator held" pre-flight, no `held` state, no second play leg.
    Admin invites every coordinator. Deploy 2 waits up to 180 s for each to have joined
    questions, and names any not joined with its membership. A `leave` one is invited again by
    the next run.
  - R2: the inbox `room` key is top-level, on questions-room items only.
  - R3: a `human_text: false` coordinator is refused at role assignment (`members.py` entry,
    `admin.add_member`). A sync never refuses it. set-role cannot see the flag, so in that one
    case the member leaves the invite pending, and status and the hook context name the
    reason.
- Task 1.1 GREEN (418ae31c, RED adb84a3b): `helpers/pingbus/protocol.py`, which now has:
  - the constants `EVENT_ROOMS`, `EVENT_ROOM_LINK`, `REPORT_KEY`, `QUESTION_KEY`,
    `QUESTION_STATE_KEY`, `ROOM_MAIN`, `PURPOSE_*` and `RECEIVING_ROOMS`;
  - the caps, the states, and `IGNORE_NOT_COORDINATOR`;
  - `SEND_REFUSALS += "no-room"` (the doc's §9 line is updated);
  - the index and link checks: `parse_rooms_index`, `check_room_link`;
  - the power levels and join rules: `side_power_levels(purpose, humans, open_=)`,
    `check_side_power_levels`, `expected_join_rules`, `check_join_rules`;
  - reports: `check_report_text`, `build_report`, `check_report_content`;
  - questions: `check_question_text` (returns the headline and the Question: section),
    `build_question`, `parse_question`;
  - threads: `build_thread_post(root, text, humans, ask=, close=, re=)`;
  - the index: `QuestionIndex` (`views()`, `next_number()`, `open_duplicate()`,
    `attached_to()`), `build_question_index(events, ctx)` and `attach_question`;
  - `HumanMessage.revises`, and `validate_event(..., room=)` with `_questions_outcome`.
  - Tests: `tests/helpers/pingbus/test_protocol_rooms.py`.
- The CCY bump: 3.99.13.0, container 2.78.0, with a changelog entry. **Every commit that
  touches `helpers/pingbus/` or `helpers/agent_bus/` needs a further container bump.** Bump
  the patch, and edit the one changelog entry's heading to the new version. The pre-commit
  gate rejects the commit otherwise, and the launcher change runs every ccy suite (slow), so
  group the GREEN work into few commits.

## Left (Phase 1)

1.2 inbox, 1.3 syncer, 1.4 members/admin refusal plus status/hook reason, 1.5 limits, 1.6 cli,
1.7 envelope, 1.8 hooks and the skill, 1.10 the Go test, 1.11 docs, 1.12 acceptance and deploy
scripts, and the SPEC §1 and §5.6 bump correction (Task 1.9, the PLAN says so). 1.13 is the
coordinator's.

## Design decided (follow it, or journal why not)

- **inbox**:
  - `commit_batch` stores `STORED_KEYS`, and also `room` and `question` when the event dict
    carries them. They are written only for a questions-room item: `"room": "questions"`, and
    `"question": {"n": int, "headline": str}` or `null`.
  - `Item` gains `room` (default `main`) and `question` (`(n, headline) | None`).
  - `_revalidate` reads the key: absent is `main`; `questions` passes `room=` on; anything
    else is rejected.
  - A new `rooms.json` state file, `{"v":1,"rooms":{purpose:{"room","state","reason"}}}`.
    Its states are `join`, `pending`, `untrusted` and `refused`, and the reason is at most 200
    printable ASCII characters. It has a `save_side_rooms`/`side_rooms` pair. `report` and
    `ask-human` read room IDs from it, and so do `status` and `_refetch`.
- **report_per_hour**: it goes into `limits.Limits` (default 6, bounds 1-60), but its send
  times are kept in a **separate** `state/reports.json`, not in the outbox gate. An old
  reader checks that the gate's keys are exactly `{bucket, recent}`, and would raise
  StateError during deploy 1's in-place coexistence window. Journal this deviation from SPEC
  §7. The duplicate check reuses the gate's window, through `admit_send("report", digest, ...)`
  as `say` does.
- **syncer**:
  - `sync_filter(room_id, limit, also=())`. Add `EVENT_ROOMS` to the state, timeline and
    trust types, and fix `test_filter_is_the_one_probe_h4_recorded` to strip it as it strips
    TOPIC.
  - After the main room verifies, `_sync_side_rooms()` does three things. It reads the index
    with get_state, where NotFound means no rooms. It joins the reports room once (POST
    /join), recording `join` or `refused` and never verifying at sync time; `report` verifies
    at send. As coordinator with human text and not yet joined, it tries to join questions:
    403 is `pending` ("not invited yet"); on success it verifies the room (create, link, the
    open or closed power levels from `bool(coordinators(record.roles))`, join rules), giving
    `join` or `untrusted`. Not a coordinator gives `pending` and no filter entry.
    `human_text` false gives `refused` with the R3 reason.
  - The filter includes the questions room for a coordinator (so its invite is seen).
    Invites: an acceptable questions invite is joined. Any other admin invite, whether
    unindexed or for a non-coordinator, is left pending and logged. Everything else is
    rejected.
  - A newly joined questions room: in the first batch after the join, process only events
    after this member's own join membership event, and do no gap fill (SPEC §4.4 skips
    history).
  - Questions batch: re-verify questions on its trust types (plus join_rules and
    agent_bus.room). On failure it is `untrusted` and dropped from the filter; the main room is
    unaffected. Gap-fill it with its own prev_batch. Validate with `room="questions"`. When
    anything is accepted, build the `QuestionIndex` from a bounded backwards `/messages` scan
    of that room, and store each item with its attachment. The fake homeserver needs
    restricted joins modelled (`_join`: allow when join_rules is restricted and the user is
    joined to an allowed room).
  - `history(limit, room="main")`: the questions room uses `room="questions"`, and its own
    posts are SENT items.
- **cli**:
  - `report` (any role), `ask-human`, `thread [Q<n>] [--ask] [--close] [--re]`, `questions [--all]`, and `history --room`.
  - QUESTION line, one schema: `QUESTION 1 TEAM Q<n> STATE EVENT_ID ASKED_BY UNANSWERED`.
    `ask-human` prints it with `open ... 0`. SPEC §8.2's shorter form conflicts with §8.4, so
    correct §8.2. Also `REPLY_TO 1 TEAM Q<n> EVENT_ID`.
  - `human_line` appends ROOM (`-`, `questions:Q<n>`, `questions:-`). `HISTORY` lines grow by
    the same appended field.
  - The REPLY command for a questions item is `pingbus --team T thread Q<n>`, or `thread --re 'E'` when the item is unattached.
  - `_refetch`, and the read receipt at cli.py ~1821, use the questions room ID for a
    questions item.
- **envelope**: `_FIELDS["HUMAN"]` becomes 8. `entry_from_line` takes `question_headline=` and
  `revises=` keyword arguments from the item and outcome. It emits `room:`, `question: Q<n> "<rendered headline>"` (or `question: none`) and `revises:` after `team:`, and only for
  questions items.
- **R3 surfaces**: `status` prints `questions: refused (human_text is false; re-issue the bundle without --no-human-text)`, and the hook context says the same.
- **members.py / admin**: a `MemberError` for a present coordinator with `human_text: false`.
  `admin.add_member` refuses coordinator with `human_text=False`, as an AdminError: "a
  coordinator must take human text: drop --no-human-text".

## Open points to journal

- Phase 0 (the spike) has not run. Phase 1 assumes that the restricted join works.
- SPEC's §1 table says an older pingbus "rejects every other invite". But its `/sync` filter
  names only the main room, and the fake's filter hides invites to other rooms too, so an old
  member may never see the questions invite at all. The deploy-2 wait leg covers both cases.
