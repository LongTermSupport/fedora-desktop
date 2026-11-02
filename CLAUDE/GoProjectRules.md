# src-go project rules (Defence Before Fix)

Defect classes the `ccy-teams` and `ccy-sessions` ports actually hit, each held by a
`go/analysis` analyzer in `src-go/qa/rules`. They run in the Go gate (`scripts/qa-go-qa.bash`:
go-qa-ci's `analyzers` tool, configured by `src-go/qa/go-qa.toml`) as a hard step: the driver
(`src-go/qa/driver`) is built once and used as `go vet -vettool` over both modules, beside
go-qa-ci's bundled analyzers. Every finding fails the gate. There is no inline suppression: the only excuse is a
row in a table in `src-go/qa/rules/table.go`, with its reason.

**A new defect class found in `src-go` gets a rule here before its fix.** Write the rule, prove
it red on a fixture reproducing the defect (`src-go/qa/rules/testdata/src`, run by
`analysistest`), then green on the real tree, fixing every real instance it finds. A rule is
also red-tested against the ways round it: a rule that holds only against the original
spelling of the defect is documentation, not a gate.

The rules live in their own module (`src-go/qa`, its own `go.mod`, depending on
`golang.org/x/tools`) so that the QA tooling's dependencies never enter the shipped binaries.
`src-go/go.mod` itself takes only the dependencies the binaries need, vendored, pinned and
allowlisted (Plan 00204 decision D2; [src-go/CLAUDE.md](../src-go/CLAUDE.md#dependencies)), so
the host build stays a plain, offline `go build`. go-qa checks the `qa` module too
(format, lint, vet, the bundled analyzers and these rules, dead code, vulnerabilities, test,
coverage). This file is under `CLAUDE/` because the markdown guard
allows no `.md` beside the Go code.

## Fit with go-qa-ci

go-qa-ci runs these rules: `src-go/qa/driver` calls its `driver.Main(rules.All()...)`, which
registers go-qa-ci's bundled analyzers beside them, and `src-go/qa/go-qa.toml` names the nested
module (`[analyzers] module = "qa"`, `driver = "./driver"`, and `modules = ["qa"]` so the
per-module tools run over it too). The analyzers are proven with `analysistest`, and the names are
identifier-only (`project_*`). Gap: go-qa-ci expects one page per analyzer under its `docs_dir`
and the per-rule pages are not written; this catalogue stands in for them.

## The rules

| Rule                              | Defect class                                                                                                                                           | Found in                                                                                                 |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------- |
| `project_seam_exec`               | a process started outside the runner, so no fake sees it                                                                                               | `teams`: `syscall.Exec` in the launcher hand-over                                                        |
| `project_seam_commands`           | a program run outside the package that owns it, or a command the rule cannot judge (run-time name, hidden wrapper argv, shell `-c`)                    | `sessions`: `systemctl reboot`; `teams`: the take-down heading via `gum style`                           |
| `project_tmux_targets`            | a tmux `-t` target in the wrong form for its command (fedora-desktop#69)                                                                               | the bash library, before the port                                                                        |
| `project_testclock`               | wall-clock reads in tests (date-dependent tests failed in the hour after midnight)                                                                     | `transcript` and `sessprobe` tests: 23 `time.Now()` fixtures                                             |
| `project_waitdelay`               | a function that sets `exec.Cmd.WaitDelay` and never names `exec.ErrWaitDelay`, so a finished command's output is dropped                               | `qa/internal/proc`: bug B25 of the qa-all port                                                           |
| `project_package_tests`           | a package with no `_test.go` file, whose code the module-wide coverage floor can hide behind the rest of the module                                    | the old Go gate's every-package-has-tests check, kept when go-qa-ci replaced it                          |
| `project_homeabbrev`              | a hand-rolled `~` home abbreviation (a bare prefix replace turned `/home/<user>X` into `~X`)                                                           | `teams` tests: `strings.Replace(..., "~", 1)` twice                                                      |
| `project_hidden_bindings`         | an action reachable only by a key, with no visible, named choice for it (also in shell fzf and Python Textual: `qa-hidden-bindings.bash`)              | `teams` picker: Ctrl-A/X/O on Running sessions; `ccy-sessions`: Ctrl-T/X/N                               |
| `project_lifecycle_turn`          | a member's lifecycle notice decided from a read of main and posted outside the team's turn, so another post of the run can come between                | teamchat `ends`: `back` posted after the run's own `disconnected` (Plan 00209)                           |
| `project_keeper_log_barrier`      | a keeper test reading keeper.log past the harness's turn-aware reader, so a wait on a line releases before its turn has done the rest                  | teamchat `keeper`: `TestOwedNotice` saw the seat held after its line (Plan 00209)                        |
| `project_fakeclock_count`         | a test that judges a fake clock's timers by the number armed, which a timer it did not mean changes: a wait advances too early, an assertion lies      | `link` tests: `TestAnOpBeforeThePIDIsNotAnswered` hung; `engine` TIMEOUT tests at a merge                |
| `project_goroutine_end`           | a goroutine that signals no end, so no owner can wait for it, and a stop missed on one path leaks it for the life of the process                       | `link`: `go l.read()`, about 145 control readers left blocked in one test binary                         |
| `project_rawconn_probe`           | a RawConn readiness callback that answers without testing its descriptor, so readiness the poller saw and Read reset is missed and the wait never ends | `link`: the pidfd watch never saw a keeper that died before the watch first ran                          |
| `project_leakcheck_once`          | a second `leakcheck.Check` on one test, which judges before the cleanups registered between the two run and reports a world they stop as a leak        | `engine`: `TestLiveNeedsEveryFact`'s second world reported the first's transcript watcher                |
| `project_refusal_kind`            | a declare.py refusal recovered by its type and never by its kind, so a refusal of the host_vars file is asked again as if the answer were wrong        | `teams lxc-add`: "entries 5 and 6 both declare" asked three times (Plan 00201)                           |
| `project_realtime_bound`          | a bound on real time that a test can shorten, which then races the step the test asserts; a test that passes when a real timer fires                   | teamchat `host/lxc`: 1 of 4 probes reached the fake hang before a 20 ms bound (Plan 00209)               |
| `project_esc_joined`              | a screen that loses an Esc typed just before another key, which arrives as that key with Alt (also in shell fzf: `qa-hidden-bindings.bash`)            | `teams` row menu: Esc then Down left the menu open and moved its cursor (Plan 00177)                     |
| `project_duration_overflow`       | a `time.Duration` built from a number with no bound that keeps it inside int64 nanoseconds, so a large input comes out negative                        | `vmtest prune-runs --older-than 200000` pruned every failed run (Plan 00192 Task 3.10)                   |
| `project_command_failure_dropped` | a command's failure only tested, never carried, so the person sees a guessed cause                                                                     | `ccy --token-usage`: any curl failure was "could not reach the API" for 15 minutes (Plan 00191 Task 4.9) |
| `project_self_erasing_clipboard`  | a secret kept short-lived by a clipboard meant to erase itself after a paste, or text promising it (also in shell: `bash-self-erasing-clipboard`)      | `teams` login: `wl-copy --paste-once`, and "for one paste" (Plan 00201 Task 2.12)                        |
| `project_liveness_word`           | a status word read as running ("active", "online", "listening", "alive") spelled where no live source decides it                                       | `teams` board: every member not parked shown green "active" (Plan 00177)                                 |
| `project_agent_branch`            | a teamchat branch on the run's launch value outside the agent seam, so a second agent program is a refactor of every comparison and not an adapter     | teamchat `keeper` and `link`: the launch comparisons the agent seam replaced (Plan 00209)                |

Each rule's doc string (`go vet -vettool=<driver> help project_<name>`) says what it does and
does not catch; the sections below are the same facts with their proofs. Anything a rule does
not catch is listed under it, so the catalogue claims no more than the code does.

### project_seam_exec

Outside `internal/execx` (and `qa/internal/proc`, the nested `src-go/qa` module's own seam, which
the rules also judge), any reference (a call, a function value, an argument, an assignment)
to `exec.Command`, `exec.CommandContext`, `syscall.Exec`, `syscall.ForkExec`,
`syscall.StartProcess` or `os.StartProcess`; any use of the `os/exec.Cmd` type (composite
literal, `new(exec.Cmd)`, a parameter); and the `Run`, `Start`, `Output` and `CombinedOutput`
methods of one. Matching is by `types.Info` object, so an import alias or a dot import does not
hide it. `exec.LookPath` is allowed anywhere.

- Red, fixture: `testdata/src/fix/internal/badexec` (`bad.go` the direct calls, `evade.go` the
  function value, the `exec.Cmd` literal and `.Run`, `new(exec.Cmd)`, a method value,
  `ForkExec`, `StartProcess`; `LookPath` clean; the owner `execx` clean).
- Red, real tree: `syscall.Exec outside internal/execx` in the launcher hand-over (now `internal/teams/host`).
- Green: `execLauncher` now hands over through `execx.Execer` (`launcherVia`, tested with the
  fake).
- Not caught: raw `syscall.Syscall(SYS_EXECVE, ...)`, cgo, and a process started by code in a
  dependency (`src-go` has none). An `sh -c` string handed to the runner is not a process start
  here; `project_seam_commands` flags it.

### project_seam_commands

For an `execx.Cmd` built outside `internal/execx` (a composite literal, keyed or unkeyed, or an
assignment to `.Name`; test files are exempt, a test spells out the argv it expects):

- `Name` must be a compile-time constant (`go/constant` through `types.Info`: a literal, a named
  constant, a constant expression). A `Name` chosen at run time is a finding unless the function
  has a row in `dynamicNames` (package, function or `Type.method`, reason). A dynamic `Name` is
  not judged against the owner table; each row's reason says what it can be.
- The program is judged by its base name: `/usr/bin/gum` and `./bin/tmux` are `gum` and `tmux`.
- A wrapper is seen through to the program it runs: `env [-u X] [VAR=val] prog`,
  `sudo [-flags] prog`, `timeout [-s S] DURATION prog`, `nice [-n N] prog`, `setsid prog`,
  `nohup prog` and `systemd-run [options] -- prog`, nested. The wrapper's argv must be a literal,
  or `append` / `slices.Concat` of literals, that shows the program; otherwise the rule reports
  `cannot see which program "env" runs` (so `declare` writes its `env` argv inline).
- An owned program may be run only by its owner (the `commandOwners` table): `sudo` by
  `internal/sudo`, `tmux` by `internal/tmuxccy`, `gum` and `fzf` by `internal/ui`, `systemctl`
  and `busctl` by `internal/systemd` (`busctl` also by `internal/wsimedia`, the media players
  on the session bus), `git` by `internal/gitx`, `ps` and `docker` by
  `internal/sessprobe`, `podman` by `internal/sessprobe` (a session's container) and
  `internal/podfreeze` (pause, unpause, networks: a table entry may list several owners), and the
  `lxc-*` tools by `internal/lxcfreeze` (`lxc-ls`, `lxc-info` and `lxc-attach` also by
  `internal/teamchat/host/lxc/lxccmd`, teamchat-admin's as root, which may not link
  lxcfreeze's TUI or sudo), and `nmcli` by `internal/lxcfreeze` (inside a
  container) and `internal/ftpcamera` (the hotspot). Any other program (`wl-copy`, `gh`, `id`, `tput`, `systemd-escape`,
  `python3`, ...) is unowned and free. The owner's own program is trusted: what the owner puts
  after it is not examined here, except behind `lxc-attach`, a wrapper that stays judged even for
  its owner (`lxc-attach -n X -- prog` is judged as `prog`, so `nmcli` is lxcfreeze's and `git`
  inside a container is still a finding). A function with a `dynamicNames` row may hand
  `lxc-attach --` a program chosen at run time (lxccmd's `LXC.Guest`, the host-written kit
  binary); without the row the program it cannot see is a finding.
- A call of a **program runner** is judged as the `Cmd` it would build. `programRunners` lists the
  entry points that take a program and its arguments with no `Cmd` at the call site:
  `sudo.Sudo.Capture`, `AsRoot` and `Run`, lxcfreeze's `runAct` and `probe`, and ftpcamera's
  `asRoot` and `rootCapture`. The argument at
  the table's index is the program, judged by `checkProgram`'s walk (constant, owner table,
  wrappers, shell `-c`). A spread slice (`Capture(ctx, argv...)`) must show a literal head or the
  enclosing function needs a `dynamicNames` row (`runAct`, `probe`, agentbus's `rootRun` and
  `rootShow`, and ftpcamera's `asRoot` and `rootCapture` have one, with the reason). The callee is resolved through `types.Info`, so an alias
  does not hide it; a runner method taken as a value (`f := s.Capture`, `(*Sudo).Capture`) is
  flagged where it is taken, because the later `f(ctx, "lxc-ls")` names its program where no
  rule can see it.
- A shell with `-c` (`sh`, `bash`, `dash`, `zsh`), directly or behind a wrapper, is flagged: it is
  the `sh -c` route round the argv the runner shows. The one excuse is a row in `shellStrings`
  (package, function or `Type.method`, reason), for a program whose own specification is a
  command line a user wrote for a shell: `pingbus status-line`'s `runInner`, which runs the
  settings' `statusLine` command as `/bin/sh -c` because Claude Code does (Plan 00213
  SPEC-status-line.md S9). A row excuses only the shell string; an owned program in the same
  function is still judged.

`LookPath("gum")` and text containing a program's name are not commands and are not flagged.

- Red, fixture: `testdata/src/fix/internal/badcmd` (every form above flagged, with the clean
  neighbours: `wl-copy`, `systemd-escape`, `env PYTHONPATH=... python3`,
  `systemd-run -- setsid gio`, `bash script.bash`; the `_test.go` file clean), the owner
  `sudo`, `other/internal/tmuxccy` (the owner running tmux, directly and under `systemd-run`,
  clean; running `git` flagged), `dynamic` (a `dynamicNames` row clean, its neighbour flagged), `shellstring` (a `shellStrings`
  row's `sh -c` and `env ... bash -c` clean, its neighbour flagged, `git` in an excused function
  flagged),
  `badrun` (a package that is not lxcfreeze running `lxc-ls`, `lxc-freeze`, `nmcli`,
  `lxc-attach`, `git` behind `env`, `sh -c`, a run-time program, a spread parameter, no program,
  and the method value and method expression forms, all flagged; unowned programs and the
  agent-bus path clean) and `lxcfreeze/runs.go` (the owner running its tools clean; `git` and
  `sh -c` inside `lxc-attach --`, a hidden program and a missing `--` flagged).
- Red, real tree: `internal/sessions/reboot.go:199` (`systemctl`),
  the take-down heading in `internal/teams/ops` (`gum`); after the rule was hardened, `busctl` and `ps` run
  from `sessions`, `podman`/`docker` queried from `sessions`, `env` hiding `python3` in `declare`,
  `systemd-run` hiding `tmux` in `tmuxccy`, and seven run-time names.
- Green: `systemd.Reboot` and `systemd.CanReboot`, `sessprobe.ProcessTable` and
  `sessprobe.RunningContainerNames` (new, tested), the existing `ui.Gum.Box`; the engine query
  in `sessions` now runs through `sessprobe`; the remaining run-time names (the container engine in
  `sessprobe`, the project's daemon CLI, the launcher, two plays) are six `dynamicNames` rows.
- Red, real tree (program runners): `internal/lxcfreeze/renew.go` built its `lxc-attach ... nmcli`
  argv in a variable and spread it into `Capture`; the rule reported both calls, and the argv is
  now spelled in the call. Zero other findings: every other caller passes a literal program.
- Not caught: an `execx.Cmd` built by copying another; a program runner reached through an
  interface; the flags and arguments an owner uses (below).

What the owners do with their program is held by their own tests, not by this rule: `sudo -n` is
asserted in `internal/sudo`'s tests (`TestAsRootRunsSudoN`, `TestRunAfterReadyUsesSudoN`; the
`Ready` probe is `-n -v`, and only its prompt, `-v`, omits `-n`, deliberately). No analyzer checks it, and the tmux argument forms by `project_tmux_targets`.

### project_tmux_targets

In `internal/tmuxccy`, the only package that builds tmux argv: the `-t` target of a pane command
(`capture-pane`, `send-keys`, ...) must be `paneTarget(name)` (`=name:`, the colon is required:
real tmux refuses the bare `=name` for panes), and the `-t` target of a session command
(`kill-session`, `list-clients`, `attach-session`, ...) must be `"=" + name`, with no colon. The
commands are classified in the `paneCommands` and `sessionCommands` tables. A `-t` beside a
command in neither table is flagged so the table is extended, and so is a function that builds
both kinds, so each target can be judged. `-s` targets (`detach-client -s`, a session) are not
checked.

- Red, fixture: `testdata/src/fix/internal/tmuxccy` (good pane and session forms clean; bare
  `=name` for a pane, `paneTarget` or `=name:` or a bare name for a session, a mixed function and
  an unclassified command flagged).
- Real tree: clean. The review claimed `tmuxccy/panes.go:84` (`"="+name`) was a missing colon; it
  is `kill-session`, a session command, so `=name` is the correct form.
- Judged per function: the command words found in the function against the `-t` in the same
  argument list. Not caught: a target built elsewhere and passed in, and an argv assembled from a
  variable. `scripts/test-ccy-tmux-targets.bash` runs the real forms against a real tmux.

### project_testclock

Any reference to `time.Now`, `time.Since` or `time.Until` in a `_test.go` file: a call, a function
value (`n := time.Now`), an argument (`stamp(time.Now)`), a struct field, a package-level
initialiser. Excuse: a `clockAllowances` row (package, test function, reason). There is none: a
test that must show a real sleep ran (`TestTakeOverDefaultsToTheRealClockAndSleep`) runs the
call in a goroutine and requires a timer (`time.After`) to fire first. A package-level
declaration cannot be excused. go-qa-ci's bundled `gqaci_test_clock` judges the same reads.

- Red, fixture: `testdata/src/fix/internal/badclock` (the three calls; the function value,
  argument, field, package-level `var` and closure forms in `evade_test.go`; `time.Date` and
  production code clean) and `allowedclock` (the allowed function clean, its neighbour flagged).
- Red, real tree: 23 findings in `transcript_test.go` and `sessprobe/join_test.go`.
- Green: fixed instants (`fixedMtime`, `sampledAt`).
- Not caught: a clock read in a non-test helper; production takes its clock through a seam.

### project_package_tests

A package whose directory holds no `_test.go` file (external `package x_test` files count). The
module-wide coverage floor can hide an untested package behind the rest of the module; this keeps
the every-package-has-tests check of the retired `scripts/qa-go.bash`. A directory that cannot be
read is reported, never skipped.

- Red, fixture: `testdata/src/fix/internal/notests` flagged; `withtests` and `externaltests` clean.
- Not caught: a test file with no `Test` function; only the presence of a test file is checked.

### project_homeabbrev

Outside `internal/term` (which owns `term.AbbrevHome`), test files included, with a string
constant that is `"~"` or begins `~` (or the rune `'~'`): `strings.Replace`, `ReplaceAll`,
`NewReplacer`, `TrimPrefix`, `HasPrefix`, `CutPrefix`, `TrimLeft`, `Trim`; any `fmt` formatter
(`Sprintf("~%s", p)`); `path.Join` and `filepath.Join`; `WriteString`, `WriteByte` and
`WriteRune` on a `strings.Builder` or `bytes.Buffer`; and a non-constant string concatenation whose
leftmost operand is such a constant (`"~" + rest`).

- Red, fixture: `testdata/src/fix/internal/badhome` (`bad.go` the five original functions,
  `evade.go` the concatenation, formatters, builder and buffer writes, trim, replacer and join; a
  `_test.go` flagged, an unrelated `Replace` and a `~` that does not begin the text clean; the
  owner `term` clean).
- Red, real tree: the `board` and `login` tests of `internal/teams`.
- Green: both use `term.AbbrevHome`.
- Not caught: a `~` assembled from non-constant pieces (`string(rune(126))`, a variable). A
  legitimate "does the answer begin with `~`" check has no exit but `internal/term`; add an
  allowance table of the same shape as the others when one is needed.

### project_waitdelay

A function that assigns `(*os/exec.Cmd).WaitDelay` and never names `exec.ErrWaitDelay`. Setting
the field makes `Run` and `Wait` return `ErrWaitDelay` when the command has exited but a child
still holds its output pipe; the output written until then is complete. A runner that treats
every error but an exit status as a failed run drops it. The function that sets the delay must
decide what the error means, so it must name it.

- Red, fixture: `testdata/src/fix/internal/badwait` (`bad.go`: the qa-all runner's original shape,
  a capture returned only on success, and a delay set in a function that leaves the decision to
  its caller; `good.go`: the same run with an `errors.Is(err, exec.ErrWaitDelay)` branch, and a
  command with no delay, clean).
- Red, real tree: `execx.OS.Run` set the delay and named the error nowhere (its default branch
  happened to keep the capture); `qa/internal/proc` had the bug itself, fixed as B25 of
  `SPEC-qa-all.md`.
- Green: `execx.OS.Run` has its own `ErrWaitDelay` branch that returns the capture with the
  error; `proc.OS.Run` keeps the capture, flags the leftover and ends its process group.
- Not caught: a function that names the error and still drops the output (the proofs of the two
  runners hold that), and a delay set by a function that does not itself run the command.

### project_fakeclock_count

A wait on a fake clock (a type whose method set has `Advance(time.Duration)`) by the number of its
armed waiters: any use of its method that takes a `context.Context` and an integer
(`BlockUntil(ctx, n)`, whatever it is called, a method value included), and a call of its
no-argument `int` method (`Waiters()`) in a `for` statement or in a `return` of a function
literal (a poll). A count says how many timers are armed, not which; the code under test arms,
stops and re-arms its own on its own goroutines, so the count can be made by another timer, the
test advances too early, and the timer it meant is armed afterwards at the new time and never
fires. The fix is `clock.(*Fake).AwaitDue(ctx, d)`, which returns once a waiter due `d` from the
fake's present reading is armed: the timer named by when it is due.

- Red, fixture: `testdata/src/fix/internal/badcount` (the originating snapshot and wait, a method
  value, a helper's parameter, a `for` poll, an `if`-`break` poll, a predicate, a second fake
  behind an interface spelling its wait `WaitForTimers`), over the stubs `fakeclock` and
  `othertimers`; a count asserted once, in a range loop or a `t.Run` body, `StepWall(ms)`, a
  wall reading (`int64`) in a predicate and a count on a type with no `Advance` clean.
- Red, real tree: 28 findings, every `BlockUntil` call in `internal/teamchat` (13 test files and
  the fake's own tests); the hang was the link server's read limit, armed on accept, making the
  count before the hook's deadline was armed.
- Green: `BlockUntil` is gone from the fake; each wait names its timer with `AwaitDue`. Two
  tests had waited on whichever timer made the count: `TestWaitAcrossARestart` now waits for
  the 5 s backoff before the link listens, and `TestTrailingFactsAloneOnceAMinute` waits for
  the loop's minute, not a 2 s pace that is never armed.
- Widened, the merge of n3 into n3e (Plan 00209): **every other use of the count method outside
  the package that declares it** (an assertion, a variable, a method value, a helper argument,
  a switch) is flagged too, with "counts the fake clock's waiters". The fake's own package, its
  external tests included, may assert its own count. A function named in `waiterCountAllowances`
  (package, function, reason) may assert the count when its claim is that nothing at all is
  armed on its clock, so every timer is the one it means; a wait by count is never excused. The
  hazard: three of n3d's TIMEOUT tests asserted `Waiters() == 1` for "the one TIMEOUT timer"
  and `== 0` for "no TIMEOUT timer", and once n3e's delivery loop and proof armed their own
  timers on the same fake they failed on correct code; the same assertion passes when the
  timer it means is missing and another makes up the number.
- Red, fixture (widened): `badcount/assert_test.go` (the originating assertion, a count compared
  with zero, kept in a variable, a method value, a helper argument, a switch, a table loop, a
  subtest, a helper returning it, a second fake), the snapshot in `TestOriginating`;
  `fakeclock`'s own and external tests clean; `countallowed` (a row clean, a poll inside it and
  its neighbour flagged).
- Red, real tree (widened, commit `cc98cda0b`): 14 findings, the same untagged and with
  `busfailpoints`, `tuwunel` and `teamchatrelease`, none in the nested `qa` module: 7
  assertions (each with its message argument) in `engine/outbox_test.go` and
  `engine/timeout_test.go`, matching the independent search's 7.
- Green (widened): `clock.(*Fake).Armed(d)`, AwaitDue's question asked without waiting; the
  engine tests name the TIMEOUT timer by the engine's own field (`w.timeoutArmed`, read on the
  state goroutine) and by its wall due time (`w.dueAt`), and each fails with the timer armed
  1 ms late. Four allowance rows: the client's `world.noLink` and `TestASendNeverRetries` (the
  client never waits), the inbox's `TestOneDeliveryRecordsTheAuthFrameThenTheContent` and
  faketranscript's `TestRecordNeverWritesNothing`.
- The four rows, each a test whose code under test is the only user of its clock and whose
  claim is that it arms nothing left armed: the client answers with no link and never retries a
  send without any wait (LN3), so any armed waiter is a wait it must not make; the inbox client
  stops every phase timer of a finished delivery; a faketranscript record with delay `Never`
  arms nothing. A timer one of them leaves armed is that claim failing, not another timer.
- Not caught: a fake whose advance is spelled otherwise; a fake held as an interface that has
  the count method but no `Advance` (`var c interface{ Waiters() int } = clk`, found by the
  conformance review; none in the tree); an allowance row whose test later shares its clock
  with other timers (the row's reason says why its clock has one user).
- The next wider form is held by the fake, not an analyzer, being a fact of a run: a timer
  named by its due must be the only waiter due then, so `AwaitDue` and `Armed` refuse a due two
  waiters share (`clock.ErrAmbiguousDue`, naming where each was armed;
  `TestAnAmbiguousDueIsRefusedNamingBothWaiters`). Every teamchat test that uses them passed
  with the refusal in place, untagged and with `busfailpoints`, `-count=2`; a test that arms
  several timers at one instant names each by a distinct due (`host/lxc`'s
  `TestProbesThatHangEndAtTheirOwnBound` starts its probes a millisecond apart).

### project_goroutine_end

A `go` statement whose goroutine signals no end. Its body (a function
literal, or a function or method of the same package, generic ones by their origin) signals its
end when it defers `close(ch)`, a `Done()` with no parameter and no result (a WaitGroup's; a
context's `Done` returns a channel and is not one) or a function literal that closes, sends or
calls such a `Done`; or when its last statement is such a close, `Done` or send and nothing in
the body returns before it; or when every return of its own directly follows a close, a
`Done` or a send on a channel made buffered in the package (a variable set once, by
`make(chan T, n)` with a constant `n` above 0, so the one send cannot block), and it cannot
fall off its end without one (its last statement is one, or a `for` with no condition and no
`break` or `goto` in it), which ends it only through a signal that cannot block; or when the
body is one call of a function that signals its end. Goroutines in the `_test.go` files of a
test package (internal or external) whose own TestMain calls a `leakcheck` package's `Tests`
are not judged: the test binary fails when one is left running.
A call of another package's function, or of a function value, is flagged: its end cannot be
seen. A goroutine that signals no end cannot be waited for, so nothing proves it stopped, and
a stop missed on one path leaks it for the life of the process with what it holds.

- Red, fixture: `testdata/src/fix/internal/badgo` (the originating reader stopped by a channel
  only the success path closes, a drain loop, another package's function, a final send an early
  return skips, a function value, a deferred call that is not a signal, a wrapper of an unowned
  body; a deferred close, a deferred `wg.Done`, a final send of a result, an owned method
  started directly and through a wrapper, a WaitGroup's `Go`, a deferred literal that closes, a
  final close, a generic function, and a loop whose every return follows a send on a channel
  made buffered there (the line reader of `teams/flow/kit.go`) clean; a return with no send
  before it, a loop left by a `break`, a loop with a condition, the same loop sending on a
  channel not known to be buffered, and a `_test.go` goroutine in a package with no leak
  check flagged; `checkedgo`, whose TestMain runs the `leakcheck` stub, clean).
- Red, real tree: 15 findings (the same untagged, with `busfailpoints` and with `tuwunel`; none
  in the nested `qa` module); once test files were judged, 15 more in the tests of 10
  packages.
- Green: every instance has an owner that stops it and waits (the link's reader and accept
  loop, the keeper's waits and readers, the units dispatch, the signal relays and listeners,
  `ui.Lines.Close`); the tests of `execx` and `stt` wait for their goroutines, and those of
  `units`, `matrix`, `inbox`, `client`, `hook`, `keeper`, `wiring`, `link` and `engine` run
  `leakcheck.Tests`; 0 findings in all three builds.
- Red again, the merge of n3 into n3e (Plan 00209): 4 findings in n3's code, matching an
  independent search; green with the team's sender waited for beside its goroutine (`engine`),
  a stalled guest call handing its late result over itself (`guestroot`), the forge checks
  awaited by the batch that starts them (`teamsync`) and a test's canceller awaited.
- Red again, the merge of v2 onto F44 (Plan 00209 Task 8.1): 5 findings in F44 code; green with
  the signal relays (`rclonetail`, `ccc`'s size follower) started by a WaitGroup's `Go` and
  awaited by their release, `ccc`'s key copy reading the terminal only once poll finds a key
  beside a stop pipe (closing a blocking `os.Stdin` does not end a read of it) and awaited after
  the session, `reclaim`'s line reader owning the terminal and closing it before it waits, and a
  test's writer awaited.
- Not caught: an end signalled and never awaited, or awaited on only some of the owner's paths;
  `time.AfterFunc` and `context.AfterFunc` callbacks. The run-time half answers for the
  goroutines the tests run: `internal/teamchat/leakcheck` (`Tests` in a TestMain, `Check` in a
  test) fails a test or a binary that leaves a goroutine running.
- Next wider rule, not built: every goroutine's end awaited on every path of its owner. Which
  function owns a goroutine, and whether each of its paths reaches the wait, is a question of
  the call graph across packages, not a shape one package shows; referred to the owner with the
  remediation (Plan 00209 n3e-10 report).

### project_rawconn_probe

A call of a method named `Read` or `Write` of `syscall.RawConn`'s shape
(`func(func(uintptr) bool) error`, on any type) whose callback does not read its descriptor: a
function literal or same-package function whose descriptor parameter is unnamed, blank or never
read, or a callback that is a function value or another package's function (its body cannot be
seen). Test files are judged too. `RawConn.Read` resets the readiness the runtime's poller has
already seen before it first calls the callback, and parks until the next edge whenever the
callback answers false; a callback that answers without testing the descriptor waits for an
edge that may have passed. The fix is to test the descriptor on every call:
`unix.Poll(fds, 0)` as the keeper's pidfd wait does, or a read that answers false only on
`EAGAIN`.

- Red, fixture: `testdata/src/fix/internal/badraw` (the originating "first call arms" callback,
  a blank descriptor, a named and unread one on `Write`, a function value, a same-package
  function that ignores it, an interface of the same shape over a file's conn; a literal and a
  same-package function that test it, `Control`, and a `Read` of another shape clean).
- Red, real tree: 1 finding, `internal/teamchat/member/link/parent.go` `watch` (the same
  untagged, with `busfailpoints` and with `tuwunel`; none in the nested `qa` module).
- Green: the link's watch polls its pidfd on every call (`pidfdReadable`); 0 findings.
- Not caught: a callback that reads its descriptor and still answers without testing its state
  (logs it, or only compares it with a bound); `RawConn.Control` decides no readiness and is not
  judged.
- Next wider rule, not built: the descriptor must reach a readiness test (`unix.Poll`, a read
  answering false only on `EAGAIN`). What counts as a test is open-ended (a helper, a wrapper,
  a struct the descriptor is put in), and the search found no callback that reads its
  descriptor without testing it; referred to the owner with the remediation.
- Gap: go-qa's analyzers stage runs the project rules untagged only, so a finding in a file
  built only with `busfailpoints` or `tuwunel` is not gated (none exists); this holds for every
  rule here and is referred to the owner.

### project_leakcheck_once

A call that registers `leakcheck.Check` again on a test it is already registered on. A call
registers the check when it is a `leakcheck` package's `Check`, or a function or method of the
same package that passes one of its parameters on to such a call (directly or through another
such function); the test is the identifier given at that parameter. Within one body (a function
literal is its own), the second such call on the same identifier is flagged, and so is one inside
a `for` or `range` loop. Cleanups run last in, first out and `Check` judges in one, so a second
`Check` judges before the cleanups registered between the two have run, and reports as a leak the
goroutines of a world that those cleanups stop and wait for: a seed- and timing-dependent red that
blames correct code. The fix: register the check once per test, first; build a second world from
the first (a method that builds without `Check`), or give each world its own test with `t.Run`.

- Red, fixture: `testdata/src/fix/internal/twicecheck` (the originating two worlds through a
  wrapper, two different wrappers, the test on a second parameter, `Check` twice, a world in a
  loop, two in a function literal, a helper that checks its `testing.TB` twice; one world and a
  second built from it, `t.Run` subtests in a loop, and two different tests clean).
- Red, real tree: 6 findings in 5 tests (the same untagged, with `busfailpoints` and with
  `tuwunel`; none in the nested `qa` module): `engine` `TestLiveNeedsEveryFact` (the originating
  instance: its second world's check reported the first world's transcript watcher, which that
  world's stop closes and waits for), `TestAnItemAwaitingProofIsNotPending`,
  `TestMissingSurvivesARestart`, `TestBeginRunReturnsAnEarlierRunsEntries` (twice), and `link`
  `TestRemoveAnswersThenLeaves` (a world in a loop).
- Green: the engine's second worlds are built by `dworld.another` and `pworld.another`, which do
  not register the check; the link's loop runs each world in a subtest; 0 findings.
- Not caught: a test passed in a struct field or through a function value; a cleanup that stops
  goroutines registered on a test before its first `Check`.
- Next wider rule, not built: every cleanup that stops goroutines registered after the test's
  first `Check`. Whether a cleanup owns goroutines is not a shape the package shows, and the
  independent search found no instance; referred to the owner (Plan 00209 n3e-watchleak report).

### project_hidden_bindings

**What it is about.** Every action an interactive tool offers is a visible, named choice (a
row, or a line of the row's menu) reached with the arrows and Enter; a key is only a shortcut
to such a choice, printed beside it. The owner's words: "TUI must be UX controls not magic
keys - keys are OK but MUST NOT BE THE ONLY CONTROL SURFACE"
([InteractiveScripts.md](InteractiveScripts.md) rule 14).

**Why it exists.** An action reachable only by a key is invisible: nobody can find it, and it
can be pressed by accident. It recurred ("ah youve added magical key combos again instead of
UX?"): the `ccy-teams` Running sessions view added a session to a team (Ctrl-A), removed it
(Ctrl-X) and changed its role (Ctrl-O) through fzf `--expect` keys alone, and `ccy-sessions`
took over (Ctrl-T), ended (Ctrl-X) and started (Ctrl-N) sessions the same way.

**How to fix a violation.** In Go, make the action a `ui.Action{ID, Label, Key}`: put it in
`PickSpec.Actions` when it belongs to the screen (ui lists it as a row above Exit) or in the
row's `PickSpec.Menus` entry when it acts on a row (Enter on the row lists the menu); `Key` is
optional, and ui prints it beside the `Label` and accepts it only where that line is offered
(`Picked.Action` carries the `ID` back, `ui.NotOfferedError` a key the row does not have). There
is no other way to hand fzf a key: `PickSpec.Keys` is gone. In shell, make the action a row of
the list (`tmux-session.bash`'s `Start a new session here`) and drop the `--expect`; in a Textual
app, give the action a `Button("Label  (k)", action="app.<name>")` on its screen and keep the
binding as its shortcut.

Navigation is not an action, decided for this project (`src-go/qa/internal/fzfbind`, which
both checkers read): moving the cursor or the page, selecting rows (Tab's toggle in a
multi-select list, whose screen says so), editing the query, changing only how the same rows
are shown (sort order, the preview, its window and its command, the prompt, header and border
label, wrapping, tracking, search on or off), and leaving (Esc, q, abort) beside the visible
Exit row. A key that only accepts chooses the highlighted row: without `--expect`, which only
ui builds, fzf cannot tell the caller which key accepted, so it is the same as Enter. It is not
the same when the key first changes what fzf answers: a query edit (`put`, `yank`,
`clear-query`, a deletion, history) before an accept is handed back by `--print-query`, and
`select-all` or `toggle-all` before an accept by `--multi`, so the caller reads the key's
action off the answer. Such a chain is an action (`put+accept` in the message), whichever key
it is on. fzf events (`start`, `focus`, `load`, `change`, ...) are not keys. In Textual,
an action named `focus*`, `scroll*` or `cursor*` moves focus, the cursor or the view, and
`dismiss` and `pop_screen` leave the screen as fzf's abort leaves the list. fzf's query editing is
navigation because it acts on the line being typed in, which always has the focus; a Textual
action that clears a filter from anywhere on the screen (Esc in the model manager) changes
what the list shows from outside the widget being typed in, so it is an action and gets its
Button. In a Bubble Tea text input the same holds: moving, editing the line, accepting and
leaving are navigation, and so is **Tab completing the line**, decided by the owner: "Tab
completion is acceptable as a magic key because it's standard UX", provided the screen shows a
hint line saying Tab completes. The rule holds that condition by a shape it can see: Tab or
Shift+Tab taken by hand is navigation only in a package that binds Tab with constant help (the
binding ui's hint line is drawn from). Any other key a ui component takes is an action, matched
through a `key.Binding` with help that ui draws.

**What it flags.**

- Go (the analyzer, every package but the rules package, test files exempt): `"--expect"` or
  `"--expect=..."` outside the one builder in `expectBuilders` (`ui.expectArg`, which takes its
  keys from `ui.Action` values); `"--bind=SPEC"`, or `"--bind"` followed by SPEC as the next
  argument, binding a key to anything but navigation; a binding it cannot read (`--bind` with
  no constant beside it, a SPEC built at run time, a run-time argument followed by text that
  closes it and adds a binding); and a `ui.Action` literal with a `Key` and no `Label`. The text
  is judged wherever it is spelt: a literal, a constant, a variable, an `append` or
  `slices.Concat` element, a `Sprintf` format, a folded concatenation.
- Go, Bubble Tea (`qa/rules/teakeys.go`, same analyzer): a `key.NewBinding` with no
  `key.WithHelp`, an empty one, or one whose key or words are not constants, and a
  `key.Binding{}` struct literal (it can carry no help); a Bubble Tea key message type
  (`KeyMsg`, `KeyPressMsg`, `KeyReleaseMsg`, `Key`) named outside `internal/ui` and its
  subpackages (only ui's components take keys); and a key compared by hand: its `String`,
  `Keystroke`, `Code` or `Text`, or a variable assigned one (`k := msg.String()`, a switch's
  `s := msg.String(); s`), against a constant in `==`, `!=`, a switch case or a searching
  `strings` function (`HasPrefix`, `Contains`, ...), unless the key is line editing or
  navigation (`lineKeys`, `lineCodes`: arrows, Home/End, paging, Backspace/Delete, the readline
  edits, Enter, Esc, Ctrl-C). Tab and Shift+Tab by hand are allowed only where the package binds
  Tab with help (above); elsewhere "Tab taken by hand with no key.Binding for it".
- Shell (`qa-hidden-bindings.bash`, every tracked shell file that runs fzf, by name, path or
  behind `command`/`env`): every `--expect` (shell has no `ui.Action`), and `--bind` judged as
  above, in any command or array assignment of the file, since fzf argv is often built in an
  array, and in a scalar assignment whose value starts with an option (`opts='--expect=...'`
  spread unquoted, `FZF_DEFAULT_OPTS=...`), split on whitespace as an unquoted spread splits it.
- Python (`qa-hidden-bindings.bash`, every tracked Python file that imports textual): each
  `BINDINGS` entry (`Binding(...)` or a tuple) and each `.bind(key, action)` must be navigation
  or the action of a `Button(..., action=...)` on the same screen: built in the `compose()` of
  the class that binds the key, with an action that runs there (no namespace, `app.` in an App,
  `screen.` in a screen). A binding or `BINDINGS` value that is not a literal is a finding.
  The Footer's list of bindings is not the visible choice: it is a hint line, not a control
  that takes focus and Enter, so a binding the Footer shows still needs its Button. A
  `key_NAME` method or an `on_key` handler takes keys with no binding, so no Button can pair
  it: each is a finding. A Button paired only through `id=` and `on_button_pressed` does not
  count; it pairs when it runs the action with `action=`.

Every finding names `project_hidden_bindings`; this section is what it resolves to.

**Narrowings, each with its sentence.** The rules package and the shell gate
(`qa/hiddenbindings`) name the flags as the text they look for and build no fzf argv (neither
starts fzf), so they are not judged (`bindingSubjects`; the fixtures `fix/qa/rules` and
`fix/qa/hiddenbindings` prove the exclusion reaches nothing else). A literal compared with argv (`==`, `!=`, a switch case, an argument of
`strings.HasPrefix`, `Contains`, `slices.Contains`, ...) reads argv something else built, so it
cannot become a binding (`teamtest/console.go`, the fake fzf, is the one real case). Of
`strings.Cut`, `CutPrefix` and `TrimPrefix` only the separator or prefix is read: the first
argument is what they return, so it is judged, and what a constant prefix leaves of a constant
is judged as the flag it becomes. A
`_test.go` file is never built into a tool a person runs. A shell file that never runs fzf
builds no fzf argv (`--expect` is a flag of other programs too). The fixtures in
`tests/fixtures/hidden-bindings` carry the pattern on purpose and are not code.

**Calibration recorded here** (the project had none): the sweep scope is all first-party Go,
shell and Python git would publish, generated and vendored trees excluded (`scripts/qa-trees.tsv`),
plus the GNOME Shell extensions read by hand; display-only actions (fzf's `toggle-sort`,
`change-preview-window`, `toggle-track`, `preview`, ...) and any key that only accepts count as
navigation.

- Red, fixture: `testdata/src/fix/internal/badbind` (`bad.go`: the originating Ctrl-A/X/O
  `--expect`, keys bound to `execute`, `become`, `reload`, a chained `toggle+execute`, split
  `--expect`; a query edit or select-all chained before accept; navigation, any key that only accepts, display-only actions and events clean; `evade.go`: a constant, a variable, split
  `--bind` words with a literal, a run-time and no binding, `slices.Concat`, an appended
  literal slice, `Sprintf`, a run-time SPEC, a folded `"--exp" + "ect=..."`, a binding hidden
  after a run-time argument, a hidden key ahead of a run-time tail, a flag that `strings.Cut`,
  `CutPrefix` or `TrimPrefix` hands back; comparisons clean),
  `fix/internal/ui` (the builder clean, a second `--expect` in ui flagged, `Action` literals
  with a key and no label, keyed, blank and positional, flagged) and `fix/qa/rules` (clean).
  The shell and Python halves are proven against `tests/fixtures/hidden-bindings/fixture.bash`
  and `fixture.py` on every gate run (`fixture.py` includes a binding whose Button is on
  another screen, a `key_NAME` method, and Esc to `dismiss` clean; `fixture.bash` a scalar of
  options, `FZF_DEFAULT_OPTS`, and an unmarked `read -n1`, which is not caught), and by `src-go/qa/hiddenbindings` and
  `tests/helpers/qa_environment/test_textual_bindings.py`.
- Red, real tree (commits `8022a152` and `9e40ea6e`): Go, `internal/ui/picker.go:105`, the one
  `--expect` every keyed picker went through (`PickSpec.Keys`: the `ccy-teams` list and
  Running sessions views and `ccy-sessions`); shell, `tmux-session.bash` (`--expect` for Ctrl-N)
  and `history-search.bash` (a binding built in a variable); Python, `wsi-model-manager`
  (eight bindings: download, remove, refresh, clear filter, quit, and the remove question's
  yes, no and Esc, none with a control on screen).
- Green: `ui.Action`, `ui.Menu`, `PickSpec.Actions` and `PickSpec.Menus` (ui renders every
  action and builds `--expect` in `expectArg` alone); the `ccy-teams` picker moved onto them;
  `ccy-sessions` gives each session a menu and starts a new one from a row of its own; the
  launcher's detached-session offer has a `Start a new session here` row and `ccy_tmux_pick`
  takes no keys; the history search spells its start-up binding in the call; the model
  manager has buttons for every action, its keys kept as shortcuts.
- Not caught: a binding inside a shell string run by Go (`project_seam_commands` flags `sh -c`),
  `FZF_DEFAULT_OPTS` set in Go or in a shell file that does not run fzf, a binding assembled
  only from non-constant pieces, a key Go code reads other than through Bubble Tea
  (a raw terminal read; none in `src-go`), whether a ui component actually draws a
  `key.Binding`'s help, and so whether the Tab binding a package has is the hint on the screen
  that takes Tab (ui's tests hold that; the next wider rule below), a Bubble Tea key read
  through a type the rule does not list, a key read that leaves through a function's result, a
  struct field, an argument or a map index (`actions[msg.String()]`), a key compared with a value
  that is not a constant, a zero `var b key.Binding` or help changed at run time (`SetHelp`,
  `SetKeys`), an `Action` whose `Label` ui
  does not show (ui's tests hold that), single-key `read -n1` menus in bash (every one found
  prints its choices), and GNOME Shell keybindings (below).

**The next wider rule, not built:** every key a tool reads, by any means (`read -n1` in bash,
GNOME Shell `Main.wm.addKeybinding`, a raw terminal read), must map to an action that is also
a visible control. Not built because the key-to-control relation there is semantic (a printed
prompt naming the letters, a panel menu item doing the same thing) rather than a shape in the
syntax tree, and the search found no instance in a terminal tool. The GNOME speech-to-text
extension's global shortcuts (start, stop and abort a recording, with no panel menu item for
them) are the case it would decide; whether desktop-wide shortcuts are in the class is the
owner's decision, referred with this remediation.

For Bubble Tea the next wider rule is narrower and buildable: **every `key.Binding` a ui
component matches is one its view draws** (the bindings handed to `key.Matches` in `Update`
are the bindings handed to the help view in `View`), which would tie the Tab exception to the
hint on the same screen rather than to the package. Not built yet: it needs the two methods of
one model read together, and `internal/ui/pathinput` is its only case; Plan 00204 Task 2.4
tracks it.

### project_refusal_kind

A function (each function literal judged on its own) that recovers a `*declare.Error` of
`internal/declare` by its type (`errors.AsType`, `errors.As`, a type assertion, a type switch
case) and never tells the one kind asking again can fix from the rest: it names no
`declare.ErrUsage` (`errors.Is(err, declare.ErrUsage)`, `.Is(declare.ErrUsage)`) and does not
hand the exit code on whole (a returned `.Code`, converted or not, alone or as a field of a
returned literal such as `&stop{term.ExitCode(de.Code)}`) whatever the recovery finds. `ErrUsage`
named anywhere in the function clears every recovery in it; a code handed on clears only the
recovery whose block returns it unconditionally: a direct statement of the `if` body whose Init
or Cond recovers (`if de, ok := ...; ok { return de.Code }`, the real tree's `create.Wizard.run`,
`lxcadd.Adder.Run` and `summary`, `chooser.failed`, `ops.childExit`) when the Cond is the bare
recovery result (the found result of an Init that is one recovery and nothing else, `de, ok := errors.AsType[...](err)` or `ok := errors.As(err, &de)`; or the `errors.As` call) or that `&& de.Code != 0`; a later direct statement
after an assignment that recovers (`de, ok := ...; if !ok {...}; return de.Code`) or after an
`if` whose Cond is the negated bare result (`if !errors.As(err, &de) {...}; return de.Code`); or
a direct statement of the type switch case. A code handed on for one kind inside a branch of its
own, or under a Cond that tests a kind or a particular code (`ok && de.Code == 130`), leaves the
others asked again, and is a finding. Naming another kind (`ErrInterrupted`,
`ErrRefused`) or reading `.Code` to print it does not count: the rest are still asked again.
declare.py refuses in kinds: exit 64 (`declare.ErrUsage`) is the value just given, which another
answer can fix; exit 78 (`declare.ErrRefused`) is the host_vars file, its entries, or the answer
against them, which no answer to the question can fix (a team the file does not declare present
is one). A question
that reads every refusal as one verdict asks again when the file is at fault, and retyping can
never succeed. Re-ask only on `declare.ErrUsage`; stop on the rest, saying the refusal and the
file it is about ([InteractiveScripts.md](InteractiveScripts.md): re-prompting is for a
recoverable input mistake).

- Red, fixture: `testdata/src/fix/internal/badrefusal` (`bad.go`: lxc-add's original `checked`,
  the create wizard's `isRefusal`, a type assertion, a type switch, a checker literal, a kind
  read of a different error, an interrupt cancelled then every other kind re-asked, `.Code`
  only printed, `.Is(declare.ErrRefused)`, the code handed on for an interrupt only and every
  other kind re-asked, the same filtered in the recovering `if`'s Cond by a kind, by a code,
  after `errors.As`, by a bool variable, folded into the Init's `ok` and declared beside the
  recovery in the Init; `good.go`: a retry on `ErrUsage` only, an exit with `.Code` bare,
  converted and inside a returned `&stop{...}`, after an assignment, after a negated `errors.As`
  guard, under `ok && de.Code != 0`, `errors.As` in the Init with its `ok` alone and from a
  type switch case, `.Is(declare.ErrUsage)`, a bare `errors.Is`). The stub
  `fix/internal/declare` has the real shape.
- Red, real tree: `lxcadd.Adder.checked` (the owner's three "entries 5 and 6 both declare"
  refusals), and in `create`: `Wizard.checked`, `isRefusal` (any refusal read as "not
  OWNER/REPO") and `askHumans` (any refusal read as "your login cannot be an account name").
- Green: each re-asks on `declare.ErrUsage` only, cancels on an interrupt, stops on a refusal of
  the file naming it, and says any other failure as declare.py failing.
- Not caught, each still carrying the hazard (a 78 asked again), and referred to the owner as a
  rule left narrower than the hazard (Plan 00201 Task 2.20):
  - `ErrUsage` named but branched on the wrong way round, or re-asked on together with another
    kind (`errors.Is(err, declare.ErrUsage) || errors.Is(err, declare.ErrRefused)`, then a
    re-ask). Which branch re-asks needs data flow the syntactic rule does not do.
  - A kind read of some other error than the one recovered (`errors.Is(other, declare.ErrUsage)`):
    error identity needs data flow too.
  - A re-ask in an earlier branch of the block before the unconditional code return
    (`if !errors.Is(err, declare.ErrInterrupted) { ...; return false, nil }; return de.Code`):
    the return is direct, so it clears; telling a re-ask from any other early return is meaning.
  - A declare refusal re-wrapped in another error type before it is read: the type is hidden.
- Not caught, hazard-free as far as found: `ok && other.Code != 0` counts the `Code` of any
  `*declare.Error`, not only the one recovered.
- Flagged though hazard-free: a code handed on through a local (`code := de.Code; return code`);
  return the field itself.
- The other half of the class, declare.py giving a refusal the right kind, is in
  [Defect classes that cannot be a rule](#defect-classes-that-cannot-be-a-rule).

### project_realtime_bound

**What it is about.** A test that waits on real time races the host. A bound on real time
whose length a test can set (a field, a variable, a parameter, a function's result) makes the
test set it short, and the short bound then races the step the test asserts. A test whose
passing case is a real timer, or whose step is driven by one, asserts what happened in that
time, which under load is a test of nothing or a flake.

**Why it exists.** Plan 00209's `TestProbesThatHangEndAtTheirOwnBound` set the L3 probe's
bound to 20 ms of real time, started four probes against a root that hangs at a step, and
asserted that every probe reached the hang. Under a loaded `-race -shuffle` run one probe's
deadline fired before it got there: "1 of 4 probes hung at once". The sweep over F44 found the
same class in `qa/changed` (a 50 ms per-check limit that the checks the test expected to pass
also had to beat), the freeze tools' "Ctrl-C at the prompt" (a cancel after 50 ms, hoping the
prompt had been reached), a 90 ms timer racing a 100 ms sleep in `tmuxccy`, and a 200 ms
deadline racing a real shell's first line in `qa/internal/proc`.

**What it flags**, in every package of both modules:

- Outside the `_test.go` files: a call of `context.WithTimeout`, `WithTimeoutCause`,
  `WithDeadline` or `WithDeadlineCause`, or of `time.After`, `AfterFunc`, `NewTimer`,
  `NewTicker`, `Tick`, `Sleep` or a `Timer`'s or `Ticker`'s `Reset`, whose bound is not a
  compile-time constant (a deadline never is), and any of them taken as a value; and
  `os/exec.Cmd.WaitDelay` assigned or keyed from a value set at run time. Matching is by
  `types.Info` object, so an import alias does not hide one.
- In the `_test.go` files: a `select` case that receives from `time.After`, `time.Tick`, or a
  `*time.Timer`'s or `*time.Ticker`'s `C` and does not fail (no `Fatal`, `Fatalf`, `Error`, `Errorf` or `FailNow`, no `panic`, no
  return of anything but `nil`; a function literal in the case does not count), and any
  `time.AfterFunc`, called or taken as a value.

The excuse is a row in `src-go/qa/rules/table.go`, with its reason: `realClocks` for the real
side of a clock seam whose tests replace it whole, `processBounds` for a bound on a real child
process or real I/O that no test shortens so that it races a step (their tests fake the process
and report the timeout, leave the production constant, or shorten it only where its passing is
the outcome asserted).

**Narrowings, each with its sentence.** A bound whose length is a constant cannot be shortened
by a test, so it is not judged. In the `select` that ends the body of a bare `for {}` (no
condition, init or post), a timer case that is empty or a bare `continue` is the poll's gap:
the loop goes round and leaves only on another case or a check before the select. A counted or
ranged loop, or one that goes on after its select, passes once the timer has fired its count,
so its timer case is judged. A real timer in a test outside a `select` and not an `AfterFunc`
(`time.Sleep` in a poll loop, a real `context.WithTimeout` as a safety net) acts on nothing by
itself.

**How to fix a violation.** Bound the work on the package's clock seam (a `Sleep`, an `After`
or an `AfterFunc` the test replaces), so the test waits until the work is at the step and only
then moves the fake past the bound. A test of the real default runs in a `testing/synctest`
bubble, whose time passes only when every goroutine in it is blocked. A test that needs a step
to happen at a point waits for the point (a channel, a read that signals) instead of a delay.
In a test `select`, fail when a real timer fires first. A bound that only ever wraps a real
child process gets a `processBounds` row saying why no test races it.

- Red, fixture: `testdata/src/fix/internal/realtime` (`bad.go`: the probe's bound from a
  field, a timer on a field, a call bound from a field; `evade.go`: an import alias, a package
  variable and a package-level timer, a parameter, both deadlines and both causes, every timer
  form and both `Reset`s, a function value, a parenthesised call; `good.go`: constant bounds, a
  ticker, an injected clock, clean; `bad_test.go`: an empty `time.After` case, a `Timer.C` case
  that only logs, a fail in a goroutine, a `return nil`, an `AfterFunc` cancel called and as a
  value, a passing `Ticker.C` or `time.Tick` case, a loop's timer case that logs, the timer
  case of a counted loop, a ranged loop and a loop that goes on after its select, all flagged,
  and safety nets that fail, return an error or panic, a bare `for {}` poll gap, a fake channel,
  clean; `evade.go` also `WaitDelay` from a field, assigned and keyed) and
  `fix/internal/realclock` (a `realClocks` row and a `processBounds` row clean, their
  neighbours flagged).
- Red, real tree (`69037f993`, through `./scripts/qa-changed.bash`): 41 findings, 26 in
  `src-go` (the same with `-tags ccyteamstest` and `difftest`) and 15 in the nested `qa`
  module; the `AfterFunc` clause, added after the independent search, found 2 more.
- Green: the test-side findings are fixed. `tmuxccy`'s real-sleep test and `qa/changed`'s
  timeout test run in a synctest bubble (the second was red with a check made to take
  real CPU time past the 50 ms, and green in the bubble); the freeze tools cancel once the prompt
  reads; `board`'s held-call safety net fails in its own case; `qa/internal/proc`'s group-kill
  test passes its deadline once the child's pid is written; `execx`'s uncancelled-run test
  leaves Grace (its `WaitDelay`) at `DefaultGrace`. The status-line golden's poll loop is the
  poll-gap shape, clean. The 39 production findings are excused by 14 `realClocks` and 24
  `processBounds` rows, each read against its package's tests. Production behaviour is
  unchanged.
- The rows excuse every production finding, so the production clause guards only code written
  after it, and a row excuses its whole function. Both are the owner's decision (SPEC 3.3 and
  section 4), referred with this remediation.
- Not caught: a test that shortens a bound through a field or an environment variable (the rule
  judges the bound, not who sets it; the tables' reasons record what each row's tests do, and
  a new test that shortens one is not seen); a test's own constant deadline around a real
  process whose expiry it asserts (`qa/internal/proc`'s was found by the search); a timer case
  that returns a non-nil value meaning success (any non-nil return counts as failing); a
  `select` that passes on a real `ctx.Done()` of a deadline the test set; a timer channel held in
  a variable before the select (`c := time.After(d)`, then `case <-c:`); `WaitDelay` set from a
  tuple assignment (`err, c.WaitDelay = f()`) or through a dereference (`(*p).WaitDelay`);
  a bare `for {}` poll that a count or a clock before its select ends (`n++; if n > 3 { break }`),
  which makes it a counted loop again; `time.Sleep` followed by an assertion that nothing
  happened.
- Flagged though hazard-free: a select case that fails indirectly (sets a flag the test reads
  later); fail in the case instead.

**The next wider rule, not built:** every real-time bound in a test, whatever its length. Not
built because the safety nets (a `select` or `WithTimeout` that fails after seconds, a poll
budget that ends in `t.Fatal`) are most of them and are correct; telling a deadline the test
asserts from one it only guards with needs the meaning of the assertion.

### project_esc_joined

**What it is about.** A terminal sends Esc as one byte, and the same byte starts every escape
sequence, so a key reader waits a moment after an Esc to see whether more follows: fzf 100 ms,
Bubble Tea 50 ms. An Esc and the key after it inside that wait, or in one read, are read as that
key with Alt: Esc then Down is `alt-down`, Esc then Enter `alt-enter` (`ctrl-alt-m` in fzf 0.38).
tmux, at its default `escape-time` of 500 ms (before tmux 3.5), holds the Esc and hands the pane
both keys in one write. A screen with no meaning for the Alt key drops it: the Esc is lost.

**Why it exists.** Plan 00177 Task 10.4, the host agent's finding: in a `ccy-teams` row menu,
Down within about 50 ms of Esc, or both in one tmux `send-keys`, left the menu open and moved its
cursor, so the person, on what they took for the list, acted on the menu. Real fzf 0.38 in tmux
lost the Esc at 20, 50 and 80 ms and in one write, and kept it at 100 ms and over; Esc then
Enter, q, Ctrl-R, Up, Tab or j did the same. The Bubble Tea path line (`ccy-teams lxc-add`'s
Checkout step) took the typed path after a joined Esc instead of cancelling.

**What it flags.** In Go (test files are not judged): an `execx.Cmd` literal running fzf (a
constant `Name` whose base name is `fzf` or `fzf-tmux`) whose `Args` do not hold, as `--bind=`
constants, a binding of every key of `fzfbind.EscJoined` (Alt with an arrow, Enter, Space, a
letter or a digit, and Ctrl+Alt with a letter, less fzf's word editing: 65 keys) to `abort`
alone; the same literal binding any of fzf's word editing keys (`fzfbind.WordEditing`:
`alt-b`, `alt-f`, `alt-d`, `alt-bs`, fzf's default backward-word, forward-word, kill-word and
backward-kill-word) to `abort`, which would throw away the query being typed; a Cmd
whose `Name` is set to fzf field by field; and a Bubble Tea `key.NewBinding` of `"esc"` in a
package that never calls `esckey.Joined` (`internal/ui/esckey`). In shell, the gate
`qa-hidden-bindings` judges every command that runs fzf (behind `command`, `exec`, `nohup` or
`env` too; `command -v fzf` only looks it up) the same way, over its literal `--bind` words. An
fzf given an option that draws no screen (`--version`, `--help`, `--filter`, `--bash`, ...:
`fzfbind.DrawsNoScreen`) reads no key and is not judged.

**How to fix a violation.** In Go, start fzf through `internal/ui`, whose `fzfEscJoined`
constant binds every one of the keys to `abort`; a key a screen hands back instead (a row menu's
Esc then an arrow, which closes the menu and moves the list's cursor) goes in `--expect`, which
fzf prefers to the binding. In Bubble Tea, cancel on `esckey.Joined(k)` wherever Esc cancels. In
shell, give the fzf call the same bindings as literal `--bind` words.

- Red, fixture: `testdata/src/fix/esc/internal/ui` (`fzf.go`: no bindings, the arrows only, a
  path, no Args, the bindings beside the Cmd instead of in it, a Cmd built field by field, all
  flagged; one binding constant, two split constants, a positional literal, `--version`,
  `--filter` and another program, clean), `fix/esc/internal/ui/badline` (Esc bound, flagged)
  and `goodline` (Esc bound and `esckey.Joined` called, clean); the shell half against
  `tests/fixtures/hidden-bindings/esc-joined.bash`, proven on every run of the gate.
- Red, real tree: 3 Go findings (`ui.Picker.Pick`, `ui.Picker.Choose`, `pathinput`'s
  `cancelKey`) and 2 shell (`tmux-session.bash`'s `ccy_tmux_pick`, `history-search.bash`'s
  Ctrl-R).
- Green: `ui.Picker.Pick` and `Choose` append `fzfEscJoined`; a row's menu expects `alt-up`
  and `alt-down` and returns a `ui.BackError` (`ui.MovedBy`), and the teams list starts its
  next pass on the moved row (`PickSpec.StartRow`, `--bind=start:pos(N)`); `pathinput` cancels
  on `esckey.Joined`; both shell fzf calls carry the bindings. The freeze golden replay pins the
  new argv as SPEC-freeze bug 28. `files/etc/tmux.conf` sets `escape-time 10`.
- Not caught: fzf started through a program runner or a shell string (`project_seam_commands`
  flags those), the bindings as a separate `--bind` and spec pair in Go, `FZF_DEFAULT_OPTS`, a
  package that calls `esckey.Joined` on some other path than its Esc binding, Esc then a
  punctuation key or a shifted letter (`alt-/`, `alt-A`), and gum, whose prompts take no
  bindings. Left out on purpose: Esc then b, f, d or Backspace inside fzf's wait is still read
  as fzf's word editing and the Esc is lost; keeping the query a person is editing (Ctrl-R
  history, every `ui` filter) is worth more than that narrow case, and the arrows stay covered.
  The fixtures hold it: `AbortsWordEditing` in `fzf.go` and `edits` in `esc-joined.bash`
  flagged, `alt-b:backward-word` clean. Flagged though hazard-free: shell bindings spread in from an array, which the rule
  cannot read.

**The next wider rule, not built:** every key reader a screen uses (gum, Textual, a raw `read`)
must treat Esc joined to the next key as Esc. Not built because gum takes no bindings at all
(its fix is replacing the prompt, the owner's decision) and Textual is Python, outside both
gates; the tmux `escape-time` of `files/etc/tmux.conf` narrows the window for all of them.

### project_duration_overflow

**What it is about.** A `time.Duration` is an int64 count of nanoseconds, and Go's integer
arithmetic wraps: 106752 days, or about 292 years of seconds, is past it, and the product comes
out negative. A number read from input (an argument, the environment, a file, another program's
output) has no size of its own, so a duration built from it can mean the opposite of what was
asked. A negative wait or timeout fires at once.

**Why it exists.** Plan 00192 Task 3.10, the Fable review of the retention move:
`time.Duration(days)*24*time.Hour` in `vmtest`'s prune rule, with `--older-than` taken as any
whole number, made the cutoff negative from 106752 days, so `vmtest prune-runs --older-than 999999 --yes` (meant as "never by age") pruned the overlay of every failed run. The bundled
`durationcheck` linter does not see it: it allows `time.Duration(x) * time.Second` on purpose,
and only flags two durations multiplied when neither is spelt as that conversion of the `time`
package (it fired on the fixture's `stdtime` import alias, never on the real tree).

**What it flags**, outside the `_test.go` files and the owner `internal/duration`: a
multiplication of type `time.Duration` (either operand order, a chain, `*=`) with a factor that is
`time.Duration(x)` of a number `x` that is not a constant, or a variable assigned such a
conversion; and a conversion of a float that is not a constant to `time.Duration`. Each is judged
by the largest value its factors can have: a constant, or a number the package refuses above a
constant. A refusal is `x > K` or `x >= K` (`K < x`, `K <= x`) in the condition of an `if`, alone
or joined by `||`, whose block ends in a `return`; it bounds `x` (a variable or a field) in the
whole package, so a field refused where it is read bounds its use in another function. Sums,
differences, products, quotients by a constant and conversions of bounded numbers are bounded. A
factor with no bound, or a bound whose product leaves int64, is a finding.

**Narrowings, each with its sentence.** A plain `time.Duration(n)` of an integer is not
multiplied, and an int or int64 converted to int64 cannot overflow. A product of constants is the
compiler's to check. A `_test.go` file's numbers are written in it. `internal/duration` is where a
product is checked (`duration.Times`), so it is not judged.

**How to fix a violation.** Refuse the number above a constant where it is read, in the same
package (a usage error or a failure naming the largest value taken), or build the duration with
`duration.Times(n, unit)`, which refuses a negative number and a product that leaves int64.

- Red, fixture: `testdata/src/fix/internal/durations` (`bad.go`: the originating prune cutoff,
  the unit first, a parsed number refused only below, a unit set at run time, a field bounded
  nowhere, a comparison that only accepts, a refusal that goes on; `evade.go`: an import alias, a
  conversion held in a variable, `*=`, parentheses and an offset, a float, a bound whose product
  leaves int64, two numbers one unbounded, a bound on another variable; `good.go`: rclone-tail's
  refusal, the reversed comparison, a bounded float, a field bounded in another function, an
  offset of a bounded number, constants, a duration times a constant, `duration.Times`, a plain
  conversion, clean; `good_test.go` clean) and `fix/internal/duration` (the owner, clean).
- Red, real tree (`./scripts/qa-changed.bash` at the rule's commit): 6 findings, all in `src-go`,
  none in the nested `qa` module: `sessions` `seconds` (six environment knobs in seconds, as a
  float), `parseVerify` (`verify-restore --wait`), `secondWarning` (`reboot --in` minutes times
  the configured minute), `wsi` `stopRequested` (the stop grace read from `wsi-stop-grace`),
  `vmtest` `cmdMemberSSH` (`VMTEST_MEMBER_SSH_TIMEOUT`) and `planPrune` (the originating cutoff,
  by then bounded by `maxPruneDays` in `parsePruneArgs`, on another function's local, which the
  rule cannot see). The independent search, made before the rule, found the same six (the sixth as bounded
  elsewhere) and no other.
- Green: `ccy-sessions` refuses a knob over 86400 seconds, `verify-restore --wait` over 86400 and
  `--in`/`--minutes` over 10080 (a week), and `secondWarning` builds its wait with
  `duration.Times` (the configured minute is a field no refusal reaches); `wsi` refuses a grace
  over 30 seconds, the top of the setting's schema range; `vmtest` refuses
  `VMTEST_MEMBER_SSH_TIMEOUT` over 86400, and `planPrune` compares against
  `started.AddDate(0, 0, days)`, so no Duration is built. Each with a test first seen red; the
  prune cutoff's (`--older-than 106752` kept, not pruned) reproduces the originating defect.
- Not caught: a comparison with the refusal's shape that does not refuse (a block that returns
  the number as accepted); a large negative number (only the upper bound is read); a number held
  in a variable other than by a direct conversion (`d := n; time.Duration(d)` is judged through
  `d`'s own bound, but `secs := parse(); wait := secs * 1000` then `time.Duration(wait)` is not
  multiplied and not seen); a bound in another package; a `uint64` converted to a Duration;
  package-level initialisers; `time.Unix` and `AddDate` with a huge count (no instance found).
- **The next wider rule, not built:** every arithmetic on a Duration whose operand comes from
  input, addition and a Duration parameter times a constant included. Not built because a
  Duration that reached the code as a Duration was already in range, and telling input from a
  computed value needs data flow across functions; `time.ParseDuration` refuses an overflow
  itself.

### project_command_failure_dropped

**What it is about.** The error of a command run through the seam is the cause of its failure:
an exit code and stderr (`execx.ExitError`), a start or stdin-feed failure, a refusal. Code that
only asks `err != nil` or `errors.Is(err, sentinel)` and then reports a fixed guess has thrown
the cause away, and the guess can be false.

**Why it exists.** Plan 00191 Task 4.9, the owner's incident of 2026-10-29: `ccy --token-usage`
recorded every curl failure as status 000 with no cause, showed "could not reach the API" for
all five tokens, and cached it for 15 minutes, while the network was fine.

**What it flags**, outside the `_test.go` files and the seam packages themselves: a call of a
function or method of `internal/execx` or `qa/internal/proc` that takes a `Cmd` (and, through
facts, any function that returns such a call's error, so a wrapper is judged as the seam is)
whose error is only tested before the variable is next assigned. Carrying is any other use:
returning it, passing it to a call (`fmt.Errorf`, `errors.Join`, `errors.As`, a logger,
`execx.ExitedNonZero`), calling a method on it, or storing it, also on the right of the
assignment that replaces it (`err = errors.Join(err, release())`, read before the store). A run whose call is a statement,
or whose error goes to the blank identifier, carries nothing.

**How to fix a violation.** Put the error itself, or what `errors.As` finds in it (the exit code,
stderr), in the message or record the caller sees, or return it. A probe whose failure is its
answer asks `execx.ExitedNonZero(err)`: a program that ran and exited non-zero is the answer, and
any other error (it could not start, was missing, was killed) is reported with its cause, so a
missing tool is never a silent default.

- Red, fixture: `testdata/src/fix/internal/badfailure` (`badfailure.go`: the originating curl
  read and the tested-only forms; `wrapped.go`: a wrapper that returns the seam's error;
  `carried.go`: each way of carrying, clean), over `fix/internal/execx` and
  `fix/internal/wrapper`; committed red (e47fc3a04) and widened to wrappers after the
  independent search (010158bcd).
- Red, real tree: 107 findings over both modules, 3 more surfaced as the sweep turned helpers
  into wrappers. The independent search, made before the rule, found 15 present and about 25
  borderline Go instances; the rule was widened to wrappers to reach them (010158bcd).
- Green: every finding fixed, none excluded and the rule unnarrowed (Plan 00191's five sweep
  batches); the origin fixed in CCY 3.104.1, with a test that reproduces it.
- Red again, the merge of v2 onto F44 (Plan 00209 Task 8.1): 1 finding, `guest check`'s gh
  login check; green with gh's cause in the message (its exit status, the signal that ended
  it, its 30 s bound, or the errno that kept it from running), gh's output still unquoted (it
  names the account).
- Not caught: an error carried somewhere it is never shown (stored and dropped), or carried and
  then contradicted by a guessed message beside it; a cause read from `Result.ExitCode` alone; an
  error passed to a call that only inspects it, such as `execx.ExitedNonZero`, whose other branch
  must still report the cause (review holds that).
- **The next wider rule, not built:** any error, not only a command's, that is tested and then
  replaced by a fixed message. Not built because most such errors are sentinels compared on
  purpose (`errors.Is(err, fs.ErrNotExist)`), and telling a dropped cause from a decided
  branch needs the meaning of each sentinel.

### project_self_erasing_clipboard

**What it is about.** A secret put on the clipboard is pasteable by anything until it is
replaced or cleared. A clipboard told to erase itself after one paste (`wl-copy --paste-once`,
`-o`, `xclip -loops N`) does not keep that promise on GNOME: the desktop reads a new selection
at once and goes on serving its own copy after wl-copy has exited.

**Why it exists.** Plan 00201 Task 2.12, the host agent's finding (2026-11-02, mutter 50.6, no
clipboard extension and no clipboard process running): `wl-copy --paste-once X` exited within
1.5 s with no paste by any client, and `wl-paste` went on returning X; pastes 1, 2 and 3 all
gave the password. ccy-teams' new-password copy used the flag and told the person the password
was "on the clipboard for one paste; after that it is gone from the clipboard".

**What it flags**, outside `qa/rules` and in test files too: a literal with a constant `Name`
and an `Args` list (an `execx.Cmd`) running wl-copy with `--paste-once` or a short-flag word
holding `o` (`-o`, `-no`), or xclip with `-loops N` for N other than 0; and any string literal
or comment group (read as one text, so a claim wrapped over two lines is still read) saying
`paste-once`, "for/to/serve(s)/after one (a single, the first) paste", "the one paste", "gone
from the clipboard" or "gone after one paste". The shell half is `bash-self-erasing-clipboard`
in `.semgrep/bash-conventions.yml` (the `qa-patterns` gate, the same regexes over every
published shell script under `files/`, `scripts/`, `helpers/`, `extensions/`,
`fedora-install/` and `CLAUDE/Plan/`, comments included).

**How to fix a violation.** Copy without the flag and say plainly that the secret is on the
clipboard. Once the person has pasted it (Enter), or a bounded wait ends, read the clipboard
back without putting the secret in any argv, and clear it (`wl-copy --clear`) only if it still
holds the secret; leave it alone if something else was copied since, and report a read or a
clear that fails as the secret possibly still being there. Do not promise more than the clear
does: GNOME or a clipboard history may still keep a copy. ccy-teams' `login.Login` is the
model (`offerCopy`, `SettleClipboard`).

- Red, fixture: `testdata/src/fix/internal/badclip` (`bad.go`: the original Cmd, `-o`, `-no`
  under a path, `xclip -loops 1`, a constant claim, four phrasings in a slice, a claim in a
  comment and one wrapped over two lines, all flagged; `bad_test.go`: a test pinning the flag,
  flagged; `good.go`: plain wl-copy, wl-paste, `--clear`, `--type`, curl's `-o`, xclip
  `-loops 0` and "one paste at stop", clean) and `fix/qa/rules` (the owner, clean); the shell
  half against its section of `.semgrep/bash-conventions.bash`, proven on every run of the gate.
- Red, real tree (`d4f2a2da3`, through `./scripts/qa-changed.bash`): 13 Go findings in
  `internal/teams/login` (`login.go` 7, `password_test.go` 6; the same with `-tags ccyteamstest,difftest`, none in the `qa` module) and 2 shell lines in
  `scripts/test-ccy-teams-command.bash`.
- Green: every finding fixed (the copy is plain, `SettleClipboard` compares and clears, the
  texts and tests say so); the Clipboard Indicator refusal now says the extension keeps the
  password in its history past any clear, which is why no copy is offered with it on.
- Not caught: a flag built at run time or spread from a variable; the claim worded otherwise
  ("vanishes once pasted", "a second paste is empty"); prose in markdown, Python and
  JavaScript (no instance in Python or JavaScript; the markdown instances in
  `docs/agent-bus.md` and Plan 00201's ACCEPTANCE-owner.md and PLAN.md were found by the
  independent search and fixed by hand).

**The next wider rule, not built:** the same claim in every published file, markdown included
(a `generic` semgrep rule over what git would publish, as `gh-scope-outside-ssot` is). Not built
because it needs a gate of its own (command, wrapper, `qa-gates.tsv`, routing and QA.md rows):
the markdown instances exist and were fixed, so leaving that half undefended is the owner's
decision, referred with this remediation.

### project_liveness_word

**What it is about.** A state a person reads as "this runs now" ("active", "online",
"listening", "alive") that is decided by something that does not know whether it runs: a
registry entry, a configured seat, a record that the thing exists.

**Why it exists.** Plan 00177, the owner's report (2026-11-02): `ccy-teams`' list showed members
green and "active" whose sessions had stopped, while `pingbus status` called the same members
stale. The word came from `agent-bus list`'s park field (`active` means only not parked), so
every member never parked looked alive for ever.

**What it flags**, outside `internal/agentbus` (the bus's `LIVE` words and the registry's park
words it parses), `internal/systemd` (a unit's ActiveState) and `qa/rules`, in files that are
not tests: a string literal that is one of those words, in any case and with any surrounding
space. A struct tag and an import path are not flagged. A word another program prints, spelled
only to read it, is excused by a row of `livenessAllowances` (in `livenessword.go`) with its
reason: the phone app's `ACTIVE` manifest state in `internal/stt`, and the fake machine's
`systemctl is-active` answer in `internal/teams/teamtest`.

**How to fix a violation.** Take the word from the owner that read it from a live source
(`agentbus.LiveListening` and the other `Liveness` values, from the member's `LIVE` line, which
`agent-bus list` judges from the member's `agent_bus.status` exactly as `pingbus status` does),
or name the state for what its source says (`agentbus.WordNotParked`, parked). Show what could
not be read as unknown, with why.

- Red, fixture: `testdata/src/fix/internal/badlive` (`bad.go`: the original map of states, a
  constant, a switch case with surrounding space, an upper-case word and a raw string, all
  flagged; `good.go`: a struct tag, words that claim nothing, a word inside a sentence and the
  owner's constant, clean; `bad_test.go`: a test, clean), the owners `fix/internal/agentbus` and
  `fix/internal/systemd`, the allowance `fix/internal/stt` (its excused word clean, another
  flagged) and `fix/qa/rules`.
- Red, real tree (the defence commit, through the driver as `go vet -vettool`, both tag sets;
  none in the `qa` module): 6 findings: `teams/board/snapshot.go` (the original),
  `teams/board/table.go` (its green colour), `teams/board/graph.go` (`online`, decided by tmux
  for a seat and guessed for every other member), `teams/lxcadd/adder.go` (the park word written
  for declare.py), `stt/recording.go` and `teams/teamtest/world.go` (another program's word,
  excused with their reasons).
- Green: the board and the graph show the member's `LIVE` liveness (listening, stale, not
  listening), parked as parked, and unknown with the reason on the team row; the park word is
  `agentbus.WordNotParked`.
- Not caught: the claim in other words ("up" or "running" for a member), a word inside a longer
  text, one assembled at run time, a wrong source inside an owner package, and the same claim in
  Python, shell or markdown (`agent-bus list` still prints the park word `active`, documented as
  meaning only not parked; a rename of that wire word is referred to the owner).

**The next wider rule, not built:** "up" and "running" as well. Both are spelled throughout the
module for things a live source does decide (a homeserver unit up, a tmux session running, a
container running), so the rule would flag correct code by the dozen and need an allowance per
use: wider than the hazard.

### project_lifecycle_turn

In teamchat's ends package (`internal/teamchat/member/ends`), a use of `notice.LatestOwn`,
`notice.Disconnected` or `notice.Client`'s `Messages` or `Send` (so `team.Notices`' too) that is
not inside a function argument of the package's `turn` method, or of a package function that
reaches one outside a turn. A run's lifecycle notices are posted from several goroutines (back
from the team's goroutine at the run's start, the end from the link's finish, a leave from a
removal), and back is decided from what main says: read outside the turn, the decision can be
overtaken. `(*ends).turn(ctx, team, fn)` runs one lifecycle step per team at a time, and an end
or a leave refuses a back still to come. A helper is judged where it is used; a function literal
started by `go` or handed to a method named `Go` is outside the turn even when written inside it.

- Red, fixture: `testdata/src/fix/internal/teamchat/member/ends` (`bad.go`: the original
  `Back`, a read and a post outside the turn, an end through a helper, `notice.Disconnected`;
  `evade.go`: an import alias, a method value of `Send` and of a helper, a goroutine and a
  `WaitGroup.Go` started inside the turn, a direct `Messages`, a chain of helpers; `good.go`:
  the read and post inside one turn, a helper called only from turns, `RoomState`, clean). The
  notice package and a host-side caller beside it carry no finding.
- Red, real tree: seven uses in `ends.go`, in `Back` (the race: `TestE2ECmdKilled` and
  `TestE2EKeeperKilled` saw `back` after the run's own `disconnected` under load), `End`,
  `Close` and `Leave`.
- Green: each of the four runs its reads and posts inside `turn`.
- Not caught: that `turn` serialises per team and refuses a back after an end (the ends tests
  hold that), a read or post through another package's function other than the named ones.

**The next wider rule, not built:** every read-then-post of a lifecycle notice anywhere in
teamchat ordered against the member's other posts. Not built because the only other posters
(the host's `notice.Disconnected` callers in `admin` and `member/client`) run in other
processes, where no in-process turn can order them; they are made safe by the run's
transaction id (`tc-disc-<run>`), which is a different defence for a different hazard.

### project_keeper_log_barrier

In the `_test.go` files of teamchat's keeper (`internal/teamchat/member/keeper`), a use of
`paths.Run`'s `KeeperLog` outside the harness functions the `keeperLogReaders` table names
(`world.keeperLog`, the one reader; `world.turned`, the turn-end copy; `world.runs`, existence
only; `logTap.StartTicks`, the launch goldens' tap, opened while the run is made and read only
once the keeper has ended, since the end removes the run). The class is a test barrier released
partway through the work it waits for. A loop turn
may log a line and then do more: the owed notice's timer logs "not posted in time" and only
then lets the seat go, and `fail` logs "the link failed" before it closes the seat locks. A
test that waited for the line by reading the file saw it mid-turn and found the seat still held
(`TestOwedNotice` under load). The defence has two halves:

- The harness guard. The keeper calls `Config.turned` at the end of the launch steps and of
  each loop turn. The field is unexported, so only the package's own tests set it. The harness
  keeps keeper.log as that call left it, and `w.keeperLog` reads that copy while the loop runs.
  It reads the file before the loop, while the launch steps run in order, and after the keeper
  ends. `w.logged`, `w.log` and `w.logOf` all read through it, so a line shows only after its
  whole turn has run.
- The rule, which keeps every read on that reader.

Proof:

- Red, fixture: `testdata/src/fix/internal/teamchat/member/keeper`. It flags the originating
  barrier that polled the file, a direct read, a method value, a method expression and a
  goroutine. It leaves clean the allowed readers, the keeper's own code and `link.log`. Another
  package's tests (`member/elsewhere`) are not judged.
- Red, real tree: five uses in the keeper harness (`log`, `logOf`, `seen`, `runDir`) and in
  `keeper_test.go`'s wait for the run.
- Red, harness: `TestLoggedWaitsForTheTurnThatWroteTheLine` evaluates the barrier from inside
  the ready turn, which logs "the link is ready" and then starts CMD. It failed every run
  against the file-reading barrier.
- Green: every read goes through `w.keeperLog`, and both `TestOwedNotice` and
  `TestLinkRestartsWithBackoff` pass with a pause planted after their lines.

Not caught: a barrier on another observable a turn writes before it is done. keeper.json is
written mid-turn too, but its readers wait on the fake clock or the link's messages first.

**The next wider rule, not built:** the same guarantee for every reader of keeper.log, by
writing the log one turn at a time. That would cover the end-to-end `wiring` tests, which read
a real keeper's log and see no turn. Not built because a turn that crashes would lose its lines,
including those before a `busfailpoints` crash or a panic. Those tests poll the effect they
assert rather than a line. The other teamchat harnesses already wait for the end of a unit of
work: engine `until` runs on its state goroutine, link has `settled` and `await`, and teamsync
has `poll`.

### project_agent_branch

**What it is about.** A teamchat run launches an agent program, Claude Code or a plain command,
and its launch value says which. The agent seam (`internal/teamchat/member/agent`, SPEC-n3 6.1)
is the one place that branches on it: `agent.For` gives the run's `Adapter`, whose
`Capabilities` the link asks (delivery, whether a SessionStart is awaited) and whose `Preparer`
the keeper asks (what follows CMD). A second agent (opencode, Plan 00215) is then its constant,
its case in `For`, its adapter and one entry in `state.Launches`, not a change to every
comparison.

**Why it exists.** Before the seam the keeper (`prepare.go`, `loop.go`, `seats.go`) and the link
(`link.go`) compared the launch value wherever their behaviour differed by agent, so a new agent
meant finding and changing each of them.

**What it flags**, in the non-test files of `internal/teamchat`, `cmd/teamchat` and
`cmd/teamchat-admin` and below, outside package `agent` and the `agentBranches` table of
`src-go/qa/rules/table.go` (the two commands hold no launch value today, so they are green; they
are in scope so that one cannot begin to branch there):

- a use (a `types.Info` use, so an unqualified one in the declaring package counts; a
  declaration is not one) of `state.LaunchClaude`, `state.LaunchPlain`, `state.Launches`,
  `cli.LaunchClaude`, `cli.LaunchPlain`, `agent.Claude` or `agent.Plain`;
- an `==` or `!=`, a `switch` tag or a map key type whose type is `agent.Agent` (through any
  alias, so `cli.Mode` too);
- an `==` or `!=`, a call, an index expression, or a `switch` tag and its cases, whose operands
  hold both a launch operand and a launch constant. The operand is the field
  `wire.Launch.Launch` or `state.SessionPID.Launch` (also reached through an embedded struct)
  or a conversion to a string of an `agent.Agent`, directly or as an argument of a call that is
  an operand, through any depth of calls (`strings.ToLower(l.Launch)`). The constant is a
  constant expression whose value is `"claude"` or `"plain"`, or a list split on `,`, `|` or
  `;` with one as a word (`"plain,x"`); among a call's arguments and as an index's map, a
  composite literal holding one counts too (`strings.EqualFold(l.Launch, "plain")`,
  `slices.Contains([]string{"claude", "plain"}, l.Launch)`,
  `map[string]bool{"plain": true}[l.Launch]`).

The rows, each with its reason: state's package-level declarations (the one list),
`SessionPID.check` and wire's `Launch.check` (the two checks of a value read from a file or a
frame), and cli's package-level declarations (its names for the values), `ParseLaunch`
(`--resume-recorded` refused on plain) and `Launch.fromFlags` (the spelling). CL2's argument
rule, `Launch.checkClaudeCmd`, compares no launch value and has no row.

**How to fix a violation.** Ask the run's `agent.Adapter`: the keeper keeps the one it resolves
at the start of `prepare`, the link the `Capabilities` it resolves in `build`. Behaviour that
differs by agent is a field of `Capabilities` or work of a `Preparer`.

- Red, fixture: `testdata/src/fix/internal/teamchat/member/launchuse` (`bad.go`: the keeper's
  session.pid value, plain early return and relaunch flag, and the link's delivery; `evade.go`:
  a local copy of a constant, a named string constant, the operands reversed and parenthesised,
  a switch on the field, a contains over `state.Launches` or a literal list, `string(a) ==`, a
  switch on an `Agent` and on its string, a map keyed by one, the owner's constants, another
  `Agent` compared, an embedded field; `widened.go`: `strings.EqualFold`, `Contains` over a
  one-string list, `HasSuffix` and `HasPrefix` with a full value, a map literal indexed by the
  field, a switch on `strings.ToLower` of it, a comparison through `TrimSpace`, a call over a
  call, and the two declared gaps below unflagged; `good.go`: the adapter asked, the value spelled and
  resolved, another field compared with the same strings, Claude Code's argument found by its
  name, clean; `bad_test.go`, clean), beside stand-ins of `agent` (the owner, clean), `state`,
  `wire` and `cli` (each row clean and a function beside it flagged), and
  `fix/internal/elsewhereagent` (outside teamchat, clean), and `fix/cmd/teamchat` and
  `fix/cmd/teamchat-admin` (the commands, each comparison flagged). The test runs the real
  `agentBranches` table. Before the widening the new forms and both commands were missed, every
  one (Plan 00209 `subagent-reports/261023-n3e-seam-widen.md`).
- Red, real tree, at the rule's own commit `822931505` (untagged, `busfailpoints` and `tuwunel` alike): twelve
  findings, every site of SPEC-n3 AS1a: `keeper/prepare.go:439-441` and `:458`,
  `keeper/loop.go:288-290`, `:482` and `:539`, `keeper/seats.go:122`, and `link/link.go:268` and
  `:423`. The widened rule gives the same twelve over the tree before the move (`8d2496eba`),
  untagged and with `busfailpoints`, and is green over the tree after it and over the two
  commands, with no finding on CL2's `filepath.Base(a) == "claude"`.
- Green: the keeper writes `string(Mode)` to session.pid and the launch frame, and asks its
  adapter's `Preparer` for what follows CMD and its `Capabilities` for `launch_failed` and
  `needs_relaunch`; the link takes its delivery and its first SessionStart from the
  `Capabilities`; session.pid's and the launch frame's checks read `state.Launches`. The launch
  goldens (`keeper/launch_golden_test.go`) pass unchanged.
- Not caught: a value that reaches the field through a function's result (`isPlain(l.Launch)`
  with a bare literal inside `isPlain`) is caught only when that function names a constant; a
  copy of the field held in a variable and then compared; a list or map of launch values held
  in a variable other than `state.Launches`; a part of a launch value
  (`strings.HasPrefix(l.Launch, "pla")`); a launch value inside a sentence, which is not a list
  (`"launch is not claude or plain"`).

**The next wider rule, not built:** either of two. One follows a copy of the field through
dataflow (`x := l.Launch` and later `x == "plain"`, or the field handed to a helper that
compares it): that is an SSA pass over every function in teamchat, for a shape the independent
search found nowhere, and the seam leaves only plain copies of the value (`string(Mode)` into
session.pid and the frame). The other flags every comparison of any string field with a
literal in teamchat: it would report every `Kind`, `Inbound` and `Desired` check, so the
`agentBranches` table would become a list of unrelated exceptions and stop saying where the
agent is branched on.

## Defect classes that cannot be a rule

A class is listed here, with what holds it instead, when no analyzer can judge it without
guessing; the catalogue claims no more than the code does.

- **A readiness probe read as a verdict on something it does not test** (the `sudo -n -v` probe,
  fixed in `internal/sudo`: a failing `-n -v` was read as "a password is needed", but `-v` fails
  on a host where only some sudoers entries are NOPASSWD while the commands that follow run
  unprompted). What an exit status means is a fact about one program on one host, not a shape in
  the syntax tree: a rule could pin that `-n -v` is not used, and would then say nothing about
  the next probe read wrongly. What holds it: `project_seam_commands` keeps every `sudo` argv in
  `internal/sudo`, so the meaning is decided in one place; that package's tests run the world
  where `-n -v` fails and `-n true` passes (no prompt, no `sudo -v`) and the world where `-n -v`
  passes (no second probe); and the freeze acceptance on the host (`acceptance.bash`) is the only
  check against a real sudoers file. A known gap: a passing `sudo -n true` shows that `true` runs
  unprompted, not the commands after it, so a host whose NOPASSWD entries cover `true` but not
  the tools would skip the prompt and then fail.
  A second instance, fixed in `teams/create` (Plan 00201): `ccy-teams create` read any failing
  `gh api users/<owner>` or `gh api repos/<repo>` as "GitHub has no such name", so no network,
  bad credentials or a rate limit asked the person for another name. Only gh's `(HTTP 404)` is
  not found now (`ghNotFound`); any other failure ends the run with gh's own words. What holds
  it: the wizard's tests, which run a 404 against no network, a 401, a 403 and a 502.
  The Plan 00192 P6 ports found it again in both directions, in their bash and in review (the
  defect-class audit, `subagent-reports/261028-defect-classes-audit.md` of that plan): a probe
  that cannot tell no match from an error (`journalctl -g` exits 1 on no match, so nvidia-status's
  bash said "could not read the journal" on every clean boot, B2; `modinfo` failing for another
  reason read as "not built", B8), an error read as an answer (reclaim's failed `dnf repoquery`
  read as "no orphaned packages", B3; open's failing type detector discarded, B11; an unknown
  `mokutil --test-key` answer read as "not enrolled", B6), a success read as a verdict it does not
  give (ssh-copy-id's own check logged in with the login key and called every new key installed,
  ssh-with-key B11), and a timeout told by the error it does not return (`execx.OS` gives the
  SIGTERM's `ExitError`, not the deadline: nvidia-status F2). What holds them: each tool's tests
  of every answer the program can give (nvidia-status's parsers against real output and
  `TestTimeoutIsNamedNotTheKill`, run on the real error shape; reclaim's B3 scenario; sshkey's
  probe tests with the new key alone), and the host checks, which run the real programs.

- **A declare.py refusal given the wrong kind** (the other half of `project_refusal_kind`, Plan
  00201): `check_lxc` and `derive_lxc` raised a clash with an entry already in the file as a
  usage error (64), as did a team the file does not declare present, and `set` gave 78 to a value
  another answer could fix; so a question that read the kind still asked again, or stopped where
  it should ask. Which refusals another answer can fix is a fact about what the question asks,
  not a shape in the syntax tree: semgrep runs over Python here, but no pattern can tell
  `UsageError` from `DeclareError` by meaning. What holds it: `tests/helpers/agent_bus/test_declare.py`
  pins the kind of each refusal (a value alone 64; the file's entries, an answer against them, a
  team not declared present, an entry refused as it stands by a rule that reads no key the
  change sets, 78; `declare.py`'s `_REFUSAL_READS` says which keys a rule reads beyond its own).

- **An add that refuses what is already there** (Plan 00201): `lxc-add` of a member the file
  declares as given was refused as a duplicate of itself, so a run after a failed play could never
  get on. Whether a second add of the same thing is "nothing to do" or a conflict depends on what
  the entry means, not on a shape: an analyzer could pin that an add prints UNCHANGED somewhere,
  and would then say nothing about one that prints it on the wrong comparison. What holds it:
  `test_declare.py` (`LxcAlreadyDeclaredTest`: identical is UNCHANGED, differing is 78 naming the
  keys, `--update` replaces) and `lxcadd/declared_test.go`, which runs the real declare.py. The
  other adds were read: `create` of a team declared as asked is UNCHANGED, and its name question
  says `ccy-teams apply <team>` is the way on; `add-repo` of a repository the team trusts is
  UNCHANGED; `drop-limit` and `admin.py add_member` are not adds of the same entry.

- **A state machine that accepts one final state and silently waits on the others** (issue
  #107): `stt` transcribed a recording only when its manifest said `COMPLETE`, and any other
  state, `INTERRUPTED` (a recording stopped early) among them, was "waiting" for ever. Which
  values of a state are final, and what "waiting" is, are facts about one producer's states and
  one program's result type, not a shape: an analyzer could flag `x != "CONST"` before a return,
  and would then flag every ordinary guard while missing a `switch` that lists one final case
  and defaults to the wait. What holds it: `internal/stt`'s `manifestStates`, which names every
  state as final or not and makes any other one a failure (S5, S7), and its tests of each
  (`TestAnInterruptedRecordingIsTranscribedAndSaysSo`,
  `TestARecordingStillBeingRecordedOrUploadedWaits`, `TestAnUnknownManifestStateIsAFailure`).
  The sweep read `internal/stt` for other final-state checks: the server poll in `transcribe`
  already names `running`, `done` and `failed` and fails on any other answer; the part and
  loose-file waits are on presence and age, not on a state.

- **A menu line offered as if it ran, where the screen it came from says it cannot** (Plan
  00177): outside a project folder the `ccy-teams` board said "Ctrl-N unavailable: ~ is not a
  project folder", and every team, person and member menu still offered "Start a ccy session in
  <team>, in this folder (Ctrl-N)"; the header and the menu decided availability in two places.
  The independent search found three more of the class in the same picker: Start on a team whose
  homeserver the heading lists as not up, Attach on a seat with no running session or open in
  another terminal, and Attach in the Running sessions view on a session open elsewhere. Whether
  an action can run is a run-time fact (the folder, a homeserver's state, tmux's clients), and
  which header line a menu line answers to is meaning, not a shape: a rule could pin one
  literal and would say nothing about the next. What holds it: each answer is decided once and
  both sides drawn from it (the picker frame's `newRefusal` for the header and every start line;
  `attachRefusal` and `sessionRefusal` read the pass `seats.AttachSeat` and
  `seats.AttachSession` refuse from), in the house wording "<label>: unavailable, <why>" that
  `ccy-sessions`' new-session row uses (the line stays, choosing it shows the refusal), and the
  picker's menu and sessions tests over every row kind. Read and left: settings' Discard and Save
  with nothing pending (no screen calls them unavailable; choosing one says there is nothing to
  do).

- **A person's decline reported as a failure** (Plan 00201 Task 2.29): a deliberate Stop at
  `ccy-teams lxc-add`'s "Update entry / Stop" exited 1, so the run read as failed and the picker
  said "(ccy-teams lxc-add failed, exit 1)". The sweep of every `Confirm`, `AskYes` and `Choose`
  in `internal/teams` found the same in lxc-add's review No and its apply No, `create`'s review
  No (from `ccy --teams` it ended the launch instead of going back to the list) and `remove`'s
  "Leave it running" and its No to taking the sessions off first. Each now exits 0, as a No
  already did in shutdown, restart, wind-down, `session` and the picker's offers; Esc stays the
  cancel (130). Read and left at 1: `remove`'s typed name that is not the team's (an answer that
  confirms nothing, refused), and the `cc --teams` consent decline (the launcher must not start;
  documented exit 1). Whether a No is a decline or a refusal the caller must stop on is meaning,
  not a shape: a rule could flag `if !yes { return ExitFailure }` and would need an excuse for
  every refusal kept. What holds it: the flows' tests of each decline (`lxcadd`, `create`, `ops`)
  and the exit-code line of `ccy-teams --help`.

- **An offer that starts or opens something, defaulting to Yes** (Plan 00201 decision A12):
  after `ccy-teams create`, "Sign <person> in to Element now?", "Open <team> in Element now?"
  and "Start a ccy session in <checkout> as a member of <team>?" started on Yes, so a stray
  Enter set a password, opened Element or replaced the terminal with ccy. The sweep of every
  `Confirm`, `AskYes` and `askYes` in `src-go` found the same in the sign-in offer after an
  apply (`login`), "Open <team> in Element now?" after a new password (`login`), and the
  picker's "Start a ccy session in <checkout>?" and "Update them now (play-agent-bus.yml)?"
  (`picker`). Each now starts on No, as `ccy-sessions`' "Start ccy in ...?" and the picker's
  "Create a team for ... now?" already did. Read and left on Yes: the review question of the
  command the person ran (`create`'s "Create team", `lxc-add`'s "Add" and "Apply"), settings'
  "Save and apply" with nothing removed, the Element profile set up on the way to an Open the
  person chose, the new password (no sessions to end) and its "Copy the password?" in the
  sign-in the person chose, and take-down's "Back <team> up first?" and "Take <sessions> off <team> first?",
  whose Yes is the safe way through the take-down chosen. Outside `ccy-teams` the yes/no
  questions of `reclaim`, `nord`, `setup-rclone` and the ccy build lock already start on No;
  `cc-project-dir`'s "Start cc in the fedora-desktop checkout instead? [Y/n]" (where to start
  the cc the person ran) and `ccc`'s "Try signing in again? [Y/n]" (the sign-in the person ran)
  are the step chosen, and stay. Whether a question is an offer of
  something further or the step the person came to take is meaning, not a shape: a rule on a
  constant `true` default would flag the safe "Back up first?" and need an excuse for every
  review question, and the call cannot see what a Yes goes on to start. What holds it: each
  offer's test pins `--default=false` or the picker's `--bind=start:last` (`create`, `login`,
  `picker`), and `ui.Picker.Confirm`'s doc (Exit unless a harmless step the person came to take).

- **A terminal left echoing between two screens** (Plan 00177): fzf, gum and Bubble Tea take the
  terminal raw and give back the mode they found; between two screens (the board collecting the
  machine after Esc from a row's menu) the line discipline echoed typed-ahead arrows as `^[[B` on
  the next screen's top line. No first-party code owned the mode between screens. The mode is
  run-time state shared across processes, and which runner reaches a screen is set through
  fields in other packages, so no syntax-tree shape says whether a command holds it. Two narrow
  rules were weighed and not built. "An `execx.OS{}` literal in a package that builds a
  `ui.Picker` or `ui.Gum`, not handed straight to `Hold.Runner`" flags the fill-in defaults of
  the wiring functions (`host.Wire`, `sessions` `wire`), which only run where a test or a
  constructor has already set the runner, and misses a runner that reaches a screen from another
  package (every `ccy-teams` flow takes `host.Host`'s); excusing the fill-ins by table would leave
  the rule guarding the constructors the tests below already pin. "An fzf or gum `execx.Cmd`
  without `EchoHeld`" restates `internal/ui`'s test: `project_seam_commands` already lets no other
  package run fzf or gum.
  What holds it: `internal/ttyecho`, a hold on the echo taken at a command's first screen and
  put back when its run ends, a panic included (a run with no screen, such as a listing piped to
  a pager, never touches the terminal; a child given the terminal for its own input runs in the
  mode found; the launcher exec and SIGTERM/SIGHUP put the mode back), `execx.Cmd.EchoHeld` on every program `internal/ui` runs
  (its test), a test of each production constructor that its runner is under the hold, and a
  real pseudo-terminal test that keys typed while held are not echoed and are still read. Held:
  `ccy-teams` (every subcommand), `ccy-sessions`, `podfreeze`, `lxcfreeze`, `open` and
  `ftp-camera`, each with its numbered or y/N reply read through `Hold.Given` (echo on). Read and
  left: a screen followed only by a prompt that reads a typed line (the bash launcher's
  `ccy_tmux_pick` then `read -rp`), where an echo is what the person needs; `ccy-teams`' "Press
  Enter to go back to the list", read held, since it waits for Enter alone.
  Plan 00192's ssh-with-key review found the same class (U6: keys typed between the key menu and
  ssh are read into the menu's buffer and lost); that family does not run under the hold yet.

The classes below were found by Plan 00192's audit of the P6 ports (reclaim, open, nord,
nvidia-status, the ssh-with-key family, and vmtest's prune-runs, retention and sweep), in the
bash they replaced and in the Go during review; each instance is in the audit report
(`subagent-reports/261028-defect-classes-audit.md` of that plan).

- **A secret reaching an argv, an output or a log** (nord: the bash passed the VPN password to
  `nmcli connection modify` in argv, visible in `/proc/<pid>/cmdline`, B3, and its
  troubleshooting said to `cat` the credentials file, B11; nmcli may echo a fed line). Which
  string is a secret is meaning: no shape tells a password from any other argument, and gosec's
  G101 sees only credentials written in the source. What holds it: `execx.Secret` and
  `execx.Feed`, which refuse at run time a command whose argv holds the value (`checkSecrets`)
  and print as its name only; nord's `creds.hide` over nmcli's output; and the tests that check
  every recorded argv, both streams and the log for the password (`wantNoPassword`).
- **Output on the wrong stream, or terminal bytes into a pipe** (open's `--list` on stderr, B12;
  nvidia-status's `--check` printing lines, B3, and colour escapes written into a pipe, B10).
  Which text is a command's payload is meaning ([StderrHygiene.md](StderrHygiene.md)). What
  holds it: each tool's tests assert stdout and stderr apart, colour is drawn only when
  `IsTerminal` and no `NO_COLOR`, and the reviews read every print against the rule.
- **A removal wider than the action means** (vmtest's sweep removed whole passing-run
  directories, with the transcripts plans cite as evidence, where only the overlay frees disk,
  B19; it removed the disk of a fast-base build still running, inferring "failed" from the
  directory's existence; reclaim's bash pruned every unused tagged image with `-af`, B16, and
  cleared through a symlinked `~/.cache`, B13; nord's `cleanup` removed every profile unasked,
  B18). What may be removed is meaning. A rule that every `os.RemoveAll` carry a table row with
  its reason was weighed and not built: the module has 15, and the sweep's whole-directory
  removal would have got its row from the same belief that was wrong. What holds it: the tests
  that name what must survive (`TestE51Sweep`, `TestE51SweepKeepsALiveBuild`,
  `TestE51SymlinksAreLeft`, reclaim's prune and clear tests), the specs (SPEC-vmtest E51, E61;
  SPEC-reclaim E22, E24), and a default-no question before every destructive action
  ([InteractiveScripts.md](InteractiveScripts.md) rule 6).
- **Ordering or ageing by a value the code itself changes** (the sweep ordered runs by their
  directory's mtime, which a removal inside moves, so a run it had just evicted read as the
  newest). Whether a `ModTime` belongs to a directory, and whether anything changes it, is not
  in the syntax tree; the search over the module found no other directory's mtime ordered by
  (`transcript` and `ftpcamera` read regular files). What holds it: the sweep orders by the
  start time in the run id, and `TestE51Sweep` gives the newest run the oldest mtime.
- **A live thing taken as a leftover** (the sweep and the build directory above; a run
  `prune-runs` must not touch while it boots). Whether a process holds something is a run-time
  fact. What holds it: libvirt's listing and the run's `run.lock`, both read before a removal
  (SPEC-vmtest E61, E63), and the tests of a live run and a running build.
- **A record that says done before it is** (`retention.log` said "evicted" before a removal that
  then failed, and a partial `prune-runs --yes` named nothing it had removed; a Ctrl-C landing
  after nord's connect turned a success into `Cancelled.`, U5). Which order is true is meaning.
  What holds it: `TestE62PruneRunsRemovalFails` and `TestE51SweepRemovalFails` (the log's
  `failed to remove` line and the "removed so far" list).
- **An interrupt that kills a change half made, or is misread** (nord's and reclaim's bash died
  on Ctrl-C with nmcli or dnf still changing the machine, nord B16 and F2, reclaim B15 and M-1;
  open reported a handler ended by Ctrl-C as `exited -1`, B20). Which command changes state and
  must finish is meaning. What holds it: changing commands run under `context.WithoutCancel`
  and, where the terminal's SIGINT would reach them, in their own session (`execx` `NewSession`);
  each tool's cancel tests, and reclaim's `TestCtrlCEndsTheRun`, which sends a real SIGINT.
- **A value another program re-parses, written or read differently from how it reads it**
  (nmcli splits `vpn.secrets` at `,` and trims, so a password with a comma was stored as half,
  nord B17; a path starting `-` reached `jq` as an option, open B16; the bash read ssh's options
  with a wrong list of argument letters, ssh-with-key B3 and B4; `IdentityFile` tokens and
  `none`, U1 and U2; another spelling of a forwarded agent, B12). Each depends on one program's
  grammar. What holds it: the value refused before it reaches the program (`readCredentials`),
  the `./` prefix, the tests against the program's own option strings (scp's getopt string), and
  the host checks.
- **A structured value matched as loose text** (`lsmod | grep "^nvidia"` counted `nvidia_drm`,
  nvidia-status B5; `grep -F | head -1` picked an older profile, nord B8; any `://` read as a URL
  and a desktop ID matched at any depth, open B15 and B3; a package asked by its old name,
  nvidia-status B1). What a field is, is meaning. What holds it: parsers that split the fields
  the program prints, and tests of the near-miss names (`nvidia_drm`, two profiles of one name).
- **A terminal screen judged by its bytes, not by what it shows** (fedora-desktop#117:
  `screen.inputLineEmpty` read any non-SGR escape, or a placeholder whose first character
  carries the inverse-video cursor, as typed text, so a visually empty box was left alone as
  `input-box-not-empty` and `verify-restore` failed for ever). Whether a byte draws something
  is a fact about the terminal, not a shape in the syntax tree; and the placeholder test must
  read the raw bytes, so a rule against byte tests on a capture would forbid the fix. What holds
  it: one reader of captures, `internal/screen` (every `CapturePane(…, true)` caller, in
  `sessions`, `teams/shutdown` and `teams/winddown`, reads it only through `Plain` and
  `ClaudeState`), whose tests run each escape shape both empty and with typed text after it;
  and set-going prints a typed box line `%q`-quoted, so the next misreading arrives with its
  evidence.
- **A failure dropped from the record a later verdict reads, so the verdict reports success**
  (fedora-desktop#108: `ccy-sessions restore` counted a record it could not start as a failure
  but left it out of the restore manifest, so a boot where every record failed wrote an empty
  manifest, `verify-restore` said "nothing to verify" and exited 0, and the self-update cycle
  announced the deploy as done). Which record a later verdict reads, and which branch of a
  loop is a failure that verdict must see, are facts about two programs and a file between
  them, not a shape in one function's syntax tree: a rule against "a counter bumped without an
  append" would flag every deliberate skip (`marked no-restore`, an opencode session) and miss
  a failure counted nowhere. What holds it: every refusal of the restore is one call,
  `restoreRun.notStarted`, which counts it and writes it into the manifest as `not-started`;
  `restore.Judge` reports that as `NOT-STARTED` before anything else; and the tests run the
  restore and then `verify-restore` over the same state
  (`TestARestoreWhereEveryRecordFailsIsReportedByVerify`). The sweep read `internal/sessions`
  and `internal/restore` and the cycle (`helpers/self_update/cycle.py`): the four refusals in
  `restoreRun.record` and the unreadable records were the instances, all fixed; set-going
  records every session it leaves alone (`untouched`, which verify reports `NOT-SET-GOING`),
  verify turns a probe it cannot make into a failure, never a state, and the cycle records each
  outcome before it announces it. One case is left as designed: a second restore in the same
  boot that starts nothing at all (its passphrase file unusable, the live list unreadable, the
  manifest unwritable) fails its unit and leaves the first restore's manifest, which still
  names every session that should be up.
- **A fake that answers what the real program does not** (reclaim's fake answered any unscripted
  command, M-2; nvidia-status's fake returned the deadline where `execx.OS` returns the kill,
  F2; nord's fake cancelled a context where a terminal delivers SIGINT, F2; ssh-copy-id's check
  was assumed, not read, B11). Whether a fake is faithful is a fact about the real program.
  What holds it: fakes that fail on an unscripted command (`execx.ErrUnexpected`), tests run
  on the real error shape, and each port's host checks (`H*`), which run the real programs.
- **A verdict that names the wrong next step** (nvidia-status told a person to enrol a key with
  Secure Boot off, B14, called a driver/library mismatch "incomplete" instead of a reboot, B15,
  and sent a person to the blue screen with nothing queued, B19; open's bash advised `--ask`
  where there is no terminal, B17; hints of `sudo dnf install` instead of the play, nord B7,
  nvidia-status B12). Which step is right is meaning. What holds it: the verdict tests
  (`TestVerdictOrder`, `TestMokKey`) and the review of every message against the play.
- **A command with no bound, or a resource never given back** (nvidia-status's bash waited for
  ever on a hung `nvidia-smi`, B11; vmtest kept every failed run's overlay for ever, about 108G
  on the host; ssh-with-key's bash left an agent holding forwarded keys when killed, B7). Which
  command may hang, and what must be reclaimed when, is design. What holds it: fixed bounds on
  each probe (and `project_realtime_bound` for a bound a test could shorten), `prune-runs` and
  the disk guard (SPEC-vmtest E61, E64), and ssh-agent's command mode, which ends with the run.
- **Mechanical, but one site** (statfs's free space computed with `Bsize` where POSIX counts
  `Bavail` in `Frsize` units, vmtest F7). A rule is possible (any `Statfs_t.Bsize` in a product)
  and would guard one function: `freeOf` is the module's only free-space reading, and
  `TestFreeOfCountsFragments` (Bsize 4096, Frsize 1024) holds it.
- **A test that reads state a live goroutine of the code under test may still change** (Plan
  00209 n3e: `TestBeginRunReturnsAnEarlierRunsEntries` read the store after `BeginRun` woke a
  delivery loop able to hand the items out; three more in `wiring` and `keeper`, where the test
  waited on a log line or a client's exit that comes before the effect it then read). Whether
  the read races depends on what is live at that moment (session facts told, a roster verified,
  items deliverable, a timer handed to another goroutine), which no syntax tree shows: every
  shape a rule could match also matches the many reads that are ordered. What holds it instead:
  a test waits on the effect itself (`until`, `eventually`, a failpoint hook that parks the loop
  while the test reads), never on a signal the code gives before the effect; and the race only
  shows under load, so a suspect test is run shuffled, `-cpu 1,2,4`, many times, beside a loop
  of other tests.
- **A fake that announces an event before it has done what the event stands for** (Plan 00209
  n3e: `fakeinbox` recorded a refused auth frame, which wakes `Await`, before its deferred
  `drop` closed the connection, so `inbox`'s `TestAWrongTokenIsRetryAndNothingIsWritten` could
  write into a connection still open; it also woke `Await` before `OnAccepted` handed the frame
  on, and the keeper harness's fake child and pidfd sent a SIGKILL to the test before they
  exited). Which action an announcement stands for is a fact of the fake's contract, not a shape
  in the syntax tree: the action may be a defer, a callback or the next statement, and the same
  shapes order an announcement after its action everywhere else. What holds it instead: each
  fake announces last (`fakeinbox`'s `serve` records a delivery's last frame after the close,
  `record` hands on before it wakes `Await`; the keeper fakes exit before they send the
  signal), and its own tests pin the order: `TestTheLastFrameIsRecordedOnceTheConnectionIsClosed`
  (the connection's `Close` reads the frames), `TestAFrameIsHandedOnBeforeItIsRecorded` and
  `TestAFakeSIGKILLExitsBeforeItIsSeen`. A new fake's announcement is placed after its action
  and tested the same way.

Classes the bundled linters already hold, each seen firing on a minimal case in the audit: an
error or status thrown away (the bash's `|| true`, unchecked process substitutions and
`2>/dev/null`: nord B3, B11, reclaim B3, B6, open B5, B10, B11) is `errcheck`'s and `nilerr`'s in
Go, and the hooks' `R-ERROR-HIDING`; a path joined from a name the user gave (nord's
`connect ../x`, B2) is gosec's G304, and nord matches the name against the listing instead; a
shell template (open's `bash -c` handlers, B8) is `project_seam_commands`' `sh -c` finding.

## Control

`scripts/qa-go.bash` was run with `strings.HasPrefix(p, "~")` planted in a package outside
`internal/term`: every earlier step passed and the gate failed at the rules step with a
`project_homeabbrev` finding. The plant was removed. After the rules were hardened a test file
with `var now = time.Now`, `"~" + rest` and `f := exec.Command` was planted in a package and all
three were reported; it was removed.

After the program runners were added, `sudo.Capture(ctx, "nmcli", "device")` was planted in
`internal/freeze`: the rules step reported `"nmcli" is owned by internal/lxcfreeze`. It was removed.

`project_hidden_bindings` was red through `scripts/qa-go.bash` at its own commit (`8022a152`:
`internal/ui/picker.go:105:23: fzf --expect outside the ui Action builder`) and through
`scripts/qa-hidden-bindings.bash` at `9e40ea6e` (10 findings in shell and Python), and green
at the end of the remediation. A planted `"--bind=ctrl-a:accept"` (since then counted as
Enter) in a package outside the rules package was reported by the rules step, and a planted `fzf --expect=ctrl-z` in a tracked
shell script by the gate; both were removed.

Its Bubble Tea clauses (Plan 00204 Task 2.3) were red at `727810c8` (`TestHiddenBindings`: 10
wants unmet over the fixtures `fix/internal/ui/teakeys.go` and `fix/internal/badtea`, with
stubs of Bubble Tea and Bubbles key under `testdata`) and green at the next commit; the
fixture `fix/internal/ui/frame` proves a ui subpackage may read keys. The independent search
made before the rule found no instance, and the sweep over both modules found none. The
evasions Fable's review and the DBF conformance review found (a key read held in a variable, a
switch on one, `strings.HasPrefix`/`Contains` on one, a `key.Binding{}` literal, Tab by hand
with no Tab binding) were red at `f66868db` (7 wants unmet, fixtures
`fix/internal/ui/evasions.go` and `fix/internal/ui/notab`) and green at the next commit; the
sweep again found none.
