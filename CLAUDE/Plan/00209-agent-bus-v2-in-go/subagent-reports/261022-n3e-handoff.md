# n3e handoff: the slice split into thirteen parts (Plan 00209 N3, slice n3e)

**State of this brief.** Sections 0-6 are written in full. Sections 7-15 (parts n3e-5 to
n3e-13) give each part's packages, files, API and tests in a compact form; section 16 carries
B27 and the offline rule in full; section 17 lists the decisions; section 18 is the common rules
text. Review round 1 was FIX-FIRST (4 major, 6 medium, 9 low); section 15a answers every major
and medium and overrides the sections above it. Round 2 was FIX-FIRST (1 major, 3 medium, 4
low); section 15b answers those and overrides 15a. The two rounds allowed are spent, so 15b is
not re-reviewed: the lead decides whether a third look is needed.

Design only; no code is written here. n3e is SPEC-n3.md section 6 (DV1-DV6, HK1-HK3, SL1, PK1,
PK2, PT1), with B27 in full (Plan 00217's team colours and short member name), the owner's
offline rule (OF1), the two NOTICEs n3c left to n3e (n3c-D23), and gap 9's last failpoints. It is
too big for one builder, so it is split into thirteen parts, `n3e-1` to `n3e-13`. Each part is one
worktree, one branch (`feat/bus-v2-n3e-<n>`) and one builder, and merges into `feat/bus-v2-n3e`.

Base: `feat/bus-v2-n3e` at 91faebcf0. It is cut from `feat/bus-v2-n3c` with n3c's P1-P6 merged,
and holds `feat/bus-v2-n3`'s B27 pointer to Plan 00217. W (n3c's wiring) is being built in
parallel and is **not** in the base: every reference to it below is "as designed, not yet built".
n3d (the sends) is briefed in parallel on its own branch: every place n3e meets it is a named seam
with a fake (section 2.3).

Every API quoted as "built" is quoted from the part reports (`261022-n3c-p2.md` to `-p6.md`, the
n3a reports). Where a report lists a file but not its fields (`member/state`'s codecs, `proto`'s
renderer), the fields are quoted from `go doc` at 91faebcf0 and say so. Each signature a part adds
is that part's decision: a builder may change it only by recording the change, and its reason, in
its part report.

## 0. Order

| Wave | Parts                                                                                                     | Needs                         | Why                                                                                                                                                                      |
| ---- | --------------------------------------------------------------------------------------------------------- | ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1    | **n3e-3** (shared additions) ∥ n3e-1 (inbox) ∥ n3e-2 (proof) ∥ n3e-4 (hook verb)                          | the base                      | n3e-3 is the types and proto additions later parts compile against; n3e-1, -2 and -4 are new leaf packages with their fakes, and none imports another                    |
| 2    | n3e-5 (status line) ∥ n3e-6 (delivery) ∥ n3e-7 (room facts, receipts, status.json) ∥ n3e-8 (admin colour) | wave 1 merged                 | n3e-5 reads n3e-3's status.json fields and n3e-4's link-down reading; n3e-6 drives n3e-1's inbox; n3e-7 and n3e-8 are the member and admin halves of the colour override |
| 3    | n3e-9 (proof transitions) ∥ n3e-10 (hooks and membership in the link) ∥ n3e-11 (kit, skill, text test)    | wave 2 merged                 | n3e-9 and n3e-10 extend n3e-6's delivery state; n3e-11 runs n3e-4's and n3e-5's verbs from a kit directory                                                               |
| 4    | n3e-12 (wiring and end to end)                                                                            | wave 3 merged, **W merged**   | `hook` and `status-line` registered in `cmd/teamchat`; the real-process tests over W's `run` and `link`                                                                  |
| 5    | n3e-13 (PT1)                                                                                              | n3e-12 merged, **n3d merged** | PT1 interleaves sends and arms n3d's send failpoint (section 2.3); its fixed seeds join the Go gate                                                                      |

