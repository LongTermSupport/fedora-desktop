# n3d handoff: the sends, split into six parts (Plan 00209 N3, slice n3d)

Design only; no code is written here. n3d is SPEC-n3.md section 5 (SD1-SD5), with the halves of
n3c's expectations that section 4 left to it: SY4's "an answer to an own ping updates the outbox"
(n3d), SY4a's own sends recorded at their send, LN5's `outbox` records, SY7 case 4 (a reply held
only by a discarded answer leaves the ping unanswered and its TIMEOUT made), and PT1's failpoint
"after a send's PUT before its answer is recorded". It is split into six parts, D1 to D6, and a
closing wiring step W. Each part is one worktree, one branch and one builder, and merges into
`feat/bus-v2-n3d`.

Base: `feat/bus-v2-n3d` at 6c3a64ab0, cut from `feat/bus-v2-n3c`. n3c's P1-P5 are merged there.
**P6 (`member/ends`, `member/client`) is still being built**: D5 waits for it, and every P6 API
below is marked "as designed, not yet built".

Every API quoted as "built" is taken from the part reports named in each quote (`261022-n3c-p2.md`
to `-p5.md`, `261022-n3a*.md`, `261021-n1-*.md`), not from the code. Section 2 lists the built
APIs that no report quotes in full; a builder reads those with `go doc` before writing against
them, and records any difference from what this brief assumes. Each signature a part adds is that
part's decision: a builder may change it only by recording the change and its reason in its part
report.

## 0. Order

| Wave | Parts                      | Needs                                 | Why                                                                                                                                                                                                                    |
| ---- | -------------------------- | ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1    | **D1** (shared types) ∥ D2 | the base                              | D1 is what every later part compiles against: the store's `sending` entry, the new records, the three send ops' args, the `team` additions, the two failpoint names. D2 is the forge package, which imports none of it |
| 2    | D3 ∥ D4                    | D1 merged                             | D3 is the engine's outbox and TIMEOUTs; D4 is teamsync's receive side (answers, echoes, the receive forge check). Different packages; each meets the other through `team.Batch` only, against fakes                    |
| 3    | D5 ∥ D6                    | D3 merged (D6); D1 and P6 merged (D5) | D6 is the engine's send ops, on D3's outbox code in the same package. D5 is the client side of `reply`, `say`, `ping`, in P6's `member/client`                                                                         |
| 4    | W                          | D2-D6 and P6 merged                   | the forge checker given to the engine and to teamsync, the three verbs in `cmd/teamchat`, the end-to-end exchange under `--plain`, the restart and failpoint real-process tests                                        |

D3 and D6 are both in `member/engine`, so they are sequential, not parallel: D3 first, because it
is the smaller and D6's send writes call its outbox helpers. D2 is used through an interface
(`team.Forge`, D1) by D4 and D6, each against a fake, so neither waits for D2; only W needs it.

