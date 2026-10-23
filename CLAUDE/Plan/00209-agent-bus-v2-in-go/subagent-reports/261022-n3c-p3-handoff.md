# n3c P3 handoff: the keeper and `run` (Plan 00209 N3, slice n3c)

The first P3 builder stopped at its context budget. This file says what is built and
committed, what is left, and the design worked out for what is left, so the next builder
starts writing code at once. Branch `feat/bus-v2-n3c-p3`, worktree
`/workspace/untracked/worktrees/bus-v2-n3c-p3`.

## Built and committed

| Commit    | What                                                                                        |
| --------- | ------------------------------------------------------------------------------------------- |
| cdb10e3e9 | `execx.Start` (gap 7), `reexec.Command`, `state.Report.Why` (n3c-D21), the CCY version bump |
| (next)    | `paths.KitAt`, `Layout.WithDesired`, `Layout.WithKitRoot` (committed with this handoff)     |

The API as built:

```go
// execx (src-go/internal/execx/start.go)
type ExitStatus struct{ Code int; Signal syscall.Signal } // Code is 128+n for signal n
type Started interface { Pid() int; Wait() (ExitStatus, error); Signal(sig syscall.Signal) error }
type Starter interface { Start(c Cmd) (Started, error) }
func (OS) Start(c Cmd) (Started, error)   // nil streams are /dev/null; NewSession is setsid
func (f *Fake) OnStart(prefix []string, fn func(Cmd) (Started, error))
func (f *Fake) Start(c Cmd) (Started, error)
func (f *Fake) Starts() []Cmd

// reexec
func Command(tb testing.TB, name string, args ...string) execx.Cmd // free-running helper; no Ready

// member/state
const WhyLaunchFailed = "launch-failed"
// Report gains `Why *string json:"why"` after Link: crash-loop or config exactly when link is
// failed; launch-failed with any link; else null.

// paths
func KitAt(dir string) (Kit, error)                          // --kit DIR
func (l Layout) WithDesired(dir string) (Layout, error)      // --desired DIR
func (l Layout) WithKitRoot(dir string) (Layout, error)      // kits under dir, current = dir/current
```

Deviations so far:

- **`Started` has no `PidFD()`.** Go's `os.Process` already holds a pidfd for a child and uses it
  for `Signal` and `Wait`, so a reused pid is never signalled; the keeper needs no fd of its own
  for its children (YAGNI). `Signal` on a child already gone is nil.
- **The CCY version is bumped to 3.99.14.12** because `execx` is a package the ccy front builds
  from (the commit gate demands it). F44 already has its own 3.99.14.12 (Plan 00211): renumber at
  the merge.
- **`qa/rules/table.go`**: the reexec `dynamicNames` row's function is now `command` (was
  `start`). The keeper will need rows of its own for the kit binary's `--version`, the link's
  `<kit>/bin/teamchat` and CMD's `Cmd[0]` (all run-time names).
- **`paths` additions** are three small exported names, each in `separation_test.go`'s sample
  names. `WithKitRoot` exists so a test can place a kit root with its `current` link; the system
  kit root is not writable by a test.

## n3c-D24 (agreed by the lead with P4)

`wire.Launch` gains `Seats int json:"seats"` (refused when negative): the number of `add`
messages that follow the launch. The link sends `ready` only after it has handled that many,
a failed add included, so CMD starts with every team's store loaded. P4 adds the same field
with the same name and type, so the two merge identically. The keeper sets `Seats` to the
number of adds it is about to send, sends exactly that many, then waits for `ready` within its
30 s bound on the clock seam. An end-only start counts its adds the same way. Record this in
the report.

## Second builder's state (read this first)

The keeper package is WRITTEN and compiles (`go vet` clean), committed as a WIP commit that
is NOT yet green. Files in `src-go/internal/teamchat/member/keeper/`:

- `keeper.go` doc, seams (`Proc`, `Pidfd`, `Pidfds`, `WatchEvent{Name; Op uint32}`, `Watcher`),
  `Config` exactly as below, constants, messages, `Run`, `Main`, `production`, `openRunRoot`.
- `prepare.go` the `keeper` struct, steps 1-6 (`resolveKit`, `currentKit`, `kitVersion`,
  `readLaunchDesired`, `trustedRead`, `sweep`, `makeRun`, `writeSessionPID`, `claudeArgv`,
  `resume`, `projectDir`, `takeLaunchSeats`, `takeSeat`, `close`).