Engine and link are touched by several parts. File ownership keeps them apart (each part's
section names its files); a shared function (`statusRow`, `held`, the link's dispatch) gets one
line per part, in its own hunk.

Every part keeps SPEC-n3 section 1 (TB4 for member files, CK1's clocks, OF1), 90% coverage per
package, and `-race`. No test sleeps (`project_testclock`). `os/exec` only through `execx`
(`project_seam_exec`), with a `dynamicNames` row in `src-go/qa/rules/table.go` for any run-time
command name (`project_seam_commands`).

## 1. Where SPEC-n3 section 6's items land

| Item                                                                               | Part                                                                                       |
| ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| DV1 the inbox socket, F2 fakeinbox                                                 | n3e-1 (client and fake); n3e-6 (its use)                                                   |
| DV2 the envelope (proto.Delivery mapping, `unusable.render`, `not shown:`)         | n3e-6                                                                                      |
| DV3 the states and the choice of items (awaiting, pacing, caps, stall, failpoints) | n3e-6                                                                                      |
| DV4 transcript proof, F3 faketranscript                                            | n3e-2 (scanner, watcher, fake); n3e-9 (the transitions)                                    |
| DV5 read receipts                                                                  | n3e-9 (added at proof); n3e-7 (sent by the team goroutine)                                 |
| DV6 membership notices                                                             | n3e-3 (proto's farewell reasons); n3e-6 (the session-level send); n3e-10 (diff and `told`) |
| HK1 the hooks                                                                      | n3e-4 (the `hook` verb, the texts); n3e-10 (the link's answers)                            |
| HK2 Stop, HK3 SessionEnd                                                           | n3e-10                                                                                     |
| SL1 the status line                                                                | n3e-5 (the `status-line` verb); n3e-7 (`status.json`'s pacing and fields)                  |
| B27 colours and short member name (Plan 00217)                                     | n3e-3 (palette, codec, fields); n3e-7 (read); n3e-8 (written by the admin); n3e-5 (drawn)  |
| PK1 the kit's files in use, PK2 the skill and the text test                        | n3e-11                                                                                     |
| PT1 the interleaving harness                                                       | n3e-13                                                                                     |
| Gap 9: n3e's failpoints                                                            | n3e-3 (the names); n3e-6, n3e-9 (the `Hit`s)                                               |
| n3c-D23: the NOTICE of a stopped team (SY8) and of a refused add (RC1)             | n3e-6 (`stopped:`); n3e-10 (the hook lines); n3e-D13                                       |
| OF1, the owner's offline rule                                                      | every part; section 16                                                                     |
| `hook` and `status-line` off `cli`'s `unbuilt` table                               | n3e-12                                                                                     |

## 2. What n3e builds on

### 2.1 Built (quoted from the reports)

**clock** (`261022-n3c-p2.md`):

```go
type Clock interface {
	Boot() time.Duration; Mono() time.Duration; WallMS() int64
	Sleep(ctx context.Context, d time.Duration) error
	AfterFunc(d time.Duration, f func()) (stop func() bool)
	Hold(d time.Duration) (<-chan time.Time, func() bool)
}
func System() Clock
func NewFake(boot time.Duration, wallMS int64) *Fake // Advance, Suspend, StepWall, Waiters, BlockUntil(ctx, n)
```

**failpoint**: `type Name string`; `func Hit(name Name)`; empty without `busfailpoints`; with it,
a name in `TEAMCHAT_FAILPOINTS` SIGKILLs the process; an unknown name panics at its first `Hit`.
The gate test scans a `-tags teamchatrelease` build of `./cmd/teamchat` for every name.

**wire** (link socket): `Request{V, Op, Kit, DeadlineMS, Args json.RawMessage, Stdin *string}`,
`DecodeRequest`, `DecodeArgs(raw, out)` (strict; a refusal is 10 `schema`), `Done{Exit, Error, Ack}`, `Ack`, `Deadline(op, waitTimeout)` (hooks 300 ms). The hook ops are named:
`OpHookSessionStart = "hook.session-start"`, `OpHookPrompt = "hook.prompt"`,
`OpHookStop = "hook.stop"`, `OpHookSessionEnd = "hook.session-end"`. **wire** (control):
`KindToken` (`Token{Token}`, redacted by `String` and `GoString`), `KindDesired`
(`state.ReportDesired`), `KindRefused` (`Refused{Team, HolderRun}`), `KindStarted`,
`Launch{Run, Kind, Launch, Inbound, Generation, ClaudeConfigDir, Cwd, Seats}`,
`Remove{Team, Farewell *string, Txn}` (farewell `released|removed|parked|null`).

**team**:

```go
type Batch struct {
	Team paths.TeamID; NextBatch string; RestoreEpoch int64; Fresh bool; Items []Item
	Floors map[string]int64 // keyed by room purpose
	Answered []proto.Answered; Redacts []string; Gap bool; Counts Counts
}
type Item struct { Pending state.Pending; Verdict proto.Verdict }
type Counts struct { Ignored, Dropped, Unheard, Gaps, Redacted int64 }
type Health struct { Down bool; Stopped string; Role string; LastSyncOKMS int64 }
type Engine interface { Load; View; Persist; ResetPosition; SetEnded; BeginRun; SetHealth }
type Team interface { Run; Refetch; Verify; History; Notices; Wake; FlushFloor }
type Notices interface { notice.Client; SetPresence; Anchors; Roster; RecordOwnSend; Token }
```

**engine** (`261022-n3c-p4.md`):

```go
type Config struct {
	Root, RunRoot *homefs.Root
	Layout paths.Layout; Run paths.RunID; Clock clock.Clock; Log io.Writer
	NewTeam func(TeamConfig) (team.Team, error)
	NewEnds func(team.Engine) team.Ends
	Delivery string // socket | wait (refuse launch) | none (plain)
	Plain bool; FirstSessionStart <-chan struct{}
	OnState func(wire.State)                // on the state goroutine; must not block
	Runtime func() (records.Runtime, error) // for status
}
type TeamConfig struct { Team paths.TeamID; Seat paths.Seat; Lock *lock.SeatLock; RemovedUserIDs []string
	Engine team.Engine; Ends team.Ends; Root *homefs.Root; Plain bool; FirstSessionStart <-chan struct{}; Clock clock.Clock; Log io.Writer }
func New(cfg Config) (*Engine, error)
func (e *Engine) Run(ctx context.Context) error
func (e *Engine) Add(ctx, t, l *lock.SeatLock, removedUserIDs []string) error
func (e *Engine) Remove(ctx, t) (Removed, error)
func (e *Engine) Serve(ctx, req wire.Request, out RecordWriter) wire.Done
func (e *Engine) Ack(ctx, ack string) error; func (e *Engine) Return(ctx, ack string) error
func (e *Engine) Refuse(ctx, t, holder paths.RunID) error
func (e *Engine) SetDesired(ctx, d state.ReportDesired) error
// and team.Engine
```

How it works (`261022-n3c-p4-handoff.md`, "How the engine works"), the facts n3e leans on:

- `do(ctx, f)` runs f on the state goroutine. **The goroutine calls no team method and makes no
  network call**; `Refetch`, `Roster()` and the like run outside it.
- recv: plan the candidates on the goroutine (unhanded pending, oldest `sent_ms` first); off it,
  read the rosters and re-fetch the restored items among the first 8 (per team in parallel, a
  2 s `AfterFunc` bound, a team stopping at its first error); on it, judge again (drop a
  non-Deliver outcome, or an `origin_server_ts` at or under its room's `floor_ms`, with a log
  line), choose within the caps, set `Handed`, one write per touched store.
- the short `from` (`render.go` `namer`): an agent's `Short()` when unique in the roster, else the
  full handle; a human or the admin by localpart. Record fields map from the Verdict (Human.Body;
  Content.Text.Text, else Body; Content.Ping; Content.Notice; Carried); a TIMEOUT is built from
  its key `event~silent` and the outbox ping.
- `publish` (after every change, on the goroutine): `OnState` per changed team, and `status.json`
  rewritten (rows for the held teams, a refused add as `{down: true, delivery: none}`; `failed`
  mapped from `Health.Stopped`). "SL1's pacing is n3e's."
- `Persist` is one write; it wakes the waits. `status`'s `team` record carries `counts` "with
  `seen_twice` 0 until n3e".

**link** (`261022-n3c-p4.md`):

```go
type Wiring struct { NewTeam func(engine.TeamConfig) (team.Team, error); NewEnds func(team.Engine) team.Ends }
func Main(w Wiring) cli.RunFunc
func Run(ctx context.Context, cfg Config) term.ExitCode
type Config struct {
	StateRoot, RunRoot *homefs.Root
	Layout paths.Layout; Run paths.RunID; Control *wire.Conn; Clock clock.Clock; Proc Proc
	Parent func() (Parent, error)
	UID int; Kit paths.Digest
	RuntimeDir, Home string
	Log io.Writer
	Engine  func(engine.Config) (*engine.Engine, error)
	NewTeam func(engine.TeamConfig) (team.Team, error)
	NewEnds func(team.Engine) team.Ends
}
type Facts struct { Op string; Peer int; SessionID, Source, Transcript, Socket string }
type Session struct{ /* session.json's one writer */ }
func (s *Session) AcceptFacts(f Facts) error
func (s *Session) EndReason() *string
func (s *Session) SetClaude(p state.Process)
func (s *Session) SetSessionPID(p state.Process)
```

"AcceptFacts is built and tested, but no op calls it yet. The hook ops come in n3e, and they
reach it through the link's `session`. A first op that is not session-start records source
`startup`. A corrupt session.json is logged, and the link starts a new one." The link holds the
`token` in memory and gives it back to the keeper (B 2.4.2).

**teamsync** (`261022-n3c-p5.md`): `Config{Team, Seat, Root, Run, Engine, Ends, Clock, Watcher, RemovedUserIDs, Plain, FirstSessionStart, Log, HTTP, Jitter}`, `New(cfg) (*Team, error)`, `Run`,
`Wake`, `Notices`, `Roster`, `Verify`, `History`, `Refetch(ctx, room, id)` (passed the room
**purpose**), `FlushFloor`; `WatchSeat(s) (*SeatWatcher, error)` (inotify).

**client** (`261022-n3c-p6.md`): `Main(verb) cli.RunFunc`, `SeatDisconnected`, `Reach`. Its
link-down answer (LN3) is unexported: `noLink` reads `keeper.json` and probes `keeper.lock` and
gives `no-link` (restarting, with the restart time; or starting) or `link-failed` with the kind's
recovery (`ccy-teams restart <session>` for ccy and lxc, else "end this session and launch it
again"). TEAMCHAT_RUNTIME is resolved by the unexported `sessionOf` (the CCY layout, then
`HomeLayout($HOME, $XDG_RUNTIME_DIR)`; anything else is 69 `no-session`).

**notice**: `LatestOwn`, `Roster(ctx, c, a)` (RoomState plus VerifyMain), `Addressees`.

**kit** (n3a, `261022-n3a-cli.md`): `Assemble(into string, kit paths.Kit, binary io.Reader) error`;
`var Hooks = []Hook{{"SessionStart","session-start"}, {"UserPromptSubmit","user-prompt-submit"}, {"Stop","stop"}, {"SessionEnd","session-end"}}`; `const StatusLineRefresh = 25`. `plugin.json`
holds `name` and `description` only.

**cli** (n3a): `Parse` is exported "for 2.12's test of every agent-facing text (n3e)"; the hook
event is value-checked at parse time (a bad event is 64 before any verb code runs);
`type RunFunc func(Env, Invocation) term.ExitCode`; `Env{Args, Stdin, Stdout, Stderr, Digest, Getenv, Verbs}`; the `unbuilt` row `"n3e": {"hook", "status-line"}`. CL2's release test
(`-tags teamchatrelease`) is red until every row is gone.

**member/state** (fields from `go doc` at 91faebcf0; the codecs are `261022-n3a-codecs.md`'s):

```go
type Session struct { V int; Run paths.RunID; SessionID, Source, Transcript, Socket *string; Claude *Process
	EndReason *string; StopGuard StopGuard; Told Told; FrameTold bool; Unusable Unusable }      // MaxSession 8 KiB
type StopGuard struct { PendingDigest *string; FailureMS, NoWakerMS *int64 }
type Told struct { Generation int64; Roles map[paths.TeamID]string }
type Unusable struct { HookStdin, Render int64 }
type Status struct { V int; Run paths.RunID; WrittenMS int64; Desired string; UnusableHookStdin int64; Teams []StatusTeam } // MaxStatus 64 KiB
type StatusTeam struct { Team paths.TeamID; Role *string; Down bool; Failed *string; Delivery string; Pending, Missing int64 }
type Store struct { …; Pending []Pending; Awaiting []Awaiting; Missed []Missed; Ledger []Ledger; Receipts []Receipt; Outbox Outbox; Run StoreRun }
type Pending struct { ID, Room, Item string; SentMS int64; From string; Repeat, Held, Restored bool; Handed *string }
type Awaiting struct { Nonce string; IDs []string; Transcript TranscriptMark; AtMS int64; Written string; StopMS *int64 }
type TranscriptMark struct { Path string; Offset int64 }
type Missed struct { Nonce string; IDs []string; Offset, AtMS int64 }
type Receipt struct { ID, Room string }
const DeliverySocket, DeliveryWait, DeliveryNone = "socket", "wait", "none"
```

The codec rules n3e keeps (n3a decision 1): `socket` is `<abs>/cc-socks/<claude.pid>.sock`
(`paths.InboxSocket`), at most 107 bytes; `transcript` absolute, clean, `.jsonl`, at most 1024
bytes; `told.roles` at most 64 teams, each `coordinator`, `worker` or `probe`; `status.json`'s
`pending` at most 2048 and `missing` at most 16; `failed` null or
`unauthorized|forbidden|trust|bundle`.

**proto** (renderer; `261021-n1-proto.md` decisions 47-54, fields from `go doc`):

```go
type Delivery struct {
	Nonce, Team string; Entries []Entry; Answered []Answered; HeldBack []HeldBack; NotShown []string
	Gap, Stopped bool; Pending int; Summarised bool
}
func (d Delivery) Fit() (Delivery, error)    // the most entries within 8 and 12 KiB; the rest added to Pending; a single entry always fits
func (d Delivery) Render() (string, error)   // refuses > 8 entries or > 12 KiB (`size`), A 13.7's order (`order`)
type Entry struct {
	Kind, Team, Handle, From, Role, EventID string; SentMs int64; Re, Revises string; Questions bool
	Question *EntryQuestion; Verb, Ref, Trust, Branch, Silent string; Event, Subject, Reason string
	Carried *EntryCarried; Copy []string; Repeat, Restored bool; Body string
}
func (e Entry) Lines() ([]string, error)     // every slot full-matched first
type Membership struct { Left []MembershipLeft; Joined, Roles []MembershipTeam; Teams []string }
func (m Membership) Render(nonce string) (string, error)
type MembershipLeft struct{ Team, Reason string }   // one reason today: FarewellRemoved
type MembershipTeam struct{ Team, Handle, Role, Coordinator, Description string }
const FrameText = `teamchat deliveries: …`         // A 13.5; "B places it"
const MaxBodyBytes = 2048; MaxDeliveryEntries = 8; MaxDeliveryBytes = 12 * 1024; MaxEntryBytes = 9 * 1024
func WireBytes(text string) int; func EscapeASCII(text string) string
func (h Handle) Short() string                     // `<repo>.<seat>`
var StateTypes = []string{CreateType, PowerLevelsType, JoinRulesType, RosterType, LinkType, MemberType}
func Desired(server string, r Roster, ids RoomIDs, description string) []RoomSpec
```

A 13.7's order, which `Render` enforces: HUMAN, then PEER and PING, then TIMEOUT, then NOTICE;
oldest first by `SentMs` within HUMAN and within PEER and PING; TIMEOUT keeps B's due order.
A 14's corpus (`proto/testdata/envelope/*.txt`, `TestEnvelopeCorpus`) is already the renderer's
golden test (DV2): n3e adds goldens only for its own mapping. A 13.1: "A delivery with no entries
carries only trailing lines; one made only of trailing lines is sent at most once a minute."

### 2.2 W, as designed, not yet built (`261022-n3c-handoff.md` sections 8 and 8a)

`cmd/teamchat/main.go` builds `cli.Env.Verbs`: `run` -> `keeper.Main`; `link` ->
`link.Main(link.Wiring{NewTeam: <adapter from engine.TeamConfig to teamsync.Config>, NewEnds: <ends.New adapter>})`; the six client verbs, `seat disconnected` and `reach`; n3c's names off
`unbuilt`. End-to-end tests: the test binary as `teamchat` through reexec, fakehs over HTTP,
`run --plain --state <tmp> --seat T/S -- <helper>`. n3e-12 starts from W as merged; where W's
adapter differs from this, n3e-12 follows W's report. n3e-7 adds `Receipts` to the adapter's
fields (`teamsync.Config.Receipts`); if W has not merged by then, n3e-12 adds it.

### 2.3 The n3d seams (faked here, filled by n3d)

| Seam                                                                     | n3e's side                                                                                       | The fake in n3e's tests                                                    |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------- |
| `team.Batch.Answered` (SD4: answers to own pings)                        | the engine keeps a team's answers until its next delivery and renders them as `answered:` lines  | a test `Persist`s a `Batch` with `Answered` set                            |
| TIMEOUT items (SD4: the state goroutine's timer)                         | a pending TIMEOUT is mapped to an `Entry` (render.go's mapping: key `event~silent`, outbox ping) | a store planted with an outbox ping and its TIMEOUT item, as P4's tests do |
| `outbox` records in `status` (SD4)                                       | none: the status line shows no outbox; the hooks count pending TIMEOUTs as pending               | none                                                                       |
| own sends (`Notices.RecordOwnSend`)                                      | none                                                                                             | none                                                                       |
| the `reply:` line's verbs (SD1)                                          | PK2 parses `teamchat reply '<id>' --verb <v>` with CL2's parser, which n3a built whole           | none                                                                       |
| n3d's failpoint "after a send's PUT before its answer is recorded" (PT1) | n3e-13 arms it by n3d's name                                                                     | none: n3e-13 starts after n3d has merged                                   |

A part that finds n3d merged when it starts uses n3d's real code instead of the fake, and says so.

## 3. n3e-1: the inbox socket and fakeinbox (DV1, F2)

Packages: new `internal/teamchat/member/inbox`, new `internal/teamchat/member/fakeinbox`.
Imports: `inbox -> clock, proto (EscapeASCII), x/sys/unix, std`; `fakeinbox -> clock, std`.
`fakeinbox` is a non-test package so the engine's, the link's and PT1's tests can import it;
production code never imports it (n3e-11's import scan proves it for `./cmd/teamchat`).

### Facts it needs, digested

- B 2.7.1: "one connection per delivery, two compact ASCII JSON lines
  (`{"type":"auth","token":...}` then `{"type":"user","message":{"role": "user","content":<envelope>}}`),
  half-close, read to EOF, 5 s per phase. A connect error `ENOENT`, `ECONNREFUSED` or `ENOTSOCK`
  means the session's socket is gone: delivery stops (the keeper will see the session end)."
- B 2.7.3: "**Sent** means the payload was written in full; a slow or failed close after that
  still counts (E11), so a lost answer never causes a second copy with a new nonce." "Nothing is
  persisted while the link only connects and authenticates": the caller persists the awaiting
  entry between `Dial` and `WriteUser`, so the API splits them.
- B11: the token is never written to disk or a log.
- B 2.15's fake: "a Unix listener that speaks the auth and user lines, records every frame, and
  can hold, refuse, apply a 30 s duplicate filter and a 30-token, 0.5/s bucket, as U01 measured."
  A frame the filter or the bucket drops is accepted on the wire and never reaches the
  transcript: that is how a held or dropped message looks to the link (it is declared missing).
- The path form: `paths.InboxSocket(runtimeDir, pid)` = `<runtime>/cc-socks/<pid>.sock`, at most
  107 bytes.

### API

```go
package inbox

const PhaseTimeout = 5 * time.Second
type Target struct { Socket, Token string }                       // String and GoString redact Token
var ErrGone = errors.New("inbox: the session's socket is gone")    // ENOENT, ECONNREFUSED, ENOTSOCK at connect
var ErrRetry = errors.New("inbox: no connection, retry later")     // any other connect or auth failure, or a timeout before the user line began
var ErrPartial = errors.New("inbox: the user line was cut")        // the user line began and did not finish
type Dialer interface { Dial(ctx context.Context, t Target) (Conn, error) } // connect and write the auth line
type Conn interface {
	WriteUser(envelope string) error // nil only for a full write; ErrPartial; ErrRetry when nothing was written
	Finish() error                   // half-close, read to EOF; its error never undoes a full write
	Close() error
}
func NewDialer(clk clock.Clock) Dialer // each phase bounded by clk.AfterFunc(PhaseTimeout, close)
func UserLine(envelope string) []byte  // the compact ASCII user line with its newline; its content's bytes agree with proto.WireBytes
```

```go
package fakeinbox

type Options struct { Token string; Clock clock.Clock; DupWindow time.Duration /* 30 s */; Bucket int /* 30 */; PerSecond float64 /* 0.5 */ }
type Frame struct { Auth bool; Content string; Accepted bool; Reason string } // reason "", "duplicate", "rate", "auth", "framing"
func Start(path string, opt Options) (*Inbox, error)
func (b *Inbox) Path() string
func (b *Inbox) Frames() []Frame
func (b *Inbox) OnAccepted(f func(Frame)) // faketranscript is attached here
func (b *Inbox) Hold() (release func())   // accept, read nothing until released
func (b *Inbox) Refuse()                  // close the listener, keep the path: ECONNREFUSED
func (b *Inbox) Remove()                  // unlink the path: ENOENT
func (b *Inbox) Close() error
```

### Tests

- **DV1** on the fake clock: one delivery records one frame, auth then content; the token never
  appears in an error or a `%v`, `%+v`, `%#v` of `Target`.
- `ENOENT` (Remove), `ECONNREFUSED` (Refuse) and `ENOTSOCK` (a regular file at the path) are
  `ErrGone`; a held fake times out each phase at 5 s: before the user line `ErrRetry`, inside it
  `ErrPartial` (the fake reads half the line, then holds).
- A `Finish` that times out after a full `WriteUser` leaves the write counted.
- `UserLine`: non-ASCII content escapes to `\uXXXX`; the line is valid JSON decoding back to the
  envelope; its content agrees with `proto.WireBytes`.
- fakeinbox: a second identical content inside 30 s is `duplicate`, after 30 s accepted; the 31st
  frame of a burst is `rate`, one more is accepted 2 s later on the fake clock; a wrong token is
  `auth`.

## 4. n3e-2: transcript proof primitives and faketranscript (DV4, F3)

Packages: new `internal/teamchat/member/proof`, new `internal/teamchat/member/faketranscript`.
Imports: `proof -> clock, x/sys/unix, std`; `faketranscript -> clock, std`. No import between
these and n3e-1's packages. n3e-2 builds the scanner and the watcher; the states they drive are
n3e-9's.

### Facts it needs, digested

- B 2.7.4: "The transcript watcher holds an inotify watch on the transcript file and its directory
  (for a new path or a rotation) and, on growth, scans only the new bytes, plus the needle's length
  less one of overlap, for the needle `teamchat delivery <nonce>:` of every delivery waiting for
  proof. The needle is ASCII with no character JSON escapes, so a byte search of the JSONL is
  exact." "A transcript that shrinks or moves restarts the scan at 0 of the new file."
- The two record shapes (host check U20, `CLAUDE/Plan/00161-agent-team-bus-matrix/u20_check.py`):
  idle `{"type":"user","message":{"role":"user","content":"<envelope>"},"origin":{"kind":"peer"}}`;
  busy `{"type":"attachment","attachment":{"type":"queued_command","prompt":"<envelope>","origin":{"kind":"peer"}}}`.
  Pinned per Claude Code version by host check E3; the scan depends only on the envelope sitting
  inside a JSON string.
- The transcript is opened as a member file is (TB4): `O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`, `fstat`
  regular file, at most a cap per read. The path is the one `AcceptFacts` accepted.
- B 2.15's fake: "appends the JSONL a real session would write for each frame (idle `user` entry
  or busy `queued_command`), after a delay, never, or after a rotation or `/clear`".

### API

```go
package proof

func Needle(nonce string) []byte // "teamchat delivery <nonce>:"
type Mark struct { Offset int64; Dev, Ino uint64 } // only Offset is persisted (n3e-D10)
type Result struct { Found []string; End Mark; Reset bool }
// Scan reads path from from.Offset less len(Needle)-1 bytes of overlap (never below 0) to its end,
// at most maxRead bytes. A file shorter than from.Offset, or another inode, is scanned from 0 (Reset).
func Scan(path string, from Mark, nonces []string, maxRead int64) (Result, error)
type Watcher interface { Changes() <-chan struct{}; Close() error }
func Watch(path string) (Watcher, error) // inotify on the file and its directory; IN_Q_OVERFLOW is a change
```

```go
package faketranscript

type Mode int
const ( Idle Mode = iota; Busy )
const Never time.Duration = -1
func New(dir, sessionID string, clk clock.Clock) (*Transcript, error) // <dir>/<session_id>.jsonl
func (t *Transcript) Path() string
func (t *Transcript) Record(envelope string, m Mode, after time.Duration) // after `after` on the fake clock; Never writes nothing
func (t *Transcript) Noise(n int)                                       // n tool-output lines, no needle
func (t *Transcript) Rotate() error                                     // a new inode at the same path
func (t *Transcript) Truncate() error
func (t *Transcript) Clear(sessionID string) (string, error)            // /clear: a new file and path
// Attach records each accepted frame; on is the inbox's OnAccepted adapted to the content, so no import is needed:
//   tr.Attach(func(f func(string)) { inbox.OnAccepted(func(fr fakeinbox.Frame) { f(fr.Content) }) }, mode, after)
func (t *Transcript) Attach(on func(func(content string)), mode func() Mode, after time.Duration)
```

### Tests

- **DV4 scan**: a needle split across two reads is found through the overlap; a needle wholly
  before `from.Offset - overlap` is not; a needle inside a JSON string with escapes around it is
  found; each nonce is reported once.
- A truncated, a rotated and a shrunk file are `Reset` and scanned from 0; a symbolic link, a FIFO
  (a writer polling it `O_WRONLY|O_NONBLOCK` gets `ENXIO` every time: no reader ever existed) and
  a directory at the path are refused by name.
- `maxRead` bounds one call; the next continues from `End`.
- `Watch`: growth, a rotation and a new file in the directory each send a change (real inotify on
  a temp dir; the test waits on the channel).
- faketranscript: both shapes are one valid JSON line holding the envelope; `Never` writes
  nothing; `Clear` gives a new path in the same directory.

## 5. n3e-3: the shared additions (colour, proto, state, team, failpoint names)

Packages: new `internal/teamchat/colour`; additions to `proto`, `member/state`, `member/team` and
`failpoint`. Each addition is small and type-level, and changes no built behaviour except where
named.

### Facts it needs, digested

- **B27** (quoted in full in section 16) and **Plan 00217's palette** (SPEC-team-colours C1-C5,
  C7, C10, C15; Plan 00217 branch, commit aea3e0ba2; v2 "takes the palette, the hash and the
  override grammar as they are"):

  | Index | Name    | Hex       |
  | ----- | ------- | --------- |
  | 0     | teal    | `#2dd4bf` |
  | 1     | cyan    | `#22d3ee` |
  | 2     | sky     | `#7dd3fc` |
  | 3     | blue    | `#60a5fa` |
  | 4     | indigo  | `#a5b4fc` |
  | 5     | violet  | `#a78bfa` |
  | 6     | purple  | `#d8b4fe` |
  | 7     | magenta | `#e879f9` |
  | 8     | pink    | `#f9a8d4` |
  | 9     | mint    | `#99f6e4` |

  - C2: every colour has a WCAG 2.x contrast of at least 4.5:1 against each of `#000000`,
    `#1e1e1e`, `#242424`, `#282c34`, `#2e3440` (linearised sRGB: `c/12.92` for `c <= 0.04045`,
    else `((c+0.055)/1.055)^2.4`; `L = 0.2126R + 0.7152G + 0.0722B`; ratio `(L1+0.05)/(L2+0.05)`).
  - C3: every hue (HSV) lies from 160° to 335°. C4: every pair at least 15 apart in CIE76 ΔE
    (CIELAB, D65).
  - C5: the automatic colour is `palette[fnv1a32(name) % 10]`, FNV-1a 32-bit over the name's
    bytes (offset basis `0x811c9dc5`, prime `0x01000193`).
  - C7: an override is a palette name (lower case) or `#rrggbb` (either case, stored lower case);
    refusals `must be one of <names>, or #rrggbb` and
    `#<hex> is too dark to read on a dark terminal (contrast <ratio>:1 against <background>, at least 4.5:1 needed)`
    (ratio to two places).
  - C10: a piece is `ESC[38;2;<r>;<g>;<b>m<piece>ESC[0m`, decimal components.
  - C15: the piece is `<team>: <member> (<r>)`, `<member>` the handle up to its `+`.

- **The override travels as room state, never in the team record** (C8): the record is the trust
  record, and a member that does not know a key refuses it. In v2 the main room's `state_default`
  is the closed level (`proto/rooms.go` `powerLevels`), so only the admin writes state there.

- **Farewell reasons** (B 2.10): `released` (the owner took the seat out of the session; the
  member stays in the team), `removed` or `parked` (the member left the team), or none. proto's
  MEMBERSHIP knows only `removed` (`membership.go` `farewellText`); DV6 needs every reason (n3e-D6).

- **The description**: SessionStart answers "each team's seat, role and description" (B 2.8);
  MEMBERSHIP's joined line names it (A 13.3). It reaches a member only as the main room's topic
  (`proto.Desired`'s `mainSpec`), which nothing reads on the member side yet (n3e-D5).

- Gap 9's n3e points (PT1's list less n3c's and n3d's): after auth before the awaiting entry is
  persisted; after it is persisted before the user line; after the user line before
  `written: "yes"`; between two stores' writes of one delivery (n3e-D8); the link after `missing`
  is persisted.

### API

```go
package colour // std only
type Colour struct{ Name, Hex string }
var Palette = [10]Colour{{"teal", "#2dd4bf"}, …}
var DarkBackgrounds = [5]string{"#000000", "#1e1e1e", "#242424", "#282c34", "#2e3440"}
const MinContrast = 4.5
func Auto(team string) string            // C5
func Parse(v string) (string, error)     // C7
func Stored(v string) bool               // lower-case #rrggbb
func Contrast(a, b string) float64       // C2
func SGR(hex string) string              // "\x1b[38;2;r;g;bm"
```

```go
// proto
const ColourType = "teamchat.colour"                                       // main room, state key "", admin only
func ColourContent(hex string) Value                                       // {"colour":"#rrggbb"}, or {} for ""
func DecodeColour(content Value) (hex string, ok bool)                     // exactly {"colour": <stored hex>} or {}
func MainColour(state []StateEvent, server string) (hex string, ok bool)   // the admin's event; absent is ("", true)
func MainDescription(state []StateEvent, server string) (string, bool)     // the admin's topic, ValidDescription, not the default topic
// StateTypes gains ColourType and the topic type (the sync filter carries both)
func Desired(server string, r Roster, ids RoomIDs, description, colour string) []RoomSpec // main gains ColourContent(colour)
const FarewellReleased = "released"; FarewellParked = "parked"; FarewellNone = "" // beside FarewellRemoved; texts n3e-D6
```

`rooms.go`'s one caller of `Desired` passes `""` here (n3e-8 threads the declared colour). The
filter goldens (`proto/testdata/filter-*.json`) are regenerated and their diff reviewed; P1's test
pinning the sync query to proto's filter stays green by construction.

```go
// member/state: StatusTeam gains
	Member  *string `json:"member"`  // the handle's Short(), null until the link knows the handle
	Colour  string  `json:"colour"`  // the admin's override, else colour.Auto(team); always stored hex
	Stalled bool    `json:"stalled"` // B 2.7.3's 120 s stall
```

```go
// member/team: fields and one interface; no built implementation breaks
type Health struct { …; Colour *string; Description *string } // nil: none
type Batch struct { …; HeldBack map[string]int64 }              // agent sender user id -> items over its budget (n3e-D1)
type Receipts interface {                                       // B 2.7.5, the engine's side (n3e-7)
	PendingReceipts(t paths.TeamID) (rs []state.Receipt, changed <-chan struct{})
	ReceiptSent(ctx context.Context, t paths.TeamID, id string) error
}
```

```go
// failpoint
const ( LinkAfterAuth Name = "link-after-auth"; LinkAfterAwaiting = "link-after-awaiting"
	LinkAfterUserLine = "link-after-user-line"; LinkBetweenStoreWrites = "link-between-store-writes"
	LinkAfterMissing = "link-after-missing" )
```

### Tests

- colour: C1 pinned; C2, C3 and C4 computed for every colour and pair; C5 on FNV-1a vectors
  (`""` -> `0x811c9dc5`, `"a"` -> `0xe40c292c`, `"foobar"` -> `0xbf9cf968`) and a team-name table;
  `Parse` refuses with each text, takes a name or either case of a legible hex, refuses `#000000`
  naming its ratio and background.
- proto: `DecodeColour` on `{}`, a stored hex, an upper-case hex, an extra key, a number;
  `MainColour` ignores every sender but the admin; `Desired` with and without a colour;
  `MainDescription` on the default topic, a valid and an invalid description, a non-admin topic;
  each farewell reason renders its line (`TestEnvelopeCorpus` gains `membership-reasons.txt`).
- state: the new keys round-trip; a member name off `<repo>.<seat>` and an upper-case colour are
  refused; the largest file (64 teams) still fits 64 KiB.
- failpoint: the release-build scan covers the five new names.

## 6. n3e-4: the hook verb, the hook texts and the link-down reading (HK1's client half)

Packages: new `member/hooktext`, new `member/hook`; additions to `member/client` (exports) and
`member/wire` (`HookArgs`).

### Facts it needs, digested

- B 2.8: "**Every hook** reads stdin (at most 1 MiB; strict UTF-8; a JSON object), sends one
  `hook.<event>` request to the link (2.4.4) with the payload fields it uses (`session_id`,
  `transcript_path`, `source`, `stop_hook_active`, `reason`) and the inbox socket variables, and
  prints the link's answer: one JSON line, `{}` or Claude Code's `hookSpecificOutput`,
  `decision`/`reason` or `systemMessage` shapes (H6). The answer is built by the link from fixed
  templates filled with counts and grammar-checked names only (H14). Stdin that is too long, not
  UTF-8 or not a JSON object is sent as `"payload": "unusable"` with the reason from a closed set
  (`size`, `utf8`, `json`)… The hook exits 0 always; a bad event argument is 64."
- "a hook with no `TEAMCHAT_RUNTIME` answers `{}` and exits 0 in a few milliseconds."
- "**When the link does not answer** (a refused connect, … or no answer within 300 ms, which is a
  hung link), the hook answers from its own fixed template, chosen by `keeper.json` and the
  `keeper.lock` probe exactly as a verb's exit 69 is (2.4.4), with the recovery for the member
  kind (2.4.2): as additional context on SessionStart and on every UserPromptSubmit while the link
  does not answer, and nothing on Stop and SessionEnd. … a hook writes no file. Stop never blocks
  on it."
- "**Hook texts name commands in B's form**: a member verb as `teamchat <verb>` with its flags as
  2.2.1 spells them, and the recovery commands of 2.4.2 (`ccy-teams restart <session>`) as the
  owner's commands they are."
- The inbox variables: `CLAUDE_CODE_MESSAGING_SOCKET`, `CLAUDE_CODE_MESSAGING_TOKEN` (B 2.7.1).
  The token is a secret (B11).
- `kit.Hooks`' args map to ops: `session-start` -> `hook.session-start`, `user-prompt-submit` ->
  `hook.prompt`, `stop` -> `hook.stop`, `session-end` -> `hook.session-end`.

### API

```go
// wire
type HookArgs struct {
	Payload        string  `json:"payload"`         // "ok" | "unusable"
	Unusable       *string `json:"unusable"`        // size | utf8 | json, exactly when unusable
	SessionID      *string `json:"session_id"`
	TranscriptPath *string `json:"transcript_path"`
	Source         *string `json:"source"`          // session-start only
	StopHookFalse  bool    `json:"stop_hook_false"` // stop: the payload held the JSON literal false (B26)
	Reason         *string `json:"reason"`          // session-end only
	Socket         *string `json:"socket"`
	Token          *string `json:"token"`           // a secret: every formatter of a request redacts it
}
```

```go
// client: a move of sessionOf, noLink and failed; the verbs keep their exits
func Runtime(getenv func(string) string) (paths.Run, error) // TEAMCHAT_RUNTIME's run, or 69 no-session
type Down struct { State string /* restarting | starting | failed */; NextMS int64; Kind, Session string }
func ReadDown(run paths.Run) Down                                    // one keeper.json read, one keeper.lock probe; never fails
func (d Down) Error(team *paths.TeamID, boot time.Duration) *exits.Error // LN3's 69, as built
```

```go
package hooktext // fixed templates; fills are counts and grammar-checked names only
type Answer struct{ Line []byte }        // one compact JSON object, no newline
func Empty() Answer                       // {}
func Context(event, text string) Answer   // {"hookSpecificOutput":{"hookEventName":event,"additionalContext":text}}
func Block(reason string) Answer          // {"decision":"block","reason":reason}
type FrameTeam struct { Team paths.TeamID; Handle, Role, Coordinator, Description string }
type Counts struct { Total, Humans, Agents, Pings, Notices, Timeouts int64 }
func Frame(teams []FrameTeam, mode string) string // proto.FrameText, one block per team, the delivery mode line
func Pending(c Counts) string; func Missing(n int64) string; func Stalled(c Counts) string
func StopPending(c Counts) string; func StopNoWaker() string
func TeamFailed(t paths.TeamID, failed string) string; func TeamDown(t paths.TeamID) string
func DesiredRefused(code string) string; func AddRefused(t paths.TeamID) string
func Membership(lines []string) string            // the MEMBERSHIP lines, as the prompt fallback (DV6)
func Recovery(d client.Down, boot time.Duration) string
func All() []string                               // every template filled from fixed samples, for PK2
```

```go
package hook
func Main(e cli.Env, inv cli.Invocation) term.ExitCode // `teamchat hook <event>`; a cli.RunFunc
```

The proposed texts (each a const; the builder may tune wording, never add a fill that is not a
count or a checked name):

| Template          | Text                                                                                                                                                    |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| frame team block  | `team <t>: you are <handle>, the <role> (coordinator: <h>). <description>` (no coordinator: `the team has no coordinator now`; no description: omitted) |
| role, coordinator | `In team <t> you direct the team's agents; ask the humans with teamchat ask --team <t>, and follow up with teamchat questions --team <t>.`              |
| role, worker      | `In team <t> take direction from the team's humans and its coordinator; report with teamchat report --team <t>.`                                        |
| role, probe       | `In team <t> this session is the team's acceptance probe.`                                                                                              |
| mode, socket      | `Deliveries arrive in this conversation by themselves.`                                                                                                 |
| mode, wait        | `Keep teamchat wait running in the background: deliveries reach this session only through it.`                                                          |
| pending           | `teamchat: <n> pending (<h> from humans, <a> from agents, <p> pings, <o> notices, <m> timeouts): run teamchat recv`                                     |
| missing           | `a delivery did not reach the conversation; <n> items are pending again: run teamchat recv` (B 2.7.4's own words)                                       |
| stalled           | `teamchat: deliveries have failed for 2 minutes; <n> pending: run teamchat recv`                                                                        |
| stop, pending     | `<n> pending (<h> from humans, <a> from agents). Run teamchat recv before stopping.` (B 2.8's text)                                                     |
| stop, no waker    | `Delivery here is by teamchat wait, and none is running: start teamchat wait in the background before stopping.`                                        |
| team failed       | `teamchat: team <t> stopped (<failed>); run teamchat status --team <t> and tell a human`                                                                |
| team down         | `teamchat: team <t> cannot reach its server; its messages wait there`                                                                                   |
| desired refused   | `teamchat: this session's membership file could not be read (<code>); the last good membership stays; tell a human`                                     |
| add refused       | `teamchat: team <t>'s seat is held by another session; tell a human`                                                                                    |
| recovery          | `client.Down.Error(...).Message`, the verbs' own 69 text                                                                                                |

### What `hook` does

1. Map the event argument to the op (the parser already refused any other: 64).
2. Read stdin, at most 1 MiB + 1 byte: over 1 MiB `size`; invalid UTF-8 `utf8`; not exactly one
   JSON object (`proto.Parse` strict) `json`. A field of the wrong type is absent, not unusable.
3. No `TEAMCHAT_RUNTIME`, or one `client.Runtime` refuses: print `{}`, exit 0.
4. Connect to `link.sock`; send the request with `deadline_ms` = Boot + 300 ms; read one answer
   line and the `done` before the deadline (`clock.AfterFunc` closes the socket).
5. One JSON object line, then `done` exit 0: print it and `\n`, exit 0.
6. No answer (`ENOENT`, `ECONNREFUSED`, a close before `done`, the deadline, a malformed answer):
   session-start and user-prompt-submit print `Context(event, Recovery(ReadDown(run), boot))`;
   stop and session-end print `{}`. Exit 0. No file is written.
7. A failed stdout write keeps exit 0.

### Tests (HK1, client half)

- **HK1**: exactly 1 MiB is used, 1 MiB + 1 is `size`; `\xff` is `utf8`; `[]`, `"x"`, two
  objects and trailing junk are `json`; each still sends the inbox variables.
- No TEAMCHAT_RUNTIME: `{}`, exit 0, no dial (the seam counts dials).
- A link that answers: its line printed byte for byte.
- A link that never answers: at 300 ms on the fake clock, session-start and prompt print the
  recovery for each `Down` (restarting with its seconds, starting, failed ccy naming the session,
  failed lxc, failed host, keeper.json missing); stop and session-end `{}`; exit 0 each time; the
  run directory's listing unchanged.
- The token is in the request and never in stderr, a log or a formatted request.
- `client`'s LN3 tests stay green after the move.
- hooktext: each output is one line with no control character; `All()`'s count is pinned, so a
  new template without a sample fails.

## 7. n3e-5: the status line (SL1, B27's drawing)

Package: new `member/statusline` (`Main(e cli.Env, inv cli.Invocation) term.ExitCode`, the
`status-line` verb). Imports `client` (`Runtime`, `ReadDown`), `state`, `lock`, `homefs`,
`colour`, `execx`, `clock`, `x/sys/unix`.

Facts (B 2.9 and SPEC-status-line S5-S15, S26-S31 of Plan 00213, as v1's rules):

1. **Inner line.** Not run when `TEAMCHAT_STATUS_LINE_INNER` is non-empty. The project directory
   is the first absolute of the payload's `workspace.project_dir`, `$CLAUDE_PROJECT_DIR`, the
   payload's `cwd`. The first usable `statusLine.command` of `<project>/.claude/settings.local.json`,
   `<project>/.claude/settings.json`, then `$CLAUDE_CONFIG_DIR/settings.json` or
   `$HOME/.claude/settings.json`; a command whose trimmed text is `teamchat status-line` or ends in
   `/teamchat status-line` is skipped (n3e-D16). It runs as `/bin/sh -c <command>` with the
   payload on stdin, `TEAMCHAT_STATUS_LINE_INNER=1` added, in a new session (its pid is its
   process group), stdout captured, stderr inherited, through `execx.Starter` (`NewSession`).
   At 10 s the group gets SIGKILL (`unix.Kill(-pgid)`, behind a seam) and the output is only
   `ESC[31m(status line timed out after 10 s)ESC[0m`; a grandchild holding the pipe never keeps
   the render waiting. SIGTERM, SIGINT or SIGHUP: the group is killed (a signal before the pid is
   known is held until it is), exit `128+n`, nothing on stdout. Exit 0: stdout decoded with U+FFFD
   per invalid byte, trailing `\n`s stripped. Non-zero: `<text> ESC[31m(status line exit N)ESC[0m`,
   N negative for a signal. Never cached. Settings errors: `status line: <path> cannot be read` /
   `is not valid JSON`, in red (n3e-D16: no Python exception names).
2. **Team segment.** No TEAMCHAT_RUNTIME: none. Else one `state.Read` of `status.json` (64 KiB,
   strict) and one `lock.Probe` of `link.lock`:
   - link.lock held: one piece per team, `<team>: <member> (<r>)` then its marks `(down)`,
     `(stalled)`, `(missing)` (missing > 0), `(failed)`, all but the last piece followed by `,`;
     each piece in its team's colour span (`colour.SGR(Colour)`, C10); `<r>` `c`, `w`, `p`, or
     `role?` (n3e-D3); `member?` while `member` is null. A held link.lock with `written_ms` more
     than 90 s behind WallMS while a team is held adds a red `teamchat not answering`.
   - link.lock free: the rows are replaced by one red form from `client.ReadDown`:
     `teamchat restarting`, `teamchat starting` (n3e-D17), `teamchat stopped: ccy-teams restart <session>`
     (ccy and lxc), `teamchat stopped: relaunch this session` (other kinds).
   - Red `teamchat membership refused` when `desired` is `refused`; red
     `teamchat hook input unusable: <n>` when `unusable_hook_stdin` > 0; both with or without
     teams. No team, link running, no red note: no segment (empty stays empty).
   - The label is `👥 `; rows wrap greedily at `COLUMNS` (1-6 digits, > 0; else one row), between
     pieces only, continuation indented 3 spaces; escapes take no columns; a red note's words wrap
     the same way, one `ESC[31m` span per row, label included.
3. Output: inner text, `\n`, segment (either alone if the other is empty), then the final `\n`.
   Exit 0 always but the cancel. B27's cost: one exec, one ~1-2 KiB read, one probe while the link
   runs; one more read and probe only while it does not.

Tests: **SL1** each inner case (missing settings, the skip of itself, the loop guard, timeout with
a grandchild holding the pipe, cancel before and after the pid, exit N and -N, invalid UTF-8);
**B27** a two-team render with each team's colour and short name byte for byte, an override
colour drawn, `member?` and `role?`; each red form; wrapping at a narrow `COLUMNS` with colour
escapes not counted; **OF1** one team `(down)` beside a running one: rendered, exit 0; a missing,
oversized and undecodable `status.json`: the red stopped form or no segment, never a non-zero exit.

## 8. n3e-6: delivery (DV1's use, DV2, DV3; the session-level send for DV6)

Packages: `member/engine` (new files `deliver.go`, `deliverloop.go`; hunks in `recv.go` for the
multi-store failpoint and in `statusRow` for `Stalled`), new `member/deliver` (pure: the choice and
the mapping), and a `member/link` hunk (the inbox dialer and the token into the engine;
`Session.CountUnusable`).

API:

```go
package deliver // pure; no I/O, no clock
type Item struct { Pending state.Pending; Verdict *proto.Verdict; Timeout *Timeout } // Verdict nil: restored, re-fetch first
type Timeout struct { Ping state.Ping; Silent string }
type Namer func(userID string) (from, role string)
func Choose(teams map[paths.TeamID][]state.Pending) (paths.TeamID, bool) // oldest pending HUMAN's team, else oldest any; ties by team id
type Facts struct { Answered []proto.Answered; HeldBack []proto.HeldBack; Gap, Stopped bool }
type Built struct {
	Delivery proto.Delivery; Text string
	IDs []string          // the items in the text
	Failed []string       // slots that failed (DV2): dropped as `render`; HUMAN ones are on the not-shown line
	Left []string         // left pending past the caps
}
func Build(t paths.TeamID, self string, items []Item, f Facts, from Namer, nonce func() (string, error)) (Built, error)
```

```go
// engine additions
type Config struct { …; Inbox inbox.Dialer; OnRender func(n int64) } // nil Inbox: no socket delivery
type SessionFacts struct { Socket, Token, Transcript string; Idle bool }
func (e *Engine) SetSession(ctx context.Context, f SessionFacts) error  // the link calls it on every accepted hook op and token message
func (e *Engine) SendMembership(ctx context.Context, m proto.Membership) (written bool, err error) // paced with deliveries; true after a full write
func (e *Engine) Live() bool                                            // socket mode, facts set, not gone
```

The loop (B 2.7.2-2.7.3, digested):

01. Live only when `Delivery == socket`, a socket and a transcript are set (no socket delivery
    before a hook gave both), and no connect has said `ErrGone` since (gone stops delivery until a
    new socket fact). Never for a launch with inbound `refuse` (its `Delivery` is `wait`).
02. One delivery per 2 s (Boot), membership notices included; items arriving meanwhile join the next.
03. `deliver.Choose` over each held team's deliverable pending (not handed, not `held`, not removed).
04. Re-fetch restored candidates off the goroutine as recv does (2 s, per team), judge again on it,
    then `deliver.Build`: A 13.7's order; a failed slot drops the item (`unusable.render` via
    `OnRender`, one link.log line naming the event id, a HUMAN one on `not shown:`); the nonce, 8 hex
    from `crypto/rand`, drawn after the bodies are rendered; `Fit` to 8 entries and 12 KiB. Trailing
    facts per team in memory: `answered:` (n3d's `Batch.Answered`), `held back:` (`Batch.HeldBack`,
    n3e-D1), `gap:` (`Batch.Gap`), `stopped:` (`Health.Stopped` set, once per stop, n3e-D13); a
    trailing-only delivery at most once a minute per team, not persisted as awaiting (n3e-D14).
05. `inbox.Dial` off the goroutine. `ErrRetry`: nothing persisted, items pending with no `repeat`,
    retry in 10 s. `ErrGone`: delivery stops.
06. `failpoint.Hit(LinkAfterAuth)`; one store write: `awaiting {nonce, ids, transcript {path, size now}, at_ms, written "unknown", stop_ms: now if the session is idle (its last hook was Stop or SessionStart with no prompt since) else null}`, the ids out of pending, the render drops too.
07. `Hit(LinkAfterAwaiting)`; `WriteUser`. Full: `Hit(LinkAfterUserLine)`, one write `written "yes"`
    and the ids into the ledger. `ErrPartial`: the entry stays `unknown` (n3e-9's scan settles it).
08. `Finish`; its error changes nothing.
09. Stall: every write failed for 120 s: `Stalled` on the team rows with pending, and on HookView.
10. `LinkBetweenStoreWrites` is hit between consecutive store writes of recv's hand-out, `Ack` and
    `Return` (n3e-D8).

Tests by expectation: **DV1** never to a `wait` session, none before both facts, gone stops;
**DV2** a mapping golden per kind (HUMAN, PEER, PING, NOTICE, carried, TIMEOUT from the n3d fake),
a failed HUMAN slot gives `not shown:` and `unusable.render` 1; **DV3** awaiting persisted before
the user line (failpoint rows), connect and auth failures persist nothing and retry at 10 s, 8
entries and a 9th, 12 KiB exactly and one byte over, a single item over the byte cap sent alone,
HUMAN items behind agent items in one team, three busy teams one team per delivery, the 2 s pace,
the 120 s stall; **T18/T19** one team per delivery, a flooding sender's `held back:` line; **OF1** a
team whose fakehs is stopped beside a running one: the running team's deliveries unchanged.

## 9. n3e-7: room facts, receipts and status.json (B27's reading, DV5's sender, SL1's link half)

Packages: `member/teamsync` (new `facts.go`, `receipts.go`), `member/engine` (new `facts.go`;
`status.go`'s `publish` pacing and the `Member`/`Colour` hunk of `statusRow`). `teamsync.Config`
gains `Receipts team.Receipts`; `engine.TeamConfig` gains it too (the engine passes itself).

- teamsync reads `proto.MainColour` and `proto.MainDescription` at every main-room verification
  and when a sync carries a `teamchat.colour` or topic state event; puts them in `Health` (Colour,
  Description). An event from anyone but the admin, or a content `DecodeColour` refuses, is no
  colour, one link.log line, and never a loss of trust (C9).
- teamsync sends `m.read` receipts (`matrix.ReadReceipt`) for `PendingReceipts`, mapping the
  receipt's room purpose to its id through `Anchors`, then `ReceiptSent`; transient failures
  retry with the team's backoff; nothing when the team is stopped or removed.
- engine: `PendingReceipts` and `ReceiptSent` (one store write per sent receipt); a removed
  team's receipts go with its store.
- `status.json`: `member` = the handle's `Short()`, `colour` = `Health.Colour` else
  `colour.Auto(team)`. Written on change at most once a second, and at least once a minute while a
  team is held, from the state goroutine's own timer (never a long-poll return); with no team,
  once and then on change.

Tests: **DV5** a proven HUMAN item's receipt sent once, retried after a 500, gone with a removed
team, kept across a link restart (in the store); **B27** an admin colour shown, a non-admin one and
a malformed one ignored with one log line and the team still trusted, the auto colour otherwise;
**SL1/T12** two changes in 300 ms are one write, a quiet held team gets one write a minute, every
team stopped on a permanent failure still refreshed each minute; **OF1** a down team's row written
with `down: true` beside a running one.

## 10. n3e-8: the admin writes the colour (B27's override, host side, inert)

Packages: `declare` (a `teamchat_teams[]` entry gains `colour`, checked by `colour.Parse`, its
error under `colour:` naming C7's rule), `rooms` (threads the colour into `proto.Desired`; the room
state event written only when it differs, `{}` when the entry sets none, n3e-D4). Inert: no play
task, no variable file changes; the playbook's documentation of `colour:` is Task 3.3's.

Tests: a declared colour reaches the main room as `{"colour":"#rrggbb"}` on fakehs; a palette name
is stored as its hex; a too-dark hex and a bad value are refused by `declare check` with C7's text;
a second apply writes nothing; removing `colour:` writes `{}`.

## 11. n3e-9: proof transitions (DV4, DV5's start; P8-P10)

Files: `member/engine/proof.go`; a `member/link` hunk (one `proof.Watch` on the session's
transcript, replaced when `SetSession` changes it).

- On a change: `proof.Scan` for every awaiting nonce (from its offset) and every missed nonce.
- Found awaiting: proven; out of `awaiting`; an `unknown` entry's ids into the ledger; HUMAN items'
  `{id, room}` into `receipts`; one write; the team's `(missing)` clears.
- The quiet clock: an entry's `stop_ms` is set at the write if idle, else at the first Stop after
  it (one write per store on the idle transition). Missing when `stop_ms` is set and the
  transcript has been quiet 180 s since the later of `stop_ms` and its last growth, or 1 h after
  `at_ms`: one write moves it to `missed {nonce, ids, offset, at_ms}`, its items back to pending
  `repeat` and `held`, their ids out of the ledger; `Hit(LinkAfterMissing)`. `at_ms` and `stop_ms`
  are CLOCK_BOOTTIME ms (n3e-D9).
- Late find of a missed nonce within 24 h: items still pending leave as proven (ledger, receipts);
  items already shown by `recv` are logged and counted `seen twice` (memory, in `status`'s
  counts); the entry leaves `missed`. Missed entries older than 24 h are dropped.
- Link restart: each awaiting entry is scanned from its offset and follows the same rules; a
  shrunk or moved transcript rescans from 0 (n3e-D10).
- `Missing` in `status.json` is the missed entries since the team's last proof (n3e-D11).

Tests: **DV4** proven on an idle and a busy record; a busy session silent 10 minutes then recording
(P8: proven, never missing, `recv` never shows them); missing at 180 s quiet after Stop and at 1 h;
a late find (P9: pending ones proven, shown ones `seen twice`, nothing a third time); P10 missing
then a `SIGKILL` (`busfailpoints`, `LinkAfterMissing`) and restart: all pending `repeat` `held`,
none in the ledger, `recv` shows each once; a `/clear` mid-proof; a truncated transcript.

## 12. n3e-10: the hooks in the link, and membership notices (HK1-HK3, DV6, n3c-D23)

Files: `member/link/hooks.go`, `member/link/membership.go`; `member/engine/hookview.go`; a hunk in
the link server routing the four hook ops to the link before `engine.Serve` (n3e-D18).

```go
// engine
type HookView struct { Teams []HookTeam; Live, Stalled, Waiting bool; Desired *state.ReportDesired; Refused []paths.TeamID }
type HookTeam struct { Team paths.TeamID; Handle, Role, Coordinator, Description string
	Counts hooktext.Counts; NotPushed []string; Missing int64; Down bool; Failed *string }
func (e *Engine) HookView(ctx context.Context) (HookView, error)
type Config struct { …; OnTeams func([]TeamRole) } // on the state goroutine when the held teams with a known role, or a role, change
type TeamRole struct { Team paths.TeamID; Role, Handle, Coordinator, Description string }
```

- Every op: decode `HookArgs`; `unusable` counted (`Session.CountUnusable("hook_stdin")`, one log
  line) and answered as an event with no payload; `AcceptFacts`; the token, when accepted with the
  socket, to the keeper (`token`) and `SetSession`. An op before the keeper gave the pid waits to
  its deadline, then the connection closes with no answer (the hook's own recovery line).
- SessionStart: frame (`hooktext.Frame` from `HookView`), plus pending and failure lines; `told`
  set to the current teams and roles, `frame_told` true, session.json written before answering.
- UserPromptSubmit: busy; transcript path updated; membership lines when the socket write failed
  (B30 fallback, `told` written then); pending counts only when delivery is not live or items are
  held as missing; a failure line once per 10 minutes (`stop_guard.failure_ms`, shared with Stop,
  n3e-D11b); the refusal lines once per link life (n3e-D13).
- Stop: idle (the quiet clock); only when `stop_hook_false`; blocks once per distinct digest of the
  not-pushed pending ids (`pending_digest`); once per 10 minutes for a `failed` team (never for a
  `down` one, n3e-D12); once per 10 minutes for a `wait` session with no `wait` running
  (`no_waker_ms`); never when session.json cannot be written; never for items the socket will push.
- SessionEnd: `clear`/`resume` `{}`; otherwise the reason recorded and `{}`; no network; one write.
- Membership (DV6): on `OnTeams`, diff against `told` (joined, left with the last remove's farewell
  reason, role changed, teams now; a team counts once its role is known); one
  `SendMembership`; `told` written when it returns `written`; on failure or no live socket, the
  next prompt answers the same lines. Told once per generation and role (`told.generation` from
  launch and `desired` messages).

Tests by expectation: **HK1** each event's answer from its template, a refused forged op answered
`{}` with nothing recorded, the unusable count surviving a restart; **HK2** each block case and its
non-block twin (items about to be pushed; `stop_hook_active` true or absent; session.json
unwritable), the 10-minute repeats on the fake clock; **HK3** one write, no network, under the
300 ms deadline on the fake clock; **DV6** a join, a leave with each reason, a role change, a failed
socket write then the prompt fallback (told once), a link restart after told (nothing twice), a new
session told by SessionStart; **n3c-D23** a refused add and a refused desired file each said once;
**OF1** a down team: Stop does not block, the prompt says `cannot reach its server`, exit 0.

## 13. n3e-11: the kit's files in use, the skill, and the text test (PK1, PK2)

Packages: `member/kit` (the skill embedded and written to `plugin/teamchat/skills/teamchat/SKILL.md`);
the text test in `member/kit` (test files may import cli, proto, hooktext, records, exits).

- **PK2's skill** (B 2.12, its eight points in order, kept short): the envelope and its kinds and
  authority; `reply`, `say`, `ping`; `verify` before a consequential HUMAN request; never carry one
  team's messages to another; the verbs with their record kinds and the four exit classes (fix,
  wait, read, tell a human) with `teamchat help exit-codes`; `report` for anyone, `ask`, `thread`,
  `questions` for a coordinator; `wait` mode and exit 69; `more` and `restored:`.
- **PK2's text test**: every verb, flag, record kind and exit class SKILL.md names exists, and none
  an agent can meet is missing; every `teamchat` command in A 13's templates (the corpus files and
  `proto.FrameText`), the authority sentences, `hooktext.All()` and the client's 69 texts parses
  with `cli.Parse`; a `reply:` line once with each of `ack`, `nack`, `done`, `blocked`, and `Q<n>`
  with a question number.
- **PK1**: a kit assembled with the test binary as `bin/teamchat` (reexec, dispatching `hook` and
  `status-line` to `hook.Main` and `statusline.Main`): each `hooks.json` command run as written
  answers `{}` with no TEAMCHAT_RUNTIME and the recovery line with a run directory and no link,
  exit 0; the settings' status-line command runs and exits 0; `refreshInterval` 25, below 30.
  `--plugin-dir` loading in Claude Code is host check E3's, not a unit test.
- An import scan: `./cmd/teamchat` imports neither fake package.

## 14. n3e-12: wiring and end to end (needs W)

`cmd/teamchat` adds `hook` -> `hook.Main` and `status-line` -> `statusline.Main`; `cli`'s `"n3e"`
row goes; the link's production `Inbox` (`inbox.NewDialer`) and transcript watcher are the
defaults. End-to-end with real processes over W's harness: a `--claude` run with a fake CMD that
plays Claude Code (runs the hooks from the kit, listens on a fakeinbox at its `cc-socks/<pid>.sock`,
records into a faketranscript): a human's item delivered, proven, its receipt seen on fakehs; a
held socket gives `missing` and the Stop block; a hook with the link killed answers the recovery; the
status line through the kit's command shows the team in its colour with its short name; OF1 with
one team's fakehs stopped.

## 15. n3e-13: PT1, the interleaving harness (needs n3d)

New test package `member/pt1` under `busfailpoints`: a seeded driver over real keeper and link
processes (reexec), interleaving sync batches, `recv`, `wait`, deliveries, fakeinbox hold and
refuse, fakehs drop and restart, n3d's sends, and a `SIGKILL` at every failpoint (n3c's ten,
n3e's five, n3d's); each run prints its seed and a failing seed replays it; after each run P1-P11
are checked over the fake transcript, the stores and fakehs. A fixed seed list runs in the gate (a
`go test -tags busfailpoints ./internal/teamchat/member/pt1/` row added to the Go gate in its own
hunk, n3e-D19); `-pt1.random=<n>` is the manual long run.

## 15a. Corrections from the brief review (round 1)

These override sections 0-15 where they differ (`261022-n3e-brief-review.md` gives each finding's
reason). A builder reads this section with its own.

- **`held back:` is filled (major 1).** n3e-7 adds the teamsync hunk: each event `proto.Judge`
  refuses with reason `rate` adds one to `Batch.HeldBack[sender]` of the batch it belongs to
  (counted once, when the batch is persisted, as the other counts are). n3e-6 renders it.
- **The coordinator reaches the engine (major 2).** n3e-3 adds `Health.Coordinator *string` (the
  roster's coordinator handle, nil when none); n3e-7's teamsync fills it at every verification and
  roster change. `HookView` and `OnTeams` read it from `Health`, never from `Roster()`: no hook op
  makes a network call or calls a team method.
- **The unusable counts are shown (major 3).** n3e-10 adds `func (e *Engine) SetUnusable(ctx, u state.Unusable) error`, called by the link whenever `Session.CountUnusable` changes a count (and
  once at start from session.json); the engine writes `unusable_hook_stdin` into `status.json`
  and both counts into `status`'s `runtime` record (`unusable` object). n3e-6's `OnRender` calls
  `CountUnusable("render")`, which leads to the same call.
- **A stall reaches the hooks (major 4).** While stalled (B 2.7.3: every write failed for 120 s),
  delivery counts as not live for the hooks: `HookView.Live` is false, every deliverable pending id
  is in `NotPushed`, the prompt brings back the pending counts with `hooktext.Stalled`, and Stop
  blocks once per pending set. The stall clears at the next full write.
- **An earlier run's entries (medium).** When a link takes a store whose `run` is not this run
  (after `Ends.Close` and `BeginRun`), each `awaiting` entry is declared missing at once in one
  write (its items back to pending `repeat` and `held`, out of the ledger; the old session's
  transcript is not this session's) and every `missed` entry is dropped. So no boot-clock time from
  another boot is ever compared (n3e-D9 holds within one run).
- **The transcript open (medium).** The FIFO test is right and the open is changed to match it:
  `proof.Scan` opens TB1's way, `O_PATH|O_NOFOLLOW`, `fstat`, refuses anything but a regular file
  by name, then reopens through `/proc/self/fd/<n>` with `O_RDONLY|O_NONBLOCK|O_CLOEXEC` and checks
  device and inode again. A FIFO is never opened for reading.
- **Tagged tests run in the gate (medium).** n3e-D19 widens: n3e-6 (the first part that adds
  `busfailpoints` tests in n3e) adds one Go gate row, `go test -race -tags busfailpoints` over
  `./internal/teamchat/...`, in its own hunk, for the lead to approve; n3e-13 adds its fixed seeds
  under the same row. Until the row merges, each part runs its tagged tests by hand and reports it.
- **execx (medium).** n3e-5 runs the inner command with `execx.Starter.Start` and `Cmd.NewSession`
  as built; only the group kill (`unix.Kill(-pid, SIGKILL)`, behind a seam) and the bounded pipe
  read are new.
- **hooktext imports no client (medium).** `hooktext.Recovery(message string) string`; the hook
  passes `client.ReadDown(run).Error(nil, boot).Message`. So the engine and the link, which import
  hooktext, never import the CLI package.
- **n3e-6 is split (medium).** The pure `member/deliver` package (`Choose`, `Build`, its goldens)
  moves to n3e-1, which becomes "the inbox socket, fakeinbox and the delivery builder" (three
  packages; `deliver` imports `proto`, `state`, `paths` only, so it needs nothing from n3e-3).
  n3e-6 keeps the engine loop and the link hunk. n3e-6's API for `deliver` is unchanged.
- The review's low findings are wording and test-list points; each part's builder applies the
  one for its part from the review report.

## 15b. Corrections from the brief review (round 2)

These override 15a where they differ (`261022-n3e-brief-review-2.md`).

- **The delivery budget is built (major).** 15a's `rate` count is wrong: `rate` is the receive
  limit, which refuses human messages too. B 2.7.3's budget (A 11.1 `deliver_per_sender`: an
  agent sender's PEER, PING and member-sent NOTICE items admitted at most 10 per team per rolling
  600 s, `proto.DeliveryWindowMs`; HUMAN, TIMEOUT and admin items never held back) is built nowhere.
  n3e-7 builds it in teamsync, applied as an item would enter `pending` (in the batch's admission
  step), a window per (team, sender) in memory; an item over it never enters `pending` and adds
  one to `Batch.HeldBack[sender]`. Tests: T19's flooding peer (11th item in 600 s held back, the
  window sliding on the fake clock), HUMAN and TIMEOUT never held.
- **The render count cannot deadlock (medium).** `OnRender` is not called on the state goroutine
  into the link. The engine keeps `Unusable.Render` itself (counted on the state goroutine when a
  slot fails) and reports it through the existing `OnState`-style path: `Config.OnUnusable func(state.Unusable)`, which must not block, and the link writes session.json from it off the
  goroutine. The hook-stdin count goes the other way through `SetUnusable`. n3e-10 owns the
  `unusable` object in `status`'s `runtime` record and `unusable_hook_stdin` in `status.json`.
- **An earlier run's awaiting entries (medium).** n3e-9 owns this (in `proof.go`, run at the
  team's first load in a new run). The items go back to `pending` marked `repeat` and **not**
  `held`: a new session never saw the old socket, so they are delivered on the new socket (B 2.7.3:
  `held` is for "pushing them again into a session that holds socket messages"); their ids leave
  the ledger in the same write; every earlier `missed` entry is dropped.
- **`WriteUser` that wrote nothing (medium).** When `WriteUser` returns `ErrRetry` after the
  awaiting entry was persisted (no byte of the user line written), the engine removes the entry
  and returns its items to `pending` with no `repeat`, in one write, and retries in 10 s, as for a
  connect failure: nothing reached Claude Code. Only `ErrPartial` leaves the entry `unknown`.
- The four low findings of round 2 go to the builders with round 1's.

## 16. B27 and the owner's offline rule, carried

**B27** (DESIGN-v2-B-member.md 1.5, in full): "The status line always exits 0, wraps the
session's own status line under v1's rules (process group, 10 s timeout, cancel, never runs
itself), refreshes every 25 s, inside the hooks-daemon supervisor's 30 s sidecar freshness, and
costs one exec and one file read for the team segment, which has its own cache separate from the
hooks-daemon line [R26; ST; Plan 00213]. It keeps what Plan 00217 gives v1's status line: each
team in its own colour, and the session's own short member name beside the team (for example
`alpha: fedora-desktop.2 (c)`). A team's colour override is a room event that only the admin
writes [Plan 00217]."

Where each clause lands: exit 0 and v1's inner rules, n3e-5; 25 s, built (`kit.StatusLineRefresh`)
and pinned by n3e-11; one exec and one read, `status.json` as the segment's cache (B 2.9: pushed on
change, no TTL), n3e-7 and n3e-5; each team's colour (palette and hash), n3e-3, drawn by n3e-5; the
short member name, `status.json` `member`, n3e-3 and n3e-7; the override as an admin-only room
event, written by n3e-8, read by n3e-7.

**OF1, the owner's rule**: an offline member or an unreachable server is shown, never blocks; the
status line and the hooks always exit 0 (the status line's one other exit is the cancel, 128+n;
the hook's one other is the parser's 64 for an event argument `hooks.json` never passes,
n3e-D15). In n3e: no hook op makes a network call; a down team is `(down)` in the status line and a
prompt line, and never a Stop block; a team whose server is unreachable never holds another team's
delivery (one team per delivery, the team goroutines apart); a socket that is gone stops delivery
and nothing waits on it; every part's OF1 test runs one team stopped beside a running one.

## 17. Decisions n3e takes (the spec is silent)

| ID      | Decision                                                                                                                                                                                                                                                                                                                      |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n3e-D1  | `held back:` counts: `Batch.HeldBack` (agent sender -> items refused `rate` in that batch), summed per team in the engine's memory until the team's next delivery renders it; a link restart loses the count (the items stay in `history`)                                                                                    |
| n3e-D2  | `status.json` rows gain `member`, `colour` (always the effective stored hex) and `stalled`, so the status line still reads one file                                                                                                                                                                                           |
| n3e-D3  | the role letter is `c`, `w` or `p` (probe); a null or unknown role is `role?`; no handle yet is `member?`                                                                                                                                                                                                                     |
| n3e-D4  | the override is `teamchat.colour`, key `""`, content exactly `{"colour":"#rrggbb"}` or `{}`, written by the admin with the room's other desired state (only when it differs); read from the admin alone; held in memory, re-read at each verification; no file of its own                                                     |
| n3e-D5  | the description is the main room's topic, read by `proto.MainDescription`; the topic and the colour join the sync filter's state types                                                                                                                                                                                        |
| n3e-D6  | proto's MEMBERSHIP gains the farewell texts: `released` "released from this session by the owner; you are still on the team", `parked` "parked by the owner", none "taken out of this session". Proposed A 13.3 text for the lead                                                                                             |
| n3e-D7  | a MEMBERSHIP delivery is not transcript-proven: it has no store; `told` is written at its full write, as DV6 says; it shares the 2 s pace                                                                                                                                                                                     |
| n3e-D8  | "between two stores' writes of one delivery": a delivery is one team's store, so the point is hit between consecutive store writes of recv's hand-out, `Ack` and `Return`, the operations that write several stores                                                                                                           |
| n3e-D9  | awaiting `at_ms`, `stop_ms` and missed `at_ms` are CLOCK_BOOTTIME ms (CK1: deadlines); a session never spans a boot                                                                                                                                                                                                           |
| n3e-D10 | the transcript's inode is not persisted; a restarted link scans from the persisted offset, and a file shorter than it from 0                                                                                                                                                                                                  |
| n3e-D11 | `status.json` `missing` is the missed entries since the team's last proof (memory; after a restart every missed entry counts until the next proof). (b) Stop's and the prompt's 10-minute failure line share `stop_guard.failure_ms`                                                                                          |
| n3e-D12 | Stop's 10-minute team-failure block is for a `failed` team only, never for `down` (the offline rule)                                                                                                                                                                                                                          |
| n3e-D13 | n3c-D23's NOTICEs: a stopped team is told by a `stopped:` line in its next delivery, or a trailing-only one, once per stop; a refused add and a refused desired file are told once per link life on the next prompt, and on every SessionStart while they hold. A 13 has no locally made NOTICE, so neither is a NOTICE entry |
| n3e-D14 | a trailing-only delivery is sent at most once a minute per team (A 13.1), is not persisted as awaiting (it has no items), and its facts are cleared at its full write                                                                                                                                                         |
| n3e-D15 | the hook exits 64 only for an event argument outside `kit.Hooks` (the parser's refusal); every other case, a link down or a server unreachable included, is exit 0                                                                                                                                                            |
| n3e-D16 | the inner status line keeps S5-S15 with `TEAMCHAT_STATUS_LINE_INNER`; it skips `teamchat status-line` and `<path>/teamchat status-line`; its settings errors carry no Python exception names (v2 shares nothing with v1)                                                                                                      |
| n3e-D17 | a free link.lock with the keeper holding keeper.lock and the link `running` (not yet ready) is a red `teamchat starting`                                                                                                                                                                                                      |
| n3e-D18 | the hook ops are served by the link, not `engine.Serve`: the answer is one raw JSON line, then `{"done":{"exit":0}}`                                                                                                                                                                                                          |
| n3e-D19 | PT1's fixed seeds join the Go gate as a `busfailpoints` test row (no gate runs tagged tests today); the lead approves the gate hunk                                                                                                                                                                                           |

## 18. Common rules for n3e part builders (paste as the part brief's preface)

```text
COMMON RULES FOR n3e PART BUILDERS (Plan 00209 N3, slice n3e; product name teamchat)

Plan folder: CLAUDE/Plan/00209-agent-bus-v2-in-go/. Your report goes in its subagent-reports/.

Your brief is your part's section of subagent-reports/261022-n3e-handoff.md in your worktree. Read
section 0 (the order), section 2 (what n3e builds on, quoted as built), your own part's section,
section 16 (B27 and the offline rule) and section 17 (the decisions). The brief digests the design
for you. Open SPEC-n3.md or a design chapter only where your section cites a point that is unclear,
and then by grep. Earlier builders ran out of context by reading widely. Owner direction: v1 is a
prototype, not an archetype; make it excellent.

Rules:
- Work only in your own worktree: absolute paths and `git -C <wt>`. Never touch /workspace itself
  or any other worktree.
- TDD (the test file before the source; a hook enforces it). Run go test -race -cover on your
  packages, and golangci-lint run from src-go with no tags, with --build-tags tuwunel and with
  --build-tags busfailpoints. Hold 90% coverage per package. No //nolint. No time.Now in tests.
  Use named results instead of `x, _ :=`.
- Run ./scripts/qa-changed.bash before each commit. It should be fully green. Any red is yours.
- Tuwunel-tagged tests: FAKEHS_TUWUNEL_BIN points at the binary in
  /workspace/untracked/worktrees/bus-v2-n1-hostchecks/untracked/scratch/tuwunel-dl/. Run them if
  your part touches matrix, the sync or the rooms.
- Commit after each green step. Messages: 'Plan 00209 N3 n3e-<n>: ...', ending with:
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
- Do NOT edit PLAN.md or the design chapters; the lead does that at merge. Keep edits to shared
  files (.golangci.yml, src-go/qa/rules/table.go, the go-deps allowlist, the engine's and the
  link's shared functions) minimal and in their own hunks, and only in the files your section owns.
- Where your part meets n3d, use the fake section 2.3 names; if n3d has merged, use its code and
  say so.
- The offline rule: an offline member or an unreachable server is shown, never blocks; the
  status line and the hooks always exit 0. Your part carries a test with one team stopped beside
  a running one.
- Never push, qa-all, Ansible, sed, stash, amend or rebase. Chain bash with &&. No pipes to head
  or tail; redirect to untracked/scratch/ and grep. No journal edits, no time estimates, no
  install-specific names.
- Gates: inert code only; no playbook, unit, tmpfiles or meta-deploy entry.
- Trust boundary: no-follow opens, never open a FIFO, never decide from a file another uid can
  write. Never write or log the inbox token.
- No review loop of your own; the lead runs one Opus review over the merged slice. If the brief
  is wrong or the built API differs from it, decide the simpler, well-tested way and record it.
- CONTEXT BUDGET: if you near it, write subagent-reports/261022-n3e-<n>-handoff.md, commit it,
  and stop.
- At the end, write subagent-reports/261022-n3e-<n>.md: what was built, the exported API as built
  (exact signatures, for the later parts), coverage, and decisions. Final message under 700
  characters.
```

## 19. Reports

Each part writes `subagent-reports/261022-n3e-<n>.md` (and `-review`, `-review-2`, …). A builder
that stops at its context budget writes `261022-n3e-<n>-handoff.md` first. This brief's own
reviews are `261022-n3e-brief-review.md` and `-review-2.md`.
