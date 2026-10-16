# Review: Plan 00201 Task 2.14, `ccy-teams shutdown` (branch `feat/teams-shutdown`, tip 6df0b733)

Adversarial pre-merge review against the branch's `SPEC-shutdown.md` and the developer's report
`subagent-reports/261016-teams-shutdown-opus.md` (both on the branch, not yet on F44). Read-only;
no qa-all. The
branch was checked out in a scratch worktree to run `go build`, `go vet`, the `teams` and
`tmuxccy` package tests, six hand mutations, and `scripts/test-ccy-teams-command.bash`.

## Verdict: FIX-FIRST (one blocking finding, small)

**B1. A pane that cannot be read is killed, not failed (`stop.go`, `step`).**
`CapturePane` error → `s.hardStop(...)` → `tmux kill-session` straight away. SPEC SD12 says the
hard stop happens *only* when a session has not ended within its wait, and SD13 says a tmux
error makes the session **failed**, the others carrying on. The design principle of the whole
feature is "when unsure, leave it alone" (unsent text is never typed over); an unreadable pane
is the most unsure state there is, and killing it can destroy unsent text and in-flight work.
The one common capture error, "can't find pane" when the session ended between `list-sessions`
and `capture-pane`, is already handled correctly downstream (`KillSession` → `ErrSessionGone` →
`ended`), so the fix is: on a capture error that is not "gone", `j.done(failed, err.Error())`,
and add the test (`m.OnErr("tmux -L ccy capture-pane", ...)` → `failed`, not killed). No test
covers this path today.

Everything else below is non-blocking.

## Correctness: can it type into the wrong pane, or kill the wrong session?

- **Targeting.** `paneTarget` is `=<name>:` (exact session match, current window, active pane);
  `KillSession` is `-t =<name>`. No prefix matching: `ccy-proj` cannot reach `ccy-proj-2`.
  Verified in `tmuxccy/panes.go` and the existing `TestSendKeysAndEnterTargetsKeepTheColon`.
- **Which sessions become jobs.** `planFor`/`addSeat`: only a seat row of the team whose
  registry record names a live tmux session of the same name (`snap.Live[row.Record]`). A record
  with no live session is never acted on (held → "cannot name", free → nothing). A session is one
  job however many of its seats are the team's, and its seats on other teams are shown
  ("also on beta (seat 2)") and checked for release. Nothing outside the team's seat rows is
  ever killed; the hard stop kills only `j.name`.
- **Stale record.** A record is written by `ccy_registry_write` when the tmux session is
  created (`lib/tmux-session.bash:521`) and removed by the trampoline or ccy-sessions' End, and a
  new session of the same name rewrites it, so a live `ccy-proj` tmux session and the `ccy-proj`
  record describe the same launch. Project-name collisions across checkouts get distinct names
  (`-N` suffix). I found no route to a wrong session.
- **Record removal after `closeWindow`.** The trampoline (`session-registry.bash:614`) does
  `rm -f record` *before* the "Press Enter to close this session." hold, so killing a window at
  `EndedText` leaves no stale record. Correct.
- **Judge prefix.** `restore.Judge(prefix, true, ...)` is only ever called with `ccy`/`cc`
  (jobs come from `isLauncherPrefix` records), so the `unknown-launcher` DEAD branch and the
  "cannot be judged" line in `closeWindow` are unreachable; harmless.
- **No server.** `Tmux.List` returns `nil, nil` when no server runs; every pending job is then
  `ended` (exited). Right: no server means no sessions.

## Pane checks

- `screen.ClaudeState` requires a framed input box (rule above and below a `❯` line), reads
  Busy from "esc to interrupt" in the four status lines above, and Ready from the raw line's
  placeholder SGR (capture is `-e`). Same function and same capture flags as
  `ccy-sessions set-going`, so the readiness and unsent-text detection are the proven ones.
- Typed before `/exit` → failed, nothing sent (mutation A: removing the guard fails
  `TestUnsentTextIsLeftAlone`). Typed after `/exit` (completion menu ate the Enter) → one more
  Enter, once. Busy → one Escape then wait (mutation B fails `TestABusySessionIsInterruptedFirst`).
- **Inherent race, not a defect:** the capture and the `send-keys` are two commands, so a
  turn starting in between would queue `/exit` as a message. `set-going` and the supervisor's
  own `/exit` accept the same race; after 60 s the hard stop bounds it. Worth one sentence in
  the spec's Limits.
- **NotDrawn** (permission dialog, model picker, starting) is waited on for the 30 s prompt
  wait and then killed. That is what SD10/SD12 say, and `stopped` counts as success (SD16). The
  preview line names only the 60 s exit wait; it should also say that a session whose prompt
  does not come back within 30 s is stopped, so the owner is not surprised by a
  `stopped: its prompt was not ready within 30 s` line that still exits 0. Minor.

## Security: local only, no injection

- Nothing is written to the bus or Matrix: `TestNothingGoesOnTheBus` whitelists only tmux,
  systemctl, git, env, `agent-bus seat list`, and the collector's `sudo agent-bus list` /
  `human devices` reads; the bash suite checks `agent-bus send` and `pingbus` never run.
