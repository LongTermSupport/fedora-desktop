# n3f-c1d (the questions room's sync, finished): per-room floor markers, Refetch, the commit rule's test, status

Branch `feat/bus-v2-n3f-c1d`, from `feat/bus-v2-n3f-c1c-int` (1cfb7c8d2). Builder: Opus 5.5. All
four items of the brief are built, each its own commit, test first.

## Built, per item

| Item                         | State | Commit    | What                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ---------------------------- | ----- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1. Per-room floor markers    | built | 245bfa2de | `Team.qfloor` beside `Team.floor` (under `t.mu`); `knowFloor`, `floorMoved` take the purpose (`markerOf`), `putFloor` a `markerRoom` (purpose and room id). A session knows the store's questions floor at its start as it knows main's. After a write that held `Floors[questions]`, `qmarked` loads the store and moves the questions marker to the store's floor (Persist answers main's). `putFloorIfDue` and `FlushFloor` walk `markerRooms`: main, and the questions room while this member coordinates it and is joined; coalescing is per room. `FlushFloor` tries every room and joins the errors. `s.marker` takes the room; its undecodable line is said once per room. With no store floor `qindex` takes `qresume`: the earlier of the questions marker and `released_ms`, either alone, 0 with neither; then the later of that and the own join, as before. `handle`'s tail is now `settle` (cyclop) |
| 2. `Refetch` for questions   | built | 99f34e5f5 | `refetchRoom` takes the room by name (`roomOf`: main, or the roster's questions room; any other refused, usage, `ErrItemGone`) and that room's resolver (`t.res`, `t.qres`); the event is fetched from that room and judged with that purpose                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| 3. The commit rule's test    | built | 1769a7789 | `TestQX4AFailedWriteLeavesTheLiveIndexUnfolded`: a questions write fails once, the team backs off (a bounded wait: a long-poll first fails the test), the answer is read again from the stored position, its post is delivered once and the re-read's write has each post unattached once                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| 4. `status`'s questions room | built | ec1b55dc3 | `questionsRoomRecord` reads `Health.Questions`: `ready` shown `joined`; `awaiting-invite`, `left`, `building`, `untrusted` as they are, `untrusted` with its check; `not-coordinator` while the health's state is empty (this member does not coordinate the room); `absent` while the roster names no questions room                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |

A fifth commit, 8773764a3, fixes the two findings the gate made at ec1b55dc3 (see Gate results
and Findings 1, 2): `markerOf` is a switch with a case per room and nil for any other, and
`engine/send.go`'s `send` gives the entry's fit and the ping bound to `fitEntry`.

Tests added: `teamsync/questions_floor_test.go` (QS5, two; one a table of four),
`teamsync/questions_commit_test.go` (QX4), `TestRefetchOfAQuestionsItem` in `verbs_test.go`,
`TestStatusShowsTheQuestionsRoomAsTheTeamReadsIt` (eight cases) in `engine/status_test.go`.
Changed: `TestRefetch` and `TestARefetchOfAnEventThatIsGoneSaysSo` refuse a room the member does
not know (`reports`, an unknown name) instead of questions, and take a worker's questions refetch
as gone (the server refuses the read); the status and reports-room tests expect a worker's
questions record `not-coordinator`. The teamsync test engine's `Persist` now keeps the questions
floor just below its owed items as `engine.ownedFloors` does (main's rule there is as it was).

## What each test was shown to catch

Every new test was red before its code. Each plant below was made, the tests run, and the file
restored from the commit (`git diff` empty after each):

- Item 1, the batch's floor marked instead of the store's: the floor-move test red (marker at the
  post, not just below it; FlushFloor likewise).
- Item 1, `markerRooms` without the questions room: red (no marker in the questions room).
- Item 1, `FlushFloor` putting main's marker only: red at the FlushFloor step.
- Item 1, `max` for `min` in `qresume`: two of the four cases red. Before the code, the
  marker-earlier and marker-alone cases were red (no marker read).
- Item 2, judged with main's purpose: red (`not-addressed`). Fetched from main: red (404), and the
  worker case of the gone test red too.
- Item 3, `handle` going on after a failed write: red (a long-poll before any backoff).
- Item 4, the check not passed on: the two `untrusted` cases red.
- **Not proven:** item 3's "the live index never held the failed fold" alone. A plant that folds
  into the live index (`qjudge` without `Copy()`) stays green: the failed write ends the session,
  `qsession` drops the live index and `qindex` loads the stored key, so the defect cannot reach
  the re-read. The rule is held twice over; the test proves the outcome, not the first layer.
  Item 2's resolver choice (`t.qres` for questions) has no test that a plant turns red.

## Gate results, exactly

- `go test -race -count=1 -shuffle=on ./internal/teamchat/member/teamsync/`: ok at each commit
  (15 to 30 s). `./internal/teamchat/member/engine/`: ok at ec1b55dc3. `./internal/teamchat/member/...`:
  every other package ok at ec1b55dc3 (that run's one engine failure was the reports-room test,
  then updated in the same commit).
- `go test -race -count=4 -shuffle=on -run 'TestQS5|TestQX4|Refetch|Floor|Marker|TestQS7|TestQS8'`
  over teamsync at ec1b55dc3: ok.
- `golangci-lint run` over teamsync: 0 issues at each commit. Over engine at ec1b55dc3: one
  finding, cyclop on `engine/send.go`'s `send` (17 > 15), the file unchanged from
  `feat/bus-v2-n3f-c1c-int`; 0 issues over both at 8773764a3.
- `./scripts/qa-changed.bash --base feat/bus-v2-n3f-c1c-int`, first run, in a detached worktree of
  ec1b55dc3 (`untracked/scratch/qa-head/`, log `untracked/scratch/qa-head.log`): **exit 1, go
  failed** on two findings, the runners not started: golangci's cyclop on `send` (above), and
  `project_room_default` at `teamsync/session.go` (`markerOf`, `if purpose == questions {...} return main's`), reported by the analyzers, `teamchat-failpoints-lint` and
  `project-rules-tagged` stages. `teamchat-keeps-off-v1` passed. Both fixed in 8773764a3; there,
  `go vet -vettool=<qa/driver>` over teamsync and engine, untagged and `-tags busfailpoints`,
  is clean, and the two packages' tests pass under `-race -shuffle=on`.
- Second run, in the same worktree moved to 8773764a3 (log `untracked/scratch/qa-head2.log`):
  **exit 0**: docs (whole gate), go (go-qa, both modules, diff mode: every detector and runner)
  and teamchat-keeps-off-v1 passed; "qa-changed passed: 13 changed file(s)". The qa-js tests of
  issue #135 were not in this run's scope. The commit holding this report adds only the report.

## Readings where the spec was silent

1. **The questions marker is put only while the room is joined and this member coordinates it.**
   While not (a worker, a demoted member, a room left or awaited), it stays known and owed, and
   is put after the next answer once joined. A demoted member keeps its questions floor (QS9), so
   its marker is never put after the demotion; the store's floor is what QS5 reads first anyway.
2. **The marker's read failing is the session's error** (`qindex` returns it, SY8's backoff), as
   main's marker read in `resume` is; it is the member's own account data, not a read of the
   room QS8 judges.
3. **`FloorRaised` stays main's.** A questions floor raised by an ack is not marked until the next
   answer that moves it, or the next session (whose `knowFloor` owes the store's floor when the
   last put differs). The marker lags low, which costs a redelivery after a seat taken with no
   store, never a lost item. Not changed: it is the engine's interface (`team.Team`) and would
   need `raiseFloors` to report each room's floor.
4. **`status`'s questions states are section 6's**: `joined`, `awaiting-invite`, `left`,
   `building`, `untrusted` (with `check`), `not-coordinator`, and `absent` kept for a roster
   naming no questions room. There is no `joining` for questions (section 6 names it for no
   room; a join not yet made reads `awaiting-invite`, as `qstate.status` already says). A
   coordinator whose team goroutine has not yet given its first questions health shows
   `not-coordinator` for that moment. `uncounted` stays null (QX3, n3f-c2b).
5. **Refetch of a questions item by a worker** reaches the server, which refuses it (403): the
   item is gone. The verbs' `reply` on a questions item (`engine/reply.go`'s `fetched` still asks
   for main) is n3f-d's.

## Findings for the owner or coordinator

1. **One instance of a known class, caught by its own rule.** This builder's first `markerOf`
   tested the questions room alone and gave every other purpose main's marker: the class
   `project_room_default` already names (GoProjectRules.md), and the gate's analyzer caught it.
   Fixed as the rule asks (a case per room, a default that refuses); no new rule is needed, and
   the rest of items 1, 2 and 4 is missing behaviour the spec names, item 3 a missing test.
2. **`engine/send.go`'s `send` was over cyclop's bound on the integration branch itself** (17 >
   15). go-qa's diff mode lints a changed package whole, so the first branch to touch `engine`
   meets it. Split here (`fitEntry`, behaviour unchanged) so the gate passes; the sends' builder
   (n3f-c2b) works in the same function and may meet the move at its merge.
3. **A post in the same millisecond as the floor** (c1c Findings 5) bit this builder's own test:
   two posts on the unstepped fake clock share a millisecond and the second is never delivered.
   The test steps the wall clock; the rule is unchanged.

## Not done, and what the next builder needs

- Nothing of the four items. Open around them: `FloorRaised` per room (reading 3); `reply` on a
  questions item through `Refetch` (n3f-d); `uncounted` in the questions record (QX3, n3f-c2b);
  QS7's own end-to-end tests with c2's builder (c1c's note stands).

## Shared files changed

`teamsync/session.go`, `teamsync/teamsync.go`, `teamsync/questions.go`, `teamsync/verbs.go`,
`teamsync/world_test.go` (the test engine's floor rule), `teamsync/verbs_test.go`,
`engine/status.go`, `engine/status_test.go`, `engine/report_test.go`, `engine/send.go`
(`fitEntry` only). Nothing in `proto`, `cli`,
`wiring`, or the `IndexBuilder` seam.
