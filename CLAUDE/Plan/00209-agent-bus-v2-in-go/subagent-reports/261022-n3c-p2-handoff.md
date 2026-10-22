# n3c P2 handoff (shared types): the state at the context budget

Worktree `untracked/worktrees/bus-v2-n3c-p2`, branch `feat/bus-v2-n3c-p2` (from a5fca1a83). Every
commit below passed `qa-changed.bash` and both `golangci-lint` runs of its packages. Each was
tested with `go test -race -cover`.

## Done (committed)

| Commit    | What                                                                                                                                                                                                                         |
| --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| e42412c86 | `clock` (99.0%)                                                                                                                                                                                                              |
| cf5772a20 | `failpoint` (94.7% with `-tags busfailpoints`; no statements without it), plus the `runSelf` dynamicNames row, plus the `teamchat qa-go` row in `scripts/go-binary-suites.tsv` (the same line as the n3a guest-check branch) |
| 26447cdde | additions to built packages: state, lock, homefs, bundle, paths, cli, exits, `cmd/teamchat`                                                                                                                                  |
| b61e1a75f | `wire` link-socket half, `request.go` (97.8%); `.golangci.yml` gives wire snake_case json tags                                                                                                                               |

## Exported API as built, and where it differs from the brief

- **clock**: `Clock` with `Boot`, `Mono`, `WallMS`, `Sleep`, `AfterFunc`, `Hold`, as the brief gives
  it; `System()`; `NewFake`, `Advance`, `Suspend`, `StepWall`, `Waiters`.
  - **Added:** `func (f *Fake) BlockUntil(ctx context.Context, n int) error` waits until exactly n
    waiters are armed. Tests use it instead of spinning on `Waiters`.
  - **Decision:** the fake's waiters are due on **Mono**, as the runtime's timers are. `Suspend`
    fires nothing.
  - `Advance` fires each due waiter on the caller's goroutine, in due order, with the clock at
    that waiter's due time.
  - The test asserts `lock.Clock` and `matrix.Clock`. For `matrix.Timers` it asserts a local
    interface with the same method set, because P1 has not merged yet. W replaces it with
    `matrix.Timers`, and adds the "a matrix.Client deadline through AfterFunc" test.
- **failpoint**: `type Name string` and the 9 constants of the brief's list (`KeeperLinkWriting` …
  `LinkAfterJudgeBeforePersist`). `Hit(Name)`.
  - With the tag, an unknown name in `TEAMCHAT_FAILPOINTS` **panics at the first Hit**, because
    a misspelt point would never fire.
  - The gate test builds `./cmd/teamchat` with `-tags teamchatrelease` (Name `go`, a constant, so
    no dynamicNames row is needed) and scans the binary for each name in `names.go`.
- **state**: `func (w *StoreWriter) Remove() error`.
  - It refuses with `lock.ErrReleased`, the same error `Write` gives, **not** `ErrNoLock`.
  - A store already gone is `fs.ErrNotExist`.
- **lock**: `func Probe(root *homefs.Root, full string) (bool, error)`.
  - **Added** for it: `func (r *homefs.Root) OpenExistingLock(full string) (*os.File, error)`,
    which opens read-only and creates nothing.
  - `homefs.Root.Unlink` now fsyncs its directory.
- **bundle**: `func ReadPairAt(root *homefs.Root, dir string, team paths.TeamID) (Pair, error)`.
  - It is built on the **added** `func paths.SeatAt(dir string) (paths.Seat, error)`.
  - A dir that is not absolute and clean is 78 bundle.
- **paths**: `func (l Layout) WithState(dir string) (Layout, error)`, registered in
  `sampleNames`. In `zero_test.go`'s `methodArgs`, a string parameter is a base directory, not
  an id. `SeatAt` is registered too.
- **cli**: `type RunFunc func(Env, Invocation) term.ExitCode`. `Env` gains `Getenv` and
  `Verbs map[string]RunFunc`.
  - `Run` tries cli's own verbs first (version, help), then `e.Verbs`, and only then reports
    the verb as unbuilt.
  - `cmd/teamchat/main.go` passes `os.Getenv` and an empty `verbs` map.
- **exits**: **added** `func (e *Error) Object() proto.Value`, the object `Write` prints, so a done
  line can carry it.
