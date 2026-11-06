# n3f-b (Plan 00209 Task 3.2): the reports room, the team goroutine's half built, the rest handed on

Branch `feat/bus-v2-n3f-b`, from `origin/feat/bus-v2-n3f-a-int` (133a5fe4f). Builder: Sonnet 5.5.
This slice is **not complete**. The builder's context passed its budget after reading the spec
and the engine, and after building the team goroutine's half. Read "Not done".

## Built

`team.Team` (`member/team/team.go`) gains two methods and the types they use:

- `VerifyReports(ctx) error`: RP4. The join is tried first when this member is not in the room
  (RP1); then `GET /rooms/{reports}/state`, judged by `proto.VerifySide` against main's verified
  roster. Nil is a room that verifies. An `*exits.Error` is a refusal: 22 `forbidden` (the join
  refused and the joined rooms do not hold the room, or a 403 on the state read) or 22
  `untrusted` with the failed check named, both naming the reports room. Any other error is
  transient, including "main not verified yet", so the engine can queue the report (n3d-D2).
  A refusal never stops the team (main goes on).
- `ReportsRoom() team.ReportsRoom{State, Check}`: from memory, for `status`: `joining` (join not
  answered, also before main is verified or after a roster names another room), `joined`,
  `refused` (Check is the errcode), `untrusted` (Check is the failed check).

`member/teamsync/reports.go` (new) and small edits to `teamsync.go` and `session.go`:

- **RP1/n3f-D12**: `Team.Run` starts `reportsLoop` beside `forward`. `session()` kicks it
  (forced) just before the first long-poll, and a trust-triggered verify of a later answer kicks it
  (not forced). A try is due when main is verified and the room is not joined, and either it is
  not refused, or refused under another roster revision, or a start forced it. A transient failure
  retries on SY8's backoff (first 1 s, doubling to 30 s, the team's jitter, `Clock.Hold`, each wait
  told to `onWait`); a refusal waits for the next kick. One try at a time (`reportsTry`), shared
  with `VerifyReports`, so a report's join and the loop's never run together. The 30 s bound is
  the injected clock's `AfterFunc`. Membership after a refusal is `GET /joined_rooms`.
- **RP2**: `sess.faults` (called from `handle`): a reports event in an answer is judged with
  `PurposeReports`; `Fault` logs `fault <id>: ...` once per event id (`faulted`, bounded), `Drop`
  (a stranger, `sender`) logs `dropped <id>: sender` once; never an item and never persisted.
- The client the loop and `VerifyReports` use is the session's own (`t.client`, `t.pair`), not
  `current()`: a second `current()` from another goroutine changed `t.pair` and broke n3c-D12's
  401 rule (the existing test `TestN3dD4ANewPairRecoversAgain` hung until I saw it).

Test fakes: `engine/engine_test.go`'s `fakeTeam` (scripted `reportsAnswers`, a `reports`
state, a `reportsVerifies` count) and `link/link_test.go`'s `fakeTeam` (stubs).
`trust_test.go`'s `TestTheCoordinatorJoinsTheQuestionsRoom` now counts joins except the reports
room's (the eager join lands at a time of its own).

## What each test was shown to catch

Tests are in `member/teamsync/reports_test.go`, written before the code. With the two methods
stubbed (nil, zero state) the non-blocking ones were red for the stated reason (the room shows
`{State: Check:}` not joining; `VerifyReports = <nil>, want 22 untrusted`; nil before main is
verified); the ones that wait on the team hang until `go test -timeout` with the stub, which is
how this package's `poll` and `batch` fail. Then the implementation, then green.
Planted defects, each run red and reverted:

- `faulted.has(id)` dropped from `faults`: `TestAReportsEventInAnAnswerIsAFaultAndNoItem` red,
  "4 fault lines for the report, want 1".
- `r.rev != t.roster.Rev` dropped from `reportsJoinDue` (a refused join only retried at a start):
  `TestARefusedReportsJoinIsTriedAgainAtARosterChangeAndAReport` red, "1 joins after the roster
  change, want 2".

Not planted (so not claimed): the 30 s bound, `memberOf` (a 403 for a member already in:
`TestAForbiddenJoinOfAMemberAlreadyInIsJoined` exercises it but no defect was planted in it), the
backoff values (`TestATransientReportsJoinFailureIsRetriedWithBackoff` pins 1 s then 2 s).

