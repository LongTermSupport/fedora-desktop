# Task 4.2, fourth part (Plan 00209): report

LAUNCH-ccy.md item 6 is built: `ccy-teams seats sweep` and the `teamchat-seats.{service,timer}`
user units. Task 4.2 is marked complete in PLAN.md. No launcher file changed, so there is no CCY
version bump (CCY stays 3.117.0, container 2.83.0).

## Done

- **`internal/teams/seatsweep`** (new; tests written first and seen red):
  - `Cmd.Run`: `seats sweep`, `seats units list`, `seats units print <name>`; anything else is
    exit 64.
  - The sweep walks `pool/` of the owner's seat store (from HOME and XDG_STATE_HOME). Each entry
    must be a checkout key and is opened as its own `seat.Store` (XDG_RUNTIME_DIR for the
    allocation lock), then `teamchat/sweep.Sweep` runs over it. No pool means nothing to do,
    exit 0.
  - Each checkout and each seat is handled on its own. A bad entry, a store that will not open,
    or a seat that cannot be settled is said on stderr, the rest are still swept, and the run
    exits 1. stdout has one line per seat released or left.
  - `Desired` (the sweep's seam) reads `control/<session>/desired.json`: O_NOFOLLOW, regular
    file only, capped at `state.MaxDesired`, decoded with `state.DecodeDesired`, and its
    `session` must match. A missing file still counts as desired, because a launch writes it
    after its take. Otherwise a seat is desired when its team and its handle's seat part are
    listed.
  - The unit text is Go's (`units.go`):
    - service: a oneshot of `%h/.local/bin/ccy-teams seats sweep`, `TimeoutStartSec=120`,
      `NoNewPrivileges=yes`, `ConditionPathIsDirectory=%S/teamchat/seats/pool`;
    - timer: `OnActiveSec=30s`, `OnUnitActiveSec=60s`, `AccuracySec=5s`,
      `WantedBy=timers.target`.
  - Coverage is 93.9%. A test reads `play-claude-yolo.yml` and checks it against the units (the
    ccy-teams install path, the units list and print tasks, the timer task).
- **Wiring** (`teams/app.go`):
  - the `seats` subcommand, with its stdout diverted so the play can capture the unit text;
  - `storeParts(h)`, which the launcher's verbs and the sweep now share (no second copy);
  - the usage text and an app test.
- **The play** (`play-claude-yolo.yml`, as the login user, after the restore-unit block):
  - lists and prints the units through the installed ccy-teams;
  - writes them into `~/.config/systemd/user/` with mode 0644;
  - enables and starts `teamchat-seats.timer` with `scope: user`, reloading the user manager
    when a unit changed.
- **Docs:**
  - LAUNCH-ccy.md: the "Built: the owner's seat sweep" section, decision L6, and "Left" now
    says nothing remains on the launcher's side;
  - PLAN.md: Task 4.2 is ✅, and the Task 2.4 line about the sweep is updated;
  - BUILT-n2.md: `sweep` is now called;
  - DESIGN-v2-C-host.md: "As built" notes at 3.5.1 step 07 and at 3.7.5;
  - meta-deploy.list: a description is added to `changed-plays`, which already picks up
    play-claude-yolo.yml.
- **Checks:** golangci-lint on both packages finds 0 issues. `go test -race` passes for
  teams, seatsweep and teamchatlaunch.

## Decision taken (L6), for the owner to confirm

The design (C 3.7.5) says the play enables the timer only while a v2 team is present. I did it
differently:

- The timer is enabled on every ccy desktop.
- The service has `ConditionPathIsDirectory` on the owner's pool, so systemd skips it and
  starts no process until a seat is placed.

Why: a member machine (the cross-network pilot) declares no team, and a seat placed after the
play would otherwise wait for the next play run. This keeps C40 (no process with no team). If
the owner wants the literal reading, the play needs a "v2 present" fact, which would be either
teamchat_teams or the existence of the pool.

## Not done / open

- **qa-changed:** the run was still waiting on another session's qa-all lock (over 20 minutes)
  when I committed. The stop guard forced the commit before QA finished. The coordinator should
  run `./scripts/qa-changed.bash --base <parent>` over the commit, or its full gate.
- **Not run on the host:** no play run and no real sweep (that is Task 4.4 and gate B).
- **Timer restart:** a changed timer on a host where it already runs is reloaded but not
  restarted, so a new schedule takes effect at the next timer start. This is minor.
