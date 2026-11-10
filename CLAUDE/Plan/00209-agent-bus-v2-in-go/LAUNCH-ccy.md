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
  written), or else if the owner's seat store holds a pool seat for that team id
  (`~/.local/state/teamchat/seats/pool/<team>/`), which only a v2 issue can have placed. Any
  other team is v1-live. A string that is both a declared v1 team and a pool seat's team is
  refused (exit 64), naming both, as a session on both systems is. Not built yet.

## Left (the handoff)

The launcher does not pass `TEAMCHAT_MEMBERSHIP` yet, so no session starts on v2. In order:

1. `ccy-teams system <team>...` (seam 55, D 3.9 and L3): one line per team
   (`<given> <id> <system>`) or exit 64 with OD-D2's refusal naming the two choices.
2. The launch take, a `ccy-teams internal` verb replacing the `membership-init` call in
   `lib/agent-bus-membership.bash` with one line that reads back the podman arguments
   (`mapfile` over its output; the library has 4 functions and no room for a fifth): for v2,
   open `seat.Store` (`StateNoRoster` with no status record), choose the seat (C 3.7.4), `Take`
   into `sessions/<session>/`, write `control/<session>/desired.json` (C 3.7.5, generation 1,
   `inbound`), and print the two binds (`sessions/<session>` read-write at
   `/run/teamchat/seats`, `control/<session>` read-only at `/run/teamchat/membership`),
   `-e PINGBUS_MEMBERSHIP=off -e TEAMCHAT_MEMBERSHIP=/run/teamchat/membership`, and
   `--stop-timeout 20`. The mount guard (seam 53) over the launch's `-v` list belongs in the
   same verb, so it needs the launcher's mount list as input.
3. Exit (seams 54, 27): `ccy_bus_disconnected` calls a `ccy-teams internal` verb for a v2
   session instead of `pingbus seat disconnected`: remove the session record, then `Release`
   each held seat; a crash (record still there) runs `Announce` and keeps the seat. The
   `seat.Sessions` and `seat.Announcer` adapters are needed (handoff of stage B/C, items 4-5).
4. RS3 and RS2: on a relaunch (exit 75) skip `seat disconnected` and record `held/notice.json`;
   in the same commit set `ends.restartSwitch = true`, flip RS1's pinned test and close PLAN.md's
   2.0e row.
5. `ccy-teams seats sweep` and the `teamchat-seats` user units (C 3.7.5, DC21).
