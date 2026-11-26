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

| Rule                               | Defect class                                                                                                                                                                                                                   | Found in                                                                                                                                                                                                                |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `project_seam_exec`                | a process started outside the runner, so no fake sees it                                                                                                                                                                       | `teams`: `syscall.Exec` in the launcher hand-over                                                                                                                                                                       |
| `project_seam_commands`            | a program run outside the package that owns it, or a command the rule cannot judge (run-time name, hidden wrapper argv, shell `-c`)                                                                                            | `sessions`: `systemctl reboot`; `teams`: the take-down heading via `gum style`                                                                                                                                          |
| `project_tmux_targets`             | a tmux `-t` target in the wrong form for its command (fedora-desktop#69)                                                                                                                                                       | the bash library, before the port                                                                                                                                                                                       |
| `project_testclock`                | wall-clock reads in tests (date-dependent tests failed in the hour after midnight)                                                                                                                                             | `transcript` and `sessprobe` tests: 23 `time.Now()` fixtures                                                                                                                                                            |
| `project_waitdelay`                | a function that sets `exec.Cmd.WaitDelay` and never names `exec.ErrWaitDelay`, so a finished command's output is dropped                                                                                                       | `qa/internal/proc`: bug B25 of the qa-all port                                                                                                                                                                          |
| `project_package_tests`            | a package with no `_test.go` file, whose code the module-wide coverage floor can hide behind the rest of the module                                                                                                            | the old Go gate's every-package-has-tests check, kept when go-qa-ci replaced it                                                                                                                                         |
| `project_homeabbrev`               | a hand-rolled `~` home abbreviation (a bare prefix replace turned `/home/<user>X` into `~X`)                                                                                                                                   | `teams` tests: `strings.Replace(..., "~", 1)` twice                                                                                                                                                                     |
| `project_hidden_bindings`          | an action reachable only by a key, with no visible, named choice for it (also in shell fzf and Python Textual: `qa-hidden-bindings.bash`)                                                                                      | `teams` picker: Ctrl-A/X/O on Running sessions; `ccy-sessions`: Ctrl-T/X/N                                                                                                                                              |
| `project_lifecycle_turn`           | a member's lifecycle notice decided from a read of main and posted outside the team's turn, so another post of the run can come between                                                                                        | teamchat `ends`: `back` posted after the run's own `disconnected` (Plan 00209)                                                                                                                                          |
| `project_keeper_log_barrier`       | a keeper test reading keeper.log past the harness's turn-aware reader, so a wait on a line releases before its turn has done the rest                                                                                          | teamchat `keeper`: `TestOwedNotice` saw the seat held after its line (Plan 00209)                                                                                                                                       |
| `project_fakeclock_count`          | a test that judges a fake clock's timers by the number armed, which a timer it did not mean changes: a wait advances too early, an assertion lies                                                                              | `link` tests: `TestAnOpBeforeThePIDIsNotAnswered` hung; `engine` TIMEOUT tests at a merge                                                                                                                               |
| `project_goroutine_end`            | a goroutine that signals no end, so no owner can wait for it, and a stop missed on one path leaks it for the life of the process                                                                                               | `link`: `go l.read()`, about 145 control readers left blocked in one test binary                                                                                                                                        |
| `project_rawconn_probe`            | a RawConn readiness callback that answers without testing its descriptor, so readiness the poller saw and Read reset is missed and the wait never ends                                                                         | `link`: the pidfd watch never saw a keeper that died before the watch first ran                                                                                                                                         |
| `project_leakcheck_once`           | a second `leakcheck.Check` on one test, which judges before the cleanups registered between the two run and reports a world they stop as a leak                                                                                | `engine`: `TestLiveNeedsEveryFact`'s second world reported the first's transcript watcher                                                                                                                               |
| `project_refusal_kind`             | a declare.py refusal recovered by its type and never by its kind, so a refusal of the host_vars file is asked again as if the answer were wrong                                                                                | `teams lxc-add`: "entries 5 and 6 both declare" asked three times (Plan 00201)                                                                                                                                          |
| `project_realtime_bound`           | a bound on real time that a test can shorten, which then races the step the test asserts; a test that passes when a real timer fires                                                                                           | teamchat `host/lxc`: 1 of 4 probes reached the fake hang before a 20 ms bound (Plan 00209)                                                                                                                              |
| `project_esc_joined`               | a screen that loses an Esc typed just before another key, which arrives as that key with Alt (also in shell fzf: `qa-hidden-bindings.bash`)                                                                                    | `teams` row menu: Esc then Down left the menu open and moved its cursor (Plan 00177)                                                                                                                                    |
| `project_duration_overflow`        | a `time.Duration` built from a number with no bound that keeps it inside int64 nanoseconds, so a large input comes out negative                                                                                                | `vmtest prune-runs --older-than 200000` pruned every failed run (Plan 00192 Task 3.10)                                                                                                                                  |
| `project_command_failure_dropped`  | a command's failure only tested, never carried, so the person sees a guessed cause                                                                                                                                             | `ccy --token-usage`: any curl failure was "could not reach the API" for 15 minutes (Plan 00191 Task 4.9)                                                                                                                |
| `project_self_erasing_clipboard`   | a secret kept short-lived by a clipboard meant to erase itself after a paste, or text promising it (also in shell: `bash-self-erasing-clipboard`)                                                                              | `teams` login: `wl-copy --paste-once`, and "for one paste" (Plan 00201 Task 2.12)                                                                                                                                       |
| `project_liveness_word`            | a status word read as running ("active", "online", "listening", "alive") spelled where no live source decides it                                                                                                               | `teams` board: every member not parked shown green "active" (Plan 00177)                                                                                                                                                |
| `project_agent_branch`             | a teamchat branch on the run's launch value outside the agent seam, so a second agent program is a refactor of every comparison and not an adapter                                                                             | teamchat `keeper` and `link`: the launch comparisons the agent seam replaced (Plan 00209)                                                                                                                               |
| `project_transport_home`           | a teamchat `http.Transport` or `tls.Config` built outside package matrix, so a second place decides whom a client trusts and which name it verifies                                                                            | teamchat `hostnet` probe and `forge`: each built its own transport (Plan 00246 Task 8.8)                                                                                                                                |
| `project_hostname_read`            | a teamchat read of the machine's hostname (a call, or a constant naming its variable or file) or of an address's name, so a hostname can reach a handle's site                                                                 | teamchat `guestfacts`: the guest's `HOOKS_DAEMON_HOSTNAME` read into a site (Plan 00246 Task 8.8, X25)                                                                                                                  |
| `project_problem_error_text`       | an error's own text in a teamchat status problem, which the record refuses past 512 bytes or with a control character, losing the team's whole record                                                                          | teamchat `health`: the leaf renewal's first draft put the CA's long refusal in a problem, and the record was not written (Plan 00246 Task 8.8, X31)                                                                     |
| `project_test_chown`               | a test that gives a file to another user, which needs root: it passes in an agent container and fails for CI's unprivileged user                                                                                               | the first CI run of the Go tests: `homefs`, `proof`, `bundle`, `state`, `host/lxc` (Plan 00246)                                                                                                                         |
| `project_listing_order`            | a test that asserts the order of a directory listing nothing sorted, which is the filesystem's: it passes on one machine and fails on a CI runner                                                                              | teamchat `restore`: `TestOSReadDir` expected `[2 5]` and CI listed `[5 2]` (fedora-desktop#140)                                                                                                                         |
| `project_test_spin`                | a wait in a test that yields in a loop and has no way to fail, so a condition that never holds spins one CPU to the package's timeout                                                                                          | `engine` `TestSD3_8bAQueuedSendOutlivesA401`: 29 minutes at `[runnable]` in CI, twice (issue 140)                                                                                                                       |
| `project_test_conn_read`           | a test's read of a connection, or accept on a listener, that nothing bounds, so code under test that never answers blocks the test to the package's timeout                                                                    | `tlsterm` `closed(c)`: CI hung for 30 minutes (fedora-desktop#300, #307)                                                                                                                                                |
| `project_poll_pure`                | a condition handed to `leakcheck.Poll` or `Drive` that moves a fake clock, sends or closes, so the read after the idle reading moves the world it judged still                                                                 | `engine` `TestWaitRetriesARestoredItemThatCouldNotBeFetched`: the clock advanced inside the condition, a false failure when the engine was idle at the sample (issue 140)                                               |
| `project_room_default`             | a teamchat send's room tested against one side room, so every other room (a later one too) takes main's path: posted, re-checked or refused as main's                                                                          | teamchat `engine`: `postRoom`, `recheck`, `sendFailed` after the reports room (Plan 00209, n3f-b3)                                                                                                                      |
| `project_questions_item_by_room`   | a pending item's room tested against the questions room in the engine or teamsync, which cannot tell a relayed answer in flight from the room's stale items, so a drop of the room loses the answer                            | teamchat `holdsQuestions` and `applyQuestions`: a session restart deleted a worker's undelivered relayed answer (Plan 00209, R0 step 7 review)                                                                          |
| `project_ledger_built_by_hand`     | a teamchat seat-store ledger entry built field by field outside the store, so a field the store's check requires (a ping's verb) can be dropped and every write that settles the delivery is refused                           | teamchat `engine`: the socket delivery's written-yes write and the proof's `ledgerOf` dropped a ping's verb, and the link retried the refused write every round (fedora-desktop#392)                                    |
| `project_defer_assign_lost`        | a deferred function literal that assigns a variable of its enclosing function that is not a named result, so the value (a joined Close error) is lost at the return                                                            | teamchat `migrate/v1` `verifyDir`: the record directory's Close error was joined into an unnamed `err` and dropped (Plan 00178)                                                                                         |
| `project_seat_hold_unrecorded`     | a teamchat seat holding made outside `seat.Store.Take`, or in it before the session record is asked, so a session the sweep judges over (no ccy record) is given a seat the sweep takes back a minute later                    | teamchat `seat.Store.Take` took a seat for a `ccy --teams` launch from a pane on ccy's tmux, which has no record, and the sweep released it under the running session (fedora-desktop#393)                              |
| `project_fake_restart`             | a test that stops or restarts a fake server in a function literal or a go statement, off the goroutine that then uses it                                                                                                       | `teamsync` `TestARestartedServerBetweenPolls`: the persist hook restarted fakehs while the test posted, `no answer` in CI (fedora-desktop#149)                                                                          |
| `project_shared_due`               | a test that names a fake-clock due (`AwaitDue`, `Armed`) two Duration limits of the package under test share, so the other limit's timer can answer (in teamsync, `reportsJoinLimit` before `reportsSettled`)                  | `teamsync` `TestARestoreUnderALongPoll`: hung to the 30-minute timeout in CI (the failpoints test stage), passing locally; `tlsterm` `TestT10ASilentClientIsClosedAtRequestLimit` under `qa-changed`'s loaded go-qa run |
| `project_walk_open`                | an entry a directory walk found opened with no regular-file check guarding the open, so a named pipe among the entries blocks the open for ever                                                                                | `stt`, `vmtest`, `registry` and 17 more of fedora-desktop#195: the Go side of #191, where `run.bash --changed` hung on a pipe under `untracked/`                                                                        |
| `project_walk_entry_type`          | a path beneath an entry a directory listing found (a file inside it, or a listing of it) used in a loop that never asks the entry's type, so a regular file, socket or broken link among the entries is read as a directory    | `seatsweep` `seats abandon`: the launcher's `disconnect.log` under the seat store's `control/` read as a session folder, refusing every run (fedora-desktop#404)                                                        |
| `project_zombie_exit`              | a process judged ended by its leader's zombie state in /proc stat, while its other threads still hold its files                                                                                                                | teamchat `wiring` `TestClaudeStatusLine`: the status line read once the killed link showed Z found link.lock held (fedora-desktop#182)                                                                                  |
| `project_coordinator_absence`      | an absence clause written outside the `CoordinatorAbsent` case of `proto.ClaimCoordinator`, so a seat is told it from a record not known to be current                                                                         | teamchat: a seat told "no coordinator" from a stale record, then acting on it (issue 192)                                                                                                                               |
| `project_container_pids_limit`     | a container run without ccy's whole limit set (`--cgroup-parent`, `--pids-limit`, `--memory`, `--memory-reservation`), so a fork bomb runs to the engine's pids default and the containers together can take the host's memory | `ccc`'s session and sign-in runs (no limit, then no memory caps), after the ccy launcher's Go probes had taken the pids limit (the Go counterpart of the Semgrep rule `ccy-container-default-pids-limit`)               |
| `project_child_exit`               | another program's exit code handed on as the tool's own, which no usage documents and no shell names                                                                                                                           | `ccy --teams`: agent-bus seat choose's 78, "Unknown error code" (Plan 00177)                                                                                                                                            |
| `project_row_kind`                 | a board row with a member's handle and a constant KIND, so the board names the wrong environment                                                                                                                               | `ccy-teams` board: a host cc seat shown as ccy (Plan 00177)                                                                                                                                                             |
| `project_session_kind`             | a seat session asked as `Kind == KindCcy`, which a cc seat is not                                                                                                                                                              | `ccy-teams --graph`: a host seat lost its session line (Plan 00245 Task 4.4)                                                                                                                                            |
| `project_seat_member_type`         | an agent-bus seat call for a session given a constant member type, so a cc session is offered ccy's seats                                                                                                                      | `ccy-teams` picker: Add to a team asked `seat choose --type=podman` for a cc session (Plan 00203)                                                                                                                       |
| `project_identity_fold`            | a GitHub owner or OWNER/REPO a person, a flag or a play gave handed to teamchat's lower-case grammar without the ASCII fold, so a name typed as GitHub shows it is refused                                                     | teamchat-admin `team approve`: a team's trust org in mixed case refused (Plan 00209)                                                                                                                                    |
| `project_semgrep_test_runner`      | a semgrep rule proven with semgrep's own test runner, whose 5 s limit per rule no flag raises, so a loaded host fails a sound rule as a Timeout or a "rule id mismatch"                                                        | the real-semgrep tests of `qa-walk-open` and six more gates, red only under a loaded `qa-all` (fedora-desktop#206)                                                                                                      |
| `project_unit_no_install`          | a systemd unit kept running (`Restart=always`) with no `[Install]`, so it is static: enable does nothing and no boot starts it                                                                                                 | teamchat `unitfiles`: `teamchat-server@.service` and `teamchat-tls@.service`, down after a reboot, and `team apply` reporting `CHANGED unit enabled` every run (fedora-desktop#247)                                     |
| `project_inbox_socket_dir`         | a resolver of Claude Code's inbox socket (cc-socks) directory that does not apply Claude Code's own order of places, so it and Claude Code disagree on the socket path                                                         | teamchat `link`: every hook refused in a ccy session with no `XDG_RUNTIME_DIR` (fedora-desktop#266)                                                                                                                     |
| `project_self_child_stderr`        | a child of the test binary required to say nothing on stderr, which a `-cover` build with no instrumented package (qa-changed's diff mode) makes it say                                                                        | teamchat `kit` and `wiring` tests, red only in `qa-changed` (fedora-desktop#155)                                                                                                                                        |
| `project_submit_unchecked`         | text typed into a TUI pane and submitted with one Enter nothing checks, which a TUI not yet ready loses; a lone Enter pressed past the pane's tmux mode check, which copy mode takes unseen                                    | `ccy-sessions` restore: `/compact` left unsubmitted in two sessions after a reboot (fedora-desktop#264); a dialog answer copy mode would take (Plan 00257)                                                              |
| `project_class_without_detail`     | a log or report line that prints a failed call's class (`matrix.Class`) where its detail, the cause an operator acts on, is available                                                                                          | teamchat `teamsync`: "down: the homeserver did not answer usably" for a seat whose pinned CA refused the server (fedora-desktop#265)                                                                                    |
| `project_start_decision`           | a recorded session's start (resume or fresh) decided outside `conversation.StartPlan`, so the restore, its check and a team restart disagree on when a record starts fresh                                                     | `ccy-sessions` restore: a declared session that never took a turn was unrestorable (fedora-desktop#270)                                                                                                                 |
| `project_stdin_seam`               | a read of stdin outside `internal/stdinread` and the ui prompt seam, so an open pipe nothing writes to holds the command silently for ever                                                                                     | teamchat `client/send.go` `readStdin` (`thread --close`'s Python twin waited minutes; issue #260)                                                                                                                       |
| `project_screen_state_compared`    | what Claude's input box shows judged by `==` or `!=` against one `screen.State`, so every other screen, one the reader does not know included, takes the opposite verdict                                                      | `ccy-sessions` set-going: a `/compact` held by Claude Code for review taken as submitted (fedora-desktop#288)                                                                                                           |
| `project_tmux_gone_text`           | tmux's "not there" answer read by a loose match on a fragment, which another program in front of tmux (lxc-attach, runuser, env) also prints                                                                                   | `ccy-teams shutdown`: a guest server lxc-attach never reached read as empty, "no session running", exit 0 (Plan 00257 B.13 review)                                                                                      |
| `project_conversation_record`      | a host reader naming a session's conversation record's path outside `conversation.Locate`, so it knows one system's record only                                                                                                | `conversation.read`: joined v1's `SessionDir` and `File`, so no host reader found a v2 session's conversation (Plan 00209 Task 5.1)                                                                                     |
| `project_record_file_by_name`      | a session record's file found by the record's name (`Store.Remove`, or `<registry dir>/<name>` built) rather than by the reading that found it, so a record whose file is named otherwise is left behind or named wrongly      | `ccy-sessions` picker: End removed `<registry>/<name>`, and the restore's refusals named it (fedora-desktop#398, after #396)                                                                                            |
| `project_foreign_strict_parse`     | input from outside teamchat's protocol (stdin, an environment variable) decoded whole by the wire's strict parser `proto.Parse`, so a field the program never reads refuses the fields it reads                                | teamchat `hook`: every Claude Code SessionStart payload unusable, no `started`, every `/exit` ending 78 (fedora-desktop#394)                                                                                            |
| `project_selector_parse`           | claude's conversation selectors (`--resume`, `-r`, `--continue`, `-c`, `--session-id`) read from a recorded launch outside registry's `selectorOf`, so two readers of one grammar disagree on what a bare `--resume` takes     | `ccy-sessions` restore: `ResumeID` named `--teams` the conversation of a session launched `ccy --resume --teams <seats>` (fedora-desktop#411)                                                                           |
| `project_refusal_reason_text`      | a teamchat refusal's reason code put into its message, so the agent is told only the code the object already carries and not the cause or the next step                                                                        | teamchat `engine` `refusal`: "the send was refused (ref); fix the message" for `done` with no `--ref` (fedora-desktop#421)                                                                                              |
| `project_question_unresolved`      | which question a questions-room event is in shown by a path that does not ask the one resolver (`proto.QuestionIndex.QuestionIn`, through `team.InQuestion`), so two paths derive the same fact differently, or one not at all | teamchat delivery and `recv`: a human's in-thread reply delivered `question: none` while `verify` named its question (Plan 00265 R34)                                                                                   |
| `project_floor_by_time`            | an event-ordering floor in teamchat's member tree held as a millisecond alone, or a wall-clock since bound cut by `<=` or `>`, so the events sharing its millisecond are dropped                                               | `teamsync` qtake and relays: an owner's answer posted in the millisecond of the post before it was skipped as below the questions floor (Plan 00209 R0 step 7)                                                          |
| `project_temp_not_exclusive`       | a temporary created without O_EXCL and then renamed or linked into place, so a link planted at its predictable name is followed and the write lands where it points                                                            | `fsx.WriteAtomic`: `<path>.tmp.<pid>` written by `os.WriteFile`, in a directory a container shares (fedora-desktop#417 review)                                                                                          |
| `project_seat_number_text`         | text a person is told to type or pick from that spells out a seat (a launch with the seat, a seat placeholder, a hand-over or a repair naming the seat, a row labelled by its seat or handle), when only the machine knows it  | `ccy --teams <team>`'s chooser rows (`seat 2  <handle>  free`) and its ways on (`ccy --teams 3@<team>`), the owner having to know a seat number (Plan 00265 R90)                                                        |
| `project_root_create_follows_link` | a create through an `os.Root` without O_EXCL, which follows a link inside the root at the last name, O_NOFOLLOW or not, so a dangling link there gets its target created                                                       | the seat-record lock, opened `O_RDWR\|O_CREATE` through the checkout's `os.Root` (Plan 00265 R36, the batch 22b review)                                                                                                 |
| `project_store_refusal_lost`       | a teamchat seat-store add's refusal (its list full) that reaches no return, so the write goes ahead without the entry and what it stood for is lost                                                                            | teamchat `engine`: the proof logged a full read-receipt queue and went on, so the human never saw the message read (Plan 00267 C2)                                                                                      |
| `project_teamchat_root_read` | a teamchat machine record (paths' status-root names) handed to a call in ccy-teams with no `TeamchatRoot` in front, so a test world reads the machine's own teams | `teams` wiring: the session-name role lookup and four more read the status record through `system.StatusRecord`, past `teamtest`'s root (Plan 00265 batch 26 review) |

Each rule's doc string (`<driver> help project_<name>`, the driver built from `src-go/qa/driver`;
`go vet -vettool=<driver> help ...` does not reach it) says what it does and
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
  goroutines the tests run: `internal/leakcheck` (`Tests` in a TestMain, `Check` in a
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
hand the exit code on whole (a returned `.Code`, converted or not or through `term.FromChild`,
alone or as a field of a returned literal such as `&stop{term.FromChild(de.Code)}`) whatever
the recovery finds. `ErrUsage`
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

**teamchat's real timers have one home** (Plan 00209 Task 8.1): `internal/teamchat/clock`,
whose `System` is the real side of the `clock.Clock` seam that teamchat's tests replace whole
with `clock.Fake`. Any other teamchat package hands its real wait to `clock.System()` (as
`lock`, `teamlock`, `admin/wire`, `matrix` and `fakehs` do). `system.Sleep`,
`system.AfterFunc` and `system.Hold` there are the package's `realClocks` rows, on the
repository owner's decision (pull request 124): this package is teamchat's one home for real
timers, so a real timer made anywhere else in teamchat is still reported.

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
reason: the phone app's `ACTIVE` manifest state in `internal/stt`, the fake machine's
`systemctl is-active` answer in `internal/teams/teamtest`, and the Matrix specification's
presence value `online` in `internal/teamchat/matrix` (which accepts it) and its `fakehs`
(which returns it as Tuwunel does): SPEC-matrix M50 and M51, the repository owner's decision on
pull request 124, not a liveness claim of ours.

**How to fix a violation.** Take the word from the owner that read it from a live source
(`agentbus.LiveListening` and the other `Liveness` values, from the member's `LIVE` line, which
`agent-bus list` judges from the member's `agent_bus.status` exactly as `pingbus status` does),
or name the state for what its source says (`agentbus.WordNotParked`, parked). A unit's
ActiveState read elsewhere (teamchat reads it over D-Bus) is compared with
`systemd.ActiveStateActive`, systemd's word in its owner. Show what could not be read as
unknown, with why.

- Red, fixture: `testdata/src/fix/internal/badlive` (`bad.go`: the original map of states, a
  constant, a switch case with surrounding space, an upper-case word and a raw string, all
  flagged; `good.go`: a struct tag, words that claim nothing, a word inside a sentence and the
  owner's constant, clean; `bad_test.go`: a test, clean), the owners `fix/internal/agentbus` and
  `fix/internal/systemd`, the allowances `fix/internal/stt` (its excused word clean, another
  flagged), `fix/internal/teamchat/matrix` and `fix/internal/teamchat/matrix/fakehs` (the
  presence value clean, another word flagged; red before the rows were added) with
  `fix/internal/teamchat/status` (the presence value flagged in a package the rows do not
  reach), and `fix/qa/rules`.
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

### project_transport_home

**What it is about.** Whom a client trusts, which TLS versions it speaks and which name it
verifies are decided where the `http.Transport` and the `tls.Config` are made. A team's members
reach their homeserver over TLS with the team's own CA pinned as the only root (Plan 00246
SPEC-cross-network X16): `matrix.NewMember` builds that transport. A second place in teamchat
that makes a transport or a TLS configuration is a second place that can trust the system's
roots, accept a lower version or skip a name check for a connection to a homeserver, and no
review of the pinned path would see it.

**Why it exists.** SPEC X19: before the first TLS client two packages already built their own
transport (the readiness probe and the forge client), and nothing stopped a third from being
written with a bare `tls.Config`. The rule keeps the building in one reviewed package.

**What it flags**, in the non-test files of `internal/teamchat`, `cmd/teamchat` and
`cmd/teamchat-admin` and below, outside package `matrix`, package `pki` (the certificate
package) and package `tlsterm` (the terminator of X18, which builds the one server
configuration: TLS 1.3, the leaf, no client authentication):

- a composite literal of `http.Transport` or `tls.Config`, by value or by pointer, through an
  alias, nested or with its type elided in a slice literal;
- `new(http.Transport)` and `new(tls.Config)`;
- a variable declared with either type by value (`var t http.Transport`);
- a `Clone` of either (`http.DefaultTransport.(*http.Transport).Clone()`, `cfg.Clone()`).

A package below `matrix` (the fake homeserver, `fakehs`) is not `matrix`: it takes the
`*tls.Config` it serves with from its caller, and a test builds one. Test files are not judged.
There is no allowance table.

**How to fix a violation.** Ask `matrix` for the client or the transport: `matrix.NewMember` for
a homeserver, `matrix.LoopbackClient` for a probe of the homeserver's own loopback listener,
`matrix.PublicTransport` for a public https service that is not a homeserver. A case they do not
cover is a constructor added to `matrix`.

- Red, fixture: `testdata/transporthome/src/fix/internal/teamchat/badtls` (`bad.go`: a transport
  literal by pointer and by value, `new`, a `var`, an alias, a `Clone` of the default transport,
  a `tls.Config` literal, `new`, a `var`, a `Clone`, a slice literal with the type elided, a
  literal in a struct field; `good.go`: a configuration only passed on or read, a listener given
  one, a pool, a key pair, a nil pointer, clean; `bad_test.go`, clean), beside `matrix`, `pki` and
  `tlsterm` stand-ins (the owners, clean), `matrix/below` (a package under matrix: one clean use, one
  flagged), `outsidetls` (outside teamchat, clean) and `cmd/teamchat` and `cmd/teamchat-admin`
  (the commands, flagged). The root is its own, because the owner is a package path and the
  shared root's `matrix` and command packages carry other rules' expectations. With the analyzer
  reporting nothing the test failed on every flagged line (14 missed diagnostics) and passed once
  the rule was written.
- Red, real tree, at the rule's own commit (untagged, `busfailpoints` and `tuwunel` alike): two
  findings, `hostnet/probe.go:69` (the loopback readiness and health probe's client) and
  `member/forge/forge.go:138` (the forge client's transport). Both now ask `matrix`
  (`matrix.LoopbackClient`, `matrix.PublicTransport`).
- Green: the tree after the fix, untagged and with each tag, with no finding; the probe's and
  the forge's own tests pass unchanged.
- Not caught: an `http.Client` with no `Transport`, `http.DefaultClient`, `http.Get` and its
  siblings (each uses the default transport, which trusts the system's roots); a transport built
  by a function of a package this rule does not judge; `reflect.New`.

### project_hostname_read

**What it is about.** A handle is `<repo>.<seat>+<site>.<type>` and is permanent once issued
(Plan 00246 SPEC-cross-network X28): it is in every roster, mention and log of the team, and a
hostname in it cannot be taken back. The site is declared per machine (`teamchat_site`, default
`local`) and no other source feeds a handle (X25). A teamchat function that reads the machine's
hostname, or the name an address resolves to, is a function that can put a real hostname in a
handle, however sound its first caller is.

**Why it exists.** SPEC X27, and the code map's Item 5: `guestfacts.Prefix` read
`HOOKS_DAEMON_HOSTNAME` into a site, and normalising it hid nothing (`member-machine-1` stays
`member-machine-1`). The tree read the hostname nowhere else; the rule keeps it that way.

**What it flags**, in `internal/teamchat`, `cmd/teamchat` and `cmd/teamchat-admin` and below,
tests included, every use of:

- `os.Hostname`, `syscall.Uname` and `unix.Uname` (`golang.org/x/sys/unix`);
- `net.LookupAddr`, and a resolver's `LookupAddr` (`net.DefaultResolver.LookupAddr`, a
  `*net.Resolver` of its own);

as a call or as a value (a function value, a package-level seam), through any import name; and
every constant string expression, judged whole (so a name spelt in pieces is the name), whose
value is `HOOKS_DAEMON_HOSTNAME` or `HOSTNAME`, each also with its `=` (the prefix an
environment-file parser cuts), `/etc/hostname` or `/proc/sys/kernel/hostname`. A constant is
reported where it is declared, not at each use.

**The one exemption**, part of this rule's own definition and not a row of any allowance table:
the file `internal/teamchat/declare/machinehost.go`, which reads the hostname once so the
declaration reader can refuse a site that spells it (X26). It is matched as a whole path: another
file of package `declare`, and a file of that name in another package, are judged. A test gets
the hostname through the seam (an injected `func() (string, error)`), as a fake.

**How to fix a violation.** Declare the site (`teamchat_site`). A function that must compare
against the hostname takes it from `declare.MachineHostname`, handed in as the seam, and never
reads it where a handle is made.

- Red, fixture: `testdata/hostnameread/src/fix/internal/teamchat/badhost` (`bad.go`: a call, a
  function value, a package-level seam, `net.LookupAddr`, the default resolver's and a resolver
  of its own; `alias.go`: an import under another name; `bad_test.go`: a test; `good.go`: the
  environment, a forward lookup, a parse and two names of the package's own, clean), beside
  `declare` (the exempt file, clean, and a sibling file, flagged), `other` (a file of the
  exempt file's name in another package, flagged), `outsidehost` (outside teamchat, clean) and
  `cmd/teamchat` and `cmd/teamchat-admin` (flagged). The root is its own, because the exemption is
  a file path. With the analyzer reporting nothing the test failed on every flagged line (19
  missed diagnostics, the package with a test file being judged twice) and passed once the rule
  was written.
- Red, fixture, the named reads (review S4 of pull request 128): `badhost/reads.go` (the
  defect's own form, an environment file's key compared with `"HOOKS_DAEMON_HOSTNAME"`; the
  `NAME=` prefix cut from a line; the name spelt in two pieces; `os.Getenv` of both variables;
  the two files, one under a root; `syscall.Uname` called and `unix.Uname` as a value, against a
  stub of `golang.org/x/sys/unix` in the root), with `good.go` clean (`HOSTNAMES`, a line of
  file content naming the variable, `/etc/hosts`, prose). With the rule as it was, the test
  missed all ten diagnostics of `reads.go`.
- Red, the defect: the rule as extended, run as go vet's tool over the tree at `503e456b5^`
  (the commit before the defect's fix), reports
  `internal/teamchat/host/lxc/guestfacts/guestfacts.go:526:14: "HOOKS_DAEMON_HOSTNAME"`, the
  `if m[1] == "HOOKS_DAEMON_HOSTNAME"` that read the guest's hostname into a site; the rule as
  first written reported nothing over that tree.
- Red, real tree, and fixed: four test lines. Two tests used `/etc/hostname` as any file outside
  a root (`guestroot` `TestReadsOnADeadRootAreGone`, `testfiles`
  `TestWhatLeavesTheDirectoryIsRefused`) and now use `/etc/os-release`; `guestfacts`'
  `TestNoSourceNamesTheHostnameVariable`, which named the variable to scan the non-test files
  for it, is removed, the rule judging every file of teamchat, tests included, for the read
  (a comment that names the variable is no longer checked by anything).
- Green: the tree, with no finding, untagged and with `busfailpoints` and `tuwunel`, over
  `./internal/teamchat/...` and the two commands.
- Not caught: `net.LookupHost` and `net.LookupIP` of a name that came from a handle; a hostname
  read through the `hostname` program; one of the names inside a longer constant (a regular
  expression naming the variable, a line of file content as a test writes) or built at run
  time; reflection.

### project_problem_error_text

**What it is about.** The health tick writes a team's whole status record at once, and
`status.Write` refuses a record any of whose problems is not one line of at most 512 bytes
(`status.OneLine`). An error's own text has no such bound: it can hold a path, a wrapped chain,
an advice sentence or a remote server's words. Put in a problem, it can lose the team's record
for as long as the failure lasts, which is exactly when the record is needed.

**Why it exists.** Plan 00246 Task 8.8, X31: the first draft of the leaf renewal's problem
appended `err.Error()`, and with the CA key gone the cause was `loadCA`'s refusal with its
advice: in the test's scratch tree that was over 512 bytes and the tick's record for the team was
not written at all; with the host's own paths the same problem is 393 bytes, so a longer chain
(a wrapped write error, a longer team name) is all it takes.

**What it flags**, in `internal/teamchat`, `cmd/teamchat` and `cmd/teamchat-admin` and below,
tests included: in the What or the Fix of a `status.Problem` literal (by name or position, a
pointer literal too) and in the two arguments of `status.NewProblem`, a call of `Error()` on an
error that is not a package-level variable, and an error passed to `fmt.Sprint`, `Sprintf` or
`Sprintln`. A sentinel's text is fixed and allowed.

**How to fix a violation.** Name the cause as a fixed class: a sentinel's text,
`matrix.Class(err)`, or a cause the failing step returns beside its error (`host.LeafError`'s
`Cause`).

- Red, fixture: `testdata/problemtext/src/fix/internal/teamchat/badproblem` (`bad.go`: `Error()`
  in What and Fix, a concatenation, `fmt.Sprintf` and `fmt.Sprint` of an error, a positional
  literal, a pointer literal, `status.NewProblem`; `bad_test.go`: a test; the clean forms beside
  them: a sentinel's `Error()`, a class function, a format with no error), beside
  `outsideproblem` (outside teamchat, clean). With `problemTexts` answering nothing the test
  failed on every flagged line, and passed with the rule.
- Red, real tree: the three problems of `health/leaf.go`'s first draft; the sweep over
  `./internal/teamchat/...`, untagged, `busfailpoints` and `tuwunel`, found no other instance (the
  view's problems carry sentinels and `matrix.Class`).
- Green: the tree after the fix (`host.LeafError`), with no finding, untagged and with each tag.
- Not caught: an error's text put in a variable first and the variable put in the problem; an
  error formatted by another function than fmt's three; a problem's text built in another package
  and handed in.

### project_test_chown

**What it is about.** A test that wants "a file another user owns" makes one with `os.Lchown` or
`os.Chown`. Giving a file away needs CAP_CHOWN. Every agent container runs as root, so the call
succeeds there; CI runs the Go tests as an unprivileged user, and it fails as
`lchown ...: operation not permitted`. Nothing run in an agent container can see it.

**Why it exists.** The first CI run of the Go tests failed in six packages of `internal/teamchat`;
five of them held this (`member/homefs` 8 tests, `member/proof`, `member/bundle`, `member/state`
and one in `host/lxc`). The older tests of the repository avoid the failure by `t.Skip` when not
root, which never runs them in CI.

**What it flags**, in `_test.go` files: a call of `os.Chown`, `os.Lchown`, `(*os.File).Chown`,
`syscall.Chown`, `Lchown` or `Fchown`, or `unix.Chown`, `Lchown`, `Fchown` or `Fchownat`, whose
uid or gid is not the caller's: the constant `-1`, or a call of `os.Geteuid`, `os.Getuid`,
`os.Getegid` or `os.Getgid`, is the caller's; anything else is another user's. A helper that
calls it is judged on its own.

**Not flagged**, each because it says what it needs: a call inside an `if` that tests the user is
root (`os.Geteuid() == 0`); a call in a function with an `if` testing `os.Geteuid() != 0` whose
body calls `Skip`, `Skipf` or `SkipNow` (and the else of that `if`); a call that is the init of
an `if` whose body skips. Those tests run nowhere CI runs, which is the other half of the hazard
and the owner's to decide: the PR 124 question is whether they stay skipped, CI runs as root, or
a gate runs the Go tests as an unprivileged user. A chown in a non-test file is production code.

**How to fix a violation.** Make the code under test take the owner it expects through a seam (a
parameter, defaulting to the caller's effective uid), and let the test expect an owner the file
does not have, with a positive twin that expects the real one. The same comparison then runs
unprivileged.

- Red, fixture: `testdata/src/fix/internal/badchown` (each call listed above with another user's
  uid or gid, the caller's uid with another gid, a helper, a fatal on the error) and
  `guardedchown` (the caller's own ids, `-1`, the root branch, the skip, the skip on the error,
  `os.Chmod`, and production code: all clean). Mutation checked: reporting off makes all 14 `want`
  lines red; each of the three guards removed makes `guardedchown` red.
- Red, real tree: 8 findings, in `member/bundle` (2), `host/lxc/seats_test.go` (2),
  `member/homefs` (1, the `giveAway` helper), `member/proof` (2) and `member/state` (1); the same 8
  with `busfailpoints`, `tuwunel`, `ccyteamstest` and `difftest`, and none in the nested `qa`
  module.
- Green: `homefs.OpenFor` takes an `Owner` (the caller's effective uid by default, `Open`), and
  `bundle`, `state` and `homefs` tests say "another user's" by an `Owner` that names the path;
  `proof` judges its file and its directory each by an `Owner` (`scanAs`); the `host/lxc` tests
  say it through `guestroot.OpenDirOwning`. Every one has a positive twin that expects the real
  owner, and each check was mutated (dropped) and went red on the intended test. The same
  packages pass as uid 65534.
- Not caught: a chown through another API (`exec` of `chown`, which `project_seam_commands`
  owns), an owner changed in a non-test helper, or a uid held in a variable (`uid := 4242` is
  flagged; `uid := -1` is too, since only the literal constant and the `os` calls are known to be
  the caller's).

### project_listing_order

**What it is about.** The order of a directory's entries is the filesystem's: ext4 hashes the
names with a seed made at mkfs, tmpfs lists the newest first, btrfs by index. `os.ReadDir`,
`fs.ReadDir`, `filepath.Glob` and `filepath.WalkDir` sort; `(*os.File).ReadDir`, `Readdir` and
`Readdirnames` do not. A test that expects the order its author's machine gave passes there and
fails on a runner whose filesystem lists the same names another way.

**Why it exists.** `restore`'s `TestOSReadDir` expected `[2 5]` and CI listed `[5 2]`
(fedora-desktop#140); with the test's temp directory on tmpfs it fails every time.

**What it flags**, in `_test.go` files: `slices.Equal`, `EqualFunc`, `Compare`, `CompareFunc` and
`reflect.DeepEqual` given a call, or a variable last assigned from a call (no sort of it between),
of a function that returns a listing in directory order. A function does when its body calls one
of the three unsorted listing methods of `os.File` (or `fs.ReadDirFile.ReadDir`) or a function
that does, has a slice among its results, and calls nothing that sorts (package `sort`, or
`slices.Sort*`, or a function of the same package whose body sorts, as `seat`'s `sortNames`). The
mark is an analysis fact, so a test of one package is judged against another package's function.
Outside `_test.go` files it flags each function that reads a directory itself (one of the
unsorted listing methods) and returns the listing in directory order, at its declaration: the
one place to sort it, so no caller, test or not, meets the filesystem's order. A wrapper of such
a function is marked but not reported, since the sort at the read unmarks it.

**How to fix a violation.** Sort the listing before comparing, or compare it as a set. A function
that returns a listing sorts it by name at its return, as `homefs.Root.Entries` does, and then it
is not marked.

- Red, fixture: `testdata/src/fix/internal/badlisting` and `badlistingx` (the originating shape,
  each listing API, a wrapper, a test helper, another package through the fact; sorted, `os.ReadDir`,
  counts, sets, conversions and function values clean; the marks themselves are `want` facts).
  Mutation checked: reporting off makes 11 `want` lines red; the sort guard off makes 2 clean
  cases red.
- Red, real tree: 1 finding, `internal/teamchat/restore/fs_test.go:139` (the originating test),
  seen before the fix; the first sweep also hit a nil dereference of the rule on a call with no
  function to judge (a conversion), now a fixture case.
- Green: the test sorts what `OS.ReadDir` returned; 0 findings over `./...` untagged, with
  `busfailpoints`, `tuwunel` and `ccyteamstest`, and over the nested `qa` module.
- The production clause (the coordinator's decision that `restore.OS.ReadDir` sorts by name):
  the rule above did not reach it, a listing returned by a function that is not a test (0
  findings with `OS.ReadDir` unsorted). Red, fixture: the four functions of `badlisting` that read
  a directory and return it are reported (with the clause off, 4 `want` lines red); a function
  that sorts at the read, one that sorts through a helper of its own package and one that reads
  but returns no listing are clean (with the same-package sorter off, the helper case goes red).
  Red, real tree: exactly one finding over `./...` untagged, with `busfailpoints` and `tuwunel`
  (the same file), `ccyteamstest`, `difftest` and the nested `qa` module:
  `internal/teamchat/restore/fs.go` `OS.ReadDir`. The first run also flagged `bus/seat`'s `names`,
  which sorts through its own `sortNames`: a false positive, fixed in the rule (the same-package
  sorter) before the sweep. Green: `OS.ReadDir` sorts by name; 0 findings.
- Not caught: an order asserted through an index (`got[0]`), a loop over the listing and the
  expectation together, a value derived from the listing, a listing read through an interface
  method (no body to mark), a comparison helper of the test's own, a production function that
  uses a directory order without returning it (a message naming the entries as read), a sort
  through another package's helper (not counted, so its listing is reported), and a sort of
  something other than the listing in the same function (counted as one).

### project_test_spin

**What it is about.** A test waits for an asynchronous effect by polling a condition and
yielding (`for !cond() { runtime.Gosched() }`). The loop's only exit is the condition, so when
the condition can never hold it spins one CPU until the package's timeout, and the only trace
is a timeout panic that names no cause. The exits that look like failure paths are not:
`t.Context()` ends only when the test does, so a guard on it never fires while the test's own
goroutine spins, and a count of polls is a bound on time in disguise that a loaded host shortens
(`project_realtime_bound`).

**Why it exists.** `engine` `TestSD3_8bAQueuedSendOutlivesA401` hung for the whole 30 minute
`go test` timeout in CI's `teamchat-failpoints-test` stage twice (issue 140), at `[runnable]` in
the engine tests' `until`, with the team's sender idle in its `select`. The cause was a test
race (the fake records a PUT as it is made, before the sender has read its 401, so the test
told the team's recovery before the sender's late stop, which then stood over a healthy team);
the spin is why it cost 30 minutes and no diagnosis. The sweep found nine yield loops in
`teamchat`'s tests, three of them with a guard on `t.Context()` that cannot fire.

**What it flags**, in `_test.go` files: a `for` statement whose own code (condition, post
statement or body, a function literal included, not a nested loop) names `runtime.Gosched`, by
the function object (an import alias does not hide it) and as a call or a value, and in which no
code, nested loops included, names `leakcheck.Idle`. A range loop ends by itself and is not
judged.

**The failure path, and where it is sound.** `leakcheck.Idle` is true only when every other
goroutine is blocked on a channel, a select, a lock or a wait group (a stack dump stops the
world, so it is one instant), so nothing can change the condition any more; a goroutine that
runs, is runnable, sleeps, or waits in a system call or the network poller keeps it false, and
so does one in a plain `semacquire`: sync's own waits name themselves (`sync.Mutex.Lock`,
`sync.WaitGroup.Wait`), and the runtime's (a collection starting, the world stopping) are ended by
goroutines a dump does not list. An `engine` wait went false-red on that one, a few runs in a
thousand under load: the snapshot taken at one failure had `Serve` in `semacquire` at its first
line, before any refetch, and read as blocked. A
goroutine that waits on a REAL timer's channel (`time.After`, a Timer, a Ticker) does not: it is
blocked on a channel like any other, and `Idle` reads it as blocked (checked, not assumed). So the
failure path is sound only in a world whose time is a fake clock that the test advances (the
engine, `health` and `proof`), where nothing but the test can fire a timer; with real timers it
can fail a wait that a timer would have ended. `leakcheck.Poll(tb, cond)` is
the loop with it, and the engine's `world.until` and `waitFor` use it. Limit: a world with a
goroutine in the network poller (a listener, a connection: `link`, `teamsync`, `wiring`) is never
idle, so there `Poll` waits as a bare yield loop does; the rule holds the failure path where it
can work and does not make one where it cannot.

**How to fix a violation.** Wait on the event the code under test produces (a channel the fake
closes, `clock.AwaitDue`, a poke), or poll with `leakcheck.Poll`.

- Red, fixture: `testdata/src/fix/internal/spin` (the originating bare spin, a select's default,
  the yield in the post statement, the dead `t.Context()` guard, a function literal, an import
  alias, the function as a value, a spin that also counts, an inner loop of an outer loop; the
  loops that ask `leakcheck.Idle`, also in a nested loop, a single yield, a range loop and a
  poll that does not yield: clean).
- Red, real tree: nine findings in `./internal/teamchat/...`: `engine` (`until`, `settle`, two in
  `recv_test.go`), `health`, `proof/watch_test.go`, `link`, `wiring`, `teamsync`.
- Green: each of the nine waits through `leakcheck.Poll` (the wait test that advances the clock
  until it is answered does so in the step of `leakcheck.Drive`: `AwaitDue` cannot name its
  retry's hold, which shares its due instant with status.json's pacing timer).
- Not caught: a poll that does not yield (a loop of round trips to another goroutine, as
  `waitFor` was), a yield taken before the loop and called in it through a variable, a spin made
  of recursion or `goto`, and a loop in a non-test file that a test calls.
- The other half of issue 140's class, a test that acts on the engine after the fake recorded a
  call without waiting for the engine's follow-up, is not a shape a rule can see (which call
  has a follow-up is the engine's meaning): see [Defect classes that cannot be a
  rule](#defect-classes-that-cannot-be-a-rule).

### project_test_conn_read

In a `_test.go` file, or a package whose name starts with `fake` (code only tests run), a read
that waits on a network peer:

- a `Read` on a value of a connection's shape (methods `Read`, `SetReadDeadline` and `RemoteAddr`);
- an `Accept`, `AcceptTCP` or `AcceptUnix` on a listener;
- such a connection handed to a reader parameter that is not a connection (`io.ReadAll`,
  `io.ReadFull`, `io.Copy`'s source, `bufio.NewReader`, `bufio.NewScanner`, `json.NewDecoder`).

A read in a goroutine is judged too, because the test, or a cleanup that waits for the goroutine,
blocks as long as the read does.

The read is bounded when its function declaration does one of these to the same expression:

- before the read, calls `testconn.Bound` on it, sets a deadline on it, or arms
  `time.AfterFunc` with a literal that closes it;
- anywhere, closes it in a `select` case on a timer (`time.After`, a Timer's or Ticker's `C`), as
  tlsterm's `awaitClose` does;
- for a read in a `go` statement, closes it in a `Cleanup` (an accept loop that the test's end
  stops).

Tests read no wall clock (`project_testclock`), so they cannot set a deadline. The fix is
`testconn.Bound(t, c)` (`internal/testconn`) in the function that waits on `c`. It arms a fixed
one-minute timer that fails the test and closes `c`, and the timer stops when the test ends.

- Red, fixture: `testdata/src/fix/internal/connread` and `fakepeer`. Flagged forms:

  - the originating `closed(c)`;
  - an accept;
  - a `*net.TCPConn`;
  - the standard wrappers;
  - a deadline on the wrong conn or after the read;
  - a goroutine's read awaited bare;
  - an echo goroutine a cleanup waits for;
  - a fake package's `AcceptUnix` and `Read`.

  Clean forms:

  - a deadline, `AfterFunc` close or `Bound` set first;
  - `awaitClose`;
  - an accept loop the cleanup ends;
  - a write;
  - a conn handed to a function that takes a conn.

- Red, real tree: 32 findings in 18 files, every one a teamchat or `stt` test, plus `fakeinbox`.
  The independent search found the same sites and four groups the rule cannot see; those are
  under "Not caught" and were fixed by hand.

- Green: every wait bound with `testconn.Bound`; 0 findings.

- Allowed, by the owner's decision (#307, Q26), one row of `connReadAllowances` with its reason:
  the fake inbox's accept loop (`fakeinbox` `Inbox.accept`), a goroutine for the inbox's life
  that Close ends and no test waits on; binding it to a test's TB was tried and does not compile.
  `TestTestConnReadAllowance` proves a row excuses its one function only (a second unbounded
  read in the same package, and a function of the same name on no receiver, still fail), and
  `TestTestConnReadAllowancesAreOneRow` holds the table to that one row.

- Not caught:

  - a read in a non-test helper whose package name does not start with `fake`;
  - a bound set in another function (a constructor);
  - a read through a wrapper whose type is not of a connection's shape (`wire.Conn`'s `Recv`, a
    decoder over a `bufio.Reader` built elsewhere);
  - a goroutine read that only a cleanup's close ends while the test body waits for that goroutine.

- Next wider rule, not built: judge the wait on a channel that a reading goroutine feeds (a bare
  `<-done`), which is a channel receive and a different class. A test that waits on a channel
  with no timer hangs whatever the channel waits for; that class needs its own rule.

### project_poll_pure

**What it is about.** `leakcheck.Poll` and `leakcheck.Drive` fail a test when every other
goroutine is blocked and the condition does not hold, and they read the condition again after
the idle reading so that no goroutine has run between the two. A condition that acts breaks that:
its second read moves the world the idle reading judged still, and the test fails although the
move would have let the code under test go on. Whether it does depends on whether that code had
finished reacting to the first read when the idle reading was taken, so the test passes on one
run and fails on the next, and more often on a loaded host.

**Why it exists.** The converted `engine` `TestWaitRetriesARestoredItemThatCouldNotBeFetched`
advanced the fake clock inside its `Poll` condition; `qa-changed` failed it once on the merged
tree (`leakcheck: every other goroutine is blocked and the condition does not hold`), with the
engine idle at the sample and its next timer not yet advanced. The same loop advanced the clock
on every poll, idle or not, so on a slow host it could also run the wait's own 10 s deadline out
before the engine reacted.

**What it flags**, in `_test.go` files: a function literal handed as the condition (the second
argument) of `leakcheck.Poll` or `leakcheck.Drive`, nested function literals included, that
calls `Advance`, `Suspend` or `StepWall` on a type named `Fake`, sends on a channel (a select's
send included) or calls `close`.

**How to fix a violation.** Read in the condition and act in the step of `leakcheck.Drive`,
which is called only when every other goroutine is blocked and the condition does not hold, at
most `driveMaxSteps` times (a count of acts on an idle world, not of time, so a loaded host does
not shorten it):

```go
leakcheck.Drive(t, answered, func() { clk.Advance(d) })
```

A condition that takes what it finds, a receive, is not an act: it holds once, and `Poll` and
`Drive` end on the read after the idle reading without reading again.

- Red, fixture: `testdata/src/fix/internal/pollpure` (the originating clock move in a select's
  default, `Suspend`, `StepWall`, a send, a select's send, `close`, a move inside a nested function
  literal, a `Drive` condition that moves; with the analyzer reporting nothing, 8 `want` lines
  go red, and with the receiver-name guard removed a clean `Advance` of another type goes red;
  pure reads, a receive, `Drive`'s step, a move outside any condition, another type's `Advance`
  and a condition held in a variable: clean).
- Red, real tree: exactly one finding, `engine/recv_test.go`, over `./...`, over `./internal/teamchat/...`
  with `busfailpoints`, and over the nested `qa` module (none there).
- Green: that wait through `leakcheck.Drive`.
- Not caught: a condition held in a variable or built by a function, a call that acts through
  another function (a helper that advances), and an act that is not a clock move, a send or a
  close (a write to a fake, a call that releases a gate).

### project_room_default

**What it is about.** A teamchat send picks its room, and what is done with its answer, by its
purpose (`state.Sending.Room`, `proto.Outgoing.Purpose`). A test against one side room, such as
`if room != proto.PurposeReports { post to main }`, gives every other room main's path: when a
further room's sends are built (the questions room), each one is posted to main, re-checked as
main's content, or has its 403 stop the team's main sends, and nothing fails.

**Why it exists.** The reports room's builder (Plan 00209 n3f-b2) removed it from `sendingOf`,
`answerSent` and `recordPosted`, which each took main's room for a report. Three were left:
`postRoom` (every room not the reports room posted to main), `recheck` (every send not a report
decoded as main's) and `sendFailed` (a 403 for anything not a report handled as main's).

**What it flags**, in `internal/teamchat/member/engine` and `internal/teamchat/member/teamsync`
(not their tests): a `!=` comparison with a side room's purpose (a `proto.Purpose*` constant other
than `PurposeMain`) anywhere; an `==` comparison with one that is the condition, or one of the
`&&` terms of the condition, of an `if` with an `else` or whose body ends in a return, a branch
or a panic; and an `==` comparison with one in a case of a tagless switch that another case
follows.

**Not flagged**: a test for main itself (`==` or `!=` `proto.PurposeMain`), whose path is main's
alone; a side room's own addition (an `if` with no `else` whose body falls through, such as the
report window `spend` adds); the last case of a tagless switch; a switch on the room. Other
packages (delivery, the verdicts, `health/view`) test purposes for what they show, not for where
a send goes, and are not judged.

**How to fix a violation.** Switch on the room: a case for each room the code handles and a
default that refuses the rest by name (`unknownRoom` in the engine), so a room added later is
refused until its own case is written.

- Red, fixture: `testdata/src/fix/internal/teamchat/member/engine` (`bad.go`: the three
  originating forms, an `if` with an `else` and a negated side room in a plain expression;
  `good.go`: a switch with a default, tests for main, an addition, a last case, a constant that
  is not a purpose) and `member/roomelsewhere` (out of scope: not judged).
- Red, real tree: three findings, `engine/report.go` (`postRoom`) and `engine/sender.go`
  (`recheck`, `sendFailed`); none in `teamsync`.
- Green: each is a switch on the room with a default that refuses it (`unknownRoom`);
  `TestAQueuedSendToAnUnpostedRoomIsNotPostedToMain` was red with the old default restored (the
  questions-room send posted to main) and is green.
- Not caught: a room held in a variable compared to another variable, a helper that answers
  whether a room is a side room, or a default written as a map lookup. A test fake that records
  no room is a related class with no rule: see below.

### project_questions_item_by_room

**What it is about.** A cleanup owed for stale state that cannot tell a delivery in flight from
that state. A pending item in room `questions` is either the questions room's own sync, which a
member that no longer coordinates the room drops (QS9), or a human's answer relayed to this
worker through main (A 10.6), owed whatever the member's role. Tested by its room alone, the
relayed answer is dropped with the stale room.

**Why it exists.** The R0 step 7 review of Plan 00209: `qstart` set the drop owed whenever
`holdsQuestions` found a questions-room item, and `applyQuestions` deleted every one, so any
sync-session restart (a network blip) deleted a worker's undelivered relayed answer, whose
relay main's floor had already passed.

**What it flags**, in `internal/teamchat/member/engine` and `internal/teamchat/member/teamsync`
(not their tests): an `==` or `!=` comparison of a `state.Pending`'s `Room` field, by value or
through a pointer, with `proto.PurposeQuestions`, either way round.

**Not flagged**: package `state`, where `Pending.OfQuestionsSync` is the one test; another
type's room (a send in flight's); a test against main's room; packages that show an item's room
rather than drop it (delivery).

**How to fix a violation.** Call `state.Pending.OfQuestionsSync`, which keeps a relayed item
(one with `relayed_by`).

- Red, fixture: `testdata/src/qitem/internal/teamchat/member/{engine,teamsync}` (the drop and
  the start's check, a pointer, the reversed and negated forms; the predicate, a send's room and
  main's room clean), `member/state` and `member/deliver` (not judged).
- Red, real tree (`a115fba1b`): three findings, `teamsync/questions.go` (`qdemote`'s log,
  `holdsQuestions`) and `engine/questionswrite.go` (`applyQuestions`); the test fake's copy of
  the drop in `teamsync/world_test.go` is not judged and was moved to the predicate by hand.
- Green: each calls `OfQuestionsSync`; `TestARelayedAnswerOutlivesASessionRestart` and
  `TestAWorkersRefetchOfARelayedPost` were red with `holdsQuestions` testing the room and are
  green.
- Not caught: the room compared through a variable or a helper of another package, and a drop
  of other stale state that a delivery in flight shares (none found in engine or teamsync: the
  demotion's `sending` drop takes only questions posts, and relays are queued as main's).

### project_ledger_built_by_hand

**What it is about.** A record the store validates as a whole, built field by field by its
callers from another record. The store refuses a whole write when one ledger entry is not well
formed, and a ledger entry for a ping must carry the ping's verb (n3d-D30). A caller that copies
the fields it thinks of, and not the one the check requires, makes every write that carries the
entry fail, and a loop that retries the write retries it for ever.

**Why it exists.** fedora-desktop#392: the engine built ledger entries from a pending item in
four places. `recv`'s `Ack` and `reply` carried the verb; the socket delivery's written-yes write
(`deliverloop`) and the proof's `ledgerOf` did not. Every write that settled a socket-delivered
ping was refused, the item stayed pending and `written: unknown`, and the link's proof loop
retried every round, at high CPU and with a fast-growing log.

**What it flags**, outside `internal/teamchat/member/state` and outside test files: a
`state.Ledger` composite literal with any element (keyed or positional; as a value, through `&`,
or as the element of a slice or map literal), and an assignment to a field of a `state.Ledger`.

**Not flagged**: package `state`, where `state.LedgerOf` is the one constructor; an empty
`state.Ledger{}`, the zero value standing for "none"; a test file, which builds its own store.

**How to fix a violation.** Build the entry with `state.LedgerOf(pending, at)`, which copies
every field of the item the ledger keeps.

- Red, fixture: `testdata/src/ledger/internal/teamchat/member/engine` (the written-yes and proof
  forms, a slice element, a pointer, a positional literal, a field assigned on a declared entry;
  the constructor and the empty entry clean, the test file not judged), `member/state` (not
  judged).
- Red, real tree: four findings, `engine/deliverloop.go`, `engine/proof.go` (`ledgerOf`),
  `engine/recv.go` (`Ack`) and `engine/reply.go`.
- Green: each calls `state.LedgerOf` (`proof.go`'s `ledgerOf` and `reply.go`'s
  `ledgerOfPending` are now one-line calls to it). `TestASocketDeliveredPingIsLedgeredWithItsVerb`
  (the written-yes write) and `TestAnUnknownPingProvenIsLedgeredWithItsVerb` (the proof) never
  settle on the unfixed engine and pass on the fix; `state`'s `TestLedgerOfKeepsAPingsVerb`
  holds the constructor and the store's refusal of an entry without the verb.
- Considered and not built: a rule over every store record built outside the store (pending,
  pings, sends). Only the ledger is built from another record in the engine; the others are
  built from the wire, where the field set is the message's.
- Not caught: an entry built in another package by a conversion from a struct of the same
  fields, or by `reflect`; none exists.

### project_defer_assign_lost

**What it is about.** A deferred call runs after the return values are set, and only a named
result is read after it. A deferred function literal that assigns any other variable of its
function (a local or a parameter) writes a value nothing reads. The usual form is
`defer func() { err = errors.Join(err, f.Close()) }()` in a function whose results are
unnamed: the Close error is dropped and the function reports success.

**Why it exists.** Plan 00178: `verifyDir` in teamchat's `migrate/v1` joined the record
directory's Close error into an `err` declared by `:=`, in a function returning
`([]byte, error)`. Neither errcheck nor vet reports it: the error is "used".

**What it flags**, in every file, test files included: an assignment (not `:=`) or an
increment in a deferred function literal whose target is an identifier naming a variable
declared in the function the `defer` sits directly in, outside the literal, that is not one of
that function's named results.

**Not flagged**: a named result; a variable declared inside the deferred literal; a field or a
pointer's target; a variable of a function further out (a `defer` inside an immediately
called literal that sets the outer function's `err`, which the outer function then returns); a
deferred named function.

**How to fix a violation.** Name the function's results, so the deferred assignment is what is
returned; or handle the value inside the deferred function.

- Red, fixture: `testdata/src/deferassign/a` (the originating form, an increment, a nested
  literal's own local, a test file's helper; a named result, a named result of a literal, a
  field, a pointer target and a deferred named function clean).
- Red, real tree: one finding, `internal/teamchat/migrate/v1/record.go` (`verifyDir`), in both
  modules and the `busfailpoints` build. An independent search (text, then reading each hit)
  found the same one, and seven of the same shape outside the class: five `carry` verbs and two
  tests whose `defer` sits in an immediately called literal, the outer function reading the
  variable afterwards.
- Green: `verifyDir` names its results.
- Considered and not built: a rule over every deferred write to a captured variable, a
  function further out included. Those are read by the outer function (the seven above), so
  the rule would flag correct code.
- Not caught: a deferred named function given a pointer to a local (`defer closeInto(&err)`),
  which is the same loss one call away; neither search covered it.

### project_seat_hold_unrecorded

**What it is about.** A take whose precondition is weaker than the keep condition of the sweep
that judges what it made. The owner's seat sweep releases every holding whose session is not
restorable (has no ccy session record), even under a live keeper. A take that accepts any
session name makes holdings the sweep will take back.

**Why it exists.** fedora-desktop#393: a `ccy --teams` launch from a pane already on ccy's tmux
writes no session record (only `ccy_tmux_insulate` writes one, and it does nothing inside
tmux). `seat.Store.Take` took the seat anyway; a minute later the sweep released it, moving the
session's seat directory away under the running keeper, and the session could not send.

**What it flags**, in `internal/teamchat/seat` outside test files: a call of `(*Store).hold`,
the one step that makes a holding (`held/` and `held.json`), outside `(*Store).Take`, or in
`Take` with no call of the session records' `Restorable` before it.

**Not flagged**: `Take` calling `hold` after `Restorable`; a test file; any other package.

**How to fix a violation.** Make the holding through `Store.Take`, whose first step (SPEC-n2c
ST0) asks the session record and refuses a session with none (`ErrNoRecord`).

- Red, fixture: `testdata/src/seathold/bad/internal/teamchat/seat` (the originating `Take`
  with no record asked, a holding made by another method, and one that asks only after it
  holds); `seathold/late/...` (a `Take` that asks only after it holds);
  `seathold/good/...` (a `Take` that asks first) clean. The rule checks order only: a `Take`
  that asks and ignores the answer is review's to catch.
- Red, real tree: one finding, `internal/teamchat/seat/take.go` (`Take`) at the F44 tip before
  the fix.
- Green: `Take` asks `Sessions.Restorable` first and returns `ErrNoRecord` for a session with
  no record; the launcher maps it to exit 78 naming the starts that write a record
  (`TestTakeRefusesASessionWithNoRecord`, `TestLaunchArgsRefusesASessionWithNoRecord`).
- Considered and not built: a rule over every entry point that reaches a take (the
  issue's first idea, `launch-args` and `cc-run`). Every take goes through `Store.Take`, so the
  check there covers them all, and one rule on the one place a holding is made is narrower and
  cannot miss a new entry point.
- Not caught: a holding written through another path that creates `held.json` directly; none
  exists (`hold` is the only writer).

### project_fake_restart

**What it is about.** A test takes a fake server down (`Stop`, `Restart`) from a hook, a
callback or a goroutine, while the test's own goroutine can be using it, with no point where the
test waits for the restart to finish. A request in flight then is dropped, and the client's
retries meet the stopped server: the request gets no answer, under load only.

**Why it exists.** `teamsync` `TestARestartedServerBetweenPolls` failed once in CI
(`resend_test.go:80: ... PUT .../send/...: no answer`, fedora-desktop#149). The engine's persist
hook restarted fakehs on the team's goroutine at its second store write, and that write is the
one the test's own post causes: the long-poll is woken when the event is stored, before the
post's answer is written, so the restart can drop the post, and the client's three tries
(`noWait` between them) can all land while the server is down.

**What it flags**, in `_test.go` files: a call of `Stop` or `Restart` on a type named `Server`
of a package whose name starts with `fake`, inside a function literal or as a go statement's
call. A literal handed straight to a method of package `testing` (`Cleanup`, a subtest's `Run`)
or deferred is not judged; a literal nested in one is.

**How to fix a violation.** The literal tells the test it has reached the point (it closes a
channel and waits on another), and the test restarts the server on its own goroutine once its
requests are answered.

- Red, fixture: `testdata/src/fix/internal/badrestart` with the stub `fix/internal/fakehs` (the
  hook's restart, a stop in a goroutine, a go statement, a literal nested in a cleanup; the
  test's own restart, a cleanup, a subtest, a deferred stop, `Start` and `Close`, and a `Server`
  of a package not named fake: clean).
- Red, real tree: exactly one finding over `./...`, the originating `resend_test.go:73`, seen
  with the test as it was. The interleaving itself was forced red deterministically: with the
  post's answer held (`fakehs.Fault.Hold`, new) and the hook's restart split so its `Start` waits
  for the test's end, the test fails at its post with the CI message (`... no answer`), every run.
- Green: the hook answers the post, signals and waits; the test restarts after its post is
  answered (50 runs, then 60 at `-cpu 1,2,4`, all passed). 0 findings over `./...` untagged, with
  `busfailpoints`, `tuwunel` and `ccyteamstest`, and over the nested `qa` module. The other tests
  that stop or restart a fake server (`fakehs`, `matrix`, `guest`, `ctl`, `seat`, `wiring`) do so
  on the test's goroutine, a subtest's or a cleanup.
- Not caught: a stop reached through a helper called in a literal, a server held in an
  interface, a fake named otherwise, and a literal stored and later called on the test's own
  goroutine (flagged, a false positive the same fix removes).

### project_coordinator_absence

**What it is about.** "The team has no coordinator" is a definite claim, true only of a team
record verified current. A seat was told it from a record that was not known to be current, after
a handover had happened, and acted on the absence (issue 192).
`proto.ClaimCoordinator(current, coordinator)` is the one decision of what a session is told of
its team's coordinator (named, absent, or not known); every clause a session reads is written in
the `CoordinatorAbsent` case of a switch on it.

**What it flags**, under `internal/teamchat` in files that are not tests: a string literal
containing "no coordinator", "without a coordinator" or "coordinator: none" (any case) unless it is
(a) inside a `case CoordinatorAbsent:` clause of a switch whose tag is a call to `ClaimCoordinator`,
or (b) the value of a constant or variable that the rule's table `absenceTexts` names with its
reason. Three are named: the frame's template (`tmplTeamNoCoordinator`, filled only in that case),
the refusal of a send or a reply (`msgNoCoordinator`, given after the roster was judged verified),
and the questions room's closed topic (`closedTopic`, room state built from the roster, not a claim
made to a session).

**How to fix a violation.** Decide the clause with `ClaimCoordinator` and write the text in its
`CoordinatorAbsent` case. Text that is not a claim to a session is named in `absenceTexts`
with the reason, which is a decision for review.

- Red, fixture: `testdata/src/fix/internal/teamchat/{badabsence,proto,member/hooktext}`: a text
  in a constant, in a returned literal, in a switch on a claim someone else decided, written
  before the case, and in the wrong case are flagged; the case of a switch on `ClaimCoordinator`,
  a named constant, a test file and a package outside `internal/teamchat` are clean. With the rule
  weakened to exempt any `CoordinatorAbsent` case, the fixture test fails at the switch on a claim
  decided elsewhere.
- Red, real tree: with `roleLine`'s tag changed from `ClaimCoordinator(...)` to a variable
  holding its result, the rule reported `proto/membership.go` at the clause. Reverted.
- Green: 0 findings over `./internal/teamchat/...` as it stands; the three constants above are
  the only absence texts outside a case.
- Not caught: the claim in other words, a wording assembled at run time, and a named constant
  used outside the case it was allowed for.

### project_walk_open

**What it is about.** A directory lists named pipes, sockets and devices as well as files, and
opening a named pipe for reading blocks until something writes to it. Code that opens whatever a
walk found hangs, with no error, the first time a pipe is among the entries.

**Why it exists.** The Python `play_text()` read every name `os.walk()` listed under a play's
inputs, reached the pipes old plan runs left under `untracked/`, and `run.bash --changed` never
returned (fedora-desktop#191, held by the semgrep rule `walk-opens-non-regular-file`,
[QA.md](QA.md#walk-opens-non-regular-file)). The Go sweep of the same class found 56 sites in 39
files (fedora-desktop#195).

**What it flags**: a call of `os.Open`, `os.OpenFile`, `os.ReadFile`, `ioutil.ReadFile`,
`fs.ReadFile`, an `fs.FS`'s `Open` or `parser.ParseFile` with no source, or of a function of the
package that opens its parameter or its receiver, whose name argument is built from a walk:
`os.ReadDir`, `fs.ReadDir`, `filepath.Glob`, `fs.Glob`, the `Readdir` methods, the parameters of
a `WalkDir`/`Walk` callback, a package function that returns walked names, and any value, field,
slice or closure built from those. A check is `X.IsRegular()` on the type or mode of a walked
value (`d.Type()`, `info.Mode()` from `Lstat`/`Stat`/`Info()`). It guards an open in the branch it
holds (also in an `&&`), in the clause of a tagless `switch` that lists only checks, and after an
`if` whose failed check leaves the block (`return`, `continue`, `break`, a fatal call; also in an
`||`). A tagless `switch` guards what follows it the same way when a clause leaves on a failed
check (`case !d.Type().IsRegular() || …: return nil`, the check also listed beside other
expressions) and every case clause before it leaves too or runs only on a passed check; clauses
after it are guarded as well. In a switch clause `break` and `fallthrough` stay in the block, so
they are no leave; a failed check joined by `&&` and a tagged `switch` guard nothing. The message
names the guard forms the rule takes (R62 of Plan 00265: it named only the open). A name assigned where a check guards it carries the check with it, so a lister that keeps
only regular files returns clean names; a listing taken there is still unchecked. In a summary of
a function, a check of any of its parameters counts (a helper handed an entry and its path), and a
parameter opens only what seeding it adds to the body's own opens. What a walked file says is
not followed: a byte slice (whichever reader returned it, one in another package too), and the
result of a `ReadFile`, `parser.ParseFile`, `io.ReadAll` or a package function that returns one,
names no entry the walk found; but a package function that returns a parameter beside content
(a value built from it on any return) hands a walked name given there back walked. An `os.OpenFile` whose flags name `O_DIRECTORY`
is not an open. One whose flags name `O_NONBLOCK` cannot wait, but a read of what it opened can:
a named pipe with no writer reads as empty, one with a silent writer still blocks Go's poller, and
a device reads without end. So the open file's first use other than `Stat`, `Close`, `Fd`, `Name`
or `SyscallConn` (a method of it, or a call it is handed to; a package function only if it reads
the file unchecked) is judged as the open, and the check that guards it is `IsRegular()` on the
open file's own `Stat()`.

**How to fix a violation.** Check the entry before the open and decide what a non-regular entry
means there, as [QA.md](QA.md#walk-opens-non-regular-file) says: skip it where a stray entry is
not this code's business, refuse it naming the path where every entry should have been a file.
Under `/proc` too: the check costs nothing and the rule cannot tell `/proc` from any other tree.
A test plants a pipe with `internal/fifotest` (`qa/internal/fifotest` in the `qa` module), whose
writer releases a reader at once, so a regression fails the test instead of hanging it.

- Red, fixture: `testdata/src/fix/internal/badwalk` (`TestWalkOpen`): each walk source and
  opener, a helper, a lister, a closure, a record's method, skip-and-warn, a check of another
  name, a check joined by `||`, a case listed beside another condition, a tagged switch (also one
  whose clause leaves on `!check`), a switch clause on a failed check that only warns, that only
  `break`s, that is joined by `&&`, or that follows a clause that stays, a
  listing taken under another check, a flag that can wait; clean: each guarding position, a
  checking lister, names kept where a check holds, a tagless switch case, a tagless switch whose
  clause leaves on a failed check (joined by `||`, or listed, after clauses that leave or hold the
  check, with a clause and a `default` after it), a helper that checks
  its entry, a parameter that is only a label, `O_DIRECTORY`, an `O_NONBLOCK` file only closed,
  read after its own `Stat()` check or handed to a helper that checks it (declared before or after
  its caller), names read out of a walked file (through a checking helper, an unguarded read, a
  reader out of sight). Red too: an `O_NONBLOCK` file read with
  `io.ReadAll`, by its `Read` method, or by a helper that does not check it, content joined to
  a walked directory, and a walked name passed through a helper that returns content beside it. Mutation checked:
  the leaving guard off makes 6 clean cases red, the positive guard off 1, the callee summary off
  leaves 1 `want` unmet, the switch-case guard off 1 clean case red, the check carried by a
  guarded assignment off 2 clean cases red, the listing distinction off
  leaves 1 `want` unmet, the parameter attribution off and the parameter checks off 1 clean case
  red each; the content-read rule off makes 1 clean case red (also with only its package
  summary off), the pass-through summary off leaves 1 `want` unmet, the byte-slice rule off 1, the `O_NONBLOCK` read off leaves 3 `want`s unmet, every package helper taken as a reader 1
  clean case red, and a helper's summary not yet computed taken as a reader 1. The leaving switch
  guard off makes 2 clean cases red, the guard of clauses after a leaving one off 1, and `break`
  taken as a leave in a clause leaves 1 `want` unmet.
- Red, real tree: 56 findings in 39 files at the defence commit, over `./...` untagged, with
  `busfailpoints`, `tuwunel`, `ccyteamstest`, `difftest` and the nested `qa` module. Four shapes
  among them were false positives, each fixed in the rule with a fixture case before the sweep
  went on: a tagless switch case (`planscriptlogging`), a lister that checks before it keeps a
  name (`ccylaunch` `IntegrityHash`), a helper that checks one parameter and opens another
  (`difftest` `describe`), and the `O_NONBLOCK`/`O_DIRECTORY` opens of `bus/seat`; a parameter
  was also marked an opener when only the body's own walk opened. A first cut let every
  `O_NONBLOCK` open pass whatever followed it; narrowed to the read, it found no site it had hidden
  (`bus/seat`'s `readCapped` checks the open file's `Stat()` before it reads). Seven more were
  content followed as names: wiring's `gone(pid)` read `/proc/<pid>/stat` for pids parsed out of
  `keeper.log`, which `session.log` read from a globbed run directory through `testfiles.Try`.
  That read was the real instance, hidden from the rule in another package, so `testfiles`'
  `Read`, `Find` and `Try` now refuse what is not a regular file.
- Green: 0 findings over `./...` untagged, with `busfailpoints`, `tuwunel`, `teamchatrelease`,
  `ccyteamstest`, `difftest`, and over the nested `qa` module.
- Not caught: a check in a callee (`readable(p)` returning a bool is not a check), an open
  through an interface method or another package's function, an `O_NONBLOCK` file returned or
  stored before it is read, or read through a copy of it (`r := f; r.Read`: only the variable the
  open assigns is followed), its `Fd()` or its `SyscallConn()`, a name converted to bytes
  (`[]byte(filepath.Join(dir, e.Name()))`, followed as content), and a time-of-check gap between
  the check and the open.

### project_zombie_exit

**What it is about.** `/proc/<pid>/stat` is the thread-group leader's own entry, and its state
letter turns `Z` as soon as the leader thread has exited. Every Go program is multi-threaded, and
a killed one ends thread by thread: the leader can read `Z` while the other threads are still
exiting and still hold the process's files (a flock, a socket, a pipe end), which are let go only
when the last of them drops the file table. A test that waits for `Z` and then reads anything
that depends on the release races those threads.

**Why it exists.** teamchat `wiring` `TestClaudeStatusLine` froze the keeper, killed the link,
waited for `gone(link)` (true at `Z`) and read the status line once: CI drew the team's normal
segment, because `link.lock` was still held (fedora-desktop#182). An experiment (a child holding
a flock with 8 or 32 busy threads, killed and not reaped, polled as `gone` polls) found the lock
still held at the first `Z` reading in 65 of 300 and 155 of 300 rounds, and never once
`/proc/<pid>/status` read `State: Z` with `Threads: 1` (0 of 600). The Python side of the class is
the semgrep rule [`proc-zombie-taken-as-exit`](QA.md#proc-zombie-taken-as-exit).

**What it flags**, in tests and production alike: a string or character literal whose text is
`Z` (`" Z"`, `"Z"`, `'Z'`) in a function, its function literals included, that also holds a
string literal naming a `/proc` stat file (`"stat"`, or one ending in `/stat`) and none starting
with `Threads` (the thread count of `/proc/<pid>/status`, which makes the reading sound).

**How to fix a violation.** Read `/proc/<pid>/status` and require both `State:` `Z` (or `X`) and
`Threads:` `1`: a non-leader thread is counted until it is released, after it dropped the file
table, so the pair holds only once the whole group has ended. Better still, wait for the effect
the test asserts (the lock free, the line drawn) with a bounded wait, as `TestClaudeStatusLine`
now does.

- Red, fixture: `testdata/src/fix/internal/badzombie` (`TestZombieExit`): the originating
  `gone`, a `Sprintf` path with a trimmed prefix, a joined path with the first field compared, a
  byte compared in a function literal, a production function; clean: another state letter from
  stat, the `status` check, a stat state believed only beside the thread count, a letter range in
  a function that reads no stat, a stat fixture holding `Z` as data.
- Red, real tree: 5 findings at the defence commit, over `./...` untagged, with `busfailpoints`,
  `tuwunel`, `ccyteamstest`, `difftest`, `teamchatrelease` and the nested `qa` module: wiring's
  `gone`, `execx`'s `living`, the status-line golden's `alive`, `qa/internal/proc`'s `gone` and
  the ccy launcher's build lock (`ccylaunch/buildlock.go` `procStat`). The first cut judged tests
  only; the independent search found the build lock, whose "build done" verdict rested on the
  same reading, so the rule was widened before the sweep.
- Green: 0 findings over the same builds.
- Not caught: a stat path built entirely at run time, a state compared through a parameter (a
  helper `procIs(pid, 'Z')` that takes the letter), and a `Threads` literal read but not
  required.

### project_self_child_stderr

**What it is about.** A test runs its own test binary as a child (run again, or copied and run as
another program by the name it is given) and requires the child's stderr to be empty. A test
binary built with `-cover` whose `-coverpkg` names no package it links has the coverage hooks and
no meta-data, and every process of it writes `program not built with -cover` on stderr as it
starts (`internal/coverage/cfile`'s `emitMetaData`), whatever it is run as and whether or not
`GOCOVERDIR` is set. A full run instruments every package (`-coverpkg=./...`), so the line never
appears there; go-qa's diff mode, which `qa-changed` runs and CI's diff gate relies on (issue
#245), passes only the changed packages, so the same test fails on an unrelated change.

**Why it exists.** Issue #155: the member kit's hook and status-line tests failed in
`qa-changed` on a change to `internal/leakcheck`, then the `wiring` end-to-end tests
(`TestClaudeDelivers`, `TestClaudeLinkSilent`, `TestClaudeStatusLine`, `TestClaudeStopBlocks`) on
a change to `internal/teamchat/reexec`. Both passed every `qa-all`.

**What it flags**, in the `_test.go` files of a package where a test file names `os.Executable`,
`os.Args[0]` or `testfiles.Executable`: a comparison with `==` or `!=` of `""` with an expression
whose last selector is `Stderr`, or `string(...)` of one.

**How to fix a violation.** Read the stderr through `testchild.Stderr` (`src-go/internal/testchild`;
the QA module's twin is `qa/internal/testchild`), which takes out that whole line and nothing
else.

- Red, fixtures: `testdata/src/fix/internal/selfchild` (`os.Executable`; a field, `string(...)`
  of a `[]byte` field), `argschild` (`os.Args[0]` in `TestMain`), `fileschild`
  (`testfiles.Executable`); clean: the helper's result, a comparison with a text, another field,
  `len`, and `otherchild`, a package that runs no copy of itself.
- Red, real tree (both modules, the tagged builds too): three findings in `wiring`'s
  `e2e_claude_test.go` (the defect), and two in `qa/internal/proc`'s `proc_test.go`, whose
  children are `sh` in a package whose `priority_test.go` re-runs the binary: read through the
  helper as well, which is harmless there. `kit` had been fixed by hand with a local copy of the
  line, now the helper. Dynamic sweep: every test of both modules run with
  `-cover -coverpkg=<a main package nothing links>` failed only in `wiring`, and passes since.
- Green: each comparison reads the helper's result; the wiring and kit tests pass under that
  foreign `-coverpkg` and plain.
- Not caught: a stderr held in a variable of another name and compared, `len(stderr) == 0`,
  `bytes.Equal` or `strings.Contains` on it, and a package that runs another package's test
  binary.

### project_child_exit

A conversion to `term.ExitCode` (through an alias too) whose operand reads the `Code` field of
an error type (a struct whose value or pointer implements `error`: `execx.ExitError`,
`agentbus.CommandError`, `declare.Error`), directly, inside an expression, or through a local
assigned or declared (`var c = ee.Code`) from such a read in the same function. `internal/term`, which owns the translation, and
test files are not judged. A child's code means what the child's documentation says, not the
tool's: `ccy --teams` with a team the host does not have exited 78, `agent-bus seat choose`'s own
code, which no ccy usage text named, and the owner's prompt reported "Unknown error code".

The fix is `term.FromChild(code)`: the codes the tools document (64, 75, 77, 78, 130) are kept
and any other is 1; say the child's own code in the message. `ccy-teams` then says a 75, 77 or
78 it ends with in words as its last line (`term.Explain`, from `App.Run`), since a shell names
none of them. `project_refusal_kind` counts a code handed on through `term.FromChild` as handed
on whole.

- Red, fixture: `testdata/src/fix/internal/badexit` (`bad.go`: `choose.listFailed`'s original,
  a code carried in a `&stop{...}`, through a local, through a local declared with `var` (review
  round 2 of the unknown-team branch found it missed: red, then green), inside arithmetic, through an alias of the
  type, in parentheses; `good.go`: `term.FromChild`, the code only printed, the tool's own
  `128 + signal`, a code from a struct that is not an error, a constant, a code only compared).
  The stub `fix/internal/term` has `ExitCode` and `FromChild`.
- Red, real tree: 14 (teams `choose.listFailed`, `chooser.failed`, `ops.playExit` and the three
  of `ops.childExit`, `create` three, `lxcadd` four; sessions `systemctlReboot`).
- Green: each through `term.FromChild`, except `ops.playExit`, which never was the tool's exit
  (it is said and compared with run.bash's 75: now `playStatus`, an int), and ccy-sessions'
  reboot, which exits 1 as its usage says and names systemctl's code in the message (a departure
  in Plan 00191 `SPEC-ccy-sessions.md` 1.3).
- Red, real tree after the rule met F44 (Plan 00245 Task 4.4): `lxccmd.failed`, which typed a
  guest op's status as `term.ExitCode` in `GuestError.Exit`. It was never the tool's exit (it is
  compared with the op's allowed codes and printed), and `FromChild` would have hidden an
  undocumented guest code from the "may not give" check. Green: `GuestError.Exit` is an `int`,
  the guest's own status, as `AttachError.Exit` already was.
- Not caught: a code returned by a helper function as an int, a `Code` read in another function
  and passed in as an int, a field named other than `Code`. The independent search found three
  such sites in the real tree. Each hands on a code its spec says is handed on as it is, so the
  hazard (a code the tool's documentation does not name) cannot arise there:
  - `vmtest` `member-ssh` (`member.go`, through `statusOf`) exits with the remote command's
    status, as `ssh` does, and 124 at its time bound (Plan 00192 `SPEC-vmtest.md` E58).
  - `vmtest` `verify` (`verify.go`, through `helperExitCode`): "`verify_response`'s exit is the
    command's exit" (the same spec, E60).
  - `open`'s `handlerResult` (`launch.go`): `128 + n` for a handler killed by signal n is the
    tool's own code, the shell's convention (Plan 00192 `SPEC-open.md` E30, B20).
- `FromChild` keeps 78 and hands it to the shell on purpose: the launcher and its tests read it
  as "a team not running here". The words for it are `term.Explain`'s last line and the usage
  text, not a different number.

**The next wider rule, not built:** every conversion to `term.ExitCode` of a value known only at
run time. Not built because the tools' own codes (a signal's `128 + n`, `pingbus status-line`'s
own statuses) are most of them and carry no hazard; telling a child's code from the tool's own
needs to know where the int came from, which is what the `Code` field of an error says. The
three pass-throughs above are the only sites the search found that the rule does not judge.
Whether to widen the rule to an int helper that returns an error's `Code` is the owner's
decision; the default is not to, as each of the three is a pass-through its spec requires.

### project_row_kind

A composite literal of the teams board's `Row` (`internal/teams/board`, through an alias, or with
its type elided in a slice literal) that gives `Handle` a value known only at run time and gives
`Kind` a compile-time constant (`board.KindCcy`, `board.Kind("lxc")`, an untyped `"ccy"`, a named
constant) or no `Kind`; a `Row` with unkeyed fields is flagged too. A member's handle is
`<repo>.<seat>+<host>.<type>`, and its type word says what runs it: `podman` a ccy container,
`host` a cc session on the desktop, `lxc`, `docker`, `vm`. The board called every seat of a
checkout ccy, so a host cc session on seat `<team>.2+local.host` showed as ccy. A row with no
member (a team, a person, an LXC member that has not joined: `Handle: "-"`) has no handle to read.

The fix is `Kind: board.KindOf(handle)`, which is `agentbus.KindOf`, the one reading of the type
word for a person (`podman` is ccy, `host` is cc). `Kind.IsSession` is "a ccy or cc session",
which the flows that act on a checkout's seats ask, where they asked `Kind == KindCcy`.

- Red, fixture: `testdata/src/fix/internal/badrowkind` (`bad.go`: the board's original
  `seatRow`, `board.Kind("lxc")`, an untyped constant, no `Kind`, an alias with a named
  constant, a slice literal, unkeyed fields; `good.go`: `KindOf(handle)`, a team, a person, a
  deferred LXC member, a kind passed in, the zero row) and the stub
  `fix/internal/teams/board` (a constant kind inside the owner's own package is judged too).
- Red, real tree: `board.seatRow` (1).
- Green: `seatRow` takes `KindOf(sa.seat.Handle)`; `agentbus.KindOf` shows `host` as cc.
- Not caught: an assignment to a row's `Kind` or `Handle` after the literal, and a `Kind`
  computed by a function that ignores the handle.

**The next wider rule, not built:** any reading of a handle's type word outside
`agentbus.KindOf`. Not built because the other readers do other jobs where the hazard (a wrong
kind shown) cannot arise: `roles.parseHandle` checks a handle's grammar and shows no kind;
`seats.nextSeat` numbers a new podman seat (its pattern matches only the roster's `.podman`
members, and this checkout's own seats of every type are counted from the seat list; a host seat
with the same number is a different handle, `.host`, so nothing clashes); `lxcadd` matches the
LXC member it added by its whole handle. The sweep for both rules was `src-go` and the nested
`qa` module, test files and fixtures excluded. The 13 `Kind == KindCcy` checks became
`Kind.IsSession` by pattern, then each was read again for the board's member rows (a roster
member with no seat row here, `Dir: "-"`), whose KIND also comes from the handle: a host member
there was `host` and is now `cc`. Most checks also ask for a folder (`Dir`) or a recorded
session, so a member row is not one of theirs. `seats.SnapshotRunning` now asks for a folder
too, so a host or podman member with no seat row here reads "not known here", not "not
running". The two left (`picker.seatOwnActions`, the `Incomplete` case of `seats.RemoveOrPark`)
treat a cc member row as they always treated a podman one: its menu has the Attach line,
unavailable, and its Park is refused while the seat list could not be read. That conversion
was by hand, and code written after it (`ccy-teams --graph`) asked `Kind == KindCcy` again:
`project_session_kind` now holds the class.

### project_session_kind

An `==` or `!=` comparison with the teams board's `KindCcy` (matched through `types.Info`)
anywhere but an `IsSession` method, which defines the question. A checkout's seat is held by a
ccy container (KIND `ccy`) or a cc session on the desktop (KIND `cc`, `project_row_kind`), so a
flow that acts on a seat's session and asks `Kind == KindCcy` treats a cc seat as no seat.

- Red, fixture: `testdata/src/fix/internal/badsessionkind` (`bad.go`: the graph's original,
  `!=`, the constant on the left; `good.go`: `IsSession`, another kind, a plain string, an
  `IsSession` method of its own).
- Red, real tree, after the unknown-team branch met F44 (Plan 00245 Task 4.4):
  `board/graph.go`'s session line and `seatOf` (F44 wrote them after the hand conversion above)
  and F44's `board.kindWord`, which re-read a `KindCcy` row's handle for the KIND column.
- Green: the graph asks `Kind.IsSession` (`TestMermaidDrawsACcSeatWithItsSession`, red first);
  `kindWord` is gone, as every seat row's `Kind` is already `KindOf` its handle
  (`project_row_kind`), so the column is `string(r.Kind)`. The sweep over both modules and
  every tag set is clean.
- Not caught: a `switch` whose `case` lists `KindCcy`, and a comparison with the string `"ccy"`.

### project_seat_member_type

A call to the agentbus client (`internal/agentbus`, matched through `types.Info`) whose
`memberType` argument is a compile-time constant (`"podman"`, `"host"`, `""`, a named
constant), outside the agentbus package. A checkout's seat belongs to a ccy container (member
type `podman`) or a cc session on the desktop (`host`), and agent-bus offers, creates and
refuses seats by that type (`seat_choices` offers only the asked type; `take_actions` refuses
a seat of the other). The picker's Add to a team asked `seat choose --type=podman` for every
session, so a cc session was offered ccy's seats, which `session add` then refused, and never
its own host seats.

The fix is `hot.MemberType(kind)` for a session of known kind (the one mapping, which hot
control's own seat calls use too), or the launcher's own type passed in by its caller.

- Red, fixture: `testdata/src/fix/internal/badmembertype` (`bad.go`: the picker's original,
  a named constant, `""`, a folded constant; `good.go`: a type from the kind, one passed on)
  and the stub client in `fix/internal/agentbus`, whose own default-taking wrapper (`""`) is
  run through the rule and stays clean.
- Red, real tree: `picker.askSeat` (the defect) and `chooser.handles`, which asked for host
  seats with the constant `typeHost`; it was correct only because its one caller is a host
  launch.
- Green: `askSeat` asks `hot.MemberType(s.Kind)` (`TestAddOfACcSessionOffersItsHostSeats`, red
  first); `handles` and `consent` take the launch's own member type. The sweep over the module
  and the `ccyteamstest` and `busfailpoints` builds is clean.
- Not caught: a type computed by a function that ignores the session's kind, and a constant
  stored in a variable before the call. Also open, not this class: `ccy-teams session add <s> <team>/<seat>` given an existing seat of the other type is not refused by `pickSeat`
  (H22 step 3 does not compare the seat's handle type with the session's, and `seat take`,
  which would refuse it, runs only for a new seat); what happens after that is not traced.

### project_shared_due

**What it is about.** The team's reports join (RP1) bounds each try by `reportsJoinLimit` on the
injected clock, 30 s, the same time the long-poll is held for. While a try is in flight two
waiters on the fake clock are due the same time. A test that waits for the long-poll's hold with
`AwaitDue(ctx, longPoll)` is answered by whichever is armed: the join's bound, which says nothing
of the hold. The test then advances the clock, which cancels the join in flight (a transient
refusal, then one more retry wait the test did not plan for) while the hold is not armed yet; the
hold is armed at the new time and never fires, and the test waits for ever. The clock refuses a
due only when two waiters share it at the moment it is asked (`clock.ErrAmbiguousDue`), so a join
that is not armed yet is not seen.

**Why it exists.** CI's failpoint stage (`go test -race -shuffle=on -count=2` with
`busfailpoints`, on a loaded runner) hung two tests of the n3f stack to the package's 30-minute
timeout: `TestARestoreUnderALongPoll` (PR 166, run 42172755928) and `TestQS2...` (PR 168, run
42173802166), after `go-qa`'s lint stage had been fixed and the test runners started. Locally the
join had ended before either test asked. `TestABackTeamIsShownUpAtItsFirstLongPoll` had the same
form and was fixed first, with the helper `reportsSettled` (the reports room's next state, then
the try's slot taken and given back, which proves no bound is armed).

**What it flags**, in a `_test.go` file of a package that declares the constant
`reportsJoinLimit` (teamchat's `teamsync`): a call of a fake clock's `AwaitDue` or `Armed` (a type
whose method set has `Advance(time.Duration)`) with a constant `time.Duration` argument equal to
that constant's value, however it is spelt (`longPoll`, `reportsJoinLimit`, `30*time.Second`,
`60*time.Second/2`), unless the same function declaration calls `reportsSettled` earlier in its
text. A function literal belongs to the declaration holding it; a helper that names the due is
judged on its own.

**Not flagged**: a due the bound does not share, a call that moves the clock without naming a
due (`Advance`), a due that is not a constant, and a package that does not declare the constant.

**How to fix a violation.** Call `reportsSettled` before the `AwaitDue`, or wait on a due the join
does not share.

- Red, fixture: `testdata/src/fix/internal/teamchat/member/teamsync` (`teamsync.go` the
  constants; `due_test.go`: the originating form, the `30*time.Second` and `60*time.Second/2`
  spellings, the bool query, a subtest's closure and a helper flagged; a settled test, a settle
  then a closure, another due, an `Advance` alone and a computed due clean) and
  `member/noreports` (no `reportsJoinLimit`: not judged).
- Red, real tree: exactly one finding over `./internal/teamchat/...`,
  `teamsync/resend_test.go` (`TestARestoreUnderALongPoll`), seen with the test as it was.
- Green: that test settles the reports room first (the same form as the back-team test); 20 runs
  at `-race -shuffle=on`, then 40 at `-cpu 1,2,4` with `busfailpoints`, all passed, and the rule
  reports nothing over `./internal/teamchat/...`.
- Not caught: a due that is a variable or a call; a settle made by a helper under another name; a
  join armed again after the settle (a second `AwaitDue` after an outage is judged settled by the
  first call); a fake whose advance is spelled otherwise; a package that bounds its join by a
  value it does not name `reportsJoinLimit`.

**Widened (Plan 00257 B.5 fix).** The same hazard with timers armed one after the other: tlsterm
bounds a connection's handshake (`HandshakeLimit`) and then its first request (`RequestLimit`),
both 10 s. `TestT10ASilentClientIsClosedAtRequestLimit` asked `AwaitDue(RequestLimit)`, was
answered by the handshake's bound, and advanced between the handshake's stop and the request's
arming; the request's bound was armed 10 s from the new time and never fired ("a client that sent
no request was left open", once, under `qa-changed`'s go-qa run). The fake refuses only a due two
waiters share at once, so a sequence is not seen. The rule now flags, in any package's
`_test.go` file, an `AwaitDue` or `Armed` whose constant due equals the value of two or more
package-level `time.Duration` constants the package under test declares in non-test files (an
external test package: the package it tests); teamsync's `reportsJoinLimit` stays a shared due
on its own, excused by `reportsSettled` as before. The fix is `clock.(*Fake).AwaitDueFrom(ctx, d, fn)`, which names the timer by its due and the function that armed it (the first caller outside
the clock package; `TestAwaitDueFromNamesTheTimerByWhereItIsArmed`).

- Red, fixture: `testdata/src/fix/internal/sharedlimits` (the originating form, the 10 s spelt as
  `10*time.Second` and `20*time.Second/2`, `Armed`, a closure, a helper and an external test
  flagged; a due one limit alone has, a test-only constant of a limit's value, `AwaitDueFrom`, a
  computed due and an `Advance` alone clean): `TestSharedDueAcrossLimits`, 6 wants unmet before
  the widening.
- Red, real tree: 12 findings, untagged, none with `busfailpoints`, `tuwunel`, `teamchatrelease`,
  `ccyteamstest` or `difftest`, none in the nested `qa` module: `tlsterm` (`gate_test.go`, T10's
  silent client; `tlsterm_test.go`, T3), `matrix/reach_test.go`, `member/client/nolink_test.go`,
  `member/keeper/prepare_test.go`, `member/link` (two in `link_test.go`, two in
  `server_test.go`), `member/engine` (one in `send_test.go`, two in `sender_test.go`).
- Green: each names its timer with `AwaitDueFrom` and the function that arms it; the rule reports
  nothing over either module, untagged or with any of those tags.
- Not caught (widened): a limit that is a variable or of another type, a timer armed with a
  duration the package does not name, and two packages' limits that one test drives together.

### project_identity_fold

**What it is about.** teamchat stores and sends a GitHub owner and an OWNER/REPO in one lower-case
form (A 9.1), so proto's grammars for them (`ValidOwner`, `ValidOwnerRepo`, `ParseRef`, and
`DecodeForge` through them) accept lower case only. GitHub shows names in mixed case and matches
them case-insensitively, so a person writes `ExampleOrg`. Handed to the grammar unfolded, the
name is refused, with nothing to say that only its case was wrong.

**Why it exists.** The first real run of the v2 play refused a team's trust org: `team approve`
and `team apply` read the declaration's forge section with `DecodeForge`, which validates without
folding (Plan 00209). The sweep found the same class in the member's sender: `ping` and
`reply --verb --ref` wrote the reference to the ping as typed, so `commit:ExampleOrg/thing@<sha>`
was refused, though A 9.1 says a sender lowercases `OWNER/REPO`; `proto.LowercaseRefRepo` had no
production caller.

**What it flags**, in every non-test file outside `internal/teamchat/proto` and outside the
`identityFoldAllowances` table: a use of `proto.ValidOwner`, `proto.ValidOwnerRepo`,
`proto.ParseRef` or `proto.DecodeForge` unless it is called with a first argument that is a call
of `proto.LowercaseRefRepo` (a function value of one is flagged); and a value written to
`proto.Ping.Ref`, `proto.Forge.Org`, `proto.Forge.Exclude` or `proto.ForgeRepo.Repo`, in a
composite literal or an assignment, unless it is such a call, `""` or `nil`.

**Not flagged**: proto's own uses (its canonical decoders of the roster, envelopes and verdicts
read what was folded when it was sent); test files; a function in the allowance table, each row a
read of the program's own file, written folded.

**How to fix a violation.** Fold the name where it enters: a forge section a person or a play
wrote is read with `proto.DecodeForgeInput`, which folds `org`, `repos[].repo` and `exclude` in
ASCII (A 2.3: never Unicode's fold) and refuses anything else naming what a GitHub name may be; a
reference is folded with `proto.LowercaseRefRepo` before it is written to a ping or parsed. A
canonical read of the program's own file is a row in `identityFoldAllowances`, with its reason.

- Red, fixture: `testdata/identityfold/src/fix/internal/teamchat` (`declare`: the originating
  `DecodeForge`, an unfolded `ValidOwner`, `ValidOwnerRepo` as a value, a forge section built
  by hand; the folded forms clean; `member/engine`: the reference written as given, in an
  assignment and a literal, and parsed unfolded; folded clean; a test file not judged;
  `member/state`: an allowance row excusing one function and not its neighbour; `proto`: its own
  uses not judged).
- Red, real tree: four findings over both modules: `declare/values.go` (`DecodeForge`),
  `member/engine/send.go` and `member/engine/reply.go` (`Ping.Ref`), and
  `member/state/store.go` (`validRef`, a read of the member's own state file).
- Green: `declare` reads `trust` with `proto.DecodeForgeInput`; `ping` and `reply --verb --ref`
  fold the reference with `proto.LowercaseRefRepo`; `validRef` is an allowance row (the member's
  own file, written folded). The rule reports nothing over either module, untagged and with every
  build tag. `TestDecodeFoldsTrustNames`, the approve case "set the trust as GitHub shows it",
  `TestAPingsReferenceIsSentFolded` and the typed `done --ref` of `TestSD1_2...` were each red
  (refused as an invalid forge section, or as a malformed reference) before the fix.
- Wider rule considered and not built: every lower-case-only grammar of a GitHub name in the
  tree, v1's `internal/bus/declare` (`isOwnerRepo`, `checkedRepo`) included. Its query entry
  points take only `GithubRemote`'s folded answer, and ccy-teams' create wizard folds its flags
  and answers, so no person's input reaches them; v1's Python (`helpers/agent_bus`) is outside
  a Go rule, and is referred to the owner (Plan 00209, the owner-case report).
- Not caught: a name compared with `==` outside these functions and fields; a name that reaches
  a grammar through a helper of another package that is not one of the four.

### project_inbox_socket_dir

**What it is about.** Claude Code binds a per-session inbox socket and reports its path to the
hooks. It picks the directory by its own order: `$XDG_RUNTIME_DIR`, else `$CLAUDE_CODE_TMPDIR`,
else the temporary directory (`os.tmpdir()`: `TMPDIR`, `TMP`, `TEMP`, else `/tmp`). The socket is
`<dir>/cc-socks/<pid>.sock`, resolved. When that path is longer than 103 bytes, it is
`/tmp/cc-socks-<uid>/<pid>.sock` instead. The order was read from the Claude Code 2.1.323 bundle;
the `os.tmpdir()` variables are Node's documented order. Any other code that builds or matches
that path applies some other order. Where it compares its answer with the reported path, it
refuses a socket that is Claude Code's own.

**Why it exists.** The teamchat link took its runtime directory from `XDG_RUNTIME_DIR` alone. In a
ccy container with none set, it expected `/cc-socks/<pid>.sock`, but Claude Code had bound
`/tmp/cc-socks/<pid>.sock`. The link then refused every hook of the session: "the inbox socket is
not a socket of the session's processes" (fedora-desktop#266).

**What it flags**, in every package of both modules: a constant string expression whose value
contains `cc-socks`, judged whole, so the name spelt in pieces is the name. A declared constant is
reported at its declaration.

**Not flagged**: `internal/teamchat/paths/inboxsocket.go`, the one resolver; the rule's own file;
test files, which spell the expected path a resolver is checked against and resolve nothing
Claude Code's process reaches.

**How to fix a violation.** Take the socket from
`paths.InboxSocket(paths.InboxEnvOf(getenv, uid), pid)`, or judge its shape with
`paths.IsInboxSocket(path, pid)`. Never build or match the path in place. A raw `XDG_RUNTIME_DIR`
cannot stand in for the resolver's environment: `paths.InboxEnv` is a type of its own.

- Red, fixture: `testdata/inboxsocketdir/src/fix` (`internal/teamchat/link`: the defect's prefix
  cut under a raw `XDG_RUNTIME_DIR` and its rebuilt path, a join, a suffix check, a declared
  constant, the name in pieces, the long-path fallback; a comment, another directory and the
  resolver's answer are clean; a test file is not judged. `internal/teamchat/paths`: the exempt
  file clean, a sibling judged. `internal/elsewhere`: a file of the exempt name, judged.
  `cmd/tool`: a command, judged).
- Red, real tree: three findings, all in `src-go`: `teamchat/paths/member.go` (`InboxSocket`
  under a bare runtime directory), `teamchat/member/link/facts.go` (`socketProcess`'s prefix cut)
  and `teamchat/member/state/values.go` (`validSocket`, which refused the `cc-socks-<uid>`
  fallback). Reading the callers found a fourth place, `link/main.go`, which handed the link a
  raw `XDG_RUNTIME_DIR`. It names no `cc-socks`, so it was fixed through the resolver's type.
- Green: `paths/inboxsocket.go` resolves Claude Code's order. The link is given
  `paths.InboxEnvOf(getenv, uid)` and judges a reported socket against it. `state` asks
  `paths.IsInboxSocket`. The rule reports nothing over either module. The test for a ccy session
  with no `XDG_RUNTIME_DIR` (with `TMPDIR` set and unset) was red before the fix.
- Wider rule considered and not built: every read of `XDG_RUNTIME_DIR` with no fallback. Most such
  reads place this repository's own state (membership, locks, the ccy launcher's runtime
  files), where a desktop session's runtime directory is required by design. Flagging them would
  match code that does not carry this hazard.
- Not caught: the name built at run time from parts that are not constants; a resolver handed
  the directory, which never names it; and code in another language. No Python or shell in the
  tree resolves the directory (the independent search). pingbus takes the socket from Claude
  Code's `CLAUDE_CODE_MESSAGING_SOCKET`.

### project_class_without_detail

**What it is about.** `matrix.Class(err)` is a failed call's class as fixed text, such as "the
homeserver did not answer usably". A `*matrix.Error` carries more, and the matrix package wrote
all of it, so it is safe to print (SPEC M9: the method, the path, the status, the errcode and a
fixed detail, never a token or the server's words). A TLS handshake that failed against the seat's
pinned CA, a timeout and a refused connection are one class with three details. A line that
prints the class alone drops the cause an operator acts on.

**Why it exists.** The teamchat link logged `down: the homeserver did not answer usably` once,
then retried in silence, for a member whose seat did not trust the server's certificate. The
detail that said so (`TLS verification of <host> failed against the pinned CA sha256:<fp>: ...`)
was never printed (fedora-desktop#265).

**What it flags**, in every package of both modules except teamchat's `matrix` package, which
defines the class: a use of `matrix.Class`, whether a call or the function taken as a value. A
helper that hands the class back is flagged where it calls it. Test files are judged as the code
is.

**Not flagged**: a use whose value, through parentheses and string concatenation, is a field of a
literal of a bounded record in `classRecords` (`table.go`, each with its reason). These are
`status.Problem` (one line of at most 512 bytes, `project_problem_error_text`) and
`seat.NoticeRecord` (notice.json's `why`, one line, with the failed call returned beside it).
There the class is what the record holds when the detail cannot be carried. A problem tries the
cause first when it can (`health`'s `viewProblem`: `matrix.Cause` when it is one line of at most
512 bytes, the class otherwise). The row cannot tell that fallback from a problem that never
tried the cause; that is listed under "Not caught".

**How to fix a violation.** Print `matrix.Cause(err)`: the class, then the failed call's own text
(for an error that is not a homeserver answer, its own text). Keep `matrix.Class` for a bounded
record only.

- Red, fixture: `testdata/classdetail/src/fix`. `internal/teamchat/badclass` covers the defect's
  log line, Sprintf, a concatenated error, stderr, a helper returning the class, the function as
  a value, a record nothing bounds, a comparison inside a bounded record, and a test's log line.
  The cause, a method named `Class` and the two bounded records (by field and by position) are
  clean. `internal/teamchat/seat` covers the record by field, by position and through a pointer
  (all clean), and a variable between the class and the record (judged). `internal/teamchat/matrix`
  is the class's own package, and `cmd/tool` is a command, which is judged.
- Red, real tree: thirteen findings, all in teamchat. Twelve are in `member/teamsync`:
  `teamsync.go` (`stopped`, `down`), `verbs.go`, `receipts.go` (two), `questions.go` (two),
  `session.go` (four) and `builder.go` (`describe`). The thirteenth is `admin/serveview.go` (the
  disconnected notice's answer). The independent search found the same thirteen, plus
  `health/health.go`'s view problem, which the `status.Problem` row hid. That problem carries the
  hazard: the child's answer, the detail, was available. It is fixed, so the count is fourteen.
- Green: `matrix.Cause` (tested in `class_test.go`) replaces the class at all thirteen.
  `describe` printed the class for a failed call and the full text for everything else; it is
  gone. The view problem names the cause when it fits the record's bound
  (`TestAViewFailureOfTheHomeserverNamesItsCause`). Found there and fixed with a test:
  `matrix.Class` called a bare class wrapped (`fmt.Errorf("%w: ...", matrix.ErrUnreachable)`, as
  admin's viewer wraps the child's answer) "not a homeserver answer". The rule reports nothing
  over either module, nor in the tagged builds.
- Wider rule considered and not built: any function that reduces an error to fixed text (an
  `errors.Is` switch to a word), wherever its value is printed. Recognising such a reducer and
  following its value to a print needs data flow the rule does not have. In the tree, every
  reducer whose output reaches a line wraps `matrix.Class` (`describe`), or else sits beside
  `err.Error()` (`admin`'s `errorClass`, `homeserverClass`).
- Not caught: a bounded record given the class without first trying the cause; a bounded
  record's class later printed from its field; a
  detail dropped by a line that prints no class at all. One example is a refusal that names
  neither of the values it compared, such as the link's "the inbox socket is not a socket of the
  session's processes". No rule can say which values a refusal compared, so those are fixed
  conventionally (fedora-desktop#265's socket refusal).

### project_start_decision

**What it is about.** A recorded session is started by the boot restore, judged by
`restore --check` and `declare`, and started again by `ccy-teams` restart. Whether it resumes its
recorded conversation or starts a new one, and the arguments it is started with, must be one
decision, or the check passes what the restore refuses and the restart refuses what the restore
starts.

**Why it exists.** fedora-desktop#270: a record marked `fresh-if-no-conversation=yes` was
started fresh only when its `conversation.json` was missing (`errors.Is` on
`conversation.ErrNotRecorded` in the restore's planner). A session that never took a turn has a
`conversation.json` naming a conversation with no transcript, which was refused as
`UNRESTORABLE no-conversation`, so the next reboot could not restore it. The team restart
(`teams/restart`, B6) resumed by the recorded conversation only and never started such a record
fresh.

**What it flags**, outside test files: a use of `internal/registry`'s `ResumeArgs` or
`FreshArgs` anywhere but `conversation.StartPlan` (and the registry itself), and a use of an
exported error of `internal/conversation` outside that package. Uses are matched through
`types.Info`, so a call, a function value and a variable holding an error are all seen.

**It also flags**, in `conversation.StartPlan` itself, a use of `FreshArgs` when `StartPlan`
never calls `recordedElsewhere` keeping what it found (a call as a statement, or one whose first
result goes to `_`, does not count). fedora-desktop#431: a renamed teamchat session, marked
`fresh-if-no-conversation`, had no record under its new name and no seat record, so it was
started fresh while its checkout's agent-bus record under its old name held its conversation. A
restore finds a conversation by keys (the session name, the system its teams are on) that a
rename or a move to teamchat changes; a miss by those keys is not "nothing recorded". So fresh
is for a session that never recorded a conversation under any name, and otherwise the record is
refused, naming each record found and the repair.

**Not caught**: a start decided from the text of `conversation.For`'s error or from an error
type the package exports (its `ArgsError` marks arguments that could not be worked out, which
is not a lookup error), and start arguments built by hand without the registry's builders.
Nor does it follow `recordedElsewhere`'s kept result to the fresh start: a `StartPlan` that keeps
it and then starts fresh whatever it holds (logs it, say) passes. That needs a data-flow check
from the result to the branch that reaches `FreshArgs`, which `go/analysis` gives only through
SSA, a weight this one function does not repay; the tests of `elsewhere_test.go` hold it.
The bash launcher builds no restore arguments (the Go restore passes them), so the class has
no shell or Python side.

**How to fix a violation.** Ask `conversation.StartPlan(rec, claudeHome)`: it returns the
conversation to resume (empty and `Fresh` for a fresh start, with the reason) and the
arguments, or an error that says why the record cannot be started (a `*conversation.ArgsError`
for arguments that could not be worked out). Act on its answer.

**The next wider rule, not built:** every reader of a session record's restore marks
(`fresh-if-no-conversation`, `restore=`, `compose=`) kept in one package. Not built: the other
marks are read for different questions (listing, `--no-restore`, the replayed `--compose`) that
do not decide a start, and the rule would flag reads that carry no hazard.

- Red, fixture: `testdata/src/fix/internal/badstart` (the restore's planner as it was, a
  resume-only restart, a builder held as a value, a lookup error held in a variable; a plain
  lookup, `StartPlan` and a test file clean) and `fix/internal/conversation` (a second function
  in the package is judged).
- Red, real tree: four findings at `60c6c40c2`, `sessions/restore.go:97`, `:98` and `:100` (the
  defect) and `teams/restart/jobs.go:231`; the independent search made before the rule found the
  same two sites, and no start decided from a record in bash or Python (the launcher's in-place
  relaunch strips selectors in `restart-request.bash`, but resumes the supervisor's live id and
  decides no fresh start).
- Green: both sites ask `conversation.StartPlan`; the sweep over both modules is clean.
  `conversation.TestStartPlanStartsAMarkedRecordWhoseConversationHasNoTranscriptFresh`,
  `sessions.TestAMarkedRecordWhoseSessionNeverTookATurnStartsFresh` and
  `restart.TestAStoppedMarkedSessionThatNeverTookATurnIsStartedFresh` reproduce the defect.
- fedora-desktop#431, red: fixture `fix/unguarded/internal/conversation` (a `StartPlan` that
  starts fresh asking nothing; `fix/internal/conversation`'s guarded one is clean); real tree,
  one finding, `conversation/start.go:59` at `84fcc3375` (the tree the fix was made on), the one
  fresh start the rule's first half already holds to `StartPlan`. Green: `StartPlan` asks
  `recordedElsewhere`; `conversation.TestStartPlanRefusesARenamedTeamchatSessionAndResumesItOnceItsSeatsAreSet`
  and the four other tests of `elsewhere_test.go` reproduce the defect and its bounds.
- fedora-desktop#431 review, red: fixtures `fix/ignored/internal/conversation` (the call as a
  statement) and `fix/discarded/internal/conversation` (its findings assigned to `_`, its error
  kept), each one finding while the rule took any call as asking. Green: the rule counts only a
  call whose first result is kept; the real tree stays clean (`start.go` keeps `found`).

### project_screen_state_compared

**What it is about.** `screen.ClaudeState` reads a pane into one of a few states, but Claude
Code draws more screens than the reader knows: a line held for review, a redraw, a dialog, the
external editor. A verdict asked as `state != screen.Typed` (or `== screen.Ready`, its else
being the verdict) puts every other state, any the reader does not know included, on one side.

**Why it exists.** fedora-desktop#288: set-going's `/compact` stayed in the box, held by Claude
Code ("Removed 1 invisible character · review and press Enter to send"), and a person's Enter
sent it. How it got past `tmuxccy.Submit` is not established: a held line reads `Typed`, which
the check entered again. What is proven is the check's shape: `ClaudeState(pane) != screen.Typed`
took every screen it did not read as typed, one with no box drawn included, as taken.

**What it flags**, outside test files and inside package `screen` too: an `==` or `!=`
comparison with an operand of type `internal/screen`'s `State`, matched through `types.Info`.
A `switch` over a `State` is the exhaustive linter's (`.golangci.yml`): a missing member or a
`default` is refused, so a member added to `screen` is a decision at every consumer.

**Not caught**: a `State` converted to a string and compared, a `State` looked up in a slice
(`slices.Contains`), and a verdict on a screen kept in another type (`restore.State`, the
restore's verdict on a pane). No instance of the first two exists in either module (searched at
the sweep); they are left to review because each needs a data-flow rule for a form nothing uses.

**How to fix a violation.** Switch over the `State`, naming every member, and decide each.

**The next wider rule, not built:** every type that holds a verdict on a pane's screen
(`restore.State` beside `screen.State`). Not built: `restore.Judge` maps every pane it cannot
read to a named verdict of its own, and its one `==` (`sessions/verify.go`, the unattended stop's
detail) decides a message, not whether text was taken; the owner decides whether it is wanted.

- Red, fixture: `testdata/src/fix/internal/badscreenstate` (Submit's check as it was, its
  positive form, a held value, a tagless switch, two states compared; an exhaustive switch, a
  plain string, a printed state and a test file clean) and `fix/internal/screen` (a comparison
  inside the package is judged).
- Red, real tree: eight findings, `tmuxccy/submit.go:69` (the defect), `screen/screen.go:94`,
  `sessclose/sessclose.go:390`, `:441` and `:582`, `teams/winddown/compact.go:96` and `:159`,
  and `teams/winddown/windstep.go:296`; the `ccyteamstest` build adds none.
- Green: every site is an exhaustive switch; the sweep over both modules and the tagged builds
  is clean. Seven keep their behaviour; Submit takes a box only when it is seen empty or busy,
  never a screen with no box (`tmuxccy.ErrBoxNotDrawn`, no further Enter), and names what the
  box showed when it gives up. `screen.TestALineHeldForReviewIsStillTyped` reads two captures
  of Claude Code 2.1.325 (a line held for review, then sent with the notice left above the empty
  box); `tmuxccy.TestSubmitEntersALineHeldForReviewAgain` drives Submit through them, and
  `TestSubmitNamesTheHeldStateWhenItGivesUp`, `TestSubmitDoesNotTakeAScreenWithNoBoxAsTaken` and
  `TestSubmitStopsAtAScreenThatDrawsNoBox` were red on the old Submit. These four tests pin the
  behaviour #288 reported: a held line is entered again, and a screen with no box is never
  taken as submitted.
- Why the seven other sites are in scope: each decided an action by one state with every other
  state, one added later included, on the opposite side (`screen.go:94` whether a box is drawn,
  `sessclose` whether a close may type or has ended, `winddown` whether a compaction or a step
  may type). None was seen to misjudge a screen today; the rule holds them because a state added
  to `screen` would land on whichever side the comparison's else gives it, unchosen. The sweep
  and Submit's fix share one commit because Submit's comparison is one of the eight findings and
  the gate is not green until every site is a switch.

### project_tmux_gone_text

**What it is about.** tmux says a server or a target is not there in a few exact forms, with
exit 1: `no server running on <socket>`, `error connecting to <socket> (No such file or directory)`, `can't find session: <name>` (and for a pane command `can't find pane`/`window`).
A match on a fragment of them (`No such file or directory`, `can't find`) takes any program's
failure that prints the fragment for the answer. A server in an LXC container is reached through
`lxc-attach`, `runuser` and `env`, and each of them prints `No such file or directory` when it
cannot attach or cannot execute the next program.

**Why it exists.** The Plan 00257 B.13 review, finding 4: `tmuxccy.List` read any stderr holding
`No such file or directory` as an empty server, so ccy-teams shutdown named an LXC seat whose
server it never reached as "no session running in container C" and exited 0.

**What it flags**, in `internal/tmuxccy` and every package that imports it (the packages that
hold a tmux answer; the program is tmuxccy's alone by `project_seam_commands`), in files that
are not tests: a string literal containing `no server running`, `can't find`, `error connecting to` or `No such file or directory`, outside tmuxccy's function `goneAnswer`. Struct tags and
import paths are not judged.

**Not caught**: a fragment assembled at run time or split across literals, a match in a package
that neither is nor imports tmuxccy, and a fragment of the same answers not on the list.

**How to fix a violation.** Ask tmuxccy: `errors.Is(err, tmuxccy.ErrSessionGone)` (Clients,
KillSession and CapturePane return it), or List and Panes, which read no server as empty.
`goneAnswer` accepts only exit 1 with every stderr line one of tmux's own forms in the call's
scope (server; session; pane or window).

- Red, fixture: `testdata/src/fix/gone/internal/tmuxccy` (List as it was; the classifier and a
  test file clean), `fix/gone/internal/badtmuxgone` (sessclose's `can't find`, constants, a
  struct tag and a comment clean) and `fix/internal/notmuxgone` (lxcfreeze's own `nmcli` text in
  a package holding no tmux answer: clean).
- Red, real tree: fourteen findings at seven sites, `tmuxccy/tmuxccy.go:130` and `:160`,
  `tmuxccy/panes.go:23` and `:96`, `tmuxccy/submit.go:93`, `sessclose/sessclose.go:504` and
  `teams/winddown/windstep.go:220`; the `ccyteamstest` build adds none. The command: the
  driver built from the rule's tree (`cd src-go/qa && go build -o <dir>/driver ./driver`), the
  tree before the fix taken out of git (`git archive 2d2c7f042^ src-go | tar -x -C <dir>/old`),
  then in `<dir>/old/src-go` `go vet -vettool=<dir>/driver ./...` and again with
  `-tags ccyteamstest`, each giving the same fourteen `tmux's answer ... matched outside tmuxccy's goneAnswer` lines.
- Green: every site asks `goneAnswer` through tmuxccy; CapturePane's not-there answer now
  matches `ErrSessionGone`, which Submit, sessclose and winddown test. `tmuxccy`
  `TestListReadsOnlyTmuxsOwnNoServerAsEmpty` and `TestCapturePaneIsGoneOnlyOnTmuxsOwnAnswer`
  (lxc-attach, runuser and env failures, exit 127 and a mixed stderr) were red on the old code;
  `shutdown` `TestAV2SeatWhoseTmuxCannotBeReachedIsNotReachable` was red with the loose match
  put back (exit 0, "no session running").

## Defect classes that cannot be a rule

A class is listed here, with what holds it instead, when no analyzer can judge it without
guessing; the catalogue claims no more than the code does.

- **teamchat code shaped by a fake homeserver answer that Tuwunel does not give** (Plan 00265
  R10, R12). `fakehs` answered a non-member's GET event 403, where Tuwunel 1.9.4 answers 404
  M_NOT_FOUND. So a worker's link joined questions only on a 403, never joined, and dropped
  every relayed answer and verify of a questions post. `fakehs` also posted a thread rooted at
  an edit, which Tuwunel refuses 400 M_UNKNOWN (MSC3440), so a coordinator's reply to an
  edited human post was refused live while every test passed. Which answer a real server gives
  is a fact about the server, not a shape in the Go syntax tree. What holds it: the fake
  itself, corrected to Tuwunel's own source for the pinned version
  (`src/api/client/room/event.rs`, `send.rs` `check_nested_thread`) and proven by
  `TestAnEventAUserCannotSeeIsNotFound` and `TestAThreadRootedAtAnEventWithARelationIsRefused`.
  The sweep is the whole teamchat test suite under the corrected fake: it turned red only the
  four relay and verify tests of `teamsync`, fixed in `getQuestionsEvent`. Where an answer has
  no H4 recording, its fake text cites the server source it was taken from.

- **A restore verdict that calls a record restorable without checking it answers every launch
  question its replay can meet** (fedora-desktop#270: `restore --check` and `declare` called a
  ccy record RESTORABLE when its project had compose files and it held no `compose=` answer, so
  the first restore stopped at the compose question). Whether a replay meets a question depends
  on the project's files and the container engine's state at boot (a compose file, a network,
  running containers, the token pool) and on the bash launcher's flow, not on a shape in the Go
  syntax tree; the next form considered, a table test pairing every `ccy_prompt_refuse` name with
  how the check judges it, would pin names without knowing which questions a restore reaches.
  What holds it: `judgeRestore` (`sessions/restore.go`) refuses a ccy record with no `compose=`
  in a project with a compose file unless its launch says `--no-network`
  (`TestRestoreCheckRefusesARecordWithNoComposeAnswerInAComposeProject`,
  `TestDeclareInAComposeProjectNeedsAComposeAnswer`). The independent search listed other
  questions a restore pane may meet with no record answer (token choice, network connect, the
  engine conflict, ssh key choice, the relabel fix); whether a restore reaches each is not
  established, and is logged as fedora-desktop#279.

- **A restore verdict that calls a record restorable although its unattended launch refuses
  it before the session starts** (fedora-desktop#283: `restore --check` and `declare` called a
  ccy record RESTORABLE when its `--token` had no usage left, and the boot launch stopped on
  it, so the self-update cycle's reboot hold let a reboot through into a lost session). A
  refusal depends on the state at boot (a token's usage window, a file, a network, a seat held
  elsewhere) and on the bash launcher's flow, not on a shape in the Go syntax tree; the next
  form considered, a test pairing each launcher `exit` before the container starts with a
  verdict word, would pin call sites without knowing which a boot reaches. What holds it:
  `judgeRestore` asks `tokenRefusal` (`sessions/tokens.go`), which finds the token file as the
  launch does and reads its usage with the launch's own reader
  (`ccylaunch.Front.TokenUsability`): `token-missing`, `token-expired`, `token-used-up`,
  `token-check-failed`, and usage that cannot be read restorable with a note
  (`TestRestoreCheckJudgesTheRecordsTokenAsTheLaunchWould`, `TestDeclareOnAUsedUpTokenIsRefused`).
  The independent search found other refusals a restore pane reaches that no verdict judges
  (a recorded `--ssh-key` or `--network` that is gone, a held `--teams` seat, the
  allowed-hostnames policy, the network preflight, the passphrase file `restore` checks and
  `--check` does not, and `cc`'s expired pool), logged as fedora-desktop#284. Those judgeable
  from files or one read-only engine question are now held by `launchRefusal` and
  `networkRefusal` (`sessions/refusals.go`): `ssh-key-missing`, `network-missing`,
  `host-not-allowed` (the launcher's rule ported as `internal/allowedhosts`),
  `passphrase-file-unusable` and `token-pool-expired`, each with its
  `TestRestoreCheck...` test in `sessions/refusals_test.go`. A held seat, the boot network
  preflight, GitHub reachability and claude's flag check are left unjudged on purpose: they
  need a probe at boot or are transient, and a launch stopped by one keeps its record.

- **A test or a code path that works only because root is not subject to permission checks**
  (found by the first CI run of the Go tests, all of it invisible in an agent container, which
  runs as root). Three forms. A fixture that assumes the tree it builds is root-owned: the guest
  root of the LXC host (`host/lxc/guestroot`) refuses a tree whose owner is not uid 0, and the
  `host/lxc` tests build a guest under `t.TempDir()`, which for an unprivileged user is the
  user's, so the walk refused it (`guestroot: refused: / is not root's`), every test that
  applied a kit failed, and one test then waited on a socket nothing connected to until the 30
  minute `go test` timeout. A directory a test makes under a umask of 0777 (`homefs`): its mode
  is 0, which only root can look into, so the test's own `t.TempDir()` could not be read, and
  `homefs.Mkdir` opened a directory it had just made for reading before it set its mode, which
  failed for a user with a umask that takes the owner's read bit off (it now holds it by
  `O_PATH`). An owner fixed at root in the production wiring (`admin/wire`'s `machine.stateOwnerUID`,
  review S8 of pull request 128), which a test whose `ca.key` is its user's reached: the CA read
  refused it, `owned by uid 1001, not 0`, in CI's run and never in a container. A run as another
  uid shows one such failure: `go test -exec 'setpriv --reuid=1001 --regid=1001 --clear-groups' ./internal/teamchat/<package>/`; over the whole tree that run fails for the setup's own reasons
  (a uid with no account, files it cannot read), so it reproduces a failure and is not a gate.
  A chown to another user is `project_test_chown`'s, not this one's. The assumption
  is in what the fixture or the call does not say, so no syntax tree shows it. What holds it:
  the code takes the owner it expects as a value (the guest root's host uid and gid, 0 and 0 in
  production and the fixture directory's owner in a fixture; `homefs.Owner`, the caller's
  effective uid in production), the tests give it their own, and CI's unprivileged user is the
  detector. Whether the agent containers also get a gate that runs the Go tests as an
  unprivileged user is the owner's question on PR 124.

- **A test that needs a kernel facility a restricted runner denies, and fails instead of
  skipping** (found by the same CI run, the one test left failing after the root-only causes were
  fixed). `guestroot`'s `TestWriterRefusesABindMount` makes a mount in a user namespace of its own,
  through `reexec.StartInUserNamespace`, which skipped a kernel that refuses the namespace but not
  one that allows it and refuses the mount in it (`bind-mount: permission denied`, consistent with
  an AppArmor-restricted runner; the container where it passed does not restrict it). Whether a
  kernel allows a call is a fact about one host, not a shape in the syntax tree, and the tests
  that use a mount are one. What holds it: the one seam. A helper that finds its environment
  unable to do its work ends with `reexec.ExitSkip` and `StartInUserNamespace` skips the test
  showing what the helper said; any other early end, a signal and a failure to run still fail it
  (`TestOnlyTheSkipExitIsASkip`). The cost is that where the runner denies the mount, CI never
  runs that test: it holds that a write through a bind mount is refused by the kernel
  (`RESOLVE_NO_XDEV`), which `TestMkdirAll` shows only by the flag it passes. Whether CI should
  allow the mount (a runner setting) or the skip is accepted is the owner's question on PR 124.

- **A test fake that keeps fewer rooms than the code under test uses** (Plan 00209 n3f-b3: the
  engine's fakehs fixture kept only main's id of the rooms `rooms.Converge` made, so no engine
  test could post to, read or recover in the reports room on a homeserver, and the recovery of
  a tried report went unpinned). Which of a fake's arguments matter is what the tests will ask of
  it, not a shape in the syntax tree: a fake `RoomState` that answers nothing for every room is
  correct where no test reads a room's state, and the same body is the defect where one does. A
  narrow rule (a `rooms.Recorder` whose `Record` keeps some purposes) would catch only the one
  shape already fixed. What holds it: the recorders keep every purpose (`roomRecorder` in
  `engine`, `hsRecorder` in `client`, `recorder` in `teamsync`, `ends` and `wiring`), the engine's
  fixture fails when Converge made no reports room, and the sends' fake keeps the room of every
  send (`fakeNotices.rooms`). Swept in the member packages' tests: the stubs that still take no
  room (`fakeNotices.RoomState` in `engine`, and `RoomState`, `Messages`, `Send` in `link`) answer
  nothing for every room and back no assertion on one. A second instance, Plan 00265 R68: the
  engine's `fakeTeam.Refetch` took no room and answered an id from every room, so no test could
  see `reply` look only in main for a questions post. It now records each fetch's room
  (`refetchedFrom`) and can hold an id in one room alone (`heldIn`), the homeserver's 404 for the
  others. `link`'s `fakeTeam.Refetch` still takes no room; no link test asks which room was read.

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

- **A size or a cap computed from another encoding than the one written** (Plan 00209, the seat
  store): teamchat's `member/state` sized each store entry in A's compact form
  (`proto.Value.Size`, which writes `<`, `>`, `&` as one byte and U+2028 as three) and wrote the
  file with encoding/json, which writes each as a six-byte escape, so a send of 16 373 `<` passed
  the 20 KiB entry rule and was written as 101 263 bytes; 32 of them were refused only by the file
  cap, which n3d-D1 promised never fires first. The sweep found the same defect in the awaiting
  list's transcript path, `MaxRosterCache` (a Matrix event's 65 536 canonical bytes, written up to
  six times as large) and `MaxSession`. Its forms are many: compact against escaped JSON, a Go
  `len` against a JSON length, characters against bytes, runes against bytes. No rule can be
  written: which call measures the value a later write writes, and in which encoding that write
  is, is the data flow between two places the syntax tree does not join (the size is taken on a
  `proto.Value`, the bytes come from a `json.Marshal` of a struct elsewhere), and a rule that
  flagged every `Size()` or `len` near a cap would flag A's own wire caps, which are right
  because both ends measure the compact form. What holds it: one encoder per writer, and every
  size a rule takes measured through it (`member/state`'s `marshal` and `writtenSize`; the
  delivery cap is `proto.WireBytes`, the same escaper the socket line is written with), and a
  table test per file over every capped field enumerated from the code
  (`store_written_test.go` walks `listCaps` and the `Store` type, so a list or key added later
  fails until it has a row; `files_written_test.go` for the roster cache and session.json),
  filling each with `<`, `&`, `"`, `\`, controls, multibyte UTF-8 and U+2028.

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
  and tested the same way. The other half is the reader: a test that has seen only the action
  (a read to EOF after the fake closed) has not seen the announcement, so it awaits the
  announcement before it reads (`fakeinbox`'s own refusal tests read the frames straight after
  `send` and failed in 32 of 900 stressed runs until they awaited with `settled`; run under
  `-cpu 1,2,4`, 300 times, the old form fails and the new one does not). No rule: every shape
  that reads after a close also matches the many reads that are ordered.

- **A test hook that announces an event and then reads the control state its test sets once it has
  seen the announcement** (Plan 00209 n3f-c1c: `teamsync`'s `gateFilter` held the team's next
  request whose filter was the coordinator's. Its hook called `announce(f)`, then looked at the
  channel `armed()` fills. `readyWith` returns when it has seen that announcement, and
  `TestQS6AnOldHumanPostIsStillDelivered` arms the gate straight after, so under load the arm
  landed before the hook's look: the request in flight, the one the test's post was to answer, was
  held, and the test waited for a batch that request had to make. 29 minutes at `[select]` in CI,
  PR 175, run 42176134603; locally 1 hang in 150 runs at `-cpu 1,2,4` with the machine busy.) A
  100 ms pause after the announcement made it hang on every run. Whether a hook's later read is a
  control the test writes after the announcement is the order of two goroutines' steps, not a
  shape in the syntax tree: every hook that announces first also matches the many that read
  nothing the test writes. What holds it instead: a hook decides, then announces, then blocks
  (`gateFilter` takes the arm before `announce`, so an arm made after the announcement is for the
  request after it), and the same pause after the announcement, run against the test, shows a
  hook that does not (the old form hangs, the new one passes). Swept in `teamsync`'s tests: the
  other hooks (`gated`'s and `held`'s `onPoll`, `watchReports`, the budget and receipt waits)
  announce nothing before they decide, or decide on nothing the test writes.

- **A test that forgets a stream of announcements after the act that makes more of them** (Plan
  00209 n3f-c1d: `TestQS5AQuestionsFloorMoveIsMarkedInTheQuestionsRoom` posted to the questions
  room, called `drainPolls()` to forget the long-poll `readyWith` left, then waited for the batch
  and for the team's next long-poll. The post ends the poll that was in flight and the team goes
  on to its next at once, so when it announced that one before the drain ran, the drain took it
  too and the wait for it was for ever: 29 minutes at `[select]` in CI, PR 178, run
  42179368091, the team parked in a held `/sync` of the fake server.) A 100 ms pause between the
  post and the drain made it hang on the one run made. `TestQX4AFailedWriteLeavesTheLiveIndexUnfolded`
  had the same order. No rule: which acts cause an announcement, and which announcements a
  later wait must still see, are what the code under test does, not a shape in the syntax tree;
  a rule on "a drain after a post" would flag the gate tests, whose drain follows a post while
  the team is held in a request and so cannot announce. What holds it instead: forget the stream
  before the act (`drainPolls()` first, then the post), so everything announced after the drain
  is the act's; the same pause after the post, run against the test, shows an order that does not
  (the old order hangs, the new one passes, 150 runs at `-cpu 1,2,4`). Swept in `teamsync`'s
  tests: the other uses of `drainPolls` are the gate tests' (the team is held when it runs) and
  `TestQS8ATrustTriggerInTheRoomReVerifies` (before its act).

- **A fake that does not keep the contract of the interface it stands for** (Plan 00209 n2/n3e
  merge: `units.Conn` says the connection closes the signal channel it was given when it is
  closed, and n3e made `session.close` wait for `dispatch`, which that close ends; n2's
  `busConn` in `admin/wire` ignored `Signal` and its `Close` did nothing, so the first refused
  `StopUnit` hung the test binary for its whole 20 minutes). The contract is prose on the
  interface, so no syntax tree says which method must do what, and the fake that breaks it
  compiles. What holds it instead: each fake of `units.Conn` does what the interface comment
  says (`units`' `fakeConn` records the channel and closes it once; `busConn` now does the
  same), and a test of a new fake runs the Manager's failure path against it, under
  `-timeout` short enough to show a hang in seconds.

- **A wait on a real process whose bound is shorter than the worst case of the schedule it waits
  on** (fedora-desktop#140: `TestE2EQueuedAtStart`, `TestE2EQueuedWhileDown` and
  `TestE2EOfflineServer` waited `settle` (30 s) for a team to show down, and a second wait of
  `backoffCap + settle` (60 s) for a queued send to be posted once the server was back). The
  link marks a team down at the first failed retry once the failure streak is 15 s old, and its
  retries come after delays of 1, 2, 4, 8 and 16 s, each less a jitter of up to a quarter: the
  fourth retry falls short of 15 s and the fifth, 23 to 31 s after the first failure, is the one
  that marks it down. A bound of 30 s is shorter than that worst case, so on an idle machine
  one run in eight failed with `not within 30s` and an empty `link.log` (it logs only `down`
  and `back up`); the second wait chains the team's retry (up to 30 s) and its first long-poll
  (30 s in an idle room: a team that resumes from a stored position is shown up only when that
  answers, `teamsync`'s `TestABackTeamIsShownUpAtItsFirstLongPoll`), and 62.3 s was seen
  against 60. Nothing was lost: every queued send was answered `queued` and posted once. The
  test owns no event to wait on, because the clock is the link's own, in another process; the
  bound is the derived worst case (`downBound`, `upBound` and `deliveredBound` in
  `member/wiring/e2e_test.go`, each stating its derivation; the reverse wait of
  `TestE2EOfflineServer`, `backoffCap + settle`, had the same gap and now waits `upBound`). No
  rule: whether a bound covers a schedule is a fact of the production code's constants and its
  jitter, not a shape in the test's syntax tree; the `project_realtime_bound` rule already stops
  a test *shortening* a bound, and the fix for this class is to state the bound as the
  schedule's worst case.

- **A request's wait ending reported as a failure of the engine rather than as a bound that
  expired** (fedora-desktop#160: under heavy CPU starvation `status` answered 1 `internal`, "the
  link failed inside"). Each op the link's engine serves waits for the state goroutine
  (`Engine.do`) under the request's own deadline (2 s for status, inbox and recv). When the state
  goroutine does not take the step in time, `do` returns the context's error, and status, inbox,
  recv, verify, history, say, ping and reply handed it to `internal` or `doneOf`, which answered
  1; only wait (and some send steps) named it 4 `timed-out`. No rule: whether an error is a
  context's end is a fact of what a call returns at run time, through `do`, a team call or a
  helper that wraps it, not a shape a syntax tree shows; a rule on the call sites would also flag
  the record-write and framing errors that are the link's own. What holds it instead: the engine's
  one sink. `internal` (which `doneOf` falls back to) answers a context's end as 4 `timed-out`
  ("the link did not answer within the request's deadline"), so no op can answer it as 1;
  `TestAContextsEndIsNeverInternal` pins the sink and
  `TestARequestWhoseStepTheStateGoroutineDoesNotTakeTimesOut` holds the state goroutine and ends
  each served op's deadline only once it is blocked waiting (`leakcheck.Drive`'s step): red on
  8 of the 9 ops before the fix (wait was sound), green after. Swept and sound: the hooks' view
  (`link/hooks.go`) answers an empty hook answer on any engine error, by design; the client's
  `internal` answers are its own framing, connection and acknowledgement failures, and a client
  past its deadline sees the link gone (a 69 answer). Not closed: `ErrStopped` (a request that
  reaches an engine already shut down) is still 1 `internal`; it is not a wait ending and is not
  reachable by load. A new op added to `Serve` is covered by the sink, but the held-goroutine test
  lists its ops by hand.

- **A refusal state set from the answer to a request, without checking that the answer belongs
  to the credential held now** (fedora-desktop#159: `engine`'s sender set the team's send stop
  from a PUT's 401 or 403; a PUT made under the old token and answered after the team's
  recovery under a reissued one had cleared the stop set it again, over the new token, until
  the next health out of a stop). The answer outlives the credential it was made under. No
  rule: which field is a refusal state, which call is a request made under a credential, and
  which value is "the credential held now" are the program's meaning, not shapes a syntax tree
  shows (a rule keyed on the one field `sendStop` would be a check of this instance, not of
  the class). What holds it instead: the state carries the credential it applies to. The
  sender keeps the digest of the token a PUT was made under, and the stop is set only when it
  is the team's `sendsUnder`, which a recovery sets in the same state-goroutine step as it
  clears the stop (`TestARefusalOfAnOldTokenDoesNotStopTheNewOne`, red before the fix for a
  401 and a 403; `TestARefusalOfTheTokenTheSenderRecoveredUnderStops`). The sweep of the
  member's other refusals set from an answer found them sound: `teamsync`'s session stop
  re-reads the pair on a 401 and retries a changed one (n3c-D12); a verb's refusal is an
  answer, not a state; the receipts loop's 401 ends it only until the next session; the
  resolver's 403 and 404 are about the user and the event, which a reissue does not change.

- **A unit text no gate judges** (Plan 00246 INSTALL-PATH.md, stage A). The unit gates of
  `internal/teamchat/v1names` (D 4.2's six paths, an allow paired with its deny) judge the texts
  Go makes, and `TestEveryUnitTextSourceIsJudged` finds every Go file that makes one; a unit
  written as a static file or a Jinja template is not Go, so no analyzer sees it. What holds it:
  `TestNoTeamchatUnitIsAStaticFile` fails on any tracked file named as a teamchat unit or drop-in
  (also as a `.j2`), red on a tracked fixture and on a fixture checkout; the play installs what
  `teamchat-admin units print` writes (package `unitfiles`). Not caught: a unit text inlined in a
  YAML `content:` or a script.

Classes the bundled linters already hold, each seen firing on a minimal case in the audit: an
error or status thrown away (the bash's `|| true`, unchecked process substitutions and
`2>/dev/null`: nord B3, B11, reclaim B3, B6, open B5, B10, B11) is `errcheck`'s and `nilerr`'s in
Go, and the hooks' `R-ERROR-HIDING`; a path joined from a name the user gave (nord's
`connect ../x`, B2) is gosec's G304, and nord matches the name against the listing instead; a
shell template (open's `bash -c` handlers, B8) is `project_seam_commands`' `sh -c` finding.

### project_semgrep_test_runner

**What it is about.** `semgrep --test` does not pass `--timeout`, `--jobs` or any other limit on
to the scan it runs. In semgrep 1.177.0, pysemgrep's `test_main` calls `run_scan` with its
defaults, so every fixture is scanned with semgrep-core's `-timeout 5`: 5 s of wall time per
rule per file. No flag and no environment variable changes that. The osemgrep `test`
subcommand takes no limit either. On a loaded host a sound taint rule can run past 5 s. It is
then aborted and reports nothing, and its self-test fails in one of two ways:

- as a `Timeout`;
- as a "rule id mismatch", when another rule of the same fixture still reports. pysemgrep exits
  on the mismatch before it prints the Timeout, so the failure reads as a broken rule.

**Why it exists.** The real-semgrep tests of the semgrep gates failed only under a full `qa-all`
(`go test -race` over both modules in parallel) and passed when run alone (fedora-desktop#206).
Running 150 semgrep-core processes at once on `.semgrep/walk-open.py` gave these results:

- with `-timeout 5`, every process lost all 21 findings of `walk-opens-non-regular-file`, each
  with a `Timeout` error;
- with `-timeout 120`, every process found all 21.

The gates passed `semgrepreport.TimeoutFlag` to `--test`, where it had no effect.

**What it flags**: in a non-test file, a string constant `"--test"` anywhere in a function that
also holds the string constant `"semgrep"`. It also flags the constant `"test"` that follows
`"semgrep"` directly in a call's arguments or a composite literal. Constants are read by value,
so a named constant or an argv built in parts is still seen. The rule's own matcher is the one
row of `semgrepTestRunnerAllowances`. Not caught: an argv whose program name or flag is computed
at run time.

**How to fix a violation.** Prove the rule with `semgrepreport`'s self-test. It runs a plain
scan with `TimeoutFlag` and judges the report against the fixture's annotations, so the gate
sets the limit the rule is judged under. Any error in the report, a Timeout included, still
fails the gate.

- Red, fixture: `testdata/src/fix/internal/badsemgreptest` (`TestSemgrepTestRunner`). These
  cases are flagged: the flag in a call, in a slice literal, through a named constant, appended
  to an argv built apart from it, and the `test` subcommand. These are clean: a `scan`, another
  program's `--test`, and a target named `test` after other arguments.
- Red, real tree: the driver run over the `qa` module reported exactly the four sites below, and
  found nothing in the main module:
  - `ghscopes/scan.go:41`
  - `patterns/semgrep.go:52`
  - `readywait/scan.go:63`
  - `sttrules/rules.go:205`
- Green: the four gates prove their rules with `semgrepreport.SelfTest`. It ports semgrep's test
  runner (`test.py` in semgrep 1.177.0) and judges a scan run under `TimeoutFlag`. A fixture's
  annotations are read as that runner reads them, and the rules' `paths:` are set aside as it
  sets them aside; `bash-conventions` needs this, because its `include` globs keep rules off the
  fixture. Any error in the report fails the gate. The driver run then finds nothing in either
  module, and the rule is in `All()`. All nine commands' real-semgrep tests pass. One pass ran
  while the host was busy with a `qa-changed` Go check, and there the speech-to-text package
  took 163 s without a limit firing.

### project_container_pids_limit

**What it is.** An argument list (a `[]string` literal, or the arguments of one call) whose
verb is `"run"` or `"create"` and which holds a container flag but not all four flags of ccy's
limit set: `--cgroup-parent` (the `agents.slice` user unit, which caps every ccy container
together), `--pids-limit`, `--memory` and `--memory-reservation`. The message names the
missing ones. The verb is the first constant after any leading non-constant arguments (a
context, a program chosen at run time) and any `"podman"`, `"docker"` or `"container"`, so
`e.run(ctx, "run", …)`, `{"podman", "run", …}` and docker's `container run` are all read.
Flags are read by name: `-dit` is `-d`, `-i` and `-t`; `--rm=true` is `--rm`;
`--memory=8g` is `--memory`. The container flags are the attach and detach flags (`--rm`,
`-i`, `-t`, `-d`, `--interactive`, `--tty`, `--detach`) and the long flags only a container
takes (`--name`, `--entrypoint`, `--volume`, `--mount`, `--network`, `--env`, `--userns`,
`--security-opt` and the rest the doc string lists). The Go counterpart of the Semgrep rule
[ccy-container-default-pids-limit](QA.md#ccy-container-default-pids-limit), which reads only
bash; the class and its hazard are described there.

**Not caught.** A run whose only flags are short ones outside that set (`-v`, `-e`, `-p`,
`-w`, `-u`: `go run -v` takes them too); a run assembled from several lists where no literal
holding a container flag starts with the verb (`netpolicy`'s `create`, whose options and
limits come from the bash launcher, is one); a flag after the image, which is the contained
program's argument and not the engine's; and a value that turns a bound off (`-1`, `0`,
`max`) or names another slice, as values are not read.

**Proof.** Fixture `fix/internal/badpids` (`TestContainerPidsLimit`). Real tree, first form:
red on `ccc`'s session and sign-in runs as they were (`git show 0ad400860^:src-go/internal/ccc/run.go`),
then review round 4 widened the reading (the engine package's `e.run(ctx, "run", …)`, an
engine word before the verb, `create`, the long container flags, `-dit` and `--rm=true`).
The four-flag form: three new fixture cases (pids only, no `--memory-reservation`, no
`--cgroup-parent`) and every good case red until the rule required the set; over origin/F44
before the change, 16 findings (`engine.go` 163, 188, 201, 210; `ccc/run.go` 52, 85; and
their tests); on the branch, the sweep over the module with the default and `difftest` builds
is clean.

**How to fix.** Build the argv with `ccylimits.Limits.Run(flags...)`, which starts with
`run`, `--rm` and the whole set as the ccy launcher settles it (`ccylimits.Settle`).

### project_unit_no_install

**What it is about.** A systemd unit with no `[Install]` section is static: `systemctl enable`
(D-Bus `EnableUnitFiles`) succeeds and does nothing, `UnitFileState` stays `static`, and no
target wants the unit, so after a reboot it stays down until something starts it by hand. A
converge step that enables a unit whenever it is not `enabled` then reports the enable as a
change on every run.

**Why it exists.** `teamchat-server@.service` and `teamchat-tls@.service` had no `[Install]`: a
reboot of the bus machine left the team homeserver and its TLS terminator down (the health
tick only reports an inactive unit), and `teamchat team apply` printed `CHANGED unit enabled`
and `CHANGED tls unit enabled` on every run (fedora-desktop#247).

**What it flags**, outside test files: a unit text written in Go (a string concatenation with
its constant parts folded and every other part read as a line break, a string literal, or a
`[]string` literal of lines) that has a `[Service]` line, a `Description=` line and a
`Restart=always` line and no `[Install]` line. A text built on a constant that is itself
flagged is not flagged again, so a head constant and the function that appends the sandbox to
it are one finding.

**Not flagged**: a unit with `[Install]`; any other `Restart=` (a oneshot retried on failure is
started by its timer and is static on purpose); a text with no `Description=`, read as a
drop-in (`render.go`'s), whose base unit carries `[Install]` (systemd ignores `[Install]` in a
drop-in). The wider rule considered, any long-running `Type=` with no `[Install]`, was not built:
a service started on demand by a socket, a timer or another unit's `Wants=` is static on
purpose and carries no hazard. **Not caught**: a unit text read from a file at run time or
built with `strings.Builder` or `fmt.Sprintf`; unit files outside Go (under `files/`, inline
in a play, in `helpers/`), which the independent search read by hand and found none of.

**How to fix a violation.** Add `[Install]` with `WantedBy=` the target that should start the
unit (`multi-user.target` for a system service wanted at boot) and enable the unit, each
instance of a template. `units.Manager`'s `Enable` refuses a unit systemd says carries no
install information, so a static unit that reaches it fails the converge instead of
reporting a change that never happens.

- Red, fixture: `testdata/src/fix/internal/badunit` (`bad.go`: the homeserver's head constant
  built from constants, the terminator's concatenation with non-constant parts, two constants
  each harmless alone and joined, one literal, a list of lines, an `[Install]` that is only a
  comment; `good.go`: `[Install]`, a timer oneshot, a drop-in, `Restart=on-failure`, a text
  built on an installed constant; `text_test.go`: a test file, not judged).
- Red, real tree: `internal/teamchat/unitfiles/unitfiles.go:34` (`serverHead`) and `:103`
  (`terminatorUnit`), and nothing else over both modules with the default, `busfailpoints`,
  `tuwunel`, `ccyteamstest` and `difftest` tags. `host/lxc/unit` (the LXC seat's session
  unit, `[Install] WantedBy=multi-user.target`) is green.
- Green: both units end in `[Install] WantedBy=multi-user.target` (`installLines`), the
  homeserver's text now written where its `[Service]` header is (`serverUnit`), and the sweep
  over both modules and every tag is clean. `TestTheTeamUnitsAreWantedAtBoot` was red (2) before
  the fix; `units.TestEnablingAStaticUnitIsRefused` holds the seam (`ErrNoInstall`).

### project_secret_scrollback

**What it is about.** A terminal keeps what was written to its normal screen in its
scrollback, and tmux in a pane's history, for as long as the window lives. A secret written
there (a password "shown once", a token, a Wi-Fi key) is still there after the program has
moved on: for anyone who scrolls up, shares the screen, or logs the terminal or the pane.

**Why it exists.** Issue 255: ccy-teams showed a person's new Element password (`login.go`,
`IssuePassword` → `showLogin`), promised it was "shown this once and stored nowhere", and after
going back to the list the password was still in the scrollback.

**What it flags**, outside test files: a secret reaching a terminal write other than through
`term.SecretScreen`. A secret is the value of a source: a function in `secretSources`
(`agentbus.Client.HumanPassword`, ftp-camera's hotspot passwords, `ui.Lines.Secret`, ccc's token
readers, teamchat-ctl's admin password and token, vmtest's random token), any method named
`Reveal` returning a string (how `matrix.Token` and `seat.Secret` hand their value out), or a
field in `secretFields` (`ccylaunch.export.token`; `admin.work.doc`, teamchat-admin's member
answer, whose `bundle` carries the issued credential's secret encoded through `seat.Secret` and
`proto.Value`, which the rule does not follow). It is followed through assignments, `+`,
conversions, the `fmt.Sprint*`/`Errorf`, `errors.New` and `strings` functions that keep their
argument, `String`/`Bytes`/`Error` of a value holding it, a `bytes.Buffer` or `strings.Builder`
written with it, a field it is stored in (across packages, as a fact), a parameter of a function
of the same package and the result of a function returning it (across packages, as a fact). A
terminal write is `fmt.Print*`/`Fprint*`, `log`'s, `io.WriteString`, `Write`/`WriteString` on an
`io.Writer`, `os.Stdout` or `os.Stderr`, and any function, in any package, that passes a
parameter on to one (worked out as a fact: `term.Printer.Print`, `flow.Kit.Tell`, a package's
own printer); the call handing the secret to it is the finding. It also flags an
`io.MultiWriter` that sends a child's output both to such a stream and to a buffer a source
then reads a secret out of (ccc's sign-in, where `claude setup-token` prints the token).

**Not flagged** (narrowed, each because the hazard cannot arise there): a `Write` on an
`*os.File` other than `os.Stdout`/`os.Stderr` (a file the program opened: every `/dev/tty` the
Go tree opens is opened to read, by search); `term.SecretScreen.Show` and `Write` (the display).
**Not caught**: a secret read from a file or a child's output with no source between (add the
reader to the table); a secret printed by a child given the terminal directly; a struct holding
a secret printed whole with `%v`; a secret handed to a function value, an interface method, or a
function of another package that only stores it. The secrets bash, Python and Ansible print are
outside this rule's language (see the remediation record of issue 255).

**How to fix a violation.** Show the secret with `term.SecretScreen` (`flow.Kit.ShowSecret` in
ccy-teams) and `Close` the screen once the person is done with it: the terminal's alternate
screen (`ESC[?1049h` … `ESC[?1049l`) keeps no scrollback and tmux keeps it out of the pane's
history. A child whose output holds a secret is teed to the screen (`io.MultiWriter(screen, &buf)`) with the screen open around it. On a stream that is not a terminal `SecretScreen`
writes the secret plainly, as the payload its reader asked for (`ccy --export-token > file`).

**The next wider rule, not built:** every write of any string a function or variable named
`password`, `token` or `secret` holds. Not built: those names also hold file paths
(`paths.Seat.Token()`), token names and problem texts (`usageReport.readToken` returns why a
token file cannot be read), so the rule would flag code that carries no secret.

- Red, fixture: `testdata/secretscrollback` (`teams/login/bad.go`: the defect's shape, Tell,
  ReportErr of an error holding it, fmt to stderr and stdout, `os.Stdout.WriteString`, a field
  printed in another method, a buffer, a wrapper, `Reveal`; `ftpcamera/banner.go`: a package
  printer and a session field; `ccc/ccc.go`: the tee to an injected stderr and to `os.Stdout`;
  `good.go` and the screen forms clean; a test file not judged; `teamchat/admin/verbs.go`: the
  answer document written through the verb's own `answer`, and on the screen clean).
- Red, issue 261: `teamchat/admin/verbs.go:178`, `teamchat-admin member reissue`/`add`/`return`
  writing the document holding the issued credential's bundle to stdout, the one finding over
  the module once `admin.work.doc` joined `secretFields`. Green: `answerIssued` writes the
  document through `term.SecretScreen`, on the alternate screen and cleared on Enter when it
  carries a bundle and stdout is a terminal, plainly otherwise (the pipe into
  `teamchat seat place-issued` unchanged); `TestAnIssuedCredentialOnATerminalLeavesNoCopy`. The
  sweep over the module and the `busfailpoints` and `tuwunel` tags is clean.
- Red, real tree: five findings over both modules: `teams/login/login.go:229` (the defect),
  `ftpcamera/server.go:377` (the hotspot's Wi-Fi password in the session banner),
  `ftpcamera/wifipass.go:110` (a new hotspot password printed for the play),
  `ccylaunch/exporttoken.go:117` (`ccy --export-token`), `ccc/app.go:229` (the sign-in tee),
  at `ccee95cd5`; the independent search found the same five in Go and no other.
- Green: each shown through `term.SecretScreen`: ccy-teams' sign-in block on the kit's screen
  (`flow.Kit.ShowSecret`), left by the Enter after signing in or the paste's Enter
  (`SettleClipboard`), and no password is set where stderr is not a terminal; ftp-camera's
  Wi-Fi password on the screen before vsftpd starts, left on Enter, the banner naming the
  `nmcli` read instead; a new hotspot password and the export script on the screen on a
  terminal and plain when piped; ccc's `setup-token` output teed to the screen. The sweep over
  both modules and the `ccyteamstest`, `difftest`, `busfailpoints` and `tuwunel` tags is clean.
  `login.TestThePasswordIsShownOnlyOnTheAlternateScreenAndLeavesNoCopy` reproduces the defect.

### project_submit_unchecked

**What it is about.** Text typed into Claude Code's TUI in a tmux pane is submitted by an
Enter, and that Enter can be lost: it arrives before the TUI is ready, or while it is still
taking the typed text. The text then sits in the input box, the caller believes it was
submitted, and the session idles.

**Why it exists.** fedora-desktop#264: after an attended reboot `ccy-sessions` restore typed
`/compact This session was restored...` into six sessions; two kept it unsubmitted in the box
(`verify-restore`: `NOT-SET-GOING compact-not-started`) for over 30 minutes, and a second
Enter submitted it.

**What it flags**, outside test files: a function using both `tmuxccy`'s `Tmux.SendKeys` and
`Tmux.SendEnter` (called, or taken as a method value), other than `Tmux.Submit`; a `SendKeys`
whose text holds a constant newline or carriage return; outside `internal/tmuxccy`, every other
`Tmux.SendEnter` (a lone Enter: a dialog answered, a stuck line re-entered), called or taken as a
method value; and in `internal/tmuxccy` a function building text (`send-keys -l`,
`paste-buffer`) beside a submit key (`Enter`, `C-m`, `KPEnter`, `C-j`) as constants, other than
`Tmux.Submit`.

**Not flagged**: a lone `SendKeys`, and `Tmux.PressEnter`. **Not caught**: text typed through a
runner other than `tmuxccy`'s; an Enter sent by a raw `send-keys` outside `internal/tmuxccy`
(`project_seam_commands` keeps every tmux run there); `send-keys` text given without `-l`. The
shell and Python sides have no tmux submit (the independent search found `send-keys` only in
test suites). The ccy supervisor (`.claude/ccy/claude-supervise.py`, `_perform_injection`)
submits by a carriage return written to the child's PTY, not tmux: outside this rule's class;
its submits are re-checked by its own state machine (`mark_own_line_typed`, `WOULD_RESUBMIT`).

**How to fix a violation.** Submit with `tmuxccy.Tmux.Submit(ctx, name, text, SubmitPace)`:
it types the text, pauses, presses Enter, reads the box until it is seen empty or busy (a pane
gone counts as left) and presses Enter again while it still holds the text, up to
`SubmitEnters`; text still there is `tmuxccy.ErrNotSubmitted` (a `*NotSubmittedError` saying
what the box showed), which the caller reports as a failure, never as taken. A screen with no
box after an Enter is `tmuxccy.ErrBoxNotDrawn`, never taken (fedora-desktop#288). A pane in a
tmux mode sends every key to the mode while the box is read from the screen beneath, so a lost
line read as taken: `Submit` takes a pane out of copy or view mode before typing, and any other
mode, or one entered while the line is typed, is `tmuxccy.ErrPaneInMode` with no Enter pressed
(Plan 00257 wind-down triage). A lone Enter meets the same mode and is never typed through
`Submit`: press it with `tmuxccy.Tmux.PressEnter(ctx, name)`, which takes the pane out of copy or
view mode first, and returns `ErrPaneInMode` for any other mode, or an error for a mode it could
not read, with no Enter pressed. Count the dialog answered or the line entered again only when it
returns nil, and say in the failure what was not answered or entered. Routing every typed line
through `Submit` and every lone Enter through `PressEnter`, both of which this rule holds, is
what holds the mode class.

**The next wider rule, not built:** any text written into a TUI (a tmux pane or a PTY, in any
language) and submitted with one unchecked Enter. Not built: its one other instance, the
supervisor's PTY injection, already re-checks every submit through its state machine, and a
static rule would have to follow that state machine across a 10 000-line module; the owner
decides whether it is wanted.

- Red, fixture: `testdata/src/fix/sub/internal/tmuxccy` (a second submitter, a raw
  `send-keys -l` then `C-m`, one argv with `Enter`, `paste-buffer` then `KPEnter`; Submit, a
  lone Enter and Escape clean) and `fix/sub/badsubmit` (the defect's shape, a method value, a
  closure, `\n` and `\r` in the text; Submit, a lone Enter, a lone type and a test file clean).
- Red, real tree: three findings, `sessions/setgoing.go:377` (the defect),
  `sessclose/sessclose.go:426` and `teams/winddown/windstep.go:328`, at `43192125b`; the
  independent search found the same three Go sites (the last two re-checked the box on later
  polls) and no tmux submit in another language.
- Green: all three submit through `Tmux.Submit`; the sweep over the module is clean.
  `sessions.TestSetGoingPressesEnterAgainWhileTheCompactStaysInTheBox` and
  `TestSetGoingFailsASessionWhoseTextNeverLeavesTheBox` reproduce the defect;
  `tmuxccy.TestSubmit*` prove the retry, the bound and the failures.
- Red, lone Enter (Plan 00257, after review found the mode class only partly held): fixture
  `fix/sub/loneenter` (two re-Enters, the background-work answer, a method value; `PressEnter`
  clean) and `badsubmit`'s lone re-Enter, which the rule had left unjudged. Real tree: four
  findings, `sessclose/sessclose.go:637` (/exit re-Enter), `:746` (/compact re-Enter), `:847`
  (`answerBackgroundWork`, which marked the question answered after an Enter copy mode took) and
  `teams/winddown/windstep.go:317`, at `eb234303b`, over the module, the `qa` module and the
  four tagged builds; no other.
- Green: all four press through `Tmux.PressEnter`, each failure saying what was not answered or
  entered again; the sweep over the module and the tagged builds is clean.
  `tmuxccy.TestPressEnter*`, `sessclose.TestTheBackgroundWorkQuestion*AfterLeavingCopyMode`,
  `...UnderAModeItCannotLeaveStaysUnanswered`, and the `TestAReEnter*` tests of `sessclose` and
  `winddown` reproduce the defect.

### project_stdin_seam

**What it is about.** A read of stdin returns at end of input. When stdin is an open pipe that
nothing writes to and nothing closes, which is what an agent's shell gives a command run with no
input, the read never returns and the command says nothing (issue #260: `pingbus thread Q<n> --close` waited over two minutes). Stdin is read in one place, `internal/stdinread`, which says
on stderr once nothing has arrived for a second that it is waiting and how to end the input,
takes a bound after which it refuses (`GiveUpAfter`), and refuses a terminal for input that is
never typed (`RefuseTerminal`); and a person's answer is read only through `internal/ui`, whose
`Lines` waits on a piped answer with the same notice and bound (`stdinread.Await`), a line at a
time, and on a terminal for as long as the person needs.

**What it flags**, in every package of both modules but the stdin seams (`internal/stdinread`,
`internal/ui`, `internal/execx`, `internal/keyrelay` (the terminal relayed to a child until its
owner's stop pipe ends it, read only once poll finds a key), `internal/term`, `internal/ttyecho`) and a test double's (a
package whose name ends in `test`), `_test.go` files not judged: a read of stdin (`os.Stdin`, or
a variable, parameter or field named `stdin` or `Stdin` whose type has `Read([]byte) (int, error)`) by `io.ReadAll`, `ReadFull`, `ReadAtLeast`, `Copy`, `CopyN`, `CopyBuffer`,
`LimitReader`, `TeeReader`, `MultiReader`, `bufio.NewReader`, `NewReaderSize`, `NewScanner`,
`json.NewDecoder`, `csv.NewReader`, `xml.NewDecoder`, or its own `Read`, `ReadString`,
`ReadByte`, `ReadBytes`, `ReadRune`, `ReadLine` or `WriteTo`; and stdin handed on under another
name, which would hide its read: an argument to a reader parameter not named `stdin`/`Stdin`
(outside the seams), a variable or field of another name assigned from it, and a struct
literal's field of another name (but a seam's own type, such as `ui.Lines`' `In`). Passing stdin
along as stdin, asking whether it is a terminal, comparing it with nil and taking its descriptor
are not reads. A `Stdin` that is a string (a request's field) is not stdin.

**Not caught**: stdin returned from a function, or kept in a slice or map and read elsewhere; an
unkeyed struct literal; a reader built by reflection; Python, which the semgrep rule
`stdin-read-unbounded` covers. The next wider rule considered was a taint of stdin through every
value built from it (a `bufio.Reader` held and read later, a reader returned and read by the
caller); it was not built because the hand-off clauses already make every route out of a
function under another name a finding, and the sweep found no read the narrower rule missed.

**How to fix a violation.** Piped input: `data, err := stdinread.Read(stdin, stdinread.Options{Prog: "tool", Limit: n, GiveUpAfter: time.Minute})`, handing
`bytes.NewReader(data)` on if a callee wants a reader. A person's answer: a prompt through
`internal/ui` (`ui.Lines`), which bounds a piped answer.

- Red, fixture: `testdata/stdinseam/src/fix/internal/badstdin` (`bad.go`: the defect's own
  form, `io.ReadAll(io.LimitReader(e.Stdin, n))`, then `os.Stdin`, a parameter, a pointer's
  field, a scanner, a decoder, a copy, a full read, a method read, a tee, a multi-reader, and
  stdin handed to a reader parameter, a variable and two fields of other names; `bad_test.go`, a
  test that reads, clean), beside `goodstdin` (stdin passed along as stdin, asked about, given to
  a child, to the shared reader and to the ui seam, a string named `Stdin`, the descriptor:
  clean), the seams `stdinread`, `ui`, `execx` and `term` and the test double `teams/teamtest`
  (reading freely, clean), and `cmd/tool` (a command's main, flagged). The root is its own,
  because the seams are matched by package path. With the analyzer reporting nothing the test
  missed all 29 diagnostics (the 14 of `bad.go` twice, its package judged with and without its
  test file, and the command's one), and it passed once the rule was written. `keyrelay` (the
  relay seam, clean) and `badrelay` (the same relay outside it, flagged) were added with the
  relay seam.
- Sweep: committed red with 29 findings in 26 files across both modules; every one fixed, by
  kind: piped machine input read through `stdinread.Read` with the 60 s bound (teamchat's text
  verbs, guest facts and bundles, `place-issued`, element, the admin requests and declarations,
  lightning, the launcher's `--secret-screen` through `stdinread.Copy`, which streams, and in the
  `qa` module git's pre-push input and the branch-delete list through the twin
  `qa/internal/stdinread`); Claude Code's hook and status-line payloads with the notice alone;
  every person's answer through `ui.Lines` (ccc, cc-project-dir, ftp-camera, the Enter that
  clears a secret screen, `--build-lock`); stdin handed on renamed to `stdin` with its callee's
  read through a seam (guest, `placeissued.Place`, `teams/find`, `reexec`); and the ctrl+z
  guard's key relay moved, unchanged, into `internal/keyrelay`. Each entry point has an
  open-pipe test on a fake clock. Green: the rule reports nothing over either module.

### project_conversation_record

**What it is about.** The host reads a session's conversation to stop it (`ccy --stop`,
shutdown's close), restore it, restart it with its team and wind it down. v1's record is
`<checkout>/.claude/ccy/pingbus/sessions/<session>/conversation.json` (the pingbus hook's); a v2
session's is `<seat store>/sessions/<session>/conversation.json` (its link's), for a ccy and a
host cc session alike. Which one a session has is decided by the system its teams are live on.

**Why it exists.** Plan 00209 DESIGN-host-cc-v2.md section 4: `conversation.read` joined
`membership.SessionDir` and `File` itself, so every host reader (sessclose, restore and its check,
team restart, wind-down) knew v1's record only, and a v2 session was resumed stale, started fresh,
or judged against the wrong transcript.

**What it flags**, in the non-test files of every package but the record's owners
(`internal/teamchat/paths`, and `internal/teamchat/member` and below), anywhere but the function
`Locate` of `internal/conversation`: a constant string expression, judged whole, that is
`conversation.json` or a path ending in `/conversation.json` (reported where it is declared;
`conversation.File`'s own declaration is the one allowed); a use of `conversation.File`; a use of
`paths.Layout.ConversationFile` (a call, a method value, a method expression). The rule holds the
single-reader shape; that `Locate` knows both systems is held by `internal/conversation`'s own
table test ({ccy, cc} × {agent-bus, teamchat, mixed, no list} × {present, missing, wrong session
id, no transcript}), not by the rule.

**How to fix a violation.** Ask `conversation.Locate`, or read through `conversation.For`,
`Transcript` or `StartPlan` with `conversation.Here(getenv)`.

- Red, fixture: `testdata/conversationrecord/src/fix`: `internal/conversation` (the defect's
  `read`, joining `SessionDir` and `File`, and a second declaration of the name, flagged; `Locate`
  and `File`'s declaration, clean), `internal/badrecord` (a literal, a constant in pieces, `File`
  through another import name, `ConversationFile` called, held and as a method expression,
  flagged; its test, clean), `internal/goodrecord` (through `Locate`, other names, clean), the
  owners `teamchat/paths` and `teamchat/member/link` (clean) and `cmd/tool` (a path ending in the
  name, flagged). With the analyzer reporting nothing the test missed all 19 diagnostics (the
  packages judged with and without their tests).
- Red, real tree: one finding, `internal/conversation/conversation.go:97` (`read`'s join). The
  independent sweep (every `conversation.json`, `conversation.File` and `ConversationFile` in
  `src-go`) found the same one outside the owners and tests; `helpers/`, `files/` and `scripts/`
  hold no Go reader.
- Green: `read` goes through `Locate`; the five callers take `conversation.Here(getenv)`, and each
  caller's tests gained a v2 row (sessclose's through the real `Transcript`, restore's resume from
  the seat store, restart's and wind-down's v1 record read with a v2 record of the same name
  present).
- Not caught: a path built at run time from pieces that are not constant; v1's directory joined
  with a name read from elsewhere.

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

### project_floor_by_time

**What it is about.** Two Matrix events can share an `origin_server_ts`. A floor raised to the
newest event's millisecond and judged by `ts <= floor` takes every later event sent in that
millisecond as already seen, and drops it; a floor judged by `ts < floor` repeats the ones it
took. A wall-clock bound with no event behind it cannot name the events of its millisecond.

**Why it exists.** Plan 00209 R0 step 7: the questions room's floor skipped a human post whose
`origin_server_ts` was the floor's own millisecond, so an owner's answer posted in the same
millisecond as the post before it was lost; the coordinator test had to step the fake clock past
it.

**What it flags** (outside `_test.go`): in `internal/teamchat/member/...`, an ordered comparison,
or a `max`/`min` argument, that is an integer named for a floor (identifier, field, map read or
called function whose name holds "floor") or the `MS` of `state.Floor` outside `Floor`'s own
methods; anywhere in `internal/teamchat`, a `<=` or `>` of an integer named for a "since" with an
integer named for a time (ending `ts` or `ms`). A comparison with a constant is a validation and
is not judged.

**Scope, examined.** Outside `member/` a "floor" is an age threshold (`health`'s backup age,
`status`'s event prune) or a question number (`proto`'s `QuestionFloor`), none an event cut. A
since compared with a stream position (`fakehs`'s `pos`) is the right form. Wider rule considered:
every ordered comparison of two event times in teamchat; not built, because a tie between two
times is the hazard only where one of them is a cutoff, and the rest are ages and deadlines.

**Not caught:** a floor held under a name without "floor" and compared as a bare integer, a
cutoff judged through a helper that takes the integers apart from their names, and a
`state.Floor{MS: t}` built with no IDs from a bare time, which covers the whole millisecond. One
such floor is left, referred to the owner: `teamsync.qindex` floors the questions room at the
member's own join as its whole millisecond, because which events of that millisecond came before
the join is a stream order the floor cannot name; the narrower `state.Before(join)` took
pre-join posts as items and broke `TestQX7ARedactionMetAfterTheBuildsStartBuildsAgain`.

**How to fix:** hold the floor as a `state.Floor` (the millisecond and the event IDs at it already
taken), judge by `Covers(ts, id)`, raise by `Raised` and `Max`; a wall-clock since keeps its
boundary millisecond (drop by `ts < since`).

- Red, fixture: `testdata/floor/src/fix/internal/teamchat/member/badfloor` (the originating
  qtake, a max raise, floor against floor, a struct field on the right, a map read, a call, a
  `Floor`'s `MS` taken apart) and `.../admin/badsince` (both orders); `Floor`'s own methods,
  `Covers`, `Max`, equality, a constant, a since cut by `<`, a position, an age floor outside
  `member`, a test file and `floorelsewhere` clean.
- Red, real tree (the defence commit, the driver as `go vet -vettool`, untagged and with
  `busfailpoints,tuwunel`): 19 findings, in `teamsync` (builder, questions, relay, session),
  `engine` (recv, storewrite) and `admin/serverestore.go`. The independent search found the same
  set, and `notice.go`'s `ts < SinceMS` (which keeps its boundary, clean).
- Green, the fix commit: 0 findings, both tag sets. The floors are `state.Floor` (store
  `floor_ids` beside `floor_ms`, the marker's optional `floor_ids`), "just below an owed item" is
  `state.Before(sent)`, and the restore export keeps since's millisecond. The original:
  `teamsync` `TestAnAnswerInTheQuestionsMillisecondIsDeliveredAndRelayed`, red at the defence
  commit (the answer neither delivered nor relayed), green after.

### project_record_file_by_name

**What it is about.** The ccy session registry holds one file per session record. Its writer
names the file after the record's `name=`, but nothing holds a record to that: a record edited or
renamed by hand lives in a file of another name. The registry's reading (`Store.ReadAll`) carries
the file each record was read from (`Reading.Files`, fedora-desktop#396).

**Why it exists.** fedora-desktop#398, after #396: ending a session in the `ccy-sessions` picker
removed `<registry>/<name>`, so a record in a file of another name survived as a stale record
that failed every later restore; the restore's and `declare`'s refusals said "the record is kept
at" a file that was not it; and the restore handed the trampoline, which removes a session's
record when it ends, the same wrong file.

**What it flags**, in the non-test files of every package but `internal/registry`: a use of
`registry.Store.Remove` (the name-keyed removal: a call, a method value, a method expression); a
path built in the registry directory, by `filepath.Join` or `path.Join` with two or more elements
whose first is the directory, or by a `+` whose leftmost operand is. The directory is a call of
`Store.Dir` or of the package's `Dir`, or a variable, package variable or struct field assigned
one (`:=`, `=`, `var`, a struct literal), followed through further assignments to a fixed point.
Every file in the registry directory is read as a record, so no other path belongs there.

**Scope, examined.** Wider rule considered: every name-keyed lookup of a record (`Store.Get`,
`SetRestore`, `SetTeams`, `registry.ReadStop`), so that a session's record is only ever found by
scanning the records' `name=`. Not built: `Get` is itself a reading (it reads the file named after
the name, and its caller knows that file), the launcher and `ccy-teams` hold only a session's name
and the writer's naming is the key they share with the bash library, and a miss there is a loud
"no record", not a wrong file. The stop file (`registry.StopFile`) is keyed by the record file's
name on the trampoline's side and by the session's name on verify-restore's; that is the same
mismatch one file over, and is referred to the owner (it needs the manifest to carry the file).

**Not caught:** the directory passed as a parameter and joined in the callee; a path built by
`fmt.Sprintf` or `strings.Join`.

**How to fix:** read the registry and use the file each record carries: `Reading.FilesOf` for the
files answering to a session's name, `Store.Find` for the one record of a session and its file;
remove with `Store.RemoveFile`, or `Store.RemoveSession` for every record of an ended session.

- Red, fixture: `testdata/recordfile/src/fix`: `internal/badrecordfile` (the picker's `Remove`,
  `Remove` as a method value and a method expression, the restore's join of `registry.Dir`'s
  result and `rec.Name`, the declare join of `store.Dir()`, the directory in a field set by a
  literal (`path.Join`) and by an assignment (`+`), in a package variable, and relayed through two
  locals: 9 findings), its test (clean), `internal/goodrecordfile` (the file the reading carries,
  `RemoveFile`, the directory alone, its parent, a one-element join, another package's `Remove`
  and `Dir`, the writer: clean), and the registry itself (clean). With the analyzer reporting
  nothing the test missed all 18 diagnostics (the package judged with and without its tests).
- Red, real tree (the defence commit, the driver as `go vet -vettool`): 9 findings:
  `sessclose.go:920` (End's `Remove`), `sessions/picker.go:313` (End's `Remove`),
  `teams/app.go:425` (the `ccy --teams` launch's `Forget`), `sessions/declare.go:133`,
  `sessions/restore.go:306` and `:510` (the joins), `teams/restart/jobs.go:300` (the restarted
  session's trampoline file), `teams/board/collect.go:311` and `teams/hot/list.go:368` (an
  unreadable record's file, rebuilt from `List`'s names). The independent search made before the
  rule ran (every `Remove(`, `Join(` and `+` of a registry directory, and every holder of
  `registry.Dir`, `Locate` and a `Registry()` store in `src-go`) found the same 9.
- Green, the fix commit: 0 findings. The name-keyed `Store.Remove` is deleted (no caller was
  left); an ended session's records go by `Store.RemoveSession` (the files `Reading.FilesOf`
  names), the restore and its check take `Reading.Files`, restart takes `Store.Find`'s file,
  declare names only a record that is there, and `Listing.UnreadableFiles` carries paths. The
  original: `sessions` `TestEndingASessionRemovesTheFileItsRecordWasReadFrom`,
  `TestARestoreRefusalNamesTheFileTheRecordWasReadFrom`,
  `TestARestoredSessionIsHandedTheFileItsRecordWasReadFrom` and
  `TestADeclareRefusalNamesOnlyARecordThatIsThere` (each a record whose file is named otherwise
  than its `name=`), red at the test commit, green after.

### project_walk_entry_type

**What it is about.** A directory listing holds whatever is in the directory: directories,
regular files, symlinks (broken ones too), sockets and pipes. Code that takes every entry for a
folder and reads a file inside it fails on the first entry that is not one.

**Why it exists.** fedora-desktop#404: `ccy-teams seats abandon` checks every session's
`desired.json` under the seat store's `control/` (`knownBy` in `internal/teams/seatsweep`). The
launcher also keeps a regular file there, `disconnect.log`; the scan read it as a session folder,
the read failed as an unreadable source, and every run refused with 78 unless
`--allow-known-team` was given, which made the refusal meaningless.

**What it flags**, in non-test files: in a range over a slice of `fs.DirEntry` or `fs.FileInfo`
(whatever produced it), a file system call (`os.Open`, `OpenFile`, `ReadFile`, `Stat`, `Lstat`,
`ReadDir`, `Readlink`, `Chdir`, `DirFS`, `ioutil.ReadFile`/`ReadDir`, `fs.ReadFile`/`ReadDir`/`Stat`,
`filepath.Walk`/`WalkDir`) given a path beneath the entry, or a directory reader given the entry's
own path, when the loop body never asks the entry's type. The entry is followed through every
value the body builds from it (a call of any package, so a path built by another package's helper
from a parsed name counts; a local; a `+` chain), but not into a bool, a number, an error or a
byte slice. The entry's own path is a `filepath.Join`/`path.Join` whose last element is its
`Name()` and whose others are not built from it, a `+` ending in its `Name()`, or a local holding
one; anything else built from it is beneath it. Asking the type is a call of the entry's `IsDir`,
`Type`, `Info` or `Mode`, or `os.Stat`/`os.Lstat`/`fs.Stat` of its own path.

**Scope, examined.** The guard is the loop, not the branch: a loop that asks the type anywhere is
clean, so a check whose answer is used wrongly is not caught (a guard-precise rule, like
`project_walk_open`'s, was weighed against the one instance found and not built). The false
positive is a path derived from the name that is not beneath the entry (a sibling such as
`<name>.lock` built from a trimmed name): flagged, and fixed by asking the type, which costs one
call. Opening the entry itself is `project_walk_open`'s concern and not judged here.

**Not caught:** a range over names only (`Readdirnames`, `Glob`); an entry indexed
(`entries[i]`) rather than ranged; the entry handed to a function of the package that builds the
path and reads it; a value built from the entry kept outside the loop and used after it.

**How to fix:** skip or refuse the entry unless its type is a directory (`if !e.IsDir() { continue }`), or `os.Stat` its own path to follow a symlink and ask the target's type.

- Red, fixture: `testdata/walkentry/src/fix`: `internal/badwalkentry` (the defect: a name parsed
  and the path built by another package's method, then `os.Lstat`; one `Join` with a further
  element and `os.ReadFile`; `os.ReadDir` of the entry; a local relaying the entry's path from
  `(*os.File).ReadDir` to `os.Open`; `Readdir`'s `FileInfo` through `fs.ReadFile`; a `+` chain over
  a listing a function returns: 6 findings), `internal/goodwalkentry` (`IsDir`, a switch on
  `Type`, a `Stat` of the entry's own path, the entry opened as a file, names only, a range over
  strings, a test: clean). With the analyzer reporting nothing the test missed all 6 diagnostics.
- Red, real tree (the defence commit, the driver as `go vet -vettool`, both modules and every
  build tag set): 1 finding, `teams/seatsweep/abandon.go` (`knownBy`'s `os.Lstat` of
  `store.DesiredFile(session)`). The independent search (every `Join` with an element after a
  `.Name()` in `src-go`) found one more loop, `tuwunelhs.processes`, which asks `e.IsDir()` first
  (clean).
- Green, the fix commit: 0 findings. `knownBy` takes only directories of `control/` as session
  folders, skips regular files, follows a symlink and judges its target the same, and refuses
  anything else (a broken link, a socket, a pipe) with its path. The original:
  `seatsweep` `TestAbandonControlFiles`, red at the test commit, green after.

### project_foreign_strict_parse

**What it is about.** `proto.Parse` is the Matrix wire's parser: no repeated key, integers only
within ±2^53-1, valid UTF-8, no lone surrogate escape, nothing after the value. Those are
teamchat's rules for its own documents. A foreign producer (Claude Code's hook payload) follows
none of them for the fields teamchat never reads, so one such field anywhere in its document
refuses the whole of it, and the fields teamchat does read are lost with it.

**Why it exists.** fedora-desktop#394: `teamchat hook` parsed Claude Code's hook payload on
stdin with `proto.Parse` to read three strings. Every SessionStart payload Claude Code wrote was
refused as `json`, so the link never sent `started`, the keeper saw Claude Code end "before its
first SessionStart" and exited 78 at every `/exit`, and the seat was marked launch-failed.

**What it flags**, in non-test files: a call of `internal/teamchat/proto`'s `Parse`, or of any
function that hands a parameter on to it (a fact, so a wrapper in another package, `proto`'s own
included, counts, through any number of calls), whose argument holds stdin or an environment
variable. Stdin is the result of `internal/stdinread`'s `Read` or `ReadLine`
(`project_stdin_seam` holds every read of stdin to that package, so this is every stdin); an
environment variable is `os.Getenv`, `os.LookupEnv`, `syscall.Getenv` or a call of a function
value named `Getenv` or `LookupEnv`. A value holds one when it is built from one: a local
assigned an expression naming it, a conversion, a function of `bytes` or `strings` given it, a
closure, a function whose result holds one (a fact). Only strings and byte slices carry it.

**Scope, examined.** Not judged: `proto.ForeignObject` (the remedy, whose parse of a key's value
is strict on purpose) and stdin (only stdin: an environment variable there is still judged) in
the packages whose stdin is a document teamchat specifies and itself writes, each decoded to its
every key (`protocolStdin` in the rule, the producer as the reason):
`teamchat/element` (the Element play's EL1 team list), `teamchat/member/guest` (the host's
teamchat over lxc-attach), `teamchat/member/placeissued` (teamchat-admin's issued seat) and
`teamchat/admin` (ctl-child's request from the root parent). There a field nobody reads is a
defect of the producer, and the strict refusal is the protocol's own. Wider rule considered:
every `proto.Parse` whose input teamchat did not write itself, files included (a transcript,
`settings.json`, a foreign program's output). Not built: a path does not say who wrote the file,
so the rule would need a list of every file teamchat owns, and no such parse exists today (the
independent search found the transcript read as bytes and Claude Code's settings and status-line
payload read by `encoding/json`).

**Not caught:** a file a foreign program writes; a foreign program's output; a socket's answer;
a value kept in a struct field or a map and parsed elsewhere; a value handed through a function
of the program that returns it changed; a function value.

**How to fix:** decode the object with `proto.ForeignObject`, naming the keys read: it must be
one JSON object, each key read is parsed strictly and refused if repeated, and every other
member is skipped whatever its form.

- Red, fixture: `testdata/foreignparse/src/fix`: `internal/badforeign` (the defect: stdin
  straight to `Parse`; a trimmed copy through a helper of the package; another package's
  wrapper; `proto`'s own wrapper and a two-call relay inside `proto` given a line of stdin; an
  environment variable by `os.Getenv`, `os.LookupEnv` and an Env's `Getenv`; stdin handed back by
  a function of the package and by another package's method; a closure: 11 findings), its test
  (clean), `internal/goodforeign` (`ForeignObject`, teamchat's own file and socket, a length of
  stdin, a constant beside an environment variable: clean), `internal/teamchat/element` (a
  protocol-stdin package: its stdin clean, an environment variable parsed there 1 finding), and
  the facts of `proto`, `wrap`, `stdinread` and
  `badforeign`. With the analyzer reporting nothing the test missed all 12 diagnostics.
- Red, real tree (the defence commit, the driver as `go vet -vettool`, both modules and every
  build tag set): 1 finding, `teamchat/member/hook/hook.go` (`payload`'s `proto.Parse` of the
  hook's stdin). Before `protocolStdin` held them, the rule also found the four protocol-stdin
  packages (6 call sites); each was read to its producer. The independent search made before
  the rule ran (every call site of `proto.Parse` and of each function reaching it in `src-go`,
  each traced to its bytes' producer) found the same one foreign instance.
- Green, the fix commit: 0 findings (both modules, every build tag set). The hook decodes the
  payload with the new `proto.ForeignObject`, reading only its fields. The original: `hook`
  `TestAFieldTheHookDoesNotReadRefusesNothing` and `TestAFieldReadTwiceIsRefused`, red before
  the fix, green after; `proto` `TestForeignObject*` hold the decoder.

### project_selector_parse

**What it is about.** Claude's conversation selectors in a recorded launch are `-c`/`--continue`,
`-r`/`--resume` (with or without an id), `--resume=<id>`, `--session-id <id>` and
`--session-id=<id>`. `-r` and `--resume` take an optional value: a bare `--resume` opens Claude
Code's picker, and the word after it is its id only when it is not a flag. `registry.selectorOf`
is the one parser of that grammar; every reader of the selectors asks it.

**Why it exists.** fedora-desktop#411: `registry.ResumeID` read the selectors for itself and took
the word after `--resume` whatever it was, while `withoutSelectors` (through `selectorOf`) took it
only when it was not a flag. A session launched as `ccy --resume --teams <seats>` (a supported
launch since owner ruling Q70) was put in the restore manifest by `ccy-sessions restore` as
resuming `--teams`.

**What it flags**, outside `_test.go`, in `internal/registry` and every package that imports it
(the readers of a session record's launch words): a constant string that is a selector (or the
prefix `--resume=` or `--session-id=`), matched by value so a named constant counts, used as an
operand of `==` or `!=`, a case of a tagged switch, or an argument of a `strings` or `slices`
function, anywhere but `selectorOf`. A selector spelled to build arguments (`ResumeArgs`'s
`--resume`) is not a read and is not judged.

**Scope, examined.** A package that does not import the registry parses some other command line:
`ccc`'s own `--continue` (ccc's flag, not a recorded launch), `teamchat/member/cli`'s refusal of a
resume flag in claude's CMD (presence only, no value read). Wider rule considered: every argv
reader in `src-go` that takes the word after an optional-value option without asking whether it
is a flag. Not built: which options take an optional value is not in the code to read (the
launcher's own value flags take the next word whatever it is, by design), so such a rule would be
a list of option names, which is this rule. The bash twin `ccy_registry_resume_id` is beyond a Go
analyzer; `TestResumeIDAndSupervisedGolden` runs every golden row through both, so it cannot
disagree with `ResumeID`.

**Not caught:** a selector held in a variable or a slice and compared through it; a map keyed by
selectors; a package handed launch words as a `[]string` that does not import the registry.

**How to fix:** ask `selectorOf` (or a registry function built on it), and add any new selector
there.

- Red, fixture: `testdata/src/selector`: `internal/registry` (`ResumeID` as it was: 4 findings;
  `selectorOf` and a built `--resume` clean), `badreader` (`==` both ways round, `!=`, a tagged
  switch with a named constant, `slices.Contains`, `strings.CutPrefix`: 7 findings; a test file and
  an unrelated word clean), `ownflags` (its own `--continue`, no registry import: clean). With the
  analyzer reporting nothing the test missed all 11 diagnostics.
- Red, real tree (the defence commit, the driver as `go vet -vettool`, both modules, untagged and
  `ccyteamstest`): 4 findings, all in `registry/restore.go` `ResumeID` (lines 247-249). The
  independent search made before the rule ran (every reader of a selector in `src-go` and the
  launcher's bash) found the same Go instance and its bash twin.
- Green, the fix commit: 0 findings (both modules, untagged and `ccyteamstest`). `selectorOf`
  also returns `--resume=<id>`'s id, and `selectorValue` is the one "is the next word a bare
  `--resume`'s id" test that `withoutSelectors` and `ResumeID` share. The original:
  `registry` `TestResumeIDAndSupervisedGolden` (`{--resume --teams x} -> ""` and five more rows,
  Go and the bash twin) and `sessions` `TestABareResumeLaunchIsNeverResumedAsTheFlagAfterIt`
  (the manifest named `--no-ssh` before the fix), green after.

### project_refusal_reason_text

**What it is about.** A teamchat send refusal reaches the agent as an error object whose `code`
is the refusal's reason (`ref`, `schema`, `target`) and whose `message` is what the agent reads to
decide its next step. A message filled with the reason repeats the code and says nothing more.

**Why it exists.** fedora-desktop#421: answering a `fetch` ping with
`teamchat reply <id> --verb done` and no `--ref` printed "the send was refused (ref); fix the
message". The reference was missing, and `done` needs one (normally the ping's own); the message
said neither, and every send refusal but four shared that template.

**What it flags**, in `internal/teamchat`, `cmd/teamchat`, `cmd/teamchat-admin` and below,
outside `_test.go`: a read of `.Reason` on a `proto.RefusalError` (pointer or value), or a local
variable a read was assigned to, inside an argument of a function of package `exits` (`New`,
`Format`, `Words`, `Name`, `Join`, ...), except the parameter named `code` (`New`'s, `ExitOf`'s),
where the reason is the right value. A nested `exits` call is judged on its own.

**Not caught:** a reason copied into a struct field, or passed to another function, and read
there; a message built outside package `exits` from the reason. The second half of the defect,
a refusal's `Detail` (its cause) dropped on the way to the message, is not a syntax a rule can
see; `engine`'s `TestEverySendRefusalSaysItsCause` holds that every reason in
`proto.SendRefusals` has its own message with no `(<reason>)` in it.

**How to fix:** choose a template by the reason, and by the refusal's cause where it has one (a
reference refusal's `proto.RefCause*`), that names what is wrong and what to do.

- Red, fixture: `testdata/refusaltext/src/fix/internal/teamchat/badrefusal` (`Words(re.Reason)`
  as a fill, a concatenation in `Name`, a local copy by `:=` and by `=`, a value refusal in
  `Format`: 5 findings; the reason as `New`'s and `ExitOf`'s code, a switch on it, a template
  chosen by it: clean), beside `outsiderefusal` (outside teamchat, clean). With the analyzer
  reporting nothing the test missed all 5 diagnostics.
- Red, real tree (the driver as `go vet -vettool`, `./internal/teamchat/...`, `./cmd/teamchat/...`,
  `./cmd/teamchat-admin/...`): 3 findings, all in `engine/send.go` `refusal` (the `role`
  template and both uses of the generic `msgRefused`).
- Green, the fix: 0 findings untagged, with `busfailpoints` and with `tuwunel`. `refusal` moved to
  `engine/refusal.go` with a template per reason; the composer's reference refusals carry their
  cause (`proto.Roster.RefCause`), and `reply --verb` and `ping` name the verb, how a reference
  is given, the forms the verb takes and, for an answer to a fetched ping, the ping's reference.

### project_question_unresolved

**What it is about.** Which question of the questions room an event is in (a question root, a
human's post in a question's thread, a reply to a thread post, a `Q<n>` prefix) is one fact.
`proto.QuestionIndex.QuestionIn` decides it; `team.InQuestionOf` hands it on as a
`team.InQuestion`, whose `Entry`, `Name` and `HeadlineName` are the forms the delivery's entry and
the `recv`, `history` and `verify` records show.

**Why it exists.** Plan 00265 R34: a human's reply inside a numbered question's thread was
delivered with `question: none` (the delivery's item and `recv`'s record were built with no
question asked), while `verify` derived the question on a path of its own and named it. The
coordinator took the reply as unattached and answered in the wrong thread.

**What it flags**, under `internal/teamchat`, outside `_test.go`: a composite literal of
`records.Human`, `records.Verified` or `deliver.Item` with elements and no `Question` key; and a
`Question` or `Headline` value of one of those types, in a literal or an assignment (a tuple
assignment from one call included), that is not a call of a `team.InQuestion` method. An empty
literal (a zero value returned beside an error) is not flagged.

**Not caught:** the fact derived by hand into a `team.InQuestion` (`team.QuestionNamed` is for a
source other than the index: a relay's account, or verify's read of a question root for a member
that holds no index); a carrier type added later and not listed in `questionCarriers`.

**How to fix:** ask `team.InQuestionOf` (in the engine, the team's `QuestionIn`) and set
`Question` and `Headline` from its methods.

- Red, fixture: `testdata/src/qres/internal/teamchat/member/engine` (the delivery's item and the
  record built bare, a question decided by hand in a literal, by assignment and by a tuple
  assignment: 9 findings; the resolver's answer everywhere, an empty literal and a test file:
  clean).
- Red, real tree (the driver as `go vet -vettool`, `./internal/teamchat/...`): 6 findings,
  `engine/deliver.go` `deliveryItems`, `engine/render.go` `itemRecord`, `teamsync/verbs.go`
  `historyOf` and `verifiedRecord` (its literal, and its `Question` and `Headline` from
  `questionOf`).
- Green, the fix: 0 findings. Every path takes `team.InQuestion`; `verify` keeps its read of the
  question root only for a member with no index, through `team.QuestionNamed`.

### project_temp_not_exclusive

**What it is about.** A file written whole is written to a temporary beside it and renamed (or
linked) into place, so no reader sees half of it. A create without `O_EXCL` (`os.WriteFile`,
`os.Create`, `os.OpenFile` with `O_CREATE` alone, a child program's output file) opens whatever is
already at the temporary's name, and follows a link there. The name is usually predictable (the
target's name with a pid or a fixed suffix), so anyone who can write the directory can plant a
link first and the write lands where it points; two writers sharing the name lose one's content.

**Why it exists.** The fedora-desktop#417 review: `fsx.WriteAtomic`, which some thirty writers
share (the seat conversation pointers among them, under `.claude/ccy`, which a ccy container
writes as its own), wrote `<path>.tmp.<pid>` with `os.WriteFile`.

**What it flags**, in non-test files: in one function body (a function literal is its own), a
create made without `O_EXCL` whose path a later call of the same body renames or links into place
(`os.Rename`, `os.Link`, `os.Root`'s `Rename` and `Link`, `unix` or `syscall` `Rename`,
`Renameat`, `Renameat2`, `Link`, `Linkat`). A create is `os.WriteFile`, `os.Create`, `os.Root`'s
`WriteFile` and `Create`; `os.OpenFile`, `os.Root.OpenFile`, `unix.Open`, `unix.Openat` and
`syscall.Open` whose flags hold `O_CREAT` and not `O_EXCL` (a constant, or a local the body
assigns one); a call of a function that creates its parameter's path so (a fact, so a helper of
any package counts, through any number of calls); and a path among the `Args` of an
`internal/execx` `Cmd` literal (directly or through a slice the body builds), which the child may
create. Two paths are the same when they are the same variable or the same expression as
written (a `filepath.Clean` around one taken off). A path built from a name `os.MkdirTemp` or
`os.CreateTemp` chose is clean: only this process made it.

**Scope, examined.** A rename before the create (a rotation: the file moved aside, then the
target written) is not a temporary and is not judged. A path handed to a child that only reads it
and then renamed would be flagged too; none is in the tree. Wider rule considered: every create
without `O_EXCL` in a directory another party can write, renamed or not. Not built: which
directories another party writes is not in the code, and a file written in place is the target
itself, whose name the writer owns.

**Not caught:** a temporary created in one function and moved in another (a returned path, a
struct field); a create or move through an interface (`convert`'s `Engine.Convert`, carry's fs
interface); a path rebuilt in another spelling between the two calls; flags the body does not
spell (a parameter).

**How to fix:** create the temporary with `os.CreateTemp` in the target's directory (a random
name, `O_CREATE|O_EXCL`) and rename `f.Name()`; for a child program, give it a path inside an
`os.MkdirTemp` directory beside the target. An exclusive create of a fixed name
(`O_CREATE|O_EXCL|O_NOFOLLOW`) is clean too, and fails on a leftover rather than following it.

- Red, fixture: `testdata/tempexclusive/src/fix`: `internal/badtemp` (the defect; `os.Create`,
  `OpenFile` with `O_TRUNC`, a link, `os.Root`'s `WriteFile` and `Create`, flags held in a
  variable, `unix.Openat`, a function literal, a helper of the package and of another package,
  curl through a built argument slice, sox through a literal: 13 findings), `internal/goodtemp`
  (`CreateTemp`, `O_EXCL` spelled and in a constant, the raw call with `O_EXCL`, a write in
  place, a rotation, a helper that creates exclusively, a child writing in an `os.MkdirTemp`
  directory, a test: clean), and the facts of `badtemp`, `goodtemp` and `tempio`. With the
  analyzer reporting nothing the test missed all 9 diagnostics of the first fixture.
- Red, real tree (the defence commit, the driver as `go vet -vettool`, both modules and every
  build tag set): 5 findings, `fsx/fsx.go` (`WriteAtomic`), `ftpcamera/upload.go`
  (`copyIntoMount` through `copyFile`), `vmtest/build.go` (`base.json.new`), `vmtest/fetch.go`
  (curl `-o <dest>.partial`) and `wsi/transcribe.go` (sox's output). The independent search made
  before the rule (every rename and link in `src-go`, each traced to its create) found the first
  four, judged sox's unsure, and found one the rule cannot see: `convert/app.go`, whose engine
  writes `<path>.tmp.<pid>` through an interface (fixed with the rest).

### project_root_create_follows_link

**What it is about.** An `os.Root` keeps every open inside its directory, but it follows a link
at the last name that stays inside, `O_NOFOLLOW` or not (Go 1.26, `os/root_unix.go`). A create
through it without `O_EXCL` opens the link's target, and when the link dangles it makes that
target: a file the caller never named. An `os.Root` is chosen because its directory is not
trusted (a checkout a container writes as its own), so a link there is the case it exists for.
`O_CREATE|O_EXCL` refuses any link at the name ("file exists"), and an open without `O_CREATE`
of a dangling link is "no such file": neither makes anything.

**Why it exists.** The batch 22b review of the seat-record lock (Plan 00265 R36): the lock was
opened `O_RDWR|O_CREATE` through the checkout's `os.Root`, so a dangling relative link at
`.claude/ccy/seat-conversations/.lock` had its target made inside the checkout before the
lock's same-file check refused it.

**What it flags**, in non-test files: `os.Root`'s `Create` and `WriteFile` (both open
`O_CREATE|O_TRUNC`), and its `OpenFile` whose flags (a constant expression, or a local the
function assigns one) hold `O_CREATE` and not `O_EXCL`.

**Scope, examined.** Wider rule considered: every `os.OpenFile` with `O_CREATE` and without
`O_EXCL` or `O_NOFOLLOW`. Not built, for the reason `project_temp_not_exclusive` gives: which
directories another party writes is not in the code, and outside an `os.Root` the caller can
spell `O_NOFOLLOW`, which `os.OpenFile` honours. An `os.Root` ignores it, so a caller that
spelled it there was still exposed, which is why this narrower class gets its own rule.

**Not caught:** flags the function does not spell (a parameter or a field, as
`teamchat/testfiles`' `openIn`); a create through a wrapper of `os.Root` (`homefs.Root`, which
is its own link-safe layer).

**How to fix:** create with `O_CREATE|O_EXCL`; when the file may already be there, open it after
`fs.ErrExist` without `O_CREATE` and check that what was opened is the plain file at the name
(`os.SameFile` of `f.Stat` and the root's `Lstat`). To replace a file whole, write an exclusive
temporary beside it and `Rename` it over the name, which replaces a link rather than following
it. Inside a directory this process just made (`os.MkdirTemp`), spell `O_EXCL`: there is nothing
to open.

- Red, fixture: `testdata/rootcreate/src/fix`: `internal/badroot` (the lock's `O_RDWR|O_CREATE`,
  with `O_NOFOLLOW` added, flags held in a local, `Create`, `WriteFile`, a function literal: 6
  findings), `internal/goodroot` (`O_EXCL` spelled and in a constant, the open-after-`ErrExist`
  form, a read, `os.OpenFile`, flags from a parameter, a test: clean). With the analyzer
  reporting nothing the test missed all 6 diagnostics.
- Red, real tree (the driver as `go vet -vettool`, both modules, untagged and with each of
  `ccyteamstest`, `busfailpoints`, `tuwunel` and `difftest`): 3 findings,
  `conversation/seatrecord.go` (`openSeatRecordLock`), `qa/bashlimits/app.go`
  (`writeAllowlist`, the tracked allowlist rewritten in place) and
  `qa/internal/semgrepreport/selftest.go` (`writeIn`, into an `os.MkdirTemp` directory).

### project_seat_number_text

**What it is about.** A teamchat seat is a name only the machine knows: the seat store keys it by
a hash of the checkout's path, and the seat records say which one this checkout worked in. A
person names the team (`ccy --teams <team>`, `cc --teams <team>`, `ccy-teams`), and the launch
finds the seat, asking in words only when there is a genuine choice.

**Why it exists.** Plan 00265 R90 (the owner, 26 Nov): the chooser listed `seat 2  <handle>  free`
rows with the lowest free seat as the default, and the ways on said `ccy --teams 3@<team>`, so
the owner had to know which seat number this checkout last worked in.

**What it flags**, in `internal/teams`, `internal/sessions`, `internal/conversation`,
`internal/ccylaunch`, `internal/registry`, `cmd/ccy-teams` and `cmd/ccy-sessions` and below,
outside `_test.go`: a string (a literal, a constant, a concatenation, or a `fmt` format with its
arguments) holding a launch with the seat spelt out (`--teams <seat>@`, `SEAT@`, `2@`, a value
filled in before the `@`), a seat placeholder (`<seat>`, `SEAT@`, `S@T`), a hand-over told as
`hand-to=<...>` or a seat repair with a seat before an `@`, a launch or a seat repair whose list
is filled by a value spelt as a seat, a ref, a list, a handle or a member; and, in a function
whose name ends in `row`, `rows`, `choices`, `options` or `label`, a row labelled `seat <value>`
or showing a `.Handle`. The decided scope is what a human must know or is shown: an info,
progress or error line naming a seat for diagnosis is not flagged, nor are keeper logs and
agent-facing text (teamchat). The allowance table has one row: `shutdown`'s `v2Label`, the
not-reached line's name for an LXC seat with no handle yet.

**Not caught:** a seat list joined into an argv and printed whole (restore's replayed command);
a row built outside a row builder. Help text and docs outside Go (embedded `usage.txt` and
`help.txt`, markdown, the launchers' messages) are held by the gate `seat-number-text`
(`scripts/qa-seat-text.bash`, [QA.md](QA.md)), which judges them by the same shapes
(`src-go/qa/seattext.Finding`; a seat name such as `dev1@` counts as a seat spelt out).

**How to fix:** name the command by its team (`membership.Launch` turns a seat list into the
launch by team), say a seat by when it last ran or its role (`seatchoice.Row`), and let the
chooser map a row back to its seat.

- Red, fixture: `testdata/src/fix/internal/teams/badseattext` (a launch by concatenation, by a
  format and by a placeholder, a placeholder constant, a hand-over, a seat repair by format and
  by a filled list, a filled seat list, a `seat %s` row, a handle row: 10 findings), beside
  `cleanseattext` (the launch by team, an argv, a seat named for diagnosis, a hand-over echoed
  back, a record's `--teams`, a row in words: clean) and `outsideseattext` (outside the trees,
  clean). Each finding is a `want`, so an analyzer reporting nothing fails all 10.
- Red, real tree (the driver as `go vet -vettool` over the trees above): 41 findings on the
  first draft, 35 once a record's `--teams`, an echoed hand-over value and the `shutdown` label
  were judged diagnosis, in `seatchoice`, `chooser`, `choose`, `seats`, `seatadd`, `seatmodel`,
  `membership`, `teamchatlaunch`, `hot`, `picker`, the `teams` seat verbs and `conversation`;
  one more (`conversation/elsewhere.go`, `seats conversation <list> --set`) once the filled-list
  shape took the seat repair.
- Green, the fix: 0 findings. The chooser takes the seat this checkout worked in
  (`seatchoice.Pick` over the seat records, `conversation.SeatLastRan`), rows are words, the ways
  on and repairs name teams (`seats conversation` takes a team and finds its one recorded seat).

### project_store_refusal_lost

**What it is about.** The seat store's capped lists refuse an entry when full (B 2.3.2: "the
operation is refused loudly, never dropped"). The refusal is what makes the whole write fail,
so nothing is lost and the work is tried again. A caller that answers it with a log line (or
with "no change, no error") lets the write go ahead without the entry, and what the entry
stood for is gone with no one told.

**Why it exists.** Plan 00267 (lossless delivery), DESIGN C2: the proof queued each HUMAN item's
`m.read` receipt with `AddReceipt` and, when the queue was full, logged `no read receipt for`
and settled the proof, so the human never saw their message read.

**What it flags**, outside `internal/teamchat/member/state` and outside test files: a call of a
`state.Store` method named `Add...` whose last result is an error, when that error reaches no
return of the function (or function literal) the call is in: the result discarded as a statement
or into `_`; held in a variable that no return names and no `if` tests with a body returning an
error of its own (a return whose last result is not `nil`); or passed anywhere but a return.

**Not flagged**: package `state`, which owns the lists; an add with no error result (the ledger
and missed lists roll, and the read-receipt queue keeps each room's latest receipt when full);
a test file, which fills a store past its cap on purpose.

**How to fix a violation.** Return the refusal, wrapped or translated into a refusal of your
own, so the write is refused and tried again; or make the add one that cannot lose (the
read-receipt queue: an `m.read` covers its room up to the event, so a full queue keeps each
room's latest).

- Red, fixture: `testdata/src/storeadd/internal/teamchat/member/engine` (the if-init form with a
  log line, the error held first, a discarded result as a statement and into `_`, `ErrFull`
  matched and swallowed, `return false, nil`, a loss inside a function literal, the error of a
  two-result add (`added, err :=`) swallowed: 8 findings; returned, wrapped, held then returned,
  translated and a two-result add returned: clean; an add with no error: clean; the test file
  and `member/state`: not judged). The two-result form was added with `AddDropped`: the first
  draft took every multi-name assignment for a loss, a false finding on `engine.applyBounces`.
- Red, real tree (the driver as `go vet -vettool` over `./internal/teamchat/...`): one finding,
  `engine/proof.go` (`receipt`). `engine/send.go`'s `AddSending`, which answers with its own
  refusal, was a finding of the first draft and is not one now.
- Green: `AddReceipt` cannot refuse (`TestAFullReceiptQueueKeepsEachRoomsLatest`), so the proof
  calls it with nothing to lose: 0 findings.

### project_teamchat_root_read

**What it is about.** ccy-teams' configuration carries `TeamchatRoot`, the root the v2 records
are read under: "" on a host, a scratch directory in a test world (`teamtest`), so a test never
reads the machine's own teams. A record read at its fixed path ignores it.

**Why it exists.** Plan 00265, the batch 26 review: the session-name wiring's role lookup (and
the seat adder, the system and launch verbs, the team lifecycle and the seat sweep) read the
status record through `system.StatusRecord`, at `/var/lib/teamchat-status`, so the teams unit
tests read whatever teams the machine running them had installed.

**What it flags**, in the non-test files of `internal/teams` and the packages under it: a call
argument naming one of `internal/teamchat/paths`' machine records (`StatusRoot`,
`StatusInstalledFile`, `LXCRecordDir`, `StatusTeamDir`, `StatusFile`, `StampFile`,
`ApprovedFile`, `LXCOnlineFile`, `SetRecord`) that is not a `+` chain whose leftmost operand is a
name or field called `TeamchatRoot` (any case). Calls of `fmt` and `errors` are not judged: they
word a message and read nothing.

**Not caught**: a fixed path built from a literal; a record path held in a variable and passed
later; a read outside `internal/teams` (`internal/sessions` and `internal/conversation` still
read through `system.StatusRecord`, having no root to honour); a unit test that resolves a
made-up checkout on the real filesystem (that half of the defect is a runtime property no
analyzer here can see: the tests now give a real checkout).

**How to fix a violation.** Read under the run's root: `h.TeamchatRoot + paths.StatusFile(id)`,
or `system.StatusUnder(h.TeamchatRoot)` for a `Status` func.

- Red, fixture: `testdata/src/teamchatroot/internal/teams/system` (the status reader given the
  fixed path, os reads of a record, a directory and a constant, a root after the path, a constant
  in front: 6 findings; the rooted forms, a name that is not a record, messages and a held path:
  clean; the test file and `internal/elsewhere`: not judged).
- Red, real tree (the driver as `go vet -vettool` over `./internal/teams/...`): one finding,
  `system/system.go` (`StatusRecord`).
- Green: `system.StatusUnder(root)`, wired with `h.TeamchatRoot` at all five ccy-teams callers;
  `StatusRecord` is `StatusUnder("")`: 0 findings.
