# n3f-b3 (Plan 00209 Task 3.2): what the n3f-b builders left undone in the reports room

Branch `feat/bus-v2-n3f-b3`, from `feat/bus-v2-n3f-b-int` (a5a738aa9). Builder: Opus 5.5.
All four items of the brief are built. The gate results are under "Gates".

## Built

1. **The recovery of a tried report** (n3f-D16; `engine/sender.go`, commit 844454ce5).
   `recoverTried` splits the tried sends by `Room`, with a `switch` whose default fails the
   recovery for a room no recovery reads. Main's sends are read back to the anchor, as before. The
   reports room's sends are read by `recoverReports`: `newerThan(ctx, n, roster.Rooms.Reports, "")`,
   so at most 5 pages of 100 events, then `matchTried` (the txn, or sender and content when the
   server gives none). A report it finds is recorded by `recordFound`, which calls `recordPosted`
   (that sets no `last_sent` for a report) and `recordOwn` (that records no own send for one).
   - A `matrix.ErrForbidden` on that read finds nothing. The member is not in the room, so it
     cannot have posted there since, and the sender's verification then decides the report.
   - Any other read error is returned as main's is (`read the reports room for the tried reports: ...`).
   - A tried report with no roster (none verified and none cached) fails the recovery, since the
     room is unknown.
   - A roster that names no reports room leaves nothing to read. The sender already holds such
     a report.
   - **Residual window** (in the code comment too): a report with more than 500 events newer than
     it in the reports room is not found, and under a new token it is put again.
