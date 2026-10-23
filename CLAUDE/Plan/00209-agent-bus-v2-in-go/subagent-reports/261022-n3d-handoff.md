# n3d handoff: the sends, split into seven parts (Plan 00209 N3, slice n3d)

Design only; no code is written here. n3d is SPEC-n3.md section 5 (SD1-SD5), with the halves of
n3c's expectations that section 4 left to it: SY4's "an answer to an own ping updates the outbox"
(n3d), SY4a's own sends recorded at their send, LN5's `outbox` records, SY7 case 4 (a reply held
only by a discarded answer leaves the ping unanswered and its TIMEOUT made), and PT1's failpoint
"after a send's PUT before its answer is recorded". It is split into seven parts, D1 to D5, D6a
and D6b, and a closing wiring step W. Each part is one worktree, one branch and one builder, and
merges into `feat/bus-v2-n3d`. Round 1 of the brief review (`261022-n3d-brief-review.md`) is
applied throughout.

Base: `feat/bus-v2-n3d`, cut from `feat/bus-v2-n3c` at 6c3a64ab0; `feat/bus-v2-n3c` was merged in
again at e32fe4719, so P1-P6 are all in.

**Lead's note: P6 is now BUILT.** Where this brief marks a P6 API (`member/ends`,
`member/client`, `seat disconnected`, `reach`) "as designed, not yet built", the API as built
in `261022-n3c-p6.md` overrides it. D5 need not wait. Any part that adds methods to
`team.Engine` also updates P6's fake `team.Engine` (in the member/ends and member/client tests)
in the same commit.

Every API quoted as "built" is taken from the part reports named in each quote (`261022-n3c-p2.md`
to `-p5.md`, `261022-n3a*.md`, `261021-n1-*.md`), or, where marked "(review)", from what the brief
review read in the code. Section 2c lists the built APIs no report quotes in full; a builder reads
those with `go doc` before writing against them, and records any difference from this brief. Each
signature a part adds is that part's decision: a builder may change it only by recording the
change and its reason in its part report.

## 0. Order

| Wave | Parts                      | Needs                                  | Why                                                                                                                                                                                                                       |
| ---- | -------------------------- | -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1    | **D1** (shared types) ∥ D2 | the base                               | D1 is what every later part compiles against: the store's new fields, the roster cache file, the new records and exit codes, the three send ops' args, the `team` additions, two failpoint names. D2 is the forge package |
| 2    | D3 ∥ D4                    | D1 merged                              | D3 is the engine's outbox, TIMEOUTs and roster cache; D4 is teamsync's receive side. Different packages; they meet through `team.Batch` and `team.Engine` only, against fakes                                             |
| 3    | D6a ∥ D5                   | D3 merged (D6a); D1 and P6 merged (D5) | D6a is the engine's `say` and `ping`, the charge, the sender and the restart recovery, on D3's outbox code. D5 is the client side of `reply`, `say`, `ping`, in P6's `member/client`                                      |
| 4    | D6b                        | D6a merged                             | `reply`: the ledger cases, carried items, unknown ids, `--verb`, on D6a's charge and sender                                                                                                                               |
| 5    | W                          | D2-D6b and P6 merged                   | the forge checker given to the engine and to teamsync, the three verbs in `cmd/teamchat`, the end-to-end exchange under `--plain`, the restart and failpoint real-process tests                                           |

D3, D6a and D6b are all in `member/engine`, so they are sequential. D2 is used through an interface
(`team.Forge`, D1) by D4 and D6a, each against a fake, so neither waits for D2; only W needs it.

