# The container name race (host deploy at 0c61144d, 00203 M2 leg)

Branch fix/ccy-name-race, from F44 741191c3. CCY 3.99.10.2; container 2.75.1 unchanged (no
image file changed).

## Root cause

`get_next_container_name` (lib/common.bash) listed every container (`podman ps -a`) and
printed the lowest free name: `<project>_yolo`, then `_1`, `_2` and so on. The launcher calls it
at claude-yolo line ~3470. `podman run --name` creates the container more than 400 lines
later, at ~3888, after mounts, seats and membership are set up. Nothing joined the two steps:
no lock, no claim and no retry. When `ccy --headless` and `ccy --headless --no-bus` started
back to back, both lists were read before either container existed. Both launches printed
`<project>_yolo_1` (the base name was already taken), and the second `podman run` failed with
"already in use", exit 125.

There is a second hazard in the same window. The launcher's safety net,
`container_cmd rm -f "$CONTAINER_NAME"` just before `podman run`, could remove a container
another launch had just created under the same name. Once two launches can no longer choose the
same name, that cannot happen.

## Design chosen: a per-process claim, made atomically with the choice

`get_next_container_name` is now one line:
`ccy-teams internal claim-container-name "$1" "$2" "$$"`. The Go code is in
`src-go/internal/teams/containername`:

1. It reads the owner's start time from `/proc/<pid>/stat`. The owner is the launcher's `$$`,
   which is the main shell even inside `$(...)`.
2. It lists every container through `sessprobe.AllContainerNames` (podman or docker, from
   `CONTAINER_ENGINE`, which common.bash exports; sessprobe owns both programs under the
   project rules).
3. It takes an flock on `$XDG_RUNTIME_DIR/ccy/container-names.lock` and reads every claim in
   `$XDG_RUNTIME_DIR/ccy/container-names/` (one file per name, containing `<pid> <start>`).
   - A claim held by a different, still-running process (same pid and start time) is taken.
   - A claim whose process has gone, or whose pid now belongs to another process, is removed.
   - The caller's own claim is removed too.
4. It picks the lowest name that no container has and no live claim holds, writes the claim
   atomically, releases the lock and prints the name.

The container list is read before the lock is taken, and that is safe. A container created
after the list was read was created by a launch that claimed its name first, and that launch's
process is still running (podman run is in the foreground), so the claim still blocks the name.

**Why a claim and not a lock held until podman creates the container.** In this launcher,
`podman run` is a foreground `--rm` run for the whole session. It does not create detached and
then attach. A lock held "until create" would mean either restructuring the run into create
plus start-attach, or holding the lock across about 400 lines of setup. That setup includes
the seat take, which can wait at a sudo prompt on an interactive `--teams` launch, so one stuck
launch would block every other launch on the machine. The claim gives the same atomicity without holding a lock
for any length of time: the lock lasts only for a few file reads and one write. The claim then
lasts as long as the launcher's process, which means as long as the session.

**Why not retry on "already in use".** CONTAINER_NAME is used before `podman run`:
`ccy_seats_take` and `ccy_membership_args` receive it (the membership directory is named
after it for an unnamed session), and so does the seat record. A retry at `podman run` would
have to undo and redo all of that under a new name. Without a claim it is also still unsafe,
because the `rm -f` safety net could remove the other launch's container.

## Every launch path

All of these go through the one call at ~3470:

- Interactive and headless launches, `--teams`, `--no-bus`.
- The registry/tmux trampoline: only the inner launcher reaches ~3470.
- The boot restore (`ccy-sessions restore` starts each session's launcher, and each launcher is
  its own process).
- The age restart and the M3 relaunch both leave through the supervisor's exit 75 into
  `ccy_handle_restart_exit`, which `exec`s claude-yolo, so the pid and start time stay the
  same. Neither reuses a name explicitly: each chooses again. Because the caller's own claim
  is removed first, a relaunch is not blocked by its own earlier claim, and it usually gets the
  same name back (its old `--rm` container is gone by then).
- Docker sessions use the same chooser and the same claim; any other engine is refused.

No other caller of `get_next_container_name` exists.

## Tests (red, then green)

- `scripts/test-ccy-container-name.bash` (new; gate row `ccy-container-name`, a row in QA.md,
  and a ccy-teams suite in go-binary-suites.tsv). It builds the real ccy-teams. Each launch is
  a separate `bash` process that sources common.bash, calls `get_next_container_name`, then
  runs `podman create --name`. The fake podman sleeps 0.5 s inside `ps` to hold the race window
  open, and its create refuses a name that already exists with podman's message and exit 125.
  - The suite was run against F44's common.bash: 5 of 14 checks failed. These were the two race
    cases (two concurrent launches; four concurrent launches) plus the three fail-loudly checks.
  - After the fix, 14 of 14 pass, three runs in a row.
  - It also covers: a dead launcher's name is free again, existing containers are skipped, a
    relaunch in the same process gets its own name, another project is unaffected, docker,
    an engine failure and no XDG_RUNTIME_DIR.
- Go tests:
  - `containername_test.go`: a fake `/proc`, a stale claim, a reused pid, the caller's own
    claim, eight concurrent claimants on separate hosts, bad arguments, an unreadable claim, a
    symlinked claims directory.
  - `sessprobe/collect_test.go`: AllContainerNames.
  - `teams/app_test.go`: the dispatch and the usage text.
- `src-go/qa/all/addedgates_test.go` lists the new gate as added after the port, so the qa-all
  golden replay stays exact.
- `scripts/bash-limits.allowlist`: common.bash ratcheted down (719 to 698 code lines, 752 to
  721 statements).

## Queue

`CLAUDE/Plan/meta-deploy.list`:

1. `playbooks/imports/play-claude-yolo.yml`: rebuilds ccy-teams and deploys launcher 3.99.10.2.
2. `00203-teams-hot-control-and-robustness`: the acceptance again. Its M2 leg is the
   back-to-back launch that failed.

00161 passed in full and is not queued again.
