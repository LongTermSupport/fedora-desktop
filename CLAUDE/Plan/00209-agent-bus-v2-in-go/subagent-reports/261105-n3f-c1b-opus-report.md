# n3f-c1b (the questions room's sync in `teamsync`, QS1-QS10): QS1-QS6, QS9, QS10 built; QS7 and QS8's tests open

Branch `feat/bus-v2-n3f-c1b`, from `feat/bus-v2-n3f-c1` (2c0a1f71c). Builder: Opus 5.5. Started from
the first builder's report (`261105-n3f-c1-sonnet.md`) and its red tests; the design there was
followed except where "Readings and findings" says otherwise. The builder stopped at about 70% of its
context, at a clean point, as the brief asks.

## Built (green)

All in `member/teamsync`; `questions.go` holds the room's code, `session.go` the calls into it.

| Rule      | What                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| state     | `qroom` on `Team` with its own lock (`qstate`: room, joined, verified, refused, left, check, head, chain, idx, last, the QS2 clock `since`, the last try, the floor). `qsession` resets what a session reads again and keeps the clock and the failed check. `QuestionsStatus` is computed at the call (300 s with no timer); `Health.Questions` is refreshed after each answer and at a session start                                                                                                                                                                                                                                                            |
| QS1       | `handle`: main's invite, main's trust, the room's role (QS9/QS10), the side-room invites, the room's membership and trust (`qanswer`), main's timeline, then the questions timeline (`qjudge`)                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| QS2       | `qenter`: the room's state read, `Join(<roster's id>)` when not a member, then the state again. An accepted questions invite only sets a signal (`sess.qinvited`), never joins the invite's id. 403 is `refused` (`awaiting-invite`); 300 s since the clock with the join refused is `untrusted`/`invite`; another failure is one log line and the next try 30 s on. Triggers (start, roster rev change, promotion, invite) skip the 30 s                                                                                                                                                                                                                         |
| QS3       | unchanged behaviour, pinned by the WIP test                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| QS4       | `filterFor(ready)`: the coordinator form only when joined, verified, no failed check and at the head. `longPoll` runs a watcher (`wg.Go`) per request that cancels the request with `errFilterMoved` when `qmoved` is poked and the filter now differs; `poll` retries with the new filter                                                                                                                                                                                                                                                                                                                                                                        |
| QS5       | `VerifySide` over the room's full state before the first sync (in `session`, before `start`); a `TrustError` is untrusted with its check, 403/404 on the read is the check `state`. Index: the store's key loaded when it has no `build`, all three fields, and `token == sync.next_batch`; otherwise `Config.Builder.Begin(IndexJob{Key, RoomID, Floor})` (a nil builder logs once). Floor: the later of `floor_ms.questions` (with none, `released_ms`) and the own join's time from the state                                                                                                                                                                  |
| QS6       | `judgeAll` split into `judgeRoom` (parse, View, resolve, judge) used by both rooms; the questions room has its own resolver (`Team.qres`). `qjudge` runs only for an answer whose request held the room (`answer.qheld`, recorded from the filter actually sent): `Feed` for `Index`, `Attach` for every decoded human post whatever its outcome, items via `take(..., PurposeQuestions)` for posts after the floor, `Floors[questions]`. The fold is into `idx.Copy()`, made live by `qcommit` after the write. `qkey` sets `Batch.Questions` (index, token = the answer's `next_batch`, last) only for such an answer that is written anyway or folded an event |
| QS9       | a demotion (in `handle` before anything else, or at a session start) stops the builder, logs the dropped questions items by id, and the answer's own write carries `DropQuestions`; a start by a non-coordinator over a store that still holds the room (`holdsQuestions`) drops it in its first write. `empty()` counts `Questions` and `DropQuestions`                                                                                                                                                                                                                                                                                                          |
| QS10      | a promotion resets the room's state, starts the clock, enters the room (join, verify, `Begin` with the store's key, nil after a demotion)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Run's end | `defer t.stopBuilder()`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |

Tests: `questions_test.go` (the WIP, now green), `questions_receive_test.go` (QS6),
`questions_role_test.go` (QS9, QS10).

## What each test was shown to catch

Each defect was planted by hand, the tests run, the code restored (all restorations checked: no
plant left, suite green):