Every part keeps SPEC-n3 section 1 (TB4 for member files, CK1's clocks), the 90% coverage floor per
package, and `-race`. No test sleeps (no `time.Now` in tests). `os/exec` is reached only through
`execx`; a run-time command name (`gh`) needs its `dynamicNames` row in
`src-go/qa/rules/table.go`.

## 1. The owner's offline rule, as n3d carries it

**An offline member or an unreachable server never blocks anything. A send to a team whose server
is down is held in the outbox and reported, never lost, and never blocks other teams.** Each part
holds to it in these places:

- **No network call on the state goroutine** (B 2.4.3, as P4 built it). Every PUT, GET and forge
  request runs on a team's own goroutine or a request's connection goroutine.
- **One sender per team** (D6). Every PUT a team's sends make goes through that team's sender
  goroutine, in `sending` order. A slow or dead homeserver holds only that team's queue.
- **A send to a down team is queued, not failed** (n3d-D2). It is composed, checked, charged and
  recorded in `sending` with its content in one store write, and the verb answers at once with a
  `sent` record `{"id":null,"queued":true}` and exit 0. The team's sender posts it, by the same
  transaction id, when the team is next up. `status` lists it as a `queued` record until then.
- **A send whose tries all fail transiently is queued the same way** (n3d-D2): the request's 30 s
  ends, the verb answers `queued`, and the PUT goes on under the sender.
- **Queued sends survive a link restart** (n3d-D1): the content is in the store, so the new link
  re-sends them (n3d-D4). They are lost only by the owner's acts: a removal, or `seat set-aside`.
- **A ping to an offline member** is an ordinary ping: it is posted, and its addressee's silence is
  a TIMEOUT. The TIMEOUT timer is on the state goroutine and needs no network, so a team that is
  down or stopped still makes its owed TIMEOUTs (B 2.6 step 08: "the stopped team keeps its store,
  and with it its owed TIMEOUTs").
- **The forge** (GitHub) is a server too: a forge that does not answer fails only the one check,
  under a bounded deadline; on the receive side the ping is dropped by A 9.3's rule and its sender
  gets a TIMEOUT, so the loss is said (n3d-D10).
- **The one case that cannot queue** (n3d-D3): a team the link has never verified (it started while
  the homeserver was down) has no roster in memory, so nothing can be composed. The verb exits 20
  `unreachable` with nothing charged and nothing recorded, so nothing is lost, and the other teams
  are untouched.

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
(`261022-n3c-handoff.md` 8a, P5).

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

How it works, from `261022-n3c-p4-handoff.md` "How the engine works": `do(ctx, f)` runs f on the
state goroutine, which calls no team method and makes no network call; `Serve` answers ops it does
not know with "unbuilt" 1 `internal`; `Persist` is one write (position, floors, new items, the
redactions, the counts) and then wakes the waits, and it ignores `Batch.Answered`; a full pending
list refuses the whole batch; the TIMEOUT record is built from its key `event~silent` and the
outbox ping; `status` prints `team`, `member`, `room` and `runtime`, with `outbox` "empty until
n3d"; `single()` resolves a single-team verb's team through `cli.ResolveTeam`, the ledger being
the team whose store holds the event id; `internal()` never quotes the error.

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

Files: `session.go` (an answer's steps), `lookup.go` (SY4a's resolver; the own-sends map, 1024,
memory only, n3c-D11), `ln6.go` (Refetch and the notices). SY7 case 4 "is the engine's work,
n3d's".

`member/wire` (`261022-n3c-p2.md`): `OpReply`, `OpSay`, `OpPing` exist;
`Deadline(op, waitTimeout)` gives sends 30 s; `DecodeArgs(raw, out)` is strict (a refusal is 10
`schema`); every args struct carries `Team *paths.TeamID` with the `team` key always present (null
for no `--team`); `Request.Stdin *string`.

`clock` (`261022-n3c-p2.md`): `Clock{Boot, Mono, WallMS, Sleep, AfterFunc, Hold}`; `Fake` with
`Advance`, `Suspend`, `StepWall`, `Waiters`, `BlockUntil`. The fake's waiters fall due on Mono.

`failpoint` (`261022-n3c-p2.md`): `type Name string`; `func Hit(name Name)`; an unknown name in
`TEAMCHAT_FAILPOINTS` panics at the first `Hit`; the release-build gate test scans the binary for
every name.

`member/state` (`261022-n3a.md`, `-writer.md`, `-state-handoff.md`): `StoreWriter`
(`NewStoreWriter(root, l)`, `Load`, `Write(Store)`, `Remove`, `Close`); the store's `Add*` growth
methods refuse a full list with `ErrFull`/`*FullError{List}`; pending agent items stop at 2048-256
and HUMAN and TIMEOUT items use the rest; `ledger` and `missed` roll; `gate.credit` is an integer,
`proto.Bucket`'s credit (one token is 60 000); a TIMEOUT item's id is `proto.TimeoutKey`; send
kinds are `text`, `ping`, `notice`; the caps (B 2.3.2) are 256 outbox pings of 3 KiB with at most
16 targets, 64 sends of 128 B, 64 gate keys of 96 B, file 2 MiB; the store is written only by a
`StoreWriter` (no exported encoder).

`member/cli`, `member/exits`, `member/records` (`261022-n3a-cli.md`):
`ResolveTeam(flag, ledger *paths.TeamID, active []ActiveTeam)` (CL6; two active teams and no
`--team` is 64 `ambiguous-team` listing them); `exits.New(code, team, Template, fills...)` with the
exit taken from the code's set; every B 2.2.2 record kind as a typed struct, `outbox` and `sent`
among them; `type RunFunc func(Env, Invocation) term.ExitCode` and `Env.Verbs`
(`261022-n3c-p2.md`). The verb table parses every verb and flag of B 2.2.1 already, `reply`, `say`
and `ping` included; a bare `ping` is already 64 listing A 8.1's verbs that are not answers
(`261022-n3a.md` decision 09).

`matrix` (SPEC-matrix M22, MB10): `Send` is `PUT /rooms/{r}/send/{type}/{txn}`; a given
transaction id is `[A-Za-z0-9._~-]{1,64}`; an `Unreachable` answer is retried with the same id, 3
tries 1 s apart through the client's `Clock`; any other failure is not retried. The member's
client is per team, inside teamsync.

### 2b. P6, as designed, not yet built (`261022-n3c-handoff.md` section 7 and 8a)

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
CLI's own `cli.Env.Digest`; copies record lines to stdout and the error to stderr; exits with
`done.exit`. With no link it answers LN3's four cases within 300 ms. Only `wait` reconnects. The
ends post notices with `proto.Composer.Notice` and record each own send with
`Notices.RecordOwnSend`; notices do not go through `sending` and are exempt from the duplicate
window (A 11.2).

### 2c. Built, but no report quotes it: read with `go doc` first

| Package          | What to read                                                                                                                                                                                                                                                                                                                    | Used by        |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------- |
| `proto`          | `Composer` (`Text`, `Ping`, `Notice`; its send-side refusals and their `Reason`s), `Bucket` and its `Admit`, the duplicate key of A 11.2 if proto has one, `OwnPing`, `Verdict` (its `Answer` and `Forge` fields, the `Record` outcome), `Answered`, `TimeoutKey`, the roster's `limits` and `forge` section types, `Localpart` | D1, D3, D4, D6 |
| `matrix`         | `Send`'s signature and how it takes a transaction id; `Error`'s `Transient()`; `GetEvent`                                                                                                                                                                                                                                       | D6             |
| `notice`         | `Client`'s `Send` and `Messages` signatures                                                                                                                                                                                                                                                                                     | D6             |
| `member/state`   | `Store`'s Go types for `outbox` (`pings`, `sending`, `gate`) and `ledger`, and their `Add*` methods                                                                                                                                                                                                                             | D1, D3, D6     |
| `member/records` | `Outbox`, `Sent`, `Timeout`, `Ping` fields                                                                                                                                                                                                                                                                                      | D1             |
| `member/exits`   | how a proto refusal reason maps to an exit and code, if a helper exists                                                                                                                                                                                                                                                         | D6             |
| `execx`          | `Runner`, `Fake`'s scripted run form                                                                                                                                                                                                                                                                                            | D2             |

## 3. The design facts every part shares, digested

- **The store's outbox** (B 2.3.2), as JSON:
  `"outbox": {"pings": [{"id","verb","ref","to":[user ids],"sent_ms","due_ms","answered":[],"reported":[]}], "sending": [{"txn":"tc<32 hex>","room":"main","kind":"text","since_ms"}], "gate": {"credit","at_ms","recent":[{"key":"<sha256>","at_ms"}]}}`.
  "`sending` holds a send whose transaction id is chosen but whose answer has not arrived; a link
  that restarts re-sends it with the same transaction id, which the homeserver stores once (M22)."
  "`gate` is the send gate: a send's token is charged in the same store write that records it in
  `sending`, before the network call, so a send that fails has still spent its token (RP 5.16)."
  "`answered` and `reported` in a ping are indexes into its `to`, and `reported` makes one TIMEOUT
  per silent target (RP 5.14)." "A send past the outbox or `sending` cap exits 78 `store`."
- **The send order** (A 9.3, `261021-n1-proto.md` decision 35): "class and role; what the content
  is built from (`schema`/`target`); the 10.1-10.3 form (`body`); the receiver's rules over the
  built content (decode, then the roster); `secret`; `duplicate`; then, in B, the forge check and
  `Admit`. A send the forge check refuses spends no token and does not enter the duplicate window."
  proto's composers run everything up to `secret` (and `duplicate` for an open question, n3f's);
  B runs the duplicate window, the forge check and `Admit`.
- **The duplicate window** (A 11.2): "60 s on `[verb or kind, ref, re, sorted to, text digest]`;
  absent parts are empty; a ping's digest is empty; notices exempt; a send refused earlier in the
  send order does not enter it." The digest is the SHA-256 of `teamchat.text`'s `text`. Exit 15
  `duplicate`, "it was already sent; do not resend".
- **The rates** (A 11.1): `send_per_minute` (default 20) is the bucket's refill per minute and
  `send_burst` (default 10) its capacity, from the roster's `limits`; the sender enforces them. A
  refusal is 14 `rate`. The admin is not limited.
- **The verbs** (A 8.1): `fetch`, `sync`, `review`, `run-qa`, `halt`, `check` expect an answer;
  `ack`, `nack`, `done`, `blocked` do not. The reply relation is forbidden for the first six,
  required for `ack` and `nack`, optional for `done` and `blocked`. `ref` is required for `fetch`,
  `sync`, `review`, `run-qa`, `done`, `blocked`; optional for `nack`; forbidden for `halt`, `check`,
  `ack`. `branch` only beside a `path` or `commit` reference. Senders: `fetch`, `sync`, `halt` the
  coordinator; `check` the probe only, with exactly one addressee; the rest the coordinator and
  workers. Addressable: a non-parked coordinator or worker; for `ack`, `nack`, `done`, `blocked`
  also a listed human. A worker's ping or text that names a human copies the coordinator (P21).
- **The answer form** (A 7.2): "a reply in the ping's thread: `{"rel_type": "m.thread", "event_id": <ping>, "is_falling_back": true, "m.in_reply_to": {"event_id": <ping>}}`. The answered
  ping is the thread root." A reply in main is `{"m.in_reply_to": {"event_id": <id>}}` (A 7.1, 7.3);
  main allows no other relation.
- **A carried ping** (A 7.7): "an answer (the answer form, rooted at the carried event) to a carried
  `ping` is addressed to its `from` when `from` is a current member, and is otherwise refused
  `target`." A carried item is delivered as a NOTICE and never forge-checked.
- **Answers and deadlines** (A 8.2): "An answer is an `ack`, `nack`, `done` or `blocked` whose reply
  relation names a ping this member sent, from a sender that ping addressed." "The deadline of a
  ping is its own `origin_server_ts` plus its timeout: `halt_ack_timeout_s` for `halt`,
  `ack_timeout_s` for the other verbs that expect an answer. An answer counts when its
  `origin_server_ts` is at or before the deadline, however late the member reads it." "The sender
  learns its ping's `origin_server_ts` when its own event comes back in sync (rule 3 of 7.6); until
  then B uses its local send time." "An answer from someone the ping did not address counts for
  nothing."
- **TIMEOUTs** (A 8.3): "When the member's clock passes a ping's deadline plus a 30 s grace, each
  addressee that has not answered gets exactly one TIMEOUT, keyed `<ping event ID>~<addressee localpart>` and marked reported." "A TIMEOUT is never retried and spends no flood budget."
  B 2.6 step 07: "Due TIMEOUTs come from the outbox on the state goroutine's timer (the earliest
  `due_ms`), not from a tick."
