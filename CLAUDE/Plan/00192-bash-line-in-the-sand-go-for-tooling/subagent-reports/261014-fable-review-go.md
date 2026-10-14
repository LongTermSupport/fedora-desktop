# Adversarial review: src-go (ccy-teams, ccy-sessions, shared internal packages, DBF rules)

Scope: `/workspace/src-go` at `da797a0b` (branch F44). Read-only. Scratch artefacts under
`/workspace/untracked/scratch/fable-go/` (built binaries, coverage profile, DBF evasion fixture).

Bar being judged: "well architected, DRY, TDD, strictly typed, high performance", "the system
is the deliverable". Verdict in one line: the port is solid, well-tested, fail-fast engineering,
but the Defence Before Fix rules do not defend (12 of 13 planted evasions pass), the "strictly
typed" claim is not met in the state-carrying code, and there are two real terminal/process
correctness risks the parity suites cannot see.

Facts established by running things:

- `go test -race -coverprofile` passes; total statement coverage 94.3% (`fable-go/cover.out`).
- Binaries build in 0.17 s; `--help` averages ~1 ms (20 runs); 4.1 MB / 4.5 MB.
- `golangci-lint` is **not installed in this container**, so `scripts/qa-go.bash` exits 2 here.
  By CLAUDE.md "Missing Dependencies" that is an IaC gap in the CCY image, not a note to skip.
- The DBF driver built from `src-go/qa` ran over `fable-go/evade/` (a scratch module with a
  copy of `execx.Cmd` and 13 evasions). It reported **one** finding (`time.Since`).

---

## 1. Blockers

### B1. The DBF rules are evaded by the first thing any author would do

Severity: **blocker** (the rules are the module's stated defence mechanism; a defence that
holds only against the literal spelling of the original defect is documentation, not a gate).

Evidence (fixture `fable-go/evade/internal/evade/e.go`, `e_test.go`; run
`cd fable-go/evade && go vet -vettool=../driver ./...`): not flagged were

| Evasion                                                                     | Rule                    | Why it passes                                                                                                                       |
| --------------------------------------------------------------------------- | ----------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `f := exec.Command; f("sh","-c",…)`                                         | `project_seam_exec`     | `qa/rules/table.go:90-102` `calledFunc` returns nil for a call through a value                                                      |
| `c := &exec.Cmd{Path:"/bin/sh"}; c.Run()`                                   | `project_seam_exec`     | `startsProcess` lists four functions; constructing `exec.Cmd` and calling `.Run/.Start` is not in it                                |
| `syscall.ForkExec(...)`                                                     | `project_seam_exec`     | not in `startsProcess` (`seamexec.go:11-16`)                                                                                        |
| `execx.Cmd{Name: gumName}` (package var)                                    | `project_seam_commands` | `seamcommands.go:89` requires a compile-time constant                                                                               |
| `execx.Cmd{Name: progName()}`                                               | `project_seam_commands` | same                                                                                                                                |
| `execx.Cmd{Name: "/usr/bin/gum"}`                                           | `project_seam_commands` | `commandOwners` keys are bare names; no `filepath.Base`                                                                             |
| `p,_ := exec.LookPath("tmux"); execx.Cmd{Name: p}`                          | `project_seam_commands` | LookPath is explicitly allowed and its result is a variable                                                                         |
| `execx.Cmd{Name:"env", Args:{"sudo","-n",…}}`                               | `project_seam_commands` | only `Name` is inspected. **Production already uses this shape**: `internal/declare/declare.go:337` runs `env PYTHONPATH=… python3` |
| `"~" + rest` after `strings.CutPrefix(path, home)`; `fmt.Sprintf("~%s", …)` | `project_homeabbrev`    | only five `strings` functions with a `~` constant argument are checked                                                              |
| `n := time.Now; n()` in a test                                              | `project_testclock`     | call through a value                                                                                                                |
| `stamp(time.Now)` (clock passed as a value) in a test                       | `project_testclock`     | not a call                                                                                                                          |
| `var pkgNow = time.Now()` at package level in `_test.go`                    | `project_testclock`     | `testclock.go:39` walks only `FuncDecl` bodies                                                                                      |

Also unflagged, and the catalogue's own headline class: `execx.Cmd{Name:"bash", Args:{"-c", …}}`
through the runner. `CLAUDE/GoProjectRules.md` says `sh -c` evasion of gosec is "the same
defect", but the rule only sees `exec.Command`; a shell string handed to the runner is invisible.
Production builds exactly such strings (`internal/tmuxccy/start.go:82`,
`internal/registry/restore.go:196`).

