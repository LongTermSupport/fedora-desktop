# Task 2.0b host acceptance: the goodbye checks, automated

Branch `feat/goodbye-acceptance` from F44 (442143f5). The owner asked for the goodbye host
checks to be run through meta-deploy instead of by hand.

## What was built

- `acceptance.bash`: the gate. It follows PlanScriptStandards R1-R14, and it has no
  triage.bash or deploy.bash beside it.
  - **G1**: Ctrl-C is sent every 0.2 s until the launcher has exited, so the presses go on
    after the container's exit and through the launcher's cleanup. Each team then gets one
    goodbye, `… left the team: session ended (exit)`, and no disconnected. The pane log shows
    no traceback and no failed `pingbus seat disconnected`.
  - **G2**: `podman stop` of the session's own container. Each team gets one disconnected
    and no goodbye.
  - **G3**: `tmux kill-window`. Each seat gets one disconnected (the session holds two seats).
  - **G4**: checked in every scenario: never both messages.
  - **C1**: nothing of the run is left.
  - Exit codes: 0 ACCEPTED, 1 REJECTED, 2 COULD NOT ESTABLISH (no token saved by Quick
    Launch), 64 usage.
- `_acceptance-goodbye.inc.bash`: the legs and checks.
- `goodbye_check.py`: the verdicts (tally, expect, next-from, last-token, pane-clean,
  fake-page). The tests in `test_goodbye_check.py` were written first.

## Design choices (each the answer to a prompt or leak found in the launcher)

- **Teams.** The teams come from Plan 00201's acceptance legs, reused by sourcing
  `_acceptance-m7.inc.bash` and calling `m7_check.py`. The script finds them in Plan/ or
  Completed/.
  - The teams are accept194a and accept194b. They are declared only in a host_vars file in
    the run directory.
  - A leftover team is purged only when its description starts with "Plan 00194 goodbye
    acceptance".
- **Scratch checkout.** It is a fresh `git init` in the run directory, with this checkout's
  origin (the team trusts its owner) and the tracked `.claude/ccy` files (the ignore rule,
  `ccy.env` and the supervisor).
  - Running interactively in the owner's checkout would stop at the "running containers for
    this project" menu, because the owner's own sessions are running there.
  - The scratch checkout has a project name of its own and no custom Dockerfile, so no image
    is built.
- **tmux server.** The run uses its own server at `$XDG_RUNTIME_DIR/accept194-tmux/ccy`.
  - The socket's basename is `ccy`, so `ccy_tmux_insulate` does nothing and
    `ccy_tmux_current_session` names the session. That gives the `ccy-session` label and the
    `seat disconnected --session` the real path uses.
  - The server is started with no stdio of the run's, so the run log's pipe still closes. It
    keeps dead panes (`remain-on-exit`), so the launcher's exit can be seen.
- **Launch.** It uses `--token <the name Quick Launch saved> --no-ssh --no-network --no-restore`, so the launch asks nothing. No model turn is run.
- **Seats.** Both seats are taken once, from the run's terminal, before any launch (as
  Plan 00203 M6 does). A tmux pane is a new tty with no sudo credential, so a launch there
  could not create a seat.
  - All three sessions reuse seats 1@A and 1@B. The scenarios are kept apart by the time each
    started and by waiting for the launcher to exit and the container to be gone.
- **Verdict.** It is each room as the team's human reads it: `/messages`, paged, only texts
  from the seat's member user, since the scenario started. The goodbye and the disconnected
  notice are addressed to the humans and coordinators, so the human's view is the one the
  owner sees.
- **Owner isolation.** Containers are matched only by `label=ccy-session=<one of the three names>`. Membership directories are removed only for those names. Leftovers of an
  interrupted run are removed first.

## Verified here (container)

- `python3 test_goodbye_check.py`: 26 tests OK, including the fake run.
- `acceptance.bash --fake`: FAKE ACCEPTED. It accepts the good rooms (including one over two
  pages) and rejects these: both messages, two goodbyes, no goodbye, no disconnected,
  another reason, a traceback on the terminal.
- `shellcheck -x` on both bash files is clean.

## Not verified (host only)

The real run has not happened.

- **Sudo after the seat take.** `agent-bus seat take` ends with `sudo -k`. The later root
  steps (purge, the leftover checks) assume `sudo -n` still works afterwards. That has held
  for Plan 00201 M7 and 00203 M6 on this host. If it does not hold here, the leg says so
  before the purge, and the teardown names the failure.
- **Launcher prompts.** Any prompt the launcher still shows (an expired token, the zombie
  container menu) ends the start poll after its bound. The run then prints the pane.
- **Claude Code readiness.** It is judged by the pingbus SessionStart record in the container,
  plus 5 s.

## Queue

`CLAUDE/Plan/meta-deploy.list` has the plan folder as its one entry. The header lines 1-21
are unchanged.
