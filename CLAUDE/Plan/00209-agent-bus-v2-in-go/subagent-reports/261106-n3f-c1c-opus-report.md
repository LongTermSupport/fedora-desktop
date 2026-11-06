# n3f-c1c (the questions room's sync, continued): QS7 built, QS8's and QS6's tests written; floor markers, Refetch and the rest open

Branch `feat/bus-v2-n3f-c1c`, from `feat/bus-v2-n3f-c1b-int` (d00227755). Builder: Opus 5.5. The
builder stopped at its context budget at a clean commit, before item 4, as the brief asks.

## Built, per item

| Item                    | State     | Commit    | What                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ----------------------- | --------- | --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. QS7                  | built     | f0dca1cb5 | `qgap` fills a limited questions timeline through `fill` (now taking the room): back to the page holding `q.last`, and with `q.chain` to the answer's `since` as main's fill; only the events after `q.last` are folded. Not reached (past `pages.max`, or the room's start without `q.last`): `qcatchup` sets `head` false, pokes, and hands `IndexJob{Key: store.Questions, RoomID, Floor: q.floor}` to `qbegin` (the one hand-off to the builder; the nil-builder line said once); `j.q` stays nil, so the key and the questions floor stay as they were. A transient fill failure returns the error (the answer held, n3f-D8); a 403 or 404 is QS8's check `state` |
| 2. QS8's tests          | written   | e9a636d71 | each failed check at the start (state 403, state 404, a join rule `VerifySide` refuses) with the worker filter and main's item delivered; a trust event in the room's own timeline re-verifies and a failed check leaves the filter; a refused history read (QS7's fill) is `state`, main's item in the same answer delivered, the key left; a kick with the roster still naming the member is `left`, the join tried again, the 300 s clock restarted at the kick (left at 299 s, untrusted `invite` at 301 s); recovery begins QX1's catch-up from the store's key with the room's floor. **Defect found and fixed** (see Findings 1)                                |
| 3. QS6's missing tests  | written   | 2e557a3d8 | with the admin's Q3 folded by the sync: a post in its thread, a reply to that thread post, a `Q3:` prefix, an edit of the attached post (`revises`) each delivered in `room: questions` and attached to Q3; a post older than `human_max_age_s` delivered in questions while one as old in main is not; posts past `recv_per_sender_minute` dropped `rate` and each still attached. The worker's post dropped `sender` was already pinned (`TestQS6AWorkersPostInQuestionsIsDropped`)                                                                                                                                                                                  |
| 4. Per-room floor marks | not built | -         | designed, not started; see "Not done"                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| 5. `Refetch` questions  | not built | -         | -                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| 6. The commit rule test | not built | -         | -                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| 7. `status` record      | not built | -         | -                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |

Two test-only commits fix flakes the gate tripped on: f12e4e598 (the QS2 refused-join test) and
97b288b8e (n3f-b's refused reports join test; see Findings 3 and 4).

Tests: `questions_gap_test.go` (QS7, five; its helpers `readyWith`, `filterGate`,
`persistedBefore`, `questionPosts`, `keyIndex` are shared by the QS6 and QS8 tests),
`questions_untrusted_test.go` (QS8, five; one is a table of three), and three more in
`questions_receive_test.go` (QS6). `readyCoordinator` is now `readyWith(w.main, nil)`.

## What each test was shown to catch

Every QS7 test was red before the code (the old `qjudge` folded a limited timeline as is). Each
plant below was made by hand, the tests run, and the code restored (the diff checked empty
after each):

- QS7, no fill (`qgap` returns the timeline as is): all five QS7 tests red.
- QS7, the found event kept (`events[i:]`): the no-chain test red (the last folded event
  attached again).
- QS7, `head` left true in `qcatchup`: both catch-up tests red (status ready, the next filter the
  coordinator form).
- QS7, a nil key handed to the builder: the past-the-bound test red. Reached ignoring the chain
  (`!gap`): the not-found test red (the room's start read as reached).
- QS8, a refused state read only logged: the 403 and 404 cases red. `sideTrustTriggered` never
  true: the trust-trigger test red (no state read after it, still ready); the recovery test red
  at its first check. The kick not restarting the clock: the kick test red (untrusted at once).
- QS8 recovery: red against the code before e9a636d71 (no catch-up begun), green after.
- QS6, `Attach` only for delivered posts: the rate test red (the dropped posts unattached).
  `Attach` without the post's relations: the thread, reply and edit cases red, the prefix case
  green, as it must be.
- **Not proven:** the old-post test (QS6, "never `stale`") was green from its first run, the
  code already right. Its rule is `proto.Judge`'s; the one teamsync plant that reaches it
  (judging the room with main's purpose) turns every post into "not addressed", which proves
  nothing specific. The test does carry its own guard (a post as old in main is not
  delivered), so the age is known to be past. Also not proven: the kick test's "join tried
  again" (a 10 h old try is due anyway, so a plant that drops the force is not caught).

## Gate results, exactly

- `go test -race -count=1 -shuffle=on ./internal/teamchat/member/teamsync/`: ok at f0dca1cb5,
  e9a636d71 and 2e557a3d8 (about 20 to 35 s each). `golangci-lint run` over teamsync: 0 issues
  at each commit.
- `./scripts/qa-changed.bash --base feat/bus-v2-n3f-c1b-int`, first run (started after
  f0dca1cb5): **void.** It ran in the working tree, which held QS8's planted defects for part of
  the run: it failed go on the two planted QS8 tests and on
  `TestARefusedReportsJoinIsTriedAgainAtARosterChangeAndAReport` (a real flake, Findings 4);
  every other go-qa stage passed. Lesson for the next builder: run the gate in a detached
  worktree of the commit, never in the tree being edited.
- Second run, in a detached worktree of 97b288b8e: queued behind another agent's run on the
  shared lock for over ten minutes, then stopped by this builder so that one run covers the
  whole branch.
- Third run, in a detached worktree of 3367c8199 (every commit of the branch): **failed go**,
  on one test only: `wiring`'s `TestClaudeStopBlocks` (`e2e_claude_test.go:359`, "the Stop of
  a new pending set answered `{}`", shuffle seed 1793940311914825481). Every other go-qa stage
  passed (fmt, record, suppression, summary, docs, golangci, analyzers, deadcode, the
  failpoints lint and vet, project-rules-tagged, vuln, mod, build). teamsync's tests passed (94.8%
  coverage), and the docs and `teamchat-keeps-off-v1` gates passed. The same test run again
  alone at that commit (`go test -race -count=5 -run TestClaudeStopBlocks`) passed 5 of 5. It is
  a real-process end-to-end test of the main room's Stop hook, which this branch does not touch,
  so it reads as a flake under a loaded host; it was not investigated further. The coordinator
  should re-run the gate on the head before merging.
- Not run by hand: the tagged builds (go-qa runs them).

## Readings where the spec was silent

1. **The chain's fill reads to `since`**, as main's does, and is reached when it gets there
   with no gap, even without meeting `q.last` (with the chain, nothing of the room lies between
   `q.last` and `since`). Without the chain only finding `q.last` is reached. A limited
   timeline with no `prev_batch` is not reached (the catch-up's).
2. **QS7's 403/404 on the fill is the check `state`** (QS8 names history reads with state
   reads), and main's items of the answer are still taken.
3. **Recovery owes the index again**: a failed check clears `begun`, so the re-verification
   loads the key when it is at the position, or begins the catch-up from it.

## Findings for the owner or coordinator

1. **Defect, fixed (e9a636d71):** `quntrusted` dropped the head but left `begun` set, so a room
   that passed its check again (next roster change) never reloaded or caught up its index and
   stayed `building` until a session restart; nothing posted while it was untrusted was taken.
   Defence Before Fix: one flag not reset on one transition is not a shape an analyzer or a
   semgrep rule can express; fixed conventionally and pinned by
   `TestQS8RecoveryCatchesTheRoomUpFromItsToken`.
2. **A kick or leave is unseen while the room is out of the filter.** fakehs sends no `leave`
   section at all (the kick test builds its answer by hand through `handle`), and the worker
   form names main alone, so a kick during a build, a catch-up, an untrusted spell or after a
   session restart reaches the team only as a refused read later (the builder's 403, which is
   `state`, not `left`), and QS2's clock is not restarted. Also, a session restart (a freeze
   or suspend) keeps QS2's clock but re-reads membership by state: a kick found that way makes
   the room `untrusted invite` at once if the term is older than 300 s. This is OW6's ground:
   option (b) (the coordinator form while the room is awaited) would also carry the room's
   membership. Suggest: when the state read shows this member's membership `leave` or `ban` for
   an event not seen before, treat it as QS8's own leave (`left`, the clock restarted). Not
   built: it changes QS2/QS8 behaviour the spec does not state.
3. **QS2 test flake (fixed, f12e4e598):** `AwaitDue(longPoll)` could share its due with the
   reports join's 30 s retry and the clock refused it as ambiguous (4 in 40 runs hung). The
   clock's own refusal is the class's detector.
4. **n3f-b's `TestARefusedReportsJoinIsTriedAgainAtARosterChangeAndAReport` was flaky on the
   integration branch too** (5 in 50 under `-race`, reproduced on d00227755 in a scratch
   worktree): it counted joins before the reports loop's try had ended. Fixed in 97b288b8e
   (test only, waits for the state the try puts).
5. **A floor in the same millisecond is never delivered**: the floor is the latest origin time
   of the last persisted answer, compared strictly, so a post in the same millisecond as the
   previous answer's newest post is taken by neither (true of main too). The tests step the wall
   clock; on a real server the window is a millisecond, but the owner may want the floor rule
   to carry the event id (as SY1's landmarks do) rather than the time alone.
6. **The engine's `Persist` returns main's floor only**, while `ownedFloors` holds every room's
   floor below that room's owed items. The questions marker (item 4) must therefore take the
   store's questions floor from a `Load` after the write, not the batch's; and `FloorRaised` is
   main's only, so a questions floor raised by an ack is not marked until the next answer moves
   it.

## Not done, and what the next builder needs

- **Item 4, per-room floor markers.** Design, not started: a second `floorMarker` on `Team`
  (`qmark`, under `t.mu`), `knowFloor`/`floorMoved`/`putFloor` taking the marker and the room;
  after a write that held `Floors[questions]`, `Engine.Load` and `floorMoved` with the store's
  questions floor (Findings 6); `putFloorIfDue` and `FlushFloor` put the questions marker in
  the questions room while it is joined; `qindex` with no store floor reads the marker in the
  questions room (`s.marker` taking the room) and takes the earlier of it and `released_ms`, one
  alone when only one exists. Tests: a questions floor move puts the marker on the questions
  room (the coordinator's room account data); with no store floor and the member already
  joined earlier, the build's `Floor` is the earlier of the marker and `released_ms`, each way
  round (pre-join the coordinator, then `StepWall` before putting the marker).
- **Item 5:** `Team.Refetch` still refuses any room but main (`verbs.go`).
- **Item 6:** the commit rule's test (a failed write leaves the live index unfolded, QX4). A
  failed `Persist` returns an error from `handle`, `session` returns, `qsession` drops the index
  at the next session; the testable claim is that the re-read answer attaches each post once
  (the live index never held the failed fold). Use `w.eng.setPersistErr` once, then the
  re-read's key.
- **Item 7:** `status`'s questions record (`engine/status.go`) still reads its own `joined`.
- **QS7's own end-to-end tests** (the 2100-event gap, the kill mid-catch-up) need c2's builder;
  here the hand-off is tested with `fakeBuilder`.

## Shared files changed

`teamsync/session.go` (`fill` takes the room; `qjudge` takes `since`), `teamsync/questions.go`,
`teamsync/questions_receive_test.go`, `teamsync/questions_test.go` (the QS2 test's wait),
`teamsync/reports_test.go` (n3f-b's test's wait). Nothing in `engine`, `cli`, `wiring`, `proto`.