- `loop.go` timers (`after`/`cancel`, every timer tracked so `finish` disarms all), the loop,
  `startLink`/`greet` (launch with Seats = adds actually dup'd, owed, adds, pid, claude, token;
  end-only: launch, adds, end), supervision (`crashed`: 10 min reset, 1 s \<< (n-1), 6th =
  crash-loop; 78 = config; deliberate kill restarts uncounted), `startCmd`, claude pidfd,
  orphan 3 s/5 s, stop 5 s/7 s, `how`, `end`, `launchFailed`.
- `seats.go` seat/removal types, keeper.json and report.json writers, RC1 (`onWatch`,
  `reread`, `reconcile`, `add`, `refuse`, `remove`, `settleRemovals`, `removed`, `drop`).
- `osproc.go` production `procStat`, `osPidfds` (pidfd + poller wait), `inotify` watcher.
- Tests: `harness_test.go` (the draft, fixed; helpers `holdFile`, `holdSeat`, `startPlain`,
  `session.finish`, `logOf`, `link()`/`cmd()` that fail fast when Run ended), `keeper_test.go`,
  `prepare_test.go`, `loop_test.go`, `seats_test.go`, `osproc_test.go` (TestMain = reexec.Main).

Known failures to fix next (from the last `go test` run):

1. Claude-mode tests that end CMD with `c.code(0)` hit launch-failed (78) because the fake link
   never sent `started`: make `world.running` (and TestLaunchHoldsSeatsAndRunsCmd) send
   `wire.Msg{Kind: wire.KindStarted, Started: &wire.Started{}}` after the pid for `--claude`.
2. TestLaunchHoldsSeatsAndRunsCmd: keeper.json missing after ready - check `writeKeeper`'s
   error in keeper.log (likely an Encode refusal of a field value).
3. TestRunRootWithoutParent "cannot be made"/"parent cannot hold it": the test must MkdirAll
   the run root's parent before planting a file at the run root.
4. TestLeftoverSweep: `runDir()` assumes one entry; make it pick the entry not planted by
   the test.

Lint (golangci-lint on the package, 31 issues): containedctx (the keeper struct holds ctx:
thread ctx as a parameter, or use one the loop owns), cyclop on the long test, funcorder
(unexported methods after exported ones in pidfd, inotify, fakeLink, procTable, fakePidfd),
gosec G115 (uid and fd conversions: compare as int64/uint64 instead), G304 (openAppend),
govet shadow (keeper.go openRunRoot, prepare.go makeRun), gocritic appendAssign
(claudeArgv), staticcheck QF1001/QF1008, exhaustive (loop.go linkExited switch), prealloc,
unparam and unused (helpers that later tests will use: `report`, `desiredFarewell`, `quiet`,
`fakeLink.state`, `procTable.gone`, `fakePidfds.get`, `cmd`, `log`; field `keepRun` to delete).
The edit hook prints the whole lint list after every edit, so fix lint in a few large edits.