- **What is delivered** (A 8.4): a ping that expects an answer, and `nack`, `done`, `blocked`, are
  PING entries; an `ack` is "no entry of its own: recorded, and the next delivery for another
  reason carries an `answered:` line for it"; an on-time answer read after its TIMEOUT was
  delivered gets an `answered:` line ending ` (after its TIMEOUT)` (n3e renders it; n3d records).
- **The `outbox` record** (B 2.2.2): `{"kind":"outbox","team","id","verb","ref","to":[short names],"due_ms","answers":[{"from","verb","id","late"}],"timed_out":[]}`, "one ping this member
  sent that still expects an answer or was answered in the last `ack_timeout_s`". `sent` is
  `{"kind":"sent","team","id","room"}`.
- **The forge check** (B 2.6 step 11, A 9.2-9.3): see D2.

## 4. D1: the shared types (lands first)

Packages: `member/state`, `member/records`, `member/wire`, `member/team`, and two names in
`failpoint`. Each change is small and additive; together they are what D3-D6 compile against.
No new import edges.

### API

```go
// member/state: the sending entry carries what a re-send needs (n3d-D1)
type Sending struct {
	Txn     string          `json:"txn"`      // ^tc[0-9a-f]{32}$ (the store's grammar, n3c-D18)
	Room    string          `json:"room"`     // a store room purpose: main (questions and reports are n3f's)
	Kind    string          `json:"kind"`     // text | ping
	SinceMS int64           `json:"since_ms"` // wall ms when it was charged
	Type    string          `json:"type"`     // the event type, m.room.message
	Content json.RawMessage `json:"content"`  // the composed content, a strict object, at most 16384 bytes (A 11.2)
	Tried   bool            `json:"tried"`    // a PUT may have reached the server (n3d-D4)
	Ping    *SendingPing    `json:"ping"`     // set for a ping whose verb expects an answer, else null
}
type SendingPing struct {
	Verb      string   `json:"verb"`
	Ref       *string  `json:"ref"`
	To        []string `json:"to"`         // user ids, 1-16 (n3d-D5)
	TimeoutMS int64    `json:"timeout_ms"` // ack_timeout_s or halt_ack_timeout_s, from the roster at send time
}
// Caps (n3d-D1): sending 32 entries of at most 20 KiB; the file cap is recomputed as the sum of
// the list caps, rounded up to the next 256 KiB (about 2.6 MiB), so a list cap still fires first.
// The Go names above are proposals: keep the built type's names where it already has these four
// B fields, and add the rest.

// member/records
type Sent struct { Team paths.TeamID; ID *string; Room string; Queued bool } // id null exactly when queued (n3d-D2)
type Queued struct { Team paths.TeamID; Room string; Send string; Verb *string; To []string; SinceMS int64; Tried bool } // kind "queued", in status (n3d-D21)
// Outbox: each answer's verb and id may be null (n3d-D8); its other fields as built.

// member/wire
type ReplyArgs struct {
	Team *paths.TeamID `json:"team"`; ID string `json:"id"`
	Verb *string `json:"verb"`; Ref *string `json:"ref"`; ToCoordinator bool `json:"to_coordinator"`
}
type SayArgs struct  { Team *paths.TeamID `json:"team"`; To []string `json:"to"`; ToCoordinator bool `json:"to_coordinator"` }
type PingArgs struct {
	Team *paths.TeamID `json:"team"`; Verb string `json:"verb"`; Ref *string `json:"ref"`
	To []string `json:"to"`; ToCoordinator bool `json:"to_coordinator"`; Branch *string `json:"branch"`
}
// The text is Request.Stdin: required for say and a plain reply, null for ping and reply --verb (n3d-D15).

// member/team
type Answer struct { Ping, From, Verb, ID string; OriginMS int64 } // an answer-form event to an own ping
type Echo struct { ID string; OriginMS int64 }                       // an own ping come back in sync
// Batch: Answered becomes []Answer (replacing []proto.Answered, which is the delivery's
// trailing-line type, unless go doc shows proto.Answered carries exactly these fields);
// Batch gains Echoes []Echo.
// Item gains Trust string: "trusted" | "external" for a forge-checked ping, "" otherwise (n3d-D12).
type ForgeResult struct { Outcome string; Trust string } // Outcome: proven | provenance | unresolved | forge-rate
var ErrForgeLogin = errors.New("team: no forge login")   // gh missing, or not logged in
type Forge interface {
	// Check proves one reference against the roster's forge section (A 9.2-9.3). The two proto
	// types are the ones go doc shows for Verdict.Forge and the roster's forge section.
	Check(ctx context.Context, need proto.ForgeNeed, f proto.ForgeSection) (ForgeResult, error)
}

// failpoint (PT1 and n3d-D16)
const (
	LinkAfterChargeBeforeSend Name = "link-after-charge-before-send"
	LinkAfterSendBeforeRecord Name = "link-after-send-before-record" // PT1: "after a send's PUT before its answer is recorded"
)
```

`proto.ForgeNeed` and `proto.ForgeSection` are placeholders: D1's builder reads proto with go doc,
puts the real type names in `team.Forge`, and records them in the report for D2, D4 and D6.

### Facts it needs