Every part keeps SPEC-n3 section 1 (TB4 for member files, CK1's clocks), the 90% coverage floor per
package, and `-race`. No test sleeps (no `time.Now` in tests). `os/exec` is reached only through
`execx`; a run-time command name (`gh`) needs its `dynamicNames` row in `src-go/qa/rules/table.go`.

## 1. The owner's offline rule, as n3d carries it

**An offline member or an unreachable server never blocks anything. A send to a team whose server
is down is held in the outbox and reported, never lost, and never blocks other teams.** Each part
holds to it in these places:

- **No network call on the state goroutine** (B 2.4.3, as P4 built it). Every PUT, GET and forge
  request runs on a team's own goroutine, a team's sender goroutine, or a request's connection
  goroutine.
- **One sender per team** (D6a). Every PUT a team's sends make goes through that team's sender
  goroutine, in `sending` order. A slow or dead homeserver holds only that team's queue.
- **A send to a down team is queued, not failed** (n3d-D2). It is composed, checked, charged and
  recorded in `sending` with its content in one store write, and the verb answers at once with a
  `sent` record `{"id":null,"queued":true}` and exit 0. The sender posts it, by the same
  transaction id, when the team is next up. `status` lists it as a `queued` record until then.
- **A send whose tries all fail transiently is queued the same way** (n3d-D2): the request's 30 s
  ends, the verb answers `queued`, and the PUT goes on under the sender.
- **A team stopped permanently keeps its queued sends** (n3d-D2): a 401, a 403 or lost trust stops
  the sender, not the queue; the sends are posted when the team runs again (a bundle change, a
  desired-generation change, the next start, SY8). Only a send whose op is still waiting is
  removed and answered 21 or 22, since the agent sees that failure.
- **A link started while its homeserver is down still queues** (n3d-D3): the last verified roster
  of each team is kept in `roster.json` beside the store, so sends can be composed before the
  first verification.
- **Queued sends survive a link restart** (n3d-D1): the content is in the store, and the new link
  recovers them (n3d-D4) before its team's first long-poll. They are lost only by the owner's acts
  (a removal, a `seat set-aside`, n3d-D23) or when the current roster refuses them at their post
  (n3d-D27), each said in `link.log` and counted in `status`.
- **A ping to an offline member** is an ordinary ping: it is posted, and the addressee's silence is
  a TIMEOUT. The TIMEOUT timer is on the state goroutine and needs no network, so a team that is
  down or stopped still makes its owed TIMEOUTs (B 2.6 step 08: "the stopped team keeps its store,
  and with it its owed TIMEOUTs").
- **The forge** (GitHub) is a server too. On the receive side one batch's checks run concurrently
  under one 30 s budget, and a forge found unreachable is not asked again for 60 s (n3d-D29), so a
  forge outage costs a team's sync at most 30 s per batch; the ping is dropped by A 9.3's rule and
  its sender gets a TIMEOUT, so the loss is said (n3d-D10). On the send side a forge with no answer
  is 20 `forge`, with nothing charged (n3d-D11).

## 2. What n3d builds on

### 2a. Built, quoted from the reports

`member/team` (`261022-n3c-p2.md`, "team"):

```go
type Batch struct {
	Team paths.TeamID; NextBatch string; RestoreEpoch int64; Fresh bool; Items []Item
	Floors map[string]int64 // keyed by room purpose (proto.PurposeMain, PurposeQuestions)
	Answered []proto.Answered; Redacts []string; Gap bool; Counts Counts
}
type Item struct { Pending state.Pending; Verdict proto.Verdict }
type View struct { SeenIDs map[string]bool; Events map[string]proto.Related; OwnPings map[string]proto.OwnPing }
type Health struct { Down bool; Stopped string; Role string; LastSyncOKMS int64 } // 0 before the first sync
var ErrRemoved error
type Engine interface {
	Load(t paths.TeamID, userID string) (state.Store, error)
	View(t paths.TeamID, ids, relations []string, r proto.Roster) (View, error)
	Persist(ctx context.Context, b Batch) error
	ResetPosition(ctx context.Context, t paths.TeamID, epoch int64) error
	SetEnded(ctx context.Context, t paths.TeamID, e state.RunEnd) error
	BeginRun(ctx context.Context, t paths.TeamID) error
	SetHealth(t paths.TeamID, h Health)
}
type Team interface {
	Run(ctx context.Context) error
	Refetch(ctx context.Context, room, id string) (proto.Verdict, error)
	Verify(ctx context.Context, id string) (records.Verified, error)
	History(ctx context.Context, room string, limit int) ([]records.History, error)
	Notices() Notices
	Wake()
	FlushFloor(ctx context.Context) error
}
type Notices interface {
	notice.Client // RoomState, Messages, Send
	SetPresence(ctx context.Context, userID, presence string) error
	Anchors() proto.Anchors
	Roster() (proto.Roster, bool)
	RecordOwnSend(id string, r proto.Related)
	Token() string
}
```

`Refetch` takes a room **purpose** (`proto.PurposeMain`), not a room id (`261022-n3c-p4.md`).
`Notices()` works before `Run` and after it returns; `Roster()` is false until a verification
(`261022-n3c-handoff.md` 8a, P5). A retried team is a new `NewTeam` (P4).

`member/engine` (`261022-n3c-p4.md`):

```go
type Config struct {
	Root, RunRoot *homefs.Root
	Layout paths.Layout; Run paths.RunID; Clock clock.Clock; Log io.Writer
	NewTeam func(TeamConfig) (team.Team, error)
	NewEnds func(team.Engine) team.Ends
	Delivery string // socket | wait | none
	Plain bool; FirstSessionStart <-chan struct{}
	OnState func(wire.State)
	Runtime func() (records.Runtime, error)
}
type TeamConfig struct { Team paths.TeamID; Seat paths.Seat; Lock *lock.SeatLock; RemovedUserIDs []string
	Engine team.Engine; Ends team.Ends; Root *homefs.Root; Plain bool; FirstSessionStart <-chan struct{}; Clock clock.Clock; Log io.Writer }
type RecordWriter interface { Record(records.Record) error; Flush() error }
type Removed struct { UserID string; Notices team.Notices }
func New(cfg Config) (*Engine, error)
func (e *Engine) Run(ctx context.Context) error
func (e *Engine) Add(ctx context.Context, t paths.TeamID, l *lock.SeatLock, removedUserIDs []string) error
func (e *Engine) Remove(ctx context.Context, t paths.TeamID) (Removed, error)
func (e *Engine) Serve(ctx context.Context, req wire.Request, out RecordWriter) wire.Done
func (e *Engine) Ack(ctx context.Context, ack string) error
func (e *Engine) Return(ctx context.Context, ack string) error
// plus Ends, Refuse, SetDesired, EndTeams, Shutdown, and team.Engine
```

How it works (`261022-n3c-p4-handoff.md` "How the engine works"): `do(ctx, f)` runs f on the state
goroutine, which calls no team method and makes no network call; `Serve` answers ops it does not
know with "unbuilt" 1 `internal`; `Persist` is one write (position, floors, new items, the
redactions, the counts), then wakes the waits, and ignores `Batch.Answered`; a full pending list
refuses the whole batch; `status` prints `team`, `member`, `room` and `runtime`, `outbox` "empty
until n3d"; `single()` resolves a single-team verb's team through `cli.ResolveTeam`, the ledger
being the team whose store holds the event id; `internal()` never quotes the error. (review):
`render.go`'s `timeoutRecord` takes a TIMEOUT's `verb` and `ref` from the store's outbox ping by
the key's event id; `seam.go` builds a ledger entry's `Related` with `Verb` empty and an outbox
ping's with `slices.ContainsFunc(p.To, r.HumanReader)`-style predicate for
`AddressedToRosterAgent` (use the same predicate for own sends); `serve.go`'s `teamDone` maps a
context end to 4 `timed-out`.

`member/teamsync` (`261022-n3c-p5.md`):

```go
type Config struct {
	Team paths.TeamID; Seat paths.Seat; Root *homefs.Root; Run paths.RunID
	Engine team.Engine; Ends team.Ends; Clock clock.Clock; Watcher Watcher
	RemovedUserIDs []string; Plain bool; FirstSessionStart <-chan struct{}
	Log io.Writer; HTTP func(matrix.Config) (*matrix.Client, error); Jitter func(time.Duration) time.Duration
}
func New(cfg Config) (*Team, error)
func (t *Team) Run(ctx context.Context) error
func (t *Team) Notices() team.Notices
func (t *Team) Roster() (proto.Roster, bool)
func (t *Team) Refetch(ctx context.Context, room, id string) (proto.Verdict, error)
// plus Wake, Verify, History, FlushFloor; WatchSeat
```

Files: `session.go` (verify, SY1, the long-poll, an answer's steps), `lookup.go` (SY4a's resolver;
the own-sends map, 1024, memory only, n3c-D11), `ln6.go` (Refetch and the notices). SY7 case 4 "is
the engine's work, n3d's".

`member/wire` (`261022-n3c-p2.md`): `OpReply`, `OpSay`, `OpPing` exist; `Deadline` gives sends
30 s; `DecodeArgs(raw, out)` is strict (10 `schema`); every args struct carries
`Team *paths.TeamID` with the `team` key always present; `Request.Stdin *string`.

`clock` (`261022-n3c-p2.md`): `Clock{Boot, Mono, WallMS, Sleep, AfterFunc, Hold}`; `Fake` with
`Advance`, `Suspend`, `StepWall`, `Waiters`, `BlockUntil`. Waiters fall due on Mono.

`failpoint` (`261022-n3c-p2.md`): `type Name string`; `func Hit(name Name)`; an unknown name in
`TEAMCHAT_FAILPOINTS` panics at the first `Hit`; the release gate scans the binary for every name.

`member/state` (`261022-n3a.md`, `-writer.md`, `-state-handoff.md`): `StoreWriter`
(`NewStoreWriter(root, l)`, `Load`, `Write(Store)`, `Remove`, `Close`); `Add*` growth methods refuse
a full list with `ErrFull`/`*FullError{List}`; pending agent items stop at 2048-256 and HUMAN and
TIMEOUT items use the rest; `ledger` and `missed` roll; `gate.credit` is an integer,
`proto.Bucket`'s credit (one token 60 000); a TIMEOUT item's id is `proto.TimeoutKey`; send kinds
`text`, `ping`, `notice`; caps 256 outbox pings of 3 KiB with at most 16 targets, 64 sends of
128 B, 64 gate keys of 96 B, entries of pending and ledger 320 B, file 2 MiB; the store is
written only by a `StoreWriter`; `state.Read`/`state.Write` write the other member files. The
store's `v` stays 1: no store has been written outside tests (the code is inert), so new keys are
required keys, with no legacy reader.

`member/cli`, `member/exits`, `member/records` (`261022-n3a-cli.md`): `ResolveTeam(flag, ledger *paths.TeamID, active []ActiveTeam)` (CL6); `exits.New(code, team, Template, fills...)` takes its
exit from the code's set in `Table()`; every B 2.2.2 record kind as a typed struct, `outbox` and
`sent` among them; `RunFunc` and `Env.Verbs` (`261022-n3c-p2.md`). The parser handles every verb
and flag of B 2.2.1, `reply`, `say` and `ping` included; a bare `ping` is already 64 listing A
8.1's verbs that are not answers (`261022-n3a.md` decision 09).

`proto` (review, and `261021-n1-proto.md` decision 35): `SendGate{Bucket, Recent DupWindow}`;
`SendGate.Admit(l, key, dedupe, nowMs)` "records one send, or returns its refusal (duplicate,
rate) having recorded nothing"; `Composer.Admit(out)` (the report window, then the gate);
`DuplicateKey(verbOrKind, ref, re, to, text)`; `Bucket.Take` (there is no `Bucket.Admit`);
`proto.ForgeCheck` (the verdict's forge need, with `Trust()`) and `proto.Forge` (the roster's forge
section); `proto.Answer.OnTime`, judged against `OwnPing.OriginTs`; the probe is addressable for an
answer only when `Lookup.Event(root)` gives `Verb == "check"` from the probe (`probeCheckOf`).

`matrix` (SPEC-matrix M22, MB10): `Send` is `PUT /rooms/{r}/send/{type}/{txn}`; a given txn is
`[A-Za-z0-9._~-]{1,64}`; an `Unreachable` answer is retried with the same txn, 3 tries 1 s apart
through the client's `Clock`; nothing else is retried.

### 2b. P6, as designed, not yet built (`261022-n3c-handoff.md` sections 7 and 8a)

```go
// ends
func New(cfg Config) team.Ends // Config{Clock, Log, Engine team.Engine, Run paths.RunID, switchOverride}
// client
func Main(verb string) cli.RunFunc   // recv, wait, inbox, status, verify, history
func SeatDisconnected(e cli.Env, inv cli.Invocation) term.ExitCode
func Reach(e cli.Env, inv cli.Invocation) term.ExitCode
```

The client: `TEAMCHAT_RUNTIME` unset is 69 `no-session`; otherwise it connects to `link.sock`,
sends one `wire.Request` with `deadline_ms` from `wire.Deadline` on `clock.Boot()` and `kit` the
CLI's own `cli.Env.Digest`; copies records to stdout and the error to stderr; exits with
`done.exit`. LN3's four no-link answers within 300 ms; only `wait` reconnects. The ends post
notices with `proto.Composer.Notice` and `Notices.RecordOwnSend`; notices never go through
`sending` and are exempt from the duplicate window (A 11.2).

### 2c. Built, but no report quotes it: read with `go doc` first

| Package          | What to read                                                                                                                                                                                                                                                                             | Used by        |
| ---------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------- |
| `proto`          | `Composer` (`Text`, `Ping`, `Admit`, its `Lookup`), `SendGate`, `DupWindow`, `DuplicateKey`, `Bucket.Take`, `OwnPing`, `Verdict` (`Answer`, `Forge`, the `Record` and `Drop` outcomes, its `Reason`s), `ForgeCheck`, `Forge`, the roster decoder and `Limits`, `TimeoutKey`, `Localpart` | D1, D3, D4, D6 |
| `matrix`         | `Send`'s signature and how it takes a txn; `Error.Transient()`                                                                                                                                                                                                                           | D6a            |
| `notice`         | `Client`'s `Send` and `Messages` signatures                                                                                                                                                                                                                                              | D6a            |
| `member/state`   | `Store`'s Go types for `pending`, `ledger`, `outbox` (`pings`, `sending`, `gate`) and their `Add*` methods                                                                                                                                                                               | D1, D3, D6     |
| `member/records` | `Outbox`, `Sent`, `Timeout`, `Ping`, `Team` (its counts) fields                                                                                                                                                                                                                          | D1             |
| `member/exits`   | `Table()` rows for 13, 14, 20; how a proto refusal reason maps to an exit and code                                                                                                                                                                                                       | D1, D6         |
| `execx`          | `Runner`, `Fake`'s scripted run form                                                                                                                                                                                                                                                     | D2             |

## 3. The design facts every part shares, digested

- **The store's outbox** (B 2.3.2): `"outbox": {"pings": [{"id","verb","ref","to":[user ids],"sent_ms","due_ms","answered":[],"reported":[]}], "sending": [{"txn":"tc<32 hex>","room":"main","kind":"text","since_ms"}], "gate": {"credit","at_ms","recent":[{"key":"<sha256>","at_ms"}]}}`.
  "`sending` holds a send whose transaction id is chosen but whose answer has not arrived; a link
  that restarts re-sends it with the same transaction id, which the homeserver stores once (M22)."
  "`gate` is the send gate: a send's token is charged in the same store write that records it in
  `sending`, before the network call, so a send that fails has still spent its token (RP 5.16)."
  "`answered` and `reported` in a ping are indexes into its `to`, and `reported` makes one TIMEOUT
  per silent target (RP 5.14)." "A send past the outbox or `sending` cap exits 78 `store`."
- **The send order** (A 9.3): "class and role; the question index's number and relation (`schema`,
  `target`); the body form (`body`); the receiver's own rules over the built content (decode, then
  the roster); the secret check (`secret`); an open duplicate question (`duplicate`, n3f's); then,
  in B, the forge check, then the flood budget (`Admit`, 11.1-11.2), which a refused send never
  spends. A send the forge check refuses spends no token and does not enter the duplicate window."
  `Admit` is the duplicate window, then the bucket (proto's `SendGate.Admit`).
- **The duplicate window** (A 11.2): "60 s on `[verb or kind, ref, re, sorted to, text digest]`;
  absent parts are empty; a ping's digest is empty; notices exempt." The digest is the SHA-256 of
  `teamchat.text`'s `text` (`proto.DuplicateKey`). 15 `duplicate`, "it was already sent; do not
  resend".
- **The rates** (A 11.1): `send_per_minute` (default 20, 1-60) refills the bucket per minute and
  `send_burst` (default 10, 1-30) is its capacity, from the roster's `limits`. 14 `rate`.
- **The verbs** (A 8.1): `fetch`, `sync`, `review`, `run-qa`, `halt`, `check` expect an answer;
  `ack`, `nack`, `done`, `blocked` do not. The reply relation is forbidden for the first six,
  required for `ack` and `nack`, optional for `done` and `blocked`. `ref` is required for `fetch`,
  `sync`, `review`, `run-qa`, `done`, `blocked`, optional for `nack`, forbidden for `halt`,
  `check`, `ack`. `branch` only beside a `path` or `commit` reference. Senders: `fetch`, `sync`,
  `halt` the coordinator; `check` the probe only, with exactly one addressee; the rest the
  coordinator and workers. Addressable: a non-parked coordinator or worker; for `ack`, `nack`,
  `done`, `blocked` also a listed human, and the probe for an answer to its own `check` only. A
  worker's ping or text that names a human copies the coordinator (P21).
- **The answer form** (A 7.2): "a reply in the ping's thread: `{"rel_type": "m.thread", "event_id": <ping>, "is_falling_back": true, "m.in_reply_to": {"event_id": <ping>}}`. The answered
  ping is the thread root." A reply in main is `{"m.in_reply_to": {"event_id": <id>}}`; main allows
  no other relation (A 7.1).
- **A carried ping** (A 7.7): "an answer (the answer form, rooted at the carried event) to a carried
  `ping` is addressed to its `from` when `from` is a current member, and is otherwise refused
  `target`." A carried item is delivered as a NOTICE and is never forge-checked.
- **Answers and deadlines** (A 8.2): "An answer is an `ack`, `nack`, `done` or `blocked` whose reply
  relation names a ping this member sent, from a sender that ping addressed." "The deadline of a
  ping is its own `origin_server_ts` plus its timeout: `halt_ack_timeout_s` for `halt`,
  `ack_timeout_s` for the other verbs that expect an answer. An answer counts when its
  `origin_server_ts` is at or before the deadline, however late the member reads it." "The sender
  learns its ping's `origin_server_ts` when its own event comes back in sync (rule 3 of 7.6); until
  then B uses its local send time."
- **TIMEOUTs** (A 8.3): "When the member's clock passes a ping's deadline plus a 30 s grace, each
  addressee that has not answered gets exactly one TIMEOUT, keyed `<ping event ID>~<addressee localpart>` and marked reported." "A TIMEOUT is never retried and spends no flood budget." B 2.6
  step 07: "Due TIMEOUTs come from the outbox on the state goroutine's timer (the earliest
  `due_ms`), not from a tick."
- **What is delivered** (A 8.4): a ping that expects an answer, and `nack`, `done`, `blocked`, are
  PING entries; an `ack` is "no entry of its own: recorded, and the next delivery for another
  reason carries an `answered:` line for it"; an on-time answer read after its TIMEOUT gets an
  `answered:` line ending ` (after its TIMEOUT)` (n3e renders; n3d records).
- **The records** (B 2.2.2): `outbox` is `{"team","id","verb","ref","to":[short names],"due_ms","answers":[{"from","verb","id","late"}],"timed_out":[]}`, "one ping this member sent that still
  expects an answer or was answered in the last `ack_timeout_s`"; `sent` is `{"team","id","room"}`.
- **The forge check** (B 2.6 step 11, A 9.2-9.3): see D2.

## 4. D1: the shared types (lands first)

Packages: `member/state` (with one name in `paths`), `member/records`, `member/exits`,
`member/wire`, `member/team`, and two names in `failpoint`. Each change is small and additive; one
builder can hold them because none has logic beyond its codec and its tests. No new import edges.

### API

```go
// member/state: the sending entry carries what a re-send needs (n3d-D1)
type Sending struct {
	Txn     string          `json:"txn"`      // ^tc[0-9a-f]{32}$ (n3c-D18)
	Room    string          `json:"room"`     // main (side rooms are n3f's)
	Kind    string          `json:"kind"`     // text | ping
	SinceMS int64           `json:"since_ms"` // wall ms of the charge
	Type    string          `json:"type"`     // m.room.message
	Content json.RawMessage `json:"content"`  // the composed content, a strict object, at most 16384 bytes (A 11.2)
	Tried   bool            `json:"tried"`    // a PUT may have reached the server (n3d-D4)
	Ping    *SendingPing    `json:"ping"`     // set for a ping whose verb expects an answer, else null
}
type SendingPing struct {
	Verb      string   `json:"verb"`
	Ref       *string  `json:"ref"`
	To        []string `json:"to"`         // user ids, 1-16 (n3d-D5)
	TimeoutMS int64    `json:"timeout_ms"` // ack_timeout_s or halt_ack_timeout_s at the charge
}
// Pending and Ledger entries gain Verb *string `json:"verb"`: the ping's verb for an item "ping",
// null otherwise (n3d-D30). It fits the 320-byte entry.
// Caps (n3d-D1, n3d-D24): sending 32 entries of at most 20 KiB (B 2.3.2's 64 of 128 B halved);
// gate keys 96 (was 64); the file cap is the new sum of the list caps, rounded up to the next
// 256 KiB (about 2.6 MiB), so a list cap still fires first.
// Keep the built type's Go names where it has these B fields already; add the rest.

// member/state: the roster cache (n3d-D3), <seat dir>/roster.json, 0600, through state.Write
type RosterCache struct {
	V       int             `json:"v"`
	Rev     int64           `json:"rev"`     // the roster's rev when verified (the field go doc shows)
	Content json.RawMessage `json:"content"` // the roster state event's content as verified
}
func DecodeRosterCache(data []byte) (RosterCache, error)
const MaxRosterCache = 64 << 10 // or the largest roster A's caps allow, proven by a test
// paths: func (s Seat) RosterCache() string, registered in sampleNames and zeroDerivations.

// member/records
type Sent struct { Team paths.TeamID; ID *string; Room string; Queued bool } // id null exactly when queued
type Queued struct { Team paths.TeamID; Room string; Send string; Verb *string; To []string; SinceMS int64; Tried bool } // kind "queued"
// Outbox: an answer's verb and id may be null (n3d-D8). Team's counts gain `unsent` (n3d-D27).

// member/exits (n3d-D11): 14 gains code forge-rate; 20 gains code forge; Table(), help exit-codes.

// member/wire
type ReplyArgs struct {
	Team *paths.TeamID `json:"team"`; ID string `json:"id"`
	Verb *string `json:"verb"`; Ref *string `json:"ref"`; ToCoordinator bool `json:"to_coordinator"`
}
type SayArgs struct { Team *paths.TeamID `json:"team"`; To []string `json:"to"`; ToCoordinator bool `json:"to_coordinator"` }
type PingArgs struct {
	Team *paths.TeamID `json:"team"`; Verb string `json:"verb"`; Ref *string `json:"ref"`
	To []string `json:"to"`; ToCoordinator bool `json:"to_coordinator"`; Branch *string `json:"branch"`
}
// The text is Request.Stdin: required for say and a plain reply, null for ping and reply --verb.

// member/team
type Answer struct { Ping, From, Verb, ID string; OriginMS int64 } // an answer-form event to an own ping
type Echo struct { ID string; OriginMS int64 }                       // an own ping come back in sync
// Batch.Answered becomes []Answer (proto.Answered is the delivery's trailing-line type);
// Batch gains Echoes []Echo.
type ForgeResult struct { Outcome string } // proven | provenance | unresolved | forge-rate; trust is ForgeCheck.Trust()
var ErrForgeLogin = errors.New("team: no forge login")
var ErrForgeDown = errors.New("team: the forge did not answer")
type Forge interface {
	Check(ctx context.Context, need proto.ForgeCheck, f proto.Forge) (ForgeResult, error)
}
// Engine gains two methods (D1 gives the engine and teamsync's test fake one-line bodies that
// return nil, so everything compiles; D3 and D6a write the real ones). P6's ends tests have a
// fake team.Engine too: whichever of D1 and P6 merges second adds the same two one-line methods
// to that fake, in the merge commit.
//   SaveRoster(ctx context.Context, t paths.TeamID, rev int64, content json.RawMessage) error // D3
//   RecoverSends(ctx context.Context, t paths.TeamID, n Notices) error                         // D6a

// failpoint (PT1 and n3d-D16)
const (
	LinkAfterChargeBeforeSend Name = "link-after-charge-before-send"
	LinkAfterSendBeforeRecord Name = "link-after-send-before-record" // PT1's "after a send's PUT before its answer is recorded"
)
```

### Facts it needs

- The store's strict decoder round-trips every struct: `content` must be a strict object (proto's
  parse, no duplicate key, no float), measured by `proto.Value.Size` as every entry is. `ping` is
  set exactly for `kind` `ping` with an answer-expecting verb. `verb` in pending and ledger is set
  exactly for item `ping`, and must be one of A 8.1's verbs.
