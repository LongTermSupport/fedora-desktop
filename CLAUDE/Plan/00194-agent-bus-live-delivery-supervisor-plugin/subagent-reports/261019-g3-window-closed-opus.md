# G3 (tmux window closed) told the teams nothing: cause and fix (Task 2.0h)

The host acceptance at F44 7f0d8e3b passed G1 (Ctrl-C) and G2 (podman stop) and failed G3:
after `tmux kill-window` the launcher exited and the container went away, but the team room got
neither a goodbye nor a disconnected notice ("posted 0 disconnected notice(s), want 1").

## Which of the two readings

Neither as stated. The launcher did reach `ccy_bus_disconnected`, and `pingbus seat disconnected` returned 0 having told no one, but not through one of the silent returns: through
the "still held" path, whose one line went to the dead terminal. The cause is that the
launcher's clean-up ran **while the container was still running and holding the seats**.

## How it is known (reproduced in the container)

A stand-in launcher (`set -e`, the launcher's own `debug` and `cleanup` cut out of it, the real
`lib/common-pure.bash` and `lib/agent-bus-membership.bash`, a stand-in podman in the foreground
that takes 2 s to exit on SIGHUP, a stand-in pingbus that logs its run) was run as the pane of a
private tmux server, then `kill-window`. Its log, in order:

```
launcher <pid> starting podman
podman running
EXIT trap entered rc=0
... cleanup steps, pingbus called, pingbus done, cleanup returned
podman got signal 1
```

1. The launcher is the pane's process, so it is the session leader of the pty. The kernel's
   hangup sends SIGHUP to the session leader only.
2. A non-interactive bash that gets SIGHUP (no HUP trap) while waiting for a foreground child
   runs its EXIT trap at once; it does not wait for the child. `podman returned` was never
   logged.
3. podman gets SIGHUP only when the launcher, the session leader, exits (the kernel then
   signals the terminal's foreground process group). So during the whole clean-up the
   container ran on, its seat keeper holding the seat locks.
4. `pingbus seat disconnected` waits up to 15 s for each seat lock, then writes "still held ...
   no disconnect notice" to stderr (the dead pane, so lost) and exits 0. The launcher then
   exits, podman is signalled, and the container ends with its claim still in `seat.lock`, but
   no one is left to tell it.

G2 passed because `podman stop` ends the container first and podman returns normally before the
launcher's clean-up runs.

A second finding, proven with a plain `kill -HUP` test: a second SIGHUP arriving while a
non-interactive bash runs its EXIT trap ends bash there and then (the trap is cut short).
`trap '' HUP INT` at the start of the trap prevents it. The old clean-up ignored only SIGINT,
and only around pingbus.

## The fix

- `cleanup()` (claude-yolo) ignores SIGHUP and SIGINT from its first line and puts them back at
  its last (a restart's exec would otherwise inherit them ignored); `ccy_bus_disconnected` no
  longer manages SIGINT itself.
- `ccy_bus_disconnected` (lib/agent-bus-membership.bash) first asks the engine whether the
  launch's container still runs (`ps -q --filter name=^<name>$`; the name is now the fourth
  item of `CCY_BUS_DISCONNECT`) and if so stops it (`stop -t 10`, as G2 does) before pingbus.
  After a normal exit the container is already gone and nothing is stopped. A probe that fails
  is said and logged, and the teams are still told.
- The "goodbye already posted means no disconnected notice" rule is unchanged: pingbus still
  reads `said-goodbye.json` for the claim.

## The record that survives a dead terminal

`.claude/ccy/pingbus/disconnect.log` in the checkout (beside `seats/` and `sessions/`), mode
0600, appended, never a message text or token:

- `pingbus seat disconnected` writes one line per seat: `told: claim N`, or `not told:` with the
  reason (no claim, older than this launch, goodbye already posted, still held, a session took
  the seat, the claim changed), or `failed: <message>`; and one line for a session with no
  applied report or no seats. A log that cannot be written is exit 78, after the notices.
- The launcher writes its own steps: a container found still running and stopped, a probe or
  stop that failed, and pingbus's exit (0, a failure, or the timeout). A write to a stderr that
  has gone is noted in the line and never stops the clean-up.

## Tests (written first, seen red)

- `tests/helpers/pingbus/test_seat_disconnected.py`: `DisconnectedLogTest` (each reason, the
  told line without the notice text or handle, owner-only, a 0644 file made 0600, appended
  across runs, a failure, no report, an unwritable log is 78 after the notice), plus the
  still-held and dead-stderr cases now check the log.
- `scripts/test-ccy-teams.bash`: a fake engine; the launcher's real `cleanup` cut out of it and
  run with the container in the foreground, then SIGHUP and a second SIGHUP during pingbus: the
  container is stopped, then pingbus runs to its end, and the log says so (owner-only). Removing
  the HUP shield alone turns it red. Also: a container already gone is not stopped, a pingbus
  failure is logged, a failed probe is said and logged and the teams are still told, Ctrl-C
  mashed through the real clean-up, the signals put back afterwards, and cleanup's first and
  last lines.
- The fixed clean-up, re-run under the real tmux `kill-window` with the stand-ins, logs the stop
  before pingbus, and pingbus's exit.

## Acceptance

G3 now launches through a wrapper that execs the launcher (so it stays the pane's session
leader) with stderr copied by a `tee` that ignores SIGHUP and SIGINT into
`<run>/<session>/launcher.stderr`, tried under tmux: lines written after the window closed reach
the file. Its stderr is then a pipe, so the launch counts as unattended, which with every seat
named asks nothing more. After the launcher has exited, G3 prints and keeps the launcher's
stderr and the checkout's `disconnect.log` before the room verdict.

## Deploy

`play-agent-bus.yml` (host pingbus), `play-claude-yolo.yml` (CCY 3.99.12.18, container 2.77.14,
merged with Plan 00203's pending ccy-teams entry), then the 00194 acceptance, queued in
`meta-deploy.list`.
