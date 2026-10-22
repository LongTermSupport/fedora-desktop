# n3c P4 handoff: the link and the engine (Plan 00209 N3, slice n3c)

The first P4 builder stopped at its context budget. This file is the state of the work and
everything the next builder needs. Read the common rules (`untracked/scratch/n3c-part-common.txt`),
section 5 of `261022-n3c-handoff.md` with 8a and section 9, and `261022-n3c-p2.md`, as the first
builder did. Do not re-read the design chapters. This file holds what they gave.

## State

Branch `feat/bus-v2-n3c-p4`, worktree `/workspace/untracked/worktrees/bus-v2-n3c-p4`. One WIP
commit after 1964da450:

- `src-go/internal/teamchat/member/engine/`: `engine.go`, `teams.go`, `seam.go`, `recv.go`,
  `render.go`, `status.go`, `serve.go`. It **compiles** (`go vet` is clean).
  - It is **not lint-clean**. The open findings:
    - gocognit: `handOut`, `settle`, `Persist` and `status`. Split them into helpers.
    - contextcheck: `teams.go` Add's `e.start` inside `do`. Pass ctx through, or restructure.
    - govet shadow: `teams.go` `load`, `if b, err := e.readBundle`.
    - ineffassign: `serve.go` `internal()`. Drop the dead nil branch and simply ignore err:
      the message is a fixed template.
    - modernize: two `maps.Copy` in `seam.go`.
    - gofumpt: `status.go`.
  - It has **no tests** yet. `engine_test.go` holds one placeholder. Coverage of 90% is still
    owed.
- `records.Team` gains `Counts records.TeamCounts` (`json:"counts"`, the fields ignored, dropped,
  unheard, gaps, redacted and seen_twice), for LN5's per-team counts. Its records test line is
  updated, and `go test ./internal/teamchat/member/records` passes.
- `link` is **not started**.

## The engine as written (exported API; change it only by recording why)

```go
type Config struct {
	Root, RunRoot *homefs.Root // state root (seats); the run dir (status.json)
	Layout paths.Layout; Run paths.RunID; Clock clock.Clock; Log io.Writer
	NewTeam func(TeamConfig) (team.Team, error)
	NewEnds func(team.Engine) team.Ends            // 8a
	Delivery string                                // socket | wait (refuse launch) | none (plain)
	Plain bool; FirstSessionStart <-chan struct{}
	OnState func(wire.State)                       // on the state goroutine, must not block; the link forwards to the keeper
	Runtime func() (records.Runtime, error)        // the link's /proc reader, for status
}
type TeamConfig struct { Team paths.TeamID; Seat paths.Seat; Lock *lock.SeatLock; RemovedUserIDs []string
	Engine team.Engine; Ends team.Ends; Root *homefs.Root; Plain bool; FirstSessionStart <-chan struct{}; Clock clock.Clock; Log io.Writer }
type RecordWriter interface { Record(records.Record) error; Flush() error }
type Removed struct { UserID string; Notices team.Notices }   // replaces the brief's (userID string, err)
var ErrStopped error
func New(cfg Config) (*Engine, error)
func (e *Engine) Run(ctx context.Context) error                // the state goroutine
func (e *Engine) Ends() team.Ends
func (e *Engine) Add(ctx, t paths.TeamID, l *lock.SeatLock, removedUserIDs []string) error // brief had seat; the seat is l.Seat(), checked against Layout.Seat(t)
func (e *Engine) Remove(ctx, t paths.TeamID) (Removed, error)
func (e *Engine) Serve(ctx, req wire.Request, out RecordWriter) wire.Done
func (e *Engine) Ack(ctx, ack string) error; func (e *Engine) Return(ctx, ack string) error
func (e *Engine) Refuse(ctx, t paths.TeamID, holder paths.RunID) error   // keeper's refused
func (e *Engine) SetDesired(ctx, d state.ReportDesired) error            // keeper's desired
func (e *Engine) EndTeams(ctx) ([]team.EndTeam, error)                    // for Ends.End
func (e *Engine) Shutdown(ctx) error                                     // cancel teams, close writers and seat fds
// plus team.Engine: Load, View, Persist, ResetPosition, SetEnded, BeginRun, SetHealth
```

## How the engine works, and the decisions taken

