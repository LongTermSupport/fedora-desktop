# n3f-c2a (QX1, the question index's build and catch-up): the builder built and wired; the kill-and-resume and QS7 end-to-end tests open

Branch `feat/bus-v2-n3f-c2a`, from `feat/bus-v2-n3f-c1c-int` (1cfb7c8d2). Builder: Opus 5.5. The
builder stopped at its context budget, at a clean commit with the targeted tests green, before
the kill-and-resume tests and QS7's two end-to-end tests.

## Built

Commit 0aefa07a9.

| Where                                | What                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `teamsync/builder.go`                | `Builder` (`NewBuilder`, `Begin`, `Stop`): one job at a time, a goroutine of its own that closes `done` (`project_goroutine_end`); `Begin` stops the job running first. `IndexJob` gains an unexported `s *sess` (the session the job was begun in: its client, its judging), set by `qindex` and `qcatchup`; a job without it is ignored. The job (`buildRun`, `newRun` reads the key as QX5's load says): no key is a build owing the ready line; a record owed (no `start`) a build with its `after` and `late_from`; a record under way a build resumed forwards from the key's token (after `VerifySide`, no backward read); an index with a token a catch-up. A build: `RoomState` + `VerifySide` (a failed check is `quntrusted` with its check); `/messages` `dir=b`, 100 a page, from the store's `sync.next_batch`, keeping only the last page with events and recording `start` (the newest event of the first page); at a page with no `end` or no events, that page folded oldest first, its token the page's `start`; then `dir=f` from that token to a page with no events. A catch-up whose first forward read is refused for good (`ErrRefused`, not transient) becomes a build replacing the index (`after` = the old last event, `late_from` = its `Next()`). Each page: judged by `judgeRoom` and folded by `qtake` under the judging hold, the key built (`Check`ed before the write), one `Engine.Persist` with `Page` and `Questions`, then the judging committed (`commitJudged`), the run's index, token and last event advanced, `after` cleared once passed, the room's floor raised (`qset`). At the head the write's build record is null and `IndexAtHead` hands the index over. Transient failures: retried under SY8's schedule (1 s doubling to 30 s, the team's jitter), told to `onBuildWait`; 403/404 on any read is `quntrusted(room, "state")`; the team's removal ends the job |
| `teamsync/questions.go`              | `qtake`: qjudge's fold loop (Feed, Attach whatever the verdict, `take` for owed events past the floor, the questions floor) moved out so the sync and the builder fold through one function (QX2: "the same function"); `qjudge` calls it. `sideCheck` shared by `qenter` and the builder                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `teamsync/session.go`, `teamsync.go` | `Team.judging` (a one-slot channel) and `judgingHold(ctx, f)`, a hold a context can end; `handle` now runs under it (`handleHeld` is the old body), so an answer's and a page's judge-write-commit never interleave over the flood window and the delivery budget. `commit` split: `commitJudged` (flood, budget, unheard, lines, forge) is what a page also commits; main's floor stays the answer's. `session` stops the builder at its end (a job reads through that session's client; the next session begins its own). Test seams `onBuildEnd`, `onBuildWait`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `team.Batch`, `engine`               | `Batch.Page`: the write leaves `sync.next_batch`, `restore_epoch` and `fresh` as the store holds them (a page is written beside the sync, which moves the position meanwhile). `Batch.Handover`: sets `outbox.questions_handover`; the builder sets it on the first page write of a build begun with no key                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `wiring`                             | `teamsyncConfig` gives each team `teamsync.NewBuilder()`, so production builds (it was nil)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `fakehs/gate.go`                     | `Server.Gate(method, path)`: each matching request held before it is handled (outside the server's lock) until `Pass`, `Open` or its caller going (then dropped, no answer); `Held`, `Arrived` say a request is held. Its own tests in `gate_test.go`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |

Tests: `teamsync/builder_test.go` (three QX1 tests and their helpers: `seed`, which posts through
fakehs in process; `historyRoom`; `teamReads`; `gateQuestions`; `atTheHead`; `watchBuilds`),
`engine/questionswrite_test.go` (`TestPersistAPageLeavesThePositionAndSetsTheHandover`),
`wiring_test.go` (`TestEachTeamHasAQuestionIndexBuilderOfItsOwn`), `fakehs/gate_test.go` (two).

## What each test was shown to catch

Each plant made by hand, the test run, the code restored (each restoration re-run green):

- Engine: `TestPersistAPageLeavesThePositionAndSetsTheHandover` red before `Page`/`Handover` were
  built (position moved to the batch's `p0`, handover false), green after.
- fakehs: the gate's wait made a `default` (no hold): both gate tests red.
- Wiring: the new test red with no builder in `teamsyncConfig` (`<nil>`), green after.
- The forward read started from the oldest page's `end` instead of its `start`: the 250-event test
  red (4 forward reads, want 3) and the transient test red (8 reads, want 7).
- The backoff not doubling: the transient test red (`backoff 1s, want 2s`).
- The handover never set: the 250-event test red.
- **Not proven:** the page's fold taken outside the judging hold: the 10 500-event test stays
  green under `-race`, because its gate orders every page write after the test's main post was
  stored, so no answer and page are judged at once in it. No test makes the two concurrent; the
  hold is held by review only.
- **Not proven** either: the three QX1 tests were written before the builder and were red only by
  not compiling; the plants above are their proof.

## Gate results, exactly

- `go test -race -count=1 -shuffle=on` over `teamsync` and `wiring` at 0aefa07a9's tree: ok
  (51.7 s, 242.7 s). Over `engine` and `fakehs`: ok. An earlier full run of the four packages at
  once (shuffle seed 1793941476467630308) hung in `TestQS6APostDroppedRateIsStillAttached`
  (c1c's test, at `batchWith` after `readyWith`): not reproduced in 30 runs of that test alone
  nor in the whole `teamsync` package with the same seed. See Findings 4.
- `golangci-lint run ./internal/teamchat/member/... ./internal/teamchat/matrix/...`: one
  finding, `engine/send.go:324` cyclop 17 > 15, in a file this branch does not touch (`git diff`
  against the base is empty for it).
- `./scripts/qa-changed.bash --base feat/bus-v2-n3f-c1c-int`, in a detached worktree of
  0aefa07a9 (`untracked/scratch/qa-c2a/`), log `untracked/scratch/qa-c2a.log` in this builder's
  worktree: **exit 1**, go failed on `engine/send.go` cyclop alone (Findings 5); the test
  runners were not started. After the fix, a second run over the branch head in a fresh detached
  worktree (`untracked/scratch/qa-c2a-2/`, log `untracked/scratch/qa-c2a-2.log`) was started and
  had not finished when this builder stopped. The coordinator should read that log, or run the
  gate on the branch head, before merging. A failure of the four `qa/cmd/qa-js` tests alone is the known
  environmental one (issue #135); a cyclop failure on `engine/send.go` is Findings 5.

## Readings where the spec was silent

1. **"Under the team's backoff (SY8)"** is read as SY8's schedule, a streak of the builder's own:
   the team's `backoff` state is the team goroutine's, and sharing it would race. A success
   resets it. A non-transient failure other than 403/404 (a 400 on a build's read, say) is also
   retried under it, off the delivery path, never given up.
2. **"Judged as the sync judges it"** is taken whole: the flood window and the delivery budget
   are the team's, so a build or catch-up taking more than `recv_per_sender_minute` human posts in
   one minute drops the rest `rate` (still attached, n3f-D18), as one large answer would. The
   counts (`dropped`, `ignored`) of a page are the sync's counts too, for events past the floor
   and `after` (events not owed are folded and not taken, so not counted).
3. **The judging hold** serialises a page's judge-write-commit with an answer's, not the reads:
   a page's relation fetches (`GetEvent`, through the questions resolver) run under it, so a page
   whose posts relate to events not in memory holds main's next answer for those fetches.
4. **fakehs's backward paging** ends a page with no `end` only when the page holds no event (as
   Tuwunel does). So "three pages each way" over 250 events is three pages of events and the read
   that finds the start backwards (4 reads), and two pages of events and the head read forwards
   (3 reads): 7 = 2 x ceil(250/100) + 1. A server that omits `end` on the page holding
   `m.room.create` is handled the same way (that page is folded).
5. **`last`** of a page write is the page's newest event of any kind (state events included), so
   a first page of state events alone still has a valid `last`; qjudge's own `last` is still the
   newest non-state event, which QS7's search accepts either way.
6. **A session's end stops the job**; the next session's QS5 begins it again from the store's key
   (resumed, as a record under way or a catch-up), so no job outlives the client it reads with.

## Findings for the owner or coordinator

1. **The ready line is not in the engine**: the store has `outbox.questions_handover` and the
   demotion drops it, and this branch sets it; nothing says the line, takes it into a hook or
   clears it (no hook text, no "a hook took it" write). That is n3f-e's text and a hook path
   still unbuilt.
2. **`Health.Questions` lags `IndexAtHead`**: the builder cannot call `qhealth` (the team
   goroutine's `health` would race), so `status`, which reads `Health.Questions`, says
   `building` until the team handles its next answer (up to one long-poll, 30 s).
   `Team.QuestionsStatus()` is live. c1d, who builds `status`'s questions record, should read the
   live status or have the team refresh health at the filter change.
3. **fakehs reads a room of ten thousand events slowly**: one `/messages` of a 10 500-event room
   (any limit) took past the client's 15 s try limit under `-race`, so the 10 500-event test
   does not read the room back after the build. The builder's own pages were not slow enough to
   time out; the cost is somewhere in fakehs's per-request work over the whole room (not
   investigated).
4. **A flake in c1c's `TestQS6APostDroppedRateIsStillAttached`**, once, with four packages'
   tests running at once: the team's next coordinator-filter request was held by the test's gate
   before the post it waits for was answered. It looks like the class c1c's Findings 3 and 4
   name (a gate armed before the request in flight has answered); not reproduced since.
5. **`engine/send.go` failed cyclop** at the base already (17 > 15), and the first `qa-changed`
   run of 0aefa07a9 failed on it alone (go: golangci and teamchat-failpoints-lint; every other
   detector passed, runners not started). Fixed in a commit of its own: `send`'s first steps moved
   into `composeSend` (behaviour unchanged; the engine's tests green under `-race -shuffle=on`).

## Not done, and what the next builder needs

- **The kill mid-build test** (QX1, QX5 (i) for a first build): design ready. A room with alice's
  human posts on both sides of the kill point (join coord early, 30 alice posts, ~470 of a
  worker's posts, which are dropped `sender`, 30 more alice posts), `gateQuestions`, pass every
  read until the third forward one is held, `r.end()`; the store's key must hold a build record
  with `start` and a token; a new team on the same engine store must read `dir=f` from that
  token first and no `dir=b`; each alice post must be in exactly one batch's items over both
  runs; the handover stays true.
- **QS7's 2100-event gap and the kill mid-catch-up** with this builder: start a coordinator with
  `withIndexBuilder` over a small room and wait for the build's end (`watchBuilds(tm).ended`),
  then gate the coordinator filter as `questions_gap_test.go` does, `seed` 2100 posts with one
  admin question among them and a main item, release: main's item in that answer, the catch-up
  then folds the question (`atTheHead` and `Next()`); for the kill, gate the catch-up's reads and
  end the team after two.
- **QX1's `ask` during a held build** (20 `not-ready`) is n3f-d's verb; here only the status
  (`building` while held, `ready` after) is tested, in the 10 500-event test.
- **A test that makes a page and an answer judge at once** (to prove the hold): e.g. a test seam
  in `buildPage` before the hold, holding the page until an answer is being judged.
- **QX7's redactions** (a redaction met after `start` restarting the build, `late_from`'s
  reminder) are c2c's: `buildRun.rec` carries `start`, `after` and `late_from` for them, and
  nothing here detects a redaction.

## Shared files changed

`teamsync/session.go` (`handle` under the hold, `commit` split, the builder stopped at a session's
end), `teamsync/questions.go` (`IndexJob.s`, `qtake`, `sideCheck`), `teamsync/teamsync.go`
(`judging`, `onBuildEnd`, `onBuildWait`), `teamsync/world_test.go` (the fake engine honours
`Page` and `Handover`), `team/team.go`, `engine/storewrite.go`, `engine/questionswrite.go`,
`wiring/wiring.go`, `fakehs/fakehs.go`. Nothing in `verbs.go`, the floor markers or
`engine/status.go`.
