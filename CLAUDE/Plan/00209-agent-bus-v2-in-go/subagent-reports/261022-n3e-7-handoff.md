# n3e-7 handoff: room facts, receipts and status.json (Plan 00209 N3, slice n3e)

Worktree `/workspace/untracked/worktrees/bus-v2-n3e-7`, branch `feat/bus-v2-n3e-7`, from
`feat/bus-v2-n3e` at 2d37b5077. The first builder stopped at its context budget. Everything
below "Done" is committed. The tip commit is in the lead's message.

## Done (tests green under -race; golangci clean untagged)

- **teamsync, room facts** (`facts.go`, `facts_test.go`). `readFacts(state, roster, anchors)`
  gives `roomFacts{colour, description, coordinator}` and a note. It uses `proto.MainColour`
  and `MainDescription`, and the coordinator is the roster's non-parked coordinator's handle.
  `Team.setFacts` puts them in `Health` when they change. It logs the note only when the note
  changes, which gives "one link.log line" for a malformed or non-admin colour, and the team
  stays trusted. `session.go`: `verify` now calls `mainState` (the roster and the state it was
  read from), then `setFacts`. `verifyMain` wraps `mainState` for ln6. A colour or topic event
  in the timeline already triggers verification, because n3e-3 put both types in `StateTypes`.
  So "re-read at each verification and at a sync that carries one" needs no other code.
- **teamsync, delivery budget** (`budget.go`, `budget_test.go`, and hunks in `session.go`).
  `budgeted(v)`: an agent sender's (coordinator, worker or probe) PEER, PING or NOTICE that is
  not carried. `sess.admit(j, v)` runs in `take`'s Deliver case: deliver_per_sender items per
  `proto.DeliveryKey(team, sender)` per `proto.DeliveryWindowMs`. Every item over the budget
  adds one to `j.batch.HeldBack[sender]` and never enters pending. The window is copied into
  `judged.budget` and committed in `commit`, as `flood` is (and then pruned). `judged.now` is
  the one wall time for the judge and the budget. `empty()` counts `HeldBack`. Tests: T19
  (11th held, the window slides by `StepWall`, humans never held), a discarded answer counts
  nothing, carried NOTICEs are never held (judge seam), and the `budgeted` table.
- **teamsync, receipts** (`receipts.go`, `receipts_test.go`). `Config.Receipts team.Receipts`
  (nil sends none), and the seam `Team.onReceiptWait`. `session()` runs
  `defer s.startReceipts(ctx)()` just before `poll`: one sender goroutine per session, on the
  session's client. A stopped team runs no session, so it sends nothing. `m.read` goes to
  `roomOf(r.Room, anchors, Roster())`. Transient failure: SY8's backoff (1 s doubling to 30 s,
  jitter). 401: the sender ends until the next session. 404, 403, or a room the roster does
  not name: dropped with one line. `ReceiptSent`'s ErrRemoved ends the sender. Any other
  `ReceiptSent` error retries, so the receipt is sent again. Tests cover each case.
- **engine, receipts** (`receipts.go`, `receipts_test.go`). `PendingReceipts` and
  `ReceiptSent` (one store write, none for an unknown id). `held.receipts` is the change
  channel. `changeLocked` (`seam.go`) closes it when the store's receipts changed, and `drop`
  closes it at a removal. `TeamConfig.Receipts`: the engine passes itself in `Add`.
  `wiring.NewTeam` passes `c.Receipts` to teamsync (one-line hunk). The link's owed-leave team
  gets nil Receipts, because it never runs.
- **inbox test fix** (`member/inbox/phase_test.go`, n3e-1's file, own hunk).
  `TestAnAuthLineNobodyReadsIsRetry` hung in qa-changed's busfailpoints stage under
  `-shuffle=1792711833194566312`. `expire` could see the connect phase's bound, which is
  already stopped, and the auth line's bound was armed after the clock moved. `expireUntil`
  keeps advancing until Dial answers.

## Left

1. **status.json pacing** (SL1, T12), engine `status.go`. Design:
   - `publish` builds the status (without `written_ms`) and hands it to a pacer
     (`e.pace`, used only on the state goroutine). A change writes at once when the last
     write is at least 1 s old (boot clock) or none was made. Otherwise it arms one timer at
     the last write + 1 s, and the newest status is written then.
   - After each write, while a team is held (not removed; refused rows do not count), arm a
     heartbeat at +60 s that rewrites with a fresh `written_ms`. With no team, disarm.
   - Arm with `Clock.AfterFunc(d, func(){ e.do(context.Background(), tick(gen)) })`. The gen
     counter ignores a stale fire. On the fake clock, Advance then runs the write
     synchronously, so the tests are deterministic. Keep the earlier due time when one is
     already armed.
   - `Run` calls `e.publish()` once before its loop ("with no team, once").
   - A failed write is logged and not retried, to avoid a log line every second; the
     heartbeat retries it.
   - Compare statuses with `reflect.DeepEqual`.
   - In `engine_test.go`, `w.status()` must flush a pending paced write first (for example
     `w.local(func(){ if e.pace.dirty { e.writeStatus(...) } })`), so the existing tests
     that read status right after several changes stay valid.
   - The drafted tests are in `untracked/scratch/engine_pace_test.go.draft` in this worktree:
     two changes within 300 ms are one write at +1 s; a quiet held team (and one stopped on
     `token`) is written each minute; no team is written once and then only on change; OF1
     has a down team written beside a running one. Use `state.DeliveryWait`, so that no
     delivery loop runs.
2. Run `golangci-lint run` from `src-go` with `--build-tags tuwunel` and with
   `--build-tags busfailpoints`. Run teamsync's tuwunel tests with `FAKEHS_TUWUNEL_BIN`
   (the sync and the verification changed). Check coverage of 90% or more per package. When
   this was written: teamsync 94.6%, and engine not yet measured with the new code.
3. The report, `subagent-reports/261022-n3e-7.md`.

## Decisions taken (for the report)

- **D1.** Engine receipts live in `engine/receipts.go`. The brief's `engine/facts.go` is not
  needed: n3e-3 already fills `Member`/`Colour` in `statusRow`, and teamsync supplies
  `Health.Colour`.
- **D2.** The budget is in a new `teamsync/budget.go` (not named in the brief), with hunks in
  `session.go`'s `take`, `judged`, `commit` and `empty`.
- **D3.** The receipt sender is scoped to a session: it starts before `poll` and ends with the
  session. "Nothing when stopped" therefore holds by construction, and the sender always uses
  the session's own client.
- **D4.** A 404, a 403, or a receipt for a room the roster does not name is dropped through
  `ReceiptSent`, with one line, because retrying could never succeed. A 401 waits for the next
  session.
- **D5.** The facts note is logged once per change of the note, not once per verification.
  This gives C9's "one link.log line" for each bad state seen.

## API as built

```go
// teamsync
type Config struct{ …; Receipts team.Receipts } // nil sends none
// engine
type TeamConfig struct{ …; Receipts team.Receipts } // the engine passes itself
func (e *Engine) PendingReceipts(t paths.TeamID) ([]state.Receipt, <-chan struct{}) // not held: nil, nil
func (e *Engine) ReceiptSent(ctx context.Context, t paths.TeamID, id string) error  // ErrRemoved after a removal
```

n3e-9 adds receipts at proof with `state.Store.AddReceipt` inside an `e.change`. The
`changeLocked` hunk wakes the sender automatically.
