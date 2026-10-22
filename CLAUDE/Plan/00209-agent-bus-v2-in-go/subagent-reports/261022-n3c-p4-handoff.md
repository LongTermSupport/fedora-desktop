# n3c P4 handoff: the link and the engine (Plan 00209 N3, slice n3c)

Two P4 builders have stopped at their context budget. This file is the state of the work and
everything the next builder needs. Read the common rules (`untracked/scratch/n3c-part-common.txt`),
section 5 of `261022-n3c-handoff.md` with 8a and section 9, and `261022-n3c-p2.md`. Do not
re-read the design chapters, and do not re-read the engine's source: its API is below.

## State

Branch `feat/bus-v2-n3c-p4`, worktree `/workspace/untracked/worktrees/bus-v2-n3c-p4`.

- **The engine is done** (commit 3baeb2a1c). Its tests give 94.2% coverage with `-race` and
  `-shuffle=on`. golangci is clean with and without `--build-tags tuwunel`, and qa-changed is
  green. The test world is `engine/engine_test.go` (`newWorld`, the fake team, notices and ends,
  the `recorder`), and the link's tests can copy it. The tests found two defects, now fixed and
  covered:

  - `Persist` woke the waits off the state goroutine (a data race on the waiter list).
  - `Persist` kept a batch's content and counts before its write, so a refused batch was
    counted twice on its retry.

  The other changes:

  - `do` refuses a context that has already ended. Before, `select` chose at random between
    the two ready cases, and an expired request could still act.
  - The team goroutine's context is `context.WithoutCancel` of the add's ctx.
  - `status` now flushes its records.
  - The hand-out writes its stores in team order (go-qa's map-range-order analyzer).

- **n3c-D24 is done** (8373cf4e7, 383ba4694): `wire.Launch.Seats` (`json:"seats"`, a negative
  count is a schema error). `control.go` is byte for byte P3's acbbacaa0 hunk, so the two
  branches merge cleanly. `control_test.go` also has `Seats: 2` in one good launch, a separate
  hunk.

- `records.Team.Counts` is as before (seen_twice 0 until n3e).

- **The link is not started.** It is all that is left. See "Left to do" item 2 below; item 1
  is done.

- P3's keeper is on `feat/bus-v2-n3c-p3`. Its link contract is in its handoff
  (`git show feat/bus-v2-n3c-p3:CLAUDE/Plan/00209-agent-bus-v2-in-go/subagent-reports/261022-n3c-p3-handoff.md`,
  lines 155-222). It starts the link:

  - as `<kit>/bin/teamchat link --run <dir>`, in its own session;
  - with stdout and stderr on `link.log`;
  - with fd 3 the link's end of `wire.Pair`;
  - with env `PATH=<kit>/bin:$PATH` and `TEAMCHAT_KIT=<kit>`.

  It sends, in order:

  - `launch` (with seats);
  - each `owed`;
  - each held seat's `add`, with the descriptor;
  - `pid`, `claude`, and `token` when known.

  An end-only start sends `launch`, the adds and `end`. It waits 30 s for `ready`. At a stop
  it sends `end` and kills the link 5 s later. If `removed` has not come 10 s after `remove`,
  it kills the link.

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

## Decision n3c-D24 (the lead's answer): Launch carries the count of adds

`wire.Launch` gains `Seats int` (`json:"seats"`, >= 0): the number of `add` messages that follow
`launch`. The link sends `ready` once that many adds are handled. This is a wire change on P2's
types: add the field, its rule and its test in `wire/control.go`, in its own hunk. The lead is
telling P3 to send it. Why: the keeper starts CMD on `ready`, and the first SessionStart request
must see every team's backlog.

The offline rule's constraints:

1. An add is complete once its store is loaded and its handed items are put back. It never
   waits on the network: a team whose homeserver is down still completes its add, and shows
   offline. `Engine.Add` already works this way: `NewTeam` must not dial, and `Run` starts in
   its own goroutine.
2. An add that fails (an unreadable store, a bad bundle, a refused `AdoptSeat`) still counts
   toward Seats. It is reported for that team (78, or offline in status) and never holds back
   `ready` for the others.
3. A Seats mismatch is a protocol error, reported, and never waited on for ever: the socket
   closing before Seats adds, or another kind arriving before them. Bound the wait for the adds
   with the clock seam, say 10 s on `clock.Hold`, and record the bound. Then send `ready` with
   what was added, log the shortfall, and treat a later add as a hot add. An add beyond Seats
   after `ready` is an ordinary hot add, not an error.

Record n3c-D24 in the part report.

## Left to do

1. The engine's lint and tests are done (see State). Every case this item listed is covered.
   An ack that comes late is the link's 10 s timer, so its test belongs with the link: the
   engine's side, a Return and then the late Ack delivering nothing, is covered.
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

## P4c state (the third builder stopped at its context budget)

The link package `src-go/internal/teamchat/member/link` is written, test first, and committed
as WIP. Read its files, not the design: they are the state.

- `proc.go` (+`proc_test.go`): `Proc` {PPid, StartTicks, Cost}, `Cost`, `OSProc{Dir}`. Green.
- `facts.go` (+`facts_test.go`): `Facts`, `Session` (session.json's writer), `newSession`,
  `SetSessionPID`, `SetClaude`, `EndReason`, `AcceptFacts` (LN7: chain of at most 64 parent
  steps, the socket only `paths.InboxSocket` of a chain pid at or below the session's, the
  transcript only the derived path with no link below the config dir, the id swap rule, one
  Claude Code pid with the fixed line, session.json then conversation.json, `started` once).
  Decision: "a new link's first op" is the first facts this link accepts; a corrupt
  session.json is logged and started afresh. Green.
- `server.go` (+`server_test.go`): `listen` (unlink stale, bind, 0600), `server` over a
  `served` interface (Serve/Ack/Return): SO_PEERCRED uid, 64 KiB line (10 size), 10 schema,
  another kit 78 runtime, past deadline closed unacted, deadline via `Clock.AfterFunc`,
  `readLimit` 5 s for the line (my bound), `ackWait` 10 s, failpoint `LinkWhileHanded` after
  done, a refusal drains the client's input so the answer is not lost to a reset. Green.
- `link.go` (+`link_test.go`): `Config`, `Parent`, `Run`, the control loop (every kind),
  n3c-D24's ready (`addsWait` 10 s on `Clock.Hold`, covering launch too), remove/owed/leave
  (30 s), finish (End + presence offline, 5 s), state coalescing, runtime. JUST WRITTEN: its
  tests had not yet been seen to pass when the budget was hit (a run was in flight). Run
  `go test -race -cover ./internal/teamchat/member/link/` and fix what fails.

Left:

1. Make `link_test.go` green; golangci (with and without `--build-tags tuwunel`); the lint
   left at the stop: `tid`, `agent` in world_test.go may be unused; check `funcorder`.
2. `main.go`: `Main(w Wiring) cli.RunFunc` (decision: Main needs W's NewTeam/NewEnds, so it
   is a constructor, `type Wiring struct{ NewTeam; NewEnds }`); fd 3 via `wire.NewConn`
   (SOCK_SEQPACKET), its `PeerPID` must be `os.Getppid()`; `--run <dir>` gives the run id
   (base name) and the layout (`paths.CCYLayout()` when its run dir is that, else
   `HomeLayout(home, $XDG_RUNTIME_DIR)`, else usage); `signal.Ignore(SIGTERM, SIGINT, SIGHUP)`; roots by `homefs.Open`; the real `Parent` (pidfd_open on getppid, then getppid
   re-checked, a poll goroutine closing Exited; a pipe to stop it on Close).
3. `reexec.StartFiles` in its own hunk (ExtraFiles from fd 3) and the real-process tests:
   signals ignored, EOF gives keeper-died within 5 s, P11's SIGKILL while handed (busfailpoints
   build, `LinkWhileHanded`), restart repeats the items.
4. qa-changed, the report `261022-n3c-p4.md` (engine and link exported API as built, the
   decisions above, D24, the owner rule section).
