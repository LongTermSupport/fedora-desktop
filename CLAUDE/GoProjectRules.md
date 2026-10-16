# src-go project rules (Defence Before Fix)

Defect classes the `ccy-teams` and `ccy-sessions` ports actually hit, each held by a
`go/analysis` analyzer in `src-go/qa/rules`. They run from `scripts/qa-go.bash` as a hard
step: the driver (`src-go/qa/driver`) is built once and used as `go vet -vettool` over
`src-go`. Every finding fails the gate. There is no inline suppression: the only excuse is a
row in a table in `src-go/qa/rules/table.go`, with its reason.

**A new defect class found in `src-go` gets a rule here before its fix.** Write the rule, prove
it red on a fixture reproducing the defect (`src-go/qa/rules/testdata/src`, run by
`analysistest`), then green on the real tree, fixing every real instance it finds. A rule is
also red-tested against the ways round it: a rule that holds only against the original
spelling of the defect is documentation, not a gate.

The rules live in their own module (`src-go/qa`, its own `go.mod`, depending on
`golang.org/x/tools`) because the shipped binaries are built on the host with plain `go build`
and `src-go/go.mod` must stay dependency-free. `qa-go.bash` checks that module too (format,
vet, test, coverage, lint). This file is under `CLAUDE/` because the markdown guard allows no
`.md` beside the Go code.

## Fit with go-qa-ci

go-qa-ci documents project analyzers as a project driver package, analyzers proven with
`analysistest`, and one page per analyzer under its `docs_dir`. The analyzers and their
`analysistest` proofs match, and the names are identifier-only (`project_*`), so adopting it is
a change of driver (`driver.Main(rules.All()...)` in place of `unitchecker.Main`). Gaps, because
of the dependency-free constraint on `src-go/go.mod`: go-qa-ci expects the driver in the
project's own module (`[analyzers] driver`) and here it sits in the nested `src-go/qa` module,
which its per-module run (`modules` in `qa/go-qa.toml`) must be told about; and the per-rule
pages are not written, this catalogue stands in until adoption.

## The rules

