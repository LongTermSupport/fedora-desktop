# Review: fix/ccy-name-race at 0aa93438 (the container name race)

Adversarial review of the developer's report `261016-name-race-opus.md`. Read-only: no qa-all, no
commits. Verdict at the end.

## What I ran

- `go test ./internal/teams/containername/ ./internal/sessprobe/ ./internal/teams/` (src-go
  module): all ok. `go test ./all/` in the `src-go/qa` module: ok, so the addedgates row is
  accepted by the golden replay. `go vet` on the new package: clean.
- `scripts/test-ccy-container-name.bash`: 14 of 14 pass.
- Red proof, done independently: a copy of the suite pointed at a `lib/` whose `common.bash` is
  F44 741191c3's. 5 of 14 fail: two launches both got `proj_yolo_1` and one create was refused;
  four launches all chose `proj_yolo` and three were refused; the three fail-loudly checks pass
  trivially on old code (it never fails). That matches the developer's claim exactly.
- `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK with the lowered `common.bash` record (698/21/721).
- `exec` and the start time: `bash -c 'a=$(field 22 of /proc/$$/stat); exec bash -c "..."'`
  prints the same value before and after the exec. The kernel's start_time belongs to the task,
  not the image, so the restart path (`ccy_handle_restart_exit` execs `claude-yolo` in the same
  pid) keeps its claim identity. The suite's "relaunch by exec" case covers the same thing.
- A scan of every added line for home paths, `/run/user/<n>`, non-example e-mail domains and
  host names: nothing.

## 1. Is the race closed?

Yes, between two and among N. The invariant is: under the flock, the chosen name is in neither
the engine's `ps -a` nor any live claim, and the claim is written before the lock is released.
Two launchers serialise on the lock; the second always sees the first's claim, whose process is
alive (it is between choosing and `podman run`, or in `podman run`). The N-way case is the same
argument applied N times, and the suite's four-way case plus the Go test's eight concurrent
claimants exercise it.

The one deliberate ordering choice, listing containers *before* taking the lock, is sound for
the reason the developer gives: a container that appears after the list was read was created by
a launcher that claimed first, and that launcher is alive while its container runs, so its
claim still blocks the name when the lock is taken. The residual is in section 3.

## 2. Claim lifecycle

- **Released** when the owner pid is gone or has a different start time, found by the *next*
  claimant under the lock (`held`, containername.go:267-289), or when the owner claims again
  (its own claim is removed first). There is no release on exit; none is needed, because
  liveness is read from `/proc`, and `$XDG_RUNTIME_DIR` is tmpfs so nothing outlives a boot.
- **Long-lived session**: the claimant is `$$` of `claude-yolo`, which is the main shell even
  inside `$( )` (claude-yolo:3470). That shell runs `podman run` in the foreground
  (claude-yolo:3887) and does not exec into it, so it lives for the whole session. The
  trampoline (`ccy_tmux_insulate`, claude-yolo:1171) execs into tmux *before* 3470, so the outer
  process never claims; only the inner launcher, a tmux pane process, reaches 3470. Good.
- **Restart paths**: the age restart and the M3 relaunch both leave `podman run` with exit 75
  and exec `claude-yolo` in the same process (claude-yolo:3843). Same pid, same start time, own
  claim removed first, so it chooses again and is not blocked by itself. It does not reuse the
  name explicitly, which is the old behaviour too (the old code also re-listed `ps -a`).
- **Reboot**: claims vanish with the tmpfs, which is correct: every pid is new. The boot
  restore (`ccy-sessions-restore.service`, a user unit) starts sessions from one process with
  `systemd-run --user --scope` into the ccy tmux server; each inner launcher is its own pid and
  claims under the lock, so a restore of many sessions is the N-way case. The user manager's
  environment carries `XDG_RUNTIME_DIR`.
- **Stale claim blocking a name forever**: impossible while `/proc` is the oracle; a claim
  needs a live pid *and* the same start time. A pid reused by another process has a different
  start time and is removed. A dead claim read as live would need pid reuse with an identical
  tick-resolution start time, which does not happen within one boot. A zombie launcher keeps
  its claim until reaped; tmux and terminals reap promptly.

## 3. The launcher's `rm -f "$CONTAINER_NAME"` (claude-yolo:3613)

Now safe in every ordinary case: the chosen name is one no listed container had and no live
launcher claimed, so the only thing `rm -f` can hit is a storage-only ghost, which is what it
was written for.

Residual, far narrower than before: launcher B lists containers, launcher A (claim live)
creates `N`, A's shell is SIGKILLed (its container survives under conmon), then B takes the
lock, finds A's claim dead, chooses `N`, and its `rm -f` removes A's orphaned running
container. This needs A's death inside B's list-to-lock window of a few milliseconds, where the
old code had the same hazard across the whole 400-line setup. Not blocking.

## 4. Every launch path through the one call

`get_next_container_name` has exactly one caller, claude-yolo:3470, and nothing else in the
launcher, its `lib/`, or `files/var/local/claude-code/cc` chooses a container name. Interactive,
headless, `--teams`, `--no-bus`, the trampoline (inner launcher only), the boot restore (inner
launchers), docker (`CONTAINER_ENGINE` is exported by common.bash:51 and claude-yolo:675 and
read by the Go), the age restart and the M3 relaunch all pass through it.

Name consumers after 3470: `ccy_seats_take` and `ccy_membership_args` (the membership directory
is `$XDG_RUNTIME_DIR/ccy/membership/<session or container>`), the labels, and the `rm -f`.
None of these is keyed on the name across launches in a way the claim changes: a relaunch that
gets a different name creates a new membership directory for an unnamed session, which it did
before this change too. The registry and the records key on the tmux session name.

## 5. The test and its wiring

The suite proves the race: separate `bash` processes (so `$$` differs), a fake `podman ps` that
sleeps to hold the window open, a `create` that refuses a duplicate with podman's message and
exit 125. Red on old code (verified above), green on new. It also covers the dead-launcher
release, existing containers, the exec relaunch, project isolation, docker, and two fail-loudly
paths. Wiring: `qa-gates.tsv` row `ccy-container-name`, a `QA.md` row in the same position,
`go-binary-suites.tsv` adds it to the `ccy-teams` row, `addedgates_test.go` lists it; the
qa/all test passes.

Nit: the "engine fails" and "no XDG_RUNTIME_DIR" checks assert exit 91, which is the launch
script's own `|| exit 91`, so they prove the claim returned non-zero, not which code. Fine.

## 6. Project rules

- Podman and docker are driven only through `sessprobe.AllContainerNames`, which owns both
  program names and refuses any other engine. The `"docker"` literal in `join.go` became the
  shared constant. Respected.
- bash-limits: common.bash ratcheted down and the gate passes.
- CCY version 3.99.10.2 with the changelog entry; container 2.75.1 unchanged and no image
  input changed. Correct.
- Fail fast: every error path returns a non-zero code with a reason; no `|| true`.
- Tooling in Go, bash shrank to one line. Correct.

## 7. The queue

`meta-deploy.list`: `playbooks/imports/play-claude-yolo.yml` (builds ccy-teams at task
"Build and Install the ccy-teams Command", then deploys the launcher), then
`00203-teams-hot-control-and-robustness`. Correct order, with descriptions.

## 8. Install-specific names

None in any added line.

## Non-blocking findings

1. **A crash between write and rename poisons the claims directory**
   (`containername.go:259` via `fsx.WriteAtomic`, `fsx.go:16`). The temp file is
   `<name>.tmp.<pid>` *inside* `container-names/`. If the ccy-teams process is killed between
   `os.WriteFile` creating the file and writing it (a shutdown or `tmux kill-server` taking the
   whole scope), an empty file remains; `held` reads it as a claim, the parse fails, and
   *every* launch of *every* project on the host is refused with "remove it and launch again"
   until someone removes it or reboots. The window is microseconds, the message is exact, and
   it is fail-fast by the rules, but a boot restore of many sessions dying on one stray file is
   a heavy outcome for a benign cause. Fix: since the lock already serialises readers and
   writers, write the claim directly (no temp), or have `held` remove a zero-length or
   `*.tmp.*` entry under the lock instead of refusing.
2. The list-before-lock residual in section 3. Document it in the package comment if
   anything; no code change warranted.
3. Every launch, including plain and `--no-bus`, now requires an absolute `XDG_RUNTIME_DIR`
   where previously only a teams launch did (through membership-init). Every supported launch
   context (desktop login, ssh with pam_systemd, the user manager's restore unit and the ccy
   tmux server started by `systemd-run --user`) has it, and the failure names the variable.
   Worth one line in the changelog so a reader of a failed cron-style launch knows why.
4. Rootful docker is shared across users while claims and `XDG_RUNTIME_DIR` are per user, so
   two different users launching the same project under docker do not coordinate. Docker is
   compatibility mode and the engine's own name check still refuses the second; note only.

## Verdict

**MERGE.** The race is closed by a correct invariant, the proof is red-then-green and I
reproduced the red independently, every launch path goes through the one call, the restart
exec keeps its claim identity, the rules and the queue are respected, and nothing
install-specific is in the diff. Finding 1 is worth a follow-up commit but does not block.