- **The state goroutine.** `do(ctx, f)` runs f on the goroutine (Run's loop). The goroutine
  calls **no team method** and makes no network call. Team calls such as `Notices().Roster()`,
  `Refetch`, `Verify`, `History` and `FlushFloor` happen outside it. This matters for more than
  the network: a team goroutine blocked in `Persist` while holding its own lock would otherwise
  deadlock against a `Roster()` call made from the goroutine.

- **Add.** It reads `seat.json` alone (no token): `homefs.Read` and `seat.DecodeMemberBundle`
  give the user id and the anchors (main room id). Then it loads the store:

  - missing: the store is nil until the team's first `Load` creates it, with this run and
    ended null, so P5 does no Close and no BeginRun;
  - not decoding: `refused`, `Health.Stopped = "store"`, and the team is not run;
  - otherwise each handed item goes back to pending, marked repeat and restored, in one write.

  `NewTeam` is called outside the goroutine. A failure there leaves the team held, stopped as
  `bundle`.

- **Remove.** The team is marked removed (its writes get `ErrRemoved`), then:

  1. `failpoint LinkInRemove`;
  2. cancel the team, wait at most 2 s on `clock.Hold`;
  3. `FlushFloor` under 2 s, best effort and logged;
  4. one log line counting the pending, handed and awaiting items dropped;
  5. `StoreWriter.Remove` (a store already gone is fine), close the writer and the lock.

  It returns `Removed{UserID, Notices}`. The link does presence, `Leave` and `left` from that.

- **recv** (`recv.go`):

  1. On the goroutine, plan the candidates: unhanded pending, oldest `sent_ms` first, stable by
     team id.
  2. Off it, read the rosters. Re-fetch the restored items (no content in memory, not a
     TIMEOUT) among the first 8 candidates: per team in parallel, in order, a team stopping at
     its first error. The bound is `clock.AfterFunc(2 s)` cancelling ctx.
  3. On the goroutine, judge them again: drop an item whose outcome is not Deliver, or whose
     `origin_server_ts` is at or under its room's `floor_ms`, with a log line. Then choose
     within the caps and set `Handed = ack`, one write per touched store. A store whose write
     fails has its records withheld, and its items stay pending.
  4. Off it, `failpoint LinkAfterHandOut`, write the records, then `more{pending}` when unhanded
     items remain in the teams read, then Flush. A write error calls `Return` and gives
     internal.

  The answer is `Done{Ack}` only when something was handed.

  - The caps: 8 entries, and at most 12288 bytes of record lines, each counted as
    `records.Encode` plus 1 for its newline. The first item always goes.
  - `Refetch` is passed the store's room **purpose**, not a room id. P5 must map it. Flag this in
    the report.

- **wait.** It loops:

  - register a waiter (woken by `Persist`);
  - if anything is pending, act as recv;
  - if recv handed nothing (a re-fetch failed), retry after `clock.Hold(1 s)` or on a wake;
  - at ctx's end, `Done{}` with no records (n3c-D7).

- **inbox** lists every pending item, handed ones included, as `records.Pending`, with the short
  `from`.

- **Ack and Return** settle every eligible team's items handed by that ack, one write each:

  - Ack moves them to the ledger, `at_ms` now;
  - Return sets `handed` null and `repeat` true;
  - a removed or refused team is skipped, so nothing is recreated (T2, P11).

- **The short `from`** (`render.go` `namer`):

  - an agent's `Short()` when it is unique in the verified roster, else the full handle;
  - a human or the admin by localpart.
  - The records' fields map from the Verdict: Human.Body; Content.Text.Text, else Body;
    Content.Ping, with `trust` "-" when there is no Forge; `re` from `Answer.Ping`;
    Content.Notice and Carried. A TIMEOUT is built from its key `event~silent` and the outbox
    ping.

- **status** (`status.go`). Per held team:

  - one `team` record. `trust` is trusted, untrusted or unverified (my set; B names none). It
    carries the new `counts`, with `seen_twice` 0 until n3e;
  - one `member` record per roster agent. `present` is true only for self: presence is not
    tracked in n3c;
  - three `room` records: main is joined; questions and reports are joined or absent by the
    roster.

  Then a `team` record with `down: true` and `delivery: none` for each refused add. Then
  `runtime`, from `Config.Runtime`; a failure to read it is logged and the record skipped.

- **publish** (after every change, on the goroutine):

  - `OnState(wire.State)` for each team whose report changed. A team with no handle is skipped:
    `wire.State` needs a handle.
  - `status.json` rewritten: rows for the held teams, plus a refused add as
    `{down: true, delivery: none}`. `failed` is mapped from `Health.Stopped`: token is
    unauthorized, forbidden is forbidden, untrusted is trust, bundle is bundle; anything else is
    null. SL1's pacing is n3e's.

- **Persist** is one write: the position and floors, new items (an id already pending or in the
  ledger is skipped), the redactions (pending or handed), the counts. Then it wakes the waits. A
  full pending list refuses the whole batch with the error, and nothing is dropped.

- **`internal()`** is a fixed template that never quotes the error. `notHeld` is 64 `usage`.
  `single()` resolves a single-team verb's team through `cli.ResolveTeam`, the ledger being the
  team whose store holds the event id. A history limit outside 1-100 is 64.

## Open question sent to the lead (no answer yet when I stopped)

Control order: nothing tells the link when the adds that follow `launch` are over. The plan is to
send `ready` right after `launch`, once `link.sock` listens, and to treat each add as it comes. A
request that lands before a late add sees no items for that team, and `--team T` gives 64. The
alternative is `Launch` carrying the count of adds. **Check the lead's reply before you build the
link start.**

## Left to do

1. Clear the engine's lint findings (above). Write its tests to 90%+ with `-race`. Use a fake
   team, fake notices and `clock.Fake`. The worlds to copy are `bundle_test.go` `newWorld` and
   `state/writer_test.go` `heldSeat` (`lock.TakeKeeper` on the run root, then `TakeSeat` on the
   state root).
   - Cover: the restore of handed items at Add, and a refused store; the caps at 8 and 9 items,
     and at 12 KiB exactly and one byte over; an ack lost, late, on a closed connection, and
     for a removed team.
   - Cover the re-fetch: it passes; it is dropped by its verdict; it is dropped by the floor; an
     offline team holds no other team's items (the fake blocks in Refetch until ctx ends;
     advance the fake clock 2 s).
   - Cover wait blocking until Persist, and wait's timeout; inbox; status and status.json;
     Remove (the flush, the store deleted, the lock closed, ErrRemoved afterwards, a team that
     ignores its cancel bounded at 2 s); View's Related and OwnPings; Load creating the store;
     BeginRun, SetEnded, ResetPosition; verify, history and the unbuilt ops.
