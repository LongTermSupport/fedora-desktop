# Task 4.2: the ccy launcher's v2 side

What Task 4.2 built, the decisions it took where the design left an input open, and what is
left. PLAN.md's Task 4.2 entry points here.

## Built: the end of the container's start is Go

`entrypoint.sh` is on the bash ratchet (`scripts/bash-limits.allowlist`), so the v2 launch
could not be added to it. Its end was ported instead (the coordinator's decision): the
supervisor wrap, the lifecycle and first-join relaunch plugins, the v1 bus kit step and the
final exec are `src-go/internal/ccystart` behind the thin `cmd/ccy-start-claude`, built static
into the image's build context by `tasks/claude-yolo-image.yml` and copied to
`/usr/local/bin/ccy-start-claude` by the Dockerfile. `entrypoint.sh` keeps the D62 refusal
(it needs the snapshot taken before `ccy.env` is sourced) and ends in
`exec /usr/local/bin/ccy-start-claude "$@"`, a net shrink of about 190 lines. CCY 3.115.0,
container 2.82.0.

- **v1 is byte for byte the bash.** `ccystart/parity_test.go` runs
  `testdata/entrypoint-tail-v1.bash.txt` (the old tail, frozen) with its paths in a scratch tree
  and its `exec` recorded, and `ccystart.Start` over the same tree, for 57 scenarios taken from
  the two bash suites that tested the tail (wrappers, `--no-supervise`, the default supervisor
  and its syntax probe, the seated kit and each missing file, a launcher older than the image,
  every lifecycle refusal, the python-on-a-shell-script refusal). Exit status, stderr, argv,
  the `CCY_CLAUDE_WRAPPER` the session inherits and the pingbus link must all be equal.
  `scripts/test-ccy-agent-bus.bash` keeps the D62 cases and the replay through the real
  zipapp; the tail cases it and `test-ccy-lifecycle.bash` held moved to the parity test.
- **v2 (seam 53's container half).** With `TEAMCHAT_MEMBERSHIP=/run/teamchat/membership` and
  `PINGBUS_MEMBERSHIP=off`, `ccy-start-claude` links `/usr/local/bin/teamchat` to the image's
  kit and execs
  `teamchat run --claude --kind ccy --desired /run/teamchat/membership -- <wrapper> claude ...`,
  the wrapper being the line v1 would run (supervisor, `--arm`, the plugins). Refused (exit 1,
  nothing linked): a v1 membership beside it (OD-D2), any other value, the kit's binary or
  `desired.json` missing, a command other than claude. `start_test.go` holds each.

## Decisions

- **L1. The v2 signal is `TEAMCHAT_MEMBERSHIP`**, set by the launcher to the mount point, beside
  `PINGBUS_MEMBERSHIP=off`. D 3.6 says env is `TEAMCHAT_*` only; the variable is consumed by
  `ccy-start-claude` and unset for the session, so `run` reads no launcher variable (B 2.4.1)
  and v1's hooks never see it. A project `ccy.env` cannot forge it: the D62 snapshot now covers
  `TEAMCHAT_*` as well as `PINGBUS_*`.
- **L2. The variables `ccy-start-claude` reads are exported by `entrypoint.sh`**
  (`CCY_CLAUDE_WRAPPER`, `CCY_SUPERVISOR_PATH`, `CCY_NO_SUPERVISOR`,
  `CCY_LIFECYCLE_MAX_AGE_SECONDS`, `CCY_LIFECYCLE_DEADLINE_EPOCH`), because a `ccy.env` may set
  them without `export` and the bash saw them anyway. The one difference from the bash: such a
  value is now also in the session's environment. The command line is unchanged.
- **L3. Seam 55 on a member machine with no status record** (the cross-network pilot's case,
  unsettled in D 3.9). Chosen, as the simplest reading consistent with D 3.9 (one Go function,
  readable without sudo, no host_vars): a team is v2-live if its status record says so (D 3.9 as
  written), or else if the owner's seat store holds a pool seat for that team id **in the
  session's own checkout**: `<seat store>/pool/<checkout key>/<team>/<seat>` (`paths.Store.PoolTeam`;
  the key is the working directory's real path, as `seat.Store` and `place-issued` key it), which
  only a v2 issue placed in this checkout can have put there. Other checkouts are not scanned: a
  seat there cannot be taken by this launch (C 3.7.3's take is per checkout), so it would only
  turn a launch into a refusal later. A placement's claim directory (`<seat>.new-<claim>`) is
  not a seat. Any other team is v1-live. A string that is both a declared v1 team and v2-live is
  refused (exit 64), naming both, as a session on both systems is. A status record that names a
  migration set is refused (exit 78): set records are not read yet (N6), and D 3.9 forbids
  inferring from a missing field. Built: `internal/teams/system` (`ccy-teams system`).

## Built: the system decision and the launch take, as Go (not wired yet)

- `internal/teams/system` and `ccy-teams system <team>...` (seam 55, D 3.9, L3): one line per
  team, `<given> <id> <system>` on stdout, or exit 64 (both systems, or a name on both) or 78
  (a status record naming a migration set). `system.PoolSeats` lists a checkout's pool seats.
- `internal/teams/teamchatlaunch` (seams 53, 54, 27 as a library): `MountSources` and `Guard`
  (every `-v`, `--volume` and `--mount` source by real path, refused when it is an ancestor of,
  equal to or inside the seat store root; a path not made yet is judged where it would land);
  `ChooseSeat` (the pool seat whose handle's seat part is the launch's `<seat>`, refused for none
  or two); `HandleState` (the roster's state, `StateNoRoster` with no status record; a handle off
  the roster or `claimed` is refused); `Desired` (generation 1, `accept`); `RunArgs` (the two
  binds, the two variables, `--stop-timeout 20`); `Launch.Run` (guard, choose, take all or none,
  then `control/<session>/desired.json` 0600). Adapters: `RegistrySessions` (`seat.Sessions`
  over ccy's session registry), `Announcer` (`seat.Announcer`: area B's seat disconnected verb
  run in process, its exit judged as the store wants), `StoreTaker` (one allocation lock; a
  failed take releases the seats before it).

## Built: the launcher's wiring (CCY 3.116.0)

Items 1-5 of the handoff below are built: a ccy launch whose list names teamchat seats starts on
v2. `teamchatlaunch.Verbs` is the four `ccy-teams internal` verbs; the bash libraries swapped a
command word each and stay within the limits.

- **L4. The exit is told the container's state.** `bus-disconnected <status> <pingbus> ...`:
  `ccy_bus_disconnected` passes the container's id while it still ran (the launcher left on a
  signal), else the launcher's `container_rc`. An exit status of 75 is a relaunch (RS3:
  `seat.Store.Restarted` records the run in `held/notice.json` as `restart`, not posted, and keeps
  the seat). Any other status is a session that is over: its record is removed, then each seat
  it holds in the checkout is released (seam 54). A status that is not a number is the
  signal case: each seat is announced and kept (the crash path; the record stays, as the
  trampoline does not run). A returned launcher is an ended session whatever its status, as the
  registry already treats it (`ccy_registry_trampoline`), so a crash with podman returning is
  released, not kept. The disconnect log of a v2 session is `<seat store>/control/disconnect.log`.
- **L5. A relaunch or a restore keeps its seats.** `seat.Take` refuses a seat whose `held/`
  names a restorable session, and the session's own seat directory, so a relaunch could not
  take its seats again. `StoreTaker` skips a seat the launch's session already holds (complete
  `held/` naming it) and takes the rest.
