# Review r1: board fixes (unavailable menu lines, echo held between screens) and the seat-chooser scaffold

Scope: `git diff b1e298e1c` on branch agent-aaf94dbcca6e93463-fb2e7585 (all staged). Read-only review.

## What was run

- `go test -race -count=1` over `internal/ttyecho`, `internal/teams/...`, `internal/sessions`,
  `internal/open`, `internal/freeze/...`, `internal/ftpcamera`, `internal/lxcfreeze`,
  `internal/podfreeze`, `internal/ui/...`, `internal/execx`: all ok.
- `go vet` on the same packages: clean.
- `shellcheck -x chooser-teams-up.bash chooser-teams-down.bash`: clean. Index modes: both scripts
  100755, the include 100644.

## What holds up

- **ttyecho core.** Start/Release/Given/give/take are all done under `h.mu`. The given counter
  nests correctly (there is a test for it). `restore` resets `given`, so a `take` after a
  signal-time restore does nothing. Every public method is nil-safe. ENOTTY is matched through
  `%w`. A second Release does not set the mode again. The signal goroutine ends on `stop` or on
  the signal. After `signal.Stop` the Go handler stays installed with `_SigKill`, so the re-raise
  does kill the process when no other channel wants the signal. Where a NotifyContext also
  catches SIGTERM (ftp-camera, ccy-sessions), the re-raise goes to that context instead, which is
  harmless.
- **Parallel collection.** The board's collectors run with nil Stdin, so they run held and never
  touch the counter. I found no child given the terminal that runs while an fzf/gum/Bubble Tea
  screen is up. Such a child would overwrite the screen's raw mode.
- **Children given the terminal with echo back.** These all go through the held Runner with Stdin
  set to the terminal: plays (ops.go runPlay and the Element play), `sudo -v` (sudo.go Ready),
  `sudo -n` Run, `tmux attach` (tmuxccy), open's handlers (launch.go:96), and ccy-sessions' attach.
  The launcher exec releases the hold first, in teams, sessions and ftp-camera. In-process line
  reads use `Given`: freeze readLine, open readLine, ftp-camera's y/N. pathinput and gum take the
  terminal raw themselves. No git or ssh network prompt runs with nil Stdin in the held tools.
- **Tests and the real terminal.** No test touches the real terminal. The production
  constructors are only inspected, never started. The pty test uses its own `/dev/ptmx` pair. It
  proves the held keys reached the line discipline before Release by reading the line, so it is
  not timing-flaky.
- **Menus.** The menus agree with `seats.go`:
  - `newRefusal` follows the same three cases as `NewSession`.
  - `attachRefusal` and `sessionRefusal` mirror `AttachSeat` and `AttachSession`.
  - Choosing an unavailable line still goes to the refusal.
  - The footer says "start a session: unavailable".
  - The wording matches ccy-sessions' new-session row.
- **Plan scripts.**
  - `CURRENT` resolves to the same `untracked/plan-runs/<plan>/chooser-teams.current` from both
    scripts, because `plan_start_log auto` is always used.
  - Legs set globals in the parent shell, since `plan_deploy_leg` runs no subshell.
  - On success the up script's cleanup is disarmed by `m7_left_up`.
  - A failed teardown keeps `CURRENT`, and a re-run adopts the records again.
  - Purges are by name only, after the check that the owner's host_vars declare neither name.
  - The up run's leftover purge requires the "Plan 00201 acceptance" description prefix, and both
    descriptions carry it.
  - The names differ from acceptance.bash's (`accept201a`/`b`), so the two do not collide.
  - Make coordinator on a podman seat changes the role live only, with no apply and no play, so it
    never reaches the owner's inventory.
- **The "cannot be a rule" conclusion.**
  - For the menu class it is honest: availability is a run-time fact.
  - For the echo class it is arguable. See finding 8.

## Findings

01. **major**: src-go/internal/teams/app.go:149, src-go/internal/sessions/app.go:210,
    src-go/internal/freeze/app/app.go (RunContext), src-go/internal/open/app.go (Run),
    src-go/internal/ftpcamera/app.go (Run). The hold starts on every run, for every subcommand,
    whenever stdin is a terminal. That includes non-interactive output commands, which never
    touched termios before. Two regressions follow:

    - **A terminal program on the other side of a pipe.** Take `ccy-teams --list | less`,
      `ccy-sessions … | fzf` or `lxc-freeze … | less`. If the hold starts first, `less` saves the
      echo-off mode and sets raw. Then the hold's Release puts the cooked mode back under the
      running pager: keys echo and wait for Enter. When `less` exits it restores echo-off, and the
      shell keeps that state (the job exited normally).
    - **A background job.** `ccy-teams status &` with a terminal stdin now gets SIGTTOU at
      TCSETS and stops ("suspended (tty output)").

    Fix: take the hold only when the run will show a screen. Either start it lazily, on the first
    EchoHeld child, Given, or child given the terminal, with Release a no-op if it never started.
    Or require stdin AND the host's screen stream (Host.Stdout; os.Stderr for NewDiverted) to be
    terminals, and `tcgetpgrp(fd) == getpgrp()`. Add tests: stdout a pipe means no Sets, and not the
    foreground group means no Sets.

