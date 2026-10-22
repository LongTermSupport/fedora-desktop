# n3c handoff: the slice split into six parts (Plan 00209 N3, slice n3c)

Design only; no code is written here. n3c is SPEC-n3.md section 4 (RN1-EN5), with RS1-RS2 and UH1
of section 8, gaps 5-7, 9, 11, 12 and 14 of section 9, and the decisions of section 10. It is too
big for one builder, so it is split into six parts, P1 to P6, and a closing wiring step W. Each
part is one worktree, one branch and one builder. Each is reviewed to MERGE on its own and merges
into `feat/bus-v2-n3c`.

Base: `feat/bus-v2-n3c` at 17bdc45f1. It holds N1, N2 (n2f included), the N3 spec and n3a's
paths, reexec, homefs, lock, state, bundle, exits, records, cli and kit. n3a's guest operations
and `seat set-aside` are still being built on their own branch: do not touch `member/guest`, and
keep `cli`'s `"n3a guest"` unbuilt row working.

Every API quoted below as "built" was read from `go doc -all` at 17bdc45f1. Each signature a part
adds is that part's decision: a builder may change it only by recording the change, and its
reason, in its part report.

## 0. Order

| Wave | Parts                      | Needs                    | Why                                                                                                                                                                                                |
| ---- | -------------------------- | ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1    | **P2** (shared types) ∥ P1 | the base                 | P2 is the types every later part compiles against (clock, failpoints, the socket and control wire, the interfaces between engine, sync and ends). P1 is the built-package gaps on the network side |
| 2    | P3 ∥ P4 ∥ P5 ∥ P6          | P1 and P2 merged         | each owns its own package(s) and meets the others only through P2's `member/team` interfaces, against fakes                                                                                        |
| 3    | W                          | P3, P4, P5 and P6 merged | the `link` and `run` verbs wired to the real parts; the end-to-end tests; the release-tag test with n3c's verbs gone from `unbuilt`                                                                |

P2 lands first in the sense that matters: nothing in wave 2 compiles without it. P1 does not
depend on P2, and P2 does not depend on P1, so they are built at once. Wave 2 starts when both
have merged, because P4 (the engine's memory-only `Lookup`) and P5 (the resolver, the
predicate, the fakehs restore and the hold timer) read P1's proto and fakehs changes.

