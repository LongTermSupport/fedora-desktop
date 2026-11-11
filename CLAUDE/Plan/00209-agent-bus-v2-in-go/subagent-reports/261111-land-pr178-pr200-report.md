# Landing PR 178 and PR 200 onto F44

Worktree branch started at F44 `15f1f4610`. Both branches merged with `git merge --no-ff`, so
each PR head (`2d2c0fead` for 178, `faff39848` for 200) is an ancestor of the result. Nothing
pushed.

## Commits

| Commit      | What                                                                                  |
| ----------- | ------------------------------------------------------------------------------------- |
| `19eb68353` | merge of `origin/feat/bus-v2-n3f-c2a-int` (PR 178), CCY 3.118.0 / container 2.84.0    |
| `c821a239e` | qa/rules: one constant `builtinPanic` (goconst after the merge)                       |
| `389d4c5c4` | merge of `origin/fix/coordinator-notice-192` (PR 200), CCY 3.118.1 / container 2.84.1 |

## PR 178 (n3f side rooms chain)

Conflicts, all resolved as the union:

- `qa/rules/table.go`, `rules_test.go`: F44's 37 rules plus `RoomDefault`, `SharedDue` (39).
- `CLAUDE/GoProjectRules.md`: F44's table plus the `project_room_default` and
  `project_shared_due` rows; F44's sections plus the branch's `project_shared_due` section.
  The table was re-aligned to the wider rows (formatting only).
- `member/cli/run.go`, `verbs_test.go`: F44's `tls` and the branch's `report` verb both outside.
- `PLAN.md` Task 3.2: the branch's n3f status text. Its opening now says n3f-a to n3f-c2a are
  merged to F44 with PR 178, not "awaiting review on `feat/bus-v2-n3f-a-int`".

These broke only when the two sides were put together. Each was a merge slip, not a new
class of defect:

- `engine/sender_test.go` set `matrix.Config.PlainHTTPHosts`, which F44 removed in Plan 00244
  S6. A loopback http URL needs no listing now, so the field was dropped.
- `engine/report_test.go` RP3 expected plain report content. F44's Plan 00241 (`c4e7f5f1a`)
  now draws reports as Matrix HTML, so the expected content carries `format` and
  `formatted_body`.
- goconst: `"panic"` now appeared three times (realtimebound, F44's walkopen and the branch's
  roomdefault), so it is now the constant `builtinPanic`. The linter already catches this,
  so no new rule was needed.
- The CCY container bump is required because F44's image now builds teamchat in its builder
  stage (issues 235/237).

PR 178's review comments:

- Coordinator, 11-06: the index is built on OW5 (a) and must be reworked to (b) first.
  **Addressed on the branch.** lts-bob's 11-06 comment says OW5 (b), the second post list,
  was merged down into head `2d2c0fead`. That is the head merged here.
- lts-bob, CI timeouts on `9e4b33584`. **Addressed**: the gateFilter fix and the QS5 drain-order
  fix were merged down. The comment of 11-06 12:00 reports both qa-all runs green on `2d2c0fead`.
- The PR body's own "Findings for the coordinator" are **still open and not addressed on the
  branch**. The PR presents them as open. They were not part of this task:
  1. The ready line is not said by the engine.
  2. `Health.Questions` lags `IndexAtHead`.
  3. fakehs is slow on a 10 500-event room.
  4. A one-off flake in `TestQS6APostDroppedRateIsStillAttached`.
- The body's "not done" list is still open: the kill-mid-build test, QS7's 2100-event gap and
  kill mid-catch-up, the concurrency test for the judging hold, `ask` during a build, and
  QX7 redactions. The PR body describes only c2a and is out of date for the collapsed chain.

## PR 200 (issue 192)

Conflicts:

- `qa/rules` table and count: 39 plus `CoordinatorAbsence` (40).
- `GoProjectRules.md`: the `project_coordinator_absence` row added. F44's sections after
  `project_zombie_exit` were kept whole; the branch side was empty there.
- `member/team/team.go`: `Health` keeps n3f's `Questions` and gains the branch's `Verified`.
- `qa/rules/testdata/.../teamchat/proto/proto.go` (both sides added the file): it is now two
  files of one fixture package. `proto.go` holds the room purposes for `project_room_default`.
  `claim.go` holds the branch's fixture unchanged, and its package doc was folded into
  `proto.go`'s.

One merge slip: n3f's `engine/report.go` read `sendCandidate.roster()` as two values, but
issue 192 made it return three. A report makes no coordinator claim, so it still composes
against the verified roster or the cache (`r, ok, _ :=`). `SetHealth` writers were checked:
n3f writes health only through `setHealth(func)`, so it never clears `Verified`.

PR 200's review comments (coordinator FIX-FIRST, 11-09). All were addressed on the branch, and
the code is present after the merge:

1. Blocker, down team framed as not known: **addressed** (`framedCurrent` in `link`,
   `TestADownTeamIsFramedAsNotKnown`, `TestDownFramedStaysNotKnown`).
2. False "you joined": **addressed** (`told.not_known` in `state/session.go`,
   `TestNotKnownThenKnown`).
3. DBF rule: **addressed** (`project_coordinator_absence`, catalogue entry). Its three named
   allowances (the frame template, the send/reply refusal, the questions room's closed topic)
   are for the coordinator to accept or refuse; the PR says so.
4. Nits: **addressed**. The coordinator-only notice opens "the coordinator of team X changed",
   and `v` staying 1 is recorded in the journal decision `2026-11-09/113832Z-decision-509d46.md`.

lts-bob's 11-09 F44 merge comment says flake #212 is filed; it is not addressed here.

## Verification

After each merge:

- `gofmt`, `go build ./...`, `go vet ./...` untagged, and with `busfailpoints` and `tuwunel`
  over teamchat.
- The project rules driver (rebuilt after each merge) over `./...` and the two tagged teamchat
  builds.
- `go test ./qa/rules/`.
- `go test -race -count=1 ./internal/teamchat/... ./cmd/teamchat/...`. All passed after the
  fixes above. The first run after merge 1 failed RP3 only, and that was fixed.
- `./scripts/qa-changed.bash --base 15f1f4610`:
  - After merge 1: exit 1 on goconst. After `c821a239e`: exit 0 (87 files).
  - After merge 2: exit 0 (122 files).

qa-all was not run.