Concrete fix:

- `seam_exec`: match on the **type** `*os/exec.Cmd` — flag any `*exec.Cmd` value created outside
  `internal/execx` (composite literal or method call `Run/Start/Output/CombinedOutput` on that
  type), plus `syscall.ForkExec`, `syscall.StartProcess`. Also resolve `pass.TypesInfo.Uses` for
  identifiers that are *values* of the listed functions (`f := exec.Command`) and report the
  assignment.
- `seam_commands`: when `Name` is not constant, require it to come from an owner's exported
  constant or report "Name is not a constant: name the program with a constant"; normalise with
  `filepath.Base`; add an `argsWrappers` table (`env`, `nice`, `setsid`, `timeout`, `sh -c`,
  `bash -c`) whose first non-flag argument is checked as the program. Add a rule
  `project_shell_string`: `Cmd{Name: sh|bash, Args: {"-c", X}}` only in a named owner, and X
  must be a constant or a call to a `shellQuote`-shaped function.
- `testclock`: inspect every node in the file (`ast.Inspect(f, …)`) not only `FuncDecl` bodies,
  and report `time.Now`/`Since`/`Until` used as a *value* (`*ast.SelectorExpr` whose `Uses` is the
  func and whose parent is not a `CallExpr`).
- `homeabbrev`: also flag a string constant `"~"`/`"~/"` as an operand of `+` or a `%s`-adjacent
  `Sprintf` format starting with `~`, outside `internal/term`.
- Prove each with a red fixture, as the catalogue requires; the evasion fixture above can be
  dropped into `qa/rules/testdata/src/fix/internal/` nearly verbatim.

### B2. Context cancellation SIGKILLs the terminal UIs, racing their own terminal restore

Severity: **blocker** for the interactive tools (a wedged terminal is the worst UX failure an
interactive tool can have, and no parity suite exercises it).

Evidence: `internal/teams/picker.go:70` wraps the picker in `signal.NotifyContext(ctx, os.Interrupt)`. `internal/execx/execx.go:75` runs every child with `exec.CommandContext` and
sets neither `cmd.Cancel` nor `cmd.WaitDelay`. On Ctrl-C the tty delivers SIGINT to the whole
foreground group: fzf/gum begin restoring the terminal (alt screen, raw mode) while Go cancels
the context and `CommandContext` sends **SIGKILL**. If the kill lands first, the terminal is left
in raw/alt-screen mode. The same default means a child holding a pipe open (e.g. a play that
backgrounded something, `agent-bus` spawning a daemon) blocks `Wait` forever when captured.

Fix: in `OS.Run`, `cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }`
and `cmd.WaitDelay = 2*time.Second` (or make both fields of `execx.Cmd` so the UI owner can
choose SIGINT). Add a test with a child that traps TERM. Alternatively do not bind the UI
children to the cancellable ctx at all; today `ui.Picker.Pick` gets the same ctx.

---

## 2. Correctness risks a parity suite would not catch

### C1. `sudo.AsRoot` misclassifies every `sudo: …` exit-1 as "needs a password"