## Gate results

`go test -race -count=1`: `teamsync` ok, coverage 95.1%; `team` 100%; `engine`, `link` ok;
`go vet ./internal/teamchat/...` clean. `gofmt -l` clean. **`./scripts/qa-changed.bash` was NOT
run** (the builder was out of context), so the commit's gate is unproven: the reviewer or the
next builder must run it. Not run: `-shuffle=on`, the tagged stages, the project-rules driver,
golangci-lint over the whole module (the edit hook linted each file as it was written).
The release test (`go test -tags teamchatrelease ./internal/teamchat/member/cli/`) before: fails
on four verbs (`thread`, `questions`, `report`, `ask`); after: unchanged, because `report` is not
wired.

## Not done, and what the next builder needs

Everything of RP3, RP5, RP6, RP4's engine half, the `status` record, the wiring and OF1's row.
Findings from reading the engine that fix the design:

1. **wire and client** (`member/wire/request.go`, `member/client/`): add `OpReport = "report"`,
   its 30 s deadline in `deadlines` (RP6), `ReportArgs{Team *paths.TeamID}`; `client.Main("report")`
   is registered in `wiring.clientVerbs`; `client.request` and `isSend` must treat it as a send;
   `sendArgs` needs a `report` case. The stdin rule (n3f-D1, cap 1500) is `readText`'s with the
   cap a parameter, but **the client must not refuse an over-long or empty text itself**: RP3's
   order is team (64), role (11), size (10 `body`), secret, room, `Admit`, and only the link
   knows the role. The client reads at most 1502 bytes, strips one trailing newline, refuses a
   terminal (64) and non-UTF-8 (10 `schema`, as `say`), and forwards the rest; the engine's
   `ReportForm` refuses with `body` in its turn. The generic cap in `readText` today is 4096
   (`proto.MaxText`).
2. **engine** (`member/engine/serve.go`, `send.go`): `Serve` gets a `wire.OpReport` case; `e.report`
   resolves the team by CL6 (`candidates` then `cli.ResolveTeam(flag, nil, active)`; with several
   held teams and no `--team` it is 64), takes the roster as `addressed` does, and calls `e.send`
   with `state.SendReport` (a new kind: add it to `state/store.go`'s `SendNotice` const block and
   `sendKinds`) and `c.Report(text)`. `send()` must stop hard-coding the main room: `sendingOf`
   sets `Room: proto.PurposeMain` (use `o.Purpose`), and `answerSent`, `awaitSend` and
   `answerResult` build `records.Sent{Room: proto.PurposeMain}` (thread the room through).
   Between the compose and the charge, for a report: `st.tm.VerifyReports(ctx)` off the state
   goroutine; an `*exits.Error` answers the verb at once (`doneOf`), nothing charged; any other
   error is the transient case, charged and queued at once (a flag on `sendTeam` read by `charge`'s
   `queuedNow`). In `charge`'s `changeLocked` closure for a report: seed
   `c.Reports = &w` with `proto.SeedWindow(c.Self, s.Outbox.Reports)`, `Admit` (it spends the
   window), then `s.Outbox.Reports = w.Within(c.Self, proto.ReportWindowMs, now)`. `refusal()`
   maps `proto.ReasonRate` with `Detail == proto.DetailReportPerHour` to 14 `report-rate`: a new
   message naming when the oldest slot leaves the window (`w.Oldest`, span `ReportWindowMs`) and
   that a report that was not posted still counts (n3f-D6); the bucket's refusal stays 14 `rate`.
   The order inside `Admit` is already RP3 step 6's.