| Rule                      | Defect class                                                                                                                              | Found in                                                                       |
| ------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| `project_seam_exec`       | a process started outside the runner, so no fake sees it                                                                                  | `teams`: `syscall.Exec` in the launcher hand-over                              |
| `project_seam_commands`   | a program run outside the package that owns it, or a command the rule cannot judge (run-time name, hidden wrapper argv, shell `-c`)       | `sessions`: `systemctl reboot`; `teams`: the take-down heading via `gum style` |
| `project_tmux_targets`    | a tmux `-t` target in the wrong form for its command (fedora-desktop#69)                                                                  | the bash library, before the port                                              |
| `project_testclock`       | wall-clock reads in tests (date-dependent tests failed in the hour after midnight)                                                        | `transcript` and `sessprobe` tests: 23 `time.Now()` fixtures                   |
| `project_waitdelay`       | a function that sets `exec.Cmd.WaitDelay` and never names `exec.ErrWaitDelay`, so a finished command's output is dropped                  | `qa/internal/proc`: bug B25 of the qa-all port                                 |
| `project_homeabbrev`      | a hand-rolled `~` home abbreviation (a bare prefix replace turned `/home/<user>X` into `~X`)                                              | `teams` tests: `strings.Replace(..., "~", 1)` twice                            |
| `project_hidden_bindings` | an action reachable only by a key, with no visible, named choice for it (also in shell fzf and Python Textual: `qa-hidden-bindings.bash`) | `teams` picker: Ctrl-A/X/O on Running sessions; `ccy-sessions`: Ctrl-T/X/N     |

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
  and `busctl` by `internal/systemd`, `git` by `internal/gitx`, `ps` and `docker` by
  `internal/sessprobe`, `podman` by `internal/sessprobe` (a session's container) and
  `internal/podfreeze` (pause, unpause, networks: a table entry may list several owners), and the
  `lxc-*` tools by `internal/lxcfreeze`, and `nmcli` by `internal/lxcfreeze` (inside a
  container) and `internal/ftpcamera` (the hotspot). Any other program (`wl-copy`, `gh`, `id`, `tput`, `systemd-escape`,
  `python3`, ...) is unowned and free. The owner's own program is trusted: what the owner puts
  after it is not examined here, except behind `lxc-attach`, a wrapper that stays judged even for
  its owner (`lxc-attach -n X -- prog` is judged as `prog`, so `nmcli` is lxcfreeze's and `git`
  inside a container is still a finding).
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
  the `sh -c` route round the argv the runner shows.

`LookPath("gum")` and text containing a program's name are not commands and are not flagged.

- Red, fixture: `testdata/src/fix/internal/badcmd` (every form above flagged, with the clean
  neighbours: `wl-copy`, `systemd-escape`, `env PYTHONPATH=... python3`,
  `systemd-run -- setsid gio`, `bash script.bash`; the `_test.go` file clean), the owner
  `sudo`, `other/internal/tmuxccy` (the owner running tmux, directly and under `systemd-run`,
  clean; running `git` flagged), `dynamic` (a `dynamicNames` row clean, its neighbour flagged),
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
initialiser. Excuse: a `clockAllowances` row (package, test function, reason). One row exists,
`TestTakeOverDefaultsToTheRealClockAndSleep`, which proves the default `Sleep` really sleeps and
can only do that against the wall clock. A package-level declaration cannot be excused.

- Red, fixture: `testdata/src/fix/internal/badclock` (the three calls; the function value,
  argument, field, package-level `var` and closure forms in `evade_test.go`; `time.Date` and
  production code clean) and `allowedclock` (the allowed function clean, its neighbour flagged).
- Red, real tree: 23 findings in `transcript_test.go` and `sessprobe/join_test.go`.
- Green: fixed instants (`fixedMtime`, `sampledAt`).
- Not caught: a clock read in a non-test helper; production takes its clock through a seam.

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
are shown (sort order, preview, wrapping), and leaving (Esc, q, abort) beside the visible Exit
row; Enter, Ctrl-M and a double click accepting the highlighted row choose the visible row.
fzf events (`start`, `focus`, `load`, `change`, ...) are not keys. In Textual, an action named
`focus*`, `scroll*` or `cursor*` moves focus, the cursor or the view.

**What it flags.**

- Go (the analyzer, every package but the rules package, test files exempt): `"--expect"` or
  `"--expect=..."` outside the one builder in `expectBuilders` (`ui.expectArg`, which takes its
  keys from `ui.Action` values); `"--bind=SPEC"`, or `"--bind"` followed by SPEC as the next
  argument, binding a key to anything but navigation; a binding it cannot read (`--bind` with
  no constant beside it, a SPEC built at run time, a run-time argument followed by text that
  closes it and adds a binding); and a `ui.Action` literal with a `Key` and no `Label`. The text
  is judged wherever it is spelt: a literal, a constant, a variable, an `append` or
  `slices.Concat` element, a `Sprintf` format, a folded concatenation.
- Shell (`qa-hidden-bindings.bash`, every tracked shell file that runs fzf, by name, path or
  behind `command`/`env`): every `--expect` (shell has no `ui.Action`), and `--bind` judged as
  above, in any command or array assignment of the file, since fzf argv is often built in an
  array.
- Python (`qa-hidden-bindings.bash`, every tracked Python file that imports textual): each
  `BINDINGS` entry (`Binding(...)` or a tuple) and each `.bind(key, action)` must be navigation
  or the action of a `Button(..., action=...)` in the same file; a binding or `BINDINGS` value
  that is not a literal is a finding.

Every finding names `project_hidden_bindings`; this section is what it resolves to.

**Narrowings, each with its sentence.** The rules package and the shell gate
(`qa/hiddenbindings`) name the flags as the text they look for and build no fzf argv (neither
starts fzf), so they are not judged (`bindingSubjects`; the fixtures `fix/qa/rules` and
`fix/qa/hiddenbindings` prove the exclusion reaches nothing else). A literal compared with argv (`==`, `!=`, a switch case, an argument of
`strings.HasPrefix`, `Contains`, `slices.Contains`, ...) reads argv something else built, so it
cannot become a binding (`teamtest/console.go`, the fake fzf, is the one real case). A
`_test.go` file is never built into a tool a person runs. A shell file that never runs fzf
builds no fzf argv (`--expect` is a flag of other programs too). The fixtures in
`tests/fixtures/hidden-bindings` carry the pattern on purpose and are not code.

**Calibration recorded here** (the project had none): the sweep scope is all first-party Go,
shell and Python git would publish, generated and vendored trees excluded (`scripts/qa-trees.tsv`),
plus the GNOME Shell extensions read by hand; display-only toggles (fzf's `toggle-sort`) count
as navigation.

- Red, fixture: `testdata/src/fix/internal/badbind` (`bad.go`: the originating Ctrl-A/X/O
  `--expect`, keys bound to `accept`, `execute`, `become`, `reload`, a chained `toggle+accept`,
  split `--expect`; navigation and events clean; `evade.go`: a constant, a variable, split
  `--bind` words with a literal, a run-time and no binding, `slices.Concat`, an appended
  literal slice, `Sprintf`, a run-time SPEC, a folded `"--exp" + "ect=..."`, a binding hidden
  after a run-time argument, a hidden key ahead of a run-time tail; comparisons clean),
  `fix/internal/ui` (the builder clean, a second `--expect` in ui flagged, `Action` literals
  with a key and no label, keyed, blank and positional, flagged) and `fix/qa/rules` (clean).
  The shell and Python halves are proven against `tests/fixtures/hidden-bindings/fixture.bash`
  and `fixture.py` on every gate run, and by `src-go/qa/hiddenbindings` and
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
  `FZF_DEFAULT_OPTS`, a binding assembled only from non-constant pieces, an action Go code runs
  on a key it reads itself (no first-party Go reads raw keys; there is no terminal library in
  `src-go`), an `Action` whose `Label` ui does not show (ui's tests hold that), single-key
  `read -n1` menus in bash (every one found prints its choices), and GNOME Shell keybindings
  (below).

**The next wider rule, not built:** every key a tool reads, by any means (`read -n1` in bash,
GNOME Shell `Main.wm.addKeybinding`, a raw terminal read), must map to an action that is also
a visible control. Not built because the key-to-control relation there is semantic (a printed
prompt naming the letters, a panel menu item doing the same thing) rather than a shape in the
syntax tree, and the search found no instance in a terminal tool. The GNOME speech-to-text
extension's global shortcuts (start, stop and abort a recording, with no panel menu item for
them) are the case it would decide; whether desktop-wide shortcuts are in the class is the
owner's decision, referred with this remediation.

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
at the end of the remediation. A planted `"--bind=ctrl-a:accept"` in a package outside the
rules package was reported by the rules step, and a planted `fzf --expect=ctrl-z` in a tracked
shell script by the gate; both were removed.