- Nothing from the bus can start it: `shutdown` is a `ccy-teams` subcommand on the host;
  pingbus verbs are fixed and the inbox socket is a message to the model (the spec's own
  finding). An agent in a container has neither `ccy-teams` nor the host's `tmux -L ccy` socket.
- `send-keys` arguments are an exec argv (no shell); the text is the constant `/exit` under
  `-l`; the only variable is the session name in `-t =<name>:`, taken from registry filenames,
  the same source every other tmuxccy call already trusts.

## Fail-fast and reporting

- Exit codes: 64 for usage and for no-terminal-without-`--yes` (nothing typed, preview still
  printed; mutation F fails `TestNoTerminalNeedsYes`); 1 for unknown team, any failed session,
  or anything unreachable (mutation C fails four tests); 0 only when all exited/stopped with
  seats released and nothing out of reach. Cancel/Esc in the question → "Nothing was shut
  down.", 0. All on stderr; stdout stays empty (bash check).
- `Collect` errors are already printed by the collector (`errReported`), so the silent
  `return ExitFailure` in `forTeam` is not a swallowed error.
- InteractiveScripts: the only question is a yes/no with default No; there is nothing to
  re-prompt. Non-interactive refusal names `--yes`. Compliant.
- Known limit worth a follow-up, not a block: a session launched `cc --teams <team>` (the
  chooser form) has a record without its seat (`cc` records its original argv at tmux start,
  `cc` line 69 / `tmux-session.bash:521`), so every shutdown of a team with such a session exits
  1 with "held by a session ccy-teams cannot name". `record-seats` for cc, as ccy does, would
  close it; the developer names this.

## Tests

- `go vet` clean; `go test ./internal/teams/... ./internal/tmuxccy/...` all ok.
  `test-ccy-teams-command.bash`: 369 passed, 0 failed, including the new shutdown section.
- Mutations, each caught by exactly the test that owns it: A unsent-text guard removed; B
  Escape not sent; C unreachable no longer fails; D submit delay removed (Enter arriving with
  the text); E registry record not removed on hard stop; F no-terminal check removed.
- Keystroke tests are adequate: the fake tmux is stateful (typed text, Enter count, Escape
  count, kill), and the asserted argv is the literal
  `tmux send-keys -t =ccy-proj: -l /exit` then `tmux send-keys -t =ccy-proj: Enter`.
- Gap: no test for a `capture-pane` error (B1), and none for `Tmux.List` failing mid-run
  (every pending job → failed with "could not be watched any further"; the code is right, it is
  just unproven).

## Side change: the table names the `cc` session holding a seat

`isLauncherPrefix` lets `cc` records and `cc-*` tmux sessions feed `SeatSession` and
`Checkouts`. `readSeats` only reads a checkout that has `.claude/ccy/pingbus/seats`, so a `cc`
session in a non-team directory adds no rows and no noise. A `cc --teams alpha` record stores
`dir|alpha` in `SeatSession`, which never matches a `seat@team` key, so it is inert. `Row.Held`
is populated from `agent-bus seat list` and not shown in the table (used by `addSeat` only).
Correct, and the board test was extended with a `cc-host` record and a `cc-desk` session.

## No version bump needed

Nothing under `files/var/local/claude-yolo/` changes; the launcher, image and `cc` are
untouched. `ccy-teams` carries no version constant. `meta-deploy.list` queues
`play-claude-yolo.yml` to rebuild `ccy-teams` only, with a correct description.

## Docs and the owner check

- `usage.txt` (subcommand and the menu line), `docs/agent-bus.md` step 10, PLAN.md Task 2.14,
  JOURNAL entry, ACCEPTANCE-owner.md step 12 (two sessions, one idle, one busy, LXC listed as
  not reachable, the lines to expect, `stopped`/`failed` fails the step). Consistent with the
  spec and the code.
- `docs/agent-bus.md` shows 202 changed lines that are the markdown formatter renumbering the
  list `1.`→`01.` once a tenth item was added. Noise, not drift.
- The spec cites `cc-teams.bash`; it lives at `files/var/local/claude-code/cc-teams.bash`. Fine.

## Public-repo hygiene

Placeholders only (`alpha`, `beta`, `~/proj`, `ccy-proj`, `proj.1+local.podman`). A grep of
the added lines for usernames, home paths and hostnames found nothing.

## Merge conflicts

- `git merge-tree --write-tree F44 origin/feat/teams-shutdown`: clean.
- `fix/watcher-survives-homeserver-restart` exists locally only (no `origin/` ref); it sits at
  the merge base 0e53ac69, so `merge-tree` against it is trivially clean.

## Non-blocking recommendations, in order

1. Preview line: name the 30 s prompt wait beside the 60 s exit wait.
2. Spec Limits: one sentence on the read-then-type race.
3. Follow-up task: `record-seats` for `cc` so chooser-form sessions can be shut down.
4. Tests for `capture-pane` and `list-sessions` failures mid-run (the first comes with B1).
