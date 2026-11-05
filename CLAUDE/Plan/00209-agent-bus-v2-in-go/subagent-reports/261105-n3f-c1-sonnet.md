# n3f-c1 (the questions room's sync, QS1-QS10): stopped early; the engine's half built, `teamsync` has red tests only

Branch `feat/bus-v2-n3f-c1`, from `feat/bus-v2-n3f-a-int` (133a5fe4f). Builder: Sonnet 5.5. The builder
spent its context reading the spec, the sync code and the n3f-a reports and writing the design below,
and stopped at a clean point as the brief asks. **QS1 to QS10 are not built in `teamsync`.** Read "Not
done, and what the next builder needs" first if you continue.

## Built (green)

| Part                          | What                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        | Where                                                                       |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| n3f-D19, QX5 (the one writer) | `team.Batch.Questions *proto.QuestionsState`: the only way the `questions` key is written; a batch without it leaves the key as it was, byte for byte. A copy is stored (JSON round trip), so the team goroutine's index never aliases the engine's memory                                                                                                                                                                                                                                  | `member/engine/questionswrite.go`, called from `Persist` in `storewrite.go` |
| QS9, the engine's half        | `team.Batch.DropQuestions`: in the one store write, the questions room's pending and handed items (an item awaiting proof stays, as a redaction leaves it, DV4), the `questions` key, `reminded_ms`, `remind_late`, `outbox.questions_unechoed`, `outbox.questions_uncounted`, `outbox.questions_handover` and a `sending` entry of the room; the questions floor stays (`floor_ms` is untouched). Each dropped item is logged by id and each dropped post by transaction id, never by body | same                                                                        |
| QS2, QS8 data                 | `team.Health.Questions` (`QuestionsHealth{State, Check}`) and the states `awaiting-invite`, `left`, `building`, `ready`, `untrusted`, with the check `invite`                                                                                                                                                                                                                                                                                                                               | `member/team/team.go`                                                       |

Tests: `engine/questionswrite_test.go` (`TestPersistWritesTheQuestionsKeyOnlyWhenTheBatchHoldsIt`,
`TestPersistDropsTheQuestionsRoomForADemotion`).

## What each test was shown to catch

Both were red first, for the right reason: the key written was `null` (no writer), and the demotion left
the questions items, the key, the reminder state, the outbox keys and the `sending` entry. Then green.
Then five defects planted one at a time into `questionswrite.go` (`untracked/scratch/mutate_engine.py`,
scratch, not committed; the file restored after each), each run against the two tests, **all five red**:
the awaiting guard removed (the item awaiting proof was dropped), the `sending` entry not dropped, the
key not dropped, the three outbox keys not dropped, and the key rewritten by every batch (the key changed
under a batch that did not hold the room). A first attempt at the last one (`|| s.Questions == nil`)
survived because it is an equivalent mutant (a nil batch key over a nil store key); the replacement
(`if true`) is red.

## Written and red (committed as work in progress, so the next builder starts from them)

`member/teamsync/questions_test.go` over `member/teamsync/questions.go`, which holds a compile-only
skeleton (`IndexBuilder`, `IndexJob`, `Team.IndexAtHead`, `Team.QuestionsStatus`, `Config.Builder`,
`Team.onFilter`; the last two are in `teamsync.go`). Run against the skeleton, each fails for the stated
reason, which is the defect and not a build error:

- `TestQS2TheQuestionsRoomIsJoinedByTheRostersIDAtTheStart`: 0 joins of the room, no builder job.
- `TestQS2ARefusedJoinAwaitsTheInviteAndTheInviteIsTheSignal`, `TestQS2NoInviteFor300sIsUntrusted`: the
  status is empty (no state is kept).
- `TestQS1TheRosterAndTheInviteInOneAnswerJoinTheRoom`: 0 joins and the invite **left** once, which is
  the old order (the invite judged against the roster from before the answer). This is the test of
  QS1's reason (n3f-D15), driven by `handle` directly because fakehs cannot put a roster and an
  invite in one answer of a running long-poll.
- `TestQS4TheFilterFollowsTheQuestionsRoomsReadiness`, `TestQS5ALoadedIndexAtThePositionNeedsNoBuild`:
  time out waiting for a filter, because `poll` does not announce one yet (`onFilter` is declared and
  never called). Give `go test` a `-timeout`, or they hold two minutes.
