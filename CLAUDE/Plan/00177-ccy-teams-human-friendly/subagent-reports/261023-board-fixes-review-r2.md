# Review r2: board fixes (unavailable menu lines, echo held between screens) and the seat-chooser scaffold

Scope: `git diff b1e298e1c` on branch agent-aaf94dbcca6e93463-fb2e7585 (all staged). This was a
read-only review by a fresh adversarial reviewer. Round 1 is in
[261023-board-fixes-review-r1.md](261023-board-fixes-review-r1.md).

## What was run

- `go test -race -count=1`, one package group at a time, over:

  - `internal/ttyecho`, `internal/ftpcamera`, `internal/open`
  - `internal/teams`, `internal/teams/picker`, `internal/sessions`, `internal/freeze/...`,
    `internal/ui`

  All passed.

- `go vet` over ttyecho, open, ftpcamera and `teams/...`: clean.

- `shellcheck -x` over chooser-teams-up.bash, chooser-teams-down.bash, `_chooser-teams.inc.bash`
  and `_acceptance-m7.inc.bash`: clean.

- The diff's added lines were grepped for install-specific names (home paths, user names,
  e-mail addresses). Only repo paths such as `files/home/...` were found.

## Round 1 responses, checked

- **F1 holds.** The hold is now taken by `engage()`, which runs only on an `EchoHeld` child:

  - every fzf (`Picker.Pick`, `Picker.Choose`), every gum (`run`, `Style`, `Box`) and
    `ui.columns`' tput;
  - `Given`, `give` and `take` do nothing while `found == nil`;
  - `release`/`restore` do nothing when it was never taken.

  The listing paths take their width from `term.FileColumns` (an ioctl), not from tput, so
  `ccy-teams --list | less` never engages. open's own `columns` runs tput without `EchoHeld`, so
  open's numbered fallback never engages either. There is a test for this:
  `TestARunThatShowsNoScreenNeverTouchesTheTerminal`, and teams has
  `TestARunWithNoScreenLeavesTheTerminalAlone`.

- **Screen paths that never take the hold.** I checked each:

  - Every `ui.Gum{}`/`ui.Picker{}` in production is built on the held runner: host.Wire,
    sessions.wire, open.NewEnv, freeze app.go:132, ftp-camera args.go:206/212.
  - The only bare `execx.OS{}` literals in those packages are the fill-in defaults
    (host.go:113 and :241, sessions app.go:261 and :264). The production constructors always set
    the field first.
  - Bubble Tea's pathinput in lxc-add only runs after earlier gum steps (Box and Explain), so the
    hold is already taken. It saves and restores the quiet mode.
  - No `ui.Lines` exists.
  - A numbered or no-fzf fallback shows no screen, so leaving the hold off there is correct.

- **F2:** not counted, as the brief says. The coordinator must still queue play-claude-yolo,
  play-lxcfreeze, play-podfreeze, play-ftp-camera, and the play that builds open.

- **F3 holds.** `Held` defers `releaseOnPanic`, which calls `recover` directly, so the recover
  works. It releases, then panics again. There is a test.

- **F4 holds.** `down_prerequisites` checks only:

  - the session paths, agent-bus, the installer, podman, firewall-cmd and jq;
  - that the owner's host_vars declare neither team name.

  It does not need the hooks daemon or the Quick Launch record.

- **F5 holds.** `--no-restore` is printed, and the launcher accepts it (claude-yolo:794).

- **F6 holds.** `Spin` is deleted and has no callers.

- **F7 holds.** `${0##*/}` gives acceptance.bash, chooser-teams-up.bash or chooser-teams-down.bash,
  as the caller is.

- **F8 holds.** The specific reasons for not building either narrow rule are now recorded:

  - the fill-in defaults, and runners that cross packages;
  - `project_seam_commands` plus the ui test.

  I accept them.

- **F9 holds.** seats.go, menu.go and SPEC-ccy-teams.md all say "not up".

- **F10 holds.** `heldExecer` wraps `err` itself (`before %s: %w`).

- **F11 holds in the normal case.** It has a test. See finding 1 for the case it does not cover.

- **F12 holds.** `m7_park_seats` parks every seat directory under each team. The glob that matches
  nothing is guarded by `[[ -d ]]`.

## New checks that held

- **Concurrency.** All state changes are made under `h.mu`.
  - If a signal and a `release` race, they settle through `h.stop = nil` and `restore` resetting
    `found` and `given`. The loser does nothing.
  - After `heldExecer`'s release, if Exec fails, the next screen engages again and starts a new
    watch goroutine. The old goroutine has already ended on `stop`. `Held`'s final release is then
    the only one.
  - No fzf or gum child runs alongside a child given the terminal: the collectors run with nil
    Stdin.