2. Build `internal/teamchat/member/link` (brief section 5 and 8a). Its parts:
   - `Main(cli.Env, cli.Invocation)`: fd 3 is SOCK_SEQPACKET and its peer pid is `getppid()`.
   - `Run(ctx, Config)`: `lock.TakeLink`, 78 if held; pidfd on the parent, then re-check
     `getppid`; read `session.json` back; launch; adds through `lock.AdoptSeat`, then
     `Engine.Add`; unlink a stale `link.sock` under the lock, bind it 0600, listen, send
     `ready`. Ignore SIGTERM, SIGINT and SIGHUP.
   - The control loop, for every kind:
     - `remove`: Engine.Remove, answer `removed`, `failpoint LinkAfterRemoveAnswered`. Then,
       within 30 s, presence offline, `Leave` when the farewell is released, and `left`. A
       `removed` user id of "" cannot be sent (wire refuses it): log it and still send `left`.
     - `owed`: `NewTeam(cfg).Notices()`, presence offline and Leave by the txn, never Run;
       then `left`.
     - `pid` and `claude`: the session facts. `token`: held in memory, never logged.
     - `desired`: SetDesired. `refused`: Refuse.
     - `end`: `Ends.End` with `EndTeams`, then exit.
     - EOF, or the pidfd firing: `End(how "keeper-died")`, presence offline, within 5 s, then
       exit.
   - The socket server: SO_PEERCRED uid; one request line of at most 64 KiB (10 size); a kit
     that is not the link's is 78 runtime; a deadline already past means discard and close;
     the records, then done. For an ack: wait for `{"ack"}` for 10 s, then Ack, or Return on a
     close or timeout. `failpoint LinkWhileHanded` goes after done is sent.
   - `AcceptFacts` for LN7: the ppid chain walked at most 64 steps, against the session pid and
     its start time; the socket only as `paths.InboxSocket(runtimeDir, pid)` for a pid on the
     chain at or below the session's; the transcript only as
     `<claude_config_dir or ~/.claude>/projects/<cwd with every non-alphanumeric made "-">/<id>.jsonl`,
     with no symlink; a new session id only on session-start clear or resume, or on a new link's
     first op; the first socket fixes Claude Code's pid (send `claude`), and another pid is
     refused with the fixed line; `session.json` written before the answer; the conversation
     record written when the session id changes; `started{}` sent once, and FirstSessionStart
     closed, on the first accepted session-start.
     - Note: `state.Session`'s codec is all-or-nothing on id, source, transcript, socket and
       claude, so facts are accepted only as a whole set. A first op that is not session-start
       records source `startup`.
   - `Proc`: real `/proc` readers for the ppid, the start ticks (field 22) and the cost (rss,
     cpu, fds, wakeups, uptime).
   - Real-process tests through reexec. `reexec.Start` cannot pass fd 3, so add a small
     `StartFiles` in its own hunk. Cover the signals ignored, EOF giving keeper-died within 5 s,
     and P11's SIGKILL while handed.
3. Run qa-changed, both lint runs (with and without `--build-tags tuwunel`) and the
   busfailpoints run; then commit, and write `261022-n3c-p4.md`, with the owner-rule section:
   how an offline member never blocks anything (the goroutine makes no network call; the
   re-fetch is 2 s per team, in parallel; the removal is bounded at 4 s; the ends are bounded).