- `TestQS2ARejectedInviteIsLeftThroughTheAnswer` and `TestQS3ACoordinatorWithoutHumanTextStopsTheTeamAndJoinsNothing`
  pass against the skeleton by accident of the old code (a worker's invite is rejected, a roster with
  `human_text: false` already fails main's trust): they pin behaviour the new code must keep.

`world_test.go`'s fake engine applies `Questions` and `DropQuestions` as the real one does. The
existing tests are untouched and green (`-skip TestQS`).

## Gate results, exactly

- `go test -race -count=1` over `member/{teamsync,engine,team,state,wiring,ends}` and `proto`, with
  `-skip TestQS` for `teamsync`: all seven ok. Not run: `-shuffle=on`, the tagged builds, `link`,
  `keeper`, `cli`, coverage figures (nothing of `teamsync` is built, so a figure would say nothing).
- `golangci-lint fmt` over the three packages: clean. `golangci-lint run` over them: the two findings in
  files this slice did not change (`unparam` on `world.as`, which every caller gives `coord`) are not
  mine; the test file's own findings were fixed.
- `./scripts/qa-changed.bash`: see the commit message of each commit.

## Not done, and what the next builder needs

Everything of QS1 to QS10 in `teamsync`. The design below is the builder's, reached by reading
`session.go`, `proto.Invite`, `VerifySide`, the filter and the index API; the spec's rules are the
authority.

01. **Where state lives.** A `qroom` struct on `Team` (`t.q`), guarded by its own mutex (the builder
    goroutine and the verbs read it): the roster-derived role (non-parked coordinator and a
    questions room id), `joined`, `left`, `verified` and the failed `check`, the index `idx`, `head`,
    `last` (the last folded event id), `chain` (below), the join clock (`since`, a `Boot()` time) and
    the last join try. `session()` resets what the store holds (the index, `head`, `verified`) and
    keeps the clocks, which are the link's. `QuestionsStatus()` computes from the clock at call time:
    awaiting-invite becomes untrusted with the check `invite` at 300 s since `since`, so a verb at 299 s
    and 301 s sees the change with no timer.
02. **QS1's order in `handle`.** Split `a.resp.Invite` into main's and the rest; (1) `invites(main's)`,
    (2) trust when triggered (a join of main only now), (3) the demotion check against the new roster
    (QS9, before anything else), (4) `invites(the rest)` with the new roster, (5) the questions room's
    membership and trust, (6) main's timeline, then the questions timeline. Keep `invites(ctx, map)`
    as it is (`TestInvitesPendingAndRejected` calls it); for an `InviteAccept` of the questions room do
    not join the invite's id, call the join by the roster's id (QS2).
03. **QS2 join.** Tried at each session start, at each answer while wanted and not joined and 30 s have
    passed since the last try, and at once when an invite is seen. Finding: the spec says "at most
    once per 30 s" and names the invite as a trigger; read literally an invite arriving 5 s after a
    refused start try would wait up to 30 s and nothing would retry it, so the invite bypasses the
    throttle (the smallest reading that keeps "an invite is the signal" true). 403 is awaiting-invite;
    another error is the team's backoff (n3f-D8). The 300 s clock starts at the later of the first
    roster naming this member and its own latest leave.
04. **QS4, G5.** `filter()` becomes `(json.RawMessage, bool)` and passes `joined && verified && !untrusted && head` as `questionsReady` to `proto.SyncFilter` and `SyncFilterReAdd` (session.go:183
    and :186 pass `true` today). Call `t.onFilter(f)` before each `Sync`. A change of readiness while a
    long-poll is in flight (the builder's `IndexAtHead`, a verb's re-verification) must end it: a
    buffered `moved` channel, a watcher started with `wg.Go` per request that cancels a
    `context.WithCancelCause` request context with a sentinel when the filter it was sent with is no
    longer the current one, and `poll` retrying with the new filter on that cause. `project_goroutine_end`
    accepts a WaitGroup's `Go`.
05. **QS5.** After the join, read the room's full state (`RoomState`) and run `proto.VerifySide( PurposeQuestions, roomID, state, roster, anchors)`; a `TrustError` is untrusted alone with its check, a
    403 or 404 on the state read is untrusted with the check `state`, anything else is transient. Take
    the own join time from the member's `m.room.member` state event in that state. Index load at
    session start: no key, or a key with a `build` record, or a token that is not the store's
    `sync.next_batch`: call `Config.Builder.Begin(IndexJob{Key, RoomID, Floor})` (a nil builder logs once
    and leaves the room at `building`); a key with no `build` and `token == next_batch`:
    `proto.LoadQuestionIndex(server, roster.QuestionFloor, *key.Index)`, at the head, `last = *key.Last`.
    The questions floor is the later of the store's `floor_ms.questions` (with no store, the earlier of
    the room's `teamchat.floor` marker and `released_ms`) and the own join time; judge questions events
    with that cut. **Per-room markers are not built:** `floorMarker` (`t.floor`, `knowFloor`,
    `floorMoved`, `putFloor`, `FlushFloor`) and `team.Team.FloorRaised(floor)` are main's alone and the
    engine passes only main's floor (`recv.go:565`); a per-room map and a second floor argument touch
    `member/team` and `member/engine`, which n3f-b is also editing, so do it after the merge.
06. **QS6, the receive path.** `judgeAll` takes a list of rooms (id, purpose, events, resolver).
    Judge the questions events with `proto.Judge{RoomID: questions, Purpose: PurposeQuestions, ...}`
    over the same flood window and `memLookup`; `Pending.Room` is `PurposeQuestions`; the verdicts of
    outcome `Index` fold into a **`Copy()` of the index**, and that copy replaces the live one only
    after `persist` succeeds (the commit rule QX4 needs, and the only way a held answer never attaches a
    post twice). Attach every human post that has a decoded `Human` whatever its outcome (`Deliver`, or
    a `Drop` for `rate` or `edit`; `Judge.human` sets `Verdict.Human` only after `DecodeHuman` succeeds,
    so `v.Human.Body != ""` is the test), through `Attach(v.Event, v.Human)`. **Use a second resolver
    for the questions room:** `resolver` caches by event id and marks a 404 or 403 unknown for the
    link's life, so a questions-room relation that names a main event would poison main's resolution
    (a human could do it on purpose). `Batch.Questions` is set only when the answer's filter held the
    room (record it in `answer`, from the filter actually sent) and its questions timeline was folded:
    `QuestionsState{Index: &state, Token: &next_batch, Last: &last, Build: nil}`.
07. **QS7.** `fill` takes the room id. With `chain` true (the previous request held the room and its
    write was made) a limited questions timeline is filled `to: since` as main's is; past `pages.max` it
    is a gap: set the answer's questions events aside, `head = false`, `Builder.Begin` a catch-up with
    the store's key, persist main as usual with `Questions` nil. With `chain` false (the room has just
    entered the filter), fill backwards until a page holds `last`, keep what follows it, and treat a
    fill that does not find it as the same gap. `chain` is true after a loaded key at the position or
    an answer that wrote the key, false after the builder's `IndexAtHead`.
