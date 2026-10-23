# n3c P6 handoff (Plan 00209 N3, slice n3c, part 6)

Worktree `untracked/worktrees/bus-v2-n3c-p6`, branch `feat/bus-v2-n3c-p6`. The builder stopped at
its context budget. Three commits are on the branch, and each passed qa-changed:
8e8337b31, 0d46f4385 and eaa7901e8.

## Done

- `member/state`: `ReasonLaunchFailed = "launch-failed"`. A `notice: none` end is a skipped
  `restart` or `launch-failed` with no txn (n3c-D20). Test: `TestRunEndedNone`.

- `notice`:

  - `func LatestOwn(ctx, c Client, n Notice) (string, bool, error)` returns the subject's
    latest own lifecycle event since `n.SinceMS`, within the page bound. `Disconnected` now
    uses it.
  - `func Roster(ctx, c Client, a proto.Anchors) (proto.Roster, error)` is `RoomState` plus
    `VerifyMain`. The ends call it when `Notices.Roster()` is false, so `notice`'s
    `stateEvents` is not copied. W may switch it to `proto.StateEvents`.
  - `func Addressees(r proto.Roster, self string) []string` is the old `addressees`,
    exported.
  - Coverage: 91.5%.

- `member/ends` (96.5%). API:

  ```go
  const restartSwitch = false
  const HowKeeperDied = "keeper-died"
  type Config struct { Engine team.Engine; Run paths.RunID; Clock clock.Clock; Log io.Writer; switchOverride *bool }
  func New(cfg Config) team.Ends
  ```

  W's adapter: `NewEnds = func(e team.Engine) team.Ends { return ends.New(ends.Config{Engine: e, Run: run, Clock: clk, Log: log}) }`.

  How each method behaves:

  - `End` follows B 2.11's table:
    - restart with the switch on: none/restart;
    - launch-failed: none/launch-failed;
    - a recorded SessionEnd reason: `left` with that reason;
    - `stop`: `left other`;
    - otherwise `disconnected`;
    - an unknown `how`: an error for each team.
  - `End` skips a team whose `Run.ID` is not `Config.Run`. The teams run in parallel.
  - A posted end is recorded attempted, with a `tc`+32 hex txn, then sent, then recorded sent.
  - Each own send goes to `RecordOwnSend`.
  - A probe seat posts nothing.
  - Failpoint `link-mid-end-sends` is hit after a send and before sent is recorded.
  - `Close`:
    - ended null: `notice.Disconnected` with `Run` the previous run's id and `SinceMS` its
      `started_ms`;
    - none or sent: nothing;
    - attempted: `LatestOwn` since `started_ms`, and a re-send by the recorded txn unless the
      latest is `left` or `disconnected` (n3c-D19).
  - `Back` posts `back` by txn `tc-back-<run>`, only when `LatestOwn` from 0 is
    `disconnected`.
  - `Leave` posts `left released` by the keeper's txn.

- **Decision: the ends set no presence.** The link already sets each team offline after
  `End` and before `Leave` (`link.go` `finish` and `leave`), so a second call would be a
  duplicate.

- `member/client` (91.0%). API:

  ```go
  func Main(verb string) cli.RunFunc
  ```

  Main serves recv, wait, inbox, status, verify and history. It reads `TEAMCHAT_RUNTIME`; an
  unset variable, or one that names no ccy or home run directory, is 69 `no-session`. It sends
  one `wire.Request`, with `kit` from `cli.Env.Digest` and `deadline_ms` from
  `clock.Boot() + wire.Deadline`.

  Defaults and bounds:

  - wait: `--timeout` defaults to 110 and runs 1..3600;
  - history: `--limit` defaults to 20 and runs 1..100, and the room defaults to `main`.
  - Anything out of range is 64 `usage`.

  Records are buffered and reach stdout only once the done arrives, so a close before done
  copies nothing. The client flushes, then acks; a failed stdout sends no ack and exits 1.

  LN3, when no link answers (`nolink.go`):

  - keeper held and `restarting`: `no-link`, "next try in N s", computed from `next_ms` on the
    boot clock;
  - held and `running`, or no `keeper.json`: `no-link` with no time;
  - `failed`, or the lock free: `link-failed`, with the B 2.4.2 recovery text for the kind.

  Only wait retries, at 1, 2, 4 and then 5 s, under its one deadline. A link that never
  answers is cut off at the deadline plus 5 s grace.

## Left to build

1. **`func SeatDisconnected(e cli.Env, inv cli.Invocation) term.ExitCode`** (EN5), in
   `member/client`.
   - `--bundle DIR --since MS --seed ID`.
   - `homefs.Open(filepath.Dir(dir), homefs.Owned)`, then `bundle.ReadPairAt(root, dir, nil)`.
     Its 78 error is passed through, and ErrTorn is 20.
   - `--seed` goes through `paths.ParseRunID`; a bad one is 64.
   - `matrix.NewMember{BaseURL, PlainHTTPHosts, Token: pair.Token.Reveal()}`.
   - `notice.Disconnected` with Server `pair.Bundle.Bundle.ServerName`, Team and MainRoom from
     the bundle, Handle the localpart of UserID, Run the seed, and SinceMS `--since`.
   - Exits: 0 for posted or found; 0 for a `Why`, with one stderr line (n3c-D17); 20
     `unreachable` for a transient error.
   - stderr is buffered and written once the notice is settled; EIO, EBADF and EPIPE are
     ignored.
   - Bound it on the clock, about 30 s.
   - Tests: fakehs (copy `newWorld` from `member/ends/world_test.go`), a bundle dir written
     with `seat` encoders, and an EPIPE stderr writer that still gives exit 0.
2. **`func Reach(e cli.Env, inv cli.Invocation) term.ExitCode`** (n3c-D16).
   - `--team T`. The layout is TEAMCHAT_RUNTIME's when it is set, else
     `HomeLayout($HOME, $XDG_RUNTIME_DIR)`.
   - `homefs.Open(layout.State(), Private)`, then read `layout.Seat(T).Bundle()` with a
     64 KiB cap, then `seat.DecodeMemberBundle`. Its team must be T, else 78 `bundle`.
   - `matrix.NewMember` with the placeholder token (as in `guest/check.go` `reachToken`), then
     `Versions` within 3 s on the clock.
   - Prints nothing. Exits 0, 20 `unreachable` or 78 `bundle`.
3. **The report** `subagent-reports/261022-n3c-p6.md`: the API above plus these two.
   - Coverage: notice 91.5, ends 96.5, client 91.0.
   - Decisions: no presence in the ends; `notice.Roster` exported; the `back` txn; buffered
     records on a close before done; the 5 s grace.

Wiring the verbs into `cmd/teamchat` and taking them off `cli`'s `unbuilt` list is W's job.
Lint is clean without tags. Under `--build-tags tuwunel` the package has no tagged files, so
golangci reports "no go files", the same as for any package without tagged files.