- The store's strict decoder (n3a) round-trips every struct: a `json.RawMessage` content must be a
  strict object (proto's strict parse, no duplicate key, no float), measured by
  `proto.Value.Size` as every entry is. A `Sending` with `kind` `ping` and an answer-expecting verb
  has `ping` set; `kind` `text`, or a ping whose verb expects none, has it null.
- Every list cap must still fire before the file cap: the existing test that builds a full store
  proves it, and must pass with the new caps.
- `records`' `List[T]` encodes nil as `[]`; every record has `team`.
- Changing `Batch.Answered`'s type must leave the engine and teamsync compiling: both pass or ignore
  nil today.

### Tests

- A store with 32 `sending` entries, each at 20 KiB, round-trips; a 33rd is `ErrFull`; an entry
  one byte over is refused by the decoder; a `content` that is not an object, or holds a float, is
  refused; `ping` set on a `text` entry is refused.
- The full-store cap test passes with the recomputed file cap.
- `Sent` with `queued: true` and an `id` encodes `id` null or is refused (pick one, and test it);
  `Queued` round-trips.
- Each new args struct refuses a missing `team` key and an unknown key (10 `schema`).
- The release-build failpoint scan sees the two new names.

## 5. D2: the forge package (SD5)

Package: new `internal/teamchat/member/forge`. Imports: proto, clock, execx, std (`net/http`,
`crypto/sha256`). It does not import `team`: it meets `team.Forge` structurally, and a compile-time
assertion in its test file (which may import team) proves it.

### API

```go
// The gh credential (R44): read through execx when a check needs it, held at most 10 minutes,
// dropped at once after a 401. Never written or logged; Token's String and GoString redact.
type Token struct{ /* unexported */ }
type Login struct{ /* unexported */ }
func NewLogin(r execx.Runner, c clock.Clock) *Login
func (l *Login) Token(ctx context.Context) (Token, error) // team.ErrForgeLogin-compatible error for a missing or logged-out gh
func (l *Login) Drop()

type Config struct {
	Login  *Login
	Clock  clock.Clock
	Origin string            // tests only (export_test.go); production is proto's origin for forge.kind github
	Client *http.Client      // nil: a client with no redirects followed off the origin
}
type Checker struct{ /* unexported: the caches, safe for concurrent use by every team */ }
func New(cfg Config) *Checker
func (c *Checker) Check(ctx context.Context, need proto.ForgeNeed, f proto.ForgeSection) (team.ForgeResult, error)
```

The command is `gh auth token --hostname github.com`, run through `execx.Runner` (a
`dynamicNames` row for `gh`). Its stdout is the token, trimmed of one trailing newline, and must be
1-1024 bytes of `\x21-\x7e`, else it is a login failure. `gh` not found, an exit status other than
0, or an empty token is `ErrForgeLogin`.

### Facts it needs, digested

- **When it runs** (B 2.6 step 11): "the link runs `gh auth token --hostname github.com` through
  `execx` when a check needs it, holds the credential in memory for at most 10 minutes and drops
  it at once after a 401, and keeps positive results for 3600 s in memory. A missing or logged-out
  `gh` is a configuration failure of the forge only (exit 78, `forge-login`, on a verb that needs
  it)." A check whose results are all cached runs no `gh` at all.

- **The token's use** (A 9.3): "sent only as an `Authorization` header to the origin fixed per
  `forge.kind` in `teamchat/proto` (`https://api.github.com` for `github`, the only kind), and
  never written or logged." A redirect to any other origin is refused, and the token is never sent
  there.

- **The class** (A 9.2), from the roster's `forge` section: in `exclude`, drop `excluded`; in
  `repos`, listed (its `branches`, when given, the only branches that prove it); owner `org`, org;
  else external, which a verb taking no external reference drops `allowlist`. Listed wins over org.
  If go doc shows proto's verdict already decides the class (`excluded`, `allowlist`), the checker
  receives the class and does not decide it again.

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
  parent's API; only reachability from a branch of a trusted repository proves provenance."
  The GitHub REST calls: `GET /repos/{o}/{r}` (owner login, `fork`, `private`, `default_branch`),
  `GET /repos/{o}/{r}/branches/{b}`, `GET /repos/{o}/{r}/compare/{base}...{sha}` (`status`),
  `GET /repos/{o}/{r}/contents/{path}?ref={sha}` (`type` `file`), `GET /repos/{o}/{r}/pulls/{n}`
  (`head.repo.full_name`, `head.sha`, `author_association`), `GET /repos/{o}/{r}/issues/{n}`
  (`pull_request` absent). "At or ahead of SHA" is `compare/{sha}...{head}` `identical` or `ahead`.

- **Results** (A 9.3): proven, with trust `trusted` for listed or org and `external` otherwise;
  `provenance` (found but not proven); `unresolved` ("not found, wrong kind, forge unreachable,
  forge refused the credential"); `forge-rate` ("the forge rate-limited the check, which says
  nothing about the reference").

- **Caching** (A 9.3, B 2.6 step 11): positive results only. Repository metadata, reachability,
  pull requests and branches for 3600 s ("so a repository that leaves the organisation or becomes a
  fork stops being trusted within that time"); file and commit content at a SHA for the link's
  life (A gives 30 days; the cache is memory, and a link lives less). Bounded: 1024 entries per
  cache, oldest evicted (n3d-D22).

- **HTTP** (A 9.3, 11.3): 15 s per request, on the clock seam (`clock.AfterFunc` cancelling the
  request's context, as matrix's per-try deadline does), never real time in tests; an 8 MiB
  response cap; redirects only to the same origin; 429 handled as A 11.3: `Retry-After` in seconds,
  else a reset header, else 5 s; three tries; a wait over 60 s is not slept but is `forge-rate`. A
  401 drops the token, re-reads it once and retries once; a second 401 is `unresolved`.

### Seams and tests

A fake GitHub on `httptest` (an origin the test passes through `export_test.go`), scripted per
route; `execx.Fake` scripting `gh`; `clock.Fake`.

- SD5-1: a check with nothing cached runs `gh` once; a second check inside 10 minutes runs none;
  past 10 minutes (on the fake clock) it runs again.
- SD5-2: a 401 drops the token at once: the next request re-reads it; a second 401 is `unresolved`.
- SD5-3: positive results are kept 3600 s: no second request at 3599 s, one at 3600 s; a negative
  result is never cached.
- SD5-4: `gh` absent (the fake's not-found), exiting 1 (logged out), or printing an empty or
  over-long token: `ErrForgeLogin`; a check whose facts are all cached never runs `gh`.
- SD5-5: each row of both proof tables, proven and not, with `trusted` and `external`; a commit
  served from a fork's parent but on no proving branch is `provenance`.
- SD5-6: the token appears only in the `Authorization` header of requests to the origin; a
  redirect to another origin is refused and that origin receives no request; a 8 MiB + 1 body is
  refused; a request held past 15 s on the fake clock is `unresolved`.
- SD5-7: 429 with each wait source, three tries, and a wait over 60 s is `forge-rate` with no
  sleep.
- B11-style source test: no writer call is handed a value of type `Token`; `fmt` of a `Token`,
  `%v`, `%+v` and `%#v`, shows no token.

## 6. D3: the outbox and TIMEOUTs in the engine (SD4; SY4's outbox half; LN5's `outbox`; SY7 case 4)

Package: `member/engine`, new files `outbox.go` and `timeout.go`, plus the hunks in `Persist`,
`status` and `Add` that call them. Imports unchanged.

### API

```go
// No new exported API. Inside the engine (D6 calls the first two):
func (e *Engine) addOutboxPing(s *state.Store, p state.Ping) error // on the goroutine; ErrFull is 78 store
func (e *Engine) armTimeouts()                                        // on the goroutine
```

### Facts it needs, digested

- **Persist applies the outbox updates** in its one write (B 2.6 step 06: "the batch's accepted
  items appended to `pending`, `next_batch`, the room floors and the outbox updates the batch made
  (an `answered` ping)"):
  - **an answer** (`Batch.Answered`): find the outbox ping by `Ping`. `From` must be in its `to`
    (index i), else it counts for nothing (logged). When `OriginMS <= due_ms`, set i in `answered`
    if not there yet, whether or not i is already in `reported` (a TIMEOUT that fell due does not
    stop an on-time answer counting, A 8.4). When `OriginMS > due_ms` it is late: not counted, the
    TIMEOUT still holds. Either way the answer's verb, id and lateness go into the engine's
    in-memory answer table for `status` (n3d-D8). An answer to a ping not in the outbox is
    ignored with one log line.
  - **an echo** (`Batch.Echoes`): set the ping's `sent_ms` to `OriginMS` and `due_ms` to
    `OriginMS + (due_ms - sent_ms)` (the timeout is unchanged), then re-arm the timer.
- **The TIMEOUT timer** (n3d-D7): one `clock.AfterFunc` for the whole engine, armed for the
  earliest `due_ms + 30 000` over every held team's outbox pings with an addressee that is in
  neither `answered` nor `reported`, as `due + 30 000 - Clock.WallMS()` (0 if past). It is re-armed
  after every write that changed an outbox, after `Add`, and when it fires. It fires a function
  that runs on the state goroutine (through `do`), which:
  - re-reads `WallMS()`, and re-arms without writing if nothing is due (a wall step back);
  - for each team with something due, in one store write: adds one pending TIMEOUT item per silent
    addressee (id `proto.TimeoutKey(ping id, addressee localpart)`, item `timeout`, `from` the
    addressee's user id, `sent_ms` the deadline plus 30 000), and adds the addressee to `reported`
    in the same write, so a crash makes it once or not at all, never twice;
  - wakes the waits and the delivery, as `Persist` does;
  - a store whose write fails (pending full even in the reserved 256) is retried 60 s later, with one
    log line per team until it succeeds.
    It is never a tick: with no ping owed, no timer is armed.
- **Pruning** (B 2.2.2 "answered in the last `ack_timeout_s`"): in the timer's write, and in any
  outbox write, a ping whose every addressee is in `answered` or `reported`, and whose `due_ms`
  plus its own timeout (`due_ms - sent_ms`) has passed, leaves the outbox.
- **A stopped or down team** keeps its store loaded and its timer duty (n3d offline rule). A
  refused store has no outbox, so nothing is owed from it until the owner's repair. A removed team
  drops its outbox with its store (LN8; already one log line counting items: add the pings).
- **`status`'s `outbox` records** (LN5): after a team's `room` records, one `outbox` record per
  outbox ping, oldest `sent_ms` first: `to` and `timed_out` as short names (P4's `namer`), `due_ms`,
  and `answers` one per answered index, plus one per late answer in the memory table, with `late`
  set; `verb` and `id` from the memory table, null for an answer recorded before this link started
  (n3d-D8).
- **SY7 case 4**: an answer held only by a discarded sync answer never reaches `Persist`, so the
  ping stays unanswered and its TIMEOUT is made. The engine half of that test is here; teamsync's
  "a discarded answer is never persisted" is P5's.

### Seams and tests

`clock.Fake` (`Advance`, `StepWall`, `Waiters`); a fake `team.Team`; stores seeded with outbox pings
through the test's own `StoreWriter`; `Persist` driven with built `Batch`es.

- SD4-1: a ping with two addressees, one answering on time: at deadline + 30 s exactly one TIMEOUT
  for the other, in `pending`, with `reported` set in the same write; at deadline + 29.999 s none.
- SD4-2: an answer whose `OriginMS` is at the deadline counts; one at deadline + 1 ms does not, and
  the TIMEOUT is made; both show in `status` (the second `late`).
- SD4-3: an answer read after its TIMEOUT was made, on time by server time: `answered` gains it,
  no second TIMEOUT.
- SD4-4: an answer from a sender the ping did not address counts for nothing.
- SD4-5: an echo moves `sent_ms` and `due_ms`; the timer follows it.
- SD4-6 (never a tick): with no ping owed `Waiters()` counts no outbox timer; with three pings owed
  it counts one; a `StepWall` back makes the firing re-arm without writing.
- SD4-7: a crash (a writer that fails after the timer's write is staged, and a fresh engine over
  the same store) never makes a TIMEOUT twice.
- SD4-8: a team marked `Down` and one stopped with `token` still get their TIMEOUTs.
- SD4-9: pruning at exactly `due + timeout` and 1 ms before.
- SD4-10: `status` prints the `outbox` records with short names, `answers` and `timed_out`, and a
  restarted engine prints `verb` and `id` null for an earlier answer.
- SD4-11: pending full of HUMAN and agent items with the reserved 256 used: the TIMEOUT write is
  retried 60 s later and succeeds once room is made.
- SY7-4 (engine half): a batch sequence in which the answer's batch is never persisted leaves the
  ping unanswered and makes its TIMEOUT.

## 7. D4: teamsync's receive side (SY4's answers, the own-ping echo, SD5's receive check)

Package: `member/teamsync`, in `session.go` (an answer's steps) and `ln6.go` (Refetch). It gains a
`Config.Forge team.Forge` field (nil: no forge, every forge-checked ping dropped `unresolved`, which
only tests use).

### API

```go
type Config struct {
	// ... as built, plus:
	Forge team.Forge
}
```

### Facts it needs, digested

- **Answers** (SY4, A 8.2): after `Judge`, every verdict that is an answer to an own ping (proto's
  `Record` outcome for an `ack`, and a PING verdict whose `Answer` names an own ping for `nack`,
  `done`, `blocked`, as go doc shows `Verdict.Answer`) gives one `team.Answer{Ping, From, Verb, ID, OriginMS}` in `Batch.Answered`. Whether it is on time is the engine's to judge from `due_ms`.
  A PING verdict that is an answer is still queued as its item; an `ack` queues nothing.
- **Echoes** (A 7.6 rule 3): an own event that `Judge` ignores `self`, whose id is in `View.OwnPings`,
  gives `team.Echo{ID, OriginMS}` in `Batch.Echoes`.
- **Both ride on the batch only**, so a discarded answer (SY6) carries neither, and they are
  persisted in the same write as the position (P3).
- **The receive forge check** (SD5, A 9.3 "every receiver before it delivers"): after `Judge`, for
  each accepted verdict whose `Forge` is set (a non-carried ping with a reference), call
  `Forge.Check` before SY6 and SY5, under a budget of 30 s per ping on the clock seam. Proven: the
  item is queued with `Item.Trust`. `provenance`, `unresolved` or `forge-rate`: dropped with that
  reason (counted, one log line naming the event id and the reason), as A 9.3 says (n3d-D10).
  `ErrForgeLogin`: dropped `unresolved`, and the log line names `forge-login`, once per link until a
  check next succeeds. Every check runs on the team goroutine: a slow forge holds this team's
  answer only.
- **Refetch runs the same check** (B 2.7.3: a restored item is judged again), so a restored ping
  carries `Trust` again, or is dropped with its reason.
- **Own sends** (SY4a): `Notices().RecordOwnSend` is built; D6 calls it. Nothing to add here but the
  test that a human's thread reply under an own `say` recorded that way is delivered with a store
  holding no record of it (SY4a's first test, now through the real send path's call).

### Seams and tests

fakehs; a fake `team.Engine` that records batches; a fake `team.Forge` scripted per reference.

- SY4-n3d-1: an `ack` in an own ping's thread gives one `Answer` and no item; a `done` gives one
  `Answer` and one PING item.
- SY4-n3d-2: an own ping's echo gives one `Echo` with its `origin_server_ts`.
- SY4-n3d-3: an answer in an answer that SY6 discards is in no persisted batch (SY7 case 4,
  teamsync half; the restore as in `TestARestoreUnderALongPoll`).
- SD5-r1: a ping whose check is `provenance`, `unresolved` or `forge-rate` is dropped with that
  reason and counted; a proven one carries `Trust`.
- SD5-r2: `ErrForgeLogin` drops `unresolved` and logs `forge-login` once.
- SD5-r3: a carried ping is never checked; a ping without a reference is never checked.
- SD5-r4: a check that never answers ends at 30 s on the fake clock, and the team's next answer
  goes on.
- SD5-r5: `Refetch` of a restored ping runs the check again.

## 8. D5: the client verbs `reply`, `say`, `ping` (SD1-SD2's CLI half)

Package: P6's `member/client` (**as designed, not yet built**; start once P6 has merged into
`feat/bus-v2-n3d`). It gains the three verbs in `Main`'s table.

### API

```go
func Main(verb string) cli.RunFunc // P6's; now also "reply", "say", "ping"
```

### Facts it needs, digested

- The request: `wire.OpReply`, `OpSay`, `OpPing`, with D1's args, `deadline_ms` from
  `wire.Deadline` (30 s for sends) on `clock.Boot()`, `kit` the CLI's own digest (8a).
- **stdin** (n3d-D15): `say` and a `reply` without `--verb` read stdin to EOF, at most 4097 bytes:
  4097 is 10 `size` (A 11.2's agent text is 1-4096 bytes), empty is 10 `empty`, and a stdin that is
  a terminal is 64 `usage` ("text on stdin"), checked before reading. `ping` and `reply --verb`
  never read stdin, and send `stdin` null.
- The answer: record lines to stdout (`sent`), the error object to stderr, exit `done.exit`. A
  `sent` with `queued: true` is exit 0 (n3d-D2).
- LN3's no-link answers are P6's and apply unchanged: a send never reconnects (only `wait` does).

### Tests

- SD-c1: each verb's request bytes for each flag combination, `team` null without `--team`.
- SD-c2: stdin rows: 4096 bytes sent, 4097 is 10 `size`, empty is 10 `empty`, a pty is 64, and
  `ping` with data on stdin sends `stdin` null and does not read it.
- SD-c3: a `sent` record and a queued `sent` record are copied and exit 0; an error object is copied
  to stderr with its exit.

## 9. D6: the send ops in the engine (SD1, SD2, SD3; the queue; the restart re-send)

Package: `member/engine`, new files `send.go` (the ops), `resolve.go` (team, room, addressees) and
`sender.go` (the per-team sender goroutine), with the hunks in `Serve`, `Add`, `Remove` and
`status` that call them. `engine.Config` gains `Forge team.Forge`. This is the largest part; if the
builder nears its budget, the restart re-send and its failpoint tests (the last three bullets of
"the sender") are the part to hand off.

### API

```go
type Config struct {
	// ... as built, plus:
	Forge team.Forge
}
// Serve answers OpReply, OpSay and OpPing.
```

### Facts it needs, digested

- **The op's steps**, in A 9.3's order, each refusal the first that applies and nothing spent by it:
  01. **Decode** the args (`wire.DecodeArgs`; 10 `schema`).
  02. **Resolve** the team, room and addressees (below). On the goroutine: the store reads; off it:
      `Notices().Roster()`.
  03. **No roster in memory** (`Roster()` false): 20 `unreachable` (n3d-D3).
  04. **The team stopped permanently** (`Health.Stopped` token, forbidden, untrusted): 21 or 22, as
      its other verbs (SY8). Nothing is charged.
  05. **Compose** with `proto.Composer` (`Text` or `Ping`), whose refusals map to 10 (their code),
      11 `role`; the composer's secret check uses `Notices().Token()` (R43).
  06. **A ping to more than 16** addressees: 64 `usage` naming the bound (n3d-D5).
  07. **The duplicate window** (A 11.2) against the store's `gate.recent`: 15 `duplicate`.
  08. **The forge check** (off the goroutine, `Config.Forge`), for a ping (an answer included) with a
      reference: `provenance` is 13 `provenance`; `unresolved` is 13 `forge`; `forge-rate` is 14
      `forge-rate`; a forge that does not answer is 20 `forge` (n3d-D11); `ErrForgeLogin` is 78
      `forge-login`.
  09. **The charge**, one store write on the goroutine (SD3): the duplicate window checked again
      (two identical sends racing), `proto.Bucket.Admit` on `gate` (14 `rate`), the duplicate key
      added to `gate.recent` (entries older than 60 s dropped), and the `sending` entry appended with
      its content, `tried` true when the team is up and false when it is down. `sending` full is 78
      `store`, and the outbox full, for an answer-expecting ping, is 78 `store`, both checked here
      before anything is written. `failpoint.Hit(LinkAfterChargeBeforeSend)` follows the write.
  10. **The answer**: the team down (`Health.Down`) or older entries queued before this one: `sent`
      `queued` at once. Otherwise hand it to the team's sender and wait for its answer until the
      request's deadline; posted: `sent{id, room}`; still not posted at the deadline: `sent`
      `queued` (the PUT goes on, B 2.4.4 "a request already started when its CLI goes away runs to
      its end").
- **Resolving** (SD1, SD2, CL6):
  - `say`, `ping`: `--to` names are matched per active team against the verified roster: a human by
    localpart, an agent by its short name when unique in the team, else its full handle (the
    `namer` rule, P69). `--to-coordinator` is the team's coordinator. With `--team`, that team.
    Without it: the team in which every name resolves; in two such teams 64 `ambiguous-team` (CL6,
    through `cli.ResolveTeam` with the candidates as the active list); in none 64 `unknown-member`;
    a name matching two members of the chosen team 64 `ambiguous-member`. The room is main.
  - `reply <id>`: the team whose store's ledger holds the id (`single()` as built). Then, from the
    ledger entry (room, item, thread, from), without the server (n3d-D13):
    - room `main` is n3d's; an item in `questions` answers "unbuilt" (n3f's `thread`).
    - no `--verb`: agent text replying to the item (`m.in_reply_to` the id), `to` its `from`, plus
      the coordinator under `--to-coordinator` (n3d-D14);
    - `--verb V` on a `ping` item with `thread` null: the answer form rooted at the id, `to` its
      `from`, `--ref` where V takes one; on any other item 10 `re`; `--verb` with
      `--to-coordinator` is 64 `usage`;
    - a `notice` item (a carried item): fetch it (`Team.Refetch`, main) to read its carried object;
      only a carried ping takes `--verb`, addressed to its `from` when that is a current member,
      else 10 `target` (A 7.7). A fetch that does not answer is 20 `unreachable`.
  - An id in no ledger: `--team` or the only active team (else 64 `ambiguous-team`), then
    `Team.Refetch(main, id)`: a 404 is 23 `not-found`; a verdict that decodes no item (a drop) is 23;
    an own event is 64 `usage`; otherwise as the ledger case, from the verdict. A fetch that does not
    answer is 20 `unreachable` (n3d-D3's reasoning: nothing is charged, so nothing is lost).
  - `ping` with `ack` or `nack` (a reply relation required) is 64 `usage` pointing at `reply --verb`,
    if the built parser does not already refuse it (n3d-D15).
- **The sender**, one goroutine per held team, started at `Add`, ended by the team's context:
  - it takes `sending` entries oldest first and posts each by `Notices().Send` with its `txn`,
    `type` and `content`; matrix retries an `Unreachable` 3 times 1 s apart (M22);
  - posted: one store write removes the entry and, for a ping with `ping` set, adds the outbox ping
    (`id` the event id, `sent_ms` the local wall time, `due_ms` `sent_ms + timeout_ms`, through D3's
    `addOutboxPing`); then `Notices().RecordOwnSend(id, Related)` with this member as `Sender`, the
    verb, `AddressedToSelf` false and `AddressedToRosterAgent` true when `to` names a roster agent
    (SY4a); `failpoint.Hit(LinkAfterSendBeforeRecord)` sits between the PUT's answer and that write;
  - transient (`Transient()`, a 429 past its retries included): the entry stays; the sender waits for
    the team to be up again (a `SetHealth` with `Down` false and a newer `LastSyncOKMS`, which the
    engine already receives) or 30 s, whichever is first, then tries the oldest entry again;
  - permanent (401, 403, a refused trust): the entry is removed with one log line, and a waiting op
    answers 21 or 22; a queued one is said in that log line and in `status` (n3d-D2);
  - at `Add` (a new link, or a re-add), entries with `tried` true are first looked for in main:
    `Messages` backwards from the room's current position, one page of 100, for an own event whose
    content equals the entry's (compact JSON) and whose `origin_server_ts` is not before `since_ms`;
    found: recorded as posted with that id, no PUT. Not found, or `tried` false: posted by the same
    txn (n3d-D4);
  - `Remove` drops a team's queued sends with its store, with one log line counting them
    (n3d-D23).
- **`status`**: after the `outbox` records, one `queued` record per `sending` entry (n3d-D21).
- **The offline rule**: the state goroutine never waits on the sender; the op waits on its own
  connection goroutine; one team's sender never touches another team.

### Seams and tests

fakehs (with its drop-before and drop-after faults and `Restart`) behind a scripted `team.Team`
whose `Notices()` is a `matrix.Client` against fakehs; a fake `team.Forge`; `clock.Fake`; the CLI
side as a byte-level client (P4's harness). The real-process tests are W's.

- SD1-1: `reply` to a human item in main: text with `m.in_reply_to`, `to` the human; a worker's reply
  to a human copies the coordinator (P21), as the composer does.
- SD1-2: `reply --verb ack` to a ping: the answer form rooted at it, `to` its sender; `--verb done --ref`
  with a ref, forge-checked; `--verb` on a peer item, or on an answer item (thread set), is 10 `re`.
- SD1-3: a carried ping answered to its original sender; one whose `from` is no longer a member is 10
  `target`.
- SD1-4: an id in no ledger: fetched and replied to; 404 is 23; with two teams and no `--team` 64
  `ambiguous-team`; an own event is 64.
- SD2-1: `--to` names resolved per team; one team's name in two teams with no `--team` is 64
  `ambiguous-team` (CL6's row); an unknown name 64 `unknown-member`; an ambiguous short name 64
  `ambiguous-member`; `--to-coordinator`.
- SD2-2: a bare `ping` is 64 naming the verbs and `status` (built; assert the test exists, else add
  it); `ping ack` is 64 pointing at `reply --verb`.
- SD3-1: the charge and the `sending` entry are in one store write, before any PUT (a fake writer
  that fails the write: no PUT, nothing spent).
- SD3-2: three tries in the deadline: fakehs dropping the first PUT after handling gives one event in
  the room (the same txn), `sent` with its id.
- SD3-3: the duplicate window: the same text to the same addressee at 59.999 s is 15, at 60 s is
  sent; two `say`s of different text both send; a send refused by the forge check (13) did not
  enter the window; two identical sends racing: one is sent, one is 15.
- SD3-4: the bucket: with `send_burst` 10, the 11th send in a burst is 14 `rate`; refilled at
  `send_per_minute` on the fake clock; a 14 spends nothing and enters no window.
- SD3-5: the send order: a text that is both a secret and to an unknown member is refused by the
  first in A's order.
- SD3-6 (offline): the team `Down`: `say` answers `sent` queued at once, exit 0; `status` shows one
  `queued` record; the team up again: the sender posts it once, by its txn, and `status` shows none.
  Another team's `say` meanwhile is posted at once.
- SD3-7 (offline): every try fails transiently for the whole 30 s: the op answers queued at the
  deadline; fakehs back: posted once.
- SD3-8: a 401 on the PUT removes the entry and the waiting op is 21; the spent token stays spent.
- SD3-9: `sending` full: the 33rd send is 78 `store` with nothing charged; the outbox full: an
  answer-expecting ping is 78 `store`.
- SD3-10 (restart, in process): an engine started over a store holding a `tried` entry that fakehs
  already has (the same content) records it without a PUT; one fakehs lacks is posted by its txn;
  a `tried: false` entry is posted without a search.
- SD3-11: queued sends post oldest first, and a new send behind them answers queued.
- SD4-n3d: a sent ping enters the outbox with `due_ms`; a `say` does not; a `done` does not.
- SY4a-n3d: each post calls `RecordOwnSend` with the flags above.
- n3d-D3: no roster in memory: 20 `unreachable`, nothing charged, nothing in `sending`.

## 10. W: wiring (after D2-D6 and P6)

- `link.Main` builds one `forge.Checker` (`forge.NewLogin(execx.OS{}, clock.System())`) for the
  link and passes it as `engine.Config.Forge` and, through W's `TeamConfig`-to-`teamsync.Config`
  adapter, as `teamsync.Config.Forge`.
- `cmd/teamchat`: `reply`, `say`, `ping` -> `client.Main(verb)`; remove the three from `cli`'s
  `unbuilt` table.
- The `dynamicNames` row for `gh` is D2's; W checks the production call site is the one it names.
- End-to-end, with real processes (the test binary as `teamchat` through reexec, fakehs over HTTP,
  `run --plain --state <tmp> --seat T/S`), two members on one team, a coordinator C and a worker
  K, using refless verbs so no forge is needed (the forge is proven in process, D2, D4, D6):
  - C `ping halt --to K`; K `recv` shows the PING; K `reply <id> --verb ack`; C `status` shows the
    `outbox` record answered.
  - C `say --to K`; K `recv`; K `reply <id>` with text; C `recv` shows the PEER.
  - fakehs stopped: C `say` answers queued, exit 0; fakehs back: K receives it once.
  - Under `busfailpoints`, `link-after-charge-before-send` and `link-after-send-before-record`:
    the keeper restarts the link, and the text is in the room exactly once (P1 and P5's "at most
    once" for sends).
- TIMEOUTs are not end-to-end (the shortest real deadline is 60 s): SD4-1 to SD4-11 hold them.

## 11. Decisions n3d takes (the spec is silent)

| ID      | Decision                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n3d-D1  | A `sending` entry carries what a re-send needs: the event type, the composed content (at most 16384 bytes), `tried`, and for an answer-expecting ping its verb, ref, addressees and timeout. B 2.3.2's entry (txn, room, kind, since_ms, 128 bytes) holds no content, so "a link that restarts re-sends it" could not. The content is in the store, not a side file, so the charge and the content are one write (SD3), and a removal or a set-aside takes them with the store. Caps: 32 entries of at most 20 KiB; the file cap is the new sum, rounded up to the next 256 KiB. A store with sends queued costs up to 640 KiB more per write while they wait. Proposed B 2.3.2 text: "`sending` holds a send whose transaction id is chosen but whose answer has not arrived, with its event type and content, so a restarted link re-sends it as it was." |
| n3d-D2  | The owner's offline rule: a send to a team that is down, or whose tries fail transiently for the request's whole deadline, is queued: the verb exits 0 with `{"kind":"sent","team","id":null,"room","queued":true}`, `status` shows a `queued` record, and the team's sender posts it by the same txn when the team is up. A permanent failure at the PUT (401, 403, trust) removes the entry with one log line, and a waiting op exits 21 or 22. A 429 past its retries is transient and queues, so sends never exit 14 `server-rate` (kept for n3f's reports room, which is verified at each send).                                                                                                                                                                                                                                                       |
| n3d-D3  | A send needs the verified roster (addressing, role, limits). A team the link has not verified since it started has none, so the verb exits 20 `unreachable` with nothing charged or recorded. Persisting the roster to queue such a send was weighed and left out: it adds a store field for one narrow case (a link started while its homeserver was down), and nothing is lost by refusing, since nothing was accepted. A reply that needs a fetch (an unknown id, a carried item) is the same.                                                                                                                                                                                                                                                                                                                                                           |
| n3d-D4  | The re-send after a link restart (B 2.4.5) first looks for an entry with `tried: true` in main: one page of 100 own events back from the room's current position, matched by equal compact content and an `origin_server_ts` not before `since_ms`. A found event is recorded as posted, with no PUT. This covers B section 4's risk (a homeserver that forgets transaction ids across its restart) and a token reissued between the tries (a new device, which a txn does not span), unconditionally, at one GET per re-sent entry.                                                                                                                                                                                                                                                                                                                        |
| n3d-D5  | A member's ping names at most 16 addressees, the store's outbox bound (B 2.3.2), not A 11.2's 32: a 17th is 64 `usage` before anything is spent. Proposed A 11.2 text: "ping object 4096 bytes; `to` 1-32 on the wire, 1-16 from a member (B's outbox bound)".                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| n3d-D6  | Only a ping whose verb expects an answer enters the outbox. A ping leaves it once every addressee is answered or reported and its deadline plus its own timeout has passed ("answered in the last `ack_timeout_s`", B 2.2.2).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| n3d-D7  | The TIMEOUT timer is one `clock.AfterFunc` armed on wall milliseconds (deadlines are server times, CK1), re-checked against `WallMS()` when it fires; a wall step back re-arms it, a step forward makes it fire early and correctly. `reported` is set in the same store write that queues the TIMEOUT item, so a TIMEOUT is made at most once ("marked reported once delivered" in A 8.3 is read as "once queued for delivery", since the pending item survives a restart).                                                                                                                                                                                                                                                                                                                                                                                |
| n3d-D8  | The store's `answered` stays an index list (B 2.3.2; a ping entry with 16 addressees and a 700-character ref already fills its 3 KiB). The `outbox` record's `answers` take `from` from the index, and `verb`, `id` and `late` from an in-memory table the engine fills at `Persist`; an answer recorded before the link started prints `verb` and `id` null. A late answer (after the deadline) is recorded in memory only: it does not count (A 8.2). Proposed B 2.2.2 text: "an answer recorded before the link started has `verb` and `id` `null`".                                                                                                                                                                                                                                                                                                     |
| n3d-D9  | The own ping's echo rides on the batch as `Batch.Echoes`. An echo that arrives before the send's answer has added the ping to the outbox finds no ping and is dropped, so `due_ms` stays on the local send time, which A 8.2 allows. An answer cannot race the same way in practice: it needs the addressee to sync, deliver and act, while the send's answer is one round trip.                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| n3d-D10 | On the receive side a ping whose forge check fails is dropped as A 9.3 says, including `unresolved` for a forge that does not answer and for a member with no `gh` login (logged as `forge-login`, once per link until a check succeeds). It is never held: holding would stop the team's sync on GitHub's health. The loss is said: the drop is counted and logged, and the sender's ping is unanswered, so the sender gets its TIMEOUT.                                                                                                                                                                                                                                                                                                                                                                                                                   |
| n3d-D11 | The send side's forge results: `provenance` 13 `provenance`; `unresolved` 13 `forge`; `forge-rate` 14 with a new code `forge-rate` (A 9.3: it "is not the bus's `rate`"); a forge that did not answer 20 with a new code `forge` (class wait: "push first" would be wrong advice for an outage); no login 78 `forge-login`. Proposed B 2.2.4 rows: 14 gains `forge-rate`, 20 gains `forge` ("the homeserver or the forge did not answer").                                                                                                                                                                                                                                                                                                                                                                                                                  |
| n3d-D12 | A forge-checked ping's trust (`trusted`, `external`) is held in memory with its item (`team.Item.Trust`), not in the store's pending entry; a restored ping is re-fetched and checked again (B 2.7.3), so it gets its trust back or is dropped. The `ping` record's `trust` is that value; it stays `-` only for a ping without a reference.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| n3d-D13 | `reply` decides from the ledger alone where it can, so a reply to a delivered item queues while the homeserver is down: no `--verb` is a reply in main to the item's sender; `--verb` applies only to a `ping` item with no thread (an answer is always in its ping's thread, so it takes none: 10 `re`). The ledger cannot tell a `done` from a `review`; an `ack` to an unthreaded `done` is harmless (it is no answer to any ping the `done`'s sender holds). A carried item and an unknown id need one fetch. Items in the questions room are n3f's.                                                                                                                                                                                                                                                                                                    |
| n3d-D14 | `reply --to-coordinator` adds the team's coordinator to the reply's `to` (nothing when it is already there or is this member); with `--verb` it is 64 `usage`, since an answer goes to its ping's sender.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| n3d-D15 | `say` and a plain `reply` read their text on stdin; `ping` and `reply --verb` read none (a ping's body is generated, A 7.2), so a verb run from a tool with an inherited stdin never blocks on it. A terminal on stdin is 64 `usage`. `ping` with `ack` or `nack` is 64 pointing at `reply --verb` (their reply relation is required); `done` and `blocked` may be sent by `ping` with no relation (it is optional for them).                                                                                                                                                                                                                                                                                                                                                                                                                               |
| n3d-D16 | Two failpoints: `link-after-send-before-record` (PT1's "after a send's PUT before its answer is recorded") and `link-after-charge-before-send` (after the charge write, before the PUT), so the restart re-send is proven at both ends of the PUT.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| n3d-D17 | `recv`'s `fact answered` records and the socket delivery's `answered:` line (A 8.4, 13.1) are n3e's, with the delivery's trailing lines; n3d records every answer so they can be rendered. Under `--plain`, an agent reads its pings' answers from `status`'s `outbox` records until then.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| n3d-D18 | The gate's times (`gate.at_ms`, `recent[].at_ms`) are wall milliseconds, since the store outlives a reboot that resets `CLOCK_BOOTTIME`. A wall step back refills nothing until the clock passes `at_ms` again; a step forward refills at most to the burst.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| n3d-D19 | A send op answers `queued` at once when its team is down or older sends wait before it, rather than holding the agent's tool call for the 30 s; otherwise it waits up to its deadline for the post.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| n3d-D20 | Isolation: one sender goroutine per team; the state goroutine never waits on a sender; the forge checker is one per link, its caches shared and safe for concurrent use, each check bounded by its own deadlines, so a team's slow homeserver or a slow forge holds only that team's work.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| n3d-D21 | `status` gains a `queued` record kind (`team`, `room`, `send` text or ping, `verb`, `to` short names, `since_ms`, `tried`), one per `sending` entry; `sent` gains `queued` and its `id` is null exactly when queued. Proposed B 2.2.2 text: the two records added to the list, and "`queued` is a send the link holds until its homeserver answers".                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| n3d-D22 | The forge caches are bounded: 1024 entries each, oldest evicted; content at a SHA is kept for the link's life, not A's 30 days, since the cache is in memory.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| n3d-D23 | A removal drops the team's queued sends with its store (one log line counting them): the member has left the team, so posting afterwards would speak for a member that is gone. A `seat set-aside` sets them aside with the store, unsent.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |

## 12. The common rules file for n3d part builders

The lead puts this text in `untracked/scratch/n3d-part-common.txt` and hands it to each builder
with the part's name.

```text
COMMON RULES FOR n3d PART BUILDERS (Plan 00209 N3, slice n3d: the sends; product name teamchat)

Plan folder: CLAUDE/Plan/00209-agent-bus-v2-in-go/. Your report goes in its subagent-reports/.

Your brief is your part's section of subagent-reports/261022-n3d-handoff.md in your worktree. Read
section 0 (the order), section 1 (the owner's offline rule), section 2 (what you build on: 2c lists
the built APIs no report quotes, which you read with go doc first), section 3 (the shared facts),
your own part's section, and section 11 (the decisions). The brief digests the design for you. Open
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
- TDD (the test file before the source; a hook enforces it). Each test is named for its expectation
  id in your section (SD3-6 and so on). Run go test -race -cover on your packages, and golangci-lint
  run from src-go with no tags, with --build-tags tuwunel, and with --build-tags busfailpoints. Hold
  90% coverage per package. No //nolint. No time.Now in tests; no test sleeps. Use named results
  instead of `x, _ :=`.
- `os/exec` only through execx; a run-time command name needs its dynamicNames row in
  src-go/qa/rules/table.go. Never run `gh auth token` yourself (a hook denies it); the tests use
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
- At the end, write subagent-reports/261022-n3d-<part>.md: what was built, the exported API as
  built (exact signatures, for the later parts), the go doc facts you relied on from section 2c,
  coverage, and decisions. Final message under 700 characters.
```

## 13. Reports

Each part writes `subagent-reports/261022-n3d-d<N>.md` (and `-review`, `-review-2`, ...); W writes
`261022-n3d-wire.md`. A builder that stops at its context budget writes
`261022-n3d-d<N>-handoff.md` first. The lead puts each part's PLAN.md Task 3.2 progress line in at
its merge.