- **wire (request.go)**: `MaxRequest`, `Version`, the `Op*` constants, `Request`,
  `DecodeRequest`, `Done{Exit int; Error *exits.Error; Ack string}`, `Done.Encode() []byte`,
  `DecodeDone`, `Ack{Ack string}`, `DecodeAck`, `Deadline`, `WaitArgs`, `VerifyArgs`, `TeamsArgs`,
  as the brief gives them.
  - **Added:**
    - `OpQuestions` (LN2's 60 s row names it);
    - `func (r Request) Encode() ([]byte, error)`;
    - `func DecodeArgs(raw json.RawMessage, out any) error` (strict; 10 schema);
    - `func NewAck() (Ack, error)`;
    - `func (a Ack) Encode() []byte`;
    - `var ErrMalformed`, returned for a bad done or ack line.
  - `HistoryArgs` has json tags `team`, `room`, `limit`.
  - `Done.Encode` takes a failure's exit from its error object.
  - `DecodeDone` requires an error object exactly when the exit is non-zero, and allows `ack`
    only on exit 0 (8 lowercase hex).
  - An unknown op is 10 `verb`. A bad kit (`paths.ParseDigest`), a non-positive deadline, or args
    that are not an object are 10 `schema`.
  - `Deadline` of an unknown op is 0.
  - wire imports `exits`, `paths` and `proto`. It does not need `records` so far.

## Left

1. **wire, `control.go` + `control_test.go`** (n3c-D3). The design was settled before the stop:
   - **Messages.** `MaxMessage = 8 << 10`. Each message on the wire is `{"<kind>":{body}}`, a single
     member. Decoding goes through the same `decodeStrict` (in request.go), then per-kind checks.

   - **The kind constants:** `KindLaunch`, `KindAdd`, `KindRemove`, `KindOwed`, `KindPID`,
     `KindClaude`, `KindToken`, `KindDesired`, `KindEnd`, `KindReady`, `KindState`, `KindRemoved`,
     `KindLeft`, `KindRefused`.

   - **`Msg{Kind string; Launch *Launch; Add *Add; …}`** has one pointer per kind.

     - `PID` and `Claude` are `*state.Process`, and `Desired` is `*state.ReportDesired`. This
       reuse means wire imports `state`, a deviation from the brief's import graph, but no cycle.
     - Send refuses a Msg whose set pointer does not match its Kind.

   - **The body types and their checks:**

     | Body                                                                                             | Checks                                                                                                                |
     | ------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------- |
     | `Launch{Run paths.RunID; Kind; Launch; Inbound; Generation int64; ClaudeConfigDir *string; Cwd}` | the state sets; the two paths absolute and clean                                                                      |
     | `Add{Team; Seat string; RemovedUserIDs []string}`                                                | `proto.ValidSeat`; each user id `proto.AgentHandle(id, paths.ServerName(team))` and at most 128 bytes; a non-nil list |
     | `Remove{Team; Farewell *string; Txn}`                                                            | farewell is `state.Farewell*` or null; txn `^tc[0-9a-f]{32}$`                                                         |
     | `Owed{Team; Txn}`                                                                                | txn as above                                                                                                          |
     | `Token{Token string}`                                                                            | 1-1024 bytes of `\x21-\x7e`; its `String` and `GoString` are redacted                                                 |
     | `End{How string; Status int}`                                                                    | how is launch-failed, stop, restart, exit or killed; status 0-255                                                     |
     | `State{Team; Link; LastSyncOKMS *int64; Pending int64; Delivery; Store; SessionStarted bool}`    | the `state.Link*`, `Delivery*` and `Store*` sets                                                                      |
     | `Removed{Team; UserID}`, `Left{Team}`, `Ready{}`, `Refused{Team; HolderRun paths.RunID}`         |                                                                                                                       |
     | `Desired`                                                                                        | report.go's rule: code nil exactly when applied, and code in json, schema, version, seat or missing                   |

   - **`Msg.String()`** redacts the token. It is a String method rather than `Format`, because
     `go vet`'s stdmethods check reserves the name `Format` for fmt's signature. Record this.

   - **The socket.**

     - `Pair()` is `unix.Socketpair(AF_UNIX, SOCK_SEQPACKET|SOCK_CLOEXEC)` as two `os.File`s.
     - `NewConn(f)` checks `SO_TYPE == SOCK_SEQPACKET` and `SO_DOMAIN == AF_UNIX` through
       `f.SyscallConn().Control`. It then calls `net.FileConn(f)` to get a pollable
       `*net.UnixConn`. It takes ownership of f and closes it on every path.
     - `Send` uses `WriteMsgUnix(data, unix.UnixRights(fd), nil)`. It sends exactly one fd on an
       add and none on any other kind, and refuses before writing otherwise.
     - `Recv` uses `ReadMsgUnix` with a `MaxMessage+1` buffer and room in oob for 4 fds. It
       returns `io.EOF` on n=0 with no oob. `MSG_TRUNC` and `MSG_CTRUNC` are refused, and so is
       the wrong fd count; every received fd is closed on a refusal. Go sets
       `MSG_CMSG_CLOEXEC` on Linux.
     - `PeerPID` is `GetsockoptUcred`. Add `Close`.
     - Use `ErrProtocol` (wrapping a rule) for a refused message.

   - **Tests the brief lists:**

     - every kind round-trips;
     - an add without a fd, and a token with one, are refused;
     - a String of a token message shows no token;
     - `NewConn` refuses a `SOCK_STREAM` socketpair and a regular file;
     - `PeerPID == os.Getpid()` for an in-process pair;
     - the peer's close is EOF.
2. **team package** (`internal/teamchat/member/team`): the types and interfaces of the brief, as
   given (`Batch`, `Item`, `Counts`, `View`, `Health`, `ErrRemoved`, `Engine`, `Team`, `Notices`,
   `Ends`, `End`, `EndTeam`).
   - **Planned addition:** View's methods `Seen`, `Event` and `OwnPing`, so View is a
     `proto.Lookup`, with a compile-time assertion and a test (the package then has statements to
     cover).
   - `team` imports `notice`.
3. **The final part report**, `subagent-reports/261022-n3c-p2.md`: copy the API section above and
   add control and team.

## The brief review changes P2's API (lead, after this handoff was written)

These come from section 8a of the brief review on `feat/bus-v2-n3c` at bb34f0e69. Read it with
`git show bb34f0e69:CLAUDE/Plan/00209-agent-bus-v2-in-go/subagent-reports/261022-n3c-handoff.md`
before building. Each change applies to the plan above and overrides it. Record each change in
the report as built.

**Already built, so it needs changing:**

- **7.** `WaitArgs` and `VerifyArgs` gain `Team *paths.TeamID` (`json:"team"`). Every Args type
  carries json tags. Update `request.go` and its args test.
- **9.** clock asserts only `lock.Clock` and `matrix.Clock`. Timers, Hold and the deadline test
  go to P5.
  - Remove `AfterFunc` and `Hold` from `Clock` and `Fake`, along with their tests and the local
    Timers interface assertion.
  - Keep `BlockUntil` and `Waiters`, which serve Sleep.
  - Before removing, check 8a: it may keep the methods and drop only the assertions.
- **12.** `ReadPairAt`'s team becomes `*paths.TeamID`, where nil means seat.json's own team.
  Update `bundle.go` and its test. When nil, the error templates name no team.

**Not built yet, so build it this way:**

- **1.** `Notices` gains `Token() string`.
- **2.** `Engine.Load(t paths.TeamID, userID string)`.
- **3.** Add `Engine.BeginRun(ctx context.Context, t paths.TeamID)`. 8a gives its result type.
- **4.** `team.Team` gains `FlushFloor(ctx context.Context) error`.
- **5.** `session_started` leaves the `state` message. It becomes a session-level `started {}`
  message (`KindStarted`), which a session with no team can send (T6).
- **6.** The `state` message gains `handle` and drops `link`.
- **8.** The control message cap is **64 KiB**, not 8 KiB.
  - Refuse `MSG_TRUNC` and `MSG_CTRUNC`, and use `MSG_CMSG_CLOEXEC`.
  - Close the fds of a refused message.
  - The control design above already planned these, apart from the cap.
- **10.** `Health` gains `LastSyncOKMS`. 8a gives its type.
- **11.** `Batch.Floors` is keyed by room purpose, not room id. 8a gives the key type.
