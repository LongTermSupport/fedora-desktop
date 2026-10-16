# Task 2.17: ccy-teams waits for the play lock

Branch `fix/teams-wait-play-lock` from F44. ccy-teams only: no CCY or container bump. Spec:
SPEC-many-teams.md section G (S-G1 to S-G11).

## The defect

A description applied from ccy-teams while a meta-deploy unit held the play lock: run.bash
refused play-agent-bus.yml with exit 75, and ccy-teams died saying the team "now has its
description in the host_vars but is not applied as declared".

## Where the lock lives

- `helpers/play_lock/lock.py`: the lock is a flock(2) on `fedora-desktop-plays.lock` in the
  user's runtime directory (`$XDG_RUNTIME_DIR`, else `/run/user/<uid>`), opened with refusals (no
  symlinks, a real directory of the user's that others cannot write). The holder writes
  `pid=… what=… since=…`.
- `run.bash` `play_lock_take`: every play's shebang enters run.bash single-play mode, which takes
  the lock with `flock -n` and exits 75 with the holder line when it is held. A caller that holds
  the lock delegates it with an inherited descriptor and `FEDORA_DESKTOP_PLAY_LOCK_FD`, which
  `lock.py held` proves before run.bash goes on inside the hold (the self-update cycle does this).

## The design chosen: lock, then write, then apply

The delegation makes the lock reentrant for a child, so ccy-teams takes it first, writes the
host_vars, and runs every play inside the same hold. A wait that is stopped or reaches its bound
writes nothing, so no flow is left saved but not applied by the wait. This is cleaner than
write-then-apply, which would leave the half state the owner hit whenever the wait did not end
in the lock.

- `src-go/internal/playlock` (new): the Go side of the same lock (same path, refusals, holder
  line), with `Take(ctx, what, bound, busy)`: it tries flock non-blocking, and while the lock is
  held reads the holder, tells the caller, sleeps and tries again until the bound or the context
  ends. It ends only in flock succeeding on its own descriptor, so there is no check-then-take
  race. `Delegate` gives the descriptor and the variable for a child. A test runs the real
  `lock.py held` against a Go-delegated hold.
- `execx.Cmd.ExtraFiles`: the one process seam hands the descriptor to the child as fd 3.
- `ops.Locked(ctx, purpose, fn)`: takes the lock (at once when free, saying nothing), holds it
  for fn, and is the same hold when nested. While another run holds it, the wait says who (play,
  pid, since when, for how long) and waits up to 30 minutes: on a terminal a screen titled
  "Waiting for the play lock" with a visible `Stop waiting (nothing is changed)` row, redrawn when
  the holder changes or another minute passes and closed by itself once the lock is taken;
  without a terminal on stderr, where Ctrl-C stops it. `runPlay` and the Element play refuse to
  start outside a hold and hand the hold to the play.
- Every flow that runs a play writes inside the hold: settings Save and apply (the unsaved
  changes stay on the screen), the start-up describe and path-limit offers (asked again next
  start), chooser scope widening, create, lxc-add (including a takeover's demote), remove
  (declared absent and the play), Element set-up, and `ccy-teams apply`.
- Exit 75 still reaching ccy-teams (a play refused the lock regardless) is said as "did not run:
  it was refused the play lock (exit 75, above), which another play run took", followed by the
  flow's saved-but-not-applied line; the board then shows the team out of date.

## Verification

- `go test -race ./internal/...`, the ccyteamstest tag build of ops, and golangci-lint: clean.
- `scripts/test-ccy-teams-command.bash` (373 passed) now runs in a runtime directory of its own
  and checks that the stub play is handed the lock as run.bash proves it (the real `lock.py held`); `scripts/test-ccy-container-name.bash` passed.

## For the owner to decide

- run.bash itself still refuses at once with exit 75: a meta-deploy unit that meets a held
  lock (for example a ccy-teams apply in progress) fails rather than waits. Making run.bash wait
  the same way would cover every caller, but changes meta-deploy and the panel's behaviour and
  run.bash is on the bash-limits ratchet; not done here.
- The bound is 30 minutes, a constant (`ops.DefaultPlayLockWait`).