02. **major**: CLAUDE/Plan/meta-deploy.list is not in the diff. These changes reach nobody until
    their plays run on the host:

    - ccy-teams, ccy-sessions and the CCY_VERSION 3.100.17 bump (play-claude-yolo.yml);
    - lxc-freeze, podfreeze and ftp-camera (play-lxcfreeze.yml, play-podfreeze.yml,
      play-ftp-camera.yml);
    - open: check that play-cli-tools.yml, already queued, is the play that builds it.

    Task 10.3 (the host agent re-drives the board) and the seat-chooser drive both need the new
    ccy-teams. CLAUDE.md requires queueing them in the same commit. Fix: add the plays to
    meta-deploy.list, and tell the owner what meta-deploy.bash will now do.

03. **minor**: src-go/internal/teams/app.go:156, and the same sequential Release in sessions,
    freeze, open and ftp-camera. Release is not deferred. A panic in `run` ends the process with
    exit 2, not by a signal. So the terminal stays echo-off, and bash saves that as the shell's tty
    state. Fix: `defer` the release (and its error report) right after a successful Start, keeping
    the exit-code merge.

04. **minor**: CLAUDE/Plan/00201-…/chooser-teams-down.bash:103. The teardown runs the up/launch
    prerequisites `m7_prerequisites`, which return 2 when the hooks daemon is missing or when
    `.claude/ccy/.last-launch.conf` is absent. So a teardown can refuse to run and leave the
    throwaway teams and their firewalld rules up, for reasons a teardown does not need. Fix: give
    the down script its own prerequisite subset: the session paths, agent-bus, the installer,
    podman, firewall-cmd, and the owner-declares-neither-name check. If the Quick Launch record is
    missing, restore it from the up copy.

05. **minor**: CLAUDE/Plan/00201-…/chooser-teams-up.bash:139. The printed drive command has no
    `--no-restore`. An interactive session left running across a reboot, on a machine that
    restores sessions, would be brought back into teams the teardown purged. Fix: add
    `--no-restore` to the printed command. Or have the down script check that no ccy-sessions
    registry record names a chooser seat.

06. **minor**: src-go/internal/ui/gumx.go:24-30 (Spin through `g.run`, now EchoHeld). `gum spin -- <argv>` runs an arbitrary child, which may read the terminal, with the echo held. This
    contradicts the claim in ui_test.go that "every program ui runs takes the terminal raw itself
    or reads nothing". Spin has no production caller. Fix: delete it (YAGNI), or run it without
    EchoHeld.

07. **minor**: CLAUDE/Plan/00201-…/\_acceptance-m7.inc.bash:917 (and :126). In the chooser scripts,
    the failure text tells the person to "run acceptance.bash again". The right step is to run
    chooser-teams-down.bash again. Fix: take the script name from `$0` or a global the caller sets.

08. **minor**: CLAUDE/GoProjectRules.md, the "terminal left echoing" entry. "No syntax-tree shape"
    is overstated. Two narrow analyzers were feasible:

    - flag an `execx.OS{}` composite literal, in a non-test file of a package that builds
      `ui.Gum`/`ui.Picker`, that is not the direct argument of `(*ttyecho.Hold).Runner`/`Execer`
      (only the constructors listed above remain);
    - flag an `execx.Cmd` literal named `fzf`/`gum` without `EchoHeld`. The ui unit test covers
      this one, but only inside `internal/ui`.

    Fix: either write one of them, or record the specific reason it was rejected (for example,
    `host.go:113`'s fill-in default is legitimately bare), not "one spelling of many".

09. **nit**: src-go/internal/teams/picker/menu.go:145 says "<team>'s homeserver is not up", while
    seats.go NewSession says "<team>'s homeserver is down". Fix: use one wording; the board heading
    says "not up", so change the refusal to match.

10. **nit**: src-go/internal/ttyecho/ttyecho.go:276. `errors.Unwrap(err)` depends on Release's
    exact wrapping. If Release ever returns an unwrapped error (or a join), the message prints
    `%!w(<nil>)` or loses detail. Fix: wrap `err` itself, or have `restore` return the bare
    `Set` error and add the wording at each caller.

11. **nit**: src-go/internal/ttyecho/ttyecho.go:201. `signal.Notify(SIGHUP)` turns SIGHUP back on
    for a process started with it ignored (nohup). On a hang-up the hold then restores the mode and
    re-raises. After Stop the signal is ignored again, so the process carries on where it would
    have before; the effect is small, but it changes behaviour. Fix: skip SIGHUP when
    `signal.Ignored(syscall.SIGHUP)`.

12. **nit**: CLAUDE/Plan/00201-…/\_acceptance-m7.inc.bash:835 (`m7_park_seats`) parks only seat
    `${SEAT}`=1. If the host agent's second drive picks "new" again, that seat 2 is deleted with
    the team purge but never parked. This is harmless, because the purge removes the homeserver.
    Fix: park every seat directory under each team, or say in the header that only seat 1 is
    parked.

VERDICT: CHANGES