08. **QS8, QS9.** A questions `Leave` entry or a failed check is that room's alone (filter drops it, its
    items stay, `QuestionsStatus` says so; re-verify at the next start, roster change and verb). A roster
    that no longer names this member the non-parked coordinator, seen in step 3 of QS1: `Builder.Stop()`,
    `Engine.Load` for the ids to log, then `Persist(Batch{DropQuestions: true, NextBatch: ...})`, and
    the room is ignored for the rest of the answer, so a demotion's roster and its kick in one answer
    are a demotion and not a trust failure. A verb racing the demotion must see 11 `role` once the
    write is made.
09. **`Team.Refetch` refuses any room but main** (`verbs.go:257`, `ErrItemGone`): a restored questions
    item is dropped. Judge it with the questions `Judge` when the roster names the room and this member
    coordinates it.
10. **Existing tests that change.** `TestTheCoordinatorJoinsTheQuestionsRoom` expects one join and at
    least three `/state` requests; the start now joins by id and verifies the side room, so its counts
    change with a stated reason.
11. **Seams for c2**: `IndexBuilder.Begin/Stop`, `IndexJob`, `Team.IndexAtHead(idx, last)`, which must
    also reset `chain`, poke `moved` and set the ready state; the builder writes its pages itself
    through `Engine.Persist(Batch{Questions: ..., Items: ...})`. The ready line
    (`outbox.questions_handover`) and `questions_unechoed` are c2's writes. The `status` room
    record reads `Health.Questions`; `engine/status.go:323-332` still says `joined` whenever the roster
    names a room, and is n3f-b's file too.

## Findings

- The brief names `leakcheck.Poll`, `leakcheck.Drive` and the rules `project_test_spin` and
  `project_poll_pure`. None exists in this tree (`internal/leakcheck` has `Tests` and `Check` only;
  `GoProjectRules.md` has no such rule). The tests wait on channels the team already offers
  (`running.poll`, `world.batch`, a new `onFilter`), with no spin loop.
- `team.Batch` carries `Questions` and `DropQuestions` instead of new `team.Engine` methods: three fakes
  (`ends`, `teamsync`, `wiring`) implement the interface, and the one-writer rule (n3f-D19) is
  clearer when every write is a `Persist`.
- `Persist`'s dropped ids are not returned (its signature is shared by the three fakes); the team
  goroutine logs a demotion's ids from an `Engine.Load` taken just before, and the engine logs each
  item it actually drops.
- QS9 says "returns and drops" handed items: the engine just removes them from `pending`, since
  returning a handed item and dropping it leave the same store; an item awaiting proof is kept, as
  `apply` keeps it for a redaction (an assumption from B 2.3.2 n3e-6, to be confirmed by the reviewer).
- The `Health` struct gained a field; `reflect.DeepEqual` and `==` over it elsewhere still compile and
  pass.