- the old QS1 order (invites judged before main's trust): `TestQS1...` red (the invite left once),
  and `TestQS2ARejectedInviteIsLeftThroughTheAnswer` red (left twice).
- the filter's readiness as `joined` instead of joined, verified and at the head: `TestQS4...` red.
- `inviteWait` 302 s: `TestQS2NoInviteFor300sIsUntrusted` red at 301 s.
- `Attach` skipped: `TestQS6AnUnmentionedHumanPostIsDeliveredAndAttached` red (no unattached post).
- questions items given `room: main`: the same test red (no questions item).
- `DropQuestions` never set: both QS9 tests red (the demotion one by hanging on `batchWith`).
- `Begin` not called: `TestQS10APromotionJoinsAndBuilds` red.
- Survived, so NOT claimed: dropping the roster-change force (a promotion's first try is never
  throttled, so no test reaches the force); a join plant equivalent under force. The QS6 and QS9/QS10
  tests were written after their code in part (the QS6 receive test was seen red first, by hanging at
  `w.batch()`, before `qjudge` existed); the plants above are their red proof.

## Gate results, exactly

- `go test -race -count=1`: `teamsync` ok 92.8%, `engine` ok 92.7%, `team` ok 100%, `wiring` ok
  100%, `link`, `proto`, `state` ok. `teamsync` `-race -count=2 -shuffle=on` ok, and the QS tests
  `-count=8 -shuffle=on` ok. Not run by hand: the tagged test builds (go-qa runs them).
- Shuffle found three races in the tests, all fixed: the QS5 order check counted the first run's
  long-poll (fakehs records a request when it ends), now judged at the first `onFilter` of the second
  run; the QS2 retry advanced the clock before the long-poll's hold was armed (`AwaitDue` first); the
  QS4 test and the QS6 helper waited for a new long-poll after `IndexAtHead`, which does not come
  when the head lands before the request is made (they now wait for the coordinator filter).
- `golangci-lint run` (and with `--build-tags busfailpoints`) over `teamsync`: 0 issues.
- `./scripts/qa-changed.bash`: first commit, passed (a first run failed go on `unparam` for
  `world.as`, every caller passing `coord`: renamed `asCoordinator`); the second, see its commit message.

## Readings and findings

1. **QS2's "at each invite seen" cannot happen through the long-poll.** While the room is not ready
   the filter is the worker form (QS4), which names main alone, and the server filters invites by
   room (fakehs does, as Tuwunel's room filter). So the invite never reaches a member waiting for
   it; the join is retried at each answer once 30 s have passed (every long-poll ends within 30 s).
   The invite trigger is built and judged through `handle` directly (the QS1 test). The WIP test
   `TestQS2ARefusedJoin...` was changed to wait for the 30 s retry. For the owner: either QS4 lets
   the coordinator form through while the room is awaited (no timeline can come before the join), or
   QS2 drops the invite trigger.
2. **A non-403 join failure is not SY8's backoff** (the first builder's design said it is): it is one
   log line and a try 30 s on, so main goes on (QS8 "that room's alone") and the existing
   `TestAFailedJoinHoldsNothing` stays as it was.
3. **The demotion's drop rides the answer's own write**, not a write of its own before the answer:
   a write of its own would need a position (the answer's would skip main's events not yet
   persisted). One store write, as QS9 asks, and the room is ignored for the rest of the answer.
4. **The questions floor compares with `>`**, as main's: a post in the same millisecond as the own
   join is not delivered (it is still attached). The tests advance the fake clock past the join.
5. `joinsOf` counts `POST /rooms/<id>/join` as well: the client's `Join` uses that path; QS2's
   `/join/<id>` is a path form, both by id.
6. A session's start always reads the room's state and joins only when not a member, so a re-join
   cannot move the own join (and the floor) on every restart.
7. The QS5 WIP test raced (see the gate section); it now waits for the first `Run` to return
   (`running.end`) and reads the state-read count at the first sync's filter.
8. The QS9 demotion test and the QS6 tests wait on `batchWith` (at most four batches), but each
   `w.batch()` waits on the test's context alone: a defect makes them hang to `-timeout`, not fail
   fast (the first QS6 red was such a hang).
9. `IndexAtHead` sets `chain` false as designed, but nothing reads `chain` yet (QS7).

## Not done, and what the next builder needs

- **QS7, not built.** A limited questions timeline is folded as is, which skips events (wrong, the
  numbering is a chain). Build in `qjudge`: when `up.Timeline.Limited`, fill the room as
  `s.fill` does main's (make `fill` take the room id), stop at the page holding `q.last`; with
  `q.chain` false fill until `q.last` is found and keep what follows; past `pages.max`, or not
  found: leave `j.q` nil, set `head` false (poke), `Builder.Begin` with the store's key. Tests per
  the spec (the 2100-event gap needs the builder: c2).
- **QS8, built in part, untested:** own leave/kick (`left`, the clock restarted, the join tried
  again) and a trust trigger in the room's section (re-verify) are in `qanswer`; `quntrusted`
  (state 403/404, a failed `VerifySide` check) is in `qread`/`qenter`. No test exercises them
  (coverage shows `quntrusted`, `sideTrustTriggered` unrun). Re-verification at a verb is the verbs'
  (n3f-d): they call nothing here yet.
- **Per-room floor markers** (QS5 "the marker is SY10's, per room"): not built; the questions floor
  with no store uses `released_ms` alone.
- **`Team.Refetch` refuses any room but main** (`verbs.go`): a restored questions item is still
  dropped.
- **QS6 tests not written:** a post in a question's thread, a reply to a thread post, a `Q3:`
  prefix, an edit, a post older than `human_max_age_s`, a post dropped `rate` still attached. The
  path is there (`Attach` is called for every decoded human post); the tests need an index holding
  a question (an `IndexState` with one, through `IndexAtHead`).
- **The commit rule's test** (a failed write leaves the live index unfolded): not written; a failed
  `Persist` restarts the session, which drops the index anyway (`qsession`).
- The `status` record still reads its own `joined` (`engine/status.go`, n3f-b's file), not
  `Health.Questions`.

## Seams c2 fills

`IndexBuilder.Begin/Stop` and `IndexJob` (QX1's build and catch-up); `Team.IndexAtHead(idx, last)`,
which sets the head, resets `chain` and ends a long-poll in flight; the builder writes its own pages
through `Engine.Persist(Batch{Questions: ...})`. Own sends' placement (QX3: `qres` has no own sends),
the compose serialisation (QX4), the ready line and `questions_unechoed` (QX5), redactions (QX7) are
not touched.

## Shared files changed

`teamsync/session.go` (`handle`, `judgeAll` split into `judgeRoom`, `take` gained a room argument,
`resolveInput` a room, `filter` split into `filterOf`/`filterFor`, `poll` through `longPoll`, `empty`,
`answer.qheld`, `judged.q`, `sess.dropOwed`/`qinvited`), `teamsync/teamsync.go` (`Team.q`, `qmoved`,
`qres`; `Run` stops the builder), `teamsync/verbs.go` (one call), `teamsync/world_test.go` and
`trust_test.go` (`as` became `asCoordinator`). Nothing in `engine`, `cli`, `wiring`.