3. **sender** (`member/engine/sender.go`): for an entry whose `Room` is reports, `post` must: call
   `p.tm.VerifyReports(ctx)` before the PUT (an `*exits.Error` removes the entry through
   `dropSend`, answering the waiting verb with that error, counting `unsent`, token and window slot
   spent; a transient error is `e.hold`); post to `r.Rooms.Reports` from the roster (not `p.main`);
   `recheck` must decode with `proto.ReportKeys` and call `c.Report(dc.Body)` (today it decodes
   `MainKeys`, which a report does not match); not call `n.RecordOwnSend`; not set
   `Outbox.LastSent` in `recordPosted` (it is main's recovery anchor); and in `sendFailed` a
   `matrix.ErrForbidden` for a reports entry removes only that entry with 22 `forbidden`, where
   today it stops the team's sends (SD3-8c). Finding: `recoverTried` reads main only, so a tried
   report is never found by a recovery; under the same token the re-PUT by its txn is idempotent,
   under a reissued token a posted report could be sent twice. Record it, or read the reports room
   for tried reports.
4. **status** (`member/engine/status.go`, `memberRecords`): the reports `room` record's state is
   `v.tm.ReportsRoom()` (`Check` for refused and untrusted), not "joined if the roster names it".
   `records.Room`'s doc says `check` is for untrusted only; widen it to the refused errcode (see
   findings). The questions room's record stays as it is (n3f-c).
5. **wiring and cli**: add `"report"` to `wiring.clientVerbs`; remove `"report"` from `cli`'s
   `unbuilt` (`run.go`) and from `TestUnbuiltAreTheLaterSlices` (`verbs_test.go`), and add it to
   `cli.Outside()`'s list (`run.go`'s `outside`, and its test); the release test must then fail on
   exactly `ask`, `thread`, `questions`. Run it before and after.
6. **Tests still to write**, each seen red first: RP3's order (64 ambiguous-team with two teams;
   a probe sending an oversize report is 11 not 10; size then headline 10 `body`; `secret`; the
   room not verified; `report-rate` with the oldest-slot message and `outbox.reports` unchanged by
   a refusal; `duplicate` 15 within 60 s; the bucket's 14 `rate`; the charge writing
   `outbox.reports` and the gate in one store write); RP4's three paths (a failed check charges
   nothing and main's next item is delivered; a join refused 403 is 22 `forbidden` with
   `outbox.reports` and the gate unchanged and `status` `refused`; a homeserver down during
   `report` is queued, exit 0, posted after verification when it returns; a queued report whose
   room fails verification at the sender is removed, logged and counted `unsent` 1); RP5 (the
   PUT's content byte for byte A 10.1's: `m.notice`, `m.mentions {}`, `teamchat.report`, no
   relation; a fakehs team of two where the other member's store gains no item); the D1 stdin
   cases for `report` (`1500` bytes and a newline accepted with a 1500-byte body; `1500` bytes, a
   newline and `x` refused; 1501 bytes with no newline refused); and OF1-n3f's `report` row (a
   team with one worker stopped, the other worker reports: exit 0, the same output as with all
   running) in `wiring`'s e2e world.

## Findings

- SPEC-n3f section 6's list of `room` states has no state for a join not yet answered; the
  builder added `joining` (in `team.ReportsJoining`), to be put into section 6 by the owner.
- Section 6 says `check` names the failed check of an untrusted room, and RP1 says a refused room
  records "its errcode for `status`": the only field is `check`, so `refused` carries the errcode
  there (`records.Room`'s doc comment still says untrusted only; not yet changed).
- SPEC-n3f 12.6 says all seven keys are dropped at a demotion, but `outbox.reports` is also a
  worker's (a worker has no demotion), so QS9 (n3f-c) must not drop it for a coordinator that
  keeps reporting: a coordinator may report too (`Composer.Report` allows both classes).
- RP1 says the join runs "after the first long-poll is sent". It runs concurrently with it
  (kicked just before the request): the client has no hook for "request sent", and waiting for
  the long-poll's answer would delay the join by up to 30 s on a quiet team, against n3f-D12.
- `leakcheck.Poll` and `leakcheck.Drive`, and the rules `project_test_spin` and
  `project_poll_pure`, do not exist in this base; the tests wait on channels as the package's
  other tests do (`polls`, `batches`, and the new `onReports` seam).
- Shared files changed, for the merge with n3f-c1: `member/team/team.go` (two methods on
  `Team`, the types after it), `member/teamsync/teamsync.go` (five fields, `New`, one line in
  `Run`), `member/teamsync/session.go` (two lines in `session` and `handle`), the two
  `fakeTeam`s in `engine/engine_test.go` and `link/link_test.go`, `trust_test.go` (one test).