2. **Wiring end-to-end rows** (`wiring/e2e_reports_test.go`, the real binary on fakehs; commit
   782933271). The world now keeps `reports`, the id `rooms.Converge` makes, and `populate` fails
   if either room is missing.
   - `TestE2EAReportWithAWorkerStopped` is OF1-n3f's row. The peer runs, the member reports,
     then the peer's keeper is stopped (SIGTERM, waited) and the member reports again. Both
     records are equal apart from `id`, and each report is in the reports room once.
   - `TestE2EReportStdinRule` holds n3f-D1's three cases at the cap 1500. The text is a
     19-byte headline and filler. 1500 bytes, a newline and `x` is 10 `body`; 1501 bytes with no
     newline is 10 `body`; 1500 bytes and a newline is posted, with a body of 1500 bytes. Only
     that one is in the room.
   - `TestE2EAReportGivesNoMemberAnItem` holds RP5 with two members. K reports, then says to C.
     C's recv gives the say alone, so its sync passed the report. C's inbox is empty, and the
     room holds one report.
   - `TestE2EARefusedReportsJoinIsRefusedInStatus` holds RP1. A fakehs fault answers the POST
     to the reports room 403 `M_FORBIDDEN`. `status` shows the reports `room` record `refused`
     with check `M_FORBIDDEN`, and main still delivers.
   - Bounds: `refusedBound = settle + reportsJoinDeadline` (the work to reach the first
     long-poll, then RP1's 30 s join deadline). Every other wait uses the world's `eventually`
     (`settle`). `downBound` and `deliveredBound`, which the brief cites, are not in this tree
     (finding 1).
3. **Defence Before Fix** (commit 6dcbade0d): the rule `project_room_default`, the fixes, and
   the sweep (section below).
4. **Stale comments** (commit 6c6b42750): `records.Queued` (Send is text, ping or report, with
   Room named), `records.Room` (Check is also a refused join's errcode), the client package doc
   (report frames a request, with its stdin rule), and the `wire` send-args comment and
   `Request.Stdin`.

## What each test was shown to catch (red and green)

Each defect was planted, run red, reverted and run green. The raw outputs are under `untracked/scratch/` (not committed).

| Test                                                                    | Planted                                                                               | Red                                                                                                               |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `TestSD3_10TheRecoveryFindsATriedReport`                                | none: written first, against the code before the fix                                  | "posted [tcb.. tcc..], want only the report the server lacks"; "the reports room holds 3 own events, want 2"      |
| `TestTheRecoveryTakesAForbiddenReportsReadAsNotFound`                   | before the fix (main read under the 403); then the `ErrForbidden` branch removed      | "read main for the tried sends: ... 403", then "read the reports room for the tried reports: ... 403 M_FORBIDDEN" |
| `TestSD3_10TheRecoveryFindsATriedReport`                                | `recordFound` calling `RecordOwnSend` directly, not `recordOwn`                       | "a recovered report touched main's bookkeeping: ... own sends map[$...]"                                          |
| `TestTheRecoveryRefusesATriedSendItCannotLookFor`                       | the room switch's default sending any room to main                                    | "a tried send to questions (roster true): the recovery passed"                                                    |
| `TestE2EReportStdinRule`                                                | the client reads 1501 bytes, not 1502 (cap + 1, the cut n3f-D1 forbids)               | "C bytes, a newline and x: exit 0 ... room reports"                                                               |
| `TestE2EAReportWithAWorkerStopped`, `TestE2EAReportGivesNoMemberAnItem` | `postRoom` verifies the reports room but posts the report to main                     | "the reports room holds [] from me"; "holds 0 reports from K, want 1"                                             |
| `TestE2EARefusedReportsJoinIsRefusedInStatus`                           | `reportsRoomRecord` turning `refused` into `joining`                                  | "not within 1m0s: the reports room record ..."                                                                    |
| `TestAQueuedSendToAnUnpostedRoomIsNotPostedToMain`                      | the old default restored in `recheck` and `postRoom` (any room but reports is main's) | "posted [tc..11 tc..12], want only the main send"                                                                 |
| `project_room_default` (fixture `member/engine`, real tree)             | the fixture's `bad.go`, and the real tree before the fixes                            | 5 `want` lines matched; on the real tree, `report.go:119`, `sender.go:245`, `sender.go:321`                       |

Two claims rest on no planted defect:

- `TestTheRecoveryReturnsAFailedReportsRead` passed before the fix as well, because main's read failed in its place. It holds the reports read's own error path only since the fix.
- RP5's "no item" half: posting the report to main did not give C an item (C's judge does not make an item of a report in main), so that row's red came from the room count, not from the item check. No defect that makes a report an item was found to plant through the binary. The item check is exercised, not proven.

The stopped-worker half of the OF1-n3f row has the same limit. No code looks at another member's presence, so no natural defect could be planted, and the row holds only the output equality.

## Gates

- In this worktree, over the packages touched: `go vet ./internal/teamchat/member/...` clean, `gofmt -l` clean for the files
  changed (the pre-existing unformatted fixtures under `qa/rules/testdata` are not mine), `golangci-lint` 0 issues over
  `engine`, `client`, `wiring`, `records`, `wire` and `qa/rules`.
- `go test -race -count=1 -cover`: `engine` 92.7%, `teamsync` 95.1%, `state` 99.8%, `team` 100%, `cli` 98.9%,
  `link` 92.3%, `wiring` 100%, `records` 100%, `wire` 94.2%, `client` 93.5%. `qa/rules` 93.6% (`go test`), and `qa/driver` ok.
- The project rules over `./internal/teamchat/...` through the built driver: clean after the fixes, and the three findings before them.
- `./scripts/qa-changed.bash --base feat/bus-v2-n3f-b-int` was run twice. Both runs **FAILED in `go` only**. Run 2 planned 22 files (after
  6dcbade0d). Run 1 planned 5 files (after 844454ce5), but it ran later and its scope widened, so it judged the same tree.
  - The single failure is go-qa's `test` runner over the nested `qa` module: `qa/cmd/qa-js`'s four `TestReal*` tests, each "the
    checkout's extensions/node_modules (cd extensions && npm ci) is needed for this test". That is issue 135, environmental.
  - There is no "tests failed in ." line, so the root module's tests passed.
  - Every other stage passed in both runs. In go-qa: fmt, record, suppression, summary, docs, golangci, analyzers (with
    `project_room_default`), deadcode, teamchat-failpoints-lint and -vet, project-rules-tagged, vuln, mod and build. Its scope was
    "whole project (widened: qa/rules/roomdefault.go changed)".
  - Also passed: `docs`, `teamchat-keeps-off-v1`, and the suite `test-qa-changed.bash` (179 passed).

## Defence Before Fix

- **Class (a): a send's room tested against one side room, so every other room takes main's path.**
  Rule `project_room_default` (`src-go/qa/rules/roomdefault.go`, in `rules.All()`, so blocking
  through go-qa's analyzers stage; catalogue entry and table row in `CLAUDE/GoProjectRules.md`).
  - Scope: `member/engine` and `member/teamsync`, not their tests.
  - Flags: a `!=` comparison with a non-main `proto.Purpose*` constant; an `==` comparison with
    one that guards an `if` with an else or a terminating body; and an `==` in a tagless-switch
    case that another case follows.
  - Sweep: three findings, all in the engine. `postRoom`, `recheck` (now split into
    `recheckMain`) and `sendFailed`'s 403 are each a `switch` on the room with a default that
    refuses through `unknownRoom` (an internal error, "the link does not serve a send to the X
    room yet"). `teamsync` had none: `verbs.go`'s `roomOf` names main first and its side room as the last case, and its event lookup refuses any room `!= proto.PurposeMain`.
- **Class (b): a test fake that keeps fewer rooms than the code under test uses.** No rule (under
  "Defect classes that cannot be a rule"): which of a fake's arguments matter depends on what
  the tests ask, not on the syntax tree.
  - Sweep: the engine's `mainRecorder` became `roomRecorder` (every purpose); the client's
    `hsRecorder` kept main only and now keeps every purpose.
  - Listed as stubs that back no assertion on a room: `engine` `fakeNotices.RoomState`, and
    `link`'s `RoomState`, `Messages`, `Send`.
  - `teamsync`, `ends` and `wiring` already kept every purpose. Five copies of this recorder now
    exist, a duplication for a shared test package to remove later (not done).

## Not done

- The skill's text for `report` and the `room` states (n3f-e's, as briefed).
- b2's item 5: a report posted at once costs two reports-room state reads (SPEC n3f-D16 asks for both; not changed).
- The recovery's 500-event window (stated above) is not tested at its edge.
- Nothing else: the journal entry is `JOURNAL/2026-11-05/235554Z-action-ac3e44.md`, written by `mkplan.bash --journal`.

## Findings

1. `downBound` and `deliveredBound`, the derived bounds the brief points to in `e2e_test.go`, are
   not in this tree (`linkEndBound`, `answerBound`, `backoffCap` and `settle` are). The new bound
   follows their form.
2. With `recoverTried`'s room switch, a tried send to the questions room fails the recovery until
   its builder adds a case. That is deliberate (fail fast), but it is a seam the c1b/c2 builders
   must extend, as are `recheck`, `postRoom` and `sendFailed`.
3. The recovery reads the reports room only when a tried report exists, and reads main only when a
   tried main send exists. Before, main was read whenever anything was tried.
4. The message of commit 782933271 has a cut sentence ("(the report report."). The next commit's
   message says what it meant. Amending is not allowed here.
5. The two `qa-changed` runs queued on the shared lock: the first planned 5 files at its start
   (commit 844454ce5) but reads the files as they are when it runs.

## Shared files changed

- `engine/sender.go`: `recoverTried` split, plus the new `recoverReports`, `recordFound`,
  `unknownRoom` and `recheckMain`. `recheck` and `sendFailed`'s 403 are now room switches, and
  the `RecoverSends` doc changed.
- `engine/report.go`: `postRoom`'s room switch.
- `engine/sender_test.go`: `roomRecorder`, `hsTeam.reports` and `roster`, `ownIn`, and the new
  tests.
- `engine/send_test.go`: `sendingUnder`.
- `client/hs_test.go`: `hsRecorder`.
- `client/client.go`, `records/records.go`, `wire/request.go`: comments only.
- `wiring/e2e_world_test.go`: `world.reports`.
- `wiring/e2e_reports_test.go`: new.
- `qa/rules/table.go`: `All()` gains `RoomDefault`.
- `qa/rules/rules_test.go`: the count is now 24.
- `CLAUDE/GoProjectRules.md`, `src-go/CLAUDE.md`, `PLAN.md`, `BUILT-n3.md`.