- **RS2 is on** (`ends.restartSwitch`), with RS3. It reaches a running session with the next
  container version bump, as the switch is in the image's `teamchat`; an image before that
  posts `left exit` for a relaunch (RS1) and the host now posts nothing for it.
- `describe-seats --block` names a teamchat seat as on teamchat rather than asking agent-bus's
  roster. `choose-teams` still knows v1 teams only, so a v2 launch names its seat.

## Built: the owner's seat sweep (item 6)

`ccy-teams seats sweep` (`internal/teams/seatsweep`, C 3.7.5, DC21) runs `teamchat/sweep` over
every checkout of the owner's seat store (`pool/<checkout key>/`), each checkout opened as its
own `seat.Store` with the launcher's parts (`teams.storeParts`: the clock, credential and reach
of `placeissued.OS`, ccy's session registry, area B's disconnected verb in process). A seat whose
session is over is released, a seat its session no longer desires is released as a leave, and a
seat a restorable session desires is kept and announced once for a run that ended (the store's
judgement, SW2-SW4). One line per seat released on stdout; a pool entry that is not a checkout
key, a store that will not open or a seat that cannot be settled is said on stderr, the others
are still swept, and the run exits 1. The desired seam reads `control/<session>/desired.json`
(no link followed, capped, decoded strictly, its `session` checked): a missing file is still
desired, because a launch writes it after its take; otherwise a seat is desired when its team and
its handle's seat part are listed.