Every part keeps the rules of SPEC-n3 section 1 (TB4 for member files, CK1's clocks), the 90%
coverage floor per package, and `-race`. No test sleeps (`project_testclock`: no `time.Now` in
tests). `os/exec` is reached only through `execx` (`project_seam_exec`). A run-time command name
needs its `dynamicNames` row in `src-go/qa/rules/table.go` (`project_seam_commands`).

Gotchas from n3a, still true:

- `sed` is denied, and so is a pipe into `head` or `tail`. Redirect output to a file under
  `untracked/scratch/` and read that.
- `x, _ := f()` is denied by the Write hook; use named results.
- `qa-changed.bash` is red only on govulncheck GO-2026-6809. Measure from `src-go` with
  `go test -race -cover ./internal/teamchat/...` and `golangci-lint run`, with and without
  `--build-tags tuwunel`. P2 adds a third run, with `--build-tags busfailpoints`.

## 1. Where the carried items land

| Item                                                                                       | Part                                                                                                                 |
| ------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------- |
| Gap 5 (the sync request takes proto's filter; the re-add form, limit 0)                    | P1                                                                                                                   |
| Gap 6 (fakehs `Snapshot`/`RestoreTo`, epoch raised in the same step)                       | P1                                                                                                                   |
| Gap 7 (`execx.Start`)                                                                      | P3                                                                                                                   |
| Gap 9 (`busfailpoints`, n3c's failpoints)                                                  | P2 (the package), each part its own points                                                                           |
| Gap 11 (proto's "reached no agent" predicate)                                              | P1 (predicate), P5 (the count, UH1)                                                                                  |
| Gap 12 (fakehs hold timer; the client's per-try deadline on a seam)                        | P1                                                                                                                   |
| Gap 14 (resolve related events before `Judge`; `Related.AddressedToRosterAgent`)           | P1 (proto half), P5 (the resolver), P4 (the ledger and outbox half)                                                  |
| Round 3 minor a (the deadline seam is its own, not the 429 `Sleep`)                        | P1                                                                                                                   |
| Minor b (LX7's deferred seat: "removed" memory keyed by `user_id`)                         | P3 (keeper memory), P4 (the `removed` answer carries the user id), P5 (SY1 (b) or (c) by it)                         |
| Minor c (n3-D20 against B 2.7.3)                                                           | closed already: n3-D20 records the departure, and B 2.7.3 (lines 1528-1531) names the second fetch. Nothing to build |
| Minor d (a poison relation)                                                                | P5 (bounded: n3c-D9)                                                                                                 |
| Minor e (UH1 dedupe by event id)                                                           | P5 (n3c-D10)                                                                                                         |
| Minor f (own sends live in memory only)                                                    | P5 (n3c-D11)                                                                                                         |
| The token race (old `seat.json` with a new token after GU3's token-then-seat.json replace) | P5 (n3c-D12)                                                                                                         |
| `reach`                                                                                    | P6 (n3c-D16)                                                                                                         |

## 2. P1: the network-side gaps (proto, matrix, fakehs)

Packages: `internal/teamchat/proto`, `internal/teamchat/matrix`, `internal/teamchat/matrix/fakehs`.
No new import edges: proto still imports nothing of teamchat, and matrix still does not import
proto.

### Built API it changes

- `proto.Related` is built as `struct { Sender, RoomID, Verb string; AddressedToSelf bool }`, and
  `proto.Lookup` as
  `interface { Seen(id string) bool; Event(id string) (Related, bool); OwnPing(id string) (OwnPing, bool) }`.
  `Judge.Judge(raw Value) Verdict` is pure and has no error path.
- `proto.SyncFilter(r Roster, a Anchors) Value` puts `timeline.limit` at `timelineLimit`
  (`filter.go:35`). `Value.Compact() []byte` exists (`strictjson.go:149`).
- `matrix.SyncRequest` is `{Since string; Timeout time.Duration; Filter *RoomFilter; SetPresence string}`,
  and `syncCall` sets `filter` from `RoomFilter.JSON()`. `RoomFilter` has no presence senders, and
  a zero `TimelineLimit` is not sent.
- `matrix.Clock` is `interface { Sleep(ctx, d) error }`. It is used for the 429 wait
  (`client.go:373`) and the send retry (`member.go:171`). `once` makes its per-try deadline with
  `context.WithTimeout(ctx, limit)` (`client.go:387`), and `Sync`'s limit is the timeout plus 15 s
  (`syncGrace`).
- `fakehs.Options` is `{ServerName; Now func() time.Time; Quirks}`. `sync.go:100` holds a
  long-poll on `time.NewTimer(timeoutMS)`. There is no snapshot or restore.

### What it adds

```go
// proto (gap 14, gap 11, gap 5)
type Related struct {
	Sender, RoomID, Verb   string
	AddressedToSelf        bool
	AddressedToRosterAgent bool // gap 14: read by ReachedNoAgent
}
// ReachedNoAgent is gap 11's predicate over a decoded human message: no agent of the verified
// roster in Mentions, no @room, no reply or thread root whose Related.Sender is a roster agent,
// and no edit of a message whose Related.AddressedToRosterAgent is true. A relation the lookup
// misses counts as reaching no agent (n3-D20's undercount).
func ReachedNoAgent(h Human, r Roster, server string, l Lookup) bool
// RelationIDs are the related event ids a human event's addressing reads (reply, thread root,
// edit original), for the resolver that runs before Judge (SY4a); nil for any other event.
func RelationIDs(raw Value) []string
// SyncFilterReAdd is SyncFilter with timeline.limit 0, for SY1 (c)'s first sync (n3-D5).
func SyncFilterReAdd(r Roster, a Anchors) Value

// matrix (gap 5, gap 12, minor a)
type SyncRequest struct {
	Since       string
	Timeout     time.Duration
	Filter      *RoomFilter     // kept for the admin side's tests
	FilterJSON  json.RawMessage // gap 5: proto.SyncFilter(...).Compact(), sent byte for byte; at most one of the two
	SetPresence string
}
// Timers is how a try's deadline is armed (minor a: not the 429 Sleep). nil is the real one.
type Timers interface {
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}
// Config gains: Timers Timers
```

`once` becomes `tryCtx, cancel := context.WithCancel(ctx)` plus `stop := c.timers.AfterFunc(limit, cancel)`.
The real `Timers` is `time.AfterFunc`. Every existing matrix test passes unchanged, because a nil
`Timers` is the real one and those tests never reach a deadline.

```go
// fakehs (gap 6, gap 12)
type Options struct {
	ServerName string
	Now        func() time.Time
	Quirks     Quirks
	Hold       func(d time.Duration) (<-chan time.Time, func() bool) // gap 12: nil is time.NewTimer
}
type Snapshot struct{ /* unexported: a deep copy of the database */ }
func (s *Server) Snapshot() Snapshot
// RestoreTo rolls the database back to snap (rooms, events, state, room account data, tokens,
// batch positions), keeps the listener and open connections, and raises the roster's
// restore_epoch, all under s.mu in one step (gap 6), so no request sees the restored database
// with the old epoch. A held sync whose since is now ahead of the database waits out its timeout
// and answers its own lower next_batch, as Tuwunel 1.9.4 does (H-C14).
func (s *Server) RestoreTo(snap Snapshot) error
```

How `RestoreTo` finds the roster: it rewrites the content of the `teamchat.roster` state event in
every room that has one. It parses the content as a generic JSON object, sets `restore_epoch` to
the old value plus 1, and stores the event as a new state event sent by the event's original
sender. It does not import proto. If the event is missing, or has no integer `restore_epoch`,
`RestoreTo` returns an error, and the test that called it is at fault (n3c-D1).

### Expectations and tests

P1 owns no SPEC ID outright. It makes these possible: SY2 (the filter byte-equal to proto's),
SY4a and UH1 (the predicate and the flag), SY7 (restore and the hold timer) and SY1 (c).

Tests:

- A sync query's `filter` is byte-equal to `proto.SyncFilter(...).Compact()` (gap 5's pin).
  `SyncFilterReAdd` differs only in the limit.
- `ReachedNoAgent` has a table for each clause, each with the related event known and unknown.
  The rows include a reply to agent X's event read at Y, and an `m.emote` (which never reaches
  the predicate, because `DecodeHuman` refuses it).
- With a fake `Timers`, a sync try times out exactly when the test fires the timer. A 429 retry
  arms a fresh timer, which proves SY7's "per try".
- fakehs: `Snapshot` and then `RestoreTo` under a held long-poll, with the fake `Hold`. The held
  sync answers the lower `next_batch` when the test releases the hold. A state read straight
  after `RestoreTo` sees the raised epoch. The replay test (`replay_test.go`) still passes.

## 3. P2: the shared types (lands first)

Packages:

- new `internal/teamchat/clock`
- new `internal/teamchat/failpoint`
- new `internal/teamchat/member/wire`
- new `internal/teamchat/member/team`
- additions to `member/state`, `member/lock`, `member/bundle`, `paths` and `member/cli`

Import graph (arrows are imports):

```text
clock      -> (std, x/sys/unix)
failpoint  -> (std)
wire       -> exits, records, paths, proto (Parse only), x/sys/unix
team       -> state, lock, paths, proto, records, notice
cli        -> (unchanged; no new teamchat import)
```

### clock (CK1; also the fake every later part drives)

```go
type Clock interface {
	Boot() time.Duration   // CLOCK_BOOTTIME since boot: deadlines, backoff, deadline_ms (LN2)
	Mono() time.Duration   // CLOCK_MONOTONIC: only for SY12's boot-minus-mono reading
	WallMS() int64         // wall milliseconds: anything compared with origin_server_ts
	Sleep(ctx context.Context, d time.Duration) error              // satisfies lock.Clock and matrix.Clock
	AfterFunc(d time.Duration, f func()) (stop func() bool)        // satisfies matrix.Timers
	Hold(d time.Duration) (<-chan time.Time, func() bool)          // for fakehs.Options.Hold
}
func System() Clock
type Fake struct{ /* unexported */ }
func NewFake(boot time.Duration, wallMS int64) *Fake
func (f *Fake) Advance(d time.Duration)        // moves Boot, Mono and Wall, and fires what is due
func (f *Fake) Suspend(d time.Duration)        // moves Boot and Wall but not Mono: SY12's suspend
func (f *Fake) StepWall(ms int64)              // a wall-clock step alone
func (f *Fake) Waiters() int                   // how many sleeps and timers are armed, so a test waits on that, never on time
```

`clock` must not import matrix or fakehs. Each interface is met structurally, and a compile-time
assertion in `clock`'s test file proves it (the test file may import both).

### failpoint (gap 9)

```go
// Hit is a named point. Without the busfailpoints tag it is an empty function the compiler
// removes. With the tag, a point named in TEAMCHAT_FAILPOINTS (comma list) SIGKILLs its own process.
func Hit(name Name)
type Name string
const (… n3c's names, below …)
```

n3c's points come from PT1's list, plus one for SY5 (n3c-D2):

`keeper-link-writing`, `link-in-remove`, `link-after-remove-answered`, `link-after-hand-out`,
`link-while-handed`, `keeper-before-store-delete`, `link-mid-end-sends`, `keeper-before-end`,
`link-after-judge-before-persist`.

The gate test: a release build (`-tags teamchatrelease`, no `busfailpoints`) of `./cmd/teamchat`
holds none of the names' strings (`go tool nm` or a strings scan of the binary). The build runs
through `execx` from the test, with its `dynamicNames` row.

### wire: the link socket and the control socket

The socket protocol (B 2.4.4; LN2's deadline table):

```go
const MaxRequest = 64 << 10
type Request struct {
	V          int             `json:"v"`
	Op         string          `json:"op"`
	Kit        string          `json:"kit"`
	DeadlineMS int64           `json:"deadline_ms"` // absolute CLOCK_BOOTTIME ms
	Args       json.RawMessage `json:"args"`        // one object, op-specific
	Stdin      *string         `json:"stdin"`
}
func DecodeRequest(line []byte) (Request, error)  // strict: proto.Parse, then DisallowUnknownFields; over MaxRequest is a *exits.Error exit 10 code size
type Done struct { Exit int; Error *exits.Error; Ack string } // {"done":{"exit":N[,"error":{...}][,"ack":"<8 hex>"]}}
func (d Done) Encode() []byte
func DecodeDone(line []byte) (Done, error)
type Ack struct{ Ack string } // {"ack":"<8 hex>"}
const ( OpRecv = "recv"; OpWait = "wait"; OpInbox = "inbox"; OpStatus = "status"; OpVerify = "verify"; OpHistory = "history"
        // reserved for n3d/n3e, named so the table is whole:
        OpReply = "reply"; OpSay = "say"; OpPing = "ping"
        OpHookSessionStart = "hook.session-start"; OpHookPrompt = "hook.prompt"; OpHookStop = "hook.stop"; OpHookSessionEnd = "hook.session-end" )
// Deadline is LN2's table: hooks 300 ms; recv, inbox, status 2 s; sends and verify 30 s (n3c-D4);
// history and questions 60 s; wait its own timeout.
func Deadline(op string, waitTimeout time.Duration) time.Duration
// Args for n3c's ops:
type WaitArgs struct{ TimeoutS int64 `json:"timeout_s"` }
type VerifyArgs struct{ ID string `json:"id"` }
type HistoryArgs struct{ Team *paths.TeamID; Room string; Limit int }
type TeamsArgs struct{ Team *paths.TeamID } // recv, inbox, status
```

The control protocol (B 2.4.2, 2.5, 2.10). The wire format is a decision (n3c-D3): an
`AF_UNIX` `SOCK_SEQPACKET` socketpair, so message boundaries survive and `SCM_RIGHTS` travels
with its message. Each message is one strict JSON object of at most 8 KiB. An `add` carries
exactly one descriptor; every other message carries none, and a message with the wrong count is
refused, which closes the link.

```go
func Pair() (keeper, link *os.File, err error)   // socketpair(AF_UNIX, SOCK_SEQPACKET|SOCK_CLOEXEC); the keeper clears CLOEXEC on the link end only through execx.Cmd.ExtraFiles (fd 3)
type Conn struct{ /* unexported */ }
func NewConn(f *os.File) (*Conn, error)          // refuses a file that is not an AF_UNIX SOCK_SEQPACKET socket (the link's "fd 3 is the keeper's control socket", B 2.2.1)
func (c *Conn) Send(m Msg, fd *os.File) error
func (c *Conn) Recv() (Msg, *os.File, error)     // io.EOF on a closed peer: the keeper's death (LN1)
func (c *Conn) PeerPID() (int, error)            // SO_PEERCRED; the link checks it is getppid()
type Msg struct { Kind string; /* one pointer field per kind */ }
// keeper -> link
//   launch  {run, kind, launch, inbound, generation, claude_config_dir, cwd}   first, once
//   add     {team, seat, removed_user_ids[]} + the seat lock fd   (minor b: SY1 (c) only for a user id in the list)
//   remove  {team, farewell: released|removed|parked|null, txn}   txn drawn by the keeper (LN8)
//   owed    {team, txn}                                            a leave notice still owed, to a restarted link
//   pid     {pid, start_ticks}                                     CMD's (RN3)
//   claude  {pid, start_ticks}                                     Claude Code's, when the keeper has it from a previous link
//   token   {token}                                                the inbox socket token (secret: Format never prints it)
//   desired {generation, state: applied|refused, code}             RC1's refusal, to show in status and one hook line
//   end     {how: launch-failed|stop|restart|exit|killed, status}  KP3; status is CMD's or Claude Code's per B 2.4.2
// link -> keeper
//   ready   {}                                                     LN1: every store loaded, link.sock listening
//   state   {team, link, last_sync_ok_ms, pending, delivery, store, session_started} for report.json; session_started is n3c-D6
//   removed {team, user_id}                                        the answer to remove (LN8)
//   left    {team}                                                 the owed notice posted, or its 30 s passed (keeper closes its seat fd)
//   claude  {pid, start_ticks}                                     the first socket fact's pid (B 2.4.2)
//   token   {token}
// keeper -> link, also
//   refused {team, holder_run}                                     RC1: an add refused because the seat is held; the link shows it in status and a NOTICE
```

### team: the seams between the engine (P4), the sync engine (P5) and the ends (P6)

The engine is P4's state goroutine. It makes no network call (B 2.4.3). The team goroutine is
P5's. It owns every homeserver request of its team. The ends are P6's, and run on a team
goroutine's client.

```go
type Batch struct {           // one judged sync answer, after SY6's check (SY5)
	Team         paths.TeamID
	NextBatch    string
	RestoreEpoch int64
	Fresh        bool                 // false once SY1's step 01 has persisted
	Items        []Item
	Floors       map[string]int64     // room -> floor_ms moved by this batch
	Answered     []proto.Answered     // n3d fills; n3c passes nil
	Redacts      []string             // admin redactions of pending or handed items
	Gap          bool                 // SY3's bound was hit
	Counts       Counts
}
type Item struct { Pending state.Pending; Verdict proto.Verdict } // content held in memory (B 2.7.3 "Content")
type Counts struct { Ignored, Dropped, Unheard, Gaps, Redacted int64 }
type View struct {            // what Judge's Lookup reads, from memory (gap 14)
	Seen     map[string]bool            // pending or ledger
	Related  map[string]proto.Related   // ledger and outbox entries, filled by SY4a's rule
	OwnPings map[string]proto.OwnPing
}
type Health struct { Down bool; Stopped string /* "", exits.CodeToken, CodeUntrusted, CodeStore… */; Role string }
var ErrRemoved = errors.New("team: the team was removed")
type Engine interface {
	Load(t paths.TeamID) (state.Store, error)                       // the store as loaded or created (a copy)
	View(t paths.TeamID, ids, relations []string, r proto.Roster) (View, error) // r: an outbox entry's AddressedToRosterAgent
	Persist(ctx context.Context, b Batch) error                     // one store write, then wake delivery; ErrRemoved after a remove
	ResetPosition(ctx context.Context, t paths.TeamID, epoch int64) error // SY6: drop next_batch, store the new epoch, fresh stays as it is
	SetEnded(ctx context.Context, t paths.TeamID, e state.RunEnd) error   // EN1, EN3
	SetHealth(t paths.TeamID, h Health)
}
type Team interface {         // one team goroutine, as the engine and the link see it
	Run(ctx context.Context) error                                       // until ctx ends or a permanent stop
	Refetch(ctx context.Context, room, id string) (proto.Verdict, error) // a restored item, judged again (B 2.7.3)
	Verify(ctx context.Context, id string) (records.Verified, error)     // LN6
	History(ctx context.Context, room string, limit int) ([]records.History, error)
	Notices() Notices                                                    // for the ends
	Wake()                                                               // a bundle or generation change: retry a stopped team (SY8)
}
type Notices interface {      // the matrix calls the ends make, on the team's client
	notice.Client            // RoomState, Messages, Send (built)
	SetPresence(ctx context.Context, userID, presence string) error
	Anchors() proto.Anchors
	Roster() (proto.Roster, bool)
	RecordOwnSend(id string, r proto.Related)  // minor f: the resolver's own-sends map
}
type Ends interface {
	Close(ctx context.Context, t paths.TeamID, n Notices, prev state.StoreRun) error // EN3
	Back(ctx context.Context, t paths.TeamID, n Notices, run state.StoreRun) error   // EN4
	End(ctx context.Context, how End, teams []EndTeam) []error                      // EN1, in parallel
	Leave(ctx context.Context, t paths.TeamID, n Notices, txn string) error          // LN8's left released
}
type End struct { How string; SessionEndReason *string; Kind string } // How: wire's end kinds, or "keeper-died" (LN1)
type EndTeam struct { Team paths.TeamID; Notices Notices; Run state.StoreRun }
```

`team` imports `notice` for `notice.Client`, so `notice` joins the graph above.

### Additions to built packages

- `member/state`: `func (w *StoreWriter) Remove() error` unlinks `store.json` and only that
  (LN8, and the keeper's deletion after a timed-out removal). It refuses once the lock is
  closed (`ErrNoLock`), and syncs the directory. The built `StoreWriter` has `Load`, `Write` and
  `Close` only.
- `member/lock`: `func Probe(root *homefs.Root, full string) (held bool, err error)` takes a
  momentary `LOCK_SH|LOCK_NB`, then closes. It is for LN3's `keeper.lock` probe, and for n3e's
  status line on `link.lock`. A missing file is `held=false` with `fs.ErrNotExist`.
- `member/bundle`: `func ReadPairAt(root *homefs.Root, dir string, team paths.TeamID) (Pair, error)`
  reads the same pair below an arbitrary seat directory: EN5's host-held `--bundle DIR`, which
  is no `paths.Seat`. `ReadPair` becomes a call of it.
- `paths`: `func (l Layout) WithState(dir string) (Layout, error)` replaces `<v2-state>`, and so
  `<v2-bundle>` (RN1, `--state DIR`). The directory must be absolute and clean. Its owner and its
  0700 mode are checked when it is opened (homefs `Private`), so they are not checked here.
  Register it in `sampleNames` and `zeroDerivations`.
- `member/cli`: `type RunFunc func(Env, Invocation) term.ExitCode`. `Env` gains
  `Getenv func(string) string` and `Verbs map[string]RunFunc`, and `Run` looks in `e.Verbs`
  before reporting a verb unbuilt. `cmd/teamchat/main.go` builds the map, so `cli` never imports
  a part (n3c-D5). With no map, every n3c verb is still unbuilt.

### Tests

- `clock.Fake` drives `lock`'s three tries, and a `matrix.Client` deadline through `AfterFunc`.
- `wire`:
  - each message round-trips;
  - an `add` without a descriptor, and a `token` with one, are refused;
  - a 64 KiB + 1 request is exit 10;
  - `Format` of a `token` message shows no token;
  - `NewConn` on a `SOCK_STREAM` socket and on a regular file is refused.
- `StoreWriter.Remove` leaves `seat.lock` the same inode.
- `Probe` against a reexec holder of `LOCK_EX` reports held, and against a freed lock reports
  not held.

## 4. P3: the keeper and `run` (RN1-RN3, KP1-KP3, RC1, LN8's keeper half, RS2's `restart` end)

Packages:

- new `internal/teamchat/member/keeper`
- `internal/execx` (gap 7)
- one exported helper in `reexec`: `func Command(tb testing.TB, name string, args ...string) execx.Cmd`,
  so the keeper starts a fake link that is a real process

Imports: keeper -> clock, failpoint, wire, lock, state, homefs, paths, exits, execx, cli
(`Launch`), `x/sys/unix`. The keeper imports no matrix, no proto, no `member/bundle` and no
`member/team` (B14: no network call; B11: only the link reads a token). A source-scan test holds
that.

### API

```go
// execx (gap 7)
type Started interface {
	Pid() int
	PidFD() int                       // pidfd_open'd at start (Linux 5.3)
	Wait() (ExitStatus, error)        // wait4 once; ExitStatus{Code int; Signal syscall.Signal}
	Signal(sig syscall.Signal) error  // pidfd_send_signal
}
type Starter interface { Start(c Cmd) (Started, error) }   // Cmd gains Setsid bool (the link) beside NewSession
func (OS) Start(c Cmd) (Started, error)
func (f *Fake) OnStart(prefix []string, fn func(Cmd) (Started, error)) // scripted form
func (f *Fake) Start(c Cmd) (Started, error)

// keeper
type Config struct {
	Launch   cli.Launch
	Layout   paths.Layout          // after WithState for --state
	Env      func(string) string
	Clock    clock.Clock
	Starter  execx.Starter
	Runner   execx.Runner          // the kit's --version check
	Execer   execx.Execer          // RN1's re-exec into the kit
	LinkCmd  func(run paths.Run, kit paths.Kit) execx.Cmd // production: <kit>/bin/teamchat link --run <dir>; tests: reexec.Command
	Proc     Proc                  // /proc reads: start ticks (field 22), ppid; a fake in tests
	Signals  <-chan os.Signal      // signal.Notify'd by Main; a channel the test writes in tests
	Inotify  Watcher               // RC1; production on x/sys/unix inotify, fake in tests
}
func Run(ctx context.Context, cfg Config, stdout, stderr io.Writer) term.ExitCode // RN1-RN3, KP1-KP3
func Main(e cli.Env, inv cli.Invocation) term.ExitCode                            // the `run` verb; W puts it in cmd/teamchat's map
type Watcher interface { Watch(dir string) (<-chan WatchEvent, error); Close() error }
type WatchEvent struct { Name string; Op uint32; Overflow bool }
type Proc interface { StartTicks(pid int) (int64, error); PPid(pid int) (int, error); Alive(pid int, ticks int64) bool }
```

### Facts it needs, digested

- **`run`'s eight steps** (B 2.4.1):

  1. The runtime parent is ccy's private `/tmp`, or `$XDG_RUNTIME_DIR`. That must be set, owned
     by the uid and 0700, else 78 `runtime`. No v1 variable is read.

  2. The kit:

     - read `<v2-kit>/current` (a link) to `<kit>`;
     - run `<kit>/bin/teamchat --version` through `execx`, and check that it prints the digest;
     - when this binary is not the kit's, re-exec `<kit>/bin/teamchat run --kit <kit>` with the
       same arguments;
     - with `--kit`, never read `current` (T7);
     - prepend `<kit>/bin` to `PATH`, and set `TEAMCHAT_KIT=<kit>`;
     - ccy has one kit (`paths.CCYKitDir`) and no `current`.

  3. Leftover runtimes: for each sibling directory whose `keeper.lock` can be taken and whose
     `session.pid` names a keeper and a session that are both gone, remove it under that lock
     (`homefs.RemoveTree`). A gone process is no such pid, or a different start time. A runtime
     whose Claude Code still lives stays (T8).

  4. Draw a run id: 16 hex from `crypto/rand`, then `paths.ParseRunID`. `Mkdir` `<v2-run>` 0700,
     refusing an existing path. `lock.TakeKeeper`. Write `session.pid` (`state.SessionPID`) with
     `session` null.

  5. Under `--claude`, write `<v2-run>/settings.json` (0600, `O_EXCL`): the kit's
     `settings.json` plus `crossSessionInbound`.

     - `crossSessionInbound` is the desired file's `inbound`, or `refuse` with no `--desired`.
       It is always written.
     - A missing or undecodable `desired.json` under `--desired` is 78 `membership` (n3-D16).
     - Append `--plugin-dir <kit>/plugin/teamchat --settings <v2-run>/settings.json` after
       CMD's last argument.
     - `--resume-recorded` reads `conversation.json` (strict, a hint). It appends
       `--resume <id>` only when `<config>/projects/<derived cwd dir>/<id>.jsonl` exists, and
       otherwise says why in `keeper.log`.
     - `--plain` gets nothing appended.

  6. Take every desired seat's lock through `KeeperLock.TakeSeat`. A seat that is held fails the
     launch with exit 75, naming the seat. Write the lock record with
     `state.WriteLockRecord(l, LockRecord{Run, StartedMS})`. With `--seat T/S` under `--state`,
     take exactly that seat.

     Start the link through `Starter`:

     - it runs `Setsid`, with stdin `/dev/null` and stdout and stderr appended to `link.log`;
     - `ExtraFiles` is `[link end of wire.Pair]`, so the control socket is fd 3;
     - send `launch`, then one `add` per held seat, with `lock.SeatLock.Dup()`;
     - wait up to 30 s for `ready`, else 78 `runtime` naming `link.log`;
     - with no seat at all the link is still started (T6);
     - CMD is never started beside a link that did not come up.

  7. Start CMD in the keeper's own process group: not `Setsid`, inherited stdio, and
     `TEAMCHAT_RUNTIME` set. Replace `session.pid` with `session` set, and send `pid`. Under
     `--claude`, a CMD that cannot start, or that ends before the link has seen the first
     SessionStart, is `launch-failed`:

     - the lock record is rewritten with `launch_failed_ms` (LK4);
     - no notice is posted;
     - `keeper.log` and every `report.json` say it, with CMD's status and its first stderr line;
     - `run` exits 78 `launch`.

     The keeper learns that the first SessionStart happened from the `state` message's
     `session_started: true` (n3c-D6).

  8. From step 7 on, nothing reaches the terminal. Every diagnostic goes to `keeper.log` (B47).

- **The loop** (B 2.4.2) has seven sources: CMD's exit (`Wait`); `SIGTERM` and `SIGHUP`;
  Claude Code's pidfd; `SIGINT` and `SIGQUIT`, which are ignored; inotify on `<v2-desired>`;
  the link's exit; and the control socket. At start, `debug.SetGCPercent(-1)` and
  `debug.SetMemoryLimit(16<<20)`. No goroutine polls.

- **A stop.** It is forwarded once: to Claude Code by `pidfd_send_signal` when the keeper has its
  pid (from a `claude` message, after re-checking the start time), else to CMD. 5 s after the
  forward, whatever of the two still runs gets `SIGTERM`, and at 7 s `SIGKILL`.

- **Without a stop.** When one of CMD and Claude Code ends, the keeper waits 3 s for the other,
  then sends it `SIGTERM`, and `SIGKILL` 2 s later.

- **The end** waits until both have gone. It is decided as follows:

  - `launch-failed`;
  - else `stop`, if a stop was forwarded;
  - else `restart`, for `--kind ccy` with CMD's exit 75 and no stop;
  - else `exit` (a status) or `killed` (a signal the keeper did not forward).

  The keeper sends `end` and waits 5 s for the link to exit, then `SIGKILL`. Then it closes the
  seat locks, removes `<v2-run>`, and exits with CMD's status, or 128+n for a signal (KP3). If
  the link is restarting at that moment, it is started once more, with only the seats and the
  `end`. RS2's switch is in the link (P6): the keeper always reports `restart` for that case.

- **Supervising the link** (B48). It is restarted after 1, 2, 4, 8 and 16 s, and a run of 10
  minutes resets the count. The 6th exit, or one exit 78, makes it `failed`:

  - `keeper.json` and every `report.json` say `failed`, with its `why` (`crash-loop`, `config`);
  - the keeper closes every seat descriptor it holds (T23);
  - desired changes are no longer applied.

  A kill the keeper made on purpose (LN8) is not counted. A new link is passed every held seat,
  every owed leave notice (`owed`), CMD's pid, Claude Code's pid if known, and the token.

- **RC1** (B 2.10).

  - Watch `<v2-desired>` with `IN_CLOSE_WRITE|IN_MOVED_TO|IN_CREATE|IN_DELETE`. An overflow
    re-reads the file. A change is applied within 2 s.
  - A file that does not decode leaves the last good membership in force, and is reported:
    `desired.state` `refused` with its `code` in `report.json` and `keeper.json`, and
    `desired{…}` to the link, which puts it in `status` and one hook line.
  - A deleted file is the same refusal with `code` `missing` (n3-D19). A file written again
    clears it.
  - **Add:** `TakeSeat`. If the seat is held, `report.json` and a NOTICE (through the link) carry
    the holder's run id, read from the lock file with `state.DecodeLockRecord`.
  - **Remove:** send `remove` with a `txn` the keeper draws itself (`tc` and 32 hex from
    `crypto/rand`, n3c-D14), with no matrix import.
  - **Remove, answered within 10 s:** the `removed` answer carries the user id. The keeper
    remembers `(team, user_id)` as removed for the session's life (minor b). It keeps its own seat
    descriptor until the link's `left` comes, or 30 s pass.
  - **Remove, not answered within 10 s:**
    - `SIGKILL` the link and wait for it;
    - `failpoint.Hit(keeper-before-store-delete)`;
    - `state.NewStoreWriter(root, seatLock).Remove()`;
    - restart the link at once, uncounted, with the owed entry.
  - A session launched `refuse` that is given a seat writes `needs_relaunch: true` in that seat's
    `report.json`.

- **Files.**

  - `keeper.json` (`state.Keeper`) and `report.json` (`state.Report`) are written by the keeper
    alone, through `state.Write`.
  - Recovery texts are chosen by `kind` (B 2.4.2's table); P6 owns the template text, and the
    keeper writes only `link` and `why`.
  - The lock record is written at take, and again at a launch failure.

- **B15.** No teamchat package calls `PR_SET_PDEATHSIG` or `runtime.LockOSThread`: a source scan
  over `internal/teamchat` and `cmd/teamchat`.

### Seams and tests

The fake link is a reexec helper. It speaks `wire.Conn` and is scripted by its arguments:
ready at once, never ready, exit N, ignore `remove`, or answer `remove`. CMD is a reexec helper,
or `/bin/sh -c`, through the real `execx.OS`. The clock is `clock.Fake`. Signals are a test
channel. The inotify fake writes events. `Proc` is a fake for start times.

Tests, each written first:

- RN1:
  - every row of step 1 (unset, wrong owner, wrong mode);
  - `--kit` never reads `current`, which the test proves by making `current` point elsewhere;
  - the re-exec through `Execer`;
  - T8: a leftover is removed only when keeper and session are both gone;
  - B47: stdout and stderr on a pipe receive nothing after CMD starts.
- RN2: a held seat is 75; a link never ready is 78 naming `link.log`; no CMD starts.
- RN3: `launch-failed`, and the lock record gains `launch_failed_ms`.
- KP1: T21's wrapper cases; the 5 s and 7 s escalation on the fake clock; `SIGINT` ignored.
- KP2: 1, 2, 4, 8, 16 s; the 6th exit is `failed` and the seats are freed (a reexec probe takes
  the seat afterwards); one exit 78 is `failed` at once; the 10-minute reset.
- KP3: each `end` kind; the 5 s `SIGKILL`; locks closed only after the link exits; `<v2-run>`
  removed.
- RC1: every row above, the overflow included.
- LN8's keeper half: no answer in 10 s gives a kill, the store deleted under the lock, and a
  restart that does not count; the seat stays held until `left` or 30 s.
- The failpoints `keeper-link-writing`, `keeper-before-store-delete` and `keeper-before-end`,
  each in a real-process test under `busfailpoints`.

## 5. P4: the link and the engine (LN1, LN2, LN4, LN5, LN7, LN8's link half; P11)

Packages:

- new `internal/teamchat/member/link`: the process, the control client, the socket server,
  the peer rules, `session.json`, the conversation record and `status.json`
- new `internal/teamchat/member/engine`: the state goroutine, with its stores, item states and
  op bodies

Imports:

- link -> engine, wire, team, lock, state, homefs, paths, exits, records, clock, failpoint,
  `x/sys/unix`
- engine -> team, state, lock, homefs, paths, proto, records, clock, failpoint

Neither imports matrix. Network work goes through `team.Team`, which a fake implements here.

### API

```go
// engine
type Config struct { Root *homefs.Root; Layout paths.Layout; Clock clock.Clock; Log io.Writer; NewTeam func(TeamConfig) (team.Team, error); Ends team.Ends }
type TeamConfig struct { Team paths.TeamID; Seat paths.Seat; Lock *lock.SeatLock; RemovedUserIDs []string; Engine team.Engine; Clock clock.Clock; Log io.Writer }
func New(cfg Config) (*Engine, error)
func (e *Engine) Run(ctx context.Context) error            // the state goroutine
func (e *Engine) Add(ctx context.Context, seat paths.Seat, l *lock.SeatLock, removedUserIDs []string) error
func (e *Engine) Remove(ctx context.Context, t paths.TeamID) (userID string, err error)
func (e *Engine) Serve(ctx context.Context, req wire.Request, out RecordWriter) wire.Done // n3c's ops; others answer "unbuilt" 1 internal
// engine implements team.Engine
type RecordWriter interface { Record(records.Record) error; Flush() error }

// link
type Config struct { Run paths.Run; Layout paths.Layout; Control *wire.Conn; Clock clock.Clock; Proc Proc; Engine EngineFactory; Kit paths.Digest }
func Run(ctx context.Context, cfg Config) term.ExitCode
func Main(e cli.Env, inv cli.Invocation) term.ExitCode     // the `link` verb: refuses unless fd 3 is a SOCK_SEQPACKET socket whose peer is getppid()
type Proc interface { StartTicks(pid int) (int64, error); PPid(pid int) (int, error) }
```

### Facts it needs, digested

- **Start** (B 2.4.3, LN1):

  1. `lock.TakeLink`; if it is held, exit 78.
  2. Open a pidfd on the parent, then check that `getppid()` is still the keeper.
  3. Read `session.json` back.
  4. Receive `launch`, then each `add` (`lock.AdoptSeat` of the descriptor), and load every store.
     A store that does not decode is a per-team failure (`store` `refused`), never the link's
     exit.
  5. Return each `handed` item to `pending`, marked `repeat` and `restored`.
  6. Holding `link.lock`, unlink a stale `link.sock` (a socket is not a lock file), bind it 0600,
     listen, and send `ready`.

  A connect before step 6 is refused by the kernel. Ignore `SIGTERM`, `SIGINT` and `SIGHUP`.
  When the control socket's `Recv` gives EOF (the keeper died):

  - post `disconnected` for every held seat through `team.Ends.End(how: "keeper-died")`;
  - record `run.ended`, set presence offline, persist, and exit;
  - all of it within 5 s (T20).

- **The socket** (B 2.4.4, LN2):

  - `SO_PEERCRED` uid must be ours, else close;
  - one request per connection, one line, at most 64 KiB (exit 10 `size`);
  - a `kit` that is not the link's digest is 78 (code `runtime`, "kit changed under the
    session");
  - a request whose `deadline_ms` is past when it would start is discarded unacted, and the
    connection closed (T4, P7).
  - The answer is record lines exactly as the verb prints them, then `done`.

- **recv, wait, inbox** (LN4, B 2.7.3):

  - the caps are 8 entries and 12 KiB of records, then a `more` record;
  - choosing items: one store write per involved store sets each item's `handed` to the recv
    id, persisted before any record is sent
    (`failpoint.Hit(link-after-hand-out)` sits between);
  - `done` carries an `ack` of 8 hex, and the items are delivered only on the CLI's `{"ack"}`:
    one write moves them out of `pending` into the ledger;
  - no `ack` in 10 s, a closed connection or a failed write returns them to `pending`, marked
    `repeat` (one write);
  - a handed item is taken by nothing else: not the delivery goroutine (n3e), not another `recv`;
  - an `ack` for a removed team is ignored and recreates no store (T2, P11).

  `wait` blocks until something is pending (its own timeout), then acts as `recv`. `inbox` lists
  without bodies and consumes nothing. Item records are `records.Human`, `Peer`, `Ping`,
  `Timeout` and `Notice`, built from the in-memory `team.Item` verdict. A `restored` item has
  only its id, so it is refetched through `team.Team.Refetch` and judged again; one that no
  longer passes is dropped with its reason (B 2.7.3 "Content"). Floors: an event whose served
  `origin_server_ts` is not newer than its room's floor is dropped after the refetch (B44).

- **status** (LN5): `team`, `member`, `room` and `runtime` records, plus `outbox` (empty until
  n3d).

  - `runtime` is read from `/proc` of the link and its parent keeper (B40: `records.KeeperCost`,
    `records.LinkCost`).
  - The per-team counts are ignored, dropped, `unheard`, gaps, redacted, and `seen twice`, which
    stays 0 until n3e.
  - `status.json` (`state.Status`) is written on each change. Its pacing (at most once a second,
    at least once a minute) is n3e's SL1.

- **Session facts** (LN7, B 2.4.4). Hook ops are served only once `pid` has come, and only to a
  peer whose `/proc` ppid chain reaches `session.pid`'s session pid and start time, at most 64
  steps.

  - The inbox socket path is accepted only as `paths.InboxSocket(runtimeDir, pid)`, for a pid on
    that chain at or below the session's.
  - The transcript is accepted only as `<claude_config_dir>/projects/<derived>/<session_id>.jsonl`,
    with no link followed. `<claude_config_dir>` is `$CLAUDE_CONFIG_DIR` from the launch, else
    `~/.claude`. `<derived>` is derived from `run`'s cwd.
  - A new session id is accepted only on `session-start` with `clear` or `resume`, or on the
    first op a new link serves.
  - The first socket path fixes Claude Code's pid. Send `claude` to the keeper, and refuse
    another pid with the fixed line ("teamchat serves one Claude Code per run; this session's
    teams ended with the first").
  - Each fact is written to `session.json` before the answer.
  - The conversation record (`state.Conversation`) is written whenever the session id changes,
    with teams or none.

  The four hook ops' bodies are n3e's. P4 builds the peer check, the path rules and the
  `session.json` writes as one function n3e calls (`link.AcceptFacts`), tested here with a
  stub op.

- **Removal, the link's half** (LN8, B 2.10):

  1. Stop the team goroutine, cancelling its request.
  2. Drop its pending, handed and awaiting items (one log line).
  3. `StoreWriter.Remove`; close its seat descriptor.
  4. Answer `removed{team, user_id}`.

  Then, from memory and within 30 s:

  - presence offline;
  - `team.Ends.Leave` when the farewell is `released`;
  - nothing for `removed` or `parked`;
  - `left` to the keeper.

  An `owed` message from the keeper posts the leave once, by its txn, with no seat held.

  Failpoints: `link-in-remove`, `link-after-remove-answered`, `link-while-handed`.

- **The memory-only `Lookup` half** (gap 14, SY4a). `View` fills `Related` from the stores.

  - A ledger entry is an item delivered to this member: `Sender` is its `from`, `RoomID` is its
    `room`, and both addressing flags are true.
  - An outbox ping has this member as `Sender`, its `verb`, `AddressedToSelf` false, and
    `AddressedToRosterAgent` true when `to` names a roster agent (the roster is given by the
    team in the call).
  - `Verb` is empty where the record holds none.
  - `Seen` is pending or ledger. `OwnPings` comes from `outbox.pings`.

- **A refuse launch** that is given a team runs it in `wait` mode: `delivery` `wait` in `status`
  and in `state`.

### Seams and tests

- A fake `team.Team` scripted per test: batches, `Refetch` answers, verify and history.
- A fake `team.Ends` that records its calls.
- `clock.Fake`; the CLI side of the socket as a byte-level client in the test.
- Real-process tests through reexec: the link as a child of a test "keeper" holding `wire.Pair`.
  They cover LN1's ignored signals, EOF giving the keeper-died path within 5 s, and P11's
  `SIGKILL` while handed (restarted, the items `repeat`).

Tests:

- LN2's every refusal, and a deadline 1 ms past being discarded.
- LN4's caps at 8 and 9 items, and at 12 KiB exactly and one byte over.
- An ack lost, late (10 s on the fake clock), on a closed connection, or for a removed team.
- LN7's chain at 64 steps and at 65; a second Claude Code pid refused; a forged id swap refused.
- LN8's link half, with each farewell.

## 6. P5: the sync engine (SY1-SY14, SY4a, UH1, LN6's network bodies, LN9)

Package: new `internal/teamchat/member/sync` (named `teamsync` if `sync` shadows the standard
library in the package's own files; the builder picks and records it).

Imports: teamsync -> team, matrix, proto, state, bundle, homefs, paths, seat, records, exits,
clock, failpoint and `x/sys/unix` (the bundle watch). It does not import `notice` or `ends`: the
notices go through `team.Ends`.

### API

```go
type Config struct {
	Team    paths.TeamID
	Seat    paths.Seat
	Root    *homefs.Root
	Engine  team.Engine
	Ends    team.Ends
	Clock   clock.Clock               // also passed to matrix.Config.Clock and .Timers
	Watcher Watcher                   // inotify on the seat dir; fake in tests
	RemovedUserIDs []string           // minor b
	Plain   bool                      // EN4's "at once for --plain"
	FirstSessionStart <-chan struct{} // closed by the link at the first SessionStart
	Log     io.Writer
	HTTP    func(matrix.Config) (*matrix.Client, error) // matrix.NewMember; tests point it at fakehs
}
func New(cfg Config) (team.Team, error)
```

### Facts it needs, digested

- **SY1, the start** (B 2.6 step 01). Read the pair with `bundle.ReadPair`; `ErrTorn` waits for
  the next bundle event (LN9). Build the client with `matrix.NewMember`. Verify main with
  `proto.VerifyMain(RoomState)`. Then:

  - **(a) A store with `fresh: false`.** Compare its `sync.restore_epoch` with the roster's
    before the first sync. Equal: sync from `next_batch`. Not equal: case (b).
  - **A store with `fresh: true`.** Run the step again, (b) or (c) as it was created, never
    from an unpersisted position. A crash between creation and the first persist restarts here.
  - **(b) No store.** Start from the earlier of the `teamchat.floor` marker and the bundle's
    `Held.ReleasedMS`.
    - The marker is read with `RoomAccountData(me, room, paths.FloorAccountData)`. Absent means
      404 `M_NOT_FOUND`, and then the start is the member's join event. An undecodable marker is
      none, and `link.log` and `status` say so.
    - A guest `Bundle` has no `released_ms`, so it starts from the marker alone.
    - Read `/messages` `dir=b` from the room's current position, under the page bound (100 per
      page, 20 pages), leaving out the oldest, and judge oldest first.
    - Items at or before `released_ms`, or at or before this member's own latest
      `left`/`disconnected`, are marked `restored`.
    - Persist with `fresh: false`.
  - **(c) A re-add.** Taken only when the pair's `user_id` is in `RemovedUserIDs` (minor b). The
    first sync uses `proto.SyncFilterReAdd` (timeline limit 0).

  After (a) or (b), before the first long-poll: `Ends.Close` with the store's previous `run`
  (EN3), then `Ends.Back` once the first SessionStart has come, or at once under `--plain`
  (EN4). Neither runs after `launch-failed`; P6's `Back` checks that.

- **SY2.** `Sync{Since, Timeout: 30 s, FilterJSON: proto.SyncFilter(r, a).Compact()}` with the
  default `set_presence`. The deadline is 45 s, per try (P1's `Timers`). One client per team,
  which keeps its connection alive.

- **SY3.** A limited timeline is filled backwards from `prev_batch`: 100 per page, at most 20
  pages, skipping ledger ids. Past the bound the oldest are left, the position moves on,
  `Batch.Gap` is set, and the count goes up.

- **SY4 and SY4a, resolve before judge** (gap 14):

  1. For each answer, collect `proto.RelationIDs` of its main-room human events.
  2. Resolve each id in order:
     - `Engine.View`, which covers the ledger and the outbox;
     - the own-sends map, which `Notices.RecordOwnSend` fills;
     - the LRU cache of 1024 entries;
     - one `GetEvent` under the team's request deadline.
  3. A fetched event fills `Related` from its own sender, mentions, `@room` and relations. Its
     own relations are not fetched in turn.
  4. A 404 or 403 is cached in a separate unknown set for the link's life: not addressed to this
     member, and not counted as unheard.
  5. A transient failure holds the whole answer before `Judge`, SY6 and SY5 run. It goes down
     SY8's transient path.
  6. Only then build the memory-only `proto.Lookup` from the `View` and the resolved map, and run
     `Judge` on each event.

  Verdicts:

  - accept: queued as a `team.Item`;
  - ignore: counted;
  - drop: counted, with one log line.

  Admission rules:

  - The flood budget is applied at admission (T19). `Judge.Flood` is the team's `*proto.Window`.
  - A `Redact` verdict goes into `Batch.Redacts`.
  - An answer to an own ping is n3d's: `Answered` stays nil.
  - Nothing but counters and log lines is written before SY5.

- **UH1.** On a `not-addressed` ignore with `proto.ReachedNoAgent` true, and on every ignore with
  `type` or `empty`, count `unheard` once per event id (minor e), with one `link.log` line naming
  the event id and the reason, never the body.

- **SY5** goes through `Engine.Persist(Batch)`, and only after SY6's check is equal.
  `failpoint.Hit(link-after-judge-before-persist)` sits just before it.

- **SY6.** `GetState(main, proto.RosterType, "")` is read after every answer before SY5, and
  before the first long-poll after a start, a failure, a stopped team's resume or a paused
  sync's resume.

  - Equal: persist.
  - Different, lower included: discard the answer, re-verify, `Engine.ResetPosition`, and resume
    as (b), or as (c) for a `fresh` re-added store.
  - A transient failure persists nothing and sends no long-poll.
  - A missing, undecodable or refused roster is a trust failure.

- **SY7.** The eight cases are SPEC-n3 SY7 1-8, on fakehs with P1's `Snapshot`/`RestoreTo` and
  `Hold`, all on one `clock.Fake`. Case 3's late drop times out on the fake clock through
  `Timers`.

- **SY8.**

  - Transient failures are unreachable, timeout, 5xx, and 429 past its retries
    (`(*matrix.Error).Transient()`). Retry from the stored position after 1 s, doubling to 30 s,
    with jitter (a seeded source in tests). The team is `down` after 15 s, with one log line at
    each end.
  - 401, 403 or lost trust stop the team: 21 or 22 for its verbs, one hook line, a NOTICE. The
    store and the owed TIMEOUTs are kept. A stopped team is retried on `Wake()` (a bundle or
    generation change) or at the next start (T10).
  - **The token race** (n3c-D12): on a 401, re-read the pair once. If it differs from the pair in
    use, retry with the new pair. Only a second 401 is the 21 `token` stop.

- **SY9.** Only the team sync marks the member online. Nothing ever sends
  `set_presence=offline`. Presence is set offline by `SetPresence` in the ends and in a removal.

- **SY10.** Put `teamchat.floor` `{"v":1,"floor_ms":N,"restore_epoch":N}` after a store write that
  moved a floor, at most once per room per 10 s (coalesced, on the clock), and at a clean end and
  a removal. A failed PUT blocks nothing. The ends need a `FlushFloor(ctx)`; add it to the
  concrete type, and W calls it.

- **SY11.** Invites in `SyncResponse.Invite` are judged by `proto.Invite(roomID, inviter, createSender, anchors, &roster)`: Join, leave pending (logged), or reject (`Leave`). A role
  change rebuilds the filter. Its NOTICE is DV6's (n3e).

- **SY12.** Drop the connection, re-verify, start a new long-poll and log one line with the pause
  in two cases: a deadline overdue by more than 10 s when the goroutine next runs, or more than
  2 s of growth in `Boot() - Mono()` since the last reading. The reading is taken at each
  request's start and answer. Tests at 10 s and 10 s + 1 ms, 2 s and 2 s + 1 ms, using
  `clock.Fake.Suspend`.

- **SY13.** A failed `Persist` (other than `ErrRemoved`) stops taking items, holds the position,
  is said once, and is retried with the transient backoff.

- **SY14.** Trust is re-verified at start, after a join, on a limited timeline, and on any event
  of a trust-bearing type (A's `proto.StateTypes`). A failure stops only that team, with 22.

- **LN6.** `Verify(id)`: `GetEvent`, judged by A's rules; the result is `records.Verified` with
  the full text and its SHA-256. `History(room, limit)`: `/messages` backwards, consuming
  nothing.

- **LN9.** Inotify on the seat directory, acting only on `seat.json` and `token` events.

  - On an event: re-read the pair, and retry a team stopped on a permanent failure.
  - A pair torn between GU3's two renames is read again on the second event, and never leaves the
    team stopped on 78.
  - The pool-seat hot reissue (H-C15) is tested on fakehs: the old token is revoked and the new
    token is placed by rename.

### Seams and tests

- fakehs (`Start`, `Inject`, `Restart`, P1's restore), with `clock.Fake` for both `Hold` and
  `Timers`.
- A fake `team.Engine` that records batches, and can fail `Persist` or answer `ErrRemoved`.
- A fake `team.Ends` that records calls.
- A watcher fake; homefs fixtures for the seat directory.

Every test named under SY1-SY14, SY4a and UH1 in SPEC-n3, plus gap 14's: a fetch error injected
for one relation id leaves the answer unpersisted, and `Judge` is never called for that batch.
Make `Judge` a seam that counts its calls. Plus:

- the poison bound (n3c-D9): after 5 failures the relation is unknown, with one log line, and
  the answer goes on;
- the token race: old `seat.json` with a new token, a 401, a re-read, success.

## 7. P6: the ends, the client verbs, `seat disconnected` and `reach` (EN1-EN5, LN3, RS1-RS2)

Packages:

- new `internal/teamchat/member/ends`: EN1-EN4 and the RS2 switch, implementing `team.Ends`
- new `internal/teamchat/member/client`: the CLI side of `link.sock`, and the verbs `recv`,
  `wait`, `inbox`, `status`, `verify`, `history`, `seat disconnected` and `reach`
- an exported reader in `notice`: the latest own lifecycle notice. `notice.Disconnected` already
  pages main backwards for it (`MaxPages`, `PageSize`) and keeps the logic unexported. Export
  `func LatestOwn(ctx, c Client, n Notice) (event string, found bool, err error)` and make
  `Disconnected` call it, so EN3 and EN4 do not copy it.

Imports:

- ends -> team, notice, proto, state, paths, failpoint, clock
- client -> wire, exits, records, state, lock, homefs, paths, bundle, seat, matrix (reach and
  seat disconnected only), notice, clock, cli

### API

```go
// ends
const restartSwitch = false // RS2: one constant; RS3's commit (Task 4.2) turns it on
func New(cfg Config) team.Ends // Config{Clock, Log, switchOverride *bool (test seam, unexported setter in export_test.go)}
// client
func Main(verb string) cli.RunFunc   // recv, wait, inbox, status, verify, history
func SeatDisconnected(e cli.Env, inv cli.Invocation) term.ExitCode
func Reach(e cli.Env, inv cli.Invocation) term.ExitCode
```

### Facts it needs, digested

- **EN1** (B 2.11's table). `End` decides from `how` and the SessionEnd reason:

  - `restart`, with the switch on: nothing is posted; record
    `run.ended = {"notice":"none","reason":"restart","state":"skipped","txn":null,"at_ms":N}`
    (n3-D2).
  - `restart`, with the switch off: decided as `exit` (RS1: `left exit` after a `/exit`).
  - A recorded reason `exit`, `logout` or `other`: `left` with that reason.
  - `stop` with no reason: `left other`.
  - `exit` with no reason, or `killed`: `disconnected`.
  - The keeper died: `disconnected`.
  - `launch-failed`: nothing, and no `back` later.

  For each notice:

  - record `ended` `attempted` with its txn (`tc` and 32 hex from `crypto/rand`, the store's
    grammar at `state/store.go:95`; not `matrix.NewTxnID`, which draws `pb`; n3c-D18), through
    `Engine.SetEnded`,
    before the send, and `sent` on the answer;
  - post to every team in parallel inside the keeper's 5 s;
  - then `SetPresence` offline.

  A seat whose roster role is `probe` posts nothing. A SessionEnd alone posts nothing. Bodies are
  built by `proto.Composer.Notice` (A's templates, grammar-checked names); the `to` list is
  humans and the coordinator, never the subject. The `notice` package's `addressees` is the
  model, and it is exported if needed. Each own send is recorded with `Notices.RecordOwnSend`
  (minor f). Failpoint: `link-mid-end-sends`.

- **EN3**, at a take, from the store's previous `run`:

  - `ended.state` `attempted`: re-send by its recorded txn.
  - `ended` null: `notice.Disconnected` with `Run` the ended run's id (`store.run.id`, not the
    lock file's new id) and `SinceMS` its `started_ms`. That posts `tc-disc-<ended run id>` only
    when the latest own lifecycle notice since then is neither `left` nor `disconnected` (T14
    with the host's call: one post).
  - `notice` `none` (`restart`): closed with no post and no `back`.

- **EN4.** Once per run, when `LatestOwn` is `disconnected`: post `back`.

- **Leave.** `left` with reason `released`, by the keeper's txn, within 30 s.

- **EN5.** `seat disconnected --bundle DIR --since MS --seed ID`:

  - `homefs.Open` the parent, then `bundle.ReadPairAt`;
  - `--seed` must parse as a `paths.RunID`;
  - `notice.Disconnected` with the bundle's anchors and token;
  - nothing else is read: no store, no container file;
  - stderr is buffered and written once the notice is settled, ignoring `EIO`, `EBADF` and
    `EPIPE`.

  Exits: 0 for posted, found, or not possible (the `Why`, as one stderr line); 20 for a transient
  failure; 78 `bundle` for a refused pair.

- **The client** (B 2.4.4, LN3):

  - `TEAMCHAT_RUNTIME` unset: 69 `no-session`.
  - Otherwise connect to `link.sock` and send one `wire.Request`, with `deadline_ms` from
    `wire.Deadline` on `clock.Boot()` and `kit` `TEAMCHAT_KIT`'s digest.
  - Copy record lines to stdout and the error to stderr, and exit with `done.exit`.
  - For `recv` and `wait`: flush stdout, then send `{"ack"}`. A failed stdout sends no ack.

- **LN3, no link.** `ENOENT`, `ECONNREFUSED`, or a close before `done`: read `keeper.json`
  (4 KiB, strict) and `lock.Probe` `keeper.lock`:

  - keeper held and `link` `restarting`: 69 `no-link`, with the restart time (`next_ms`);
  - `link` `failed`, or `keeper.lock` free: 69 `link-failed`, with the kind's recovery
    (B 2.4.2's table, by `keeper.json` `kind`, filled only with the grammar-checked session name).

  All of this within 300 ms. Only `wait` reconnects, 1 s doubling to 5 s, under its one deadline;
  no link for the whole timeout is 69 `no-link` at the deadline, and a failed link is 69
  `link-failed` at once.

- **`reach --team T`** (n3c-D16): see the decision.

### Seams and tests

- For the ends: a fake `team.Notices` over fakehs (`matrix.Client` against fakehs, which already
  satisfies `notice.Client`) and a fake `team.Engine` that records `SetEnded`.
- For the client: a fake link, a test listener on a temporary run directory speaking `wire`.
- For `seat disconnected` and `reach`: fakehs.

Tests:

- EN1's whole table, the switch in both positions through the test seam, and RS1's test pinning
  the shipped value off (`restartSwitch == false`).
- EN3's three states, and one post with the host's call for the same run.
- EN4, never after `launch-failed`.
- LN3's three answers under 300 ms on the fake clock.
- `wait` across a link restart, with the same deadline.
- EN5's dead-terminal case: a stderr writer that answers `EPIPE` gives exit 0.

## 8. W: wiring (after P3-P6)

- `cmd/teamchat/main.go` builds `cli.Env.Verbs`:

  - `run` -> `keeper.Main`;
  - `link` -> `link.Main`, with `engine.Config.NewTeam` = `teamsync.New` and `Ends` = `ends.New`;
  - the six client verbs, `seat disconnected` and `reach`.

  Remove n3c's names from `cli`'s `unbuilt` table. `TestUnbuiltEmpty` under `teamchatrelease`
  stays red until n3f; that is expected.

- End-to-end tests with real processes: the test binary as `teamchat` through reexec, fakehs
  over HTTP, and `run --plain --state <tmp> --seat T/S -- <helper>`.

  - A human's item and a peer's item reach `recv` and are acked.
  - A `SIGTERM` to the keeper posts `left other`.
  - A `SIGKILL` of CMD posts `disconnected`.
  - A `SIGKILL` of the keeper makes the link post `disconnected` within 5 s.
  - A hot add and a hot remove through the desired file.
  - Each n3c failpoint under `busfailpoints`, with P1-P11's checks that apply before n3e (P3, P4,
    P5, P6, P7, P11).

- The PLAN.md Task 3.2 progress line, in the same commit.

- The VM lab's LXC run follows n3b's merge (SPEC-n3 section 0). It is a host run: put it in
  `meta-deploy.list` only when n3b's play exists. It is not W's to start.

## 9. Decisions n3c takes (the spec is silent)

| ID      | Decision                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n3c-D1  | fakehs's `RestoreTo` raises `restore_epoch` by rewriting the roster state event's content generically (no proto import); a roster without an integer epoch is the test's fault, an error                                                                                                                                                                                                                                                                                                                                                                                          |
| n3c-D2  | a tenth n3c failpoint, `link-after-judge-before-persist`, beside PT1's list: P3's crash-before-write ("a crash before it re-fetches from the old position") needs a named point                                                                                                                                                                                                                                                                                                                                                                                                   |
| n3c-D3  | the control socket is `AF_UNIX` `SOCK_SEQPACKET` (boundaries kept, `SCM_RIGHTS` with its message), one strict JSON object of at most 8 KiB per message, exactly one descriptor on `add` and none elsewhere; B 2.4.2 names the messages but no format                                                                                                                                                                                                                                                                                                                              |
| n3c-D4  | `verify`'s deadline is 30 s, as a send's (it is one network request); B 2.4.4's table does not list it                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| n3c-D5  | the verb functions are passed to `cli.Run` in `Env.Verbs` by `cmd/teamchat`, so `cli` never imports keeper, link or client (keeper imports `cli.Launch`; a cycle otherwise)                                                                                                                                                                                                                                                                                                                                                                                                       |
| n3c-D6  | the keeper learns "the first SessionStart reached the link" from a `state` message field `session_started: true` (B 2.4.1 step 7 needs it and names no message); `--plain` has none, and RN3's launch failure applies under `--claude` only                                                                                                                                                                                                                                                                                                                                       |
| n3c-D7  | `wait --timeout` defaults to 110 s (inside Claude Code's 120 s default Bash timeout) and is at most 3600 s; at its deadline with nothing pending it exits 0 with no records, as `recv` does (B 2.2.1 gives no default)                                                                                                                                                                                                                                                                                                                                                            |
| n3c-D8  | `history --limit` defaults to 20 and is at most 100 (one `/messages` page)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| n3c-D9  | a poison relation (round-3 minor d) is bounded: after 5 consecutive transient failures fetching the same related id, it is cached as an unknown sender for the link's life with one `link.log` line, and the answer goes on; 5 tries at 1, 2, 4, 8, 16 s is about 31 s, so a team is `down` for at most about that long on one bad event                                                                                                                                                                                                                                          |
| n3c-D10 | UH1's dedupe (minor e) is a set of counted event ids, 1024 entries, oldest evicted, the cache's size and lifetime; a discarded and re-fetched answer counts nothing twice                                                                                                                                                                                                                                                                                                                                                                                                         |
| n3c-D11 | own sends (minor f) are kept in memory only, per team, inside the resolver (bounded as the cache, 1024); a link restart loses them and the fetch backs them. This is the statement round 3 asked of n3-D20: the builder of P5 adds one sentence to n3-D20 in SPEC-n3 in the same commit                                                                                                                                                                                                                                                                                           |
| n3c-D12 | the token race: on a 401 the team goroutine re-reads the pair once (MS3's reader); a pair whose `seat.json` or token bytes differ from the pair in use is retried at once; only a 401 on an unchanged pair, or a second 401, stops the team with 21 `token`                                                                                                                                                                                                                                                                                                                       |
| n3c-D13 | the keeper's removal memory (minor b) is `(team, user_id)`, reported by the link's `removed`; an `add` carries the team's removed user ids and the link takes SY1 (c) only when the pair's `user_id` is among them, so a new identity (LX7's retired-and-replaced seat) resumes by SY1 (b)                                                                                                                                                                                                                                                                                        |
| n3c-D14 | the keeper draws `remove`'s transaction id itself, `tc` and 32 hex from `crypto/rand` (the store's txn grammar, n3c-D18), without importing matrix, so the source scan "the keeper imports no network package" stays simple                                                                                                                                                                                                                                                                                                                                                       |
| n3c-D15 | the link's refusal of a `kit` mismatch is 78 code `runtime`; an oversized request is 10 code `size`                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| n3c-D16 | `reach --team T` (B 2.2.1, ccy-teams' hot add): reads `<v2-bundle>` of T's `seat.json` alone (`homefs` `Read` and `seat.DecodeMemberBundle`; no token read), then `matrix.NewMember` with the bundle's `base_url` and `plain_http_hosts` (placeholder token, as `guest check`'s reach) and `Versions` within 3 s; prints nothing and exits 0, 20 `unreachable`, or 78 `bundle`. It is P6's                                                                                                                                                                                        |
| n3c-D17 | `seat disconnected`'s "not possible for good" outcomes (`notice.Outcome.Why`) exit 0 with one stderr line, since the host's pass must not retry them; a transient failure is 20, so the pass tries again next minute                                                                                                                                                                                                                                                                                                                                                              |
| n3c-D18 | every transaction id the link records in `run.ended` (the end's `left` and `disconnected`) is `tc` and 32 hex from `crypto/rand`, as `member/state` requires (`store.go:95`, `^tc[0-9a-f]{32}$`); `matrix.NewTxnID` (`pb`) is not used for them. Only EN3's closing of an unseen run uses `notice.TxnID` (`tc-disc-<ended run id>`), posted through `notice.Disconnected` and recorded nowhere. The host's call for the same run then finds the link's `disconnected` as the latest own lifecycle notice and posts nothing, so no double post depends on the two txns being equal |

## 10. Reports

Each part writes `subagent-reports/261022-n3c-p<N>.md` (and `-review`, `-review-2`, …) and puts
its PLAN.md Task 3.2 progress line in the same commit as its code. W writes `261022-n3c-wire.md`.
A builder that stops at its context budget writes `261022-n3c-p<N>-handoff.md` first.
