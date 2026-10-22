# n3e-6 handoff: delivery (DV1's use, DV2, DV3, the session-level send for DV6)

Worktree `untracked/worktrees/bus-v2-n3e-6`, branch `feat/bus-v2-n3e-6` (base 640af55ed). The
first builder ran out of context after the design and the tests. No production code is written.

## State

- Committed with this file: `engine/engine_test.go` gains `newWorldWith(t, delivery, prep func(*Config), ready func(*Engine))`; `newWorld` calls it.
- **Uncommitted in the worktree, written first (RED, they do not compile yet):**
  `src-go/internal/teamchat/member/engine/deliver_test.go` (about 25 tests: a fake dialer, a
  `dworld`, every DV1/DV2/DV3/OF1/DV6 row of section 8) and `deliverloop_test.go` (end to end over
  `inbox.NewDialer` and `fakeinbox`). Read them first: they pin the API and the field names below.
  Adjust a test only where it is wrong, and say so.
- The `busfailpoints` gate row (15a) already exists: `src-go/qa/go-qa.toml` stage
  `teamchat-failpoints-test`. Nothing to add.

## The decision the code needs (record it as n3e-6 D1)

An `awaiting` entry holds ids only, so items that "leave pending" would lose room, item, from and
`sent_ms`, which the ledger needs at the full write and `missing` (n3e-9) needs to put them back.
So an item in an awaiting entry **stays in `pending` with `handed` set to the delivery's nonce**
until proof settles it. The awaiting entry's `ids` say which items are awaiting, never the handed
value. This keeps the floor below every owed item for free (`ownedFloors`). The consequences are
the hunks below: `awaitingIDs(s)` guards `settleStore` (Ack/Return never settle them),
`restoreHanded` (a restarted link does not return them), `apply`'s redaction (left alone), and
`held.pending()` (status.json's `pending` excludes them). At the full write the ids also go into
the ledger. For n3e-9: proven removes the items from pending (and the entry); missing sets
`handed` nil, `repeat` and `held`, and takes them out of the ledger.

## API to build (the tests use these names)

```go
// engine.Config gains
Inbox      inbox.Dialer          // nil: no socket delivery
Unusable   state.Unusable        // session.json's counts at start
OnUnusable func(state.Unusable)  // on the state goroutine, must not block; nil allowed (15b)
// Engine gains: kick chan struct{} (cap 1), members chan memberReq, hit func(failpoint.Name)
// (= failpoint.Hit; tests replace it), idles atomic.Int64 (each time the loop goes idle), and on
// the goroutine: sess {socket, token, transcript string; idle, gone bool},
// stall {failing, stalled bool; since time.Duration}, unusable state.Unusable.
// held gains `delivery deliveryState`: answered []proto.Answered; heldBack map[string]int64;
// gap bool; stopTold string; trailingAt time.Duration; trailingSent bool; fetchAfter time.Duration.
type SessionFacts struct{ Socket, Token, Transcript string; Idle bool } // empty string keeps the previous; Idle always taken; a Socket clears gone; pokes the loop
func (e *Engine) SetSession(ctx context.Context, f SessionFacts) error
func (e *Engine) Live() bool // socket mode, Inbox set, socket+token+transcript set, not gone
func (e *Engine) SendMembership(ctx context.Context, m proto.Membership) (bool, error)
const paceWait, retryWait, stallAfter, trailingEvery = 2*time.Second, 10*time.Second, 120*time.Second, time.Minute
```

## The loop (`deliverloop.go`)

`Run` starts it (socket mode and Inbox only) and waits for it before closing `done`. Loop:
`waitTurn(next)` (Hold until boot >= next, taking one membership request meanwhile), then a
queued membership send or `deliverOnce`; `next = start+retryWait` on a retry, `start+paceWait`
on any dial; idle waits on `kick`, `members`, a due timer (trailing facts, refetch backoff) or ctx.
`wake()` and `SetHealth` and `SetSession` call `poke()`.

`deliverOnce`: (1) on the goroutine, a snapshot: if not live, idle; per eligible team with a team
and a handle, the deliverable items (handed nil, not held), skipping restored items without
content while `now < fetchAfter`; whether its trailing facts are due. (2) off it, `rosters()`;
only verified teams; `deliver.Choose`, else the first team with due facts; none: idle with the
earliest due. (3) off it, `e.refetch` over the team's candidates ordered HUMAN first then
`sent_ms`. (4) on it, `prepare`: same `*held`, still live; a transient refetch failure sets
`fetchAfter = now+retryWait`; `rejudgeAll`+`commit`+`floorRaised`; items mapped to
`deliver.Item` (content from `h.content`, TIMEOUT from the key and `Outbox.Pings`, Question
nil, recorded as a decision: there is no question index in the engine yet); `deliver.Build`
with a namer (agent: `Handle.String()` and its roster role; admin: `admin`, `RoleAdmin`;
human: localpart, `""`) and a nonce from `crypto/rand` (8 hex) distinct from the store's awaiting
and missed nonces. A Build error drops the team's trailing facts with a `trailing facts` log
line (so they never hold the team). Empty text: drop any Failed ids in one write and count them;
consume a pace slot. (5) off it, `Dial`: `ErrGone` marks the socket gone (log `gone`), idle;
any other error is a failed write (stall) and a retry. `hit(LinkAfterAuth)`; transcript size by
`os.Lstat` (missing: 0). (6) on it: abandon (no write, no user line) if the team or any id
changed (a recv took them); else one write: Failed ids dropped (`raiseFloors`), and when there
are ids, `AddAwaiting{nonce, ids, {path, size}, at_ms = Boot ms, unknown, stop_ms = Boot ms if idle}` and `handed = nonce`; then count the render drops (`unusable.Render`, `OnUnusable`, a log
line per id). `hit(LinkAfterAwaiting)` when an entry was written. (7) `WriteUser`: nil:
`hit(LinkAfterUserLine)`, one write `written yes` and `AddLedger` per id (wall `at_ms`),
facts told (answered trimmed by the snapshot's length, heldBack subtracted, gap cleared,
stopTold set), `trailingAt` for a trailing-only one, stall cleared. `ErrRetry`: one write
removing the entry and setting handed nil (no repeat), `wake()`, stall failure, retry.
Otherwise (ErrPartial): log, stall failure, the entry stays unknown. (8) `Finish` (log its
error), `Close`; `context.AfterFunc(ctx, conn.Close)` so a shutdown never waits on a phase.

Stall: the first failure sets `since`; at `boot-since >= stallAfter` set `stalled` and
`publish()`; a full write clears it and publishes. `statusRow`: `Stalled = stalled && Pending > 0`.

Facts: `Persist` hunk appends `b.Answered`, adds `b.HeldBack`, ORs `b.Gap`; `SetHealth` hunk
resets `stopTold` when `Stopped` clears. Stopped is due while `Stopped != "" && != stopTold`.
Trailing-only is due at most once per `trailingEvery` per team, never an awaiting entry. Held
back lines: sorted by sender, `From` from the namer.

Failpoint `LinkBetweenStoreWrites` (n3e-D8): `commit` hits before every write after the first;
`settle` hits inside the change closure when a write is coming and one already happened.

SendMembership: false, nil when not socket mode or not live; else a request into the loop
(shares the pace); the loop renders with a fresh nonce (`m.Render` error is returned), dials,
writes: true only on a full write; gone marks gone; failures count for the stall. n3e-D7: no
store, no awaiting.

## The link hunk

`build`: `Inbox: inbox.NewDialer(l.cfg.Clock)` in socket mode, `Unusable: l.session.Unusable()`, `OnUnusable: l.onUnusable` (latest kept under a mutex, a wake channel,
a `l.bg.Go` writer calling `Session.RecordRender(n)`, which writes session.json's
`unusable.render`). `KindToken`: also `l.eng.SetSession(ctx, SessionFacts{Socket, Token, Transcript})` with the socket and transcript from session.json (`Session.Inbox()`), so a
restarted link delivers again before any hook. Tests for both in `link`.

## Left

All production code, the link hunk and its tests, then coverage (90%), `-race`, golangci-lint
untagged and with `tuwunel` and `busfailpoints`, `qa-changed`, and the report
`subagent-reports/261022-n3e-6.md` (what was built, the API as built, coverage, decisions D1 and
the Question-nil one, and that 15a's gate row already existed).