The units are Go's text, as teamchat-admin's are: `ccy-teams seats units list` (name and path
under the home) and `seats units print <name>`. `teamchat-seats.service` is a oneshot of
`%h/.local/bin/ccy-teams seats sweep`, `TimeoutStartSec=120`; `teamchat-seats.timer` is
`OnActiveSec=30s`, `OnUnitActiveSec=60s`, `AccuracySec=5s`. `play-claude-yolo.yml`, which
installs ccy-teams as the login user, writes what they print into `~/.config/systemd/user/` and
enables and starts the timer (a user `daemon_reload` when a unit changed). A Go test holds the
play's install path and tasks to the units.

- **L6. The timer is enabled on every ccy desktop; the service runs only where a seat was
  placed.** C 3.7.5 says the play enables the timer only while a v2 team is present. On a member
  machine (the cross-network pilot) no team is declared, and a seat placed after the play would
  wait for the next play run. So the service carries
  `ConditionPathIsDirectory=%S/teamchat/seats/pool`: with no pool, systemd skips it and starts no
  process, which keeps C40's "a host with no team starts nothing", and the first placement turns
  the sweep on with no play run. A host's play installs the units only where ccy is installed
  (play-claude-yolo.yml), so a bus server host without ccy has neither. Confirmed by the owner
  (desktop Q22, option 1), over C 3.7.5's rule.

## Left

Nothing of the launcher's side. A real ccy launch on a v2 team, and the sweep announcing one, need
the host and a placed pool seat (Task 4.4, gate B).

## The handoff items 1-6 (built, kept for the record)

1. The verb `ccy-teams internal launch-args <session> <checkout> <seats> <Z or empty> -- <mount args>`: resolve the system (`system.Cmd.Lines`). For agent-bus, run
   `membership-init` in process (split `membership.Init.Run` into a `Prepare` that returns the
   directory) and print `<checkout>/.claude/ccy/pingbus/seats`, then the four arguments the
   bash builds today. For teamchat, open the store (`placeissued.OS` gives the clock, the
   credential and the reach; plus `RegistrySessions` and `Announcer`), run `Launch.Run`, and
   print `store.Control(session)` then `RunArgs`. In `ccy_membership_args`, lines 62-68 become
   that call (the mount arrays passed with the `${A[@]+"${A[@]}"}` form, as the lib tests
   leave them unset), the same error block, one `mapfile -O` of the lines after the first into
   `CCY_SEAT_RUN_ARGS`, and `CCY_BUS_DISCONNECT` with the first line as its root: seven lines,
   as now.

2. `agent-bus seat check` and `seat take` (in `agent-bus-seats.bash`) refuse a v2 team. Replace
   the command word with `ccy-teams internal seat-check` and `seat-take`, which exec agent-bus
   with the same arguments for agent-bus and, for teamchat, print the canonical list (check) or
   do nothing (take: `launch-args` took the seats). `describe-seats --block` needs a teamchat
   branch. `ccy --teams <team>` alone goes to `choose-teams`, which knows v1 teams only: until it
   learns teamchat, a v2 launch names its seat (`<seat>@<team>`).

3. Exit (seams 54, 27): replace the pingbus command word in `ccy_bus_disconnected` with
   `ccy-teams internal bus-disconnected <pingbus>`, which execs pingbus as before unless the root
   is the seat store's `control/<session>`, and otherwise removes the session record, then
   `Release`s each seat of `desired.json` (a crash, record still there: `Announce`, seat kept).

4. `scripts/test-ccy-teams.bash` fakes `ccy-teams`: the fake answers `internal launch-args` as
   it answered `membership-init`, and forwards `seat-check`, `seat-take` and `bus-disconnected`
   to the fake agent-bus and pingbus without a log line of its own, so the expectations keep
   their meaning. CCY version bump with the wiring (above 3.115.0, container above 2.82.0).

5. RS3 and RS2: on a relaunch (exit 75) skip `seat disconnected` and record `held/notice.json`;
   in the same commit set `ends.restartSwitch = true`, flip RS1's pinned test and close PLAN.md's
   2.0e row.

6. `ccy-teams seats sweep` and the `teamchat-seats` user units (C 3.7.5, DC21).
