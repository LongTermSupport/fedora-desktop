# n3f-b2 (Plan 00209 Task 3.2): the `report` verb built end to end; the recovery and the end-to-end rows handed on

Branch `feat/bus-v2-n3f-b2`, from `feat/bus-v2-n3f-b` (fc99f7f72). Builder: Sonnet 5.5.
This slice is **not complete**. The builder's context passed its budget after the verb, the
sender's post and verification, the `status` record and the wiring were built and green. Read
"Not done".

## Built

- **wire** (`member/wire/request.go`): `OpReport`, its 30 s deadline in the table (RP6), `ReportArgs{Team}`.
- **client** (`member/client/send.go`, `client.go`): `report` is a send (`isSend`, so a cut
  link says "check status"). `readReport` is n3f-D1's rule with the cap 1500: at most 1502
  bytes are read; one trailing newline is removed unless the read reached 1502 bytes (that is
  over the cap whatever its end, so it is sent whole); a terminal is 64; non-UTF-8 is 10
  `schema` (it cannot travel in the request's JSON). **The client refuses no size and no empty
  text**: RP3's role check (11) comes before the size check (10 `body`) and only the link knows
  the role. `readText` and `readReport` share `readStdin`.
- **engine** (`member/engine/report.go` new; `send.go`, `serve.go`, `sender.go`, `status.go`):
  - `Serve` routes `OpReport` to `e.report`. The team is CL6's (`reportingTeam`: the flag's,
    else the only team held, 64 `ambiguous-team` for several; an unverified team with no cached
    roster is 20 `unreachable`).
  - `e.send` runs the composer's order (`Composer.Report`: role 11, size and headline 10
    `body`, secret 10 `secret`), then `reportsRoomOK` (RP4 at the verb, off the state
    goroutine, inside the request's context): an `*exits.Error` (22 `forbidden` / `untrusted`)
    answers at once and charges nothing; the request's own deadline ended is 4 `timed-out`; any
    other failure is the transient case: the report is charged and queued at once (`charge`'s
    new `queueAtOnce`, n3d-D2). Then `charge`.
  - `spend` (extracted from `charge`) is `Admit` over the store's budgets: the gate and, for a
    report, `proto.SeedWindow(self, outbox.reports)`; both are written back into the store in
    the charge's one write, only when the send is admitted (`Window.Within` pruned to 3600 s).
    A refusal writes nothing.
  - 14 `report-rate` (`admitRefusal`): "team T allows N reports an hour and they are made; the
    oldest leaves the window in S seconds; a report that was not posted still counts". The
    bucket's refusal stays 14 `rate`, a repeat within 60 s 15 `duplicate`. The three `body`
    refusals name which rule broke (1500 bytes, an empty headline, a headline over 120).
  - `sendingOf` takes the entry's room from `Outgoing.Purpose`; `answerSent`, `awaitSend` and
    `answerResult` take the room, so the `sent` record says `reports`.
  - **Sender** (`postRoom`, `reportsTarget`, `recheckReport`, `droppedForbidden`, `recordOwn`):
    a queued report is re-checked with `proto.ReportKeys` and `Composer.Report` (n3d-D27);
    its room is verified again before the PUT, bounded by 30 s on the engine's clock
    (`AfterFunc`); the PUT goes to the roster's reports room; a refusal (22) removes it through
    `dropSend` (logged by its transaction id, counted `unsent`, token and window slot spent,
    its waiting verb answered); a failure that is not a refusal holds the sender; a PUT answered
    403 removes only that report (22 `forbidden`) and, unlike main's 403, stops nothing. A report
    records no own send and does not set `Outbox.LastSent` (main's recovery anchor).
  - `status`: the reports `room` record is `v.tm.ReportsRoom()` (`joining`, `joined`, `refused`
    with the errcode, `untrusted` with the check); a roster naming no reports room is `absent`.
    The questions room's record is as it was.
- **state**: `SendReport = "report"` in the closed set of send kinds.
- **wiring and cli**: `report` is in `wiring.clientVerbs`, in `cli.Outside()`, and out of
  `unbuilt`.

## Tests, and what each was shown to catch

Written first and run red before the code, for the right reason (wire: the deadline was 0; client:
the stdin was nil and the verb exit 69; state: "a send in flight is not well formed"; engine: "the
link does not serve report yet", and one test hung until `-timeout` because the verb never
reached the fake, as `TestRP6...` does without code). Files: `wire/request_test.go`,
`client/send_test.go`, `state/store_test.go`, `engine/report_test.go` (new), `cli/verbs_test.go`.

Planted defects, each run red and reverted:

- client: strip the trailing newline even when 1502 bytes were read: `TestN3fD1ReportStdin` red,
  "1501 and a newline: stdin of 1501 bytes, want 1502".
- engine: `queueAtOnce` never set: `TestRP4ATransientCheckQueuesTheReport` red, "the report was
  not queued ... Queued:false".
- engine: `LastSent` set for a report: `TestRP3AReportIsPostedToTheReportsRoom` red, "a report
  touched main's bookkeeping: last_sent ...".

Not planted (so not claimed to catch a specific defect): the window arithmetic and the
`report-rate` message, the order of the room check before `Admit`, the 30 s bound of the sender's
verification (the test would hang if the bound were absent), the 403 per-report removal, the
recheck, the `status` record. They are exercised by tests that pass; no defect was planted in
them.

Test list (engine): RP3 posted to the reports room with A 10.1's content byte for byte (RP5) and
no `last_sent`/own send; the team rule (64, and `--team`); no flag with one team; role before size
(a probe with an oversize, empty or secret text is 11); the size and the headline (three
messages; 1500 bytes post, 1501 do not); the secret; the window (seventh report 14
`report-rate`, "600 seconds" and "not posted still counts", store unchanged, 1 ms before and at
the oldest leaving); the gate (15 `duplicate`, 14 `rate`, window not spent); the charge is one
write (a closed writer leaves the store and memory as they were); RP4 a failed check
(`untrusted`, `forbidden`) charges nothing and main's `say` and an item go on; the room is
checked before the window; a transient check queues, the sender verifies again (2
verifications) and posts; a down server queues, lists as a queued report, posts on return; a
queued report that fails at the sender is removed, logged by its txn, `unsent` 1, slot spent; a
waiting verb is answered the sender's 22; a 403 at the PUT removes that report and stops nothing;
RP6 (the verb's context ends: 4 `timed-out`, nothing charged); the sender's verification is
bounded by 30 s; a queued report is re-checked against the roster (a probe: removed, `unsent`);
a request with no stdin (64) or other args (10 `schema`); the `status` room record for the four
states.

## Gate results

`go vet ./internal/teamchat/member/...` clean; `gofmt -l` clean; `golangci-lint run` over `engine`,
`client`, `cli`, `state`, `wire` clean as the edit hook ran it. `go test -count=1`: `cli`,
`wiring`, `wire`, `client`, `state`, `team`, `teamsync`, `link`, `engine` all ok. With `-race`:
`engine` 92.8%, `client` 93.5%, `wire` 94.2%, `state` 99.8% coverage.
**Release test** (`go test -tags teamchatrelease ./internal/teamchat/member/cli/`): before, it
failed on four verbs (`questions`, `report`, `ask`, `thread`); after, on exactly three: `ask`,
`thread`, `questions`.
`./scripts/qa-changed.bash --base 133a5fe4f`, which covers the first builder's commit as well as
this work (`fc99f7f72`, the first builder's, was never run through it before): the first run
(28 files) FAILED in `go`: `cmd/teamchat`'s `TestTheN3fVerbsAreStillUnbuilt` still listed `report`
as unbuilt (`exit 69 no-session` through the binary); I removed `report` from that list and added
`TestReportIsBuiltInTheBinary`. The second run (30 files, after that fix) PASSED: docs, go
(go-qa over both modules in diff mode: format, golangci, analyzers, deadcode, the tagged
project-rules stages, vuln, mod, build, test), teamchat-keeps-off-v1; no "tests failed in ." line.
The report file and this paragraph were written after that run; the run saw this report as it was
then, without its gate text. `ansible-syntax` and `qa-js` were not part of the plan for these files.
Not run: `-shuffle=on` by hand, the tagged stages by hand, `qa-all.bash` (forbidden here).

## Not done, and what the next builder needs

1. **The recovery of a tried report** (the finding of the first builder, n3f-D16). `recoverTried`
   reads main only (`newerThan(ctx, n, rc.main, rc.anchor)`), so a tried report is never found. Under
   the same token the PUT by its txn is idempotent; under a reissued token (a new device) a posted
   report is put again and appears twice. **Neither fixed nor pinned by a failing test.** Design:
   split `rc.tried` by `Room`; for the reports entries read the reports room back with
   `newerThan(ctx, n, roster.Rooms.Reports, "")` (at most five pages of 100 events, the same bound as
   main with no anchor) and `matchTried`, which already matches sender and content when the server
   gives no txn; record a found report with `recordPosted` (now safe: it sets no `last_sent` for it)
   and skip `RecordOwnSend` (use `recordOwn`). A `matrix.ErrForbidden` on that read is "not found"
   (the sender's verification then refuses it); any other error is returned as main's is. The
   residual window to state: more than 500 events newer than the tried report in the reports room.
   Test with fakehs (`onHS` makes the rooms; `mainRecorder` must also record `reports`; the roster
   must name that id; the member joins it; another device posts the content first, as
   `TestSD3_10TheRecoveryFindsTriedSends` does for main).
2. **Wiring end-to-end rows** (`member/wiring`, the real binary on fakehs): OF1-n3f's `report` row
   (a team with one worker stopped, the other reports: exit 0 and the same output as with all
   running); n3f-D1's three stdin cases through the binary (the accepted text needs a short first
   line: 1500 bytes of one line is a headline over 120); RP5's two-member check (the other
   member's store gains no item); a refused eager join shows `refused` in `status`. The world's
   `populate` already makes the reports room by `rooms.Converge`; members join it by the restricted rule.
3. **Defence Before Fix** was not done: no defect class was assessed for a project rule. Candidates
   to judge: a send that hard-codes the main room (the class this slice removed from `sendingOf`,
   `answerSent` and `recordPosted`; questions will meet it), and a test fake that records no room.
4. Doc comments left stale: `records.Queued`'s "Send is text or ping" (now also report) and
   `records.Room`'s "check is for untrusted only" (now also a refused join's errcode); the
   client package doc and `wire.Request.Stdin`'s comment name only say, reply and ping. The skill
   (PK2) and its text test do not yet name `report`'s record or `room` states `joining` and `refused`.
5. The sender verifies the room again after the verb verified it, so a report that posts at once
   costs two `GET /rooms/{reports}/state` (SPEC n3f-D16 asks for it; stated, not changed).

## Findings

- `leakcheck.Poll`, `Drive` and `Idle` and the rules `project_test_spin` and `project_poll_pure`
  are not in this tree (PR #157); the new tests wait on channels the fakes close
  (`reportsHold`) and on `awaitDue`, and use the package's existing `until` and `waitFor`.
- `report-rate`'s message names when the oldest report leaves as seconds from now (rounded up),
  not a wall time, so it reads the same with any clock; SPEC says "naming when".
- RP3 step 3 says the message names which of the empty and over-long headline broke: the proto
  refusal has one detail for both, so the engine chooses among three messages from the text
  (`reportBodyError`).
- `Composer.Report` already runs the role check before the size check; the client was the
  place the order could be broken, which is why it forwards sizes.
- `sendFailed` for a reports 403 does not set the room's `refused` state: that is the team's,
  and no seam tells it; the next `VerifyReports` finds out.
- The sender's `reportsTarget` ends silently with no hold when the engine's context ended,
  as the other post paths do.

## Shared files changed (for the merge with n3f-c1)

`engine/send.go` (`send`'s verification step and `queueAtOnce`; `charge`'s signature and the new
`spend`; `sendingOf`'s room; `answerSent`, `awaitSend`, `answerResult` take the room),
`engine/sender.go` (`post` calls `postRoom` and `recordOwn`; `recheck` branches to
`recheckReport`; `sendFailed`'s 403 case; `recordPosted`'s `LastSent` guard), `engine/serve.go`
(one op case), `engine/status.go` (`memberRecords` split; `reportsRoomRecord`),
`engine/engine_test.go` (the fakes: `fakeNotices.rooms`, `fakeTeam.reportsHold`),
`state/store.go` (one const, one slice entry), `wire/request.go`, `client/client.go`,
`client/send.go`, `cli/run.go`, `cli/verbs.go` (`verbReport`), `wiring/wiring.go`,
`cmd/teamchat/main_test.go` (`report` out of the unbuilt list), and the tests of each. The questions room will meet the same room-threading in `send.go` and
`sender.go`: take `entry.Room` as these do, and do not set `last_sent` for it.
