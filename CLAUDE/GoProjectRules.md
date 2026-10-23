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

| Rule                         | Defect class                                                                                                                                           | Found in                                                                                  |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------- |
| `project_seam_exec`          | a process started outside the runner, so no fake sees it                                                                                               | `teams`: `syscall.Exec` in the launcher hand-over                                         |
| `project_seam_commands`      | a program run outside the package that owns it, or a command the rule cannot judge (run-time name, hidden wrapper argv, shell `-c`)                    | `sessions`: `systemctl reboot`; `teams`: the take-down heading via `gum style`            |
| `project_tmux_targets`       | a tmux `-t` target in the wrong form for its command (fedora-desktop#69)                                                                               | the bash library, before the port                                                         |
| `project_testclock`          | wall-clock reads in tests (date-dependent tests failed in the hour after midnight)                                                                     | `transcript` and `sessprobe` tests: 23 `time.Now()` fixtures                              |
| `project_waitdelay`          | a function that sets `exec.Cmd.WaitDelay` and never names `exec.ErrWaitDelay`, so a finished command's output is dropped                               | `qa/internal/proc`: bug B25 of the qa-all port                                            |
| `project_package_tests`      | a package with no `_test.go` file, whose code the module-wide coverage floor can hide behind the rest of the module                                    | the old Go gate's every-package-has-tests check, kept when go-qa-ci replaced it           |
| `project_homeabbrev`         | a hand-rolled `~` home abbreviation (a bare prefix replace turned `/home/<user>X` into `~X`)                                                           | `teams` tests: `strings.Replace(..., "~", 1)` twice                                       |
| `project_hidden_bindings`    | an action reachable only by a key, with no visible, named choice for it (also in shell fzf and Python Textual: `qa-hidden-bindings.bash`)              | `teams` picker: Ctrl-A/X/O on Running sessions; `ccy-sessions`: Ctrl-T/X/N                |
| `project_lifecycle_turn`     | a member's lifecycle notice decided from a read of main and posted outside the team's turn, so another post of the run can come between                | teamchat `ends`: `back` posted after the run's own `disconnected` (Plan 00209)            |
| `project_keeper_log_barrier` | a keeper test reading keeper.log past the harness's turn-aware reader, so a wait on a line releases before its turn has done the rest                  | teamchat `keeper`: `TestOwedNotice` saw the seat held after its line (Plan 00209)         |
| `project_fakeclock_count`    | a test that judges a fake clock's timers by the number armed, which a timer it did not mean changes: a wait advances too early, an assertion lies      | `link` tests: `TestAnOpBeforeThePIDIsNotAnswered` hung; `engine` TIMEOUT tests at a merge |
| `project_goroutine_end`      | a goroutine that signals no end, so no owner can wait for it, and a stop missed on one path leaks it for the life of the process                       | `link`: `go l.read()`, about 145 control readers left blocked in one test binary          |
| `project_rawconn_probe`      | a RawConn readiness callback that answers without testing its descriptor, so readiness the poller saw and Read reset is missed and the wait never ends | `link`: the pidfd watch never saw a keeper that died before the watch first ran           |
| `project_leakcheck_once`     | a second `leakcheck.Check` on one test, which judges before the cleanups registered between the two run and reports a world they stop as a leak        | `engine`: `TestLiveNeedsEveryFact`'s second world reported the first's transcript watcher |

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
- Not caught: a fake whose advance is spelled otherwise; two timers due at the same instant
  are one to `AwaitDue` and `Armed`; an allowance row whose test later shares its clock with
  other timers (the row's reason says why its clock has one user).
- Next wider rule, not built: a timer named by its due time must be the only waiter due then
  (`AwaitDue` and `Armed` refusing an ambiguous due). That is a fact of a run, not a shape in
  the syntax tree, so it belongs in the fake, not in an analyzer; the independent search found
  no two timers sharing a due in any test (it did not check each of about 25 `AwaitDue`
  calls), and it is referred to the owner (Plan 00209 n3e merge report `261023-n3e-merge-n3b.md`).

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
only). The class is a test barrier released partway through the work it waits for. A loop turn
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