- The roster cache is decoded by its own codec; its `content` is decoded later by proto's roster
  decoder (the engine's, D3), never trusted beyond composing (n3d-D3).
- `records`' `List[T]` encodes nil as `[]`.
- Changing `Batch.Answered`'s type and `team.Engine`'s method set must leave the engine and
  teamsync compiling and green.

### Tests

- A store with 32 `sending` entries at 20 KiB each round-trips; a 33rd is `ErrFull`; an entry one
  byte over is refused; a `content` that is not an object, or holds a float, is refused; `ping` on a
  `text` entry is refused; 96 gate keys fit and a 97th is `ErrFull`; a pending `ping` item with no
  `verb`, or a `human` item with one, is refused.
- The full-store cap test passes with the recomputed file cap.
- `RosterCache` round-trips through `state.Write`/`Read`; the largest roster A's caps allow fits.
- `Sent` queued with an `id` is refused by its encoder; `Queued` round-trips; `exits` builds 14
  `forge-rate` and 20 `forge`, and `help exit-codes` lists them.
- Each new args struct refuses a missing `team` key and an unknown key (10 `schema`).
- The release-build failpoint scan sees the two new names.

## 5. D2: the forge package (SD5)

Package: new `internal/teamchat/member/forge`. Imports: proto, team (for `ForgeResult` and the two
errors), clock, execx, std (`net/http`). A compile-time assertion proves `*Checker` is a
`team.Forge`.

### API

```go
// The gh credential (R44): read through execx when a check needs it, held at most 10 minutes,
// dropped at once after a 401. Never written or logged; Token's String and GoString redact.
type Token struct{ /* unexported */ }
type Login struct{ /* unexported */ }
func NewLogin(r execx.Runner, c clock.Clock) *Login
func (l *Login) Token(ctx context.Context) (Token, error) // team.ErrForgeLogin for a missing or logged-out gh
func (l *Login) Drop()

type Config struct { Login *Login; Clock clock.Clock } // the origin is proto's; a test sets another only through export_test.go
type Checker struct{ /* unexported: caches and the forge's health, safe for concurrent use by every team */ }
func New(cfg Config) *Checker
func (c *Checker) Check(ctx context.Context, need proto.ForgeCheck, f proto.Forge) (team.ForgeResult, error)
```

The command is `gh auth token --hostname github.com`, through `execx.Runner` (a `dynamicNames` row
for `gh`). Its stdout, one trailing newline trimmed, must be 1-1024 bytes of `\x21-\x7e`. `gh` not
found, an exit status other than 0, or a token off that grammar is `team.ErrForgeLogin`.

### Facts it needs, digested

- **When it runs** (B 2.6 step 11): "the link runs `gh auth token --hostname github.com` through
  `execx` when a check needs it, holds the credential in memory for at most 10 minutes and drops it
  at once after a 401, and keeps positive results for 3600 s in memory. A missing or logged-out
  `gh` is a configuration failure of the forge only (exit 78, `forge-login`, on a verb that needs
  it)." A check whose facts are all cached runs no `gh`.

- **The token's use** (A 9.3): "sent only as an `Authorization` header to the origin fixed per
  `forge.kind` in `teamchat/proto` (`https://api.github.com` for `github`, the only kind), and never
  written or logged." A redirect to another origin is refused, and that origin gets nothing.

- **The class** (A 9.2): in `exclude`, drop `excluded`; in `repos`, listed (its `branches`, when
  given, the only branches that prove it); owner `org`, org; else external, which a verb taking no
  external reference drops `allowlist`. Listed wins over org. Where `proto.ForgeCheck` already
  carries the class, the checker takes it and does not decide it again.

- **What proves a reference** (A 9.3):

  | Class                    | Repository facts                            | Branch                                                                                                          |
  | ------------------------ | ------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
  | listed with branches     | owner and name as listed; `fork` is `false` | the ping's `branch` must be listed; the commit is reached from it, or from any listed branch when none is given |
  | listed, no branches; org | owner login equals `org`; `fork` is `false` | the ping's `branch` (which must exist) or the default branch                                                    |
  | external                 | `private` is `false`; `fork` is `false`     | when given, it exists and reaches the commit                                                                    |

  | Form     | Proof                                                                                                                                                                          |
  | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
  | `commit` | `compare/BASE...SHA` is `identical` or `behind` for a proving branch                                                                                                           |
  | `path`   | the commit proof, and `contents/PATH?ref=SHA` is a file                                                                                                                        |
  | `pr`     | the pull request's head repository is the reference's; `author_association` is `OWNER`, `MEMBER` or `COLLABORATOR` (not judged for external); the head is at or ahead of `SHA` |
  | `issue`  | the issue exists and is not a pull request                                                                                                                                     |

  "A commit the API returns proves nothing, because a fork's commits are served through its
  parent's API; only reachability from a branch of a trusted repository proves provenance." The
  REST calls: `GET /repos/{o}/{r}` (owner login, `fork`, `private`, `default_branch`),
  `GET /repos/{o}/{r}/branches/{b}`, `GET /repos/{o}/{r}/compare/{base}...{sha}` (`status`),
  `GET /repos/{o}/{r}/contents/{path}?ref={sha}` (`type` `file`), `GET /repos/{o}/{r}/pulls/{n}`
  (`head.repo.full_name`, `head.sha`, `author_association`), `GET /repos/{o}/{r}/issues/{n}`
  (`pull_request` absent). "At or ahead of SHA" is `compare/{sha}...{head}` `identical` or
  `ahead`.

- **Results** (A 9.3): proven (trust from `ForgeCheck.Trust()`: `trusted` for listed or org,
  `external` otherwise); `provenance` (found but not proven); `unresolved` ("not found, wrong kind,
  forge unreachable, forge refused the credential"); `forge-rate` ("the forge rate-limited the
  check, which says nothing about the reference"). A forge with no answer (no connection, a timeout,
  a 5xx) returns `ErrForgeDown` with outcome `unresolved`, so the receive side drops `unresolved`
  and the send side says 20 `forge`.

- **The forge's health** (n3d-D29): after `ErrForgeDown`, every check within 60 s on the clock
  returns `ErrForgeDown` at once, with no request.

- **Caching** (A 9.3, B 2.6 step 11): positive results only. Repository metadata, reachability,
  pull requests and branches for 3600 s; file and commit content at a SHA for the link's life
  (n3d-D22). 1024 entries per cache, oldest evicted.

- **HTTP** (A 9.3, 11.3): 15 s per request on the clock seam (`clock.AfterFunc` cancelling the
  request's context, as matrix's per-try deadline does); 8 MiB response cap; redirects only to the
  same origin. A 429, or a 403 with `Retry-After` or `x-ratelimit-remaining: 0`, is A 11.3's rate
  path: `Retry-After` in seconds, else a reset header, else 5 s; three tries; a wait over 60 s is
  not slept and is `forge-rate`. Any other 403 is `unresolved`. A 401 drops the token, re-reads it
  once and retries once; a second 401 is `unresolved`.

### Seams and tests

A fake GitHub on `httptest` (its origin set through `export_test.go`), scripted per route;
`execx.Fake` scripting `gh`; `clock.Fake`.

- SD5-1: nothing cached runs `gh` once; a second check inside 10 minutes runs none; past 10 minutes
  it runs again.
- SD5-2: a 401 drops the token: the next request re-reads it; a second 401 is `unresolved`.
- SD5-3: positive results kept 3600 s: no request at 3599 s, one at 3600 s; a negative result is
  never cached.
- SD5-4: `gh` absent, exiting 1, or printing an empty or over-long token: `ErrForgeLogin`; a check
  whose facts are all cached never runs `gh`.
- SD5-5: each row of both proof tables, proven and not, with `trusted` and `external`; a commit
  served from a fork's parent but on no proving branch is `provenance`.
- SD5-6: the token only in the `Authorization` header of requests to the origin; a redirect to
  another origin is refused and that origin receives nothing; an 8 MiB + 1 body is refused; a
  request held past 15 s is `ErrForgeDown`.
- SD5-7: 429 with each wait source, three tries; a wait over 60 s is `forge-rate` with no sleep; a
  403 with `x-ratelimit-remaining: 0` is the rate path; a plain 403 is `unresolved`.
- SD5-8: after `ErrForgeDown`, a check at 59.999 s makes no request; one at 60 s does.
- A source test: no writer call is handed a `Token`; `%v`, `%+v`, `%#v` of a `Token` show none.

## 6. D3: the outbox, TIMEOUTs and the roster cache in the engine (SD4; SY4's outbox half; LN5's `outbox`; SY7 case 4)

Package: `member/engine`: new files `outbox.go`, `timeout.go` and `roster.go`, plus the hunks in
`Persist`, `Ack`, `status` and `Add` that call them.

### Facts it needs, digested

- **Persist** (B 2.6 step 06: "the batch's accepted items appended to `pending`, `next_batch`, the
  room floors and the outbox updates the batch made") also, in its one write:
  - writes each new pending `ping` item's `verb` from its verdict (n3d-D30);
  - **an answer** (`Batch.Answered`): find the outbox ping by `Ping`. `From` must be in its `to`
    (index i), else it counts for nothing (proto makes no `Answer` for such a sender, so this is the
    engine's own guard). The engine judges on time by its own `due_ms` alone, never
    `proto.Answer.OnTime` (n3d-D8): `OriginMS <= due_ms` sets i in `answered`, whether or not i is in
    `reported` (A 8.4); later is late, not counted, and the TIMEOUT holds. Either way the verb, id and
    lateness go into the engine's in-memory answer table for `status`. An answer to a ping not in the
    outbox is ignored with one log line;
  - **an echo** (`Batch.Echoes`): `sent_ms` becomes `OriginMS` and `due_ms` becomes
    `OriginMS + (due_ms - sent_ms)`;
  - re-arms the TIMEOUT timer.
- **Ack** carries a pending item's `verb` into its ledger entry (n3d-D30); n3e's proof does the same.
- **The TIMEOUT timer** (n3d-D7): one `clock.AfterFunc` for the engine, armed for the earliest
  `due_ms + 30 000` over every held team's pings with an addressee in neither `answered` nor
  `reported`, as `due + 30 000 - Clock.WallMS()` (0 if past). Re-armed after every write that changes
  an outbox, at `Add`, on every `Persist` and every `SetHealth` (so a monotonic timer that slept
  through a suspend is corrected by the first sync after it), and when it fires. Its firing runs on
  the state goroutine (`do`), and:
  - re-reads `WallMS()`, and re-arms without writing when nothing is due;
  - for each team with something due, in one store write: one pending TIMEOUT item per silent
    addressee (id `proto.TimeoutKey(ping id, addressee localpart)`, item `timeout`, `from` the
    addressee's user id, `sent_ms` the deadline plus 30 000), with the addressee added to `reported`
    in the same write, so a crash makes it once or not at all;
  - wakes the waits and the delivery, as `Persist` does;
  - a store whose write fails (pending full even in the reserved 256) is retried 60 s later, one log
    line per team until it succeeds.
    It is never a tick: with nothing owed, no timer is armed.
- **Pruning** (n3d-D6): a ping whose every addressee is in `answered` or `reported`, whose
  `due_ms + (due_ms - sent_ms)` has passed, and for which no item keyed on it
  (`proto.TimeoutKey(ping id, ...)`) is still in `pending` or `awaiting`, leaves the outbox at the
  next outbox write (`render.go`'s `timeoutRecord` reads the ping for its `verb` and `ref`).
- **A stopped or down team** keeps its store and its timer duty. A refused store owes nothing until
  the owner's repair. A removed team's outbox goes with its store (add the pings to LN8's log line).
- **The roster cache** (n3d-D3): `SaveRoster(ctx, t, rev, content)` writes `roster.json` on the
  state goroutine through `state.Write`, only when `rev` differs from the one held; `Add` reads it
  (a missing or undecodable file is no cache, logged), decodes `content` with proto's roster
  decoder, and holds the roster per team for D6a. `Remove` unlinks it with the store. Two paths
  move or delete a store without the link, and must take `roster.json` too: the keeper's deletion
  after a timed-out removal (D3 adds the unlink beside `StoreWriter.Remove` in `member/keeper`,
  one hunk) and `seat set-aside` (D3 moves it aside with the store in `member/guest`, one hunk). A
  stale cache is only ever a composing aid (receivers and n3d-D27 check again), but a re-added
  seat should not start from a removed team's roster.
- **The lookup's ledger `Verb`**: `seam.go`'s `view` fills a ledger entry's `Related.Verb` from the
  new `verb` (one place, used by the receive side and by D6a's send Composer).
- **`status`'s `outbox` records** (LN5): after a team's `room` records, one per outbox ping, oldest
  `sent_ms` first: `to` and `timed_out` as short names (P4's `namer`), `due_ms`, and `answers` one per
  answered index plus one per late answer in the memory table (`late` true); `verb` and `id` from the
  table, null for an answer recorded before this link started (n3d-D8).
- **SY7 case 4** (engine half): an answer held only by a discarded sync answer never reaches
  `Persist`, so the ping stays unanswered and its TIMEOUT is made.

### Seams and tests

`clock.Fake` (`Advance`, `StepWall`, `Waiters`); a fake `team.Team`; stores seeded with outbox pings
through the test's own `StoreWriter`; `Persist` driven with built `Batch`es.

- SD4-1: a ping with two addressees, one answering on time: at deadline + 30 s exactly one TIMEOUT
  for the other, `reported` in the same write; at deadline + 29.999 s none.
- SD4-2: an answer with `OriginMS` at the deadline counts; at deadline + 1 ms it does not, the
  TIMEOUT is made, and `status` shows it `late`.
- SD4-3: an on-time answer read after its TIMEOUT: `answered` gains it, no second TIMEOUT.
- SD4-4: an answer from a sender the ping did not address counts for nothing.
- SD4-5: an echo moves `sent_ms` and `due_ms`; the timer follows.
- SD4-6 (never a tick): nothing owed, no outbox timer in `Waiters()`; three pings owed, one; a
  `StepWall` back makes the firing re-arm without writing; a `SetHealth` after a `Suspend` re-arms
  to the wall time.
- SD4-7: a failed write after the timer's staging, then a fresh engine over the same store, never
  makes a TIMEOUT twice.
- SD4-8: a team `Down`, and one stopped with `token`, still get their TIMEOUTs.
- SD4-9: pruning at exactly `due + timeout` and 1 ms before; SD4-9b: a TIMEOUT still pending past
  `due + timeout` keeps its ping, and its record shows `verb` and `ref`.
- SD4-10: `status`'s `outbox` records; a restarted engine prints `verb` and `id` null for an earlier
  answer.
- SD4-11: pending full with the reserved 256 used: the TIMEOUT write retries 60 s later and succeeds
  once room is made.
- SD4-12: a pending ping's `verb` reaches the ledger at `Ack`.
- n3d-D3 (engine half): `SaveRoster` writes once per new `rev`; `Add` reads it back; a planted
  undecodable file is no cache and logged; `Remove` unlinks it.
- SY7-4 (engine half): the answer's batch never persisted leaves the ping unanswered, TIMEOUT made.

## 7. D4: teamsync's receive side (SY4's answers, the echo, the roster cache, SD5's receive check, the recovery call)

Package: `member/teamsync` (`session.go`, `ln6.go`). It gains `Config.Forge team.Forge` (nil: every
forge-checked ping dropped `unresolved`, tests only).

### Facts it needs, digested

- **Answers** (SY4, A 8.2): after `Judge`, each verdict that answers an own ping (the `Record`
  outcome, for an `ack` or a stale answer, A 8.5; a PING verdict whose `Answer` names an own ping for `nack`, `done`,
  `blocked`) gives `team.Answer{Ping, From, Verb, ID, OriginMS}` in `Batch.Answered`; `OnTime` is not
  passed (D3 judges). A PING answer is still queued as its item; an `ack` queues nothing.
- **An answer whose reference fails its forge check** (n3d-D25) is dropped and gives no `Answer`, so
  its ping gets its TIMEOUT; a `Record` answer is not delivered, is not checked, and counts.
- **Echoes** (A 7.6 rule 3): an own event ignored `self` whose id is in `View.OwnPings` gives
  `team.Echo{ID, OriginMS}`.
- Both ride on the batch only: a discarded answer (SY6) carries neither (P3).
- **The roster cache**: after each verification that reads a roster `rev` the team has not saved,
  `Engine.SaveRoster(ctx, team, rev, content)` (n3d-D3). A failure is logged and blocks nothing.
- **The recovery call** (n3d-D4): after the start verification and before the first long-poll (SY2),
  `Engine.RecoverSends(ctx, team, notices)`, so a recovered ping is in the outbox before any answer
  to it is judged. Its transient failure is SY8's transient path, as the verification's is.
- **The receive forge check** (SD5, A 9.3 "every receiver before it delivers"; n3d-D29): after
  `Judge`, the accepted verdicts whose `Forge` is set (a non-carried ping with a reference) are
  checked concurrently, at most 8 at once, under one 30 s budget for the batch on the clock seam,
  before SY6 and SY5. Proven: queued (its trust is `ForgeCheck.Trust()`, which P4's renderer reads).
  `provenance`, `unresolved` (`ErrForgeDown` included) or `forge-rate`, or still running at 30 s: the
  verdict becomes `Outcome` drop with that `Reason` (proto's reasons), counted, one log line with the
  event id and the reason. `ErrForgeLogin`: dropped `unresolved`, the log line naming `forge-login`,
  once per team until a check next succeeds (n3d-D10).
- **Refetch runs the same check** and returns the dropped verdict on a failure, so the engine's
  re-judge drops a restored ping as any drop (n3d-D12).
- **Own sends** (SY4a): `Notices().RecordOwnSend` is built; D6a calls it. Add the test that a
  human's thread reply under an own `say` recorded that way is delivered with a store holding no
  record of it.

### Seams and tests

fakehs; a fake `team.Engine` recording batches and calls; a fake `team.Forge` scripted per
reference, able to hang.

- SY4-n3d-1: an `ack` in an own ping's thread gives one `Answer` and no item; a `done`, one `Answer`
  and one PING item; a `done` whose check fails, a drop and no `Answer`.
- SY4-n3d-2: an own ping's echo gives one `Echo`.
- SY4-n3d-3 (SY7 case 4, teamsync half): an answer inside an answer SY6 discards is in no persisted
  batch.
- SD5-r1: `provenance`, `unresolved`, `forge-rate`: dropped with that reason and counted; proven:
  queued.
- SD5-r2: `ErrForgeLogin` drops `unresolved` and logs `forge-login` once.
- SD5-r3: a carried ping, and a ping without a reference, are never checked.
- SD5-r4 / SD5-r6: a batch of 10 referenced pings against a forge that never answers is persisted
  within 30 s of fake time, the pings dropped `unresolved`; the checks ran at most 8 at once.
- SD5-r5: `Refetch` of a restored ping runs the check and returns a drop on failure.
- n3d-D3: a verification with a new `rev` calls `SaveRoster` once; the same `rev` again, not.
- n3d-D4: `RecoverSends` is called once per start, before the first long-poll; its transient failure
  sends no long-poll.

## 8. D5: the client verbs `reply`, `say`, `ping` (SD1-SD2's CLI half)

Package: P6's `member/client` (**as designed, not yet built**; start once P6 has merged into
`feat/bus-v2-n3d`). `Main(verb)` also serves `reply`, `say`, `ping`.

- The request: `wire.OpReply`, `OpSay`, `OpPing` with D1's args, `deadline_ms` from `wire.Deadline`
  (30 s) on `clock.Boot()`, `kit` the CLI's own digest.
- **stdin** (n3d-D15, n3d-D28): `say` and a `reply` without `--verb` read stdin to EOF, at most 4098
  bytes; one trailing `\n` is removed; then over 4096 bytes is 10 `size`, empty is 10 `empty`, not
  UTF-8 is 10 `schema`; a stdin that is a terminal is 64 `usage` ("text on stdin"), checked before
  reading. `ping` and `reply --verb` never read stdin and send `stdin` null.
- The answer: records to stdout, the error object to stderr, exit `done.exit`; a queued `sent` is
  exit 0. LN3's no-link answers are P6's; a send never reconnects.

Tests: SD-c1, each verb's request bytes per flag combination, `team` null without `--team`; SD-c2,
4096 bytes sent, 4097 is 10 `size`, `text\n` is sent as `text`, empty and `\n` alone are 10 `empty`,
invalid UTF-8 is 10 `schema`, a pty is 64, `ping` with data on stdin sends `stdin` null without
reading it; SD-c3, a `sent` and a queued `sent` are copied and exit 0, an error object is copied
with its exit.

## 9. D6a: `say` and `ping`, the charge, the sender and the recovery (SD2, SD3; the offline queue)

Package: `member/engine`: new files `send.go` and `sender.go`, with the hunks in `Serve`, `Add`,
`Remove` and `status`. `engine.Config` gains `Forge team.Forge`.

### Facts it needs, digested

- **The op's steps** (A 9.3's order; each refusal the first that applies, nothing spent by it):
  1. **Decode** the args (10 `schema`).
  2. **Resolve** the team and addressees (below). The roster is `Notices().Roster()` when it is
     verified, else D3's cached roster; with neither, 20 `unreachable` (n3d-D3).
  3. **The team stopped permanently** (`Health.Stopped` token, forbidden, untrusted): 21 or 22.
  4. **Compose** with `proto.Composer` (`Text` or `Ping`), its `Lookup` a memory-only lookup over
     the store (ledger with `verb`, outbox) built on the goroutine as `engine.view` is, its secret
     check given `Notices().Token()` (R43). Refusals map to 10 (their code) or 11 `role`.
  5. **More than 16 addressees** on a ping: 64 `usage` naming the bound (n3d-D5).
  6. **The forge check** (off the goroutine, `Config.Forge`) for a ping with a reference: `provenance`
     13 `provenance`; `unresolved` 13 `forge`; `forge-rate` 14 `forge-rate`; `ErrForgeDown` 20
     `forge`; `ErrForgeLogin` 78 `forge-login` (n3d-D11).
  7. **The charge**, one store write on the goroutine (SD3): build a `proto.SendGate` from the store's
     `gate` (`credit`, `at_ms` to its `Bucket`; `recent` to its `DupWindow`, keys older than 60 s
     dropped) and run `Composer.Admit` with it and the roster's limits: 15 `duplicate` or 14 `rate`,
     nothing recorded on a refusal. Then, in the same write: the gate back into the store, and the
     `sending` entry with its content, `tried` true when the team is up and the queue empty, else
     false. Checked first, with nothing written: `sending` full is 78 `store`; for an
     answer-expecting ping, outbox pings plus `sending` entries with `ping` set at 256 is 78 `store`.
     Because the charge is on the goroutine, two identical sends racing are ordered, and one gets 15.
     `failpoint.Hit(LinkAfterChargeBeforeSend)` follows the write.
  8. **The answer**: the team `Down`, or older entries queued: `sent` `queued` at once (n3d-D19).
     Otherwise hand it to the sender and wait until the request's deadline: posted,
     `sent{id, room}`; not posted by then, `sent` `queued`. The op maps its own ends and never goes
     through `teamDone` (which would make 4 `timed-out`).
- **Resolving** `say`, `ping` (SD2, CL6): `--to` names are matched per active team against the
  roster: a human by localpart, an agent by its short name when unique in the team, else its full
  handle (P69, P4's `namer`). `--to-coordinator` is the team's coordinator. With `--team`, that team;
  without, the team in which every name resolves: in two, 64 `ambiguous-team` (CL6, through
  `cli.ResolveTeam` over the candidates); in none, 64 `unknown-member`; a name matching two members
  of the chosen team, 64 `ambiguous-member`. The room is main. `ping` with `ack` or `nack` is 64
  pointing at `reply --verb`, where the built parser does not already refuse it (n3d-D15).
- **The sender**, one goroutine per held team, started at `Add`, ended at `Remove` or `Shutdown`; it
  lives through a stop and a retry (n3d-D2). It always uses the team's current `team.Team` (a
  retried team is a new `NewTeam`), never one captured at `Add`. The engine tells it of health
  changes by a non-blocking send, so the state goroutine never waits.
  - It posts nothing until that start's `RecoverSends` has returned successfully (each `Add`, and
    each retried team, starts a new recovery), so a `tried` entry is never PUT while the search
    for it runs; a recovery that keeps failing transiently leaves the queue waiting, as a down
    team's does.
  - It takes `sending` entries oldest first. Before a post, it re-checks the content against the
    current verified roster (proto's decode and roster rules, as the composer runs them): an entry the
    roster now refuses is removed with one log line naming the reason, and the team's `unsent` count
    rises (n3d-D27). Before the first PUT of a `tried: false` entry it sets `tried` true in a store
    write (`do`).
  - It posts by `Notices().Send` with the entry's `txn`, `type` and `content`; matrix retries an
    `Unreachable` 3 times 1 s apart (M22).
  - Posted: `failpoint.Hit(LinkAfterSendBeforeRecord)`, then one store write (`do`) removes the entry
    and, for `ping` set, adds the outbox ping (`id` the event id, `sent_ms` the local wall time,
    `due_ms` `sent_ms + timeout_ms`, through D3's code); then `Notices().RecordOwnSend(id, Related)`
    with this member as `Sender`, the verb, `AddressedToSelf` false and `AddressedToRosterAgent` by
    the same predicate `seam.go` uses for outbox pings (SY4a).
  - Transient (`Transient()`, a 429 past its retries included): the entry stays; the sender waits for
    a health change with `Down` false and a newer `LastSyncOKMS`, or 30 s, then tries the oldest entry
    again.
  - Permanent (401, 403, lost trust): the sender stops for that team. An entry whose op is waiting is
    removed and the op answers 21 or 22; entries already answered `queued` stay, shown in `status`
    with the team's stop, and are posted when the team runs again (n3d-D2).
- **`RecoverSends(ctx, t, n)`** (n3d-D4), on the team goroutine before its first long-poll (D4 calls
  it): for each `tried: true` entry, one page of 100 own events back in main (`Messages`) is searched
  for an event whose decoded content equals the entry's by `proto.Value` equality (strict parse,
  never bytes) and whose `origin_server_ts` is not before `since_ms`. Found: recorded as posted with
  that id (the outbox ping's `sent_ms` that event's `origin_server_ts`), no PUT. Not found, and every
  `tried: false` entry: left for the sender, by the same txn.
- **`Remove`** drops a team's queued sends with its store, one log line counting them (n3d-D23).
- **`status`**: after the `outbox` records, one `queued` record per `sending` entry (n3d-D21), and
  the team record's `unsent` count.

### Seams and tests

fakehs (drop before and after handling, `Restart`) behind a scripted `team.Team` whose `Notices()`
is a `matrix.Client` against fakehs; a fake `team.Forge`; `clock.Fake`; P4's byte-level CLI client.

- SD2-1: names resolved per team; a name in two teams with no `--team` is 64 `ambiguous-team` (CL6's
  row); unknown 64 `unknown-member`; an ambiguous short name 64 `ambiguous-member`;
  `--to-coordinator`.
- SD2-2: a bare `ping` is 64 naming the verbs and `status` (built; assert the test exists, else add
  it); `ping ack` is 64 pointing at `reply --verb`.
- SD3-1: the charge and the `sending` entry in one write, before any PUT (a writer that fails it: no
  PUT, nothing spent).
- SD3-2: three tries in the deadline: fakehs dropping the first PUT after handling gives one event
  (the same txn) and `sent` with its id.
- SD3-3: the duplicate window: the same text to the same addressee at 59.999 s is 15, at 60 s is
  sent; different texts both send; two identical sends racing, one 15.
- SD3-4: with `send_burst` 10 the 11th send is 14 `rate`; refilled at `send_per_minute` on the fake
  clock; a 14 spends nothing and enters no window.
- SD3-5: the send order: a duplicate whose reference fails provenance is 13, not 15 (the forge before
  `Admit`); a secret to an unknown member is refused by the first in A's order.
- SD3-6 (offline): the team `Down`: `say` answers queued at once, exit 0; `status` shows one `queued`
  record; the team up: posted once by its txn, `status` shows none; another team's `say` meanwhile is
  posted at once.
- SD3-7 (offline): every try fails transiently for 30 s: queued at the deadline; fakehs back: posted
  once.
- SD3-8: a 401 on the PUT of a waiting op: removed, the op 21, the token stays spent. SD3-8b: a queued
  entry, then 401, then a bundle change (a new team): posted once, and `status` shows it until then.
- SD3-9: `sending` full: the 33rd is 78 `store`, nothing charged; outbox pings plus pending pings at
  256: an answer-expecting ping is 78 `store`.
- SD3-10 (recovery, in process): a `tried` entry fakehs already holds (content re-serialised with
  another key order) is recorded without a PUT; one it lacks is posted by its txn; a `tried: false`
  entry is posted without a search.
- SD3-11: queued sends post oldest first; a new send behind them answers queued.
- SD3-12: a queued ping to a member parked meanwhile is removed at its post, logged, `unsent` 1.
- n3d-D3: with no verified roster and a cached one, a `say` to a down team is queued; with neither,
  20 `unreachable`, nothing charged.
- SD4-n3d: a sent ping enters the outbox with `due_ms`; a `say`, a `done`, do not.
- SY4a-n3d: each post calls `RecordOwnSend` with the flags above.

## 10. D6b: `reply` (SD1)

Package: `member/engine`, new file `reply.go`, on D6a's steps 3-8 (the same compose, forge, charge,
sender and answer), with its own resolving (n3d-D13):

- The team: the store whose ledger holds the id (`single()`). An id holding `~` (a TIMEOUT's key) is
  64 `usage` ("a TIMEOUT is not a message; ping the member again", n3d-D26).
- From the ledger entry (room, item, thread, from, verb), without the server:
  - room `main` is n3d's; an item in `questions` answers "unbuilt" (n3f's `thread`);
  - no `--verb`: agent text replying to the item (`m.in_reply_to`), `to` its `from`, plus the
    coordinator under `--to-coordinator` (n3d-D14);
  - `--verb V`: only on a `ping` item whose `verb` expects an answer (A 8.1); the answer form rooted
    at the id, `to` its `from`, `--ref` where V takes one; on any other item 10 `re`; with
    `--to-coordinator`, 64 `usage`. The probe's `check` is answerable from the ledger, since the
    Composer's lookup gives the root's `Verb` `check` (`probeCheckOf`);
  - a `notice` item (a carried item): fetched (`Team.Refetch`, main) for its carried object; only a
    carried ping takes `--verb`, to its `from` when that is a current member, else 10 `target`
    (A 7.7). A fetch with no answer is 20 `unreachable`.
- An id in no ledger: `--team` or the only active team (else 64 `ambiguous-team`), then
  `Team.Refetch(main, id)`: 404, or a verdict that decodes no item, is 23 `not-found`; an own event
  64 `usage`; otherwise as the ledger case, from the verdict. No answer is 20 `unreachable`.

Tests:

- SD1-1: `reply` to a human item: `m.in_reply_to`, `to` the human; a worker's reply to a human copies
  the coordinator (P21).
- SD1-2: `reply --verb ack` to a `review` ping: the answer form, `to` its sender; `--verb done --ref`
  forge-checked; `--verb` on a peer item, an answer item, or a ping item whose verb expects no answer,
  is 10 `re`.
- SD1-3: a carried ping answered to its original sender; one whose `from` is no longer a member is 10
  `target`.
- SD1-4: an unknown id fetched and replied to; 404 is 23; two teams and no `--team` 64
  `ambiguous-team`; an own event 64; a TIMEOUT key 64.
- SD1-5: `reply <check id> --verb ack` from a worker to the probe is sent, from the ledger alone.
- SD1-6 (offline): a reply to a ledger item while the team is `Down` is queued, exit 0.

## 11. W: wiring (after D2-D6b and P6)

- `link.Main` builds one `forge.Checker` (`forge.NewLogin(execx.OS{}, clock.System())`) and passes it
  as `engine.Config.Forge` and, through W's `TeamConfig`-to-`teamsync.Config` adapter, as
  `teamsync.Config.Forge`.
- `cmd/teamchat`: `reply`, `say`, `ping` -> `client.Main(verb)`; the three leave `cli`'s `unbuilt`.
- End-to-end with real processes (the test binary as `teamchat` through reexec, fakehs over HTTP,
  `run --plain --state <tmp> --seat T/S`), a coordinator C and a worker K on one team, refless verbs
  (the forge is proven in process):
  - C `ping halt --to K`; K `recv` shows the PING; K `reply <id> --verb ack`; C `status` shows the
    `outbox` record answered.
  - C `say --to K`; K `recv`; K `reply <id>` with text; C `recv` shows the PEER.
  - fakehs stopped: C `say` answers queued, exit 0; fakehs back: K receives it once.
  - a link started with fakehs stopped, after an earlier run saved `roster.json`: C `say` is queued,
    and posted once fakehs is back.
  - under `busfailpoints`, `link-after-charge-before-send` and `link-after-send-before-record`: the
    keeper restarts the link and the text is in the room exactly once.
- TIMEOUTs are not end-to-end (the shortest real deadline is 60 s): SD4-1 to SD4-12 hold them.

## 12. Decisions n3d takes (the spec is silent)

| ID      | Decision                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| n3d-D1  | A `sending` entry carries what a re-send needs: the event type, the composed content (at most 16384 bytes), `tried`, and for an answer-expecting ping its verb, ref, addressees and timeout. B 2.3.2's 128-byte entry holds no content, so "a link that restarts re-sends it" could not. It is in the store, so the charge and the content are one write (SD3) and a removal or set-aside takes them with the store. Caps: 32 entries of at most 20 KiB (B's 64 halved); the file cap is the new sum rounded up to the next 256 KiB. Queued sends cost up to 640 KiB more per write while they wait. Proposed B 2.3.2 text: "`sending` holds a send whose transaction id is chosen but whose answer has not arrived, with its event type and content, so a restarted link re-sends it as it was; 32 sends in flight (20 KiB)". |
| n3d-D2  | The owner's offline rule. A send to a down team, or whose tries fail transiently for its whole deadline, is queued: exit 0 with `{"kind":"sent","team","id":null,"room","queued":true}`, a `queued` record in `status`, posted by the same txn when the team is up. A permanent failure stops the sender for that team, not the queue: an entry whose op still waits is removed and answered 21 or 22; entries already answered `queued` are posted when the team runs again (SY8's retries; a restore's token reissue among them). A 429 past its retries queues, so a send never exits 14 `server-rate` (kept for n3f's reports room).                                                                                                                                                                                       |
| n3d-D3  | The last verified roster of each team is cached in `<seat dir>/roster.json` (`rev` and the roster event's content), written by the engine when teamsync verifies a new `rev`, read at `Add`, used only to compose sends before the first verification, unlinked at removal. The member's own file (TB4); every receiver checks the addressing again, and the first verification replaces it. With no cache (a seat never verified), a send exits 20 `unreachable` with nothing charged.                                                                                                                                                                                                                                                                                                                                        |
| n3d-D4  | The recovery after a link restart (B 2.4.5) runs on the team goroutine before its first long-poll: each `tried: true` entry is searched for in one page of 100 own events back in main, by `proto.Value` equality of the decoded content and an `origin_server_ts` not before `since_ms`; found is recorded as posted, with no PUT. This covers B section 4's risk (a homeserver that forgets transaction ids) and a token reissued between tries (a new device), unconditionally, at one GET per entry, and puts a recovered ping in the outbox before any answer to it is judged.                                                                                                                                                                                                                                            |
| n3d-D5  | A member's ping names at most 16 addressees, the store's outbox bound, not A 11.2's 32: a 17th is 64 `usage` before anything is spent. Proposed A 11.2 text: "`to` 1-32 on the wire, 1-16 from a member (B's outbox bound)".                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| n3d-D6  | Only a ping whose verb expects an answer enters the outbox. It leaves once every addressee is answered or reported, its deadline plus its own timeout has passed ("answered in the last `ack_timeout_s`", B 2.2.2), and no TIMEOUT item keyed on it is still pending or awaiting.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| n3d-D7  | The TIMEOUT timer is one `clock.AfterFunc`, armed from wall milliseconds (deadlines are server times, CK1) and re-checked against `WallMS()` when it fires; it is re-armed on every outbox write, `Persist` and `SetHealth`, so a suspend (when a monotonic timer stops) is corrected at the first sync after it. `reported` is set in the same write that queues the TIMEOUT item: A 8.3's "marked reported once delivered" is read as "once queued for delivery", since the pending item survives a restart.                                                                                                                                                                                                                                                                                                                 |
| n3d-D8  | The store's `answered` stays an index list (a 16-addressee ping with a 700-character ref already fills its 3 KiB). The engine judges on time by `due_ms` alone, not `proto.Answer.OnTime` (which uses the current roster's timeout). The `outbox` record's `answers` take `from` from the index and `verb`, `id`, `late` from an in-memory table filled at `Persist`; an answer from before the link started prints `verb` and `id` null. A late answer is in memory only: it does not count (A 8.2). Proposed B 2.2.2 text: "an answer recorded before the link started has `verb` and `id` `null`".                                                                                                                                                                                                                          |
| n3d-D9  | The own ping's echo rides on the batch as `Batch.Echoes`. An echo before the send's answer has added the ping finds none, and `due_ms` stays on the local send time, which A 8.2 allows. An answer cannot race the same way in practice: it needs the addressee to sync, deliver and act, while the send's answer is one round trip; after a restart, D4's ordering closes it.                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| n3d-D10 | On the receive side a ping whose forge check fails is dropped as A 9.3 says, `unresolved` included for a forge with no answer and for a member with no `gh` login (logged as `forge-login` once per team, each line naming its team, until a check succeeds). It is never held, which would stop the team's sync on GitHub's health. The loss is said: counted, logged, and the sender gets its TIMEOUT.                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| n3d-D11 | The send side's forge results: `provenance` 13 `provenance`; `unresolved` 13 `forge`; `forge-rate` 14 with a new code `forge-rate` (A 9.3: "not the bus's `rate`"); no answer 20 with a new code `forge` (class wait; "push first" would be wrong for an outage); no login 78 `forge-login`. Proposed B 2.2.4 rows: 14 gains `forge-rate`; 20 gains `forge`, "the homeserver or the forge did not answer".                                                                                                                                                                                                                                                                                                                                                                                                                     |
| n3d-D12 | A forge-checked ping's trust is the class's (`ForgeCheck.Trust()`), which P4's renderer already reads: no field is added. A failed check, live or in `Refetch`, turns the verdict into a drop with proto's reason, so a restored ping that no longer proves is dropped by the engine's re-judge as any drop.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| n3d-D13 | `reply` decides from the ledger where it can, so a reply to a delivered item queues while the homeserver is down: no `--verb` is a reply in main to the sender; `--verb` applies to a `ping` item whose recorded `verb` expects an answer, and to the probe's `check`. A carried item and an unknown id need one fetch. Items in the questions room are n3f's.                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| n3d-D14 | `reply --to-coordinator` adds the coordinator to the reply's `to` (nothing when already there or when this member is the coordinator); with `--verb` it is 64 `usage`, since an answer goes to its ping's sender.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| n3d-D15 | `say` and a plain `reply` read text on stdin; `ping` and `reply --verb` read none (a ping's body is generated, A 7.2), so a verb run with an inherited stdin never blocks. A terminal on stdin is 64 `usage`. `ping` with `ack` or `nack` is 64 pointing at `reply --verb` (their relation is required); `done` and `blocked` may be sent by `ping` with no relation.                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| n3d-D16 | Two failpoints: `link-after-send-before-record` (PT1's) and `link-after-charge-before-send`, so the restart re-send is proven on both sides of the PUT.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| n3d-D17 | `recv`'s `fact answered` records and the delivery's `answered:` line are n3e's; n3d records every answer so they can be rendered. Under `--plain`, an agent reads its pings' answers from `status`'s `outbox` records until then.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| n3d-D18 | The gate's times are wall milliseconds, since the store outlives a reboot that resets `CLOCK_BOOTTIME`. A wall step back refills nothing until the clock passes `at_ms`; a step forward refills at most to the burst.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| n3d-D19 | A send answers `queued` at once when its team is down or older sends wait, rather than holding the agent's tool call for 30 s; otherwise it waits up to its deadline for the post.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| n3d-D20 | Isolation: one sender goroutine per team, told of health by a non-blocking send; the state goroutine never waits on it; one forge checker per link, its caches and health shared and safe for concurrent use; so a slow homeserver or forge holds only one team's work.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| n3d-D21 | `status` gains a `queued` record kind (`team`, `room`, `send` text or ping, `verb`, `to` short names, `since_ms`, `tried`), one per `sending` entry; `sent` gains `queued`, its `id` null exactly when queued. Proposed B 2.2.2 text: both added to the list, "`queued` is a send the link holds until its homeserver answers".                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| n3d-D22 | The forge caches hold 1024 entries each, oldest evicted; content at a SHA is kept for the link's life, not A's 30 days, since the cache is in memory.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| n3d-D23 | A removal drops the team's queued sends with its store (one log line counting them): the member has left, and a later post would speak for a member that is gone. A `seat set-aside` sets them aside with the store, unsent.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| n3d-D24 | `gate.recent` holds 96 keys (B 2.3.2's 64 is below the 90 sends one 60 s window allows at the limits' maxima, `send_burst` 30 plus `send_per_minute` 60).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| n3d-D25 | An answer (`nack`, `done`, `blocked`) whose reference fails its receive check is dropped and does not count, so its ping gets its TIMEOUT; a `Record` answer (`ack`, or a stale answer, A 8.5) is not delivered, is not checked, and counts.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| n3d-D26 | `reply` to a TIMEOUT's id (`<event>~<localpart>`, not an event) is 64 `usage`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| n3d-D27 | A queued send is checked again against the current verified roster before its post: one the roster now refuses (an addressee parked or removed, this member demoted) is removed with a log line naming the reason and counted as `unsent` in `status`, never posted to be dropped by every receiver.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| n3d-D28 | stdin text: one trailing `\n` is removed before the size and empty checks (`echo text \| teamchat say`), and the text must be UTF-8 (10 `schema`).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| n3d-D29 | The receive check of one batch runs at most 8 checks at once under one 30 s budget; a check still running then is `unresolved`. After a forge with no answer, checks within 60 s on the link are `ErrForgeDown` at once, with no request: the forge's health, not a cached fact about a reference. A forge outage costs a team's sync at most 30 s per batch.                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| n3d-D30 | Pending and ledger entries gain `verb` (the ping's verb for an item `ping`, else null), so `reply --verb` and the send Composer's lookup know the ping's verb from the store alone (the probe's `check` round trip among them).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |

## 13. The common rules file for n3d part builders

The lead puts this text in `untracked/scratch/n3d-part-common.txt` and hands it to each builder with
the part's name.

```text
COMMON RULES FOR n3d PART BUILDERS (Plan 00209 N3, slice n3d: the sends; product name teamchat)

Plan folder: CLAUDE/Plan/00209-agent-bus-v2-in-go/. Your report goes in its subagent-reports/.

Your brief is your part's section of subagent-reports/261022-n3d-handoff.md in your worktree. Read
section 0 (the order), section 1 (the owner's offline rule), section 2 (what you build on: 2c lists
the built APIs no report quotes, which you read with go doc first), section 3 (the shared facts),
your own part's section, and section 12 (the decisions). The brief digests the design for you. Open
SPEC-n3.md or a design chapter only where your section cites a point that is unclear, and then by
grep. Earlier builders ran out of context by reading widely. Owner direction: v1 is a prototype,
not an archetype; make it excellent.

The owner's rule, which every part keeps: an offline member or an unreachable server never blocks
anything. A send to a team whose server is down is held in the outbox and reported, never lost, and
never blocks other teams. No network call on the state goroutine; one team's failure never touches
another team.

Rules:
- Work only in your own worktree: absolute paths and `git -C <wt>`. Never touch /workspace itself or
  any other worktree.
- TDD (the test file before the source; a hook enforces it). Name each test for its expectation id
  in your section (SD3-6 and so on). Run go test -race -cover on your packages, and golangci-lint run
  from src-go with no tags, with --build-tags tuwunel, and with --build-tags busfailpoints. Hold 90%
  coverage per package. No //nolint. No time.Now in tests; no test sleeps. Use named results instead
  of `x, _ :=`.
- `os/exec` only through execx; a run-time command name needs its dynamicNames row in
  src-go/qa/rules/table.go. Never run `gh auth token` yourself (a hook denies it); tests use
  execx.Fake.
- Run ./scripts/qa-changed.bash before each commit. It should be fully green. Any red is yours to
  fix.
- Tuwunel-tagged tests: FAKEHS_TUWUNEL_BIN points at the binary in
  /workspace/untracked/worktrees/bus-v2-n1-hostchecks/untracked/scratch/tuwunel-dl/. Run them if
  your part touches matrix or the sync.
- Commit after each green step. Messages: 'Plan 00209 N3 n3d <part>: ...', ending with:
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
- Do NOT edit PLAN.md or the design chapters; the lead does that at merge. Keep edits to shared
  files (.golangci.yml, src-go/qa/rules/table.go, the go-deps allowlist, failpoint's names) minimal
  and in their own hunks.
- Never push, qa-all, Ansible, sed, stash, amend or rebase. Chain bash with &&. No pipes to head or
  tail; redirect to untracked/scratch/ and grep. No journal edits, no time estimates, no
  install-specific names.
- Gates: inert code only; no playbook, unit, tmpfiles or meta-deploy entry.
- Trust boundary: no-follow opens, never open a FIFO, never decide from a file another uid can
  write. A token (the member's, or gh's) is never logged, written or formatted.
- No review loop of your own; the lead runs one Opus review over the merged slice. If the brief is
  wrong or the built API differs from it, decide the simpler, well-tested way and record it.
- CONTEXT BUDGET: if you near it, write subagent-reports/261022-n3d-<part>-handoff.md, commit it,
  and stop.
- At the end, write subagent-reports/261022-n3d-<part>.md: what was built, the exported API as built
  (exact signatures, for the later parts), the go doc facts you relied on from section 2c, coverage,
  and decisions. Final message under 700 characters.
```

## 14. Reports

Each part writes `subagent-reports/261022-n3d-<part>.md` (`d1` ... `d5`, `d6a`, `d6b`; reviews as
`-review`, `-review-2`, ...); W writes `261022-n3d-wire.md`. A builder that stops at its context
budget writes `261022-n3d-<part>-handoff.md` first. The lead puts each part's PLAN.md Task 3.2
progress line in at its merge.