Severity: should-fix. `internal/sudo/sudo.go:87`: `exitErr.Code == 1 && HasPrefix(stderr, "sudo: ")` → `ErrNeedsPassword`. sudo prints `sudo: /usr/local/bin/agent-bus: command not found`, `sudo: unable to resolve host…`, `sudo: PAM …` with exit 1 too. The caller then says
"sudo asked for a password again, so nothing was changed. Run sudo -v first"
(`internal/teams/session.go:37-45`) and `collectMembers` records `need sudo`, prompting for a
password that will not help. With a sudoers rule that matches **exact argv**
(`files/etc/sudoers.d/agent-bus-read-only.j2:9`), a wrapper path or regexp drift is a plausible
failure and it would be reported as the wrong thing.
Fix: match `strings.Contains(stderr, "a password is required")` (sudo's `-n` refusal text) and
leave the rest to the generic path.

### C2. Unquoted interpolation into shell strings handed to tmux

Severity: should-fix. `internal/registry/restore.go:196-200` `Trampoline` splices `prefix` into
a single-quoted `printf` format unescaped although `shellQuote` sits beside it; `validate()`
allows any newline-free prefix. `internal/tmuxccy/tmuxccy.go:196` `SingleAttachHookFor` puts
`"#{session_name}"` inside an `if-shell` string — tmux expands the name first, then `sh`
evaluates `$(…)`/backticks in it; and it uses the bare `-t "name"` form the owner package itself
documents as wrong (prefix-match hazard, fedora-desktop#69; cf. `panes.go:84` `"="+name` vs
`paneTarget` with the trailing colon in the same file). Session names derive from directory
basenames, which are user-chosen.
Fix: `shellQuote(prefix)`, restrict `prefix` to `ccy|cc` at validation, and use
`-t "=#{session_name}"` (or `#{q:session_name}`) in the hook; one `paneTarget`-style helper for
every tmux target.

### C3. `execx.OS.Exec` ignores `Cmd.Dir`; `teams` works around it with `os.Chdir`

Severity: should-fix. `internal/execx/exec.go:73-92` reads only `Name/Args/Env/Unset`.
`internal/teams/app.go:265-283` therefore mutates process-global cwd before `Exec`, and the
`teams.App.Exec func(path, argv, dir)` field (`app.go:69`) is a second seam shape for the same
thing `sessions.App.Execer execx.Execer` already is. A silently ignored struct field is a trap.
Fix: honour `Dir` inside `OS.Exec` (`Chdir` there, documented), drop the function-typed field,
use `execx.Execer` in both tools.

### C4. `readLine` leaks a goroutine blocked on the TTY per cancelled wait

Severity: nit→should-fix. `internal/teams/offers.go:284-305`: on `ctx.Done()` the reader
goroutine stays blocked in `r.Read`; a later `readLine` on the same reader races it for bytes
(the stale one can swallow the Enter). Today the only cancellation is a Ctrl-C that also ends
the picker, which is why nothing visibly breaks. Fix: make the wait a method on a type that owns
one long-lived reader goroutine and a channel of lines, or read via `OpenTTY` with a deadline
(`SetReadDeadline` on `*os.File`).

### C5. Discarded write errors in two of three print paths

Severity: nit. `internal/teams/app.go:286-290` `print` swallows the error; `sessions/app.go`
`out` likewise; only `term.Term` keeps `WriteErr`, and **nothing reads `WriteErr`** (grep: no
consumers). "No discarded errors" (src-go/CLAUDE.md) is satisfied syntactically, not in effect.
Fix: route all human output through `Term`, and check `Term.WriteErr` in `Run` before returning
(exit 1 with nothing more to print).

### C6. Secrets handling: fine. Checked and no finding

Password is returned on stdout of `sudo -n agent-bus human password` and reaches `wl-copy` on
stdin (`internal/teams/password.go:106-109`); the passphrase **path**, not content, goes on the
restored launcher's argv and is unset from the restorer's own environment first
(`internal/sessions/restore.go:66-71`). Passphrase file ownership/mode checks are strict. Good.

### C7. Lock/clock in `restore` is outside every clock seam

Severity: nit. `internal/restore/manifest.go:343-357` uses `time.Now`/`time.Sleep` directly, so
`LockSetGoing`/`Locked` waits are untestable deterministically and every other package's
`Now/Sleep` seam does not reach them. Fix: `Store.Now/Sleep` fields filled by `sessions.wire`.

---

## 3. "Strictly typed": where it is not

Severity: should-fix (this is an explicit owner criterion).

- A string **protocol inside one package**: `internal/restore/verdict.go:121-150` `GoingState`
  encodes an outcome as `"failed:<detail>"`, `"setting-going"`, `"-"`, and `goingVerdict`
  (`verdict.go:71-82`) parses it back with `HasPrefix(going, "failed:")`. `Judge(prefix string, live bool, pane, container, going string)` takes three stringly states; `containerState`
  returns `"-"|"up"|"starting"`. Fix: a `GoingOutcome` struct `{Kind outcomeKind; Detail string}`
  and a `ContainerState` enum; `Verdict.State` as a typed constant set (it is `string` with
  untyped consts today).
- `restore.Entry.At string` ("Unix seconds, digits") with `AtSeconds()` parser
  (`manifest.go:74`, `:400`), `Detail string` meaning either a token count or a reason;
  `sessions.Config.CompactFloor string` re-parsed on every run (`sessions/app.go:63`,
  `setgoing.go:146`). Fix: `At time.Time`/`int64`, `CompactFloor int64` parsed in `loadConfig`
  (which already refuses bad knobs), `Detail` split into `Tokens int64` + `Reason string`.
- `teams` status words: constants exist (`status.go:7-12`) but literals are compared in
  `collect.go:405` (`"decide"`), `takedown.go:202` (`"removed"`), `offers.go:136`
  (`"outdated" || "unbuilt"`). `declare.Status.State` is `string`; `Row.Kind` is a string;
  `Row.Dir == "-"` is a sentinel; `SeatRestore` values are `"yes"/"no"` and read back with
  `current != "yes"` (`session.go:186`); `homeserverState` is `"up"`/`"down (…)"` compared at
  `session.go:132`, `collect.go:415`. Fix: `type ApplyState uint8` with a parse function at the
  marker boundary; `Row.Kind`/`Restore` enums; `Dir` as `string` + `HasDir bool` or an option type.
- Display text parsed back as data: `session.go:165` recovers the seat from `strings.Fields(picked.Row)[1]`,
  `session.go:180` strips `"(stopped)"` from `row.Session`, `takedown.go:293` `backupIDOf(row)`.
  `ui.PickSpec.Tags` exists precisely to avoid this and the main picker uses it. Fix: tag the
  seat/backup pickers too.
- Composite string map keys: `Snapshot.SeatSession/SeatRestore` keyed `"<dir>|<seat>@<team>"`
  and scanned with `HasPrefix/HasSuffix` (`offers.go:264-274`). Fix: a struct key.
- Exit codes are untyped `int` consts (`term/term.go:14-19`); every `run` returns `int`. A
  `type ExitCode int` costs nothing and makes `leaveError{code}` and `step.code` self-describing.
- `transcript.entries` returns `[]map[string]any` (`transcript.go:105`) and every reader does
  type-switch navigation. Fix: a `struct` with the six fields read, decoded with
  `json.Decoder` per line.

---

## 4. Architecture, DRY, seams

- **`internal/teams` is a god package** (28 files, 5,500 lines; `App` has 20 injected fields
  plus `Config`). Picker, wizard, take-down and collect are distinct bounded contexts sharing
  `App` as a service locator. `teamOperator` (`app.go:77-83`) + `liveOps` (`ops.go`, 0%
  covered) is an interface that exists only so the picker tests can stub the flows — a test-only
  seam, which the brief asked me to hunt for. Fix: split `teams` into `teams/picker`,
  `teams/create`, `teams/apply` packages taking narrow interfaces; delete `liveOps` and pass a
  real `*teamOps`.
- **Second ways to do one thing** (src-go/CLAUDE.md calls this a defect):
  `sessions.isTerminal` (`sessions/app.go:361`) vs `term.FileIsTerminal`; `teams.App.print`
  vs `Term.Printf`; `yesNo` thrice (`registry.go:165`, `manifest.go:251`, `setgoing.go:431`) and
  a fourth inline at `collect.go:236`; `removeQuietly`/`removeIfPresent`; write-tmp-then-rename
  in both `registry.Write` and `restore.Write`; `ccyInstalled` in both tools with the same
  sentence (`teams/app.go:230`, `sessions/app.go:298`), and `sessions.Run` reads `CCY_LIB`
  twice (`app.go:183` then `loadConfig`); the `ps -ww -eo pid=,ppid=,args=` read and parse exists
  in both `sessprobe/collect.go:70` and `sessions/routes.go:65`, so a verify pass reads the
  process table twice. Fix: `internal/fsx` (atomic write, remove-if-present), `term.YesNo`, one
  `ccy.Installed(lib)` helper, `sessprobe.Procs(ctx)` used by routes.
- **Ownership table is incomplete so the doctrine is violated by the module itself**:
  `systemd-run` is run from both `internal/systemd` and `internal/tmuxccy`; `ps` from two
  packages; `busctl`, `env`, `tput`, `gh`, `wl-copy`, `podman/docker` have no owner. Either add
  them to `commandOwners` or document them as "unowned single-use" in the catalogue.
- **Ad-hoc construction mid-flow**: `sessions/reboot.go:200` builds `&systemd.Systemd{}` inline
  instead of using a wired adapter, so it cannot be replaced in a test and bypasses the pattern
  every other adapter follows.
- **`declare.Client.run` spawns `env` to set a variable** (`declare.go:337`) when `execx.Cmd.Env`
  exists for exactly that; one extra process per declare call and it hides the real program from
  the `seam_commands` rule (see B1).
- Global state: `os.Chdir` in `teams` (C3); `a.Unsetenv` in restore is deliberate and documented.
  `execx.syscallExec` is a mutable package var for tests — acceptable, but it is the pattern the
  rules are supposed to forbid for `time.Now`.

---

## 5. Performance

- **teams picker: O(teams + humans) process spawns per redraw, several of them Python.**
  Each loop (`picker.go:…once`) calls `collect`, which runs `declare.py` twice (declared, status
  — Python start-up ~60-120 ms each), `systemctl is-active` per team (`collect.go:410`),
  `sudo -n agent-bus list` per team, and `sudo -n agent-bus human devices` **per human**
  (`collect.go:294`), plus `agent-bus seat list` per checkout. For 3 teams × 4 humans that is
  ~20 spawns and >0.5 s before fzf draws, after every keypress. Fix: a `declare snapshot`
  subcommand returning declared+status in one run; `agent-bus list --with-devices <team…>` (or a
  `list --all`) so the roster and devices come back in one root call; `systemctl is-active unit1 unit2 …` is already one call for N units.
- **sessions picker: every redraw costs ≥ `CPUSample` (1 s) wall time** because `rows()`
  (`table.go:54`) calls `Probe.Detail`, which sleeps for the CPU sample. Fix: sample in a
  background goroutine between redraws, or show the previous sample and refresh it async.
- **Transcripts are re-read and fully JSON-decoded every poll.** `transcript.entries`
  (`transcript.go:105`) reads the whole `.jsonl` into memory and `json.Unmarshal`s every line
  into `map[string]any`; `set-going` and `verify-restore --wait` call `ContextTokens`,
  `TookInputSince`, `CompactedSince` every `Poll` (5 s) per session. Transcripts are routinely
  tens of MB. Fix: a streaming `json.Decoder` over `bufio.Reader` with a typed struct; for
  `anySince`, read backwards from the end (entries are append-only, timestamps monotonic).
- Minor: `ChildEnv` is rebuilt from `os.Environ()` per call (fine); `Fake.Run` is O(rules) per
  call with `slices.Backward` (fine for tests).

---

## 6. Tests

- Quality is generally high: exact argv scripting (`h.on(sudoBus+"human password alpha owner", …)`),
  exact stderr text for usage errors, payload/stderr separation asserted
  (`password_test.go:18-45`). The sessions harness (`helpers_test.go`) injects clock, pauses and
  signals properly.
- Weak spots:
  - `execx.Fake.Called(prefix)` (`execx.go:198`) and `harness.count(prefix)` match on the
    space-joined line, so `Called("tmux")` / `Called("sudo")` / `Called("fzf")` (used once each)
    pass for any invocation; and `"gum style --width 76"` would match `--width 760`. Fix: match on
    argv prefix (`hasPrefix(argv, …)`) like `On` does.
  - 258 `strings.Contains` assertions on stderr. Most are on distinctive sentences; the risk is
    the generic ones (`"failed"`, `"ERROR"`, `"refreshed"`). Not a tautology epidemic, but a
    golden-file approach for the picker transcripts would be stronger.
  - Untested production paths that matter (from `fable-go/lowcov.txt`): `teams/ops.go` all 0%
    (`liveOps` is dead in tests, see §4); `teams/app.go` `NewApp`, `openTerminal`,
    `execLauncher` 0%; `sessions/app.go` `inContainer` 0%; `restore.removeIfPresent` 0%
    (error path of the atomic write never exercised); `teams/session.go reportRootErr` 33%;
    `teams/picker.go confirm` 57%.
  - `execx` has **no test for context cancellation or a hanging child** — exactly the gap of B2.
  - Three `t.Skip` on missing `tmux`/`jq` (`tmuxccy/panes_test.go:167`, `start_test.go:173`,
    `transcript_test.go:243`): by this repo's rules a missing tool is a hard failure, not a skip,
    or the test is not part of the proof. `scripts/test-ccy-tmux-targets.bash` guards one of
    them; the other two can pass green with the oracle absent.
- `scripts/qa-go.bash`: the 90% floor is whole-module, so a new package at 20% hides behind the
  rest; `go test` runs without `-shuffle=on` or `-count=1`; the "untested package" detection is an
  awk over go's human output. Fix: per-package floor from the profile, `-shuffle=on`.

---

## 7. The DBF rules as a system (beyond B1)

- **Catalogue overstates the rules.** `CLAUDE/GoProjectRules.md` row 2 says the rule decides
  "sudo only with `-n`; tmux targets as `=name:`". `project_seam_commands` checks only that the
  program *name* is used inside its owner; inside the owner nothing is checked, and
  `tmuxccy/panes.go:84` uses `"="+name` without the colon beside `paneTarget` that adds it.
  Either write the rules the catalogue describes (flag-shape checks inside the owner) or state
  what is actually enforced.
- **Allow-list mechanism**: `clockAllowances` is data-driven and narrow (package suffix + func
  name + reason) and tested for reasons — good. `homeabbrev` has no allowance mechanism at all,
  so a legitimate `HasPrefix(input, "~")` (does the user's answer need expansion?) has no exit
  other than moving the code into `internal/term`. Give every rule the same table shape.
- **Defect classes in the journal/reports with no rule** (candidates, each real in this tree):
  1. Shell string assembly with interpolated values (C2) — `project_shell_string`.
  2. String literal duplicating a package constant (`"decide"`, `"removed"`, `"outdated"`;
     §3) — `project_const_literal`: a basic-lit string equal to a same-package const's value.
  3. `os.Stdin/Stdout/Stderr` or `os.Getenv` referenced outside a constructor/`wire` function
     (`declare.go:360` `stderr()` fallback, `sudo.New`) — `project_seam_streams`.
  4. `time.Now`/`time.Sleep` in production outside a method whose receiver has a `Now`/`Sleep`
     field (`restore/manifest.go:343`) — `project_clock_seam`.
  5. Process-global mutation (`os.Chdir`, `os.Setenv`) outside `cmd/` and `execx` —
     `project_no_global_mutation`.
  6. Reading `/proc` or spawning `ps` from more than one package — covered by extending
     `commandOwners`.
- **False positives**: `homeabbrev` flags any `~`-prefixed constant in the five functions
  regardless of intent; `seam_commands` would flag a test-support `Cmd{Name:"git"}` in a
  non-`_test.go` helper file. Both acceptable with an allowance table.
- The rules do run on `_test.go` files for `seam_exec`/`seam_commands`-exempt-by-design; fine.
  The qa module is not run against itself (documented).

---

## 8. golangci config

- `exhaustive: default-signifies-exhaustive: true` (`.golangci.yml:98`) neuters the one linter
  that would protect the typed enums this review asks for: any `switch` with a `default:` passes
  when a new `Going`/`lookKind`/`Outcome` member is added. `setgoing.go` and `verdict.go` use
  `default`/fallthrough returns in exactly those switches. Set it to `false` and list the cases.
- `gosec G204` excluded wholesale (`:96`). The justification is right for `gum`/`fzf`, but it also
  silences `teamOps.runPlay` executing `o.Cfg.Play` — a path taken from `CCY_TEAMS_PLAY` in the
  environment — which then elevates through Ansible. Prefer per-call-site exclusion via
  `exclusions.rules` with `path:` for `internal/ui`, `internal/execx`.
- `revive: enable-all-rules: false` leaves revive at defaults; `unused-receiver`,
  `import-shadowing`, `cognitive-complexity`, `unhandled-error` are worth turning on given the
  "no discarded errors" rule (C5 would be caught by `unhandled-error` on `io.WriteString`).
- `gochecknoglobals` disabled module-wide: a `path:`-scoped exclusion for `execx` (the
  `syscallExec` seam) and `ui` (`gumEnv`) would keep it on for the flows.
- Nothing else important is off; `govet enable-all`, `errcheck check-blank`, `gofumpt` are good
  choices. The pin (`GOLANGCI_LINT=2.14.0` in `/.qa-versions`) is not installed in the CCY image,
  so the Go gate cannot run inside a container session.

---

## 9. Smaller nits

- `systemd.LaunchElement` builds a unit name from `l.Team` without `systemd-escape`
  (`systemd.go:145`) while `tmuxccy.StartDetached` escapes; team names are validated by
  `declare.py`, so this is consistency, not a bug.
- `sessions/picker.go` `confirmEnd`: `registry.Locate` + `Remove` after `KillSession` — a kill
  that succeeds and a registry that fails exits 1 with the session gone; order is right, message
  could say the record is left.
- `registry.Store.SetRestore` is a read-modify-write without the manifest-style lock; the
  bash trampoline deletes the file concurrently; the stat-before-write narrows but does not
  close the window. Acceptable; say so in the doc comment.
- `restore.acquire` returns `f.Close` as the release — correct (flock dies with the fd) but a
  named `release()` that also handles `EBADF` would read better.
- `teams.Config.Play`/`ElementPlay` are overridable by environment and then run with
  elevation; document that these are test hooks and refuse them when `RootDir` is the deployed one.