Still to write: tests for KP1 (pidfd stop, 3 s/5 s, 5 s/7 s, INT ignored, one forward), KP2,
KP3 (each kind, 5 s KILL, locks closed only after the link's exit, end-only start), RC1 rows
(refused with code, missing, overflow, removal with farewell released/owed/left/30 s,
remove timeout kill + store delete, busy hot add -> refused), RN3, LN8, the B11/B14/B15 source
scans (B14: cli pulls in guest -> matrix -> net/http, so scan the keeper's own imports and
every module package it imports except cli, whose types alone it uses, and assert it calls
no cli function), the busfailpoints real-process test, Main's tests, the qa/rules
`dynamicNames` rows the analyzer asks for, coverage >= 90% with -race, golangci with and
without --build-tags tuwunel, qa-changed, and the report `261022-n3c-p3.md`.

## Left to build (first builder's list)

The whole `internal/teamchat/member/keeper` package, its tests, its failpoint tests, the
B14/B11/B15 source scans, coverage at 90%, and the report `261022-n3c-p3.md`.

A draft of the test harness is in `untracked/scratch/p3/harness_test.go.draft` in this worktree
(not in git, never compiled). Copy it to `keeper/harness_test.go` once `Config` exists; it
fakes every seam: in-process link and CMD children (the link's socket end dup'd from
`ExtraFiles[0]`), a `/proc` table, pidfds, a watcher, the kit root with `current`, and helpers
for desired.json, reports, lock records and `lock.Probe`.

## The design for the keeper

### Files

- `keeper.go`: the package doc, `Config`, the seams, `Run`, `Main`, steps 1-5 (`prepare`).
- `loop.go`: steps 6-8, the loop, the link's supervision, the stop, the end, RC1.
- `osproc.go`: production `Proc` (`/proc/<pid>/stat` field 22), `Pidfds` (`pidfd_open` with
  `PIDFD_NONBLOCK`, `os.NewFile`, a `SyscallConn().Read` that waits for readability, so `Close`
  unblocks it), `Watcher` (`inotify_init1(IN_NONBLOCK|IN_CLOEXEC)` through `os.NewFile`, so a
  read waits in the netpoller and `Close` ends it). Each needs its `_test.go` first (TDD hook).

### Config as it will be (departs from the brief, record it in the report)

```go
type Config struct {
	Launch        cli.Launch
	Layout        paths.Layout        // WithState, WithDesired applied by Main
	RuntimeParent string              // $XDG_RUNTIME_DIR, opened homefs Private; "" for ccy's /tmp
	Digest        string              // this binary's source digest (cli.Env.Digest)
	Args          []string            // run's arguments after the verb, for the re-exec
	Env           func(string) string
	Cwd           string              // run's cwd: wire.Launch.Cwd and the transcript's project dir
	Stdin         io.Reader           // CMD's inherited stdin
	Clock         clock.Clock
	Locks         lock.Clock          // the 3 x 50 ms lock tries; lock.SystemClock() in tests too
	Runner        execx.Runner        // the kit's --version
	Execer        execx.Execer        // the re-exec
	Starter       execx.Starter       // the link and CMD
	LinkCmd       func(run paths.Run, kit paths.Kit) execx.Cmd // prod: <kit>/bin/teamchat link --run <dir>
	Proc          Proc                // interface { StartTicks(pid int) (int64, error) } (no PPid: unused)
	Pidfds        Pidfds
	Signals       <-chan os.Signal
	Watcher       Watcher
}
func Run(ctx context.Context, cfg Config, stdout, stderr io.Writer) term.ExitCode
func Main(e cli.Env, inv cli.Invocation) term.ExitCode
```

`Main`: kind ccy is `paths.CCYLayout()` with parent `""`; else `$XDG_RUNTIME_DIR` unset is 78
`runtime`, and the layout is `HomeLayout(HOME, XDG)`. Then `WithState`, `WithDesired`.
`signal.Notify` on TERM, HUP, INT, QUIT. `debug.SetGCPercent(-1)` and `SetMemoryLimit(16<<20)`
just before `Run` (never in `Run`: tests run it in process).

### Steps 1-5 (`prepare`; failures go to stderr as an `exits.Error`, and to `keeper.log` once open)

1. Run root: with a parent, `homefs.Open(parent, Private)` (else 78 `runtime`), `MkdirAll` the
   run root, `Sub` it Private. ccy: `os.MkdirAll(runRoot, 0700)` then `homefs.Open` Private.
2. Kit: `--kit` is `paths.KitAt` and never reads `current`; the ccy layout's one kit is
   `CurrentKit()` when it equals `KitRoot()`; else `os.Readlink(current)`, which must name a
   digest directory directly under the kit root. Run `<kit>/bin/teamchat --version` through
   `Runner`: one line, a digest. A kit named by a digest must print it (78 `runtime`). Printed
   different from `cfg.Digest`: with `--kit` it is 78 `runtime` (no loop); without, `Execer.Exec`
   `<kit>/bin/teamchat run --kit <kit> <Args...>`. A nil from a fake Exec returns 0.
3. Desired (read before step 4, since session.pid holds inbound): with `--desired`, `Watch` the
   directory first, then read `desired.json` by a trusted read (`openat2` with
   `RESOLVE_NO_SYMLINKS`, `O_NONBLOCK`, `fstat` regular, owner uid or root, no group or other
   write, a cap) and `state.DecodeDesired`; missing or undecodable is 78 `membership`. A ccy or
   lxc launch without `--desired` is 78 `membership` too (keeper.json's `session` must be set for
   them). Inbound is the file's, else `refuse`.
4. Leftover sweep (T8): each run-id entry of the run root; `lock.TakeKeeper` with `Locks` (busy:
   leave it); `session.pid` decoded; remove (`RemoveTree`) when the keeper and the session (if any)
   are gone by `Proc.StartTicks` (no pid, or other ticks), or when there is no `session.pid`
   (the lock was free, so its keeper is dead and never started CMD); an undecodable one is left
   and logged.
5. Run: 16 hex from `crypto/rand`, `ParseRunID`, `Mkdir`, `TakeKeeper`, `keeper.log` opened
   `O_CREAT|O_EXCL|O_APPEND|O_NOFOLLOW|O_CLOEXEC` 0600, `session.pid` with `session` null.
6. Claude: the kit's `settings.json` by the trusted read, a JSON object without
   `crossSessionInbound` (else 78 `runtime`), the key added, `runRoot.Create` 0600. Argv: CMD,
   then `--resume <id>` when asked and the transcript `<CLAUDE_CONFIG_DIR or HOME/.claude>/ projects/<cwd with every non-alphanumeric byte as '-'>/<id>.jsonl` is a regular file by
   `Fstatat(AT_SYMLINK_NOFOLLOW)` (else say why in keeper.log), then `--plugin-dir <kit plugin> --settings <run settings>`.
7. Seats: desired seats, or the `--seat` refs; `KeeperLock.TakeSeat` on the state root
   (`homefs.Open(State, Private)`, opened lazily). Busy is 75 `seat-held` naming the seat; any
   other failure (no seat directory, no state root) is logged and the seat is not held, the launch
   goes on (the owner rule). `state.WriteLockRecord(l, {Run, StartedMS: WallMS})`.

### Steps 6-8 and the loop (one goroutine; timers post closures)

- `post(f)`: `Clock.AfterFunc(d, func(){ go send(f) })`, `send` selects on `events` and `done`.
  Each child has a `Wait` goroutine and the link a `Recv` goroutine; both send closures, so a
  source's order holds. The loop selects on `events`, `Signals`, the watch channel, the Claude
  pidfd's `Exited()` (nil until known) and `ctx.Done()` (a stop by SIGTERM). Stop every timer
  that becomes moot, so tests can count waiters with `BlockUntil`.
- Link start: `wire.Pair`, `NewConn` on the keeper's end, `LinkCmd` plus `NewSession`, stdout
  and stderr the one `link.log` file (opened like keeper.log at the first start, kept open),
  `ExtraFiles` the link's end (closed after Start), env `PATH=<kit>/bin:$PATH` and
  `TEAMCHAT_KIT=<kit>`. Send `launch` (Run, Kind, Launch, Inbound, Generation,
  ClaudeConfigDir when absolute and clean, Cwd), each `owed`, each held seat's `add` with
  `Dup()` (closed after Send), `pid`, `claude`, `token` when known. The end-only start sends
  `launch`, the adds and `end`.
- Ready wait 30 s; the link's exit or the timeout kills it and is 78 `runtime` naming
  `link.log`, with `<v2-run>` KEPT so link.log can be read (the next sweep removes it); seats
  and keeper.lock released; CMD never started. A stop signal while launching kills the link and
  exits 128+n, removing the run.
- CMD: `Starter.Start` with `Stdin`, the run's stdout and stderr, env `TEAMCHAT_RUNTIME`, PATH
  and KIT. Then session.pid rewritten with `session` (`Proc.StartTicks`), `pid` sent, reconcile.
  Nothing is written to stdout or stderr from here on.
- Launch failed (claude only; CMD's start error, or its exit before the link's session-level
  `started`): every held seat's lock record rewritten with `LaunchFailedMS`, keeper.log has the
  status, each report.json `why` `launch-failed`, `end` `launch-failed`, exit 78. CMD's first
  stderr line is NOT captured (a deviation): CMD inherits the terminal's stderr (B 2.4.1 step 7),
  and a pipe in between would change what Claude Code sees. A plain CMD that cannot start ends as
  `exit` 127.
- Stop (TERM, HUP, once): to Claude Code by its pidfd when known, else to CMD; 5 s: TERM to
  whichever still runs; 7 s: KILL. INT and QUIT are ignored. Without a stop: CMD ends with
  Claude Code alive, 3 s then TERM it, 5 s KILL; Claude Code ends first, the same to CMD.
  `claude` with CMD's own pid is CMD. A second `claude` is logged and ignored. A pidfd whose pid's
  start ticks differ from the message's is closed and not used.
- End: `launch-failed`; else `stop`; else `restart` for kind ccy with CMD's code 75 and no
  signal; else `killed` for a signal the keeper did not send CMD; else `exit`. Status is CMD's
  code. `failpoint.Hit(KeeperBeforeEnd)`, then `end` to a live link; a restarting link (timer
  cancelled) is started end-only; a failed link means no wait. 5 s, then KILL the link. After its
  exit: close every seat lock, `RemoveTree(<v2-run>)`, release keeper.lock, close the files,
  return the status (78 for launch-failed).
- Supervision: an exit in the running phase. A deliberate kill is not counted and restarts at
  once. Exit 78 is `failed` `config`. A link that ran 10 min resets the count first; then count+1;
  the 6th is `failed` `crash-loop`; else restart after `1s << (count-1)`, keeper.json `restarting`
  with `next_ms` = Boot+delay in ms and `restarts` = the count. Failed: keeper.json and each
  report.json written first, then every seat lock closed (T23), desired changes no longer applied.
  A Start error counts as an exit 1. Any link exit also settles every seat in `removing`: the
  keeper deletes its store under the lock and treats it as removed (its user id is then not known,
  so a re-add is SY1 (b); record it).
- Link messages: `ready` (running; keeper.json; reports), `started` (the first SessionStart),
  `state` (`failpoint.Hit(KeeperLinkWriting)`: the link has a store; the seat's report.json,
  first written now that the handle is known), `removed`, `left`, `token` (memory only, never
  logged; `Msg.String` redacts it), `claude`; anything else is logged. `ErrProtocol` from Recv is
  logged and the reading goes on; EOF ends the reader.
- RC1: any watch event naming `desired.json`, or an overflow, re-reads it. Missing (or refused
  by the trusted read) is code `missing`; undecodable is the `*state.InvalidError`'s Code. A
  refusal keeps the last good membership, sets keeper.json `desired` `refused`, each report's
  `desired` {gen, refused, code}, and sends `desired`. A good file applies: `desired` {gen,
  applied, null} and reconcile. Reconcile (running phase, link not failed): a held seat not
  wanted, or wanted with another seat name, is removed; a wanted team with no entry is added.
  - Add: `TakeSeat`; busy: the holder's run from the lock file (`root.Read(seat.Lock(), state.MaxLockRecord)`, `DecodeLockRecord`) to keeper.log and `refused{team, holder_run}`
    to the link, and no report.json for that seat; other errors logged. Held: lock record,
    `add` with the team's removed user ids (n3c-D13) when a link is up.
  - Remove: `txn` `tc` + 32 hex; `farewell` is the reason from the file's `farewell` for that
    team and seat; `remove` sent and a 10 s timer. `removed` in time: remember the user id; if
    the farewell is `released` the notice is owed (keep the lock until `left` or 30 s; a new link
    is given `owed`), else close the lock at once (deviation from the brief's "until left or
    30 s": an `owed` given to a new link with no notice owed would post one). No answer in 10 s:
    KILL the link (deliberate), and on its exit `failpoint.Hit(KeeperBeforeStoreDelete)`,
    `state.NewStoreWriter(stateRoot, lock).Remove()` (ErrNotExist is fine), then the same as
    answered, and the link restarted at once, uncounted. With no link up (restarting), the
    keeper deletes the store itself at once.
  - `needs_relaunch` is a `--claude` launch whose inbound is `refuse`.
- Files: keeper.json on each change of link, why, restarts, next_ms, desired; report.json
  (`StartedMS` the seat's lock-record time) for each held seat with a known handle.

### Tests (the harness draft supports all of these through fakes)

RN1 rows (parent mode 0755; `--kit` with `current` pointing elsewhere; the re-exec argv;
mismatch with `--kit`; T8's four cases; B47 on a pipe through real processes), RN2 (held seat
75 with no link and no CMD; never ready is 78 naming link.log, no CMD), RN3 (launch-failed and
`launch_failed_ms`), KP1 (T21: the pidfd gets the stop; CMD first then 3 s/5 s; Claude first;
5 s and 7 s; INT ignored; one forward), KP2 (1, 2, 4, 8, 16; 6th failed and the seat free by
`lock.Probe`; 78 at once; the 10 min reset), KP3 (each kind; 5 s KILL; locks closed only after
the link's exit; run removed; end-only start of a restarting link), RC1 (every row and the
overflow), LN8's keeper half, B15/B14/B11 source scans (go/parser over the package's own files
for B11 and B15; B14 through the import graph of the module's source, as
`paths/separation_test.go` reads files), failpoints under `busfailpoints` in a real-process test
(a reexec helper running `Run` with the fake link as another reexec helper).

## The owner rule, and how the keeper meets it

The keeper makes no network call and reads no token, so a homeserver that cannot be reached, a
team that is down or a peer that is offline never reaches it. Each of its waits is bounded (30 s
ready, 10 s remove, 30 s owed, 5 s end). A seat that cannot be taken for any reason but another
session holding it is logged and skipped at launch and in a hot add; only the launcher's own
mistake (a desired seat held by another session) fails the launch, as B 2.4.1 step 6 says.