- **Callers.**
  - teams: the release comes before `stop()` and `Sudo.Forget`, so the credential is still
    dropped.
  - sessions: a release error is always printed with the "run reset" hint, and a run that passed
    becomes a failure.
  - freeze: `ExitCode` prints the release error always.
- **Menus.** `startAction` decides from the same frame refusal as the header, plus
  `snap.StateOf`. The refusal order matches `NewSession`. `attachRefusal` and `sessionRefusal`
  mirror `AttachSeat` and `AttachSession`.
- **ftp-camera prune.**
  - EOF is still "no" (E39).
  - Any other read error, or an echo that cannot be given back, now fails the run instead of
    being taken as "no".
- **Plan scripts.** I traced the stop paths:
  - **The down run fails before adopting** (`DOWN_FROM` is empty). The cleanup does nothing, which
    is right: nothing was touched.
  - **The down run fails after adopting.** The cleanup:
    - removes the containers, parks and deletes the seats;
    - puts the Quick Launch record back;
    - purges the teams if `TEAMS_PRESENT` is set;
    - then runs `m7_check_gone` and keeps `chooser-teams.current`.
  - **The purge fails because the sudo credential lapsed.** The after-stop purge fails too, the
    check fails, and the run exits non-zero with the record kept.
  - **The up run.** `up_not_already` runs before `m7_leftovers`, so a second up cannot purge the
    first one's teams.

## Findings

1. **minor**: `src-go/internal/ttyecho/ttyecho.go:248-254`. When both SIGTERM and SIGHUP are
   ignored (for example `nohup` under a `trap '' TERM`), `watched` is empty, and
   `h.Notify(sigs)` is `signal.Notify` with no signals. That relays **every** incoming signal:

   - The Go runtime's own SIGURG preemption signals, SIGCHLD and SIGWINCH all arrive on the
     channel. The goroutine then drops the hold (`restore`) on the first one, mid-run, and
     `Raise`s that signal.
   - Until then, the default actions of all signals are taken over, SIGTSTP among them.
   - The effect is silent: the echo returns between screens, the bug this change fixes.

   Fix: if `len(watched) == 0`, do not call Notify or start the goroutine. Only `h.stop` needs to
   be set, or a nil-safe `release`. Add a test with both signals ignored.

2. **minor**: `src-go/internal/open/app.go:112-118`. `rerr` (the mode could not be put back) is
   used only when `err == nil`. When the run also failed, the restore failure is dropped, so the
   person is never told the terminal is left echo-off. This is a discarded error under the house
   rules, and teams, sessions, freeze and ftp-camera all report it unconditionally. open's message
   also lacks the "run reset to see what you type" hint that the others give. Fix: report `rerr`
   whenever it is non-nil, for example `r.say(rerr.Error()+"; run reset to see what you type.")`,
   and keep the existing exit-code precedence.

3. **nit**: `src-go/internal/ftpcamera/app.go:175-178`. A release failure returns `ExitFailure`
   whatever `code` was, so `ExitCancelled` (130) becomes 1. teams and sessions keep a non-OK code.
   Fix: `if code == term.ExitOK { code = term.ExitFailure }`, as in teams.

4. **nit**: `src-go/internal/ftpcamera/prune.go:116-136`.

   - With `errors.Join(io.EOF, takeErr)`, `errors.Is(err, io.EOF)` is true, so an echo that could
     not be turned off again after EOF is dropped. The run-end release normally resets the mode
     anyway, so this is harmless.
   - A give failure is reported as "could not read the answer: give the terminal's echo back: …",
     which reads oddly.

   Fix: check the take error separately from EOF. Optionally, word the give failure on its own.

5. **nit**: `src-go/internal/ttyecho/ttyecho.go` package doc, and the changelog. Because the hold
   is lazy, keys typed **before the first screen** still echo onto it. An example is the arrows
   pressed while `ccy-teams` collects the board for the first time. This was accepted as the price
   of F1, but the doc says only that a run with no screen is left alone. Fix: one clause in the
   package doc and the changelog ("keys typed before the first screen are not held").

6. **nit**: `CLAUDE/Plan/00201-…/_acceptance-m7.inc.bash` (`m7_owner_declares_neither`). The
   remedy text "Rename the acceptance teams in `${0##*/}`" names the script that ran. For the
   chooser scripts, the names are defined in both chooser-teams-up.bash and chooser-teams-down.bash,
   so renaming them in one leaves the other out of step. Fix: say "in chooser-teams-up.bash and
   chooser-teams-down.bash" when the name is a chooser script, or move the two names into
   `_chooser-teams.inc.bash`.

None of these blocks the merge. Finding 1 needs an unusual start (both signals ignored) to
trigger. Finding 2 affects only a run that has already failed. Fixing 1 and 2 before the host
deploy is recommended but not required.

VERDICT: MERGE
